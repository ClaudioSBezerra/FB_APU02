---
baseline_commit: f13b4a78132afcdcdcf0e955dd8064f82fe320be
---

# Story 1.1: Cadastro de Credenciais e Mapeamento de BUKRS por Empresa

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a administrador do FB_APU02,
I want cadastrar credenciais OAuth2 do SAP (client_id/client_secret) e o(s) código(s) de empresa SAP (BUKRS) para uma empresa,
so that a futura sincronização automática (Epic 2) saiba com qual(is) empresa(s) SAP se comunicar e com quais credenciais.

Realiza FR-5 e FR-6 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Primeira story da Epic 1 ("Credenciais e Habilitação SAP por Empresa") — ver `_bmad-output/planning-artifacts/epics.md`.

## Acceptance Criteria

1. **Given** sou admin autenticado, **When** acesso a tela de credenciais SAP de uma empresa, **Then** posso inserir client_id, client_secret e um ou mais códigos BUKRS.
2. **Given** salvo as credenciais, **When** persistidas, **Then** o client_secret é criptografado antes de gravar no banco e **nunca** é retornado em texto plano por nenhuma rota de leitura (nem mascarado com últimos 4 dígitos — ver Dev Notes sobre por que o padrão de RFB/CGIBS não deve ser copiado aqui).
3. **Given** não sou admin, **When** tento acessar a tela ou a API, **Then** recebo 403.
4. **And** a migration correspondente roda e cria a tabela `sap_credentials` (client_id, client_secret criptografado, base_url, bukrs_list, company_id).
5. **Given** uma empresa sem nenhum BUKRS configurado, **When** qualquer sincronização futura (Epic 2) rodar para ela, **Then** nenhuma chamada ao SAP é feita (100% fluxo CSV) — este AC documenta o contrato de dados que a Epic 2 vai consumir; esta story só precisa garantir que "zero BUKRS" é um estado válido e persistível, não implementar a sincronização em si.

## Tasks / Subtasks

- [x] **Task 1 — Migration: tabela `sap_credentials`** (AC: #4)
  - [x] Criar `backend/migrations/115_sap_credentials.sql` (115 é o próximo número disponível — o último arquivo hoje é `114_abort_stuck_pending_rfb_requests.sql`)
  - [x] Colunas: `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`, `company_id UUID NOT NULL UNIQUE REFERENCES companies(id) ON DELETE CASCADE` (ver Dev Notes), `client_id TEXT NOT NULL`, `client_secret TEXT NOT NULL` (vai guardar o valor **já criptografado**, formato base64 do `EncryptField`), `base_url TEXT`, `bukrs_list TEXT[]` (array Postgres — permite N códigos por empresa, ver FR-6), `ativo BOOLEAN NOT NULL DEFAULT TRUE`, `created_at`/`updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
  - [x] `UNIQUE` já garantida na própria coluna `company_id` (uma credencial por empresa, igual a `cgibs_credentials`) — não precisa de índice adicional

- [x] **Task 2 — Backend: handlers Go** (AC: #1, #2, #3, #5)
  - [x] Criar `backend/handlers/sap_credentials.go`
  - [x] Struct `SAPCredential` com campos: `ID`, `CompanyID`, `ClientID`, `ClientSecretSet bool` (**não** incluir `ClientSecret` como string no struct de resposta — ver Dev Notes), `BaseURL`, `BukrsList []string`, `Ativo`, `CreatedAt`, `UpdatedAt`
  - [x] `GetSAPCredentialHandler(db *sql.DB) http.HandlerFunc` — GET: retorna a credencial sem o secret (campo booleano `client_secret_set`), seguindo o padrão de `FBTaxPasswordSet`/`OracleSenhaSet` em `backend/handlers/erp_bridge.go:29,32`
  - [x] `SaveSAPCredentialHandler(db *sql.DB) http.HandlerFunc` — POST: recebe `client_id`, `client_secret` (opcional — só atualiza se enviado, mesmo padrão condicional de `erp_bridge.go:183-203`), `base_url`, `bukrs_list` (array de strings); criptografa `client_secret` com `EncryptField()` (`backend/handlers/crypto.go:38`) **antes** do INSERT/UPDATE; UPSERT por `company_id` (`ON CONFLICT (company_id) DO UPDATE`, mesmo padrão de `cgibs_credentials.go:125-131`)
  - [x] Validações: `client_id` e ao menos 1 item em `bukrs_list` obrigatórios na criação; `client_secret` obrigatório apenas se não houver credencial existente (permite editar `base_url`/`bukrs_list` sem reenviar o secret)
  - [x] Registrar rotas em `backend/main.go` com `withAuth(handlers.GetSAPCredentialHandler, "admin")` e `withAuth(handlers.SaveSAPCredentialHandler, "admin")` — **usar `"admin"`, não `""`** (ver Dev Notes, divergência deliberada do padrão RFB/CGIBS/ERP Bridge)

- [x] **Task 3 — Frontend: nova página `SAPCredentials.tsx`** (AC: #1)
  - [x] Criar `frontend/src/pages/SAPCredentials.tsx`, modelo estrutural em `frontend/src/pages/RFBCredentials.tsx` (useState/useEffect/fetch, componentes `Card`/`Button`/`Input`/`Label` de `@/components/ui/*`)
  - [x] Campos do formulário: `client_id`, `client_secret` (input tipo password, vazio ao carregar), `base_url`, e um textarea com um BUKRS por linha para `bukrs_list` (sem componente de "tag list" pronto no design system — decisão: textarea simples)
  - [x] Exibir um indicador "credencial já configurada" quando `client_secret_set === true`, sem nunca tentar mostrar o valor
  - [x] Registrar rota em `frontend/src/App.tsx`, envolvida em `<AdminRoute>` (mesmo padrão de `/config/erp-bridge` e `/cgibs/credenciais` — não o padrão desguarnecido de `/rfb/credenciais`)
  - [x] Adicionar tab em `frontend/src/lib/navigation.ts`, dentro do módulo `config`: `{ label: 'Credenciais SAP', path: '/config/sap-credenciais', adminOnly: true }`

- [x] **Task 4 — Verificação manual**
  - [x] `go build ./...` sem erros
  - [x] `npx tsc -p tsconfig.app.json --noEmit` sem erros novos (erros pré-existentes em arquivos não relacionados a esta story, confirmados por não aparecerem nos meus arquivos)
  - [x] Testado fluxo completo **end-to-end contra Postgres local real** (não apenas unitário): migration 115 aplicada com sucesso (`\d sap_credentials` confirmado via psql); GET/POST sem token → 401; GET com token válido de role `user` → **403** (AC #3 confirmado); GET/POST com token de role `admin` → 200/201; `client_secret` confirmado **criptografado no banco** (valor base64 opaco, não o texto plano enviado) via `SELECT client_secret FROM sap_credentials`; GET subsequente confirma `client_secret_set: true` e o campo `client_secret` **ausente** da resposta JSON (nunca em texto plano, nem mascarado). Dados de teste removidos ao final (`DELETE FROM sap_credentials WHERE client_id = 'teste123'`).

### Review Findings

*(Revisão adversarial em 3 camadas — Blind Hunter, Edge Case Hunter, Acceptance Auditor — sobre o diff da Story 1.1)*

- [x] [Review][Decision] Log de auditoria para alteração de credenciais SAP — **resolvido: adicionar agora** (log simples via `log.Printf`, padrão já usado no projeto). Vira patch abaixo.
- [x] [Review][Decision] Cobertura de teste automatizado para os cenários E2E da Task 4 — **resolvido: aceitar a verificação manual documentada como suficiente**, mantendo a convenção atual do projeto (sem harness de teste com banco real). Nenhuma ação de código necessária.
- [x] [Review][Decision] AC #5 ("zero BUKRS é estado válido e persistível") — **resolvido: manter como está**, ausência de linha já cumpre o contrato do AC. Nenhuma ação de código necessária.

- [x] [Review][Patch] Panic não tratado em asserção de tipo do claim `user_id` [backend/handlers/sap_credentials.go — ambos os handlers] — corrigido: `userID, ok := claims["user_id"].(string)` com retorno 401 se `!ok`
- [x] [Review][Patch] Log de auditoria (log.Printf) ao salvar/atualizar credencial SAP — quem, empresa, quando [backend/handlers/sap_credentials.go — `SaveSAPCredentialHandler`] — corrigido: `log.Printf("[SAPCredentials] Credencial SAP salva por userID=%s companyID=%s (nova=%v)", ...)`
- [x] [Review][Patch] Condição de corrida (lost update) entre leitura do secret existente e o UPSERT [backend/handlers/sap_credentials.go:132-158] — corrigido: leitura (`SELECT ... FOR UPDATE`) e UPSERT agora ocorrem dentro da mesma transação (`db.Begin()`/`tx.Commit()`), serializando escritas concorrentes na mesma empresa
- [x] [Review][Patch] `bukrs_list` não deduplica códigos repetidos antes de persistir [backend/handlers/sap_credentials.go:114-120] — corrigido: dedup via `map[string]bool` antes de montar `cleanBukrs`; verificado end-to-end contra Postgres real (`["1000","1000","2000","2000","2000"]` → `{1000,2000}`)
- [x] [Review][Patch] Status HTTP sempre `201 Created`, mesmo em atualização [backend/handlers/sap_credentials.go:230] — corrigido: `200` quando `hasExisting == true`, `201` na criação; verificado end-to-end (criação → 201, atualização subsequente → 200)
- [x] [Review][Patch] Frontend exibe corpo de erro bruto (JSON cru) em vez de mensagem parseada [frontend/src/pages/SAPCredentials.tsx — `handleSave`] — corrigido: tenta `JSON.parse` e extrai `.error`, com fallback ao texto bruto se a resposta não for JSON (ex.: 401/405 em texto plano)
- [x] [Review][Patch] Textarea de BUKRS não trata lista colada separada por vírgula [frontend/src/pages/SAPCredentials.tsx — `handleSave`] — corrigido: `split(/[\n,]/)` aceita quebra de linha e/ou vírgula
- [x] [Review][Patch] `base_url` sem validação mínima de esquema (http/https) [backend/handlers/sap_credentials.go — `SaveSAPCredentialHandler`] — corrigido: rejeita com 400 se não começar com `http://`/`https://`; verificado end-to-end (`ftp://...` → 400 com mensagem clara)
- [x] [Review][Patch] Teste de criptografia não confirma nonce/IV aleatório [backend/handlers/sap_credentials_test.go] — corrigido: novo teste `TestClientSecretEncryptionUsesRandomNonce` confirma que duas criptografias do mesmo texto produzem ciphertexts diferentes

- [x] [Review][Defer] Usuário sem empresa associada recebe 500 genérico em vez de 403/404 [backend/handlers/sap_credentials.go] — deferred, pre-existing (mesmo padrão usado em RFB/CGIBS/todos os handlers via `GetEffectiveCompanyID` + `sanitizeDBErr`, não introduzido por esta story)
- [x] [Review][Defer] Sem limite de tamanho/quantidade nos campos de entrada (client_id, bukrs_list, etc.) [backend/handlers/sap_credentials.go] — deferred, endurecimento geral de baixa severidade dado que a rota já é admin-only

## Dev Notes

### ⚠️ Divergência crítica #1 — Criptografia real, não mascaramento

O PRD (FR-5) diz para seguir "o padrão já existente de `RFBCredentials`/`CGIBSCredentials`" — **mas esse padrão não criptografa nada**. Verificado em `backend/handlers/rfb_credentials.go` e `backend/handlers/cgibs_credentials.go`: o `client_secret` é gravado em texto plano no banco (`INSERT INTO rfb_credentials (..., client_secret, ...) VALUES (..., $4, ...)`, sem nenhuma chamada de criptografia) e apenas **mascarado na exibição** (`strings.Repeat("*", ...) + ultimos4chars`, ex. `rfb_credentials.go:70-71`). Isso não satisfaz a AC #2 desta story ("client_secret é criptografado... nunca retornado em texto plano").

O padrão correto a seguir é o de `backend/handlers/erp_bridge.go`, que já usa criptografia real via `backend/handlers/crypto.go`:
- `EncryptField(plaintext string) (string, error)` — AES-256-GCM, retorna base64 (nonce + ciphertext)
- `DecryptField(encoded string) (string, error)` — decripta
- `DecryptFieldWithFallback(encoded string) string` — decripta com fallback para dados legados não criptografados (não relevante aqui, pois `sap_credentials` é tabela nova, sem dado legado)
- Chave derivada de `ENCRYPTION_KEY` (env var), `crypto.go:16-34`

Uso real em produção: `erp_bridge.go:184` (`EncryptField(*req.FBTaxPassword)`) e `erp_bridge.go:137` (`DecryptFieldWithFallback(apiKey.String)`).

**Mais estrito que o próprio `erp_bridge.go`:** lá, `api_key` é decriptado e devolvido ao frontend (`erp_bridge.go:137,592`) — mas para senhas o padrão é nunca devolver o valor, só um booleano (`FBTaxPasswordSet`, `erp_bridge.go:29,128`). Como a AC #2 desta story exige "nunca retornado em texto plano por nenhuma rota de leitura", siga o padrão de senha (booleano `client_secret_set`), não o padrão de api_key (decriptar e devolver) nem o padrão de RFB/CGIBS (mascarar com últimos 4 dígitos — isso ainda expõe texto plano parcial).

### ⚠️ Divergência crítica #2 — Rota admin-only de verdade

Todas as rotas de credenciais existentes que servem de "padrão" são registradas com `withAuth(handler, "")` (qualquer usuário autenticado da empresa, não só admin):
- `backend/main.go:419,421,423,434` (RFB) — role `""`
- `backend/main.go:462,479` (CGIBS, mesmo padrão, não lido diretamente mas confirmado pela estrutura idêntica)
- `backend/main.go:558,561` (ERP Bridge Config) — role `""`

O `adminOnly: true` que essas telas têm em `frontend/src/lib/navigation.ts` é **apenas um gate de UI** (esconde a aba do menu) — a API por trás aceita qualquer usuário autenticado da empresa. Isso é uma característica conhecida do código atual, não um padrão a replicar.

A AC #3 desta story exige 403 real para não-admin. Portanto: registre as rotas de `sap_credentials` com `withAuth(handlers.XHandler, "admin")` — **literalmente a string `"admin"`**, o mesmo usado em `backend/main.go:316-325` (`/api/admin/*`). Isso é uma decisão deliberada de ir além do padrão observado nas 3 features análogas, não um erro de leitura do código existente.

### Padrão de referência para o fluxo de token OAuth2 (útil para a Story 1.2, futura)

Embora esta story não implemente o "testar conexão" (isso é Story 1.2, ainda em backlog), o padrão de client_credentials OAuth2 já existe no código e deve ser reaproveitado quando aquela story chegar: `backend/services/rfb.go:120-165` (`RFBClient.GetToken`) — `POST` form-encoded com `grant_type=client_credentials`, `req.SetBasicAuth(clientID, clientSecret)`, cache de token por `client_id`. Não há hoje nenhum botão "testar conexão" em nenhuma tela do sistema — não existe precedente de UI para copiar ali, será funcionalidade nova.

### Estrutura de dados: `bukrs_list` como array

FR-6 permite N BUKRS por empresa (`[ASSUMPTION]` do PRD, ver `prd.md` Glossário/§14). Modele como `TEXT[]` do Postgres em vez de uma tabela de junção separada — mais simples para o volume esperado (poucos BUKRS por empresa) e compatível com o padrão de arrays já usado no projeto (verificar se há precedente de array em outras migrations; se preferir uma tabela `sap_credentials_bukrs (credential_id, bukrs)` por normalização, é uma escolha de design válida — documentar a decisão tomada no Dev Agent Record ao final).

### Tipo de `company_id`: `UUID REFERENCES companies(id)` — já confirmado

Há inconsistência pré-existente no projeto: `rfb_credentials.company_id` é `UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE` (`051_create_rfb_credentials.sql:4`), mas `cgibs_credentials.company_id` é `TEXT NOT NULL UNIQUE` sem FK (`102_cgibs_tables.sql:7`) — a tabela CGIBS é o outlier, não o padrão predominante. Para `sap_credentials`, use `company_id UUID NOT NULL UNIQUE REFERENCES companies(id) ON DELETE CASCADE`, seguindo o padrão de `rfb_credentials` (mais correto — `GetEffectiveCompanyID` retorna `string`, que o driver `lib/pq` converte transparentemente de/para `UUID` nas queries parametrizadas, sem necessidade de tratamento especial).

### Project Structure Notes

- Novo arquivo backend: `backend/handlers/sap_credentials.go` (segue convenção de arquivo por domínio, ex. `cgibs_credentials.go`, `rfb_credentials.go`)
- Nova migration: `backend/migrations/115_sap_credentials.sql`
- Novo arquivo frontend: `frontend/src/pages/SAPCredentials.tsx`
- Edições: `backend/main.go` (registro de rotas), `frontend/src/App.tsx` (nova rota), `frontend/src/lib/navigation.ts` (nova tab em `config`)
- Nenhum arquivo existente de RFB/CGIBS/ERP Bridge deve ser modificado por esta story — apenas lido como referência de padrão

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 1] — Epic goal e story original
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md#FR-5] — requisito de credenciais criptografadas, admin-only
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md#FR-6] — mapeamento company_id ↔ BUKRS
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/addendum.md#B] — esboço de arquitetura (`sap_credentials`, campos client_id/client_secret criptografado/base_url/bukrs_list)
- [Source: backend/handlers/crypto.go] — `EncryptField`/`DecryptField`/`DecryptFieldWithFallback`
- [Source: backend/handlers/erp_bridge.go:19-36,128,137,183-203] — padrão real de uso de criptografia + booleano `_set`
- [Source: backend/handlers/rfb_credentials.go, backend/handlers/cgibs_credentials.go] — padrão estrutural de handler (UPSERT, validação, mascaramento — **não copiar o mascaramento nem a ausência de criptografia**)
- [Source: backend/main.go:276-297,316-325,411-435] — `withDB`/`withAuth` helpers e exemplos de registro admin vs. não-admin
- [Source: backend/services/rfb.go:120-165] — padrão OAuth2 client_credentials (relevante para Story 1.2 futura)
- [Source: backend/migrations/102_cgibs_tables.sql, backend/migrations/051_create_rfb_credentials.sql] — estrutura de migration de referência
- [Source: frontend/src/pages/RFBCredentials.tsx] — padrão estrutural de página de credenciais
- [Source: frontend/src/lib/navigation.ts:103-117] — módulo `config`, padrão `adminOnly`

## Dev Agent Record

### Agent Model Used

Claude Fable 5 (claude-fable-5)

### Debug Log References

- `go build ./...` — sem erros
- `go test ./...` — `ok fb_apu02/handlers`, `ok fb_apu02/middleware`, sem regressões
- `npx tsc -p tsconfig.app.json --noEmit` — nenhum erro novo introduzido (erros pré-existentes em arquivos não tocados por esta story)
- Verificação E2E contra Postgres local (`fiscal_db`): migration aplicada, 401/403/200/201 confirmados nas rotas reais, criptografia do `client_secret` confirmada via `psql`, dado de teste removido ao final
- Pós-revisão (code review): `go build`/`go vet`/`go test` limpos após os 9 patches; `tsc --noEmit` sem erros novos; reverificado end-to-end contra Postgres real: dedup de BUKRS (`["1000","1000","2000","2000","2000"]` → `{1000,2000}`), status 201 (criação) vs 200 (atualização), e rejeição 400 de `base_url` com esquema inválido (`ftp://...`)

### Completion Notes List

- Duas divergências deliberadas do "padrão existente" citado no PRD, documentadas em Dev Notes e confirmadas por leitura do código real antes de implementar: (1) `client_secret` é de fato criptografado via `EncryptField`/`DecryptField` (`crypto.go`), diferente de RFB/CGIBS que só mascaram texto plano; (2) rotas registradas com `withAuth(..., "admin")` de verdade, diferente de RFB/CGIBS/ERP Bridge que usam `""` (qualquer usuário autenticado) apesar do `adminOnly: true` no menu.
- `company_id` de `sap_credentials` segue o padrão de `rfb_credentials` (`UUID REFERENCES companies(id)`), não o de `cgibs_credentials` (`TEXT` sem FK, outlier identificado durante a análise).
- `client_secret_set` (booleano) substitui o padrão de mascaramento com últimos-4-dígitos usado em RFB/CGIBS — mais estrito, alinhado à AC #2 ("nunca retornado em texto plano").
- `bukrs_list` modelado como `TEXT[]` do Postgres (via `pq.Array`), não uma tabela de junção separada — decisão de simplicidade dado o baixo volume esperado de códigos por empresa.
- Rota de teste de conexão (Story 1.2) não foi implementada aqui — fora do escopo desta story, corretamente adiado.
- **Pós code-review (3 revisores adversariais):** 3 decisões resolvidas com o usuário (log de auditoria → adicionar agora; cobertura de teste E2E → aceitar verificação manual; AC#5 → manter como está) e 9 patches aplicados: panic de type assertion corrigido em ambos os handlers; condição de corrida (lost update) eliminada via transação com `SELECT ... FOR UPDATE`; dedup de `bukrs_list`; status HTTP 200/201 corretos; log de auditoria; parsing de erro no frontend; aceitação de vírgula no textarea de BUKRS; validação de esquema de `base_url`; teste de nonce aleatório na criptografia. 2 achados deferidos (pré-existentes, não introduzidos por esta story) registrados em `deferred-work.md`.

### File List

- `backend/migrations/115_sap_credentials.sql` (novo)
- `backend/handlers/sap_credentials.go` (novo, revisado pós code-review)
- `backend/handlers/sap_credentials_test.go` (novo, revisado pós code-review)
- `backend/main.go` (editado — registro de rotas `/api/sap/credentials`)
- `frontend/src/pages/SAPCredentials.tsx` (novo, revisado pós code-review)
- `frontend/src/App.tsx` (editado — import + rota `/config/sap-credenciais`)
- `frontend/src/lib/navigation.ts` (editado — tab "Credenciais SAP" no módulo `config`)
- `_bmad-output/implementation-artifacts/deferred-work.md` (novo — 2 achados de code review deferidos)

### Change Log

- 2026-07-11: Implementação completa da Story 1.1 — migration, handlers Go com criptografia real e admin-gating, página frontend, testes unitários, verificação E2E contra banco real.
- 2026-07-11: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor) — 3 decisões resolvidas, 9 patches aplicados (panic, race condition, dedup, status HTTP, log de auditoria, parsing de erro, parsing de BUKRS, validação de base_url, teste de nonce), 2 itens deferidos.
