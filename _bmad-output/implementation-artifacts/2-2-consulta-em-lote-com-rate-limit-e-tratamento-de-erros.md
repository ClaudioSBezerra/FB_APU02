---
baseline_commit: f13b4a78132afcdcdcf0e955dd8064f82fe320be
---

# Story 2.2: Consulta em Lote com Rate Limit e Tratamento de Erros

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a sistema,
I want consultar `SearchPayments` em lotes bem dimensionados com lógica de retry,
so that a sincronização seja confiável dentro dos limites do SAP.

Realiza FR-2 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Segunda story da Epic 2 — ver `_bmad-output/planning-artifacts/epics.md`. Continuação direta da Story 2.1 (`done`): `services.TriggerSAPSync` → `syncBukrsSafe` → `syncBukrs` já fazem o gating, a idempotência e o registro em `sap_sync_runs`, mas hoje `syncBukrs` marca a execução como `concluido` **imediatamente após o INSERT, sem nenhuma chamada real ao SAP** — essa story substitui esse "final falso" pela chamada real, em lote, com retry/backoff e tratamento de erros.

## Acceptance Criteria

1. **Given** um conjunto de chaves a sincronizar para um BUKRS, **When** o sistema monta os lotes para `SearchPayments`, **Then** usa no máximo 500 chaves por requisição.
2. **Given** o rate limit configurado (sugestão inicial: 60 req/min), **When** o sistema envia requisições ao SAP, **Then** respeita esse limite entre chamadas sucessivas (pacing, não apenas reação a 429).
3. **Given** uma resposta HTTP 429 do SAP, **When** recebida, **Then** o sistema tenta novamente com backoff exponencial, respeitando o header `Retry-After` quando presente.
4. **Given** uma resposta HTTP 400 por lote grande demais, **When** recebida, **Then** o sistema reduz automaticamente o tamanho do lote e tenta novamente (sem exigir intervenção manual).
5. **Given** uma chave malformada dentro de um lote (tamanho inválido para NF-e/CT-e), **When** detectada, **Then** ela é filtrada/reportada antes do envio, sem invalidar o lote inteiro.
6. **Given** uma resposta HTTP 401/403, **When** recebida, **Then** não há retry — o lote é marcado em `sap_sync_runs` com um status de falha de credencial, distinto de falha transitória (429/500).
7. **Given** 3 tentativas malsucedidas (429/500) para um lote, **When** atingidas, **Then** o lote é marcado como falho (`status = 'falha'`) em `sap_sync_runs`, sem interromper o processamento dos demais lotes/BUKRS.

## Tasks / Subtasks

- [x] **Task 1 — Migration: novo status de falha de credencial** (AC: #6)
  - [x] Criada `backend/migrations/117_sap_sync_runs_falha_credencial.sql`
  - [x] Adicionado `CHECK (status IN ('em_andamento','concluido','falha','falha_credencial'))` — decisão: enrijecer o enum agora que passa a ter 4 valores, evitando typos silenciosos (a coluna não tinha `CHECK` desde a migration 116)
  - [x] Migration 116 não foi alterada

- [x] **Task 2 — Cliente HTTP para `SearchPayments`** (AC: #1, #5)
  - [x] Criado `backend/services/sap_payments_processor.go`
  - [x] `callSearchPayments` monta `{"dfeKeys": [...]}` e faz `POST` ao endpoint OData completo, com `Authorization: Bearer <token>` via `GetToken` já existente
  - [x] Reaproveitado `truncateForLog`; `io.LimitReader` de 10 MiB (vs 1 MiB do `/token`)
  - [x] Resposta `{"value": [...]}` parseada em `dfeResponse`/`paymentItem` com todos os campos do addendum — apenas logada/contada (`logBatchSummary`), não persistida (Story 2.3)
  - [x] `isValidDFeKey`/`filterValidKeys` filtram chaves fora de 44/50 caracteres antes do envio
  - [x] `splitIntoBatches` com `maxBatchSize = 500` nomeado

- [x] **Task 3 — Rate limiting (pacing) das chamadas ao SAP** (AC: #2)
  - [x] `callPacer` (intervalo mínimo entre chamadas, baseado em `time.Minute / rate`), com registro `pacerForCompany` — 1 pacer por empresa (rate limit é por credencial/consumidor, não global ao processo)
  - [x] Taxa configurável via `SAP_SYNC_RATE_LIMIT_PER_MIN` (default 60)

- [x] **Task 4 — Retry com backoff exponencial** (AC: #3, #7)
  - [x] `processBatch`: até 3 tentativas (`maxRetries`) para 429/500, backoff exponencial (na prática 1s e 2s — a 3ª tentativa já esgota o orçamento antes de dormir 4s, ver Review Findings), respeitando `Retry-After` quando presente (com teto de 60s pós-revisão)
  - [x] Após esgotar tentativas, lote marcado como falho (`batchOutcome{success:false}`) sem interromper os demais lotes/BUKRS — `ProcessSAPSync` continua o loop de lotes mesmo após uma falha
  - [x] Primeiro retry/backoff do projeto — implementado sem biblioteca externa

- [x] **Task 5 — Tratamento de 400 (lote grande demais) e 401/403 (credencial)** (AC: #4, #6)
  - [x] HTTP 400 → `processBatch` divide o lote ao meio recursivamente e tenta novamente (até `len(batch) <= 1`)
  - [x] HTTP 401/403 → retorna `credFail: true` imediatamente, sem retry — `ProcessSAPSync` propaga como `status = "falha_credencial"`

- [x] **Task 6 — Integração em `syncBukrs`** (AC: todos)
  - [x] `syncBukrs` agora chama `ProcessSAPSync` e grava o status final via `finishRun` (substituindo o `UPDATE` incondicional)
  - [x] `TriggerSAPSync` estendida para também trazer `client_id`, `client_secret`, `base_url` de `sap_credentials` (uma única query, sem query redundante por BUKRS); adicionado gating por `base_url` vazio (necessário agora que há chamada real)
  - [x] `client_secret` decriptado via `crypto.DecryptField` apenas dentro de `syncBukrs`, nunca logado — **desvio do plano original:** `DecryptField`/`EncryptField` foram **movidos** de `backend/handlers/crypto.go` para um novo pacote `backend/crypto/` (ver Completion Notes) — `services` não pode importar `handlers` (regra de arquitetura do projeto, "handlers imports services; services has no dependency on handlers"), então a story original citava um caminho de import que criaria dependência circular

- [x] **Task 7 — Testes e verificação**
  - [x] Testes unitários cobrindo: divisão de lote (500+500+201), filtro de chave malformada, pacing real (medido por tempo decorrido), retry 429 esgotando em 3 tentativas, 401 e 403 sem retry, redução automática de lote em 400, token inválido → `falha_credencial`, nenhuma chave válida → `concluido` sem chamada ao SAP, isolamento de pacer por empresa
  - [x] Verificação manual E2E contra o `sap-mock-server` real: 3 cenários (chave `ERR429`, chave `ERR500`, chaves válidas) — resultados exatos: `falha` (após retry esgotado), `falha` (após retry esgotado), `concluido` (round-trip real de sucesso com parsing da resposta)
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões (suíte completa do projeto + 20 testes do pacote `services`, após os patches de code review)

### Review Findings

*(Revisão adversarial em 3 camadas — Blind Hunter, Edge Case Hunter, Acceptance Auditor — sobre o diff isolado desta story)*

- [x] [Review][Decision] Falha ao obter token OAuth2 (`GetToken`) é tratada incondicionalmente como `falha_credencial`, sem distinguir erro transitório de rede (timeout, DNS, SAP fora do ar) de uma credencial de fato inválida — **resolvido: aceitar por ora (opção 1)**. Razão: corrigir corretamente exigiria alterar `GetToken` (código já testado da Story 1.2) para expor o status code HTTP; risco residual aceito no momento — revisitar se esse falso-positivo se mostrar recorrente em produção.

- [x] [Review][Patch] Corpo de resposta de erro do SAP (400/401/403/429/500) é lido mas nunca logado [backend/services/sap_payments_processor.go] — corrigido: `processBatch` agora loga `truncateForLog(result.errorBody)` em todos os branches não-200
- [x] [Review][Patch] Migration 117 usava `ADD CONSTRAINT ... CHECK` direto [backend/migrations/117_sap_sync_runs_falha_credencial.sql] — corrigido: `NOT VALID` + `VALIDATE CONSTRAINT` em dois passos, evitando lock exclusivo bloqueante
- [x] [Review][Patch] `Retry-After` do SAP usado sem teto superior [backend/services/sap_payments_processor.go] — corrigido: `maxRetryAfterSecs = 60` capa o valor antes do sleep
- [x] [Review][Patch] Pacer isolado por `company_id` em vez de por credencial [backend/services/sap_payments_processor.go] — corrigido: `pacerForClient(clientID)`, renomeado de `pacerForCompany`; teste atualizado (`TestPacerForClient_IsolaOrcamentoPorClientID`) confirma que o mesmo `client_id` compartilha o mesmo orçamento
- [x] [Review][Patch] `base_url` malformado consumia os 3 retries como falha transitória [backend/services/sap_payments_processor.go] — corrigido: nova `validateBaseURL` falha rápido (status `falha`, nenhuma chamada HTTP) antes de qualquer tentativa; testado em `TestValidateBaseURL_RejeitaSemChamarSAP`/`TestProcessSAPSync_BaseURLInvalida_FalhaSemChamarSAP`
- [x] [Review][Patch] Divisão recursiva por HTTP 400 disparava a metade direita mesmo com a esquerda já em `credFail` [backend/services/sap_payments_processor.go] — corrigido: `processBatch` retorna imediatamente após a metade esquerda se `left.credFail`; testado em `TestProcessBatch_400ComCredFailNaMetadeEsquerda_NaoChamaMetadeDireita` (confirma zero chamadas à metade direita)
- [x] [Review][Patch] Task 4/comentário de `exponentialBackoff` descreviam backoff "1s/2s/4s" quando 4s nunca é usado com `maxRetries=3` — corrigido: comentário e Task 4 da story ajustados para descrever o comportamento real (1s, 2s)
- [x] [Review][Patch] Debug Log References afirmava "22 testes" — corrigido para a contagem real (20 testes no pacote `services` após os patches, incluindo os 3 testes novos desta rodada)

- [x] [Review][Defer] Fallback de `ENCRYPTION_KEY` para `JWT_SECRET`/valor hardcoded sem o log de aviso que o próprio comentário promete [backend/crypto/crypto.go] — deferred, lógica pré-existente movida verbatim de `handlers/crypto.go`, não introduzida por esta story
- [x] [Review][Defer] `DecryptFieldWithFallback` trata qualquer erro de decriptação como "provavelmente plaintext legado", mascarando também chave rotacionada/dado corrompido [backend/crypto/crypto.go] — deferred, mesmo raciocínio (pré-existente)
- [x] [Review][Defer] Sem `context.Context`/timeout de nível superior em `ProcessSAPSync` (chamadas HTTP sem cancelamento coordenado) — deferred, mesmo padrão já deferido na Story 2.1, ausente em todo o pacote `services`
- [x] [Review][Defer] Orçamento de chamadas da divisão recursiva por 400 sem teto (pior caso ~500 chamadas individuais para um lote de 500) — deferred, baixo risco prático dado que mock e AC já assumem 500 como limite real do SAP; revisitar se a Open Question #1 do PRD (500 vs 5000) for resolvida a favor de um limite real menor
- [x] [Review][Defer] Nenhuma checagem de que a contagem de itens em `value[]` bate com as chaves enviadas — deferred, reconciliação por item é escopo da Story 2.3 (persistência em `pagamentos_fornecedores`)
- [x] [Review][Defer] Nenhum teste unitário dedicado ao pacote `backend/crypto` isoladamente — deferred, a lógica já é exercitada indiretamente por `TestClientSecretEncryptionRoundTrip`/`UsesRandomNonce` em `handlers/sap_credentials_test.go` (mesmos testes de antes da extração, agora via o novo import)

## Dev Notes

### Ponto exato de integração — `syncBukrs` em `sap_sync_trigger.go`

Estado atual (Story 2.1, já `done`), linhas 107-132:
```go
func syncBukrs(db *sql.DB, companyID, bukrs, requestID string, chaves []string) {
	var runID string
	err := db.QueryRow(`
		INSERT INTO sap_sync_runs (company_id, bukrs, request_id, chaves_enviadas, status)
		VALUES ($1, $2, $3, $4, 'em_andamento')
		ON CONFLICT (company_id, bukrs, request_id) DO NOTHING
		RETURNING id
	`, companyID, bukrs, requestID, pq.Array(chaves)).Scan(&runID)
	if err == sql.ErrNoRows { /* já existe, ignora */ return }
	if err != nil { /* loga, retorna */ return }
	log.Printf("[SAP Sync] Disparando sincronização para company_id=%s bukrs=%s (%d chaves)", companyID, bukrs, len(chaves))

	// Escopo desta story: nenhuma chamada real ao SAP ainda (Story 2.2).
	// Encerra a execução aqui para provar o gating/idempotência ponta a ponta.
	if _, err := db.Exec(`
		UPDATE sap_sync_runs SET status = 'concluido', concluido_em = NOW() WHERE id = $1
	`, runID); err != nil {
		log.Printf("[SAP Sync] Erro ao concluir execução run_id=%s: %v", runID, err)
	}
}
```
A Story 2.2 substitui o comentário e o `UPDATE` incondicional por: chamar o novo processor de lote/retry, e só então gravar o status final (`concluido`, `falha`, ou `falha_credencial`) baseado no resultado real. `runID`, `companyID`, `bukrs`, `chaves` já estão disponíveis neste escopo. `TriggerSAPSync` (linhas 20-61) já garante que só chega até aqui quando há `sap_credentials.ativo = true` e `bukrs_list` não vazio — mas hoje só lê `ativo, bukrs_list` (linha 35), não `client_id/client_secret/base_url`. Estender essa query (ou adicionar uma nova, self-contained dentro de `syncBukrs`) é decisão de implementação desta story.

### Contrato exato do `SearchPayments` no `sap-mock-server`

Endpoint: `POST {base_url}/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments`, roteado em `sap-mock-server/main.go` via `handleDFePaymentSubtree` (linha 170) → `handleSearchPayments` (linha 225). Exige `Authorization: Bearer <token>` — sem token válido, 401.

Request: `{"dfeKeys": ["chave1", "chave2", ...]}`.

Response de sucesso (`200`): `{"value": [...]}`, um objeto por chave com os campos do addendum (`dfeKey, dfeType, matchType, companyCode, supplierCNPJ, supplierId, invoiceDocument, invoiceFiscalYear, invoiceDate, invoiceAmount, currency, paymentStatus, totalPaidAmount, payments[]`); cada item de `payments[]` tem `clearingDocument, clearingDate, paymentPostingDate, paidAmount, paymentMethod, paymentMethodDesc, settlementMode, isPartial`.

**Limite de lote no mock:** `const maxBatchSize = 500` (`main.go:51`) — lote maior que isso → `400 batch_too_large`. O mock já resolveu a favor de **500** a inconsistência do PRD (addendum menciona 500 vs 5000 como Open Question #1 não resolvida) — o AC #1 desta story ("no máximo 500") está alinhado com essa decisão; não é uma decisão em aberto para o dev agent.

**Magic trigger keys para forçar erros deterministicamente** (dentro do array `dfeKeys`, não no client_id como em `/token`):
- Chave com prefixo `"ERR429"` → aborta a resposta inteira do lote com `429 rate_limited` + header `Retry-After: 5` (`main.go:266-269`)
- Chave com prefixo `"ERR500"` → aborta com `500 internal_error` (`main.go:271-272`)
- Chave com tamanho diferente de 44 ou 50 caracteres → **não aborta** o lote; entra no resultado como `{dfeType:"DESCONHECIDO", matchType:"NAO_LOCALIZADO", paymentStatus:"NAO_LOCALIZADO"}` — é o único cenário "gracioso" dentro do array, e é exatamente o comportamento que a validação de chave malformada desta story (AC #5, Task 2) deve reproduzir do lado do FB_APU02 **antes** de enviar (evitar depender do SAP para essa validação)

**Rate limit simulado independente de magic keys:** `SAP_MOCK_SIMULATE_RATE_LIMIT=true` faz o mock devolver `429` a cada 10ª requisição em qualquer endpoint autenticado (`main.go:101-103, 458-467`) — útil para testar o Task 3 (pacing) e Task 4 (retry) sem depender de chaves mágicas.

**401/403:** o mock só produz 401 (Bearer ausente/inválido, ou `client_id="erro_401"` no `/token`) — **nunca gera 403** em nenhum lugar do código. Isso significa que o tratamento de 403 (AC #6) não pode ser testado E2E contra o mock hoje; testar apenas via teste unitário que simula a resposta HTTP diretamente (sem depender do mock), e registrar essa limitação nos Completion Notes.

### Nenhum retry/backoff existe hoje no projeto — primeira implementação

Confirmado via busca em todo `backend/`: não há backoff exponencial em lugar nenhum. Os únicos padrões relacionados a rate limit são:
- `rfbRateLimitCache` (`backend/services/rfb.go`) — bloqueio reativo (`sync.Mutex` + `map[string]time.Time`), setado **depois** de já ter recebido um 429; não faz retry automático, só bloqueia a chamada seguinte até a janela expirar. Não serve como base para pacing proativo.
- `rateLimiter` (`backend/handlers/middleware.go:122-154`) — sliding window (`sync.Mutex` + `map[string][]time.Time`), usado para limitar requisições **recebidas** (login, register), não chamadas de **saída**. Padrão estrutural interessante (`Allow(key string) bool`), mas o propósito é rejeitar, não pacear.

Nenhum dos dois resolve o Task 3 (pacear 60 req/min ao SAP) nem o Task 4 (retry com backoff) diretamente — escrever do zero, mantendo simples (sem lib externa).

### `GetToken` — padrão a reaproveitar (Story 1.2, já `done`)

`backend/services/sap_payments.go:35-74`. Sem cache de token (documentado explicitamente como decisão da Story 1.2 — "a Epic 2 pode adicionar cache por cima se necessário"). Múltiplos lotes do mesmo BUKRS dentro de uma mesma execução de `TriggerSAPSync` provavelmente devem reusar 1 único token (obtido 1x por BUKRS, não 1x por lote) — decisão de implementação, mas evita chamadas OAuth2 desnecessárias. `truncateForLog` (mesmo arquivo, package-private mas acessível de qualquer novo arquivo em `services/`) e o padrão de `io.LimitReader` devem ser reaproveitados no novo processor.

### Migration — próximo número livre

Maior migration existente: `116_sap_sync_runs.sql` → **próximo número livre é `117`**. Não alterar a 116 (regra do projeto).

### Fora de escopo desta story (não implementar)

- Persistência em `pagamentos_fornecedores` — Story 2.3 (esta story apenas consulta e decide o status da execução; o parse da resposta pode ser feito, mas gravar os pagamentos é da próxima story)
- Contagens detalhadas por `paymentStatus`, tela de histórico, alertas por e-mail — Story 2.4
- Coexistência CSV/SAP, sinalização de `total_pago > valor_nota` — Story 2.5

### Project Structure Notes

- Novo arquivo: `backend/migrations/117_sap_sync_runs_falha_credencial.sql`
- Novo arquivo: `backend/services/sap_payments_processor.go` (cliente de lote, retry, rate limit)
- Editado: `backend/services/sap_sync_trigger.go` (`syncBukrs` — substituir o `UPDATE` incondicional pela chamada real)
- Possivelmente editado: `backend/services/sap_payments.go` (se `GetToken` precisar de algum ajuste para reuso, ex. cache por BUKRS/execução — avaliar necessidade real antes de mudar código já testado)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 2, Story 2.2] — AC originais (linhas 172-206)
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md#FR-2]
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/addendum.md#A] — contrato SearchPayments do SAP real, NFRs, inconsistência 500 vs 5000 (Open Question #1)
- [Source: backend/services/sap_sync_trigger.go] — ponto de integração exato (Story 2.1, `done`)
- [Source: backend/services/sap_payments.go] — `GetToken`, `truncateForLog`, padrão de HTTP client (Story 1.2, `done`)
- [Source: sap-mock-server/main.go, sap-mock-server/README.md] — contrato exato do mock, magic trigger keys, simulação de rate limit
- [Source: backend/services/rfb.go] — `rfbRateLimitCache`, padrão de bloqueio reativo (não diretamente reaproveitável, mas contexto)
- [Source: backend/handlers/middleware.go:122-154] — `rateLimiter` sliding window (padrão estrutural, não diretamente reaproveitável)
- [Source: backend/migrations/116_sap_sync_runs.sql] — schema atual de `sap_sync_runs`, sem `CHECK` constraint
- [Source: _bmad-output/implementation-artifacts/2-1-disparo-automatico-de-sincronizacao-pos-apuracao.md] — story anterior, código e decisões que esta story estende diretamente

## Dev Agent Record

### Agent Model Used

Claude Sonnet 5 (claude-sonnet-5)

### Debug Log References

- `go build ./...`, `go vet ./...` — limpos após cada task
- Migration `117` aplicada em `fiscal_db` real via `psql` (`ALTER TABLE ... ADD CONSTRAINT`, depois recriada em 2 passos — `NOT VALID` + `VALIDATE CONSTRAINT` — na rodada de patches do code review)
- `go test ./services/... -v` — 20 testes passando no total, incluindo `TestCallPacer_PaceiaChamadasSucessivas` (mede tempo real decorrido), `TestProcessBatch_Retry429ComBackoff_EsgotaEmFalha` (confirma exatamente 3 tentativas), `TestProcessBatch_401.../403...NaoTentaNovamente` (confirma exatamente 1 tentativa), `TestProcessBatch_400ReduzLoteAutomaticamente` (confirma divisão recursiva até sucesso), e 3 testes novos da rodada de patches (`TestProcessBatch_400ComCredFailNaMetadeEsquerda_NaoChamaMetadeDireita`, `TestValidateBaseURL_RejeitaSemChamarSAP`, `TestProcessSAPSync_BaseURLInvalida_FalhaSemChamarSAP`)
- `go test ./...` (suíte completa) — sem regressões, antes e depois dos patches
- **Verificação E2E manual contra `sap-mock-server` real** (não apenas httptest in-process): subi o mock (`go run .`, porta 8090) e um programa Go throwaway (removido ao final, não faz parte do File List) chamando `services.TriggerSAPSync` direto contra Postgres real + mock real. 3 cenários: (1) chave `ERR429` (44 chars) → retry com backoff real (~10s) → `status=falha` após 3 tentativas; (2) chave `ERR500` → retry (~4s) → `status=falha`; (3) 2 chaves válidas (44 chars) → round-trip real de sucesso, resposta parseada (`EM_ABERTO`/`NAO_LOCALIZADO`) → `status=concluido`. Dados de teste (`environments`/`enterprise_groups`/`companies`/`sap_credentials`/`rfb_requests`/`rfb_creditos`/`sap_sync_runs`) removidos ao final.
- Confirmado: HTTP 403 não pode ser reproduzido pelo `sap-mock-server` (só gera 401) — coberto exclusivamente por teste unitário com `httptest` custom, conforme já antecipado nos Dev Notes

### Completion Notes List

- **Desvio arquitetural não previsto na story original, resolvido nesta implementação:** `syncBukrs` (pacote `services`) precisa decriptar `client_secret` para chamar o SAP, mas `DecryptField`/`EncryptField`/`DecryptFieldWithFallback` viviam em `backend/handlers/crypto.go` (pacote `handlers`). O projeto documenta explicitamente "Circular imports: None detected — handlers imports services; services has no dependency on handlers" — `services` importar `handlers` quebraria essa invariante (e provavelmente criaria um ciclo real, já que `handlers` já importa `services`). Resolvido extraindo as 3 funções para um novo pacote `backend/crypto/` (import path `fb_apu02/crypto`), importado tanto por `handlers` (3 arquivos atualizados: `sap_credentials.go`, `sap_credentials_test.go`, `erp_bridge.go` — 13 call sites, mecanicamente prefixados com `crypto.`) quanto por `services` (`sap_sync_trigger.go`). Nenhuma lógica de criptografia foi alterada — apenas movida. Testes de round-trip/nonce (`TestClientSecretEncryptionRoundTrip`, `TestClientSecretEncryptionUsesRandomNonce`) continuam passando sem alteração de comportamento.
- **Rate limit por empresa, não global ao processo:** o addendum do PRD diz "60 req/min por consumidor" — como cada empresa tem suas próprias credenciais SAP (Epic 1), um único pacer global penalizaria empresas sem relação entre si. Implementado `pacerForCompany(companyID)` com registro (`map[string]*callPacer` + mutex), isolando o orçamento de cada empresa — decisão de escopo não detalhada na story original, registrada aqui.
- **Token OAuth2 tratado como falha de credencial sempre que `GetToken` falha:** não há como o código atual distinguir com certeza "credencial inválida" de "erro transitório de rede" na resposta de `/token` (`GetToken` não expõe o status code, só uma mensagem de erro). Optei por classificar qualquer falha de token como `falha_credencial` (sem retry) — interpretação mais segura dado que retentar sem corrigir a credencial raramente ajuda; registrado como simplificação deliberada, não escondida.
- **Testes de retry/backoff usam `backoffSleep` (var substituível) para rodar instantaneamente**, mas o pacing (`pacerSleepFunc`) foi mantido **separado e real** — só o teste dedicado do pacer (`TestCallPacer_...`) depende de tempo real (~200ms), medido de propósito para provar que o pacing funciona de verdade, não apenas por contagem de chamadas.
- `logBatchSummary` foi adicionado para dar visibilidade mínima (contagem por `paymentStatus`) sem persistir nada — atende ao pedido da Task 2 de "parsear e logar/contar" sem invadir o escopo da Story 2.3 (persistência em `pagamentos_fornecedores`).
- Escopo mantido estritamente dentro da story: nenhuma linha foi gravada em `pagamentos_fornecedores`; a resposta do SAP é parseada e contada, não persistida.
- **Pós code-review (3 revisores adversariais):** achado mais relevante — divisão recursiva por HTTP 400 disparava a metade direita mesmo após a esquerda retornar `credFail`, uma chamada evitável ao SAP com um token já sabidamente inválido (contraria o espírito do AC #6). Também corrigidos: rate limit passou a ser isolado por `client_id` em vez de `company_id` (mais correto — o limite é por credencial, não por tenant); `base_url` malformada agora falha rápido sem gastar retries; `Retry-After` do SAP ganhou teto de 60s; corpo de resposta de erro passou a ser logado (antes era lido e descartado); e uma imprecisão de documentação foi corrigida (backoff "1s/2s/4s" declarado vs. "1s/2s" real com `maxRetries=3`, já que a 3ª tentativa esgota o orçamento antes de dormir 4s). 1 decisão (classificação de falha de token como `falha_credencial` incondicionalmente) foi deferida pelo usuário. 6 itens adicionais deferidos (ver `deferred-work.md`) — principalmente padrões pré-existentes movidos verbatim (`backend/crypto`) ou fora do escopo real desta story (reconciliação por item, Story 2.3).

### File List

- `backend/migrations/117_sap_sync_runs_falha_credencial.sql` (novo, revisado pós code-review — `NOT VALID` + `VALIDATE CONSTRAINT`)
- `backend/services/sap_payments_processor.go` (novo, revisado pós code-review — log de corpo de erro, teto no Retry-After, pacer por client_id, validação de base_url, short-circuit de credFail na divisão recursiva)
- `backend/services/sap_payments_processor_test.go` (novo, revisado pós code-review — 3 testes novos + 1 renomeado)
- `backend/crypto/crypto.go` (novo — `EncryptField`/`DecryptField`/`DecryptFieldWithFallback` movidos de `backend/handlers/crypto.go`)
- `backend/handlers/crypto.go` (removido — conteúdo movido para `backend/crypto/crypto.go`)
- `backend/handlers/sap_credentials.go` (editado — import `fb_apu02/crypto`, chamadas prefixadas)
- `backend/handlers/sap_credentials_test.go` (editado — import `fb_apu02/crypto`, chamadas prefixadas)
- `backend/handlers/erp_bridge.go` (editado — import `fb_apu02/crypto`, chamadas prefixadas)
- `backend/services/sap_sync_trigger.go` (editado — `TriggerSAPSync` estendida para trazer credenciais; `syncBukrs`/`syncBukrsSafe` com nova assinatura; chamada real a `ProcessSAPSync`; `finishRun` novo)
- `backend/services/sap_sync_trigger_test.go` (editado — `TestMain` novo, helper `insertSAPCredential`, testes existentes atualizados para a nova assinatura de `syncBukrs` e para fornecer `base_url`/`client_secret` válidos)

### Change Log

- 2026-07-11: Implementação completa da Story 2.2 — cliente HTTP para `SearchPayments` (lote de até 500, filtro de chave malformada), rate limiting, retry com backoff exponencial (429/500), redução automática de lote (400), falha de credencial sem retry (401/403), integração real em `syncBukrs` substituindo o placeholder da Story 2.1. Extração do pacote `crypto` (de `handlers` para um novo pacote compartilhado) para resolver um conflito de arquitetura não previsto na story original (services não pode importar handlers). 10 testes novos + verificação E2E manual contra o `sap-mock-server` real (3 cenários: 429, 500, sucesso). Sem regressões.
- 2026-07-11: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor) — 1 decisão resolvida (classificação de falha de token deferida), 8 patches aplicados (log de corpo de erro, migration NOT VALID/VALIDATE, teto no Retry-After, pacer por client_id em vez de company_id, validação rápida de base_url, short-circuit de credFail na divisão recursiva, correção de documentação do backoff, correção da contagem de testes), 3 testes novos, 6 itens deferidos.
