# Scribo — Design e Decisões

O propósito deste documento é registrar **por que** o Scribo é como é. Para o "como implementar", ver `SPEC.md`. Para o que é comum com o Colligo, `CONTRACT.md`. Para o que já existe fora daqui, `RELATED.md`. Para "o que foi adiado", `BACKLOG.md`.

---

## 1. Visão

Scribo é o livro-razão de resultados de pipeline: grava valores por amostra, sabe de onde cada número veio, e sabe quando ele deixou de valer. O nome vem do latim *scribere* — "eu escrevo, eu registro".

Gravar valores em SQLite com histórico é problema resolvido (ver §2). O que nenhuma ferramenta faz é a pergunta seguinte:

> Este `Alignment_Rate = 94.3` ainda é verdade? De qual BAM veio, derivado de quais FASTQs, com qual versão da ferramenta — e alguma dessas coisas mudou desde então?

O Scribo responde isso junto com o Colligo. O Colligo garante as entradas antes do pipeline; o Scribo observa os resultados depois; e cada lado corrige o outro.

**Faz:** grava valores por registro de forma atômica; mantém histórico por campo e reconstrói o estado em qualquer data; liga cada valor às entradas de que depende; recusa resultado que contradiz a entrada; lista resultados obsoletos quando uma entrada é invalidada; compara resultados entre execuções e diz por que mudaram; detecta amostras fora do padrão e devolve ao Colligo como sinal sobre a entrada; importa métricas do MultiQC e identidade genética do somalier.

**Não faz:** cria schema de tabela de dados; analisa dado; orquestra pipeline; adquire dado externo; alinha reads.

---

## 2. O que já existe, e o que sobra

| Ferramenta | Faz | Não faz |
| ---------- | --- | ------- |
| sqlite-utils | insert, upsert, query, export a partir de CSV/TSV/JSON | histórico, proveniência |
| sqlite-history | histórico por trigger com versão, timestamp e máscara de colunas alteradas | CLI de reconstrução por data; proveniência; binário sem Python |
| Dolt | banco SQL versionado, `AS OF` e `dolt_history_<tabela>` nativos, binário Go | não é SQLite; não conhece entradas verificadas nem invalidação |
| Nextflow data lineage | registra caminho, tamanho e checksum dos outputs de um workflow | não invalida resultado quando a fonte muda depois; não guarda métrica por amostra |
| MultiQC | agrega métricas de QC por amostra num relatório | histórico entre execuções, proveniência, invalidação |

Histórico sozinho está saturado: um Scribo que só faz isso não tem motivo para existir. O que sobra, e ninguém cobre, é a combinação com o Colligo: **proveniência por valor, invalidação por alcançabilidade, invariantes entrada × resultado e diff com causa.**

A Fatia 0 testa exatamente isso, com a pergunta reformulada: "alguma ferramenta liga cada resultado às entradas verificadas e o invalida quando elas mudam?". Se a resposta for sim, o projeto encerra — e é o melhor resultado possível.

---

## 3. Princípios

**Atomicidade total.** Erro em qualquer passo, nada é persistido. Vale para escrita unitária e para batch de mil registros.

**Leitura e escrita na mesma transação.** O estado atual lido para calcular o histórico é o estado que a escrita altera. Ver §4.

**Declarado contra observado, também no resultado.** O mesmo princípio do Colligo: um resultado que contradiz os fatos da entrada é recusado, não gravado com aviso.

**Erro claro, sempre.** Stderr com mensagem descritiva e exit code específico.

**Genérico por design, especializado por contrato.** O Scribo funciona sem o Colligo, como livro-razão com histórico. Com o Colligo presente no mesmo banco, ganha identidade de amostra, proveniência e invalidação.

**Binário único.** Zero dependência de runtime.

**Stdout limpo no sucesso de escrita.**

---

## 4. Decisões cravadas e seus porquês

### Leitura dentro da transação, com `BEGIN IMMEDIATE`

A versão anterior validava e lia o estado atual fora da transação, para encurtar a janela de lock. Isso está certo para a validação de schema e errado para o estado: se outro job escreve entre a leitura e o `BEGIN`, o `old_value` gravado no histórico fica errado e a reconciliação acusa lacuna que não existe.

Decisão: validação de tabela, colunas e coerção fora da transação. `SelectRow`, `ProjectedRow`, `Upsert` e `AppendHistory` dentro de uma transação `BEGIN IMMEDIATE`. Com `BEGIN` comum (deferred) em WAL, a promoção de leitura para escrita pode falhar com `SQLITE_BUSY` sem que o `busy_timeout` ajude; `IMMEDIATE` pega o lock de escrita na entrada e espera pelo `busy_timeout` normalmente.

### Journal mode depende do filesystem, e é detectado

Em HPC, scratch compartilhado é quase sempre NFS, Lustre ou GPFS, e WAL sobre filesystem de rede é corrupção silenciosa. `statfs` na abertura: local → WAL; rede → DELETE com aviso. A regra vale igual no Colligo, pelo `CONTRACT.md` §2 — um banco compartilhado protegido por uma ferramenta e corrompido pela outra não está protegido.

### Spool: o valor é o merge atômico com histórico

O padrão recomendado para HPC é spool: cada job escreve um arquivo local e um `upsert --batch` consolida no fim. Isso parece o "TSV + script de atualização" que o Scribo substitui, e é — no formato intermediário. A diferença está no merge: atômico, com histórico por campo, proveniência e invariantes checadas. O arquivo intermediário não é o problema; o merge sem garantias é.

### `tool_runs` é contrato, não convenção

A versão anterior dizia que a `tool_runs` era compartilhada com o Colligo, mas os dois schemas divergiam, e com `CREATE TABLE IF NOT EXISTS` quem cria primeiro quebra o outro. Agora o schema está em `CONTRACT.md`, idêntico nos dois repositórios, com `contract_version` e conferência na abertura.

### `run_id` no lugar de `command`

Gravar o comando expandido vaza caminho, token e identificador de paciente. O histórico aponta para `tool_runs`, que guarda só a forma do comando.

### NULL tem sintaxe própria

O shell remove aspas antes de o programa ver o argumento: `FIELD="null"` e `FIELD=null` chegam iguais. Distinção por aspas é impossível. Decisão: `FIELD=null` é o texto `null`; NULL real é `--null FIELD`. Sem mágica em valor.

### `record_key` é injetivo

`k=v,k2=v2` colide quando um valor contém `,` ou `=`: registros diferentes, mesma chave de histórico. A chave canônica passa a ser um array JSON de pares ordenados pelo nome do campo, `[["accession","ENCFF001"],["replicate","1"]]`, serializado sem espaços. Injetivo por construção.

### Proveniência por valor

`upsert --input arquivo` grava arestas em `result_inputs`: o registro, ou o campo, depende da raiz BLAKE3 daquele arquivo. Se o arquivo está sob custódia do Colligo, a raiz vem de `files`; se não está, o Scribo calcula com o extrator de fatos do núcleo comum, na mesma leitura única. Nenhum valor precisa de proveniência, mas quem a declara ganha invalidação e invariantes.

### Obsolescência é alcançabilidade

Resultados, arquivos e accessions formam um grafo de dependências. Um resultado está obsoleto se alcança algum nó invalidado: arquivo `stale` ou `superseded`, run suprimido, identidade reprovada, ou outro resultado obsoleto. É fecho transitivo sobre o grafo, calculado sob demanda; nunca uma coluna `stale` que alguém esquece de atualizar. `scribo stale` lista; `--samplesheet` gera a samplesheet nf-core só com as amostras afetadas — detectar, medir o impacto, reprocessar só o necessário.

### Invariantes entrada × resultado

Um resultado não pode contradizer a entrada: reads mapeados ≤ reads do FASTQ; reads atribuídos a genes ≤ mapeados; taxa em [0, 1]. Regras declaradas em `scribo.rules.toml`, versionado com a análise, com hash registrado em `tool_runs.config_hash`. Violação → recusa, exit 3. É o que pega "arquivo errado no lugar certo": 12 milhões de reads mapeados numa amostra cujo FASTQ tem 10 milhões. Só é possível porque o Colligo já contou os reads.

### Diff com causa

`scribo diff --run A --run B` compara campo a campo, com tolerância por campo, e atribui a causa de cada mudança:

- **entrada:** alguma raiz de entrada do registro difere entre as execuções;
- **ferramenta:** `tool_version` ou `step` difere em `tool_runs`;
- **sem causa aparente:** entradas e versões idênticas, resultado diferente — não-determinismo, o alerta que quase ninguém dá.

É o "diff semântico" que o Colligo registrou como ideia de maior valor científico, com o Scribo como substrato natural.

### Desvio entre amostras vira sinal sobre a entrada

O Colligo não alinha reads, por decisão. Mas o pipeline alinha, e o Scribo grava o resultado. Uma amostra com taxa de alinhamento de 5% entre amostras com 90% sugere organismo trocado ou arquivo errado.

`scribo outliers --field F --step S` usa z-score robusto — mediana e MAD por campo e etapa, porque média e desvio padrão são arrastados pelo próprio outlier — e grava sinal `suspect` em `input_signals` para as entradas da amostra quando `|z|` passa do limite. O Colligo consome o sinal na custódia. Resolve "organismo declarado × observado" sem o Colligo cruzar a fronteira da análise.

### Identidade confirmada por genótipo

`scribo ingest somalier` (e `ngscheckmate`) lê a saída dessas ferramentas e grava, por par de amostras, se batem geneticamente. Amostras declaradas como a mesma pessoa que não batem, ou declaradas diferentes que batem, viram `identity_mismatch`; as que batem como declarado, `identity_confirmed`. É a "identidade de amostra propagada" do backlog do Colligo, fechada com ferramentas que já existem.

### MultiQC é a porta de entrada

`scribo ingest multiqc multiqc_data.json` importa as métricas gerais por amostra para a tabela declarada. Todo mundo que roda nf-core já tem esse arquivo: o Scribo tem uso no primeiro minuto, sem mudar o pipeline. A tabela precisa existir; `schema --from-multiqc` gera o DDL para revisão.

### Afinidade de tipo é respeitada, e comparação sem tipo é erro

Comparação numérica contra coluna sem tipo declarado é lexicográfica e silenciosa. Na escrita, valor numérico em coluna numérica é vinculado como número. No `--where`, `>`, `<`, `>=`, `<=` contra coluna sem tipo → erro `untyped_comparison`.

### O histórico pode ter lacuna, e a lacuna é detectada

Escrita por `sqlite3` ou por qualquer outra via cria lacuna. Dentro da transação, o estado atual é comparado com o projetado pelo histórico; divergência vira linha `reconciled`, e o `log --at` que a atravessa avisa. Histórico nunca é apagado nem reescrito.

### Classificação é consumida, não reimplementada

Com `study_meta` do Colligo marcando o estudo como restrito: `--debug` recusado, permissão conferida, pasta sincronizada recusada.

### `upsert` preserva campos não mencionados

Campos não mencionados ficam como estavam.

### Timestamp com milissegundo, `id` como desempate

Ordenação cronológica sempre `(changed_at, id)`.

### `schema` gera, não cria

`scribo schema --from arquivo.tsv` e `--from-multiqc` **imprimem** o `CREATE TABLE`. O Scribo nunca cria tabela de dados.

### Filtros do `--where`: operadores básicos, não SQL

`=`, `!=`, `>`, `<`, `>=`, `<=`, `field=null`, `field!=null`. AND implícito. Quem precisa de mais usa `sqlite3`.

### JSON é JSONL

`query`, `export` e `diff` produzem TSV por default; `--format json` gera um objeto por linha.

### Abstração de backend permanece

Toda persistência passa por `db.Backend`. Uma implementação na v1. Handlers nunca chamam SQL direto.

### Infraestrutura

Go estável mais recente. MIT. cobra. v0 até a interface estabilizar.

---

## 5. Relação com o Colligo

### Um módulo, dois binários — decisão proposta

Com proveniência, invariantes e invalidação, o Scribo passa a precisar do extrator de fatos, do contrato de `tool_runs` e da abertura de banco do Colligo. A duplicação que "ainda não tinha disparado" dispara aqui.

Opções:

1. **Um módulo, dois binários.** `colligo` e `scribo` importam um núcleo comum: `facts`, `runs`, `store/open`. Mantém o nome e a ferramenta genérica para quem não usa o Colligo.
2. **Um binário.** O Scribo vira o lado de resultados: `colligo record`, `colligo stale`, `colligo diff`.

Proposta: **opção 1**. A decisão final é parte da Fatia 0. Até lá, o `CONTRACT.md` garante que as duas ferramentas conversam sem compartilhar código.

### O que o Scribo dá ao Colligo

| Contribuição | Mecanismo | Item do Colligo que destrava |
| ------------ | --------- | ----------------------------- |
| Sinal de entrada suspeita | `outliers` → `input_signals` | organismo declarado × observado |
| Identidade confirmada | `ingest somalier` → `input_signals` | identidade de amostra propagada |
| Plano de reprocessamento | `stale --samplesheet` | fecha o ciclo de `recheck --upstream` |
| Completude de ponta a ponta | resultados esperados em `[results]` | `status` com o funil completo |
| Prioridade de scrub | contagem em `result_inputs` | scrub do que mais importa primeiro |
| Coleta de lixo com proveniência | `result_inputs` como histórico de uso | `gc` que sabe o que é regenerável |
| Corpus de falhas reais | `export-signals` | `FAILURES.md` e benchmark |

### O que o Colligo dá ao Scribo

Identidade de amostra (`samples`), raízes verificadas (`files`), fatos para invariantes (`facts`), status de accession na fonte, classificação do estudo.

---

## 6. Limites declarados

- Não cria schema de tabela de dados.
- Não substitui SQL.
- Histórico é de aplicação, não de trigger. Escrita fora do Scribo é detectada, não impedida.
- Escrita no SQLite serializa.
- Não orquestra nem supervisiona execução.
- Não alinha reads nem interpreta biologia. Outliers e identidade genética vêm de métricas que o pipeline e ferramentas externas produziram; o Scribo só os compara e sinaliza.
- Sem Colligo no banco, não há proveniência verificada, invariantes `input.*` nem invalidação por fonte. O Scribo avisa quando uma funcionalidade não está disponível por falta dele, nunca finge.
