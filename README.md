# Scribo

The ledger for pipeline results: which inputs each number came from, and when it stopped being true.

```
scribo upsert --db study.db --table rnaseq_qc --sample SRR2133898 \
  Alignment_Rate=94.3 --input results/SRR2133898.bam
scribo stale --db study.db
```

Sibling of [Colligo](https://github.com/colligo-go/colligo): Colligo guarantees the inputs
before the pipeline, Scribo watches the results after, and each side corrects the other.

Status: pre-release. The design decision on whether this should exist at all is open —
see Fatia 0 in `docs/ROADMAP.md`.

License: MIT.
