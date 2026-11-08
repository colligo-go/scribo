# MultiQC fixture

`multiqc_data.json` is real MultiQC output over real tool outputs; not produced by nf-core/rnaseq: add an nf-core run as a second fixture when one is available.

The file is versioned verbatim, exactly as MultiQC wrote it: no field was edited.

## Provenance

- MultiQC: 1.30, run through `uvx`.
- Tool outputs: [MultiQC/test-data](https://github.com/MultiQC/test-data) at commit `ae5e10025fa1b0fd6792f8f2b513e160d78474fe` (ae5e100).
- License of the test data: MIT, Copyright (c) 2015 Phil Ewels.

## Input files

Copied from the test-data repository, keeping the path below `data/modules/`:

```
cutadapt/v1.8/SRR1067503_1.fastq.gz_trimming_report.txt
cutadapt/v1.8/SRR1067505_1.fastq.gz_trimming_report.txt
fastp/issue_2138/HG001.fastp.json
fastp/issue_2138/HG002_son.fastp.json
fastp/issue_2138/HG003_father.fastp.json
fastp/issue_2138/HG004_mother.fastp.json
fastqc/v0.10.1/SRR1067503_v10_1_fastqc.zip
fastqc/v0.10.1/SRR1067505_v10_1_fastqc.zip
salmon/v0.9.1/DRR016125_quant/aux_info/ambig_info.tsv
salmon/v0.9.1/DRR016125_quant/aux_info/exp3_seq.gz
salmon/v0.9.1/DRR016125_quant/aux_info/exp5_seq.gz
salmon/v0.9.1/DRR016125_quant/aux_info/exp_gc.gz
salmon/v0.9.1/DRR016125_quant/aux_info/expected_bias.gz
salmon/v0.9.1/DRR016125_quant/aux_info/fld.gz
salmon/v0.9.1/DRR016125_quant/aux_info/meta_info.json
salmon/v0.9.1/DRR016125_quant/aux_info/obs3_seq.gz
salmon/v0.9.1/DRR016125_quant/aux_info/obs5_seq.gz
salmon/v0.9.1/DRR016125_quant/aux_info/obs_gc.gz
salmon/v0.9.1/DRR016125_quant/aux_info/observed_bias.gz
salmon/v0.9.1/DRR016125_quant/aux_info/observed_bias_3p.gz
salmon/v0.9.1/DRR016125_quant/cmd_info.json
salmon/v0.9.1/DRR016125_quant/lib_format_counts.json
salmon/v0.9.1/DRR016125_quant/libParams/flenDist.txt
salmon/v0.9.1/DRR016125_quant/logs/salmon_quant.log
star/older_logs/SRR1067503_1_trimmedLog.final.out
star/older_logs/SRR1067505_1_trimmedLog.final.out
```

## Command

Run from the neutral directory `/tmp/scribo-multiqc-fixture`, with the files above under `data/`, and with uv's cache in `/tmp/scribo-multiqc-uv`, so that no path in the JSON names a local user: MultiQC records its own command line, its install path and the output directory.

```sh
UV_CACHE_DIR=/tmp/scribo-multiqc-uv uvx --from multiqc==1.30 multiqc data -o out --force --data-format json --no-report
cp out/multiqc_data/multiqc_data.json testdata/multiqc/multiqc_data.json
```

`TestFixtureCarriesNoLocalPath` fails if the file ever holds `/home/` or `/Users/`.

## Samples

| Module | Samples |
|---|---|
| cutadapt, STAR | `SRR1067503_1`, `SRR1067505_1` |
| FastQC | `SRR1067503_v10_1`, `SRR1067505_v10_1` |
| fastp | `HG001`, `HG002_son`, `HG003_father`, `HG004_mother` |
| Salmon | `DRR016125_quant` |
