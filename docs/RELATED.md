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

Consequence: Scribo does not compete with the orchestrator. The orchestrator knows what ran; Scribo knows whether what ran is still valid.

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

As of this review, no tool is known that does:

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
- somalier — github.com/brentp/somalier
- NGSCheckMate — github.com/parklab/NGSCheckMate
