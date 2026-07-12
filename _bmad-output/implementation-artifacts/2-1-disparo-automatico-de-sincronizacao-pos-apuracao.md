---
baseline_commit: f13b4a78132afcdcdcf0e955dd8064f82fe320be
---

# Story 2.1: Disparo Automático de Sincronização Pós-Apuração

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a sistema,
I want disparar automaticamente a sincronização de pagamentos SAP quando uma apuração RFB concluir,
so that Ana nunca precise solicitar isso manualmente.

Realiza FR-1 e parte de FR-7/FR-10 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Primeira story da Epic 2 — ver `_bmad-output/planning-artifacts/epics.md`. Depende da Epic 1 (Stories 1.1/1.2, já `done`) para existir `sap_credentials` com `bukrs_list` por empresa. Esta story cobre **apenas** o gatilho, o registro de execução (`sap_sync_runs`) e a idempotência — a chamada real ao SAP (`SearchPayments`, lotes, retry) é escopo da Story 2.2, ainda não implementada.

## Acceptance Criteria

1. **Given** uma apuração RFB muda para `completed` e grava novos `rfb_creditos`, **When** o processamento termina, **Then** o sistema extrai as chaves NF-e/CT-e (`chave_dfe`) desses créditos e dispara a sincronização SAP de forma assíncrona (goroutine), sem bloquear a resposta do webhook RFB.
2. **Given** uma apuração concluída sem nenhum crédito novo (ou sem nenhuma empresa/BUKRS habilitado para SAP), **When** isso ocorre, **Then** nenhuma chamada/registro de sincronização SAP é disparado.
3. **Given** a sincronização SAP falha (por qualquer motivo, incluindo erros ainda não tratados nesta story), **When** isso ocorre, **Then** a apuração RFB em si não é afetada — a falha fica isolada (nunca chama `updateRequestError` em `rfb_requests`, nunca propaga erro para o chamador do processamento RFB).
4. **Given** a migration desta story roda, **When** aplicada, **Then** a tabela `sap_sync_runs` é criada com os campos mínimos necessários para rastrear execuções (`company_id`, `bukrs`, chaves do lote, `status`, `iniciado_em`, `concluido_em`) — campos adicionais de contagem detalhada por status **não** são adicionados aqui (ficam para a Story 2.4).
5. **Given** o webhook RFB é reentregue para a mesma apuração (ou o processamento é re-executado por qualquer caminho — reprocessamento manual, race de status), **When** o disparo seria repetido, **Then** o sistema verifica em `sap_sync_runs` se já existe execução em andamento/concluída para aquele conjunto de chaves (`company_id` + `bukrs` + mesmo `request_id`/conjunto de chaves) e não dispara de novo.
6. **And** apenas os BUKRS configurados (`sap_credentials.bukrs_list`, Epic 1) para aquele `company_id` são considerados — nenhuma chamada é feita para BUKRS fora do escopo da empresa (isolamento multi-tenant, FR-10).

## Tasks / Subtasks

- [x] **Task 1 — Migration `sap_sync_runs`** (AC: #4)
  - [x] Criar `backend/migrations/116_sap_sync_runs.sql`
  - [x] Tabela `sap_sync_runs`, uma linha por `(company_id, bukrs)` por tentativa de sincronização — modelada como `erp_bridge_runs`, NÃO como `sap_credentials`
  - [x] Colunas mínimas: `id UUID PK`, `company_id`, `bukrs VARCHAR(4)`, `request_id UUID REFERENCES rfb_requests(id) ON DELETE SET NULL`, `chaves_enviadas TEXT[]` (array de chaves — forma mais simples que sustenta a checagem de idempotência sem introduzir tabela extra), `status VARCHAR(20)` (`em_andamento`/`concluido`/`falha`), `iniciado_em`, `concluido_em`
  - [x] **Não adicionadas** as colunas de contagem detalhada — reservadas para a Story 2.4
  - [x] Índice em `(company_id, bukrs, request_id)` criado

- [x] **Task 2 — Extração das chaves recém-gravadas e gating por empresa/BUKRS** (AC: #2, #6)
  - [x] `TriggerSAPSync(db *sql.DB, requestID string)` criada em `backend/services/sap_sync_trigger.go`, chamada via `go TriggerSAPSync(db, requestID)` nos 4 pontos de conclusão (`rfb_processor.go:334/649`, `rfb_creditos_processor.go:326/472` — após a inserção do `go` correspondente)
  - [x] `company_id` resolvido via `SELECT company_id FROM rfb_requests WHERE id = $1`
  - [x] Gating por `sap_credentials.ativo`/`bukrs_list` implementado — `sql.ErrNoRows`, `!ativo` ou lista vazia encerram sem nenhum registro
  - [x] Gating por créditos novos implementado — `fetchChavesRecemGravadas` retorna vazio → encerra sem registro
  - [x] Loop por BUKRS com captura de cópia local (`b := bukrs`) antes de `syncBukrs`

- [x] **Task 3 — Idempotência via `sap_sync_runs`** (AC: #5)
  - [x] Checagem por `(company_id, bukrs, request_id, status IN ('em_andamento','concluido'))` antes de inserir nova linha — implementada em `syncBukrs`
  - [x] Inserção com `status = 'em_andamento'` seguida de atualização para `concluido` (nenhuma chamada real ao SAP nesta story — ver escopo)

- [x] **Task 4 — Isolamento de falhas** (AC: #3)
  - [x] Toda a lógica roda na goroutine disparada; erros apenas logados (`[SAP Sync] ...`), nunca propagados, nunca chamam `updateRequestError`
  - [x] `defer recover()` adicionado no topo de `TriggerSAPSync` — precedente novo no projeto (nenhuma goroutine existente tinha `recover()`), justificado porque esta goroutine roda fora do ciclo de vida de uma request HTTP e um panic aqui derrubaria o processo inteiro

- [x] **Task 5 — Testes e verificação**
  - [x] Testes de integração contra Postgres real em `backend/services/sap_sync_trigger_test.go` (sem harness de mock de banco no projeto — mesmo padrão raw-SQL usado em produção): sem `sap_credentials` → 0 registros; sem créditos novos → 0 registros; caminho feliz com 2 BUKRS → 2 registros, status `concluido`; idempotência (2 disparos, mesmo `requestID`) → 1 registro; `requestID` inexistente → nenhum erro propagado
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões (todos os 5 testes novos + suíte completa do projeto)
  - [x] Verificação manual: migration `116` aplicada em `fiscal_db` real via `psql`; testes de integração confirmam via consulta direta a `sap_sync_runs` (não apenas mock) o comportamento de todos os cenários do AC #2/#4/#5/#6; dados de teste (environment/enterprise_group/company temporários) removidos ao final via cascade delete

### Review Findings

*(Revisão adversarial em 3 camadas — Blind Hunter, Edge Case Hunter, Acceptance Auditor — sobre o diff isolado desta story)*

- [x] [Review][Decision] Testes de integração fazem skip silencioso quando o Postgres está indisponível — os workflows de CI existentes (`deploy-staging.yml`/`deploy-production.yml`) rodam `go test ./...` sem nenhum serviço Postgres provisionado, então os 5 testes novos de `sap_sync_trigger_test.go` nunca executam de fato em CI hoje, e o pipeline reporta sucesso mesmo assim — **resolvido: aceitar por ora (opção 1)**. Razão: consistente com a política já aceita nas Stories 1.1/1.2 (verificação manual/local é suficiente por ora, sem harness de CI com banco); adicionar Postgres ao CI é uma mudança de infraestrutura nos workflows de deploy, fora do escopo natural desta story.

- [x] [Review][Patch] Condição de corrida SELECT-then-INSERT quebra a idempotência exigida pelo AC #5 sob concorrência real [backend/services/sap_sync_trigger.go, backend/migrations/116_sap_sync_runs.sql] — corrigido: adicionada constraint `UNIQUE(company_id, bukrs, request_id)` na migration + troca do `SELECT`-then-`INSERT` por `INSERT ... ON CONFLICT (company_id, bukrs, request_id) DO NOTHING RETURNING id`; validado com novo teste `TestSyncBukrs_ConcorrenciaReal_NaoDuplicaViaConstraintUnique` (10 goroutines reais concorrentes, 10 rodadas — sempre 1 execução)
- [x] [Review][Patch] `chave_dfe` é nullable no schema mas `fetchChavesRecemGravadas` fazia `Scan` em `string` não-nullable — corrigido: `Scan` agora em `sql.NullString`, ignorando linhas com `chave_dfe` NULL/vazio em vez de abortar o fetch inteiro; validado com novo teste `TestTriggerSAPSync_ChaveDFeNula_NaoAbortaFetch` [backend/services/sap_sync_trigger.go]
- [x] [Review][Patch] `recover()` no topo de `TriggerSAPSync` englobava o loop inteiro por BUKRS — corrigido: extraída `syncBukrsSafe` com `recover()` próprio por BUKRS, chamada individualmente dentro do loop; um panic em 1 BUKRS não impede mais o processamento dos demais [backend/services/sap_sync_trigger.go]
- [x] [Review][Patch] `TestTriggerSAPSync_RequestIDInexistente_NaoPropagaErro` não afirmava nada além de "não houve panic" — corrigido: teste agora confere explicitamente que 0 linhas foram criadas em `sap_sync_runs` [backend/services/sap_sync_trigger_test.go]

- [x] [Review][Defer] Estado `falha` documentado na migration mas nunca gravado pelo código — se o `UPDATE` final para `concluido` falhar, a execução fica presa em `em_andamento` para sempre, sem mecanismo de reconciliação/timeout [backend/services/sap_sync_trigger.go] — deferred, baixa probabilidade (exigiria falha do Postgres entre o INSERT e o UPDATE); revisitar na Story 2.4 (histórico/monitoramento)
- [x] [Review][Defer] Goroutine fire-and-forget sem `WaitGroup` nem hook de graceful shutdown [backend/services/sap_sync_trigger.go, 4 pontos de chamada] — deferred, padrão pré-existente no projeto (mesmo formato em `rfb.go`/`rfb_scheduler.go`), não introduzido por esta story
- [x] [Review][Defer] Sem limite de concorrência (worker pool/semáforo) para as goroutines disparadas — deferred, mesmo padrão pré-existente em `rfb_scheduler.go` (loop de goroutines por empresa sem limite)
- [x] [Review][Defer] Sem `context.Context`/timeout nas queries de `sap_sync_trigger.go` — deferred, padrão ausente em todo o pacote `services` hoje, não introduzido por esta story
- [x] [Review][Defer] BUKRS com mais de 4 caracteres causa falha silenciosa de INSERT (truncamento de `VARCHAR(4)`), isolada por BUKRS mas sem sinalização visível — deferred, causa raiz é ausência de validação de formato em `sap_credentials` (Epic 1, Story 1.1, já `done`), não desta story
- [x] [Review][Defer] Idempotência não reconsidera se o "conjunto de chaves" mudar para o mesmo `request_id` (reprocessamento) — deferred, teoricamente possível pela letra do AC #5, mas impraticável hoje porque `ReprocessarRawJSON`/`ReprocessarRawJSONCreditosRFB` derivam de `raw_json` fixo e determinístico
- [x] [Review][Defer] `status = 'concluido'` gravado sem nenhuma sincronização real ao SAP ter ocorrido (escopo aceito desta story) — deferred, mas registros "concluído" desta story ficarão indistinguíveis de sincronizações reais quando a Story 2.2 existir; revisitar a semântica desse campo antes da Story 2.4 expor histórico em tela
- [x] [Review][Defer] Sem transação entre `INSERT` e `UPDATE` em `syncBukrs` (janela intermediária com status `em_andamento` visível) — deferred, decorre do design "concluir imediatamente" desta story; será resolvido naturalmente quando a Story 2.2 substituir por lógica real de lote com timing real
- [x] [Review][Defer] Nenhum teste cobre explicitamente `ativo = false` nem BUKRS duplicado na lista de configuração — deferred, cobertura incremental de baixo valor; BUKRS duplicado fica mitigado indiretamente pelo patch de idempotência (`ON CONFLICT`)
- [x] [Review][Defer] Nome de fixture de teste fixo (`teste-sap-sync-2.1`) pode colidir se um teste anterior falhar antes do `defer cleanup()` rodar — deferred, baixo impacto (ambiente de dev/teste local, não produção)

## Dev Notes

### Fluxo atual de conclusão da apuração RFB — 4 pontos de gatilho necessários

A pesquisa de contexto identificou que **não existe hoje um único ponto de "apuração concluída"** — há 4 funções que chamam `updateRequestStatus(db, requestID, "completed")` de forma independente, todas em `backend/services/`:

- `ProcessarDownloadRFB` — `rfb_processor.go:334`
- `ReprocessarRawJSON` — `rfb_processor.go:645`
- `ProcessarDownloadCreditosRFB` — `rfb_creditos_processor.go:326`
- `ReprocessarRawJSONCreditosRFB` — `rfb_creditos_processor.go:472`

Todas as quatro inserem/atualizam `rfb_creditos` antes de chegar nesse ponto. O disparo da Story 2.1 precisa ser adicionado logo após a chamada a `updateRequestStatus(..., "completed")` em **todas as quatro**, ou centralizado dentro do próprio `updateRequestStatus` (`rfb_processor.go:658`) com um `if status == "completed"` — mas atenção: `updateRequestStatus` é usado também para outras transições de status (não seria seguro assumir que toda chamada é "concluído com sucesso" sem esse if explícito).

`updateRequestStatus` e `updateRequestError` (linha 667) são os dois helpers compartilhados hoje — nenhum dos dois tem qualquer hook/callback existente; será a primeira vez que um efeito colateral é anexado a essa transição de status.

### Como identificar "créditos recém-gravados desta apuração" — limitação real do schema atual

`rfb_creditos.chave_dfe` é único por `(company_id, chave_dfe)` (índice parcial, migration 096). `insertCredito` (`rfb_creditos_processor.go:478`) faz `INSERT ... ON CONFLICT (company_id, chave_dfe) DO UPDATE SET request_id = EXCLUDED.request_id, ...` — ou seja, **toda reimportação de uma chave já existente sobrescreve `request_id` para o request mais recente**. Não há flag separada distinguindo "linha nova" de "linha atualizada".

Na prática, `SELECT chave_dfe FROM rfb_creditos WHERE request_id = $1` (usando o `requestID` que acabou de ser processado) é a única forma disponível no schema atual de obter "as chaves tocadas por esta apuração" — isso inclui tanto créditos genuinamente novos quanto créditos pré-existentes que foram apenas re-tocados pela mesma execução. O AC #1 fala em "grava novos rfb_creditos", mas o schema não separa isso de forma limpa. **Decisão de escopo para esta story:** tratar "chaves com `request_id` = o request recém-processado" como a definição operacional de "créditos recém-gravados" — é o comportamento correto para o propósito de sincronização (mesmo uma chave re-tocada pode precisar de sync SAP atualizado) e evita introduzir uma migration adicional só para rastrear new-vs-update (fora de escopo desta story). Se precisar de precisão adicional no futuro, a técnica Postgres `xmax = 0` dentro do `insertCredito` permitiria distinguir INSERT de UPDATE, mas isso é uma mudança em código já existente e testado (Story 1.1/1.2 já não tocaram nele) — não fazer nesta story sem necessidade comprovada.

### Padrão de disparo assíncrono a copiar

Precedente mais próximo, em `backend/services/rfb.go:121-128` (`SolicitarApuracaoParaEmpresa`) — goroutine fire-and-forget, falha isolada e apenas logada, nunca propagada:

```go
go func() {
    if credErr := SolicitarCreditoParaEmpresa(db, companyID); credErr != nil {
        log.Printf("[RFB Scheduler] AVISO: falha ao solicitar créditos CBS para company_id=%s: %v", companyID, credErr)
    } else {
        log.Printf("[RFB Scheduler] Créditos CBS solicitados com sucesso para company_id=%s", companyID)
    }
}()
```

Para o loop por BUKRS (Task 2), seguir o idioma já usado em `rfb_scheduler.go:269-278` — capturar a variável de loop em cópia local antes de qualquer goroutine aninhada:

```go
for _, bukrs := range bukrsList {
    b := bukrs
    // ... usar b, não bukrs, dentro de qualquer closure
}
```

Não há precedente de `recover()` em goroutines no projeto — avaliar se vale introduzir um `defer func() { if r := recover(); ... }()` na goroutine de disparo desta story (uma vez que ela roda fora do ciclo de vida da request HTTP e um panic não tratado aqui derruba o processo inteiro, diferente de um panic dentro de um handler HTTP que o `net/http` já isola por conexão).

### `sap_credentials` — padrão de leitura para o gating (Epic 1, já implementado)

```go
var bukrsList []string
var ativo bool
err := db.QueryRow(`SELECT ativo, bukrs_list FROM sap_credentials WHERE company_id = $1`, companyID).
    Scan(&ativo, pq.Array(&bukrsList))
// err == sql.ErrNoRows → empresa sem SAP habilitado → encerrar (AC #2, #6)
// !ativo ou len(bukrsList) == 0 → encerrar (AC #2, #6)
```
`client_secret`/`client_id` **não** são necessários nesta story (não há chamada real ao SAP ainda — isso é escopo da Story 2.2).

### Migration — próximo número livre e modelo de referência

Maior migration existente: `115_sap_credentials.sql` → **próximo número livre é `116`**.

Modelar `sap_sync_runs` como `erp_bridge_runs` (`backend/migrations/074_erp_bridge.sql`), não como `sap_credentials`: uma linha por tentativa de sincronização por `(company_id, bukrs)`, não uma linha fixa por empresa. Nenhuma tabela ou código `sap_sync*` existe ainda no repositório (confirmado via `grep -rl "sap_sync"` sem resultados) — esta story cria o primeiro artefato do Epic 2.

Lembrete de constraint do projeto: migrations existentes não podem ser alteradas, apenas adicionadas (CLAUDE.md).

### Padrão de log

Convenção do projeto: `log.Printf("[ComponentName] mensagem: %v", ...)`. Não existe correlationId estruturado hoje em nenhum lugar do código (apesar do NFR5 do PRD mencionar isso) — introduzir um tag de componente novo, sugestão `[SAP Sync]`, seguindo o mesmo padrão:

```go
log.Printf("[SAP Sync] Disparando sincronização para company_id=%s bukrs=%s (%d chaves)", companyID, b, len(chaves))
```

### Fora de escopo desta story (não implementar)

- Chamada HTTP real ao SAP (`SearchPayments`, `services/sap_payments.go`/`sap_payments_processor.go`) — Story 2.2
- Retry/backoff, rate limit, redução de lote — Story 2.2
- Contagens detalhadas por `paymentStatus`, tela de histórico, alertas por e-mail — Story 2.4
- Persistência em `pagamentos_fornecedores` — Story 2.3

### Project Structure Notes

- Novo arquivo: `backend/migrations/116_sap_sync_runs.sql`
- Novo arquivo: `backend/services/sap_sync_trigger.go`
- Editado: `backend/services/rfb_processor.go` (2 pontos de chamada, após `updateRequestStatus(..., "completed")`)
- Editado: `backend/services/rfb_creditos_processor.go` (2 pontos de chamada, mesmo padrão)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 2, Story 2.1] — AC originais (linhas 142-170)
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md#FR-1, FR-7, FR-10]
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/addendum.md#B, #C] — esboço de arquitetura e migration
- [Source: backend/services/rfb_processor.go:88-386] — `ProcessarDownloadRFB`/`ReprocessarRawJSON`, pontos de conclusão
- [Source: backend/services/rfb_creditos_processor.go:51-472] — `ProcessarDownloadCreditosRFB`/`ReprocessarRawJSONCreditosRFB`
- [Source: backend/handlers/rfb_apuracao.go:465-572] — `RFBWebhookHandler`, padrão de goroutine + idempotência de webhook (nível `rfb_requests`, não substitui a idempotência de `sap_sync_runs`)
- [Source: backend/migrations/096_rfb_creditos.sql] — schema de `rfb_creditos`
- [Source: backend/migrations/074_erp_bridge.sql] — modelo de referência para `sap_sync_runs`
- [Source: backend/migrations/115_sap_credentials.sql, backend/handlers/sap_credentials.go] — Epic 1, já implementada
- [Source: backend/services/rfb.go:121-128, backend/services/rfb_scheduler.go:269-278] — padrões de goroutine a copiar
- [Source: _bmad-output/implementation-artifacts/1-2-teste-de-conexao-com-credenciais-sap.md] — story anterior, aprendizados de auth/erro (não diretamente aplicável aqui, pois esta story não expõe endpoint HTTP novo, mas reforça o padrão de log/erro do projeto)

## Dev Agent Record

### Agent Model Used

Claude Sonnet 5 (claude-sonnet-5)

### Debug Log References

- `go build ./...`, `go vet ./...` — limpos
- Migration `116_sap_sync_runs.sql` aplicada manualmente em `fiscal_db` (Postgres local, `docker` container `fb_testesfc-db`) via `psql` para viabilizar os testes de integração
- `go test ./services/... -run TestTriggerSAPSync -v` — 5/5 testes passando: sem credencial SAP (0 registros), sem créditos novos (0 registros), caminho feliz com 2 BUKRS (2 registros, status `concluido`), idempotência (2 disparos → 1 registro), `requestID` inexistente (nenhum erro propagado)
- `go test ./...` (suíte completa do projeto) — sem regressões, todos os pacotes passando
- Confirmado via `psql` que nenhum dado de teste (environment/enterprise_group/company temporários) permaneceu após os testes (cascade delete)
- Pós-revisão (code review): migration `116` recriada em `fiscal_db` com a constraint `UNIQUE(company_id, bukrs, request_id)` (drop + reaplicação, tabela ainda não estava em produção); `go build`/`go vet` limpos; `go test ./services/... -run TestTriggerSAPSync -v -count=5` — todos os 7 testes (5 originais + 2 novos) passando em 5 repetições; `go test ./services/... -run TestSyncBukrs_ConcorrenciaReal -v -count=10` — 10 rodadas com 10 goroutines reais concorrentes cada, sempre exatamente 1 execução criada (comprova o fix da condição de corrida); `go test ./...` (suíte completa) sem regressões; sem dados de teste remanescentes

### Completion Notes List

- Identificados 4 pontos de conclusão da apuração RFB (não 1) — `ProcessarDownloadRFB`, `ReprocessarRawJSON` (`rfb_processor.go`), `ProcessarDownloadCreditosRFB`, `ReprocessarRawJSONCreditosRFB` (`rfb_creditos_processor.go`). O disparo `go TriggerSAPSync(db, requestID)` foi inserido logo após cada chamada a `updateRequestStatus(..., "completed")`, em vez de centralizar dentro de `updateRequestStatus` — decisão tomada porque esse helper é usado também para outras transições de status (não apenas "completed"), e centralizar exigiria um `if status == "completed"` a mais sem ganho real de simplicidade.
- Limitação de schema documentada nos Dev Notes foi confirmada na prática: `rfb_creditos.request_id` é sobrescrito a cada upsert, então "créditos recém-gravados" foi implementado como "chaves com `request_id` = requestID recém-processado" — decisão de escopo já pré-aprovada na story, sem necessidade de tocar em `insertCredito` (código já testado da Epic RFB, fora de escopo).
- `sap_sync_runs.chaves_enviadas` implementado como `TEXT[]` (array de chaves), não como contagem — permite reconstituir o lote exato em stories futuras (2.2+) sem nova migration.
- Introduzido `defer recover()` em `TriggerSAPSync` — não havia precedente de `recover()` em goroutine no projeto, mas foi considerado necessário aqui porque a goroutine roda fora do ciclo de vida de uma request HTTP (onde o `net/http` isola panics por conexão); um panic não tratado aqui derrubaria o processo inteiro.
- Testes de integração escrevem direto no Postgres real de desenvolvimento (`fiscal_db`), seguindo a mesma política já aceita nas Stories 1.1/1.2 (sem harness de mock de banco no projeto) — mas, diferente daquelas stories, aqui foi possível automatizar os testes como testes Go de fato (`go test`), não apenas verificação manual via `curl`/`psql`, porque toda a lógica desta story é bancária (sem chamada HTTP externa a mockar). Os testes criam e limpam (via cascade delete) sua própria cadeia `environment → enterprise_group → company` para não colidir com dados reais.
- Escopo mantido estritamente dentro da story: nenhuma chamada HTTP real ao SAP foi implementada (reservado para a Story 2.2); a execução é marcada `concluido` imediatamente após o registro, apenas para provar o gating/idempotência ponta a ponta.
- **Pós code-review (3 revisores adversariais):** achado real e crítico dos 3 revisores independentemente — a checagem de idempotência original (`SELECT` seguido de `INSERT` separado) deixava uma janela de corrida real sob concorrência (ex.: duplo clique em "reprocessar"), e o teste de idempotência original só chamava a função 2x sequencialmente na mesma goroutine, nunca exercitando a corrida de verdade. Corrigido movendo a garantia para o banco (`UNIQUE` + `ON CONFLICT DO NOTHING`) e adicionado teste com 10 goroutines reais concorrentes. Edge Case Hunter também encontrou 2 bugs reais de tratamento: `chave_dfe` nullable causando abort total do fetch, e `recover()` no escopo errado (abortando BUKRS irmãos em vez de isolar por BUKRS) — ambos corrigidos e cobertos por novos testes. 1 decisão (testes de integração não rodam em CI hoje, sem serviço Postgres nos workflows) foi deferida pelo usuário, consistente com a política já aceita nas Stories 1.1/1.2. 10 itens adicionais deferidos (ver `deferred-work.md`) — principalmente padrões pré-existentes no projeto (goroutine sem WaitGroup, sem worker pool, sem context.Context) não introduzidos por esta story, e refinamentos de baixo valor incremental para a Story 2.4.

### File List

- `backend/migrations/116_sap_sync_runs.sql` (novo, revisado pós code-review — `UNIQUE(company_id, bukrs, request_id)`)
- `backend/services/sap_sync_trigger.go` (novo, revisado pós code-review — `ON CONFLICT DO NOTHING`, `sql.NullString`, `recover()` por BUKRS)
- `backend/services/sap_sync_trigger_test.go` (novo, revisado pós code-review — 2 testes novos + 1 teste reforçado)
- `_bmad-output/implementation-artifacts/deferred-work.md` (editado — 11 achados de code review desta story adicionados)
- `backend/services/rfb_processor.go` (editado — 2 pontos de disparo, após `updateRequestStatus(..., "completed")`)
- `backend/services/rfb_creditos_processor.go` (editado — 2 pontos de disparo, mesmo padrão)

### Change Log

- 2026-07-11: Implementação completa da Story 2.1 — migration `sap_sync_runs`, serviço `TriggerSAPSync` (gating por `sap_credentials`/créditos novos, idempotência por `request_id`, isolamento de falhas via goroutine + recover), disparo integrado nos 4 pontos de conclusão da apuração RFB, 5 testes de integração contra Postgres real, sem regressões na suíte existente.
- 2026-07-11: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor) — 1 decisão resolvida (testes de integração sem cobertura em CI, deferido), 4 patches aplicados (condição de corrida na idempotência corrigida via constraint UNIQUE + ON CONFLICT, chave_dfe nullable, recover() por BUKRS, teste fraco reforçado), 2 testes novos adicionados (concorrência real, chave_dfe NULL), 10 itens deferidos.
