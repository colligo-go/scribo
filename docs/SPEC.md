# Scribo — Especificação Técnica

Documento declarativo. Para o porquê, ver `DESIGN.md`. Para tudo que é comum com o Colligo — abertura do banco, `tool_runs`, `result_inputs`, `input_signals`, invariantes e grafo de dependências —, `CONTRACT.md`, que prevalece sobre este documento em caso de conflito. Implemente a partir daqui.

Módulo: `github.com/<org>/scribo`
Go: estável mais recente.
Driver SQLite: `modernc.org/sqlite`.
CLI: `github.com/spf13/cobra`.

---

## 1. Estrutura de pacotes

```
scribo/
├── main.go
├── cmd/
│   ├── root.go
│   ├── upsert.go
│   ├── log.go
│   ├── query.go
│   ├── export.go
│   ├── schema.go
│   ├── ingest.go
│   ├── stale.go
│   ├── diff.go
│   ├── outliers.go
│   └── signals.go
├── internal/
│   ├── db/
│   │   ├── backend.go
│   │   ├── sqlite/
│   │   │   ├── sqlite.go
│   │   │   ├── tx.go
│   │   │   ├── open.go
│   │   │   └── fsdetect.go
│   │   └── value.go
│   ├── contract/
│   │   └── contract.go
│   ├── history/
│   │   ├── history.go
│   │   ├── recordkey.go
│   │   └── reconcile.go
│   ├── provenance/
│   │   ├── inputs.go
│   │   └── graph.go
│   ├── rules/
│   │   └── rules.go
│   ├── ingest/
│   │   ├── multiqc.go
│   │   └── somalier.go
│   ├── stats/
│   │   └── robust.go
│   ├── run/
│   │   └── run.go
│   ├── writer/
│   │   └── writer.go
│   ├── reader/
│   │   └── reader.go
│   ├── format/
│   │   └── format.go
│   └── exitcode/
│       └── exitcode.go
└── testdata/
```

Tudo em `internal/`. Quando o núcleo comum com o Colligo existir (`DESIGN.md` §5), `db/sqlite/open.go`, `db/sqlite/fsdetect.go`, `contract/` e o cálculo de raiz de `provenance/` passam a importar dele.

---

## 2. Interface `db.Backend`

Toda persistência passa por aqui. SQL nunca vive nos handlers.

```go
type Backend interface {
    Open(ctx context.Context, dsn string, opts OpenOptions) error
    Close() error

    JournalMode() string
    Classification(ctx context.Context) (string, error)
    ColligoPresent(ctx context.Context) (bool, error)

    TableExists(ctx context.Context, table string) (bool, error)
    Columns(ctx context.Context, table string) ([]Column, error)
    EnsureContractTables(ctx context.Context) error
    EnsureHistoryTable(ctx context.Context, table string) error
    BeginRun(ctx context.Context, meta RunMeta) (int64, error)
    EndRun(ctx context.Context, runID int64, exitCode int) error

    SelectRows(ctx context.Context, table string, where []Predicate, fields []string) (Rows, error)
    ResolveSample(ctx context.Context, ref string) (Sample, error)
    FileRoot(ctx context.Context, path string) (string, bool, error)
    InputFacts(ctx context.Context, roots []string) (InputFacts, error)
    InvalidatedNodes(ctx context.Context) ([]Node, error)
    Edges(ctx context.Context) ([]Edge, error)
    HistoryBetween(ctx context.Context, table string, runA, runB int64) ([]HistoryRow, error)

    BeginTx(ctx context.Context) (Tx, error)
}

type Tx interface {
    SelectRow(ctx context.Context, table string, key Key, fields []string) (Row, error)
    ProjectedRow(ctx context.Context, table string, recordKey string) (Row, error)
    Upsert(ctx context.Context, table string, key Key, values Values, mode WriteMode) ([]FieldChange, error)
    AppendHistory(ctx context.Context, table string, recordKey string, changes []FieldChange, runID int64, at time.Time, source HistorySource) error
    AppendInputs(ctx context.Context, edges []InputEdge) error
    AppendSignals(ctx context.Context, signals []Signal) error
    Commit() error
    Rollback() error
}
```

`Tx` não embute `Backend`: transação aninhada não existe em SQLite, e a interface estreita torna isso impossível de escrever. As leituras que alimentam o histórico — `SelectRow` e `ProjectedRow` — existem **só** em `Tx`, para que seja impossível lê-las fora da transação que escreve.

`BeginTx` abre `BEGIN IMMEDIATE` (`_txlock=immediate` no DSN).

Tipos auxiliares:

```go
type WriteMode int

const (
    ModeUpsert WriteMode = iota
    ModeInsertOnly
    ModeUpdateOnly
)

type HistorySource int

const (
    SourceScribo HistorySource = iota
    SourceReconciled
)

type OpenOptions struct {
    JournalMode string
    BusyTimeout int
    Restricted  bool
}

type Column struct {
    Name     string
    Affinity string
    Nullable bool
    Declared bool
}

type Key    map[string]Value
type Values map[string]Value

type Value struct {
    Raw    string
    IsNull bool
}

type Predicate struct {
    Field string
    Op    string
    Value Value
}

type FieldChange struct {
    Field    string
    OldValue Value
    NewValue Value
}

type RunMeta struct {
    ToolVersion string
    GitSHA      string
    GoVersion   string
    ArgvShape   string
    Step        string
    Host        string
    ConfigHash  string
}

type Sample struct {
    ID           string
    Accession    string
    ExperimentID string
    Role         string
    Replicate    string
}

type InputEdge struct {
    RunID       int64
    ResultTable string
    RecordKey   string
    Field       string
    InputRoot   string
    InputKind   string
}

type InputFacts struct {
    Reads int64
    Bases int64
    Known []string
    Missing []string
}

type Signal struct {
    RunID     int64
    InputRoot string
    Kind      string
    Source    string
    Detail    string
}

type Node struct {
    ID     string
    Kind   string
    Reason string
}

type Edge struct {
    From string
    To   string
}

type Row  map[string]Value
type Rows []Row
```

---

## 3. Abertura

Conforme `CONTRACT.md` §2: detecção de filesystem, PRAGMAs no DSN com `_txlock=immediate`, leitura de volta em cada conexão, conferência das tabelas do contrato. `SetMaxOpenConns(1)` para o processo de escrita.

Depois:

1. Ler `study_meta` se existir. Classificação `restricted` liga o regime de §11.
2. `ColligoPresent`: existem `samples`, `files` e `facts` com o schema do contrato. Resultado cacheado por processo; funcionalidades dependentes do Colligo consultam este valor e reportam indisponibilidade em vez de falhar em silêncio.

---

## 4. Tabelas que o Scribo cria

As do contrato de que é dono — `result_inputs`, `input_signals` — e a sua parte de `tool_runs`, todas com DDL de `CONTRACT.md` §3. Mais uma por tabela de dados:

```sql
CREATE TABLE IF NOT EXISTS {table}_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES tool_runs(id),
    record_key  TEXT NOT NULL,
    field       TEXT NOT NULL,
    old_value   TEXT,
    old_is_null INTEGER NOT NULL,
    new_value   TEXT,
    new_is_null INTEGER NOT NULL,
    source      TEXT NOT NULL CHECK (source IN ('scribo','reconciled')),
    changed_at  TEXT NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_{table}_history_record ON {table}_history(record_key, changed_at, id);
CREATE INDEX IF NOT EXISTS idx_{table}_history_field  ON {table}_history(field, changed_at);
CREATE INDEX IF NOT EXISTS idx_{table}_history_run    ON {table}_history(run_id);
```

A tabela de dados nunca é criada nem alterada.

`old_is_null` e `new_is_null` separam NULL de texto vazio sem depender de valor mágico.

### `record_key`

Array JSON de pares `[nome, valor]` ordenados pelo nome do campo, sem espaços, valor NULL como `null` JSON:

```
[["accession","ENCFF001"],["replicate","1"]]
```

Injetivo por construção: registros diferentes nunca produzem a mesma chave.

---

## 5. Fluxo de uma escrita

Validação fora da transação. Estado e escrita dentro.

```
1.  validar flags                                    erro → exit 1
2.  abrir Backend (§3)                               erro → exit 5
3.  TableExists                                      falso → exit 2, table_not_found
4.  Columns; validar que todo campo existe           falta → exit 2, column_not_found, sugestão por distância de edição
5.  coagir valores conforme afinidade                falha → exit 3, invalid_value
6.  resolver --sample, se houver                     falha → exit 3, unknown_sample
7.  resolver --input em raízes                       falha → exit 4, input_not_found
8.  carregar scribo.rules.toml, se houver            falha → exit 4, invalid_rules
9.  EnsureContractTables, EnsureHistoryTable         uma vez por processo, com cache
10. BeginRun                                         devolve run_id
11. BeginTx                                          BEGIN IMMEDIATE
12. SelectRow e ProjectedRow                         estado atual e projetado, dentro da transação
13. reconciliar divergência                          FieldChange com source=reconciled
14. avaliar regras (§8) sobre o estado resultante    violação → exit 3, invariant_violated
15. Upsert                                           FieldChange dos campos que mudaram
16. AppendHistory, AppendInputs                      mesmo run_id
17. Commit
18. EndRun                                           ended_at e exit_code
```

Erro a partir de 11 → Rollback, mensagem no stderr, exit code apropriado, `EndRun` com o código.

`--batch` itera as linhas dentro da mesma transação. Erro em qualquer linha → ROLLBACK total, mensagem nomeando a linha.

`--dry-run` executa até 14 dentro de uma transação que termina em Rollback e imprime os `FieldChange` e as regras avaliadas.

---

## 6. Reconciliação de lacuna

`ProjectedRow` reconstrói o estado que o histórico afirma. Comparado com `SelectRow`, dentro da transação:

- Campo igual → nada.
- Campo divergente → linha `source='reconciled'` com `old_value` do projetado e `new_value` do atual, `changed_at` da detecção. Aviso no stderr.
- Campo sem histórico algum → registro anterior à adoção do Scribo. Ignorado.

`log --at` que atravessa `reconciled` avisa que o estado antes daquele ponto é aproximado. Histórico nunca é apagado nem reescrito.

---

## 7. Valores e afinidade

```
FIELD=           Value{Raw: "",     IsNull: false}
FIELD=null       Value{Raw: "null", IsNull: false}
FIELD=abc        Value{Raw: "abc",  IsNull: false}
FIELD=94.3       Value{Raw: "94.3", IsNull: false}
--null FIELD     Value{IsNull: true}
```

Não existe valor mágico: `null` digitado é texto. NULL real só por `--null`, que pode ser repetido. Nos formatos de `--batch`: TSV e CSV usam célula vazia com `--empty-as-null` explícito, e JSON usa `null` nativo.

**Coerção na escrita.** Coluna `INTEGER`, `REAL` ou `NUMERIC` e valor que parseia como número → vinculado como número. Valor não numérico → `invalid_value`, exit 3.

**Comparação.** `>`, `<`, `>=`, `<=` contra coluna com `Declared=false` ou afinidade textual → `untyped_comparison`, exit 2, sugerindo `--force-text`.

---

## 8. Regras: invariantes entrada × resultado

Arquivo `scribo.rules.toml`, procurado ao lado do banco ou passado por `--rules`. Hash canônico gravado em `tool_runs.config_hash`.

```toml
[[rule]]
table = "rnaseq_qc"
check = "Mapped_Reads <= input.reads"

[[rule]]
table = "rnaseq_qc"
check = "Assigned_Reads <= Mapped_Reads"

[[rule]]
table = "rnaseq_qc"
check = "Alignment_Rate in [0, 100]"
```

Vocabulário conforme `CONTRACT.md` §5. Avaliação sobre o estado do registro **depois** da escrita pretendida, dentro da transação.

- Regra satisfeita → nada.
- Regra violada → Rollback, `invariant_violated`, exit 3, mensagem com os dois números e as entradas envolvidas.
- Regra `input.*` sem entrada sob custódia do Colligo → não avaliada; aviso `rule_unavailable` no stderr, nunca tratada como aprovada. `--require-rules` transforma o aviso em erro.

Toda violação também gera sinal `suspect` em `input_signals`, com `source = invariant:<regra>`, gravado numa transação própria depois do Rollback — a escrita do resultado não acontece, mas o fato de a entrada ter produzido um resultado impossível fica registrado.

---

## 9. Proveniência e obsolescência

### Declaração

`--input PATH` repetível. Cada caminho vira aresta `input_kind='file'` em `result_inputs`:

1. `FileRoot(path)`: se o arquivo está em `files` do Colligo e o `stat` bate com o baseline de verificação, usa `blake3_root` de lá.
2. Senão, calcula a raiz com o extrator de fatos numa leitura única, e avisa que a entrada não está sob custódia.

`--input-result TABLE:KEY` cria aresta `input_kind='result'`, para cadeias entre resultados.

`--input-field FIELD` restringe as arestas seguintes a um campo; sem ele, o registro inteiro depende da entrada.

### Grafo

Nós e arestas conforme `CONTRACT.md` §6. `provenance/graph.go` monta o grafo em memória a partir de `Edges` e `InvalidatedNodes`, e calcula obsolescência por busca reversa a partir dos nós invalidados: O(V + E) por consulta, sem estado persistido.

### `stale`

```
scribo stale --db DB [--table T] [--format tsv|json] [--samplesheet PATH --template nfcore-rnaseq|nfcore-chipseq|nfcore-atacseq]
```

Lista registros obsoletos com o caminho no grafo até o nó invalidado e o motivo. `--samplesheet` escreve a samplesheet nf-core só com as amostras afetadas, a partir dos arquivos de entrada em `files` do Colligo. Sem Colligo presente → `colligo_required`, exit 2.

Código de saída 0 se nada obsoleto, 3 se há obsoletos.

---

## 10. Subcomandos

### Flags globais

```
--db             path do banco                        obrigatório
--table          nome da tabela                       obrigatório onde indicado
--journal-mode   wal|delete                           opcional, default detectado
--busy-timeout   ms                                   opcional, default 5000
--step           rótulo da etapa do pipeline          opcional, gravado em tool_runs
--rules          caminho do scribo.rules.toml         opcional
--quiet          suprime stdout não-essencial
--debug          loga SQL e tempos no stderr          proibido em banco restrito
```

### upsert

```
scribo upsert --db DB --table T [--key K=V]... [--sample S] [FIELD=VALUE]... [--null FIELD]... [--input PATH]... [--input-result T:K]... [--input-field F]
scribo upsert --db DB --table T --batch PATH|- [--format csv|tsv|json] [--empty-as-null]
```

`--sample S` resolve pela tabela `samples` do Colligo e acrescenta a chave `sample=<id>`. `--insert-only`, `--update-only`, `--dry-run`, `--progress`.

No `--batch`, colunas especiais `_sample` e `_input` (lista separada por `;`) fazem o papel das flags por linha.

### log

```
scribo log --db DB --table T --key K=V... [--field F] [--at TIMESTAMP]
```

### query, export

```
scribo query  --db DB --table T [--where COND]... [--fields F1,F2] [--format tsv|json|csv] [--limit N] [--with-staleness]
scribo export --db DB --table T [--where COND]... [--out PATH] [--format tsv|json|csv] [--with-staleness]
```

`--with-staleness` acrescenta a coluna `_stale` calculada pelo grafo.

### schema

```
scribo schema --from arquivo.tsv --table T [--key K]...
scribo schema --from-multiqc multiqc_data.json --table T
```

Imprime o `CREATE TABLE`. Não executa.

### ingest

```
scribo ingest multiqc  --db DB --table T multiqc_data.json [--map-sample colligo|as-is]
scribo ingest somalier --db DB somalier.pairs.tsv [--relatedness-min 0.8] [--relatedness-max 0.1]
scribo ingest ngscheckmate --db DB output_all.txt
```

`multiqc` lê as métricas gerais por amostra e faz upsert na tabela, uma transação para o arquivo inteiro. `--map-sample colligo` resolve nomes de amostra do MultiQC contra `samples`; nome sem correspondência → `unknown_sample`, nada gravado.

`somalier` e `ngscheckmate` comparam o parentesco observado com o declarado: mesma amostra ou réplica técnica declarada e parentesco abaixo de `--relatedness-min` → `identity_mismatch`; amostras declaradas distintas acima do limite → `identity_mismatch` nas duas; declarado e observado coerentes → `identity_confirmed`. Sinais gravados em `input_signals` para as raízes das entradas das amostras.

### diff

```
scribo diff --db DB --table T --run A --run B [--tolerance F=0.01]... [--format tsv|json]
```

Para cada registro e campo com valor nas duas execuções: diferença absoluta e relativa; mudança acima da tolerância é reportada com a causa:

| Causa | Condição |
| ----- | -------- |
| `input` | conjunto de raízes de entrada do registro difere entre A e B |
| `tool` | `tool_version` ou `step` difere entre os `tool_runs` de A e B |
| `none` | entradas e ferramenta idênticas |

Causa `none` é destacada: é não-determinismo. Código de saída 0 se nada acima da tolerância, 3 se há mudança com causa `none`, 0 com relatório nos demais casos. `--fail-on input,tool,none` escolhe o que falha.

### outliers

```
scribo outliers --db DB --table T --field F [--step S] [--group-by COL] [--z 3.5] [--signal]
```

Por campo, dentro de cada grupo: mediana `m`, MAD `d`, e `z = 0.6745 · (x − m) / d`. `|z|` acima do limite é outlier. Grupo com menos de 8 amostras → não avaliado, aviso. `d = 0` → não avaliado, aviso.

`--signal` grava `suspect` em `input_signals` para as entradas de cada outlier, com `source = outlier:<campo>` e o `z` em `detail`. Sem `--signal`, só imprime.

### signals

```
scribo export-signals --db DB [--since TIMESTAMP] [--format json]
```

Exporta sinais, violações de invariante e linhas `reconciled` como casos para o corpus de falhas do Colligo.

---

## 11. Banco restrito

Com `study_meta` restrito, ou `--restricted`:

- `--debug` → erro.
- Permissão `0600` esperada; mais frouxa → erro, `--allow-insecure-permissions` registrado.
- Caminho em pasta sincronizada → erro, `--allow-synced-path`.
- `detail` de sinais e mensagens de erro usam identificadores opacos do Colligo, nunca nome real.
- `export-signals` recusa sem `--allow-restricted-export`.

---

## 12. Exit codes

```
0   sucesso
1   erro de uso
2   erro de banco        tabela ou coluna inexistente, comparação sem tipo, contrato divergente, Colligo necessário
3   erro de dado         valor inválido, amostra desconhecida, invariante violada, resultados obsoletos, diff sem causa
4   erro de arquivo      batch, regras ou entrada não encontrados; formato inválido
5   erro de sistema      disco, permissão, I/O, PRAGMA sem efeito
```

Mensagens no stderr:

```
ERROR [<código_tipado>]: <mensagem principal>
→ <contexto, linha do erro, sugestão>
```

Códigos tipados: `table_not_found`, `column_not_found`, `key_not_found`, `key_already_exists`, `invalid_value`, `untyped_comparison`, `unknown_sample`, `input_not_found`, `invalid_rules`, `invariant_violated`, `rule_unavailable`, `colligo_required`, `contract_mismatch`, `stale_results`, `nondeterministic_change`, `file_not_found`, `invalid_format`, `batch_failed`, `db_busy`, `permission_denied`, `synced_path`, `pragma_ineffective`, `usage`.

---

## 13. Concorrência

Escritas serializam. `BEGIN IMMEDIATE` pega o lock de escrita na entrada da transação; `busy_timeout` controla a espera. Sob alta contenção, spool: cada job escreve arquivo local, um `upsert --batch` consolida. Em filesystem de rede, DELETE no lugar de WAL e spool vira requisito prático.

---

## 14. Testes

Integração do binário completo em subprocesso, SQLite em tmpdir.

Obrigatórios:

- atomicidade unitária e batch: erro no meio não persiste nada; banco byte-idêntico antes e depois
- concorrência: N processos escrevendo o mesmo registro; todo `old_value` do histórico igual ao valor imediatamente anterior na ordem `(changed_at, id)`; `-race`
- histórico e `record_key`: chave independe da ordem das flags; valores com `,`, `=`, `"` e `[` não colidem
- NULL: `FIELD=null` grava texto; `--null FIELD` grava NULL; histórico distingue os dois
- reconciliação: escrita por fora com `sqlite3` gera `reconciled`; `--at` avisa
- afinidade e comparação sem tipo
- detecção de filesystem por tipo simulado
- contrato: `tool_runs` com schema divergente → `contract_mismatch` sem escrita
- regras: `Mapped_Reads > input.reads` recusado com exit 3 e sinal `suspect` gravado; regra `input.*` sem Colligo → `rule_unavailable`
- obsolescência: invalidar um arquivo no fixture do Colligo torna obsoletos exatamente os registros que o alcançam, inclusive por cadeia `result → result`
- `stale --samplesheet` gera samplesheet válida só com as amostras afetadas
- `diff`: fixture com mudança por entrada, por ferramenta e sem causa; cada uma atribuída corretamente
- `outliers`: amostra plantada com `z` alto detectada; grupo pequeno e MAD zero não avaliados
- `ingest multiqc` de um `multiqc_data.json` real de nf-core/rnaseq; `ingest somalier` com par trocado gera `identity_mismatch`
- restrito: `--debug` recusado; permissão frouxa recusada; `detail` sem nome real
- vazamento: sentinela em caminho e valor ausente de `argv_shape`, de `detail` e de todo output
