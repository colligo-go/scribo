# Scribo — Backlog

Features conscientemente adiadas. **Não implementar nada daqui sem pedido explícito.**

Saíram do backlog: a biblioteca compartilhada com o Colligo virou a decisão de núcleo comum da Fatia 0 (`DESIGN.md` §5) e o `CONTRACT.md`; `--file` para registro único foi coberto por `ingest multiqc` no caso mais comum.

Regra: todo item carrega uma frase de "o que destravaria". Sem ela, o item é capricho; com ela, é decisão adiada com gatilho.

---

## Comandos cortados da v1

### `status`

Visão geral: total de registros, contagem por valor de `Status`, NULLs por coluna.

Status: é um `query` com agregação. Não justifica comando próprio, e a contagem de NULL por coluna exige varredura completa a cada chamada.

Destrava quando: a repetição do mesmo `query` virar atrito real.

### `delete`

Remover registro preservando histórico.

Status: em pipeline raramente se deleta amostra — invalida-se com um campo de status, e o histórico segue íntegro. Deletar por `sqlite3` é possível e a Fatia 1 detecta como lacuna reconciliada.

Destrava quando: aparecer caso real de remoção que precise de histórico correto do ato de remover.

### `--file` para registro único

Ler valores de um arquivo de métricas para um único registro.

Status: parsing em bash e passagem por `FIELD=VALUE` resolve. O `--batch` cobre o caso de volume.

Destrava quando: a conversão em bash virar frágil o suficiente para justificar um parser na ferramenta.

---

## `--clear` no upsert

Limpar campos não mencionados, oposto do default de preservar.

Destrava quando: aparecer caso real de "quero reiniciar este registro mantendo só estes campos". O workaround é mencionar todos os campos.

---

## `--where` com OR, parênteses, LIKE, IN

Filtros complexos. Hoje só AND de operadores básicos.

Destrava quando: o caso real exigir. Quem precisa hoje usa `sqlite3` — não estamos no caminho de virar substituto de SQL.

---

## `prune --before`

Apagar histórico anterior a uma data, com confirmação.

Destrava quando: uso real mostrar `_history` virando problema de espaço, na casa de milhões de linhas. Comum só em produção contínua.

---

## `copy` — copiar registros entre bancos

Mover ou clonar registros entre `.db` diferentes.

Destrava quando: aparecer caso real de fusão ou divisão de banco. O workaround é `ATTACH DATABASE` no `sqlite3`.

---

## `validate` — checar integridade de paths no disco

Verificar que os paths gravados ainda existem no filesystem.

Status: sobrepõe o que o Colligo já faz melhor, com hash e grade de chunks. Um path registrado pelo Scribo que aponta para arquivo sob custódia do Colligo já é verificável por lá.

Destrava quando: existir caso de path gravado pelo Scribo fora de qualquer custódia do Colligo, e a podridão de path virar problema medido.

---

## Preditor de recursos a partir do histórico

"Este job vai estourar a memória que pedi?" A partir do histórico de execuções em `tool_runs`.

Status: o Nextflow já faz retry com escalonamento de memória, o que cobre o caso prático sem prever nada. O valor marginal é saber antes em vez de depois de uma falha.

Destrava quando: você não estiver usando orquestrador com retry, e o custo das falhas por OOM virar mensurável.

---

## Store PostgreSQL

Segunda implementação de `db.Backend`.

Status: a abstração existe desde a Fatia 1 exatamente para isto. Diferenças a cobrir: dialeto (`AUTOINCREMENT` contra `SERIAL`, `?` contra `$1`, `ON CONFLICT`, quoting), modelo de tipo (afinidade tolerante do SQLite contra tipagem estrita do Postgres), modelo de concorrência (WAL mais busy_timeout contra MVCC mais statement_timeout), DSN, e testes de integração com container.

Destrava quando: aparecer caso real onde o SQLite não atende — múltiplos processos em hosts diferentes, escala de consórcio, requisito institucional. Não na demanda especulativa.

Nota: em filesystem de rede, o Postgres deixa de ser luxo e vira a resposta certa. Se o padrão spool não resolver o seu HPC, este item sobe de prioridade.

---

## Output estruturado em sucesso

JSON no stdout com o que foi escrito, para pipelines que consomem o resultado da escrita programaticamente.

Destrava quando: caso real de pipeline que precise disso. Hoje sucesso silencioso é feature.

---

## Suporte a Windows

Nada impede (`filepath` em vez de strings), mas não será testado na v1. Atenção ao regime de permissões do banco restrito, que é POSIX.

Decisão atual, igual à do Colligo: desenvolvimento e teste em Linux ou WSL. O binário nativo de Windows compila; banco restrito em Windows nativo é recusado.

Destrava quando: houver usuário real em Windows e CI multiplataforma estável.

---

## Registro de resultados em HPC sem servidor

Uso fora da bioinformática: varreduras de parâmetros e simulações onde MLflow ou W&B são pesados demais, precisam de servidor ou de internet. O Scribo já faz o essencial — upsert por chave, histórico, `diff` entre execuções —; faltaria documentação e exemplos.

Destrava quando: aparecer usuário fora da bioinformática. Não muda código, só README.

---

## Regras além do vocabulário mínimo

Expressões aritméticas em `scribo.rules.toml` (`F <= 0.5 * input.reads`), regras entre registros (réplicas técnicas dentro de uma tolerância), regras condicionais por `role`.

Destrava quando: o vocabulário da v1 deixar de cobrir um caso real repetido. Linguagem de regras cresce fácil demais para crescer especulativamente.

---

## Outliers multivariados

Hoje `outliers` olha um campo por vez. Distância de Mahalanobis robusta (MCD) sobre vários campos pega a amostra que é normal em cada métrica isolada e estranha no conjunto.

Destrava quando: o univariado deixar passar um caso real que o multivariado pegaria. Exige amostras suficientes por grupo para estimar covariância, o que raramente há em estudo pequeno.

---

## Configuração persistente

Arquivo de config para defaults de `--db`, `--busy-timeout`, `--step`.

Destrava quando: a repetição das mesmas flags virar atrito. Hoje variáveis de shell resolvem.
