---
title: 'Login via SSO Keycloak (Ferreira Costa) — opção paralela ao login por senha'
type: 'feature'
created: '2026-08-05'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: 'ce72b09'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O Marlos (gerente de TI da Ferreira Costa) pediu suporte a login via SSO corporativo (Keycloak) no FB_APU02. Hoje só existe login por e-mail/senha (`LoginHandler`, JWT próprio access+refresh).

**Approach:** Adicionar Keycloak como opção PARALELA — login atual continua 100% inalterado. Fluxo: frontend completa OIDC/PKCE direto contra o Keycloak (`https://iam.fcxlabs.com/realms/ferreiracosta`), recebe o access token do Keycloak, envia pra um endpoint novo (`POST /api/auth/sso/keycloak`) que valida esse token via JWKS (usando os templates da skill `keycloak-identity-react-go`, adaptados: zap→`log` stdlib), extrai o e-mail, busca o usuário existente em `users` por e-mail (reaproveitando a lógica de `LoginHandler`, extraída em função compartilhada), e emite os MESMOS tokens próprios do FB_APU02 (JWT access 30min + refresh cookie) — nunca expõe o token do Keycloak ao resto do sistema. O frontend chama `useAuth().login(data)` com a resposta, exatamente como no login normal — zero mudança em `AuthContext`, `GetUserIDFromContext`, `GetEffectiveCompanyID` ou qualquer autorização existente.

## Boundaries & Constraints

**Always:**
- Login por senha (`LoginHandler`, `/api/auth/login`) continua funcionando exatamente como hoje, sem nenhuma alteração de comportamento.
- E-mail do Keycloak sem usuário correspondente em `users` → erro claro (401/404), NUNCA cria usuário automaticamente.
- Checks de `is_blocked` e trial expirado se aplicam igualmente ao login via Keycloak (mesma regra de negócio, não é bypassada).
- Token do Keycloak nunca é persistido/exposto ao frontend além do uso imediato de troca — depois da troca, a sessão é 100% o token próprio do FB_APU02.
- `azp` (authorized party) é validado contra uma allowlist de client IDs (`IAM_ALLOWED_CLIENT_IDS`) — nunca `aud` (Keycloak não garante `aud == client_id`).
- Endpoints Keycloak usados são sempre `{realm-url}/protocol/openid-connect/{auth,token,certs}` — nunca paths achatados.

**Ask First:** Antes de rodar os `curl` que criam/configuram o client no Keycloak real (usando a credencial de automação da skill), PARAR e confirmar explicitamente com o usuário — mesmo com a spec já aprovada. Isso é uma ação em infraestrutura compartilhada da empresa, fora do escopo de "aprovar a spec".

**Never:** Não aplicar `IAMAuthMiddleware` como middleware global (`router.Use`) — só protege o endpoint novo de troca. Não tocar em `AuthGuard`/rotas protegidas existentes (`ProtectedRoute` em `App.tsx` continua sendo o único gate de rotas, alimentado pelo mesmo `AuthContext` de sempre).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Login Keycloak com e-mail existente | Token Keycloak válido, e-mail bate com `users` | Resposta idêntica ao login normal (token, user, environment, group, company) | N/A |
| Login Keycloak com e-mail não cadastrado | Token Keycloak válido, e-mail sem linha em `users` | 401 "Usuário não encontrado no FB_APU02" | Não cria usuário |
| Token Keycloak inválido/expirado/assinatura errada | JWT malformado ou expirado | 401, mesmo formato de erro do `IAMAuthMiddleware` | N/A |
| `azp` fora da allowlist | Token de outro client Keycloak | 401 "client não autorizado" | N/A |
| Usuário bloqueado ou trial expirado | E-mail bate, mas `is_blocked=true` ou trial vencido | Mesmo erro que o login por senha já retorna hoje | Regra de negócio compartilhada |

</frozen-after-approval>

## Code Map

- `backend/handlers/auth.go` -- extrair de `LoginHandler`: (1) `fetchUserByEmail(db, ctx, email) (user User, passwordHash string, isBlocked bool, err error)` (SELECT já existente, linhas ~633-636); (2) `finishLogin(db *sql.DB, w http.ResponseWriter, r *http.Request, user User, isBlocked bool)` (blocked/trial check, GenerateToken, refresh cookie, resolução de environment/group/company com auto-provisioning, encode de `AuthResponse` — linhas ~661-818). `LoginHandler` passa a chamar as duas.
- `backend/iam/iam_key_provider.go`, `iam_jwks_client.go`, `iam_auth_middleware.go` -- portar dos templates da skill (`~/.claude/plugins/marketplaces/fc-skills/plugins/keycloak-identity-react-go/skills/keycloak-identity-react-go/templates/backend-go/`), trocando todo uso de `go.uber.org/zap` por `log.Printf("[iam] ...")` da stdlib (5 call sites mapeados: 3 em `iam_jwks_client.go`, 2 em `iam_auth_middleware.go`) e removendo o parâmetro `*zap.Logger` das assinaturas. Adicionar extração da claim `email` no middleware (novo `ContextKeyIAMEmail`), já que os templates só expõem `sub`/`azp` por padrão e o handler novo precisa do e-mail.
- `backend/handlers/auth_sso.go` (novo) -- `KeycloakSSOHandler(db *sql.DB) http.HandlerFunc`, `POST /api/auth/sso/keycloak`, body `{"access_token":"..."}`. Envolvido por `iam.IAMAuthMiddleware` (só nesta rota). Lê e-mail do context, chama `fetchUserByEmail` + `finishLogin`.
- `backend/main.go` -- instanciar `jwksClient := iam.NewJWKSClient(os.Getenv("IAM_BASE_URL"), "", time.Hour, false)`; registrar rota `/api/auth/sso/keycloak` envolta em `iam.IAMAuthMiddleware(jwksClient, iamBaseURL, allowedClientIDs)`. Ler `IAM_ALLOWED_CLIENT_IDS` (CSV) e `IAM_BASE_URL` do ambiente.
- `frontend/src/lib/keycloak/buildLoginUrl.ts` -- portar do template quase sem mudança (PKCE + monta URL de authorize). Precisa `npm i @noble/hashes`.
- `frontend/src/lib/keycloak/handleCallback.ts` -- REESCRITO (não é cópia do template): valida `state`/`verifier` da `sessionStorage`, troca `code` por token do Keycloak (POST direto no `/protocol/openid-connect/token`), envia o `access_token` recebido pro backend novo, retorna o JSON de resposta (mesmo shape do login normal) pro chamador — não usa `tokenStore`/`userStore` do template (desnecessários, ver Design Notes).
- `frontend/src/pages/AuthCallback.tsx` (novo) -- chama `handleCallback()`, depois `useAuth().login(data)`, navega pra `/`. Erro → navega pra `/auth/error`. Sem Mantine (usa loading state simples, sem precisar portar `Center`/`Loader`/`Stack`/`Text` do template).
- `frontend/src/pages/AuthError.tsx` (novo) -- tela de erro amigável, usando `@/components/ui/card` e `@/components/ui/button` (shadcn, já usados no projeto) em vez dos componentes Mantine do template.
- `frontend/src/pages/Login.tsx` -- adicionar botão "Entrar com SSO Ferreira Costa" chamando `buildLoginUrl()` e redirecionando (`window.location.href`).
- `frontend/src/App.tsx` -- nova rota pública `/auth/callback` → `<AuthCallback />` (fora de `ProtectedRoute`).
- `.env.example` (backend e frontend, ou onde já documentado) -- `IAM_BASE_URL`, `IAM_ALLOWED_CLIENT_IDS` (backend); `VITE_IAM_BASE_URL`, `VITE_IAM_CLIENT_ID`, `VITE_IAM_REDIRECT_URI`, `VITE_IAM_SCOPES` (frontend).

## Tasks & Acceptance

**Execution:**
- [ ] `backend/handlers/auth.go` -- extrair `fetchUserByEmail`/`finishLogin` de `LoginHandler` -- permite reaproveitar 100% da lógica pós-autenticação no fluxo Keycloak
- [ ] `backend/iam/*.go` -- portar os 3 arquivos da skill, trocar zap por `log` stdlib, adicionar extração de e-mail -- middleware de validação JWKS/JWT do Keycloak
- [ ] `backend/handlers/auth_sso.go` -- novo handler de troca de token -- ponte entre identidade Keycloak e sessão própria do FB_APU02
- [ ] `backend/main.go` -- registrar rota + instanciar JWKS client + ler env vars -- liga o endpoint novo
- [ ] `frontend/src/lib/keycloak/buildLoginUrl.ts` + `npm i @noble/hashes` -- gera URL de login PKCE
- [ ] `frontend/src/lib/keycloak/handleCallback.ts` -- troca code→token Keycloak→sessão FB_APU02
- [ ] `frontend/src/pages/AuthCallback.tsx`, `AuthError.tsx` -- telas do fluxo, shadcn em vez de Mantine
- [ ] `frontend/src/pages/Login.tsx`, `frontend/src/App.tsx` -- botão de entrada + rota `/auth/callback`
- [ ] Env vars documentadas em `.env.example`

**Acceptance Criteria:**
- Given um usuário com e-mail cadastrado em `users`, when ele completa login via Keycloak, then recebe sessão FB_APU02 idêntica à de um login por senha (mesmo `environment`/`group`/`company`).
- Given um e-mail do Keycloak sem usuário correspondente, when a troca é tentada, then recebe erro claro, sem criar usuário novo.
- Given o login por senha, when usado normalmente, then continua funcionando sem nenhuma diferença de comportamento.

## Design Notes

Dos 9 arquivos frontend do template, só 2 são portados quase como estão (`buildLoginUrl.ts`) ou reescritos (`handleCallback.ts`); `tokenStore.ts`, `userStore.ts`, `refreshToken.ts`, `logout.ts` e `AuthGuard.tsx` são deliberadamente **não portados** — todos existem no template pra sustentar um app onde o Keycloak É a única fonte de sessão (token vive na SPA, precisa refresh próprio, guarda de rota própria). Nesta integração PARALELA, a sessão pós-login é 100% `AuthContext` (já existe, já tem refresh/guarda/logout via `ProtectedRoute`) — o token do Keycloak só vive o tempo de uma chamada. Portar esses 5 arquivos seria manter dois sistemas de sessão paralelos sem necessidade.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./handlers/... ./iam/...` -- expected: sem erros
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json` -- expected: sem erros novos nos arquivos tocados

**Manual checks (if no CLI):**
- Sem ambiente Keycloak de teste isolado nesta sessão — o realm real (`ferreiracosta`) é produção. Testar o fluxo completo exige o client já provisionado (Passo Ask First, ainda pendente) e um usuário Ferreira Costa real com e-mail cadastrado no FB_APU02. Combinar com o usuário antes de testar contra produção.

## Suggested Review Order

**Fluxo principal**

- `finishLogin` extraída de `LoginHandler` — mesma lógica de sempre (bloqueio/trial/token/contexto/resposta), agora compartilhada com o SSO.
  [`auth.go:669`](../../backend/handlers/auth.go#L669)

- `KeycloakSSOHandler` — ponte entre token Keycloak (já validado pelo middleware) e sessão própria.
  [`auth_sso.go:19`](../../backend/handlers/auth_sso.go#L19)

- `IAMAuthMiddleware` — validação JWKS/JWT, portada da skill com zap→log stdlib.
  [`iam_auth_middleware.go:47`](../../backend/iam/iam_auth_middleware.go#L47)

- `handleCallback` — troca code→token Keycloak→sessão FB_APU02 (reescrito, não é cópia do template).
  [`handleCallback.ts:20`](../../frontend/src/lib/keycloak/handleCallback.ts#L20)

**Correções críticas da revisão adversarial**

- **Achado gravíssimo, corrigido:** sem checar `email_verified`, um usuário do Keycloak que editasse o próprio e-mail de perfil (sem reconfirmação, se o realm permitir) poderia logar como outra pessoa no FB_APU02 — a claim `email` era a única identidade usada. Adicionada checagem obrigatória.
  [`auth_sso.go:31`](../../backend/handlers/auth_sso.go#L31)

- Endpoint novo não tinha rate limit, diferente de todo outro endpoint de auth — adicionado `SSOKeycloakRL` (mesmo limite de `LoginRL`).
  [`middleware.go:143`](../../backend/handlers/middleware.go#L143)

- Sem `jwt.WithLeeway`, qualquer desvio de relógio Keycloak↔API rejeitava tokens recém-emitidos.
  [`iam_auth_middleware.go:87`](../../backend/iam/iam_auth_middleware.go#L87)

- Comparação de e-mail case-sensitive contra uma claim de IdP externo (antes só comparava e-mail digitado pelo próprio usuário).
  [`auth.go:20`](../../backend/handlers/auth.go#L20)

- Toast/tela de erro mostrava JSON cru (`{"error":"..."}`) em vez de mensagem legível.
  [`handleCallback.ts:72`](../../frontend/src/lib/keycloak/handleCallback.ts#L72)

**Ainda pendente — Ask First obrigatório**

- Provisionamento do client no Keycloak real (`https://iam.fcxlabs.com/realms/ferreiracosta`) via credencial de automação da skill — NÃO executado ainda, precisa confirmação explícita separada antes de rodar.

**Achados da revisão adversarial — registrados em `deferred-work.md`, nenhum bloqueia esta correção**

- Amplificação de fetch JWKS via `kid` arbitrário (herdado do template).
- JWKS vazio tratado como sucesso, trava login por até 1h.
- `iss`/URL JWKS sem normalização de barra final.
- Sem controle de autorização extra por role/grupo do Keycloak (mesmo modelo do login por senha).
- Drift possível entre toggle de frontend (build-time) e backend (runtime).
- `generateCodeChallenge` depende de detalhe de implementação do `@noble/hashes` (código do template, não alterado).
- Bug pré-existente de trial NULL sempre tratado como expirado (copiado do `LoginHandler` original).
- Auto-provisioning de empresa sem `ON CONFLICT` (mesmo bug pré-existente).
- `fetchUserByEmail` agora retorna o hash de senha como valor público (risco teórico).
