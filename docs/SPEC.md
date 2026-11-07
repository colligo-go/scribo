# Scribo — Technical Specification

Declarative document. For the why, see `DESIGN.md`. For everything that is shared with Colligo — database opening, `tool_runs`, `result_inputs`, `input_signals`, invariants and dependency graph —, `CONTRACT.md`, which prevails over this document in case of conflict. Implement from here.

Module: `github.com/colligo-go/scribo`
Go: the same version as Colligo, set by the `go` directive of `go.mod` (today 1.26.3), which CI uses through `go-version-file`. Raised in both repositories together, and always a release the Go team still supports.
SQLite driver: `modernc.org/sqlite`.
CLI: `github.com/spf13/cobra`.

---

## 1. Package structure

```
scribo/
├── main.go
├── cmd/
│   ├── root.go
│   ├── upsert.go
│   ├── log.go
│   ├── query.go
│   ├── export.go
│   ├── schema.go
│   ├── ingest.go
│   ├── stale.go
│   ├── diff.go
│   ├── outliers.go
│   └── signals.go
├── internal/
│   ├── db/
│   │   ├── backend.go
│   │   ├── sqlite/
│   │   │   ├── sqlite.go
│   │   │   ├── tx.go
│   │   │   ├── open.go
│   │   │   └── fsdetect.go
│   │   └── value.go
│   ├── contract/
│   │   └── contract.go
│   ├── history/
│   │   ├── history.go
│   │   ├── recordkey.go
│   │   └── reconcile.go
│   ├── provenance/
│   │   ├── inputs.go
│   │   └── graph.go
│   ├── rules/
│   │   └── rules.go
│   ├── ingest/
│   │   ├── multiqc.go
│   │   └── somalier.go
│   ├── stats/
│   │   └── robust.go
│   ├── run/
│   │   └── run.go
│   ├── writer/
│   │   └── writer.go
│   ├── reader/
│   │   └── reader.go
│   ├── format/
│   │   └── format.go
│   └── exitcode/
│       └── exitcode.go
└── testdata/
```

Everything in `internal/`. Scribo shares no code with Colligo (`DESIGN.md` §5): `db/sqlite/open.go`, `db/sqlite/fsdetect.go` and `contract/` implement `CONTRACT.md` here, and tests check them against a database created by Colligo.

---

## 2. The `db.Backend` interface

All persistence goes through here. SQL never lives in the handlers.

```go
type Backend interface {
    Open(ctx context.Context, path string, opts OpenOptions) error
    Close() error

    JournalMode() string
    Warnings() []exitcode.Warning
    Classification(ctx context.Context) (string, error)
    ColligoPresent(ctx context.Context) (bool, error)

    TableExists(ctx context.Context, table string) (bool, error)
    Tables(ctx context.Context) ([]string, error)
    UniqueKeys(ctx context.Context, table string) ([][]string, error)
    Columns(ctx context.Context, table string) ([]Column, error)
    EnsureContractTables(ctx context.Context) error
    EnsureHistoryTable(ctx context.Context, table string) error
    BeginRun(ctx context.Context, meta RunMeta) (int64, error)
    EndRun(ctx context.Context, runID int64, exitCode int) error

    SelectRows(ctx context.Context, table string, where []Predicate, fields []string, limit int) (Rows, error)
    ResolveSample(ctx context.Context, ref string) (Sample, error)
    FileRoot(ctx context.Context, path string) (string, bool, error)
    InputFacts(ctx context.Context, roots []string) (InputFacts, error)
    InvalidatedNodes(ctx context.Context) ([]Node, error)
    Edges(ctx context.Context) ([]Edge, error)
    HistoryBetween(ctx context.Context, table string, runA, runB int64) ([]HistoryRow, error)
    HistoryOf(ctx context.Context, table, recordKey, field string) ([]HistoryRow, error)
    RecordExists(ctx context.Context, table string, key Key) (bool, error)

    BeginTx(ctx context.Context) (Tx, error)
}

type Tx interface {
    SelectRow(ctx context.Context, table string, key Key, fields []string) (Row, bool, error)
    ProjectedRow(ctx context.Context, table string, recordKey string) (Row, error)
    Upsert(ctx context.Context, table string, key Key, values Values, mode WriteMode) ([]FieldChange, error)
    AppendHistory(ctx context.Context, table string, recordKey string, changes []FieldChange, runID int64, at time.Time, source HistorySource) error
    AppendInputs(ctx context.Context, edges []InputEdge) error
    AppendSignals(ctx context.Context, signals []Signal) error
    Commit() error
    Rollback() error
}
```

`SelectRow` returns `false` when no record matches the key and an `ambiguous_key` error (exit 3) when two or more do: the key columns must be `UNIQUE`. It matches with `IS`, so a NULL key value matches a NULL, and it reads every column as `+"column"`: the unary plus gives the expression no declared type, so the driver does not turn the text of a `DATE`, `DATETIME` or `TIMESTAMP` column into a `time.Time` that would no longer read back as the same text. `Upsert` reads the record, writes with `INSERT` or `UPDATE`, reads it again and reports the fields whose stored text differs, ordered by field name; a record that did not exist has a NULL old value for every field. `Rollback` after `Commit` returns nil so that `defer tx.Rollback()` is safe.

`SelectRows` serves `query` and `export`: it reads outside any transaction and never feeds the history. Columns are read as `+"column"`, like `SelectRow`, and rendered as in §7. The predicates are joined by AND; `OpIsNull` and `OpNotNull` are `IS NULL` and `IS NOT NULL`, `OpNe` is `IS NOT ?` (so a NULL is kept: `!=` is the complement of `=`), and a predicate with `AsText` compares `CAST("column" AS TEXT)` with the value as text. Otherwise the value is bound as in `Coerce`: a number for a numeric column, text for any other. Records are ordered by the columns of the first unique key (§4) and then by `rowid` when the table has one, so the order is the same on every read; `limit` greater than zero adds `LIMIT`.

`HistoryOf` and `RecordExists` serve `log` and never feed the history: they read outside any transaction. `HistoryOf` on a table without `{table}_history` returns no rows and creates nothing.

While a `Tx` is open, every method of the `Backend` returns an `internal` error, because the pool holds one connection and the call would otherwise wait for ever. Every identifier that comes from the user is quoted with `quoteIdent` (double quotes, an embedded quote doubled); values always go as `?` parameters.

`Tx` does not embed `Backend`: a nested transaction does not exist in SQLite, and the narrow interface makes that impossible to write. The reads that feed the history — `SelectRow` and `ProjectedRow` — exist **only** on `Tx`, so that it is impossible to read them outside the transaction that writes.

`Open` takes the path of an existing database file, not a DSN: the DSN is built inside the SQLite backend. A `Backend` never writes to stderr; `Warnings()` returns what `Open` found (the network-filesystem notice) and the command layer prints it.

`BeginTx` opens `BEGIN IMMEDIATE` (`_txlock=immediate` in the DSN).

Auxiliary types:

```go
type WriteMode int

const (
    ModeUpsert WriteMode = iota
    ModeInsertOnly
    ModeUpdateOnly
)

type HistorySource int

const (
    SourceScribo HistorySource = iota
    SourceReconciled
)

type OpenOptions struct {
    JournalMode string
    BusyTimeout int
    Restricted  bool
}

type Column struct {
    Name         string
    DeclaredType string
    Affinity     string
    Nullable     bool
    Declared     bool
}

type Key    map[string]Value
type Values map[string]Value

type Value struct {
    Raw    string
    IsNull bool
}

type Op int

const (
    OpEq Op = iota
    OpNe
    OpLt
    OpLe
    OpGt
    OpGe
    OpIsNull
    OpNotNull
)

type Predicate struct {
    Field  string
    Op     Op
    Value  Value
    AsText bool
}

type FieldChange struct {
    Field    string
    OldValue Value
    NewValue Value
}

type RunMeta struct {
    ToolVersion string
    GitSHA      string
    GoVersion   string
    ArgvShape   string
    Step        string
    Host        string
    ConfigHash  string
}

type Sample struct {
    ID           string
    Accession    string
    ExperimentID string
    Role         string
    Replicate    string
}

type InputEdge struct {
    RunID       int64
    ResultTable string
    RecordKey   string
    Field       string
    InputRoot   string
    InputKind   string
}

type InputFacts struct {
    Reads int64
    Bases int64
    Known []string
    Missing []string
}

type Signal struct {
    RunID     int64
    InputRoot string
    Kind      string
    Source    string
    Detail    string
}

type Node struct {
    ID     string
    Kind   string
    Reason string
}

type Edge struct {
    From string
    To   string
}

type HistoryRow struct {
    ID        int64
    RunID     int64
    Field     string
    Source    string
    ChangedAt string
    Old       Value
    New       Value
}

type Row  map[string]Value
type Rows []Row
```

---

## 3. Opening

Per `CONTRACT.md` §2: filesystem detection, PRAGMAs in the DSN with `_txlock=immediate`, read-back on each connection, check of the contract tables. `SetMaxOpenConns(1)` for the writing process.

- The database must already exist: `Open` checks the file first (`file_not_found`, exit 4; `permission_denied`, exit 5) and the DSN carries `mode=rw`, so the driver never creates a missing file on its own.
- The DSN is a `file:` URI built with `net/url`, never by string concatenation, so a path with a space, `#`, `?` or `%` still names the right file. The PRAGMAs go in as the driver's short keys (`_busy_timeout`, `_foreign_keys`, `_journal_mode`, `_synchronous`) next to `_txlock=immediate`; the driver validates all of them before applying any, so a wrong DSN never leaves the database half converted.
- Read-back is a connection hook registered once for the driver: on every new connection it parses the DSN it was given, runs `PRAGMA <name>` for each of the four keys present and compares. A difference is `pragma_ineffective` (exit 5), for example `PRAGMA journal_mode is memory, expected wal`. A DSN without those keys is not checked.
- The contract tables are checked against a reference, not against a hand-written column list. `Open` runs the DDL of `CONTRACT.md` in a separate in-memory database and compares, in both, `PRAGMA table_info` (name, type, NOT NULL, default, primary key, position), `PRAGMA foreign_key_list` and the `strict` flag of `PRAGMA table_list`. The DDL in `internal/contract` is the single source of truth, and a test checks that it appears verbatim in `CONTRACT.md`. The first difference is named in the `contract_mismatch` message (exit 2) and nothing is written. Known limit: `CHECK` constraints appear in none of these PRAGMAs and are not compared. Only `tool_runs` is checked in Slice 1; `result_inputs` and `input_signals` arrive with Slice 3.
- `BeginRun` and `EndRun` each run in their own transaction, so the `tool_runs` row is committed before the write it describes and survives its rollback. `EndRun` only closes an open row whose `tool` is `scribo`; anything else is an `internal` error.
- Filesystem detection is `statfs` on the database directory: the Linux magic numbers of `linux/magic.h` and the macOS `f_fstypename`. A network filesystem opens in `DELETE` and produces a `network_filesystem` warning. Linux and macOS are the only targets.

Then:

1. Read `study_meta` if it exists. A `restricted` classification turns on the regime of §11.
2. `ColligoPresent`: `samples`, `files` and `facts` exist with the contract's schema. Result cached per process; features that depend on Colligo consult this value and report unavailability instead of failing silently.

---

## 4. Tables that Scribo creates

Those of the contract that it owns — `result_inputs`, `input_signals` — and its part of `tool_runs`, all with the DDL of `CONTRACT.md` §3. Plus one per data table:

```sql
CREATE TABLE IF NOT EXISTS {table}_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES tool_runs(id),
    record_key  TEXT NOT NULL,
    field       TEXT NOT NULL,
    old_value   TEXT,
    old_is_null INTEGER NOT NULL,
    new_value   TEXT,
    new_is_null INTEGER NOT NULL,
    source      TEXT NOT NULL CHECK (source IN ('scribo','reconciled')),
    changed_at  TEXT NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_{table}_history_record ON {table}_history(record_key, changed_at, id);
CREATE INDEX IF NOT EXISTS idx_{table}_history_field  ON {table}_history(field, changed_at);
CREATE INDEX IF NOT EXISTS idx_{table}_history_run    ON {table}_history(run_id);
```

The data table is never created nor altered.

`old_is_null` and `new_is_null` separate NULL from empty text without depending on a magic value.

`changed_at` never goes back within a record. The order of the history is `(changed_at, id)` and `changed_at` comes from the clock of whoever writes: two cluster nodes with clocks apart, or an NTP step backwards, would give a later write an earlier `changed_at`, and the projection (§6) would pick the wrong value. Before appending, `AppendHistory` reads `max(changed_at)` of the record (through `idx_{table}_history_record`) and writes the later of that and its own time; the fixed format makes string order the order of time, and on a tie `id` decides. The rule is per record: other records keep their own clock.

Key fields never get a history row: they are in the `record_key`. The first row of a record that did not exist has `old_value` NULL and `old_is_null = 1`: the record was not there. A field written as NULL on a record that did not exist changes nothing (NULL to NULL) and so has no row. An existing `{table}_history` is checked against this DDL before anything is created and a different schema is `contract_mismatch`, naming SPEC §4; the data table is never touched.

### `record_key`

A JSON array of `[name, value]` pairs ordered by field name, without spaces, a NULL value as JSON `null`:

```
[["accession","ENCFF001"],["replicate","1"]]
```

Injective by construction: different records never produce the same key.

Encoding decisions, so that the canonical form is the obvious one:

- No HTML escaping: `<`, `>` and `&` are written as they are, not as `\u003c`, `\u003e` and `\u0026`. The encoder is `encoding/json` with `SetEscapeHTML(false)`, and the trailing newline it adds is removed.
- Field names are sorted by their UTF-8 bytes, which is the same as Unicode code point order and never depends on locale: `B` sorts before `a`.
- Escaping inside a string, and nothing else: `"` → `\"`, `\` → `\\`, `\b` `\f` `\n` `\r` `\t` → their short forms, any other byte below `0x20` → `\u00XX` with lowercase hex, U+2028 and U+2029 → `\u2028` and `\u2029`. Every other character, DEL (`0x7f`), `/` and all non-ASCII text included, is written as raw UTF-8.
- A field name, or a value that is not NULL, with invalid UTF-8 is refused with `invalid_value` (exit 3). `encoding/json` would replace each invalid byte with U+FFFD, and two different values would collapse into one key, breaking injectivity. The `Raw` of a NULL value is ignored.
- An empty key (no field) is refused with `usage` (exit 1): a record without a key does not exist.

These rules are part of the canonical form, not a detail of the encoder: `record_key` is stored for good in `{table}_history` and `result_inputs`, and a different spelling of the same record would split its history in silence. `encoding/json` has changed its escaping before (Go 1.22 moved `\b` and `\f` from `\u0008` and `\u000c` to the short forms), so `TestRecordKeyEscapingIsPinned` fixes every rule above byte for byte; a Go release that changes one fails CI, and the answer is an encoder of our own, never a new spelling.

Injectivity is checked by a native fuzz test (`FuzzRecordKey`): every accepted key decodes back to exactly the original, with NULL, `"null"` and `""` kept apart.

### The key is a unique key of the table

The fields given as the key must be exactly the columns of one unique key of the data table, compared by name without regard to case: no column missing, none extra. A unique key is a `UNIQUE` constraint, a non-partial `UNIQUE` index whose columns are plain columns, or the `PRIMARY KEY` (an `INTEGER PRIMARY KEY` counts). A partial index does not guarantee uniqueness over the whole table and an index on an expression names no column, so neither is one.

- A key that is not exactly one of them → `key_mismatch`, exit 2, naming the closest unique key of the table (the one with the smallest symmetric difference; a tie goes to the first) and what is missing or extra, with its columns in alphabetical order: `the key of rnaseq_qc is (replicate, sample); replicate is missing`. With more than one unique key, the context lists them all: `unique keys: (id); (replicate, sample)`.
- A table with no `UNIQUE` constraint and no `PRIMARY KEY` → `no_unique_key`, exit 2, suggesting one.

Why: one row must have one `record_key`. A partial key (`sample` alone when the key is `sample, replicate`) still finds the row when only one matches, so the write succeeds, but its history goes under a second `record_key`; the record's history splits, and the next write with the full key reconciles a change that Scribo itself made. The rule holds for every command that takes `--key`: `upsert` and `log` in Slice 1, and every later one. `ambiguous_key` stays as a defence that this rule makes unreachable.

---

## 5. Flow of a write

Validation outside the transaction. State and write inside.

```
1.  validate flags                                   error → exit 1
2.  open Backend (§3)                                error → exit 5
3.  TableExists                                      false → exit 2, table_not_found
4.  Columns; validate that every field exists        missing → exit 2, column_not_found, suggestion (§10)
    and that the key is a unique key (§4)             otherwise → exit 2, key_mismatch or no_unique_key
5.  coerce values according to affinity              failure → exit 3, invalid_value
6.  resolve --sample, if any                         failure → exit 3, unknown_sample
7.  resolve --input into roots                       failure → exit 4, input_not_found
8.  load scribo.rules.toml, if any                   failure → exit 4, invalid_rules
9.  EnsureContractTables, EnsureHistoryTable         once per process, cached
10. BeginRun                                         returns run_id
11. BeginTx                                          BEGIN IMMEDIATE
12. SelectRow and ProjectedRow                       current and projected state, inside the transaction
13. reconcile divergence                             FieldChange with source=reconciled
14. evaluate rules (§8) over the resulting state     violation → exit 3, invariant_violated
15. Upsert                                           FieldChange of the fields that changed
16. AppendHistory, AppendInputs                      same run_id
17. Commit
18. EndRun                                           ended_at and exit_code
```

Error from 11 on → Rollback, message on stderr, appropriate exit code, `EndRun` with the code.

An error before 10 writes nothing. An error from 11 on leaves the data table, its history, `result_inputs` and `input_signals` as they were, and exactly one new `tool_runs` row with a non-zero `exit_code`: the failed run is recorded, its effects are not. The one exception is the `suspect` signal of a violated invariant (§8), stored on purpose in a transaction of its own.

`--batch` iterates the lines inside the same transaction. An error on any line → total ROLLBACK, message naming the line.

`--dry-run` executes up to 14 inside a transaction that ends in Rollback and prints the `FieldChange` and the rules evaluated. It writes nothing at all, `tool_runs` included, and reports the reconciliation it would record. The output goes to stdout, one line per change in the format of `log` (`source` is `scribo` or `reconciled`), so it can be read and piped like any other data.

**Interruption.** `SIGINT` and `SIGTERM` (Ctrl-C, or the scheduler ending a job at its time limit) cancel the command's context. An open transaction rolls back, a run already begun is closed with `EndRun`, and the command exits 5 with `interrupted` (context: `the transaction was rolled back; nothing was written`). The `EndRun` of every failure path runs on a context detached from the cancelled one (`context.WithoutCancel`) and bounded to 5 seconds, since a cancelled context could not write the exit code. The wait for a lock held by another process (`--busy-timeout`) is cut short by the signal as well. A second signal ends the process at once; the database is still safe, since SQLite rolls back an unfinished transaction on the next open, and the run stays with `ended_at` NULL, which is how an interrupted run is recognised.

---

## 6. Gap reconciliation

`ProjectedRow` reconstructs the state that the history asserts. Compared with `SelectRow`, inside the transaction:

- Equal field → nothing.
- Divergent field → a `source='reconciled'` row with `old_value` from the projected and `new_value` from the current, `changed_at` of the detection. Warning on stderr.
- Field with no history at all → a record prior to Scribo's adoption. Ignored.
- Field whose column no longer exists in the data table (dropped with `ALTER TABLE`) → outside reconciliation. Its history stays as it is; there is no current value to compare, and refusing the write would lock the record for good.

- Record deleted from outside (no row for the key) → a `reconciled` row per field with history, `new_value` NULL, unless the projected value is already NULL.

`ProjectedRow` takes, per field, the `new_value` of the last history row of the record in `(changed_at, id)` order, with `ROW_NUMBER() OVER (PARTITION BY field ORDER BY changed_at DESC, id DESC)`. Values are compared in the stored, rendered form (§7): `94.30` written from outside over `94.3` in a `REAL` column is no change.

Reconciliation runs inside the transaction of the write, after `BeginTx` and before `Upsert`, and its rows carry the same `run_id` and the same `changed_at` as the write's own rows. They are appended first and so get the lower ids: the chain `old → new` of every field stays continuous, a `reconciled` row followed by the `scribo` row that starts from its `new_value`.

The warning goes to stderr only after the commit, with code `history_reconciled`, and names the table, the fields and the `record_key`, never a value: `rnaseq_qc: 2 fields changed outside Scribo: Mapped_Reads, note; history reconciled`, or `rnaseq_qc: record deleted outside Scribo; history reconciled`.

Known limit: if the write fails after the reconciliation, the rollback also undoes the `reconciled` rows. The gap is still there and the next successful write to the record detects it again; until then, nothing records that it was seen.

A `log --at` that crosses `reconciled` warns that the state before that point is approximate. Precisely: the state at a moment is, per field, the `new_value` of the last row with `changed_at <= at`, and a field is approximate when its first row after `at` is `reconciled`, because the change made outside Scribo happened at some unknown moment between the last row Scribo knew and the detection. The warning is `history_approximate`, one per field, on stderr. History is never deleted nor rewritten.

---

## 7. Values and affinity

```
FIELD=           Value{Raw: "",     IsNull: false}
FIELD=null       Value{Raw: "null", IsNull: false}
FIELD=abc        Value{Raw: "abc",  IsNull: false}
FIELD=94.3       Value{Raw: "94.3", IsNull: false}
--null FIELD     Value{IsNull: true}
```

There is no magic value: a typed `null` is text. A real NULL only through `--null`, which can be repeated. In the `--batch` formats: TSV and CSV use an empty cell with an explicit `--empty-as-null`, and JSON uses native `null`.

**Coercion on write.** A numeric column and a value that is a decimal number → bound as a number. A value that is not → `invalid_value`, exit 3.

- A numeric column is one with `INTEGER` or `REAL` affinity, or a `NUMERIC` column declared as `NUMERIC` or `DECIMAL`. `DATE`, `DATETIME`, `TIMESTAMP`, `BOOLEAN` and every other type that is `NUMERIC` only by elimination in SQLite's affinity rules are text: a date in text must never become `invalid_value` for not being a number.
- The affinity of a declared type follows the order of SQLite's own rules (`sqlite.org/datatype3.html` §3.1): contains `INT` → `INTEGER`; `CHAR`, `CLOB` or `TEXT` → `TEXT`; `BLOB` or no type → `BLOB`; `REAL`, `FLOA` or `DOUB` → `REAL`; otherwise `NUMERIC`. `ANY`, the flexible type of a `STRICT` table, is `BLOB`.
- The accepted literal is `[+-]?(digits[.digits*]|.digits)([eE][+-]?digits)?`, checked by a regular expression before `strconv` sees the text. `strconv.ParseFloat` alone would accept `NaN`, `Inf`, `0x1A` and `1_000`; those, a leading or trailing space and the empty text are `invalid_value`. An `INTEGER` column takes only `[+-]?digits` that fit in 64 bits, so `94.3`, `1e3` and an overflow are refused. `REAL` and `NUMERIC` bind an integer text as an integer and any other as a float.
- What is compared and written to the history is the canonical text of the stored value, not what the user typed: `94.30` over `94.3` in a `REAL` column is no change, and `01` and `1` in an `INTEGER` key are the same record. Integers render in base 10 and floats as the shortest text that reads back as the same number. Negative zero renders as `0`: SQLite compares it equal to zero, and `-0` read back as an integer would give one record two keys. The canonical form is stable — canonicalizing it again gives the same text — and `FuzzCanonicalIsStable` checks that for every column kind.

**Hints for a refused number.** `invalid_value` names the most likely fix in its context line, and never changes what is accepted. The first rule that matches wins:

- a trailing `%` (`94.3%`) → `drop the % sign: 94.3`;
- commas or spaces grouping thousands, at least two groups (`12,000,000`, `12 000 000`) → `drop the thousands separators: 12000000`;
- one comma and exactly three digits after it (`1,234`), which can be either a decimal comma or a thousands separator → `use a dot as the decimal separator (1.234) or drop the thousands separator (1234)`;
- a comma between digits otherwise (`94,3`) → `use a dot as the decimal separator: 94.3`.

An `INTEGER` column never gets a suggestion it would refuse itself: a `%` or grouping hint whose value has a decimal point drops the value (`drop the % sign`), and the two comma hints are not given at all.

**Comparison.** A filter uses the definition of a numeric column of coercion above. On a numeric column the value must be a number (`invalid_value`, exit 3, with the hints for a refused number) and is compared as a number, so `100 > 90`. On any other column `=` and `!=` compare the exact text, and `>`, `<`, `>=`, `<=` → `untyped_comparison`, exit 2: `run_date is not a numeric column; > would compare text`, suggesting `--force-text`. `--force-text` compares every filter as text, `CAST("column" AS TEXT)`, numeric columns included.

---

## 8. Rules: input × result invariants

File `scribo.rules.toml`, looked up next to the database or passed by `--rules`. Canonical hash stored in `tool_runs.config_hash`.

```toml
[[rule]]
table = "rnaseq_qc"
check = "Mapped_Reads <= input.reads"

[[rule]]
table = "rnaseq_qc"
check = "Assigned_Reads <= Mapped_Reads"

[[rule]]
table = "rnaseq_qc"
check = "Alignment_Rate in [0, 100]"
```

Vocabulary per `CONTRACT.md` §5. Evaluation over the record's state **after** the intended write, inside the transaction.

- Rule satisfied → nothing.
- Rule violated → Rollback, `invariant_violated`, exit 3, message with the two numbers and the inputs involved.
- An `input.*` rule without an input under Colligo's custody → not evaluated; `rule_unavailable` warning on stderr, never treated as passed. `--require-rules` turns the warning into an error.

Every violation also generates a `suspect` signal in `input_signals`, with `source = invariant:<rule>`, stored in a transaction of its own after the Rollback — the write of the result does not happen, but the fact that the input produced an impossible result is recorded.

---

## 9. Provenance and staleness

### Declaration

`--input PATH`, repeatable. Each path becomes an `input_kind='file'` edge in `result_inputs`:

1. `FileRoot(path)`: if the file is in Colligo's `files` and the `stat` matches the verification baseline, it uses the `blake3_root` from there.
2. Otherwise, it computes the root with the fact extractor in a single read, and warns that the input is not under custody.

`--input-result TABLE:KEY` creates an `input_kind='result'` edge, for chains between results.

`--input-field FIELD` restricts the following edges to one field; without it, the whole record depends on the input.

### Graph

Nodes and edges per `CONTRACT.md` §6. `provenance/graph.go` builds the graph in memory from `Edges` and `InvalidatedNodes`, and computes staleness by reverse search from the invalidated nodes: O(V + E) per query, with no persisted state.

### `stale`

```
scribo stale --db DB [--table T] [--format tsv|json] [--samplesheet PATH --template nfcore-rnaseq|nfcore-chipseq|nfcore-atacseq]
```

Lists stale records with the path in the graph up to the invalidated node and the reason. `--samplesheet` writes the nf-core samplesheet with only the affected samples, from the input files in Colligo's `files`. Without Colligo present → `colligo_required`, exit 2.

Exit code 0 if nothing is stale, 3 if there are stale items.

---

## 10. Subcommands

### Global flags

```
--db             path of the database                 mandatory
--table          name of the table                    mandatory where indicated
--journal-mode   wal|delete                           optional, default detected
--busy-timeout   ms                                   optional, default 5000
--step           label of the pipeline step           optional, stored in tool_runs
--rules          path of scribo.rules.toml            optional
--quiet          suppresses non-essential stdout
--debug          logs SQL and timings on stderr       forbidden on a restricted database
--version        prints the version, the Git commit and the Go version, and exits
```

`scribo help exit-codes` prints the table of §12 with one line of meaning per code, for whoever writes a pipeline around Scribo. It is generated from one registry, `exitcode.Codes`, and a test keeps that registry and the `Typed codes:` list of §12 identical. `--version` prints `scribo <version> (commit <sha>, <go version>)` on stdout.

A missing column or table gets a suggestion, compared without regard to case: `did you mean "rnaseq_qc"?` for the closest name within edit distance 2 (a tie goes to the alphabetically first); failing that, for the one name the given text is a prefix of (`rnaseq` → `rnaseq_qc`, but not when it is a prefix of two or more); otherwise the list of names, at most 20. A missing column is `column_not_found` with the columns of the table. A missing table is `table_not_found` with the list of data tables (reserved ones left out, at most 20), or `the database has no data table yet` when there is none. `Backend.Tables` lists every table; the command layer leaves out the reserved ones. The context of an error never names a command that does not exist yet.

### upsert

```
scribo upsert --db DB --table T [--key K=V]... [--sample S] [FIELD=VALUE]... [--null FIELD]... [--input PATH]... [--input-result T:K]... [--input-field F]
scribo upsert --db DB --table T --batch PATH|- [--format csv|tsv|json] [--empty-as-null]
```

Flags of Slice 1:

```
--db           path of the database file, which must already exist   required
--table        name of the data table, which must already exist      required
--key          FIELD=VALUE naming the record; repeat for a composite key   required
--null         FIELD to set to a real NULL; repeatable
--insert-only  key_already_exists if the record exists
--update-only  key_not_found if the record does not exist
--busy-timeout milliseconds to wait for a lock; default 5000, must be positive
```

`FIELD=VALUE` arguments are split at the first `=` and `--key` and `--null` are repeatable string flags that are never split on commas, so a value or a key such as `a,b` stays one value. A usage error (exit 1) is raised before the database is opened for: a missing `--db`, `--table` or `--key`; nothing to write; an argument without `=` or with an empty name; a field given twice, including `FIELD=1` with `--null FIELD`; a key field also written; a repeated key; `--insert-only` with `--update-only`; a `--busy-timeout` that is not positive. Success prints nothing.

The record key is the `record_key` of the canonical form of the key values (§7), so `replicate=01` and `replicate=1` name the same record. A table is refused with `reserved_table` (exit 2) when it is `tool_runs`, `result_inputs`, `input_signals`, `samples`, `files`, `facts` or `study_meta`, ends in `_history` or starts with `sqlite_`, compared without regard to case. Slice 1 has no `--sample`, `--input`, `--batch`, `--dry-run`, `--step`, `--quiet` or `--debug`; they arrive in later slices.

`--sample S` resolves through Colligo's `samples` table and adds the key `sample=<id>`. `--insert-only`, `--update-only`, `--dry-run`, `--progress`.

In `--batch`, the special columns `_sample` and `_input` (a `;`-separated list) play the role of the flags per line.

`--key` is checked in the handler, not by the parser, so that a record key written as a positional argument gets an answer: with positional arguments and no `--key`, the usage error is `--key is required` with the context `the record key goes in --key, for example --key sample=S1`, built from the first positional argument. Without positional arguments there is no such hint.

### log

```
scribo log --db DB --table T --key K=V... [--field F] [--at TIMESTAMP] [--format tsv|json]
```

Without `--at`, one line per history row in `(changed_at, id)` order. TSV (the default) has the header `changed_at  run_id  source  field  old_value  old_is_null  new_value  new_is_null`; a NULL is an empty cell with `is_null` 1; every value escapes `\` as `\\`, tab as `\t`, LF as `\n` and CR as `\r`. JSON is one object per line, `{"changed_at":…,"run_id":N,"source":…,"field":…,"old":"x"|null,"new":"y"|null}`, without HTML escaping.

With `--at`, the state at that moment: TSV `field  value  is_null`, one line per field in alphabetical order, or a single JSON object with the fields as sorted keys. `TIMESTAMP` is RFC 3339 with a time and a zone, truncated to the millisecond in UTC; a date alone is ambiguous and is a usage error. `--field` limits both forms to one field and must name a column.

A key with no history: `key_not_found` (exit 3) when no record has it, an empty result (header only, exit 0) when the record exists but was never written by Scribo. `log` writes nothing: no `tool_runs` row, no contract or history table created.

Planned for Slice 2:

- `--format pretty`: the same rows as TSV in aligned columns, with `NULL` shown as `∅` and without the `is_null` columns, for a person at a terminal. It is chosen explicitly, never by detecting a terminal: the output of a command does not change with where it is written.
- `--with-runs`: adds `tool_version`, `step` and `exit_code` of the run that wrote each row, from `tool_runs`.
- `--at run:N`: the state right after the last history row that run `N` wrote in this table, every row up to it in `(changed_at, id)` order. A run that wrote nothing in this table → `run_not_found`, exit 3. Run ids are not used as an order by themselves: two runs may start in one order and commit in the other.

### query, export

```
scribo query  --db DB --table T [--where COND]... [--is-null F]... [--not-null F]... [--fields F1,F2] [--format tsv|json|csv|pretty] [--limit N] [--force-text] [--with-staleness]
scribo export --db DB --table T [--where COND]... [--is-null F]... [--not-null F]... [--out PATH] [--overwrite] [--format tsv|json|csv] [--force-text] [--with-staleness]
```

`--where COND` is `FIELD<op>VALUE` with one of `=`, `!=`, `>`, `<`, `>=`, `<=`; repeatable. `null` in a value is the text `null`, never NULL: a real NULL is filtered with `--is-null FIELD` and `--not-null FIELD`, both repeatable. Every filter is joined by AND.

`--fields` takes comma-separated names, repeatable, printed in the order given; a name given twice is a usage error. Without it, every column in table order. `--limit N` needs `N > 0`. Records come in the order of `SelectRows` (§2): the first unique key, then `rowid`.

Formats:

- `tsv` (the default): a header with the names, a NULL as `\N`, and `\`, tab, LF and CR escaped as `\\`, `\t`, `\n`, `\r`, the escaping of `log`; a literal text `\N` is written `\\N`.
- `csv`: RFC 4180, written by hand because `encoding/csv` cannot quote an empty text. A field holding `,`, `"`, CR or LF is quoted with `"` doubled; the empty text is `""`; a NULL is an empty unquoted field. Lines end in LF.
- `json`: one object per line, keys in column order, values as strings or `null`, no HTML escaping.
- `pretty` (`query` only): aligned columns with a line of dashes under the header, a NULL as `∅`, control characters escaped as in `tsv`. Column width counts characters, not bytes.

`export` writes to stdout, or to `--out PATH`. An existing `PATH` without `--overwrite` → `file_exists`, exit 4, before the database is opened. The file is written to a temporary file in the same directory, synced, closed and renamed over `PATH`, so a failure leaves `PATH` as it was and removes the temporary file; an overwritten file keeps its permissions and a new one gets `0644`. `export` has no `--limit` nor `pretty`.

`query` and `export` never write: no `tool_runs` row, no table created. A reserved or missing table gets the errors of `upsert`.

`--with-staleness` adds the `_stale` column computed from the graph (Slice 3).

### schema

```
scribo schema --from file.tsv --table T [--key K]...
scribo schema --from-multiqc multiqc_data.json --table T
```

Prints the `CREATE TABLE`. It does not execute.

`--from-multiqc` reads the General Statistics table by default; `--module NAME` (for example `fastp`, `cutadapt`, `star`) reads that module's full per-sample data instead, one table per tool. The generated table always has `sample TEXT NOT NULL` and `UNIQUE(sample)`, so it satisfies the key rule (§4), and, with `--module`, a `tool_version TEXT` column.

Column names are the MultiQC metric keys verbatim, as quoted identifiers (`"after_filtering.q30_rate"`, `"percent_duplication"`): no renaming, so the mapping is injective and a re-ingest finds the same columns. Types come from the values in the file: integers → `INTEGER`, other numbers → `REAL`, anything else → `TEXT`.

### ingest

```
scribo ingest multiqc  --db DB --table T multiqc_data.json [--map-sample colligo|as-is]
scribo ingest somalier --db DB somalier.pairs.tsv [--relatedness-min 0.8] [--relatedness-max 0.1]
scribo ingest ngscheckmate --db DB output_all.txt
```

`multiqc` reads the general per-sample metrics and upserts them into the table, one transaction for the whole file. `--map-sample colligo` resolves MultiQC sample names against `samples`; a name with no match → `unknown_sample`, nothing stored.

`--module NAME` imports that module's per-sample data into a table made by `schema --from-multiqc --module NAME`. The sample name is MultiQC's, verbatim, with `--map-sample as-is` (the default): no suffix such as `_R1` or `_trimmed` is stripped by guessing, since a wrong guess merges two samples. When the file records the tool's version, it goes to the `tool_version` column of every record; when it does not, the column stays NULL and a `tool_version_unknown` warning says so. Slice 4's `diff` counts a changed `tool_version` column as the cause `tool`.

There is no parser per tool (fastp, cutadapt, FastQC): MultiQC already reads more than a hundred of them, and its JSON is the one format Scribo follows. A tool MultiQC does not read comes in as TSV, through `schema --from` and `upsert --batch`.

`somalier` and `ngscheckmate` compare the observed relatedness with the declared: the same sample or technical replicate declared and relatedness below `--relatedness-min` → `identity_mismatch`; samples declared distinct above the limit → `identity_mismatch` in both; declared and observed coherent → `identity_confirmed`. Signals stored in `input_signals` for the roots of the samples' inputs.

### diff

```
scribo diff --db DB --table T --run A --run B [--tolerance F=0.01]... [--format tsv|json]
```

For each record and field with a value in both runs: absolute and relative difference; a change above the tolerance is reported with the cause:

| Cause   | Condition                                                           |
| ------- | ------------------------------------------------------------------- |
| `input` | the set of the record's input roots differs between A and B         |
| `tool`  | `tool_version` or `step` differs between the `tool_runs` of A and B |
| `none`  | identical inputs and tool                                           |

Cause `none` is highlighted: it is non-determinism. Exit code 0 if nothing above the tolerance, 3 if there is a change with cause `none`, 0 with a report in the other cases. `--fail-on input,tool,none` chooses what fails.

### outliers

```
scribo outliers --db DB --table T --field F [--step S] [--group-by COL] [--z 3.5] [--signal]
```

Per field, within each group: median `m`, MAD `d`, and `z = 0.6745 · (x − m) / d`. `|z|` above the limit is an outlier. A group with fewer than 8 samples → not evaluated, warning. `d = 0` → not evaluated, warning.

`--signal` stores `suspect` in `input_signals` for the inputs of each outlier, with `source = outlier:<field>` and the `z` in `detail`. Without `--signal`, it only prints.

### signals

```
scribo export-signals --db DB [--since TIMESTAMP] [--format json]
```

Exports signals, invariant violations and `reconciled` rows as cases for Colligo's failure corpus.

---

## 11. Restricted database

With a restricted `study_meta`, or `--restricted`:

- `--debug` → error.
- Permission `0600` expected; looser → error, `--allow-insecure-permissions` recorded.
- Path in a synced folder → error, `--allow-synced-path`.
- `detail` of signals and error messages use Colligo's opaque identifiers, never the real name.
- `export-signals` refuses without `--allow-restricted-export`.

---

## 12. Exit codes

```
0   success
1   usage error
2   database error       nonexistent table or column, untyped comparison, contract divergence, Colligo required
3   data error           invalid value, unknown sample, violated invariant, stale results, diff without cause
4   file error           batch, rules or input not found; invalid format
5   system error         disk, permission, I/O, PRAGMA without effect; an error Scribo did not classify
```

Messages on stderr:

```
ERROR [<typed_code>]: <main message>
→ <context, line of the error, suggestion>
```

Typed codes: `table_not_found`, `column_not_found`, `key_not_found`, `key_already_exists`, `invalid_value`, `untyped_comparison`, `unknown_sample`, `input_not_found`, `invalid_rules`, `invariant_violated`, `rule_unavailable`, `colligo_required`, `contract_mismatch`, `stale_results`, `nondeterministic_change`, `file_not_found`, `invalid_format`, `batch_failed`, `db_busy`, `permission_denied`, `synced_path`, `pragma_ineffective`, `usage`, `internal`, `io_error`, `network_filesystem`, `constraint_failed`, `reserved_table`, `ambiguous_key`, `history_reconciled`, `history_approximate`, `key_mismatch`, `no_unique_key`, `interrupted`, `run_not_found`, `tool_version_unknown`, `file_exists`.

New in Slice 2: `key_mismatch` and `no_unique_key` (the key is not exactly one unique key of the table, §4; exit 2), `interrupted` (a signal ended the command, §5; exit 5), `run_not_found` (`log --at run:N` for a run that wrote nothing in the table; exit 3) the warning `tool_version_unknown` (§10, ingest) and `file_exists` (`export --out` to a file that exists, without `--overwrite`; exit 4).

New in Slice 1: `constraint_failed` (the database refused a value: NOT NULL, UNIQUE, CHECK or a trigger; exit 3), `reserved_table` (the table is one Scribo or Colligo owns, or a `_history` or `sqlite_` table; exit 2), `ambiguous_key` (a key matches more than one record because its columns are not UNIQUE; exit 3), `io_error` (a database error with no more specific code, exit 5) and the warnings `network_filesystem`, `history_reconciled` and `history_approximate` (§6), which never change the exit status.

Warnings use the same shape with a different prefix, and do not change the exit status:

```
WARNING [<typed_code>]: <main message>
→ <context>
```

`usage` comes from the CLI layer only: the parser (an unknown flag or command, a wrong number of arguments) and the flag checks of each subcommand handler before anything is opened. Below the CLI layer, every error is typed; one that escapes untyped becomes `internal`, exit 5, and is a bug to report, never a usage error.

---

## 13. Concurrency

Writes serialise. `BEGIN IMMEDIATE` takes the write lock at the entry of the transaction; `busy_timeout` controls the wait. Under high contention, spool: each job writes a local file, one `upsert --batch` consolidates. On a network filesystem, DELETE in place of WAL and spool becomes a practical requirement.

---

## 14. Tests

Integration of the complete binary in a subprocess, SQLite in tmpdir.

Mandatory:

- unit and batch atomicity: an error in the middle persists nothing; data table, history, `result_inputs` and `input_signals` identical before and after, and one new `tool_runs` row with a non-zero `exit_code`
- concurrency: N processes writing the same record; every `old_value` of the history equal to the value immediately before in the order `(changed_at, id)`; `-race`
- history and `record_key`: the key is independent of the order of the flags; values with `,`, `=`, `"` and `[` do not collide
- NULL: `FIELD=null` stores text; `--null FIELD` stores NULL; the history distinguishes the two
- reconciliation: a write from outside with `sqlite3` generates `reconciled`; `--at` warns
- affinity and untyped comparison
- filesystem detection by simulated type
- contract: `tool_runs` with a divergent schema → `contract_mismatch` without writing
- rules: `Mapped_Reads > input.reads` refused with exit 3 and a `suspect` signal stored; an `input.*` rule without Colligo → `rule_unavailable`
- staleness: invalidating a file in Colligo's fixture makes stale exactly the records that reach it, including by `result → result` chain
- `stale --samplesheet` generates a valid samplesheet with only the affected samples
- `diff`: fixture with a change by input, by tool and with no cause; each one attributed correctly
- `outliers`: a planted sample with a high `z` detected; a small group and zero MAD not evaluated
- `ingest multiqc` of a real `multiqc_data.json` from nf-core/rnaseq; `ingest somalier` with a swapped pair generates `identity_mismatch`
- restricted: `--debug` refused; loose permission refused; `detail` without a real name
- leakage: sentinel in path and value absent from `argv_shape`, from `detail` and from all output
