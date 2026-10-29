# Contrato Colligo ↔ Scribo

Documento normativo compartilhado. Uma cópia idêntica vive em `Colligo/docs/CONTRACT.md` e em `Scribo/docs/CONTRACT.md`. **Divergência entre as duas cópias é bug**, e o CI de cada repositório compara a sua contra a do outro.

Este documento define tudo que as duas ferramentas escrevem ou leem em comum num mesmo banco. Fora daqui, cada uma é dona das próprias tabelas e nenhuma escreve nas tabelas da outra.

Versão do contrato: **1**.

---

## 1. Princípio

Integração por contrato, não por acoplamento de runtime. Um banco tocado pelas duas ferramentas tem uma trilha de execução única, uma identidade de amostra única e um grafo de dependências único — e nenhuma delas precisa estar instalada para a outra funcionar.

Regras:

- Cada tabela tem um dono. Só o dono escreve. A outra ferramenta lê.
- As tabelas deste documento são criadas por quem chegar primeiro, com o DDL exato daqui.
- Na abertura, cada ferramenta confere o schema efetivo das tabelas do contrato (`PRAGMA table_info`) contra o esperado. Divergência → erro `contract_mismatch`, sem escrever nada.
- Mudança neste documento incrementa a versão e exige migration nas duas ferramentas no mesmo ciclo de release.

---

## 2. Abertura do banco

Idêntica nas duas ferramentas, implementada no núcleo comum (`store/open`).

1. `statfs` no diretório do banco. Tipo de rede (`nfs`, `nfs4`, `cifs`, `smb2`, `lustre`, `gpfs`, `fuse.*`, `afs`, `9p`) → journal mode `DELETE`, com aviso no stderr. Local → `WAL`. Override explícito registrado em `tool_runs`.
2. PRAGMAs no DSN, aplicados a toda conexão: `foreign_keys(1)`, `journal_mode(<resolvido>)`, `synchronous(NORMAL)`, `busy_timeout(<valor>)`, e `_txlock=immediate`.
3. Leitura de volta em cada conexão nova. Divergência → `pragma_ineffective`.
4. Conferência das tabelas do contrato (§1).

Nenhuma das duas ferramentas abre um banco compartilhado em `WAL` sobre filesystem de rede. WAL exige memória compartilhada entre processos no mesmo host; em NFS é corrupção silenciosa.

---

## 3. Tabelas do contrato

### `tool_runs` — dono: ambos, uma linha por execução de cada ferramenta

```sql
CREATE TABLE IF NOT EXISTS tool_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    contract_version INTEGER NOT NULL,
    tool             TEXT NOT NULL CHECK (tool IN ('colligo','scribo')),
    tool_version     TEXT NOT NULL,
    git_sha          TEXT NOT NULL,
    go_version       TEXT NOT NULL,
    host             TEXT,
    step             TEXT,
    config_hash      TEXT,
    argv_shape       TEXT NOT NULL,
    started_at       TEXT NOT NULL,
    ended_at         TEXT,
    exit_code        INTEGER
) STRICT;
```

Cada ferramenta só escreve as próprias linhas. `argv_shape` guarda a forma do comando, nunca valores. Timestamps em UTC, RFC 3339 com milissegundo.

### `samples` — dono: Colligo

A tabela `samples` do Colligo é a fonte da identidade de amostra. O Scribo lê: `id`, `accession`, `experiment_id`, `role`, `replicate`. Resolver `--sample X` significa achar a linha cujo `accession` ou `id` é `X`.

### `files` e `facts` — dono: Colligo

O Scribo lê `files.blake3_root`, `files.path` e os fatos de `facts` indexados pela raiz. É assim que um resultado se liga a uma entrada verificada e é assim que as invariantes entrada × resultado (§5) obtêm, por exemplo, a contagem de reads da entrada.

### `result_inputs` — dono: Scribo

Arestas de proveniência: cada valor de resultado aponta para as entradas de que depende.

```sql
CREATE TABLE IF NOT EXISTS result_inputs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id       INTEGER NOT NULL REFERENCES tool_runs(id),
    result_table TEXT NOT NULL,
    record_key   TEXT NOT NULL,
    field        TEXT,
    input_root   TEXT NOT NULL,
    input_kind   TEXT NOT NULL CHECK (input_kind IN ('file','result')),
    recorded_at  TEXT NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS idx_result_inputs_root   ON result_inputs(input_root);
CREATE INDEX IF NOT EXISTS idx_result_inputs_record ON result_inputs(result_table, record_key);
```

`input_kind = 'file'` aponta para uma raiz BLAKE3 de arquivo. `input_kind = 'result'` aponta para outro resultado, codificado como raiz canônica do par `(result_table, record_key)`, o que permite cadeias de dependência entre resultados. `field` nulo significa que o registro inteiro depende da entrada.

### `input_signals` — dono: Scribo; lida pelo Colligo

Sinais que o Scribo observa depois do pipeline e que dizem algo sobre as entradas.

```sql
CREATE TABLE IF NOT EXISTS input_signals (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES tool_runs(id),
    input_root  TEXT NOT NULL,
    signal      TEXT NOT NULL CHECK (signal IN ('suspect','identity_confirmed','identity_mismatch')),
    source      TEXT NOT NULL,
    detail      TEXT NOT NULL,
    created_at  TEXT NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS idx_input_signals_root ON input_signals(input_root);
```

`source` diz de onde veio o sinal: `outlier:<campo>`, `somalier`, `ngscheckmate`, `invariant:<regra>`. `detail` é JSON canônico com os números que justificam o sinal. O Colligo consome estes sinais na custódia (§4); o Scribo nunca altera estado do Colligo diretamente.

---

## 4. O que cada sinal significa para o Colligo

| Sinal | Efeito na custódia do Colligo |
| ----- | ----------------------------- |
| `suspect` | Estado derivado `suspect` somado ao estado de custódia; `advisory`, listado em `status` com o `source` |
| `identity_confirmed` | Afirmação `identity` aprovada; aparece no certificado |
| `identity_mismatch` | Afirmação `identity` reprovada; `blocking` |

O estado original de custódia nunca é sobrescrito. Sinal é afirmação a mais, com fonte declarada, na mesma lógica de declarado contra observado.

---

## 5. Invariantes entrada × resultado

O Scribo avalia regras que ligam um valor de resultado aos fatos das entradas. Vocabulário mínimo da v1:

| Regra | Significado |
| ----- | ----------- |
| `F <= input.reads` | campo F não excede o total de reads das entradas declaradas |
| `F <= input.bases` | idem para bases |
| `F <= G` | campo F não excede o campo G do mesmo registro |
| `F in [a, b]` | campo F no intervalo fechado |

`input.reads` é a soma de `records.reads` dos fatos das entradas `file` do registro, lida da tabela `facts` do Colligo. Sem entrada sob custódia, regras `input.*` não são avaliadas e isso é reportado, nunca assumido como aprovado.

---

## 6. Grafo de dependências e invalidação

Nós: raízes de arquivo, registros de resultado e accessions. Arestas: `result_inputs` (resultado → entrada) e a relação arquivo → amostra → accession do Colligo.

Um resultado está **obsoleto** se algum nó alcançável a partir dele está invalidado: arquivo `stale`, `invalid` ou `superseded`; accession suprimida na fonte; sinal `identity_mismatch`; ou resultado de que ele depende já obsoleto. Obsolescência é alcançabilidade no grafo, calculada sob demanda, nunca gravada como coluna.

---

## 7. Completude de ponta a ponta

`colligo status` mostra o funil: declarado → baixado → verificado → processado. "Processado" significa que cada amostra do estudo tem, na tabela de resultados declarada em `[results]` do TOML do Colligo, os campos esperados preenchidos e não obsoletos. Sem tabela de resultados declarada, o funil termina em "verificado".

---

## 8. Uso real alimentando os dois

- **Prioridade de scrub.** O Colligo ordena o scrub por número de resultados dependentes (contagem em `result_inputs`), depois por `last_verified_at`.
- **Coleta de lixo.** O `gc` do Colligo usa `result_inputs` para distinguir arquivo sem uso, arquivo regenerável e arquivo insubstituível.
- **Corpus de falhas.** Linhas `reconciled`, violações de invariante e sinais `suspect` são exportáveis (`scribo export-signals`) como casos para o `FAILURES.md` do Colligo.
