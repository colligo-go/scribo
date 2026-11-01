# Scribo — Related work

What already exists, and where Scribo fits. Every claim of "nobody does X" carries a date and is revisited.

Last review: 2026-09.

---

## 1. The thesis in one line

Value history in a database is a solved problem. Scribo adds, together with Colligo, what nobody links: **each result knows which verified inputs it came from, and stops being valid when they stop being valid.**

---

## 2. Data history and versioning

| Tool           | What it does                                                                                                              | What it does not do                                                                                      |
| -------------- | ------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| sqlite-history | Trigger-based history, with `_version`, `_updated` and `_mask` of changed columns; cannot be bypassed by direct `sqlite3` | Provenance, invalidation; reconstruction by date is not a ready-made command; requires Python to install |
| sqlite-utils   | insert, upsert, query, export from CSV/TSV/JSON                                                                           | History                                                                                                  |
| Dolt           | Versioned SQL database with commits; native `AS OF` and `dolt_history_<table>`; Go binary                                 | Not SQLite; does not know verified inputs, invariants nor invalidation by source                         |
| DVC            | Versions files and metrics per git commit                                                                                 | Per-sample metric with per-field history; verified provenance                                            |

Consequence: Scribo's history is not a differentiator. If Slice 0 shows that sqlite-history or Dolt cover the real use, Scribo's history may even be swapped for one of them.

---

## 3. Execution provenance

| Tool                  | What it does                                                                               | What it does not do                                                                                                            |
| --------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| Nextflow data lineage | Since 25.04, experimental: path, size and checksum of outputs; `lineage diff` between runs | Does not invalidate when the source changes later; does not store per-sample metrics; does not attribute a cause to the change |
| Snakemake             | Provenance inside the workflow; re-runs what changed                                       | Same limit: everything inside the workflow, nothing about the external source                                                  |

| DVC                   | Versions data and pipeline stages; `dvc status` lists the stages whose inputs changed      | Trusts size and modification time by default; marks only the stage that reads the changed file, not the ones that depend on it |
| Dagster               | Data assets with versions; marks an asset stale when an upstream version changes           | Sees a change of an external source only through an observation the user writes; staleness stops one step down             |
| nf-prov               | Nextflow plugin that writes the provenance of a run (BioCompute Object, RO-Crate)          | A static record of one run: nothing is invalidated later                                                                      |
| DataLad               | Versions datasets on git-annex; `datalad run` records the command and its inputs          | Re-execution is asked for (`datalad rerun`); no list of the results invalidated by a changed or suppressed input             |
| Pachyderm             | Versioned data repositories; pipelines re-run when an input repository gets a new commit   | Reacts only to changes inside its own storage, per file and not per value; knows nothing of the status at the source           |
| LaminDB               | Lineage of artifacts and transforms, queried in Python                                     | Records where a result came from; as reviewed, does not mark results invalid when an input is replaced or suppressed            |

Consequence: Scribo does not compete with the orchestrator. The orchestrator knows what ran; Scribo knows whether what ran is still valid.

### Hands-on test (Slice 0, 2026-10-31)

The same two-step pipeline — a QC step per sample, then a summary table over the samples — on three real ENA FASTQ files, in each tool. Scenario: one byte of a FASTQ changed with the same size and modification time; then the FASTQ replaced by a valid but different file.

| Tool | Corrupted FASTQ, same size and time | Valid replacement | Final summary marked invalid? |
| --- | --- | --- | --- |
| Nextflow 26.10.4, data lineage on | `-resume` reused all 4 tasks, exit 0 (the default cache trusts path, size and time; `cache 'deep'` catches it only by re-running) | re-runs the affected tasks; `lineage` finds results one hop at a time | no stale state; `lineage check` checks outputs only |
| DVC 3.67.1 | "Data and pipelines are up to date", exit 0 | lists only the QC stage of that sample | no, even with `--with-deps` |
| Dagster 1.13.28 | without an observation written by the user, everything stays fresh | with a SHA-256 observation, the QC asset of that sample becomes stale | no: the summary stays fresh |

The test was small (not a full nf-core pipeline, one day). Its point is the shape of the answer: none of the three links a result to the verified content of its inputs and invalidates it, and the summary — the value a person reads — stays valid in all three.

---

## 4. Per-sample metrics

| Tool        | What it does                                 | What it does not do                                               |
| ----------- | -------------------------------------------- | ----------------------------------------------------------------- |
| MultiQC     | Aggregates per-sample QC metrics in a report | History between runs, diff, provenance                            |
| MLflow, W&B | Registration of ML experiment metrics        | Heavy for HPC without a server; no provenance of biological input |

Consequence: MultiQC is an entry door, not a competitor. `ingest multiqc` turns the report that everybody already has into a queryable history.

---

## 5. Sample identity

| Tool         | What it does                                              | Relationship                                                                 |
| ------------ | --------------------------------------------------------- | ---------------------------------------------------------------------------- |
| somalier     | Relatedness between samples by genotype at selected sites | `ingest somalier` compares with the declared and returns a signal to Colligo |
| NGSCheckMate | Sample matching by SNPs                                   | same                                                                         |

Consequence: Scribo does not compute genetic identity. It compares what those tools observed with what the study declared.

---

## 6. What nobody covers together

As of this review (2026-11-01: the three tools tested above, plus nf-prov, DataLad, Pachyderm, LaminDB and MegaQC from their documentation), no tool is known that does:

- Link each result value to the content roots of the verified inputs.
- List the results that became invalid because an input was replaced or suppressed at the source, and generate the samplesheet to reprocess only them.
- Refuse a result that contradicts the facts of the input, such as more mapped reads than exist in the FASTQ.
- Say, when a result changes between runs, whether it was the input, the tool, or neither of the two.
- Return signals observed after the pipeline — outlier between samples, genetic identity — to the custody of the inputs.

---

## References

- sqlite-history — github.com/simonw/sqlite-history
- Dolt, querying history — docs.dolthub.com/sql-reference/version-control/querying-history
- Nextflow data lineage — docs.seqera.io/nextflow/tutorials/data-lineage
- MultiQC — multiqc.info
- MegaQC — github.com/MultiQC/MegaQC
- DVC — dvc.org
- Dagster, data assets and staleness — docs.dagster.io
- nf-prov — github.com/nextflow-io/nf-prov
- DataLad — datalad.org
- Pachyderm — docs.pachyderm.com
- LaminDB — docs.lamin.ai
- somalier — github.com/brentp/somalier
- NGSCheckMate — github.com/parklab/NGSCheckMate
