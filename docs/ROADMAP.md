# Scribo — Roadmap

> **Estado atual: Fatia 0 — decisão pendente.** Nada deve ser construído antes dela.

A regra deste documento: fatia fina de ponta a ponta antes de largura. O código existente em `internal/` é um esqueleto anterior a esta SPEC e é reescrito na Fatia 1, não remendado.

---

## Fatia 0 — Decidir se o Scribo precisa existir, e em que forma

Não é código.

**Pergunta 1, reformulada.** Não "o sqlite-history cobre histórico?" — cobre. A pergunta é: **alguma ferramenta liga cada resultado às entradas verificadas e o invalida quando elas mudam?** Testar sqlite-history, Dolt e o data lineage do Nextflow num estudo real por dois ou três dias, tentando responder "quais resultados ficam inválidos se este FASTQ for substituído?".

**Pergunta 2.** Um módulo com dois binários ou um binário só (`DESIGN.md` §5).

Critério de pronto: duas frases no `DESIGN.md` — o que faltou nas ferramentas existentes, e a forma escolhida. Se nada faltou, o projeto encerra aqui.

---

## Fatia 1 — Vertical: upsert, histórico, `--at`

O coração, reescrito conforme a SPEC.

Escopo:

- `db.Backend` e `Tx` da SPEC §2: `Tx` sem embutir `Backend`; `SelectRow` e `ProjectedRow` só em `Tx`; sem `Insert`, `Update`, `Delete`.
- Abertura do `CONTRACT.md` §2: detecção de filesystem, PRAGMAs no DSN com `_txlock=immediate`, leitura de volta, conferência do contrato.
- `tool_runs` com o DDL do contrato; `run_id` no histórico; nada de `command`.
- `{table}_history` da SPEC §4: `STRICT`, `old_is_null`/`new_is_null`, timestamps RFC 3339.
- `record_key` como array JSON canônico.
- `--null FIELD`; `FIELD=null` como texto.
- Fluxo da SPEC §5 com leitura dentro da transação.
- Reconciliação e `log --at`.
- Erros tipados e exit codes.

Critério de pronto:

- Upsert de 3 campos gera 3 linhas de histórico; o segundo, mudando 1 campo, gera 1.
- 8 processos escrevendo o mesmo registro: todo `old_value` do histórico confere com a sequência real. `-race` limpo.
- `record_key` de valores com `,` e `=` não colide.
- Escrita por fora com `sqlite3` gera `reconciled`, e `--at` avisa.
- Banco em filesystem de rede simulado abre em `DELETE` com aviso.
- `tool_runs` criada pelo Colligo é aceita; `tool_runs` com schema divergente → `contract_mismatch`.

---

## Fatia 2 — Leitura e porta de entrada

Escopo: `query` e `export` com `--where` e recusa de comparação sem tipo; formatos TSV, JSONL e CSV; `schema --from` e `--from-multiqc`; `ingest multiqc`.

Critério: um `multiqc_data.json` real do nf-core/rnaseq vira tabela por `schema --from-multiqc | sqlite3` e é importado por `ingest multiqc` numa transação; `query` devolve as métricas; `--where "campo>90"` contra coluna sem tipo falha com mensagem clara.

---

## Fatia 3 — Integração com o Colligo

A tese do projeto. Exige o Colligo na Fatia 3 dele (store com `samples`, `files`, `facts`).

Escopo: `--sample` resolvido contra `samples`; `--input` gravando `result_inputs` com raiz do Colligo ou calculada; `scribo.rules.toml` e invariantes entrada × resultado; sinal `suspect` em violação; `stale` com grafo e `--samplesheet`; `query --with-staleness`.

Critério:

- `Mapped_Reads` maior que os reads do FASTQ é recusado com exit 3 e sinal gravado.
- Marcar um FASTQ como `superseded` no Colligo torna obsoletos exatamente os resultados que dependem dele, inclusive por cadeia entre resultados.
- `stale --samplesheet` gera samplesheet nf-core só com as amostras afetadas, e o nf-core/rnaseq a aceita.

---

## Fatia 4 — Olhar para os resultados

Escopo: `diff` com tolerância e causa; `outliers` com z-score robusto e `--signal`; `ingest somalier` e `ingest ngscheckmate`; `export-signals`.

Critério:

- Fixture com três mudanças — por entrada, por versão, sem causa — atribuídas corretamente; a sem causa sai com exit 3.
- Amostra plantada com taxa de alinhamento de 5% entre amostras a 90% vira sinal `suspect`, e o `colligo status` do mesmo banco a mostra como suspeita.
- Par de amostras trocado num `somalier.pairs.tsv` gera `identity_mismatch` nas duas.

---

## Fatia 5 — Batch, restrito e polimento

Escopo: `--batch` com atomicidade total, stdin, colunas `_sample` e `_input`, `--empty-as-null`, `--progress`; regime de banco restrito; `--help` exaustivo com exemplos; README com pipeline real Colligo → nf-core → Scribo; GoReleaser Linux e macOS em amd64 e arm64; CI com `-race`, `vet`, lint e comparação do `CONTRACT.md` com a cópia do Colligo.

Critério: batch de 1000 linhas com erro na linha 500 deixa o banco idêntico; `--debug` recusado em banco restrito.

---

## Marcos de versão

```
v0.x.x   interface CLI em desenvolvimento, pode mudar
v1.0.0   interface CLI congelada, semver estrito; CONTRACT.md versão estável
v1.x     Postgres como segunda implementação, eventualmente
```

Permanece em v0 até a interface estar exercitada em pipeline real, junto com o Colligo.
