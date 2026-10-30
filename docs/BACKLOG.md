# Scribo — Backlog

Features that were consciously postponed. **Do not implement anything from here without an explicit request.**

These left the backlog: the library shared with Colligo became the common-core decision of Slice 0 (`DESIGN.md` §5) and `CONTRACT.md`; `--file` for a single record was covered by `ingest multiqc` in the most common case.

Rule: every item carries a sentence on "what would unlock it". Without it, the item is a whim; with it, it is a postponed decision with a trigger.

---

## Commands cut from v1

### `status`

Overview: total records, count per `Status` value, NULLs per column.

Status: it is a `query` with aggregation. It does not justify its own command, and counting NULLs per column requires a full scan on every call.

Unlocks when: repeating the same `query` becomes real friction.

### `delete`

Remove a record while preserving history.

Status: in a pipeline a sample is rarely deleted — it is invalidated with a status field, and the history remains intact. Deleting through `sqlite3` is possible and Slice 1 detects it as a reconciled gap.

Unlocks when: a real case of removal appears that needs a correct history of the act of removing.

### `--file` for a single record

Read values from a metrics file into a single record.

Status: parsing in bash and passing through `FIELD=VALUE` solves it. `--batch` covers the volume case.

Unlocks when: the conversion in bash becomes fragile enough to justify a parser in the tool.

---

## `--clear` on upsert

Clear fields not mentioned, the opposite of the default of preserving.

Unlocks when: a real case of "I want to reset this record keeping only these fields" appears. The workaround is to mention all the fields.

---

## `--where` with OR, parentheses, LIKE, IN

Complex filters. Today only AND of basic operators.

Unlocks when: the real case requires it. Whoever needs it today uses `sqlite3` — we are not on the path to becoming a substitute for SQL.

---

## `prune --before`

Delete history prior to a date, with confirmation.

Unlocks when: real use shows `_history` becoming a space problem, in the range of millions of rows. Common only in continuous production.

---

## `copy` — copy records between databases

Move or clone records between different `.db` files.

Unlocks when: a real case of merging or splitting a database appears. The workaround is `ATTACH DATABASE` in `sqlite3`.

---

## `validate` — check integrity of paths on disk

Verify that the stored paths still exist on the filesystem.

Status: it overlaps with what Colligo already does better, with hash and chunk grid. A path registered by Scribo that points to a file under Colligo's custody is already verifiable there.

Unlocks when: a case exists of a path stored by Scribo outside any Colligo custody, and path rot becomes a measured problem.

---

## Resource predictor from history

"Will this job exceed the memory I asked for?" From the history of runs in `tool_runs`.

Status: Nextflow already does retry with memory escalation, which covers the practical case without predicting anything. The marginal value is knowing before instead of after a failure.

Unlocks when: you are not using an orchestrator with retry, and the cost of OOM failures becomes measurable.

---

## PostgreSQL store

A second implementation of `db.Backend`.

Status: the abstraction has existed since Slice 1 exactly for this. Differences to cover: dialect (`AUTOINCREMENT` versus `SERIAL`, `?` versus `$1`, `ON CONFLICT`, quoting), type model (SQLite's tolerant affinity versus Postgres's strict typing), concurrency model (WAL plus busy_timeout versus MVCC plus statement_timeout), DSN, and integration tests with a container.

Unlocks when: a real case appears where SQLite does not suffice — multiple processes on different hosts, consortium scale, an institutional requirement. Not on speculative demand.

Note: on a network filesystem, Postgres stops being a luxury and becomes the right answer. If the spool pattern does not solve your HPC, this item rises in priority.

---

## Structured output on success

JSON on stdout with what was written, for pipelines that consume the result of the write programmatically.

Unlocks when: a real pipeline case that needs it. Today silent success is a feature.

---

## Windows support

Nothing prevents it (`filepath` instead of strings), but it will not be tested in v1. Mind the permission regime of the restricted database, which is POSIX.

Current decision, the same as Colligo's: development and testing on Linux or WSL. The native Windows binary compiles; a restricted database on native Windows is refused.

Unlocks when: there is a real user on Windows and a stable cross-platform CI.

---

## Recording results on HPC without a server

Use outside bioinformatics: parameter sweeps and simulations where MLflow or W&B are too heavy, need a server or the internet. Scribo already does the essentials — upsert by key, history, `diff` between runs —; documentation and examples would be missing.

Unlocks when: a user outside bioinformatics appears. It changes no code, only the README.

---

## Rules beyond the minimum vocabulary

Arithmetic expressions in `scribo.rules.toml` (`F <= 0.5 * input.reads`), rules between records (technical replicates within a tolerance), conditional rules by `role`.

Unlocks when: the vocabulary of v1 stops covering a repeated real case. A rule language grows too easily to grow speculatively.

---

## Multivariate outliers

Today `outliers` looks at one field at a time. A robust Mahalanobis distance (MCD) over several fields catches the sample that is normal in each metric in isolation and strange in the set.

Unlocks when: the univariate one lets through a real case that the multivariate one would catch. It requires enough samples per group to estimate covariance, which a small study rarely has.

---

## Persistent configuration

A config file for defaults of `--db`, `--busy-timeout`, `--step`.

Unlocks when: repeating the same flags becomes friction. Today shell variables solve it.
