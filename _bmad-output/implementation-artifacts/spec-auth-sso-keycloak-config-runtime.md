---
title: 'SSO Keycloak: mover config do frontend de build-time pra runtime'
type: 'bugfix'
created: '2026-08-05'
status: 'done'
review_loop_iteration: 1
context: []
baseline_commit: '1b040f0'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A entrega anterior (commit `1b040f0`) planejava configurar o SSO Keycloak via `VITE_IAM_*`, variáveis lidas em *build-time* pelo Vite. Só que a imagem Docker do frontend (`ghcr.io/claudiosbezerra/fb_apu02-web:latest`) é **compartilhada** entre 2 servidores físicos distintos: Hostinger/Coolify (`apuracao.fbtax.cloud`, atende outros clientes além da Ferreira Costa) e AWS (`fctax.fcxlabs.com`, dedicado à Ferreira Costa). Cravar a config do Keycloak da Ferreira Costa como default no `Dockerfile` vazaria um botão de SSO quebrado (redirect_uri não registrado) pra outros clientes no Hostinger.

**Approach:** Mover a config do Keycloak do frontend pra runtime, buscada de um endpoint novo no backend (`GET /api/auth/sso/config`, público). Cada servidor já tem seu próprio backend com seu próprio `.env` — o backend do Hostinger não tem `IAM_BASE_URL` configurado (endpoint retorna `enabled:false`), o do AWS/cliente-aws tem (retorna a config da Ferreira Costa). A mesma imagem de frontend serve os dois; o botão de SSO só aparece onde o backend correspondente estiver configurado.

## Boundaries & Constraints

**Always:**
- O endpoint `/api/auth/sso/config` é público (sem JWT), retorna só dados não-sensíveis (realm URL, client_id, redirect_uri, scopes — nenhum segredo, o fluxo é PKCE público por natureza).
- `frontend/Dockerfile` NÃO precisa mais de nenhum `ARG`/`ENV` novo — reverter a ideia de `VITE_IAM_*` cravado na imagem.
- `docker-compose.yml` (raiz) e `installer/cliente-aws/docker-compose.yml` recebem as MESMAS 5 env vars no bloco `environment:` do serviço `api`, mantendo os 2 manifests consistentes.
- Login por senha continua inalterado (já garantido na spec anterior, não muda aqui).

**Ask First:** Nenhuma — decisão já validada com o usuário nesta sessão.

**Never:** Não reintroduzir `VITE_IAM_*`/build-args no Dockerfile ou no workflow de CI — é exatamente o que causava o vazamento entre servidores.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Backend do Hostinger (sem IAM_BASE_URL) | `GET /api/auth/sso/config` | `{"enabled": false}` | Botão de SSO não aparece na tela de login |
| Backend do AWS (com IAM_BASE_URL etc.) | `GET /api/auth/sso/config` | `{"enabled": true, "base_url": "...", "client_id": "fb-apu02", "redirect_uri": "...", "scopes": "..."}` | Botão de SSO aparece |
| Falha de rede ao buscar `/api/auth/sso/config` | Backend fora do ar/timeout | Botão de SSO não aparece (fail-safe: esconde, não quebra a tela de login) | Sem erro visível ao usuário |
| Usuário clica no botão de SSO | Config já buscada e válida | Mesmo fluxo PKCE de antes, agora usando os valores vindos do backend | N/A |

</frozen-after-approval>

## Code Map

- `backend/main.go` -- ler `IAM_CLIENT_ID`, `IAM_REDIRECT_URI`, `IAM_SCOPES` (novos, default `"openid profile email"` pro último) junto com `IAM_BASE_URL`/`IAM_ALLOWED_CLIENT_IDS` já existentes; registrar `GET /api/auth/sso/config` (público, sem `withAuth`/`withDB` — só lê env vars, não precisa de banco).
- `backend/handlers/auth_sso.go` -- novo `SSOConfigHandler() http.HandlerFunc` retornando o JSON de config (ou `{"enabled": false}` se `IAM_BASE_URL` vazio).
- `frontend/src/lib/keycloak/buildLoginUrl.ts` -- `isIAMEnabled` vira `async fetchIAMConfig(): Promise<IAMConfig | null>` (busca e cacheia em módulo, uma vez); `buildLoginUrl()` usa a config buscada em vez de `import.meta.env.VITE_IAM_*`.
- `frontend/src/pages/Login.tsx` -- botão de SSO passa a depender de estado (`useState`/`useEffect` buscando a config no mount) em vez de checagem síncrona.
- `frontend/Dockerfile` -- reverter/não aplicar a ideia anterior de `ARG VITE_IAM_*` (não fazer essa mudança).
- `docker-compose.yml` (raiz) e `installer/cliente-aws/docker-compose.yml` -- adicionar as 5 vars (`IAM_BASE_URL`, `IAM_ALLOWED_CLIENT_IDS`, `IAM_CLIENT_ID`, `IAM_REDIRECT_URI`, `IAM_SCOPES`) ao bloco `environment:` do serviço `api`.

## Tasks & Acceptance

**Execution:**
- [x] `backend/main.go` + `backend/handlers/auth_sso.go` -- endpoint `/api/auth/sso/config` -- fonte única de verdade da config, por servidor
- [x] `frontend/src/lib/keycloak/buildLoginUrl.ts` -- busca runtime em vez de env var de build -- permite imagem compartilhada com comportamento por servidor
- [x] `frontend/src/pages/Login.tsx` -- botão condicionado a estado assíncrono
- [x] `docker-compose.yml` + `installer/cliente-aws/docker-compose.yml` -- 5 env vars no serviço `api`

**Acceptance Criteria:**
- Given um backend sem `IAM_BASE_URL`, when a tela de login carrega, then o botão de SSO não aparece.
- Given um backend com `IAM_BASE_URL`/`IAM_CLIENT_ID`/`IAM_REDIRECT_URI` configurados, when a tela de login carrega, then o botão aparece e o clique gera a URL de authorize correta.
- Given a mesma imagem Docker, when rodada em 2 servidores com `.env` diferentes, then o comportamento do botão difere corretamente entre eles — sem rebuild.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json` -- expected: sem erros novos

**Manual checks (if no CLI):**
- Sem os 2 servidores reais disponíveis nesta sessão pra testar lado a lado. Validar manualmente depois do deploy: `curl https://apuracao.fbtax.cloud/api/auth/sso/config` deve retornar `enabled:false`; o mesmo no domínio AWS deve retornar a config completa.

## Suggested Review Order

1. `backend/handlers/auth_sso.go` (`SSOConfigHandler`) -- ponto central da mudança. Note especialmente: reporta `enabled:true` só se `IAM_BASE_URL`, `IAM_CLIENT_ID`, `IAM_REDIRECT_URI` **e** `IAM_ALLOWED_CLIENT_IDS` estiverem TODOS presentes (achado de revisão adversarial: sem essa checagem completa, um `.env` incompleto reproduziria via env var o mesmo bug de "botão de SSO quebrado" que essa entrega inteira existe pra evitar -- a versão inicial só checava `IAM_BASE_URL`).
2. `backend/main.go` (bloco de registro de rotas SSO, linhas ~313-331) -- confirma que os nomes de env var batem com os lidos em `auth_sso.go`, e que a rota `/api/auth/sso/config` é pública (sem `withAuth`/`withDB`).
3. `frontend/src/lib/keycloak/buildLoginUrl.ts` -- `fetchIAMConfig()` (cache em módulo, fail-safe em erro de rede) e `buildLoginUrl()` consumindo a config buscada em vez de `import.meta.env.VITE_IAM_*`.
4. `frontend/src/lib/keycloak/handleCallback.ts` -- mesmo padrão de `fetchIAMConfig()` aplicado à etapa de troca de código por token (não fazia parte do Code Map original, mas era necessário pro fluxo funcionar fim-a-fim).
5. `frontend/src/pages/Login.tsx` -- botão de SSO condicionado a `useState`/`useEffect` assíncrono em vez de checagem síncrona; inclui guarda de unmount (achado de revisão, severidade baixa) pra evitar `setState` depois do componente desmontar.
6. `docker-compose.yml` e `installer/cliente-aws/docker-compose.yml` -- as mesmas 5 vars novas (`IAM_BASE_URL`, `IAM_ALLOWED_CLIENT_IDS`, `IAM_CLIENT_ID`, `IAM_REDIRECT_URI`, `IAM_SCOPES`) nos dois manifests, mantendo consistência entre os dois ambientes de deploy.

**Achados de revisão adversarial (Blind Hunter + Edge Case Hunter, ambos convergiram no mesmo achado principal):**
- **Corrigido:** `SSOConfigHandler` reportava `enabled:true` baseado só em `IAM_BASE_URL`, sem validar `IAM_CLIENT_ID`/`IAM_REDIRECT_URI`/`IAM_ALLOWED_CLIENT_IDS` -- um `.env` parcialmente preenchido mostraria o botão de SSO e falharia ao clicar. Corrigido exigindo as 4 vars completas antes de `enabled:true`.
- **Corrigido (menor):** `Login.tsx` sem guarda de unmount no `fetchIAMConfig().then(...)` -- poderia gerar warning do React se o componente desmontasse antes do fetch resolver. Adicionada flag `mounted`.
- **Aceito sem mudança:** cache em módulo (`cachedConfig`) nunca invalida exceto em falha de fetch -- aceitável porque a config só muda no boot do container, não em runtime; uma aba aberta há muito tempo só veria a config nova após reload, o que é esperado.
