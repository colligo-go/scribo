# Scribo — Convenções

Estilo da casa. Inviolável durante implementação. Curto de propósito.

---

## Código

**Sem comentários. Nenhum.** Nem em Go, nem em SQL, nem em exemplos. Nomes comunicam intenção. Se um trecho precisa de comentário para ser entendido, ele precisa ser reescrito.

**Erros são valores e são tratados.** Nada de `_` engolindo erro fora de casos genuinamente irrelevantes. Erros de domínio carregam código tipado que vira a tag da mensagem no stderr.

**`context.Context` é o primeiro parâmetro** de toda operação que faz I/O, incluindo as chamadas do `Backend`.

**SQL nunca vive nos handlers de subcomando.** Persistência passa por `db.Backend`, sempre. Regra arquitetural inviolável — preserva a porta para Postgres e mantém os handlers testáveis.

**Validação fora, estado dentro.** Tabela, colunas, coerção de valor, resolução de amostra e de entradas, e carga de regras acontecem fora da transação. Leitura do estado atual e do projetado, avaliação de regras e escrita acontecem dentro de uma transação `BEGIN IMMEDIATE`. `SelectRow` e `ProjectedRow` só existem em `Tx`, para que o erro seja impossível de escrever.

**`Tx` não embute `Backend`.** Transação aninhada não existe em SQLite. A interface estreita torna o erro impossível de escrever.

**Stdout limpo no sucesso de escrita.** `upsert` que dá certo não imprime nada, a menos de `--debug`. Erros no stderr. `query`, `export` e `log` produzem dados no stdout.

**Toda escrita é transação explícita.** Mesmo unitária. Atomicidade total, sem exceção.

**Sem valor mágico.** `null` digitado é texto. NULL real só por `--null` ou `null` nativo de JSON.

**Obsolescência nunca é coluna.** É calculada pelo grafo a cada consulta. Nada de flag `stale` gravada que alguém esquece de atualizar.

**Stdlib primeiro. Dependências conscientes da v1: `modernc.org/sqlite` e `github.com/spf13/cobra`. Qualquer adição exige justificativa.

---

## Banco

**Abertura conforme `CONTRACT.md` §2.** PRAGMAs no DSN, aplicados a toda conexão, com `_txlock=immediate`; lidos de volta; journal mode resolvido pelo filesystem; tabelas do contrato conferidas.

**Journal mode é detectado, não assumido.** Filesystem de rede nunca abre em WAL. Ver SPEC §3.

**O Scribo cria só as tabelas do contrato de que é dono e `{table}_history`.** A tabela de dados nunca é criada nem alterada estruturalmente. `schema` gera DDL para o stdout; não executa.

**Nunca escrever em tabela do Colligo.** `samples`, `files` e `facts` são só leitura. O que o Scribo tem a dizer sobre uma entrada vai para `input_signals`.

**`CONTRACT.md` é idêntico nos dois repositórios.** Mudança nele é mudança nos dois, no mesmo ciclo, com `contract_version` incrementada.

**`tool_runs` segue o DDL do `CONTRACT.md`**, distinguida por `tool`. Criar se não existir com aquele DDL exato; se existir com schema diferente, `contract_mismatch`.

**Nunca gravar valor de argumento em `argv_shape`.** Só a forma do comando. É o que impede vazamento por construção.

**Histórico nunca é apagado nem reescrito.** Lacuna detectada vira linha `reconciled`, não correção retroativa.

**Timestamps em UTC, RFC 3339 com milissegundo.**

**`record_key` é array JSON canônico** de pares ordenados pelo nome do campo. Nunca concatenação de strings. Ordenação cronológica é sempre `(changed_at, id)`.

**Tabelas `STRICT`.**

---

## CLI

**`--help` é documentação primária.** Cada subcomando tem `--help` suficiente para uso sem consulta externa, com exemplos concretos.

**Exit codes são contratuais.** O conjunto do SPEC §10 é estável. Adicionar significa pensar duas vezes e atualizar o doc.

**Falhar fechado onde a ambiguidade produz número errado.** Comparação numérica contra coluna sem tipo declarado é erro, não aviso.

**Flags em kebab-case, longo descritivo.** Atalhos de letra única só para as realmente frequentes.

---

## Testes

**Integração do binário completo.** Rodar o binário em subprocesso e verificar banco, stdout, stderr e exit code. Unit tests só para lógica isolada complexa: parsing de valor, coerção por afinidade, construção de `record_key`.

**SQLite em tmpdir do teste**, nunca fora dele. Sem testes que toquem a rede.

**Sentinela de vazamento.** Valor único e improvável como caminho e como valor de campo, com grep em todo output produzido, incluindo `argv_shape` e o banco.

**Teste escrito junto com o código.** `-race -count=2` obrigatório.

---

## Documentação

**GoDoc em `internal/db/`.** Mesmo sendo `internal`, mantenedores precisam entender a interface `Backend` que sustenta a portabilidade. É o único lugar onde texto explicativo é permitido no código.

**Decisão muda código e doc no mesmo commit.** SPEC e implementação não desincronizam.

**Onde cada coisa mora:** por quê em `DESIGN.md`, o quê em `SPEC.md`, o comum com o Colligo em `CONTRACT.md`, o que já existe fora em `RELATED.md`, o que vem depois em `ROADMAP.md`, o que foi adiado em `BACKLOG.md`.

---

## Git

**`CLAUDE.md` é privado** (`.gitignore`). Não vai para o repositório.

**`.gitignore` obrigatório:** `*.db`, `*.db-wal`, `*.db-shm`, `*.local.toml`.

**Commits pequenos, por fatia ou sub-fatia.** Mensagem descreve o que mudou e por quê em uma linha.
