# Scribo — Trabalhos relacionados

O que já existe, e onde o Scribo se encaixa. Toda afirmação de "ninguém faz X" tem data e é revisitada.

Última revisão: 2026-09.

---

## 1. A tese em uma linha

Histórico de valores em banco é problema resolvido. O Scribo acrescenta, junto com o Colligo, o que ninguém liga: **cada resultado sabe de quais entradas verificadas veio, e deixa de valer quando elas deixam.**

---

## 2. Histórico e versionamento de dados

| Ferramenta | O que faz | O que não faz |
| ---------- | --------- | ------------- |
| sqlite-history | Histórico por trigger, com `_version`, `_updated` e `_mask` de colunas alteradas; não contornável por `sqlite3` direto | Proveniência, invalidação; reconstrução por data não é comando pronto; exige Python para instalar |
| sqlite-utils | insert, upsert, query, export a partir de CSV/TSV/JSON | Histórico |
| Dolt | Banco SQL versionado com commits; `AS OF` e `dolt_history_<tabela>` nativos; binário Go | Não é SQLite; não conhece entradas verificadas, invariantes nem invalidação por fonte |
| DVC | Versiona arquivos e métricas por commit de git | Métrica por amostra com histórico por campo; proveniência verificada |

Consequência: o histórico do Scribo não é diferencial. Se a Fatia 0 mostrar que o sqlite-history ou o Dolt cobrem o uso real, o histórico do Scribo pode até ser trocado por um deles.

---

## 3. Proveniência de execução

| Ferramenta | O que faz | O que não faz |
| ---------- | --------- | ------------- |
| Nextflow data lineage | Desde a 25.04, experimental: caminho, tamanho e checksum dos outputs; `lineage diff` entre execuções | Não invalida quando a fonte muda depois; não guarda métrica por amostra; não atribui causa à mudança |
| Snakemake | Proveniência dentro do workflow; reexecuta o que mudou | Mesmo limite: tudo dentro do workflow, nada sobre a fonte externa |

Consequência: o Scribo não compete com o orquestrador. O orquestrador sabe o que rodou; o Scribo sabe se o que rodou ainda vale.

---

## 4. Métricas por amostra

| Ferramenta | O que faz | O que não faz |
| ---------- | --------- | ------------- |
| MultiQC | Agrega métricas de QC por amostra num relatório | Histórico entre execuções, diff, proveniência |
| MLflow, W&B | Registro de métricas de experimentos de ML | Pesados para HPC sem servidor; nada de proveniência de entrada biológica |

Consequência: MultiQC é porta de entrada, não concorrente. `ingest multiqc` transforma o relatório que todo mundo já tem em histórico consultável.

---

## 5. Identidade de amostra

| Ferramenta | O que faz | Relação |
| ---------- | --------- | ------- |
| somalier | Parentesco entre amostras por genótipo em sítios selecionados | `ingest somalier` compara com o declarado e devolve sinal ao Colligo |
| NGSCheckMate | Correspondência de amostras por SNPs | idem |

Consequência: o Scribo não calcula identidade genética. Ele compara o que essas ferramentas observaram com o que o estudo declarou.

---

## 6. O que ninguém cobre junto

Até esta revisão, não se conhece ferramenta que faça:

- Ligar cada valor de resultado às raízes de conteúdo das entradas verificadas.
- Listar os resultados que ficaram inválidos porque uma entrada foi substituída ou suprimida na fonte, e gerar a samplesheet para reprocessar só eles.
- Recusar um resultado que contradiz os fatos da entrada, como mais reads mapeados do que existem no FASTQ.
- Dizer, quando um resultado muda entre execuções, se foi a entrada, a ferramenta, ou nenhuma das duas.
- Devolver sinais observados depois do pipeline — outlier entre amostras, identidade genética — para a custódia das entradas.

---

## Referências

- sqlite-history — github.com/simonw/sqlite-history
- Dolt, querying history — docs.dolthub.com/sql-reference/version-control/querying-history
- Nextflow data lineage — docs.seqera.io/nextflow/tutorials/data-lineage
- MultiQC — multiqc.info
- somalier — github.com/brentp/somalier
- NGSCheckMate — github.com/parklab/NGSCheckMate
