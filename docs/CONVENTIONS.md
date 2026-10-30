# Scribo — Conventions

House style. Inviolable during implementation. Short on purpose.

---

## Code

**No comments. None.** Not in Go, not in SQL, not in examples. Names communicate intent. If a piece of code needs a comment to be understood, it needs to be rewritten.

**Errors are values and are handled.** No `_` swallowing an error outside genuinely irrelevant cases. Domain errors carry a typed code that becomes the tag of the message on stderr.

**`context.Context` is the first parameter** of every operation that does I/O, including the calls of the `Backend`.

**SQL never lives in the subcommand handlers.** Persistence always goes through `db.Backend`. An inviolable architectural rule — it preserves the door to Postgres and keeps the handlers testable.

**Validation outside, state inside.** Table, columns, value coercion, sample and input resolution, and loading of rules happen outside the transaction. Reading the current and the projected state, evaluating rules and writing happen inside a `BEGIN IMMEDIATE` transaction. `SelectRow` and `ProjectedRow` only exist on `Tx`, so that the error is impossible to write.

**`Tx` does not embed `Backend`.** A nested transaction does not exist in SQLite. The narrow interface makes the error impossible to write.

**Clean stdout on write success.** An `upsert` that succeeds prints nothing, unless `--debug`. Errors on stderr. `query`, `export` and `log` produce data on stdout.

**Every write is an explicit transaction.** Even a single one. Total atomicity, without exception.

**No magic value.** A typed `null` is text. A real NULL only through `--null` or JSON's native `null`.

**Staleness is never a column.** It is computed from the graph on every query. No stored `stale` flag that someone forgets to update.

**Stdlib first. Conscious dependencies of v1: `modernc.org/sqlite` and `github.com/spf13/cobra`. Any addition requires a justification.

---

## Database

**Opening per `CONTRACT.md` §2.** PRAGMAs in the DSN, applied to every connection, with `_txlock=immediate`; read back; journal mode resolved by the filesystem; contract tables checked.

**Journal mode is detected, not assumed.** A network filesystem never opens in WAL. See SPEC §3.

**Scribo creates only the contract tables it owns and `{table}_history`.** The data table is never created nor structurally altered. `schema` generates DDL to stdout; it does not execute.

**Never write to a Colligo table.** `samples`, `files` and `facts` are read-only. What Scribo has to say about an input goes to `input_signals`.

**`CONTRACT.md` is identical in both repositories.** A change to it is a change to both, in the same cycle, with `contract_version` incremented.

**`tool_runs` follows the DDL of `CONTRACT.md`**, distinguished by `tool`. Create if it does not exist with that exact DDL; if it exists with a different schema, `contract_mismatch`.

**Never store an argument value in `argv_shape`.** Only the shape of the command. It is what prevents leakage by construction.

**History is never deleted nor rewritten.** A detected gap becomes a `reconciled` row, not a retroactive correction.

**Timestamps in UTC, RFC 3339 with millisecond precision.**

**`record_key` is a canonical JSON array** of pairs ordered by field name. Never string concatenation. Chronological ordering is always `(changed_at, id)`.

**`STRICT` tables.**

---

## CLI

**`--help` is the primary documentation.** Each subcommand has a `--help` sufficient for use without outside consultation, with concrete examples.

**Exit codes are contractual.** The set of SPEC §10 is stable. Adding one means thinking twice and updating the doc.

**Fail closed where ambiguity produces a wrong number.** A numeric comparison against a column without a declared type is an error, not a warning.

**Flags in kebab-case, descriptive long form.** Single-letter shortcuts only for the really frequent ones.

---

## Tests

**Integration of the complete binary.** Run the binary in a subprocess and check database, stdout, stderr and exit code. Unit tests only for complex isolated logic: value parsing, affinity coercion, construction of `record_key`.

**SQLite in the test's tmpdir**, never outside it. No tests that touch the network.

**Leak sentinel.** A unique, improbable value as a path and as a field value, with grep over all output produced, including `argv_shape` and the database.

**A test written together with the code.** `-race -count=2` mandatory.

---

## Documentation

**GoDoc in `internal/db/`.** Even though it is `internal`, maintainers need to understand the `Backend` interface that sustains portability. It is the only place where explanatory text is allowed in the code.

**A decision changes code and doc in the same commit.** SPEC and implementation do not drift apart.

**Where each thing lives:** the why in `DESIGN.md`, the what in `SPEC.md`, what is shared with Colligo in `CONTRACT.md`, what already exists outside in `RELATED.md`, what comes next in `ROADMAP.md`, what was postponed in `BACKLOG.md`.

---

## Git

**`CLAUDE.md` is private** (`.gitignore`). It does not go into the repository.

**Mandatory `.gitignore`:** `*.db`, `*.db-wal`, `*.db-shm`, `*.local.toml`.

**Small commits, by slice or sub-slice.** The message describes what changed and why in one line.
