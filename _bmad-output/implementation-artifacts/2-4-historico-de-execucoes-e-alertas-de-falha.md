---
baseline_commit: f13b4a78132afcdcdcf0e955dd8064f82fe320be
---

# Story 2.4: Histórico de Execuções e Alertas de Falha

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a administrador ou analista,
I want ver o histórico de execuções de sincronização e receber alertas em falhas repetidas,
so that eu confie que a automação está saudável.

Realiza FR-7 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Quarta e última story da Epic 2 — ver `_bmad-output/planning-artifacts/epics.md`. Continuação direta das Stories 2.1/2.2/2.3 (todas `done`): `sap_sync_runs` já existe e já é populada por `syncBukrs`/`finishRun` (`backend/services/sap_sync_trigger.go`), mas hoje só grava `status`/`iniciado_em`/`concluido_em` — sem contagens por resultado de pagamento, sem detalhe de erro persistido, sem API, sem tela, e sem nenhum alerta. Esta story fecha a Epic 2.

## Acceptance Criteria

1. **Given** a migration desta story, **When** aplicada, **Then** estende `sap_sync_runs` com contagens detalhadas por `paymentStatus` (`chaves_pago_total`, `chaves_pago_parcial`, `chaves_em_aberto`, `chaves_nao_localizado`) e um campo de detalhe de erro (`erro_detalhe`) — o tempo total de execução é exposto calculado (`concluido_em - iniciado_em`), sem precisar de coluna própria.
2. **Given** a migration aplicada, **When** o histórico é consultado, **Then** está disponível via uma nova API paginada e uma nova tela no frontend.
3. **Given** 3 execuções **seguidas** com erro transitório (`status = 'falha'`, Story 2.2) para o mesmo `(company_id, bukrs)`, **When** a 3ª ocorre, **Then** um alerta por e-mail dispara via `services/email.go` — não deve realertar a cada falha subsequente (4ª, 5ª...), só na transição exata para 3 consecutivas.
4. **Given** uma execução marcada como `falha_credencial` (401/403, Story 2.2), **When** isso ocorre, **Then** dispara um alerta imediato e distinto (assunto/corpo diferentes do alerta de falha transitória), sem esperar 3 falhas.
5. **Given** falha em 1 de N BUKRS configurados para uma empresa, **When** visualizado o histórico, **Then** o resultado por BUKRS é visível isoladamente na tela/API — nunca apenas agregado por empresa.

## Tasks / Subtasks

- [x] **Task 1 — Migration: contagens e detalhe de erro** (AC: #1)
  - [x] Criada `backend/migrations/119_sap_sync_runs_contagens.sql` com exatamente os nomes pré-acordados na migration 116
  - [x] Tempo de execução calculado (`EXTRACT(EPOCH FROM (concluido_em - iniciado_em))`), sem coluna própria

- [x] **Task 2 — Acumular e persistir as contagens por `paymentStatus`** (AC: #1)
  - [x] `logBatchSummary` agora retorna `paymentStatusCounts` (struct tipada, não `map[string]int`) além de logar
  - [x] `SyncResult` ganhou 4 campos (`ChavesPagoTotal`/`ChavesPagoParcial`/`ChavesEmAberto`/`ChavesNaoLocalizado`), acumulados em `ProcessSAPSync` via `paymentStatusCounts.add`
  - [x] `finishRun` estendida com os 4 parâmetros de contagem + persiste `erro_detalhe` (sempre, não só em falha — decisão tomada conforme já antecipado nos Dev Notes)
  - [x] `syncBukrs` atualizado para passar as contagens e `result.Detail`

- [x] **Task 3 — Alertas por e-mail** (AC: #3, #4)
  - [x] `SendSAPSyncFailureAlert`/`SendSAPSyncCredentialAlert` criadas em `email.go`, compartilhando um helper `sendPlainAlertEmail` (HTML simples, não os templates elaborados de `SendAIReportEmail`)
  - [x] Descoberta de destinatários implementada em `findCompanyAdminEmails` (`sap_sync_alerts.go`), com a query de `admin.go` adaptada para `SELECT`
  - [x] Alerta imediato de credencial implementado (`alertCredentialFailure`, chamado via `maybeAlertOnFailure` logo após `finishRun`)
  - [x] Contagem de falhas consecutivas extraída em `countConsecutiveFailures` (testável isoladamente) — alerta dispara só quando a contagem é exatamente 3 (`consecutiveFailuresToAlert`), nunca antes nem depois
  - [x] Falha ao enviar e-mail (incluindo `SMTP_PASSWORD` vazio) é isolada e apenas logada — nunca propagada

- [x] **Task 4 — API de histórico paginada** (AC: #2, #5)
  - [x] `SAPSyncRunsListHandler` criado em `backend/handlers/sap_sync_runs.go`, seguindo o padrão de paginação de `PagamentosFornecedoresListHandler` (não o de `ERPBridgeRunsHandler`)
  - [x] Filtros `bukrs`/`status`, paginação `page`/`page_size` (default 50, máx 200)
  - [x] Resposta inclui `bukrs` por item (nunca agregado) e `duracao_segundos` calculado
  - [x] Rota registrada em `main.go`

- [x] **Task 5 — Tela de histórico no frontend** (AC: #2, #5)
  - [x] `SAPSyncHistory.tsx` criado, seguindo o esqueleto de `ERPBridgeLogs.tsx` (sem necessidade de linha expansível, já que os dados desta story são flat por natureza — BUKRS e todas as contagens já visíveis diretamente na linha, reforçando AC #5)
  - [x] Entrada em `navigation.ts` e rota `/config/sap-sincronizacoes` em `App.tsx`

- [x] **Task 6 — Testes e verificação**
  - [x] 10 testes novos (revisados de "13" para o valor real após auditoria — ver Review Findings): contagem de falhas consecutivas (3 cenários incluindo isolamento por BUKRS), decisão de alertar (não antes/depois da 3ª), descoberta de destinatários, persistência real de contagens/erro_detalhe via `TriggerSAPSync` fim a fim, handler da API (unauthorized/method not allowed). +4 testes adicionados na revisão (ver abaixo), total final: 14
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões
  - [x] Verificação manual E2E: subi o backend real + `sap-mock-server`, mintei um JWT real (via `handlers.GenerateToken`, sem nunca expor o secret), criei dados de teste diretamente no banco e confirmei via `curl` que o endpoint `/api/sap/sync-runs` retorna os 2 BUKRS isoladamente (AC #5), com filtros `bukrs`/`status` e paginação funcionando corretamente. **Limitação registrada:** não há ferramenta de navegador neste ambiente — a tela React foi verificada por leitura de código contra o formato exato da resposta da API (mesmos nomes de campo), não por captura visual no navegador

### Review Findings

*(Revisão adversarial — Blind Hunter + Edge Case Hunter + Acceptance Auditor — sobre o diff/delta isolado desta story, última da Epic 2)*

- [x] [Review][Patch] Alerta de falha de credencial reenviado a cada apuração enquanto a credencial seguir quebrada, sem gating/cooldown, contradizendo o próprio texto do e-mail ("nenhuma nova tentativa automática será feita") [backend/services/sap_sync_alerts.go, backend/services/email.go] — corrigido: `alertCredentialFailure` agora só dispara na transição exata para `falha_credencial` (reaproveitando a mesma semântica anti-ruído do alerta de falhas transitórias, via `countConsecutiveByStatus` generalizada), e o texto do e-mail foi corrigido para não afirmar uma suspensão de tentativas que não existe. Testado em `TestAlertCredentialFailure_DisparaApenasNaTransicao` e `TestAlertCredentialFailure_RealertaAposSucessoIntermediario`
- [x] [Review][Patch] Rota `/api/sap/sync-runs` registrada com role vazia (`withAuth(handlers.SAPSyncRunsListHandler, "")`), permitindo qualquer usuário autenticado da empresa (não só admin) ver o histórico completo via API, embora a UI restrinja a página a admins [backend/main.go] — corrigido: role alterada para `"admin"`, consistente com `/api/sap/credentials/test`
- [x] [Review][Patch] Nenhum índice cobre o padrão `WHERE company_id = ? AND bukrs = ? ORDER BY iniciado_em DESC` usado tanto por `countConsecutiveFailures`/`countConsecutiveByStatus` quanto pela listagem paginada — tende a full scan conforme a tabela cresce [backend/migrations/116_sap_sync_runs.sql] — corrigido: nova migration `120_sap_sync_runs_idx_iniciado_em.sql` com índice `(company_id, bukrs, iniciado_em DESC)`
- [x] [Review][Patch] `strconv.Atoi` de `page`/`page_size` com erro descartado (`_`) — um valor fora do range de `int` (ex. `page=99999999999999999999`) retorna o valor máximo de `int` sem erro capturado, passando pelo guard `page < 1` e estourando o cálculo de `offset`, gerando 500 do Postgres em vez de página vazia [backend/handlers/sap_sync_runs.go] — corrigido: erro de parse agora força o default (`page=1`/`page_size=50`)
- [x] [Review][Patch] `SAPSyncHistory.tsx` não tratava `isError` do `useQuery` — uma falha real (401/500/rede) exibia o mesmo texto de "Nenhuma execução registrada ainda.", escondendo do usuário que houve erro [frontend/src/pages/SAPSyncHistory.tsx] — corrigido: novo estado de erro visível, distinto do estado vazio
- [x] [Review][Patch] Caminho de disparo real dos alertas (`SendSAPSyncFailureAlert`/`SendSAPSyncCredentialAlert`) sem nenhuma cobertura de teste — os testes existentes só confirmavam "não panicar", nunca que o e-mail seria de fato enviado (ou não) [backend/services/sap_sync_alerts.go, backend/services/sap_sync_alerts_test.go] — corrigido: `sendFailureAlertFunc`/`sendCredentialAlertFunc` extraídas como variáveis de pacote (mesmo padrão de override de `backoffSleep`/`pacerSleepFunc`), permitindo espionar o disparo real sem SMTP. 4 testes novos confirmam disparo exato na 3ª falha, não-disparo antes/depois, e a nova semântica de transição do alerta de credencial
- [x] [Review][Patch] `findCompanyAdminEmails` nunca testada com admins reais (só o caminho vazio) [backend/services/sap_sync_alerts_test.go] — corrigido: `TestFindCompanyAdminEmails_RetornaEmailsQuandoAdminsExistem`, cria usuário admin vinculado ao ambiente da empresa de teste e confirma o e-mail retornado
- [x] [Review][Patch] Dev Agent Record afirmava "13 testes novos" mas o total real (contado em disco) era 10 [Task 6 acima] — corrigido: contagem revisada para o valor real, e para 14 após os testes adicionados nesta revisão
- [x] [Review][Dismiss] `findCompanyAdminEmails` resolve admins no nível de *ambiente* (via `user_environments`), não da empresa/grupo específico — investigado: é exatamente o mesmo escopo de autorização já usado em `backend/handlers/admin.go:126-138` (EXISTS idêntico, adaptado de existencial-por-usuário para enumeração de todos os usuários), ou seja, o codebase já trata admin de ambiente como autorizado para qualquer empresa daquele ambiente. Não é um vazamento novo introduzido por esta story — é consistente com a semântica de autorização já estabelecida no projeto
- [ ] [Review][Defer] Nenhum lock/idempotência ao redor do check-and-send dos alertas — duas chamadas quase simultâneas a `finishRun`/`maybeAlertOnFailure` para o mesmo (company_id, bukrs), disparadas por goroutines de apurações concorrentes, podem ambas ler a mesma contagem "de transição" antes de qualquer commit intermediário e disparar e-mails duplicados. Razão do adiamento: corrigir corretamente exigiria transação + `pg_advisory_xact_lock` em torno de `finishRun`+`maybeAlertOnFailure` (hoje `finishRun` é um `db.Exec` solto, não uma transação) — refatoração maior para um risco de baixa severidade (e-mail duplicado, não corrupção de dado). **Revisitar** se alertas duplicados se mostrarem recorrentes em produção
- [ ] [Review][Defer] Falha transitória ao enviar o e-mail de alerta (SMTP indisponível) no exato momento da transição para a 3ª falha consecutiva mascara silenciosamente a sequência inteira — como o alerta só dispara na transição exata (AC #3), uma falha de envio nesse instante nunca é reavaliada nas falhas seguintes (4ª, 5ª...). Razão do adiamento: é uma consequência direta do design "alertar 1x, sem ruído" já definido pela própria AC #3; corrigir exigiria rastrear "já alertado nesta sequência" via uma coluna nova, redesenho fora do escopo desta revisão. **Revisitar** se o volume de falso-silêncio se mostrar relevante em produção
- [ ] [Review][Defer] Paginação por `OFFSET`/`LIMIT` sem tiebreaker além de `iniciado_em DESC` pode gerar duplicação/omissão de linhas entre páginas se novas execuções forem inseridas entre a navegação de uma página para outra (a tela atualiza a cada 15s) — impacto prático baixo dado o volume esperado de execuções e uso por poucos admins visualizando ocasionalmente. **Revisitar** se o volume de sincronizações por empresa crescer muito
- [ ] [Review][Defer] Sem teste de isolamento de tenant (`company_id` A não vê execuções de B) para `SAPSyncRunsListHandler` — gap real, mas nenhum handler do pacote `handlers` tem hoje um teste desse tipo com banco real (toda a suíte de handlers testa só caminhos pré-DB); construir esse harness do zero é maior que o escopo desta story isolada. A proteção real (`WHERE company_id = $1` via `GetEffectiveCompanyID`) segue o mesmo padrão usado e confiado em todos os outros handlers do projeto. **Revisitar** ao planejar expansão de cobertura de teste do pacote `handlers` como um todo
- [ ] [Review][Defer] `paymentStatus` fora dos 4 valores conhecidos (`PAGO_TOTAL`/`PAGO_PARCIAL`/`EM_ABERTO`/`NAO_LOCALIZADO`) é silenciosamente descartado das contagens — comportamento pré-existente (já presente no `map[string]int` solto antes desta story), mas agora persistido e exposto a admins como número "oficial" na tela de histórico, tornando o gap mais visível. **Revisitar** se o SAP introduzir um novo valor de status, ou adicionar uma contagem "outros/desconhecido" residual
- [ ] [Review][Defer] Ordenação de "falhas consecutivas" por `iniciado_em` (início) em vez de `concluido_em` (fim) pode distorcer a contagem se duas execuções para o mesmo (company_id, bukrs) se sobrepuserem (ex. trigger manual concorrente com o agendado) — baixa probabilidade dado que a Epic 2 dispara sincronização apenas ao final de uma apuração, sem botão de "forçar sincronização manual" hoje. **Revisitar** se um trigger manual for adicionado no futuro
- [ ] [Review][Defer] `erro_detalhe` grava texto informativo mesmo em execuções `concluido` (decisão já documentada nesta story) — nome de coluna um pouco enganoso, mas o frontend já rotula a coluna genericamente como "Detalhe". Cosmético, baixo valor de correção isolada
- [ ] [Review][Dismiss] Headers de auth montados manualmente em `SAPSyncHistory.tsx` em vez de depender do interceptor global de `window.fetch` — reproduz exatamente o mesmo padrão pré-existente de `ERPBridgeLogs.tsx` (o modelo explícito que a story pediu para seguir), não uma inconsistência introduzida por esta story
- [ ] [Review][Defer] `<select>` HTML nativo no filtro de status em vez do componente `Select` do Radix/shadcn usado no resto do design system — inconsistência visual/acessibilidade de baixa severidade, mesma classe de débito já aceito em `ERPBridgeLogs.tsx`

## Dev Notes

### `finishRun` atual — ponto de integração exato

`backend/services/sap_sync_trigger.go:149-155`:
```go
func finishRun(db *sql.DB, runID, status, detail string) {
	if _, err := db.Exec(`
		UPDATE sap_sync_runs SET status = $2, concluido_em = NOW() WHERE id = $1
	`, runID, status); err != nil {
		log.Printf("[SAP Sync] Erro ao finalizar execução run_id=%s (status=%s, detail=%s): %v", runID, status, detail, err)
	}
}
```
`detail` já existe como parâmetro mas **nunca foi persistido**, só logado (confirmado, incluindo em `syncBukrs` linha ~143). Esta story finalmente grava esse valor em `erro_detalhe` (só quando `status` indica falha, ou sempre — decisão de implementação, mas gravar sempre é mais simples e não há AC contra isso) e adiciona as 4 contagens como novos parâmetros.

### `SyncResult` atual — sem contagens

`backend/services/sap_payments_processor.go:463-466`:
```go
type SyncResult struct {
	Status string // concluido | falha | falha_credencial
	Detail string
}
```
Precisa ganhar 4 campos inteiros. Não há coluna `run_id` em `pagamentos_fornecedores` — as contagens **não podem** ser reconstituídas via `JOIN` depois; precisam ser acumuladas durante a própria execução, dentro de `ProcessSAPSync`, a partir do que `logBatchSummary`/`processBatch` já calculam por lote mas hoje só logam.

### Alertas de e-mail — sem precedente de descoberta de destinatários

`backend/services/email.go` tem `SendPasswordResetEmail` (único uso real em produção, `backend/handlers/auth.go:867`, destinatário = e-mail informado no request) e `SendAIReportEmail` (assinatura pronta para múltiplos destinatários, mas código morto — nenhum handler chama). **Nenhum dos dois mostra como descobrir "admins de uma empresa"** — a query de referência mais próxima é o `EXISTS` de autorização em `backend/handlers/admin.go:131-140`, que precisa ser adaptada para um `SELECT DISTINCT u.email ...` (ver Task 3). Se `SMTP_PASSWORD` não estiver configurada, `GetEmailConfig`/as funções de envio já retornam erro sem tentar nada — tratar como falha isolada.

### Contagem de "falhas consecutivas" — sem precedente, lógica nova

Não existe hoje nenhuma lógica de "N falhas seguidas" no backend. Implementação sugerida: após `finishRun` gravar `status='falha'` para um `(company_id, bukrs)`, consultar:
```sql
SELECT status FROM sap_sync_runs
WHERE company_id = $1 AND bukrs = $2
ORDER BY iniciado_em DESC
LIMIT 5
```
e contar, a partir do primeiro resultado (mais recente = a execução que acabou de finalizar), quantos `status = 'falha'` seguidos aparecem antes do primeiro valor diferente (ou do fim da lista). **Alertar somente quando essa contagem for exatamente 3**, não `>= 3` — do contrário a 4ª, 5ª... falhas consecutivas também disparariam alerta repetidamente, o que a AC não pede e seria ruído. Este comportamento exato (alertar só na transição, não a cada falha) deve ser coberto por teste (Task 6).

### Padrão de paginação a copiar — `PagamentosFornecedoresListHandler`

`backend/handlers/pagamentos_fornecedores.go:311-439` — `page`/`page_size` reais com `COUNT(*)` + `LIMIT`/`OFFSET`, resposta `{"items", "page", "page_size", "total"}`. **Não copiar** o padrão de `ERPBridgeRunsHandler` (`backend/handlers/erp_bridge.go:227`), que só usa `LIMIT 200` fixo sem paginação de verdade nem filtros de query string.

### Padrão de tela a copiar — `ERPBridgeLogs.tsx`

`frontend/src/pages/ERPBridgeLogs.tsx` — `useQuery` com `queryKey` incluindo `companyId`, `authHeaders` padrão (`Authorization: Bearer <token>` + `X-Company-ID`), componentes `StatusBadge`/`StatusIcon` (lucide-react) mapeando status para cor/ícone, linha expansível para detalhe. Adaptar trocando o agrupamento "por servidor" (ERP Bridge) por "por BUKRS" (esta story) — BUKRS deve ser uma coluna sempre visível na linha principal, não escondida atrás de um expand (AC #5 pede visibilidade isolada por BUKRS, não agregada).

### Nenhuma rota/navegação existe ainda para isso

Confirmado via grep em `navigation.ts`/`App.tsx`: só existe a rota de credenciais SAP (`/config/sap-credenciais`, `Story 1.1`). Esta story cria a rota/aba/página do zero.

### Próximo número de migration livre

Maior migration existente: `118_pagamentos_fornecedores_origem_sap.sql` → **próximo número livre é `119`**.

### Fora de escopo desta story (não implementar)

- Coexistência CSV/SAP com sinalização de conflito de valor — Story 2.5
- Qualquer mudança na tela de conciliação (`rfb_pagamentos_fornecedores.go`/`RFBPagamentosFornecedores.tsx`) — Epic 3
- Escalonamento/repetição de alerta além da transição exata para 3 falhas consecutivas (ex. re-alertar a cada 5 falhas adicionais) — não pedido pela AC, não implementar sem necessidade comprovada

### Project Structure Notes

- Novo arquivo: `backend/migrations/119_sap_sync_runs_contagens.sql`
- Novo arquivo: `backend/handlers/sap_sync_runs.go`
- Novo arquivo: `frontend/src/pages/SAPSyncHistory.tsx`
- Editado: `backend/services/sap_payments_processor.go` (`SyncResult`, `logBatchSummary`/nova função de contagem, `ProcessSAPSync`)
- Editado: `backend/services/sap_sync_trigger.go` (`finishRun` estendido, lógica de alerta)
- Editado: `backend/services/email.go` (2 novas funções de alerta)
- Editado: `backend/main.go` (nova rota)
- Editado: `frontend/src/lib/navigation.ts`, `frontend/src/App.tsx` (nova entrada/rota)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 2, Story 2.4] — AC originais (linhas 232-254)
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md#FR-7]
- [Source: backend/services/sap_sync_trigger.go] — `finishRun`, `syncBukrs` (Stories 2.1/2.2/2.3, já `done`)
- [Source: backend/services/sap_payments_processor.go] — `SyncResult`, `logBatchSummary`, `ProcessSAPSync` (Story 2.2, já `done`)
- [Source: backend/migrations/116_sap_sync_runs.sql] — comentário pré-acordando os nomes de coluna desta story
- [Source: backend/services/email.go] — `GetEmailConfig`, `SendPasswordResetEmail`, `SendAIReportEmail`, `sendMailSSL`
- [Source: backend/handlers/admin.go:131-140] — query de referência para descobrir admins de uma empresa
- [Source: backend/handlers/pagamentos_fornecedores.go:311-439] — padrão de paginação a copiar
- [Source: backend/handlers/erp_bridge.go:227-360] — padrão de listagem/detalhe de runs (paginação NÃO copiar, estrutura geral sim)
- [Source: frontend/src/pages/ERPBridgeLogs.tsx] — padrão de tela de histórico a copiar
- [Source: frontend/src/lib/navigation.ts, frontend/src/App.tsx] — confirmado que não existe rota/nav prévia para esta story
- [Source: _bmad-output/implementation-artifacts/2-3-persistencia-de-pagamentos-com-deduplicacao-robusta.md] — story anterior, código que esta story estende diretamente

## Dev Agent Record

### Agent Model Used

Claude Sonnet 5 (claude-sonnet-5)

### Debug Log References

- `go build ./...`, `go vet ./...` — limpos após cada task
- Migration `119` aplicada em `fiscal_db` real (via `psql` e depois confirmada pelo próprio runner de migrations do backend ao subir o servidor real)
- `go test ./services/... -v` — 41 testes passando; `go test ./handlers/... -v` — 25 testes passando (10 novos desta story no total entre os dois pacotes, não 13 como registrado antes da revisão — ver Review Findings)
- `go test ./...` (suíte completa) — sem regressões
- **Pós-revisão**: migration `120` (índice) aplicada; `go build ./...`, `go vet ./...`, `go test ./...` — limpos, +4 testes novos em `sap_sync_alerts_test.go` (14 testes novos no total da story)
- `npx tsc -p tsconfig.app.json --noEmit` — 26 erros pré-existentes (mesma baseline de antes desta story), nenhum no código novo
- **Verificação manual E2E**: subi o `sap-mock-server` real e o backend real (`go run .`), mintei um JWT válido via `handlers.GenerateToken` (secret lido internamente do `.env`, nunca exposto — token gravado em arquivo `0600`, nunca impresso), criei uma empresa/usuário de teste (owner) e 2 linhas de `sap_sync_runs` (uma `concluido` com contagens, uma `falha_credencial`) diretamente via SQL, e confirmei via `curl` no endpoint real `/api/sap/sync-runs`: os 2 BUKRS aparecem como itens isolados (nunca agregados), os filtros `?bukrs=` e `?status=` funcionam, e a paginação (`page`/`page_size`) retorna a fatia correta. Dados de teste removidos ao final.
- **Limitação registrada:** este ambiente não tem uma ferramenta de navegador — a tela `SAPSyncHistory.tsx` foi verificada por leitura de código contra o formato exato da resposta real da API (mesmos nomes de campo confirmados via `curl` acima), não por captura visual no navegador. `tsc --noEmit` confirma que o componente compila sem erros novos.

### Completion Notes List

- `logBatchSummary` foi refatorada para retornar uma struct tipada (`paymentStatusCounts`) em vez de um `map[string]int` solto — mais seguro contra erro de digitação nas chaves do mapa (`"PAGO_TOTAL"` vs `"PagoTotal"` etc.) e mais fácil de somar entre lotes (`paymentStatusCounts.add`).
- `finishRun` passou a persistir `erro_detalhe` **sempre** (não só em falha) — decisão já antecipada nos Dev Notes da própria story; para `status='concluido'` o campo fica com o texto informativo ("N chaves sincronizadas..."), o que é inofensivo e ainda pode ser útil para diagnóstico.
- A contagem de falhas consecutivas foi deliberadamente extraída para uma função pura (`countConsecutiveFailures`, sem efeito colateral de e-mail) para ser testável sem depender de configuração de SMTP — o teste de decisão (`alertIfConsecutiveFailuresReached`) só confirma que a função não panica nos cenários "antes da 3ª" e "depois da 3ª"; a decisão de "enviar ou não" já está coberta indiretamente pela contagem correta somada ao fato de que, sem `SMTP_PASSWORD` configurada no ambiente de teste, qualquer tentativa de envio real falharia de forma isolada e logada (comportamento também coberto pelo teste, que não falha mesmo sem SMTP).
- Sem precedente de código para "descontar destinatários de alerta" ou "contar falhas consecutivas" — ambos implementados do zero, com a query de `admin.go` adaptada e documentada.
- Escopo mantido dentro da story: nenhuma alteração na tela/lógica de conciliação (Epic 3), nenhum "escalonamento" de alerta além da transição exata para 3 falhas (não pedido pela AC).

### File List

- `backend/migrations/119_sap_sync_runs_contagens.sql` (novo)
- `backend/migrations/120_sap_sync_runs_idx_iniciado_em.sql` (novo — revisão: índice `(company_id, bukrs, iniciado_em DESC)`)
- `backend/services/sap_payments_processor.go` (editado — `paymentStatusCounts`, `logBatchSummary` retorna contagens, `batchOutcome`/`SyncResult`/`ProcessSAPSync` estendidos)
- `backend/services/sap_sync_trigger.go` (editado — `finishRun` estendida, chamada a `maybeAlertOnFailure`)
- `backend/services/sap_sync_alerts.go` (novo — `maybeAlertOnFailure`, `alertCredentialFailure`, `countConsecutiveByStatus`/`countConsecutiveFailures`, `alertIfConsecutiveFailuresReached`, `findCompanyAdminEmails`; revisão: `alertCredentialFailure` agora dedupa por transição via `countConsecutiveByStatus`, `sendFailureAlertFunc`/`sendCredentialAlertFunc` extraídas para injeção em teste)
- `backend/services/sap_sync_alerts_test.go` (novo — 7 testes; revisão: +4 testes — disparo exato na 3ª falha, dedup/transição do alerta de credencial, admins reais em `findCompanyAdminEmails`)
- `backend/services/sap_sync_trigger_test.go` (editado — 1 teste novo de persistência de contagens/erro_detalhe)
- `backend/services/email.go` (editado — `sendPlainAlertEmail`, `SendSAPSyncCredentialAlert`, `SendSAPSyncFailureAlert`; revisão: corrigido texto do alerta de credencial que afirmava suspensão de tentativas inexistente; revertida reformatação `gofmt` acidental de código não relacionado em `generateReformaHTML`)
- `backend/handlers/sap_sync_runs.go` (novo — `SAPSyncRunsListHandler`; revisão: `strconv.Atoi` de `page`/`page_size` agora trata erro de parse)
- `backend/handlers/sap_sync_runs_test.go` (novo — 2 testes)
- `backend/main.go` (editado — rota `/api/sap/sync-runs`; revisão: role corrigida de `""` para `"admin"`)
- `frontend/src/pages/SAPSyncHistory.tsx` (novo; revisão: tratamento de `isError` do `useQuery`)
- `frontend/src/lib/navigation.ts` (editado — entrada "Sincronizações SAP")
- `frontend/src/App.tsx` (editado — import + rota `/config/sap-sincronizacoes`)

### Change Log

- 2026-07-11: Implementação completa da Story 2.4 (última da Epic 2) — migration com contagens por paymentStatus e erro_detalhe; acumulação das contagens ao longo de `ProcessSAPSync`; alertas por e-mail (imediato para falha de credencial, na 3ª falha consecutiva exata para falha transitória) com descoberta de destinatários via admins do grupo da empresa; API paginada com filtros por bukrs/status; tela de histórico no frontend. 10 testes novos (não 13 como registrado inicialmente — contagem corrigida na revisão), verificação E2E manual via API real (mock SAP + backend real + JWT real). Sem regressões.
- 2026-07-11: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor) — 8 patches aplicados: dedup do alerta de credencial (deixou de reenviar a cada apuração) + correção do texto do e-mail que afirmava suspensão de tentativas inexistente; role `"admin"` na rota `/api/sap/sync-runs`; índice novo (migration 120) para o padrão de consulta por `(company_id, bukrs, iniciado_em)`; `strconv.Atoi` de paginação agora trata erro de parse; `SAPSyncHistory.tsx` trata `isError`; cobertura de teste do disparo real dos alertas (antes só "não panicava"); `findCompanyAdminEmails` testada com admins reais; contagem de testes corrigida (10, +4 da revisão = 14). 7 achados adiados para `deferred-work.md` (idempotência/lock de alerta concorrente, mascaramento por falha transitória de SMTP na transição, paginação por offset, ausência de teste de isolamento de tenant no handler, `paymentStatus` desconhecido descartado silenciosamente, ordenação por `iniciado_em` vs `concluido_em`, nome de coluna `erro_detalhe`). 2 achados descartados (escopo de `findCompanyAdminEmails` já é o padrão de autorização do projeto; headers manuais em `SAPSyncHistory.tsx` reproduzem o padrão pré-existente de `ERPBridgeLogs.tsx`). `go build`/`go vet`/`go test ./...` limpos após os patches.
