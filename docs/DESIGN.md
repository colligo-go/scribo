# Scribo — Design and Decisions

The purpose of this document is to record **why** Scribo is the way it is. For "how to implement", see `SPEC.md`. For what is shared with Colligo, `CONTRACT.md`. For what already exists outside here, `RELATED.md`. For "what was postponed", `BACKLOG.md`.

---

## 1. Vision

Scribo is the ledger of pipeline results: it stores values per sample, knows where each number came from, and knows when it stopped being valid. The name comes from the Latin *scribere* — "I write, I record".

Storing values in SQLite with history is a solved problem (see §2). What no tool does is the next question:

> Is this `Alignment_Rate = 94.3` still true? Which BAM did it come from, derived from which FASTQs, with which version of the tool — and has any of those things changed since?

Scribo answers this together with Colligo. Colligo guarantees the inputs before the pipeline; Scribo observes the results after; and each side corrects the other.

**It does:** stores values per record atomically; keeps history per field and reconstructs the state at any date; links each value to the inputs it depends on; refuses a result that contradicts the input; lists stale results when an input is invalidated; compares results between runs and says why they changed; detects out-of-pattern samples and returns them to Colligo as a signal about the input; imports metrics from MultiQC and genetic identity from somalier.

**It does not:** create the schema of a data table; analyse data; orchestrate a pipeline; acquire external data; align reads.

---

## 2. What already exists, and what is left

| Tool                  | Does                                                                         | Does not do                                                                                   |
| --------------------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| sqlite-utils          | insert, upsert, query, export from CSV/TSV/JSON                              | history, provenance                                                                           |
| sqlite-history        | trigger-based history with version, timestamp and mask of changed columns    | reconstruction CLI by date; provenance; binary without Python                                 |
| Dolt                  | versioned SQL database, native `AS OF` and `dolt_history_<table>`, Go binary | not SQLite; does not know verified inputs nor invalidation                                    |
| Nextflow data lineage | records path, size and checksum of a workflow's outputs                      | does not invalidate a result when the source changes later; does not store per-sample metrics |
| MultiQC               | aggregates per-sample QC metrics in a report                                 | history between runs, provenance, invalidation                                                |

History alone is saturated: a Scribo that only does that has no reason to exist. What is left, and nobody covers, is the combination with Colligo: **provenance per value, invalidation by reachability, input × result invariants and diff with cause.**

Slice 0 tests exactly that, with the question reformulated: "does any tool link each result to the verified inputs and invalidate it when they change?". If the answer is yes, the project ends — and that is the best possible outcome.

---

## 3. Principles

**Total atomicity.** An error at any step, nothing is persisted. It holds for a single write and for a batch of a thousand records.

**Read and write in the same transaction.** The current state read to compute the history is the state that the write alters. See §4.

**Declared against observed, in the result too.** The same principle as Colligo: a result that contradicts the facts of the input is refused, not stored with a warning.

**A clear error, always.** Stderr with a descriptive message and a specific exit code.

**Generic by design, specialised by contract.** Scribo works without Colligo, as a ledger with history. With Colligo present in the same database, it gains sample identity, provenance and invalidation.

**Single binary.** Zero runtime dependencies.

**Clean stdout on write success.**

---

## 4. Settled decisions and their whys

### Reading inside the transaction, with `BEGIN IMMEDIATE`

The earlier version validated and read the current state outside the transaction, to shorten the lock window. That is right for schema validation and wrong for the state: if another job writes between the read and the `BEGIN`, the `old_value` stored in the history is wrong and the reconciliation flags a gap that does not exist.

Decision: validation of table, columns and coercion outside the transaction. `SelectRow`, `ProjectedRow`, `Upsert` and `AppendHistory` inside a `BEGIN IMMEDIATE` transaction. With a plain (deferred) `BEGIN` in WAL, the promotion from read to write can fail with `SQLITE_BUSY` without `busy_timeout` helping; `IMMEDIATE` takes the write lock on entry and waits for the `busy_timeout` normally.

### Journal mode depends on the filesystem, and is detected

In HPC, shared scratch is almost always NFS, Lustre or GPFS, and WAL on a network filesystem is silent corruption. `statfs` on opening: local → WAL; network → DELETE with a warning. The rule holds the same in Colligo, per `CONTRACT.md` §2 — a shared database protected by one tool and corrupted by the other is not protected.

### Spool: the value is the atomic merge with history

The pattern recommended for HPC is spool: each job writes a local file and an `upsert --batch` consolidates at the end. That looks like the "TSV + update script" that Scribo replaces, and it is — in the intermediate format. The difference is in the merge: atomic, with per-field history, provenance and invariants checked. The intermediate file is not the problem; the merge without guarantees is.

### `tool_runs` is a contract, not a convention

The earlier version said that `tool_runs` was shared with Colligo, but the two schemas diverged, and with `CREATE TABLE IF NOT EXISTS` whoever creates it first breaks the other. Now the schema is in `CONTRACT.md`, identical in both repositories, with `contract_version` and a check on opening.

### `run_id` in place of `command`

Storing the expanded command leaks path, token and patient identifier. The history points to `tool_runs`, which stores only the shape of the command.

### NULL has its own syntax

The shell removes quotes before the program sees the argument: `FIELD="null"` and `FIELD=null` arrive the same. Distinction by quotes is impossible. Decision: `FIELD=null` is the text `null`; a real NULL is `--null FIELD`. No magic in values.

### `record_key` is injective

`k=v,k2=v2` collides when a value contains `,` or `=`: different records, same history key. The canonical key becomes a JSON array of pairs ordered by field name, `[["accession","ENCFF001"],["replicate","1"]]`, serialised without spaces. Injective by construction.

### Provenance per value

`upsert --input file` stores edges in `result_inputs`: the record, or the field, depends on that file's BLAKE3 root. If the file is under Colligo's custody, the root comes from `files`; if not, Scribo computes it with the common core's fact extractor, in the same single read. No value needs provenance, but whoever declares it gains invalidation and invariants.

### Staleness is reachability

Results, files and accessions form a dependency graph. A result is stale if it reaches any invalidated node: a `stale` or `superseded` file, a suppressed run, a failed identity, or another stale result. It is a transitive closure over the graph, computed on demand; never a `stale` column that someone forgets to update. `scribo stale` lists; `--samplesheet` generates the nf-core samplesheet with only the affected samples — detect, measure the impact, reprocess only what is necessary.

### Input × result invariants

A result cannot contradict the input: mapped reads ≤ reads of the FASTQ; reads assigned to genes ≤ mapped; rate in [0, 1]. Rules declared in `scribo.rules.toml`, versioned with the analysis, with the hash recorded in `tool_runs.config_hash`. Violation → refusal, exit 3. It is what catches "the wrong file in the right place": 12 million mapped reads in a sample whose FASTQ has 10 million. It is only possible because Colligo already counted the reads.

### Diff with cause

`scribo diff --run A --run B` compares field by field, with a per-field tolerance, and attributes the cause of each change:

- **input:** some input root of the record differs between the runs;
- **tool:** `tool_version` or `step` differs in `tool_runs`;
- **no apparent cause:** identical inputs and versions, different result — non-determinism, the alert that almost nobody gives.

It is the "semantic diff" that Colligo recorded as the idea of highest scientific value, with Scribo as the natural substrate.

### Deviation between samples becomes a signal about the input

Colligo does not align reads, by decision. But the pipeline does, and Scribo stores the result. A sample with an alignment rate of 5% among samples with 90% suggests a swapped organism or a wrong file.

`scribo outliers --field F --step S` uses a robust z-score — median and MAD per field and step, because mean and standard deviation are dragged by the outlier itself — and records a `suspect` signal in `input_signals` for the sample's inputs when `|z|` passes the limit. Colligo consumes the signal in custody. It solves "declared × observed organism" without Colligo crossing the boundary of the analysis.

### Identity confirmed by genotype

`scribo ingest somalier` (and `ngscheckmate`) reads the output of those tools and records, per pair of samples, whether they match genetically. Samples declared as the same person that do not match, or declared different that match, become `identity_mismatch`; those that match as declared, `identity_confirmed`. It is the "propagated sample identity" of Colligo's backlog, closed with tools that already exist.

### MultiQC is the entry door

`scribo ingest multiqc multiqc_data.json` imports the general per-sample metrics into the declared table. Everyone who runs nf-core already has this file: Scribo is useful in the first minute, without changing the pipeline. The table must exist; `schema --from-multiqc` generates the DDL for review.

### Type affinity is respected, and an untyped comparison is an error

A numeric comparison against a column without a declared type is lexicographic and silent. On write, a numeric value in a numeric column is bound as a number. In `--where`, `>`, `<`, `>=`, `<=` against a column without a type → error `untyped_comparison`.

### The history may have a gap, and the gap is detected

A write through `sqlite3` or any other route creates a gap. Inside the transaction, the current state is compared with the one projected by the history; a divergence becomes a `reconciled` row, and a `log --at` that crosses it warns. History is never deleted nor rewritten.

### Classification is consumed, not reimplemented

With Colligo's `study_meta` marking the study as restricted: `--debug` refused, permission checked, synced folder refused.

### `upsert` preserves fields not mentioned

Fields not mentioned stay as they were.

### Timestamp with millisecond precision, `id` as tie-breaker

Chronological ordering is always `(changed_at, id)`.

### `schema` generates, it does not create

`scribo schema --from file.tsv` and `--from-multiqc` **print** the `CREATE TABLE`. Scribo never creates a data table.

### `--where` filters: basic operators, not SQL

`=`, `!=`, `>`, `<`, `>=`, `<=`, `field=null`, `field!=null`. Implicit AND. Whoever needs more uses `sqlite3`.

### JSON is JSONL

`query`, `export` and `diff` produce TSV by default; `--format json` produces one object per line.

### Backend abstraction remains

All persistence goes through `db.Backend`. One implementation in v1. Handlers never call SQL directly.

### Infrastructure

Latest stable Go. MIT. cobra. v0 until the interface stabilises.

---

## 5. Relationship with Colligo

### One module, two binaries — proposed decision

With provenance, invariants and invalidation, Scribo comes to need Colligo's fact extractor, the `tool_runs` contract and the database opening. The duplication that "had not yet triggered" triggers here.

Options:

1. **One module, two binaries.** `colligo` and `scribo` import a common core: `facts`, `runs`, `store/open`. It keeps the name and the generic tool for those who do not use Colligo.
2. **One binary.** Scribo becomes the results side: `colligo record`, `colligo stale`, `colligo diff`.

Proposal: **option 1**. The final decision is part of Slice 0. Until then, `CONTRACT.md` guarantees that the two tools talk without sharing code.

### What Scribo gives Colligo

| Contribution                       | Mechanism                           | Colligo item it unlocks                  |
| ---------------------------------- | ----------------------------------- | ---------------------------------------- |
| Signal of a suspect input          | `outliers` → `input_signals`        | declared × observed organism             |
| Confirmed identity                 | `ingest somalier` → `input_signals` | propagated sample identity               |
| Reprocessing plan                  | `stale --samplesheet`               | closes the cycle of `recheck --upstream` |
| End-to-end completeness            | expected results in `[results]`     | `status` with the complete funnel        |
| Scrub priority                     | count in `result_inputs`            | scrub of what matters most first         |
| Garbage collection with provenance | `result_inputs` as usage history    | `gc` that knows what is regenerable      |
| Corpus of real failures            | `export-signals`                    | `FAILURES.md` and benchmark              |

### What Colligo gives Scribo

Sample identity (`samples`), verified roots (`files`), facts for invariants (`facts`), accession status at the source, study classification.

---

## 6. Declared limits

- It does not create the schema of a data table.
- It does not replace SQL.
- History is of the application, not of a trigger. A write outside Scribo is detected, not prevented.
- A write to SQLite serialises.
- It does not orchestrate or supervise execution.
- It does not align reads nor interpret biology. Outliers and genetic identity come from metrics that the pipeline and external tools produced; Scribo only compares and flags them.
- Without Colligo in the database, there is no verified provenance, no `input.*` invariants and no invalidation by source. Scribo warns when a feature is not available for lack of it, never pretends.
