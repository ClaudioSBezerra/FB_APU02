---
baseline_commit: f13b4a78132afcdcdcf0e955dd8064f82fe320be
---

# Story 1.2: Teste de Conexão com Credenciais SAP

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a administrador do FB_APU02,
I want testar a conexão com as credenciais SAP salvas,
so that eu confirme que a integração está corretamente configurada antes de depender dela para a sincronização automática (Epic 2).

Realiza FR-5 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Segunda story da Epic 1 — ver `_bmad-output/planning-artifacts/epics.md`. Depende da Story 1.1 (cadastro de credenciais) já concluída.

## Acceptance Criteria

1. **Given** credenciais válidas salvas, **When** clico em "testar conexão", **Then** o sistema executa uma chamada OAuth2 mínima (`client_credentials`) ao SAP e reporta sucesso/falha.
2. **Given** credenciais inválidas (ou SAP indisponível/URL errada), **When** testo a conexão, **Then** recebo mensagem clara de falha, sem vazar detalhes internos (stack trace, URL completa, etc.).
3. **And** o teste de conexão nunca persiste dado de negócio — não grava em `pagamentos_fornecedores` nem em nenhuma outra tabela além de, no máximo, ler `sap_credentials`.
4. **Given** nenhuma credencial cadastrada para a empresa, **When** clico em "testar conexão", **Then** recebo mensagem clara indicando que não há credencial para testar (não uma falha genérica de conexão).

## Tasks / Subtasks

- [x] **Task 1 — Serviço Go de obtenção de token OAuth2 SAP** (AC: #1, #2)
  - [x] Criar `backend/services/sap_payments.go`
  - [x] Função `GetToken(baseURL, clientID, clientSecret string) (string, error)` — `POST {baseURL}/token`, form-encoded `grant_type=client_credentials`, `req.SetBasicAuth(clientID, clientSecret)`, mesmo padrão de `services/rfb.go:120-165` (`RFBClient.GetToken`), sem cache de token (chamada única de teste)
  - [x] `http.Client{Timeout: 10 * time.Second}`
  - [x] Erros retornados como `error` com mensagem detalhada (para log), nunca expostos diretamente ao cliente HTTP

- [x] **Task 2 — Handler Go de teste de conexão** (AC: #1, #2, #3, #4)
  - [x] Criar `TestSAPConnectionHandler(db *sql.DB) http.HandlerFunc` em `backend/handlers/sap_credentials.go`
  - [x] Auth com `userID, ok := claims["user_id"].(string)` (padrão corrigido da Story 1.1, sem assert direto)
  - [x] `GetEffectiveCompanyID` → busca `client_id`, `client_secret` (criptografado), `base_url` em `sap_credentials`
  - [x] Sem credencial cadastrada → 400 "Nenhuma credencial SAP cadastrada para esta empresa" (AC #4)
  - [x] `base_url` vazio → 400 "Base URL não configurada"
  - [x] `DecryptField` usado apenas internamente para montar a chamada OAuth2 — nunca serializado em nenhuma resposta
  - [x] Sucesso → `200 {"success": true, "message": "Conexão bem-sucedida"}`; falha → `502` com mensagem genérica, erro real apenas em `log.Printf`
  - [x] Handler é somente leitura (`SELECT` em `sap_credentials`) — nenhuma escrita (AC #3)
  - [x] Rota `POST /api/sap/credentials/test` registrada em `backend/main.go` com `withAuth(handlers.TestSAPConnectionHandler, "admin")`

- [x] **Task 3 — Frontend: botão "Testar Conexão"** (AC: #1, #2, #4)
  - [x] Em `frontend/src/pages/SAPCredentials.tsx`, botão "Testar Conexão" adicionado na visualização não-editável (ao lado de "Editar", quando `credential` existe)
  - [x] Estado `testingConnection` (loading) e exibição do resultado via o mesmo padrão de `message` já usado na página
  - [x] Parsing de erro JSON reaproveitado (extrai `.error`/`.message`, fallback ao texto bruto)

- [x] **Task 4 — Testes e verificação**
  - [x] Testes unitários em `backend/handlers/sap_credentials_test.go`: `TestSAPConnectionHandlerUnauthorized` (401) e `TestSAPConnectionHandlerMethodNotAllowed` (405)
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões (8 testes passando em `fb_apu02/handlers`)
  - [x] `npx tsc -p tsconfig.app.json --noEmit` sem erros novos (26 erros pré-existentes, mesmo baseline de antes desta story)
  - [x] **Verificação end-to-end contra o `sap-mock-server` + Postgres real:** subi o mock (`go run .`) e o backend real; (1) testei conexão sem credencial cadastrada → 400 "Nenhuma credencial SAP cadastrada" (AC #4); (2) cadastrei credencial com `base_url=http://localhost:8090` → testei conexão → **200 sucesso, token OAuth2 real obtido do mock** (AC #1); (3) atualizei para `client_id=erro_401` → testei conexão → **502 com mensagem genérica**, sem vazar detalhe interno (AC #2); (4) confirmei via `psql` que `pagamentos_fornecedores` permaneceu com 0 linhas e `sap_credentials.updated_at` não mudou entre os testes de conexão — handler é 100% somente leitura (AC #3). Dados de teste removidos ao final.
  - [x] ⚠️ **Nota de code review:** a verificação (1) acima foi feita batendo direto na API (curl), não pelo botão da tela — o code review encontrou que, na implementação original, o botão "Testar Conexão" só existia na visualização de credencial já cadastrada, tornando o cenário do AC #4 irreproduzível pelo produto real. Corrigido — ver Review Findings.

### Review Findings

*(Revisão adversarial em 3 camadas — Blind Hunter, Edge Case Hunter, Acceptance Auditor — sobre o código novo desta story)*

- [x] [Review][Decision] Mitigação de SSRF via `base_url` — **resolvido: deferir por ora (opção C)**. `base_url` só é gravável por admin já autenticado da própria empresa (tenants são subsidiárias do mesmo grupo Ferreira Costa, não clientes externos arbitrários); risco residual aceito no momento, dado também que bloquear IPs privados quebraria o fluxo de desenvolvimento com o `sap-mock-server` (`http://localhost:8090`). Revisitar se o modelo de tenants mudar (ex.: onboarding de empresas fora do grupo) ou antes de produção.

- [x] [Review][Patch] Botão "Testar Conexão" inatingível quando não há credencial cadastrada (AC #4 irreproduzível pela UI) [frontend/src/pages/SAPCredentials.tsx] — corrigido: botão adicionado também no formulário de primeiro cadastro/edição, sempre testando a credencial já salva no servidor (não os valores digitados e ainda não salvos)
- [x] [Review][Patch] Sem guarda contra duplo clique/duplo submit em `handleTestConnection` [frontend/src/pages/SAPCredentials.tsx] — corrigido: `if (testingConnection) return;` no início do handler
- [x] [Review][Patch] `io.ReadAll` sem limite de tamanho no corpo de resposta do `/token` [backend/services/sap_payments.go] — corrigido: `io.LimitReader(resp.Body, 1<<20)` (1 MiB)
- [x] [Review][Patch] Corpo de resposta externo logado sem truncamento/sanitização [backend/services/sap_payments.go] — corrigido: `truncateForLog` corta em 500 caracteres antes de incluir em mensagens de erro/log
- [x] [Review][Patch] `base_url` concatenado com string crua em vez de `net/url` [backend/services/sap_payments.go] — corrigido: `url.Parse` + `.JoinPath("token")`; revalidado end-to-end contra o `sap-mock-server` com `base_url` terminando em barra — conexão bem-sucedida

- [x] [Review][Defer] Cobertura de teste automatizado não exercita os caminhos comportamentais dos ACs #2/#3/#4 (`sql.ErrNoRows`, falha de `GetToken`, ausência de escrita) — apenas verificação manual E2E [backend/handlers/sap_credentials_test.go] — deferred, consistente com a política já aceita na Story 1.1 (verificação manual documentada é suficiente, sem harness de teste com banco/HTTP real)
- [x] [Review][Defer] `token_type` da resposta OAuth2 não validado (só `access_token` não-vazio) [backend/services/sap_payments.go] — deferred, informativo — o token é descartado logo após o teste, sem uso downstream nesta story
- [x] [Review][Defer] `http.NewRequest` em vez de `NewRequestWithContext` (sem propagação de cancelamento) [backend/services/sap_payments.go] — deferred, baixo valor dado que o timeout de 10s já limita o pior caso
- [x] [Review][Defer] `client_secret` malformado/não decriptável não orienta o usuário a recadastrar [backend/handlers/sap_credentials.go] — deferred, cenário de baixa probabilidade (exige linha corrompida no banco), UX de baixa prioridade
## Dev Notes

### Aprendizados da Story 1.1 (mesma epic, já concluída)

- **Panic de type assertion:** a Story 1.1 teve um achado de code review sobre `claims["user_id"].(string)` sem `ok` — **já corrigido lá e o padrão correto deve ser usado desde o início aqui**: `userID, ok := claims["user_id"].(string); if !ok || userID == "" { ... 401 ... }`.
- **Nunca expor segredo:** a Story 1.1 estabeleceu que `client_secret` nunca é retornado ao frontend, nem mascarado. Esta story **decripta o secret no backend para uso interno** (montar a chamada OAuth2) — isso é diferente e permitido: o valor decriptado nunca deve ser serializado em nenhuma resposta HTTP, só usado internamente para autenticar contra o SAP.
- **Erros nunca vazam detalhes internos:** seguir o mesmo padrão de `sanitizeDBErr` — mensagem genérica ao cliente, log detalhado no servidor.
- **Admin-only de verdade:** `withAuth(handler, "admin")`, mesmo padrão da Story 1.1 (não o `""` usado em RFB/CGIBS/ERP Bridge).

### Ambiente de teste: `sap-mock-server`

A API SAP real ainda não existe (spec técnica em elaboração pelo time FI/Basis). Foi criado um simulador em `sap-mock-server/` (raiz do projeto, fora de `backend/` — ferramenta de desenvolvimento, não código de produção) que replica o contrato exato da spec: `POST /token` (OAuth2 client_credentials), além de `PaymentByDFe`/`SearchPayments` (para uso futuro da Epic 2). Ver `sap-mock-server/README.md` para detalhes completos.

**Para esta story:** o mock aceita qualquer `client_id`/`client_secret` não vazios como válidos, **exceto** `client_id = "erro_401"`, que força uma falha de autenticação (401) — usar esse valor para testar o caminho de falha (AC #2) de forma determinística.

### Padrão de referência: `RFBClient.GetToken`

`backend/services/rfb.go:120-165` já implementa exatamente o fluxo OAuth2 `client_credentials` necessário aqui (form-encoded, `SetBasicAuth`, parse de `access_token`). A única diferença: RFB cacheia o token entre chamadas (necessário porque o fluxo de apuração faz múltiplas chamadas com o mesmo token); o teste de conexão desta story é uma chamada única e não precisa de cache — manter `GetToken` simples aqui, sem estado global.

### Project Structure Notes

- Novo arquivo: `backend/services/sap_payments.go` (primeira peça do motor de sincronização da Epic 2 — reaproveitado lá)
- Editado: `backend/handlers/sap_credentials.go` (novo handler `TestSAPConnectionHandler`)
- Editado: `backend/handlers/sap_credentials_test.go` (novo teste)
- Editado: `backend/main.go` (nova rota)
- Editado: `frontend/src/pages/SAPCredentials.tsx` (botão + estado)

### References

- [Source: _bmad-output/implementation-artifacts/1-1-cadastro-de-credenciais-e-mapeamento-de-bukrs-por-empresa.md] — story anterior, mesma epic, aprendizados de code review
- [Source: _bmad-output/planning-artifacts/epics.md#Epic 1] — AC originais da Story 1.2
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md#FR-5] — "testar conexão... reporta sucesso/falha sem persistir nenhum dado de negócio"
- [Source: backend/services/rfb.go:120-165] — padrão OAuth2 client_credentials a reaproveitar
- [Source: backend/handlers/crypto.go] — `DecryptField`
- [Source: backend/handlers/sap_credentials.go] — handlers e auth pattern já corrigidos na Story 1.1
- [Source: sap-mock-server/README.md] — contrato e comportamento do simulador usado para verificação E2E desta story

## Dev Agent Record

### Agent Model Used

Claude Fable 5 (claude-fable-5)

### Debug Log References

- `go build ./...`, `go vet ./...`, `go test ./...` — limpos, 8 testes passando em `fb_apu02/handlers`
- `npx tsc -p tsconfig.app.json --noEmit` — sem erros novos
- Verificação E2E contra `sap-mock-server` (novo, criado nesta sessão) + Postgres real (`fiscal_db`): fluxo completo de sucesso e falha confirmado, dados de teste removidos ao final
- Pós-revisão (code review): `go build`/`go vet`/`go test` limpos após os 5 patches; `tsc --noEmit` sem erros novos; revalidado end-to-end contra o mock: `base_url` com barra final (`http://localhost:8090/`) confirmado funcionando com a nova montagem via `net/url.JoinPath`

### Completion Notes List

- Esta story foi a primeira a consumir o `sap-mock-server` (criado sob demanda do usuário para destravar o desenvolvimento enquanto a API real do SAP não existe) — validou que o mock replica o contrato OAuth2 corretamente o suficiente para um teste de conexão real de ponta a ponta.
- `services/sap_payments.go` é a primeira peça do motor de sincronização da Epic 2 — `GetToken` foi implementado propositalmente sem cache (diferente de `RFBClient.GetToken`), já que esta story só precisa de uma chamada única; a Epic 2 pode adicionar cache por cima da mesma função se o volume de chamadas justificar.
- Erro de conexão nunca vaza detalhe interno ao cliente (nem a URL, nem a mensagem exata do SAP/mock) — apenas logado no servidor (e agora truncado, ver code review), seguindo o mesmo princípio de `sanitizeDBErr` já estabelecido no projeto.
- Reaproveitado o padrão de auth corrigido na Story 1.1 (`userID, ok := claims["user_id"].(string)`) desde o início, evitando reintroduzir o mesmo problema que a revisão de código pegou na story anterior.
- **Pós code-review (3 revisores adversariais):** achado crítico real do Acceptance Auditor — o botão "Testar Conexão" só existia na visualização de credencial já cadastrada, tornando o AC #4 (testar sem nenhuma credencial salva) irreproduzível pela UI real, apesar do Dev Agent Record ter documentado essa verificação via `curl` direto na API. Corrigido: botão agora também aparece no formulário de primeiro cadastro. SSRF via `base_url` (confirmado independentemente por 2 revisores) foi deferido por decisão do usuário — ver `deferred-work.md` — dado que `base_url` só é gravável por admin já autenticado de uma empresa do próprio grupo, e uma mitigação rígida quebraria o fluxo de dev com o `sap-mock-server`. Demais 5 patches aplicados: guarda de duplo-clique, limite de tamanho de resposta, truncamento de log, e montagem de URL via `net/url` em vez de concatenação de string crua.

### File List

- `backend/services/sap_payments.go` (novo, revisado pós code-review)
- `backend/handlers/sap_credentials.go` (editado — novo handler `TestSAPConnectionHandler`)
- `backend/handlers/sap_credentials_test.go` (editado — 2 novos testes)
- `backend/main.go` (editado — rota `/api/sap/credentials/test`)
- `frontend/src/pages/SAPCredentials.tsx` (editado, revisado pós code-review — botão "Testar Conexão" + estado + handler)
- `_bmad-output/implementation-artifacts/deferred-work.md` (editado — 5 achados de code review desta story adicionados)

### Change Log

- 2026-07-11: Implementação completa da Story 1.2 — serviço de token OAuth2, handler de teste de conexão, botão frontend, testes unitários, verificação E2E contra o `sap-mock-server` recém-criado.
- 2026-07-11: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor) — 1 decisão resolvida (SSRF deferido), 5 patches aplicados (botão inatingível no AC #4, guarda de duplo-clique, limite de tamanho, truncamento de log, montagem de URL via net/url), 4 itens deferidos.
