# Colligo ↔ Scribo Contract

Shared normative document. An identical copy lives in `Colligo/docs/CONTRACT.md` and in `Scribo/docs/CONTRACT.md`. **A divergence between the two copies is a bug**, and each repository's CI compares its copy against the other's.

This document defines everything the two tools write or read in common in the same database. Outside it, each one owns its own tables and neither writes to the other's tables.

Contract version: **1**.

---

## 1. Principle

Integration by contract, not by runtime coupling. A database touched by both tools has a single execution trail, a single sample identity and a single dependency graph — and neither needs to be installed for the other to work.

Rules:

- Every table has an owner. Only the owner writes. The other tool reads.
- The tables in this document are created by whoever arrives first, with the exact DDL given here.
- On opening, each tool checks the effective schema of the contract tables (`PRAGMA table_info`) against the expected one. Divergence → `contract_mismatch` error, writing nothing.
- A change to this document increments the version and requires a migration in both tools in the same release cycle.

---

## 2. Opening the database

Identical in both tools, implemented in the common core (`store/open`).

1. `statfs` on the database directory. Network type (`nfs`, `nfs4`, `cifs`, `smb2`, `lustre`, `gpfs`, `fuse.*`, `afs`, `9p`) → journal mode `DELETE`, with a warning on stderr. Local → `WAL`. An explicit override is recorded in `tool_runs`.
2. PRAGMAs in the DSN, applied to every connection: `foreign_keys(1)`, `journal_mode(<resolved>)`, `synchronous(NORMAL)`, `busy_timeout(<value>)`, and `_txlock=immediate`.
3. Read back on each new connection. Divergence → `pragma_ineffective`.
4. Check of the contract tables (§1).

Neither tool opens a shared database in `WAL` on a network filesystem. WAL requires shared memory between processes on the same host; on NFS it is silent corruption.

---

## 3. Contract tables

### `tool_runs` — owner: both, one row per execution of each tool

```sql
CREATE TABLE IF NOT EXISTS tool_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    contract_version INTEGER NOT NULL,
    tool             TEXT NOT NULL CHECK (tool IN ('colligo','scribo')),
    tool_version     TEXT NOT NULL,
    git_sha          TEXT NOT NULL,
    go_version       TEXT NOT NULL,
    host             TEXT,
    step             TEXT,
    config_hash      TEXT,
    argv_shape       TEXT NOT NULL,
    started_at       TEXT NOT NULL,
    ended_at         TEXT,
    exit_code        INTEGER
) STRICT;
```

Each tool writes only its own rows. `argv_shape` stores the shape of the command, never values. Timestamps in UTC, RFC 3339 with millisecond precision.

### `samples` — owner: Colligo

Colligo's `samples` table is the source of sample identity. Scribo reads: `id`, `accession`, `experiment_id`, `role`, `replicate`. Resolving `--sample X` means finding the row whose `accession` or `id` is `X`.

### `files` and `facts` — owner: Colligo

Scribo reads `files.blake3_root`, `files.path` and the facts of `facts` indexed by root. This is how a result is linked to a verified input and how the input × result invariants (§5) obtain, for example, the input's read count.

### `result_inputs` — owner: Scribo

Provenance edges: each result value points to the inputs it depends on.

```sql
CREATE TABLE IF NOT EXISTS result_inputs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id       INTEGER NOT NULL REFERENCES tool_runs(id),
    result_table TEXT NOT NULL,
    record_key   TEXT NOT NULL,
    field        TEXT,
    input_root   TEXT NOT NULL,
    input_kind   TEXT NOT NULL CHECK (input_kind IN ('file','result')),
    recorded_at  TEXT NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS idx_result_inputs_root   ON result_inputs(input_root);
CREATE INDEX IF NOT EXISTS idx_result_inputs_record ON result_inputs(result_table, record_key);
```

`input_kind = 'file'` points to a file's BLAKE3 root. `input_kind = 'result'` points to another result, encoded as the canonical root of the pair `(result_table, record_key)`, which allows dependency chains between results. A null `field` means that the whole record depends on the input.

### `input_signals` — owner: Scribo; read by Colligo

Signals that Scribo observes after the pipeline and that say something about the inputs.

```sql
CREATE TABLE IF NOT EXISTS input_signals (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES tool_runs(id),
    input_root  TEXT NOT NULL,
    signal      TEXT NOT NULL CHECK (signal IN ('suspect','identity_confirmed','identity_mismatch')),
    source      TEXT NOT NULL,
    detail      TEXT NOT NULL,
    created_at  TEXT NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS idx_input_signals_root ON input_signals(input_root);
```

`source` says where the signal came from: `outlier:<field>`, `somalier`, `ngscheckmate`, `invariant:<rule>`. `detail` is canonical JSON with the numbers that justify the signal. Colligo consumes these signals in custody (§4); Scribo never changes Colligo's state directly.

---

## 4. What each signal means for Colligo

| Signal               | Effect on Colligo's custody                                                                          |
| -------------------- | ---------------------------------------------------------------------------------------------------- |
| `suspect`            | Derived state `suspect` added to the custody state; `advisory`, listed in `status` with the `source` |
| `identity_confirmed` | `identity` assertion passed; appears in the certificate                                              |
| `identity_mismatch`  | `identity` assertion failed; `blocking`                                                              |

The original custody state is never overwritten. A signal is one more assertion, with a declared source, in the same logic of declared against observed.

---

## 5. Input × result invariants

Scribo evaluates rules that link a result value to the facts of the inputs. Minimum vocabulary of v1:

| Rule               | Meaning                                                        |
| ------------------ | -------------------------------------------------------------- |
| `F <= input.reads` | field F does not exceed the total reads of the declared inputs |
| `F <= input.bases` | the same for bases                                             |
| `F <= G`           | field F does not exceed field G of the same record             |
| `F in [a, b]`      | field F in the closed interval                                 |

`input.reads` is the sum of the `records` field of the `records` fact of the of the record's `file` inputs, read from Colligo's `facts` table. Without an input under custody, `input.*` rules are not evaluated and this is reported, never assumed to have passed.

---

## 6. Dependency graph and invalidation

Nodes: file roots, result records and accessions. Edges: `result_inputs` (result → input) and Colligo's file → sample → accession relation.

A result is **stale** if any node reachable from it is invalidated: a `stale`, `invalid` or `superseded` file; an accession suppressed at the source; an `identity_mismatch` signal; or a result it depends on that is already stale. Staleness is reachability in the graph, computed on demand, never stored as a column.

---

## 7. End-to-end completeness

`colligo status` shows the funnel: declared → downloaded → verified → processed. "Processed" means that every sample of the study has, in the results table declared in `[results]` of Colligo's TOML, the expected fields filled in and not stale. Without a declared results table, the funnel ends at "verified".

---

## 8. Real use feeding both

- **Scrub priority.** Colligo orders the scrub by number of dependent results (count in `result_inputs`), then by `last_verified_at`.
- **Garbage collection.** Colligo's `gc` (postponed, see Colligo's `BACKLOG.md`) uses `result_inputs` to tell an unused file, a regenerable file and an irreplaceable file apart.
- **Failure corpus.** `reconciled` rows, invariant violations and `suspect` signals are exportable (`scribo export-signals`) as cases for Colligo's `FAILURES.md` (planned; the file does not exist yet).
