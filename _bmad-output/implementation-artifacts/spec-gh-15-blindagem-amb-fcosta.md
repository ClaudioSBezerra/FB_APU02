---
title: 'Blindar AMB_FCOSTA: isolamento entre tenants e ERP Bridge somente admin'
type: 'bugfix'
created: '2026-10-27'
status: 'done'
baseline_commit: '9d11f984047122cc1b9b94e97d33a6bb75035e7c'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Qualquer usuário autenticado consegue entrar em outro tenant: `POST /api/user/preferred-company` grava `user_environments` com `role='admin'` para qualquer `company_id` (vira "membro" e passa em `GetEffectiveCompanyID`), e `/api/config/{environments,groups,companies}` leem/alteram/apagam por id sem checar dono ou membro. Foi o caminho do pentest de 29/09 que regenerou a API key do ERP Bridge da FC. Além disso, `/api/erp-bridge/*` e as páginas `/importacoes/erp-bridge*` não exigem admin, e o registro público não pode ser desligado.

**Approach:** Fechar o isolamento no servidor (falha fechada, uma vez só no `withAuth` e nos handlers de hierarquia), exigir `admin` no ERP Bridge (backend e frontend) e tornar o cadastro público configurável por `ALLOW_REGISTRATION`.

## Boundaries & Constraints

**Always:** Autorizar no servidor (dono da empresa ou linha em `user_environments`); `admin` da plataforma (`users.role`) mantém visão global só nas telas de administração. Rotas do daemon com `X-API-Key` (`import/batch`, `parceiros/sync`, `heartbeat`, `credentials`) ficam como estão. Logar `user/company/ip` nas ações sensíveis. Migrations existentes intocadas.

**Ask First:** Definir `ALLOW_REGISTRATION=false` no `.env` de produção do `aws-fc` (ação do usuário). Exigir `admin` no ERP Bridge tira a tela e o daemon de clientes Hostinger com `users.role='user'`.

**Never:** Alterar tabelas globais `forn_simples`/`cfop` (adiado); aplicar RLS/`WithTenant`; apagar contas ou linhas de teste do pentest em produção; fazer push sem aprovação.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Empresa própria | `X-Company-ID` de empresa do ambiente do usuário | processa normalmente | N/A |
| Empresa alheia | `X-Company-ID` de outro ambiente | 403 `company_not_authorized`, sem fallback | log user/company/ip |
| Preferida alheia | `preferred-company` com empresa de outro ambiente | 403, nenhuma linha em `user_environments` | log |
| Preferida própria | empresa de ambiente do usuário | atualiza `preferred_company_id` (sem criar vínculo) | N/A |
| Hierarquia alheia | PUT/DELETE/POST em environment/group/company de outro tenant | 403, nada alterado | log |
| Listagem | GET `/api/config/*` sem filtro | só itens do usuário (admin da plataforma vê todos) | N/A |
| ERP Bridge, não admin | JWT `role=user` em `/api/erp-bridge/{config,servidores,generate-api-key,trigger,pending,runs}` | 403 | N/A |
| Cadastro | `ALLOW_REGISTRATION=false` | `POST /api/auth/register` 403 | N/A |

</frozen-after-approval>

## Code Map

- `backend/handlers/auth.go` -- `GetEffectiveCompanyID` (fallback silencioso), `UpdatePreferredCompanyHandler`, `RegisterHandler`, `AuthMiddleware`
- `backend/handlers/environment.go` -- handlers de environment/group/company sem checagem
- `backend/main.go` -- `withAuth` (~289), rotas ERP Bridge (~679-694), `/api/config/*` (~385-440)
- `backend/handlers/erp_bridge.go` -- handlers ERP Bridge; log de auditoria já commitado
- `frontend/src/App.tsx` -- `AdminRoute`, rotas `/importacoes/erp-bridge*` (~288) sem proteção
- `frontend/src/contexts/AuthContext.tsx` -- interceptor de fetch; tratar `company_not_authorized`
- `backend/handlers/rfb_pagamentos_fornecedores_test.go` -- `openTestDB`, `setupTestCompany` (testes com Postgres real via `DATABASE_URL`)

## Tasks & Acceptance

**Execution:**
- [x] `backend/handlers/auth.go` -- helper `userCanAccessCompany/Group/Environment`; `GetEffectiveCompanyID` retorna `ErrCompanyNotAuthorized` para header explícito não autorizado; `UpdatePreferredCompanyHandler` exige acesso e só faz UPDATE (sem INSERT com `role admin`); `RegisterHandler` respeita `ALLOW_REGISTRATION` -- fecha a escalada e o cadastro aberto
- [x] `backend/main.go` -- `withAuth` valida `X-Company-ID` e responde 403 JSON `company_not_authorized`; `admin` nas rotas ERP Bridge (menos daemon) -- falha fechada central
- [x] `backend/handlers/environment.go` -- autorização e filtro por usuário nas 10 rotas de hierarquia -- remove o IDOR
- [x] `frontend/src/App.tsx` -- `AdminRoute` em `/importacoes/erp-bridge` e `/logs`; `AuthContext.tsx` volta à empresa padrão ao receber `company_not_authorized` sem derrubar a sessão
- [x] `docker-compose.yml`, `docker-compose.prod.yml` -- `ALLOW_REGISTRATION=${ALLOW_REGISTRATION:-true}`
- [x] `backend/handlers/*_test.go` -- testes (matriz acima) com Postgres real; `AuthMiddleware(...,"admin")` nega `role=user`

**Acceptance Criteria:**
- Given um usuário de outro ambiente, when chama qualquer rota com o `company_id` da FC (header, body ou query), then 403 e nenhum dado ou vínculo muda.
- Given `role=user`, when abre `/importacoes/erp-bridge` ou chama `/api/erp-bridge/config`, then é redirecionado / recebe 403; o daemon do `aws-fc` (admin) segue funcionando.
- Given `go build -mod=vendor ./...` e `go test ./handlers/...`, then passam; `npx tsc -p tsconfig.app.json --noEmit` sem erros.

## Spec Change Log

## Design Notes

Validar `X-Company-ID` uma vez no `withAuth` evita tocar nos ~89 chamadores e impede que um 401 genérico acione o refresh/logout do interceptor (só 403). Itens adiados em `deferred-work`: `forn_simples`/`cfop import` globais sem admin, `UPDATE/DELETE managers` sem `AND company_id`.

## Verification

**Commands:**
- `cd backend && go build -mod=vendor ./... && go test -mod=vendor ./handlers/ -run 'Isolamento|Preferred|Hierarquia|Admin'` -- expected: ok
- `cd frontend && npx tsc -p tsconfig.app.json --noEmit` -- expected: sem erros

## Suggested Review Order

**Isolamento: falha fechada (ponto de entrada)**

- Regra única de acesso: dono ou membro; erro de banco não vira "não autorizado".
  [`tenant_guard.go:29`](../../backend/handlers/tenant_guard.go#L29)

- `X-Company-ID` alheio vira 403 uma vez só, antes de qualquer handler.
  [`tenant_guard.go:60`](../../backend/handlers/tenant_guard.go#L60)

- Fallback silencioso removido: empresa pedida e não autorizada retorna erro.
  [`auth.go:342`](../../backend/handlers/auth.go#L342)

- Todas as rotas autenticadas passam pelo `CompanyScope`.
  [`main.go:299`](../../backend/main.go#L299)

**Escalada via preferred-company (causa do pentest)**

- Exige acesso e só atualiza; nunca cria vínculo nem concede `admin`.
  [`auth.go:294`](../../backend/handlers/auth.go#L294)

**Hierarquia ambiente/grupo/empresa**

- Update/create por membro; listagens filtradas pelo usuário.
  [`environment.go:127`](../../backend/handlers/environment.go#L127)
  [`environment.go:279`](../../backend/handlers/environment.go#L279)

- Exclusão exige admin do ambiente; inexistente dá 404 só ao admin da plataforma.
  [`tenant_guard.go:170`](../../backend/handlers/tenant_guard.go#L170)
  [`environment.go:464`](../../backend/handlers/environment.go#L464)

**ERP Bridge somente admin e cadastro**

- Rotas `/api/erp-bridge/*` com `admin` (daemon por API key fica de fora).
  [`main.go:681`](../../backend/main.go#L681)

- Páginas de importação deixam de depender só do menu.
  [`App.tsx:288`](../../frontend/src/App.tsx#L288)

- `ALLOW_REGISTRATION` (padrão ligado) e log do estado no startup.
  [`tenant_guard.go:159`](../../backend/handlers/tenant_guard.go#L159)
  [`docker-compose.prod.yml:24`](../../docker-compose.prod.yml#L24)

**Frontend e auditoria**

- 403 de empresa: limpa estado e preferência, recarrega; nunca repete escrita noutra empresa.
  [`AuthContext.tsx:113`](../../frontend/src/contexts/AuthContext.tsx#L113)

- Log user/company/ip no PATCH de credenciais do bridge.
  [`erp_bridge.go:152`](../../backend/handlers/erp_bridge.go#L152)

**Testes**

- Isolamento, preferred-company, hierarquia, papel admin e cadastro.
  [`tenant_guard_test.go:78`](../../backend/handlers/tenant_guard_test.go#L78)
  [`tenant_guard_test.go:173`](../../backend/handlers/tenant_guard_test.go#L173)

- Testes de NFS-e ajustados: header forjado agora é recusado, não redirecionado.
  [`nfse_credentials_test.go:788`](../../backend/handlers/nfse_credentials_test.go#L788)
