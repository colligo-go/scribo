# Scribo

The ledger for pipeline results: it keeps the values of each sample in SQLite, remembers where every number came from, and will tell you when it stopped being true. A command-line tool written in Go, sibling of [Colligo](https://github.com/colligo-go/colligo): Colligo guarantees the inputs before the pipeline, Scribo watches the results after, and each side corrects the other.

## Install

```sh
go install github.com/colligo-go/scribo@latest
```

## Quick start

Scribo never creates your data table: you declare it, with a `UNIQUE` key that names one record, and Scribo writes to it and keeps its history.

```sh
sqlite3 study.db "CREATE TABLE rnaseq_qc (sample TEXT, replicate INTEGER, Alignment_Rate REAL, Mapped_Reads INTEGER, UNIQUE(sample, replicate))"
scribo upsert --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1 Alignment_Rate=94.3 Mapped_Reads=12000000
scribo upsert --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1 Alignment_Rate=95.1
scribo log --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1
scribo log --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1 --at 2026-11-07T12:00:00Z
```

The first `upsert` creates the record, the second changes one value, and `log` lists every change with the run that made it, oldest first. The last line shows the record as it stood at a moment; replace `2026-11-07T12:00:00Z` with an instant between your two writes (RFC 3339, with a time and a zone).

## From a MultiQC report

Everyone who runs nf-core already has a `multiqc_data.json`. `schema` prints a table for it, which you review and create, and `ingest multiqc` writes every sample in one transaction. `--module fastp` takes every per-sample field of fastp, reads before and after filtering included, with the fastp version; without `--module` you get the General Statistics. `testdata/multiqc/multiqc_data.json` in this repository is a real report to try it on.

```sh
scribo schema --from-multiqc multiqc_data.json --table fastp_qc --module fastp > fastp_qc.sql
sqlite3 study.db < fastp_qc.sql
scribo ingest multiqc --db study.db --table fastp_qc --module fastp multiqc_data.json
scribo query --db study.db --table fastp_qc --fields sample,tool_version,before_filtering_total_reads,after_filtering_total_reads --format pretty
```

Ingesting the same report again records nothing new; a report whose values changed adds one history row per changed value.

Run `scribo help exit-codes` for the exit status and the typed error code of every failure, and `scribo <command> --help` for the flags of a command.

## Status

Pre-release. The plan is in `docs/ROADMAP.md`.

License: MIT.
