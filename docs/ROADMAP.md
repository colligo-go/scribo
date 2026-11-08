# Scribo — Roadmap

> **Current slice: 3 — Integration with Colligo.** Slices 0, 1 and 2 are closed: Scribo writes values with a per-field history in one transaction, reconciles changes made outside it, reads the data and the history back, and imports a MultiQC report in one transaction. Slice 3 depends on Colligo's Slice 3 (`samples`, `files`, `facts`). Nothing outside Slice 3 should be built yet.

The rule of this document: a thin slice end to end before breadth.

---

## Slice 0 — Decide whether Scribo needs to exist, and in what form (closed)

It is not code.

**Question 1, reformulated.** Not "does sqlite-history cover history?" — it does. The question is: **does any tool link each result to the verified inputs and invalidate it when they change?** Test sqlite-history, Dolt and Nextflow's data lineage on a real study for two or three days, trying to answer "which results become invalid if this FASTQ is replaced?".

**Question 2.** One module with two binaries or a single binary (`DESIGN.md` §5).

Definition of done: two sentences in `DESIGN.md` — what the existing tools lacked, and the chosen form. If nothing was lacking, the project ends here.

Outcome (2026-11-01): the tools tested lacked transitive invalidation, per-value provenance, source and custody events and input × result invariants, and two of three trust size and modification time (`DESIGN.md` §2, `RELATED.md` §3). Form: its own binary in its own repository, sharing only `CONTRACT.md` with Colligo (`DESIGN.md` §5).

---

## Slice 1 — Vertical: upsert, history, `--at` (closed)

The heart, rewritten per the SPEC.

Scope:

- `db.Backend` and `Tx` of SPEC §2: `Tx` without embedding `Backend`; `SelectRow` and `ProjectedRow` only on `Tx`; no `Insert`, `Update`, `Delete`.
- Opening per `CONTRACT.md` §2: filesystem detection, PRAGMAs in the DSN with `_txlock=immediate`, read-back, contract check.
- `tool_runs` with the contract's DDL; `run_id` in the history; no `command`.
- `{table}_history` of SPEC §4: `STRICT`, `old_is_null`/`new_is_null`, RFC 3339 timestamps.
- `record_key` as a canonical JSON array.
- `--null FIELD`; `FIELD=null` as text.
- Flow of SPEC §5 with the read inside the transaction.
- Reconciliation and `log --at`.
- Typed errors and exit codes.

Definition of done:

- Upsert of 3 fields generates 3 history rows; the second, changing 1 field, generates 1.
- 8 processes writing the same record: every `old_value` of the history matches the real sequence. `-race` clean.
- `record_key` of values with `,` and `=` does not collide.
- A write from outside with `sqlite3` generates `reconciled`, and `--at` warns.
- A database on a simulated network filesystem opens in `DELETE` with a warning.
- A `tool_runs` created by Colligo is accepted; a `tool_runs` with a divergent schema → `contract_mismatch`.

Outcome (2026-11-06): `upsert` and `log --at` work end to end on SQLite opened per `CONTRACT.md` §2; the six criteria are covered by tests, eight concurrent processes keep an unbroken history chain under `-race`, and changes made outside Scribo become `reconciled` rows.

---

## Slice 2 — Reading and entry door (closed)

Scope:

- Carried from Slice 1: the key must be exactly one unique key of the table (SPEC §4), fixing the history split by a partial key.
- Polish: `--version`; `scribo help exit-codes`; suggestion of table names; hints for refused numbers (SPEC §7); the `--key` hint for a positional key field; interruption by signal (SPEC §5); integration tests in parallel; a quick-start in the README.
- Reading: `query` and `export` with `--where` and refusal of untyped comparison; TSV, JSONL, CSV and `pretty` formats; `log --format pretty`, `--with-runs` and `--at run:N`; `upsert --dry-run`.
- Entry door: `schema --from` and `--from-multiqc` with `--module`; `ingest multiqc` with `--module` and the tool version.

Criterion:

- A partial key is refused with `key_mismatch` and the history of the record stays in one `record_key`.
- A real `multiqc_data.json` from nf-core/rnaseq becomes a table by `schema --from-multiqc | sqlite3` and is imported by `ingest multiqc` in one transaction; `query` returns the metrics; `--where "field>90"` against a column without a type fails with a clear message. The fixture that meets it is real MultiQC 1.30 output over MultiQC's own test data (`testdata/multiqc/`), not an nf-core/rnaseq run; a run of nf-core/rnaseq joins it as a second fixture when one is available.
- `--module fastp` of the same file gives a `fastp_qc` table with the reads before and after trimming and the fastp version.
- Ctrl-C during an upsert leaves the data and its history as they were and the run closed with `interrupted`.

Outcome (2026-11-08): the key rule, `query`, `export`, `log --format pretty`, `--with-runs`, `--at run:N` and `upsert --dry-run` read the ledger back; `schema` prints a STRICT table for a MultiQC report or a TSV, and `ingest multiqc` writes a whole report, General Statistics or one module with its tool version, in one run and one transaction. Each criterion is covered by a test (PR #18).

---

## Slice 3 — Integration with Colligo (current)

It depends on Colligo's Slice 3: a store with `samples`, `files` and `facts`.

The thesis of the project. It requires Colligo at its Slice 3 (store with `samples`, `files`, `facts`).

Scope: `--sample` resolved against `samples`; `--input` storing `result_inputs` with Colligo's root or a computed one; `scribo.rules.toml` and input × result invariants; `suspect` signal on violation; `stale` with graph and `--samplesheet`; `query --with-staleness`.

Criterion:

- `Mapped_Reads` greater than the FASTQ's reads is refused with exit 3 and a signal stored.
- Marking a FASTQ as `superseded` in Colligo makes stale exactly the results that depend on it, including by chain between results.
- `stale --samplesheet` generates an nf-core samplesheet with only the affected samples, and nf-core/rnaseq accepts it.

---

## Slice 4 — Looking at the results

Scope: `diff` with tolerance and cause; `outliers` with robust z-score and `--signal`; `ingest somalier` and `ingest ngscheckmate`; `export-signals`.

Criterion:

- A fixture with three changes — by input, by version, with no cause — correctly attributed; the one with no cause exits with 3.
- A planted sample with an alignment rate of 5% among samples at 90% becomes a `suspect` signal, and the `colligo status` of the same database shows it as suspect.
- A pair of samples swapped in a `somalier.pairs.tsv` generates `identity_mismatch` in both.

---

## Slice 5 — Batch, restricted and polish

Scope: `--batch` with total atomicity, stdin, `_sample` and `_input` columns, `--empty-as-null`, `--progress`; restricted-database regime; exhaustive `--help` with examples; shell completion of table and column names read from `--db`; README with a real Colligo → nf-core → Scribo pipeline; GoReleaser Linux and macOS on amd64 and arm64; CI with `-race`, `vet`, lint and comparison of `CONTRACT.md` with Colligo's copy.

Criterion: a batch of 1000 lines with an error on line 500 leaves the data table and its history identical, with the failed run recorded in `tool_runs`; `--debug` refused on a restricted database.

---

## Version milestones

```
v0.x.x   CLI interface under development, may change
v1.0.0   CLI interface frozen, strict semver; CONTRACT.md stable version
v1.x     Postgres as a second implementation, eventually
```

It stays at v0 until the interface is exercised in a real pipeline, together with Colligo.
