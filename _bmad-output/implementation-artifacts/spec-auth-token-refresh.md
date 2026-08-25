---
title: 'Auth: usar refresh token automático em vez de deslogar no primeiro 401'
type: 'feature'
created: '2026-08-25'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: 'defb570'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O usuário reclamou que o app desloga com muita frequência. Causa raiz: o access token JWT dura 30min (`backend/handlers/auth.go:145`) e já existe um cookie httpOnly `refresh_token` de 7 dias com rotação e um endpoint pronto pra usá-lo (`POST /api/auth/refresh`, `RefreshHandler` em `backend/handlers/auth.go:1113`) — mas o frontend nunca chama esse endpoint. Hoje, tanto o interceptor global de `fetch` (401 em chamada de API) quanto a restauração de sessão no mount (`/api/auth/me`) deslogam na primeira falha de autenticação, sem tentar renovar.

**Approach:** Nos 2 pontos que hoje deslogam direto num 401 (interceptor de `fetch` e restauração de sessão no mount), tentar `POST /api/auth/refresh` primeiro; se retornar um token novo, atualizar a sessão e repetir a chamada original que falhou; só desloga se o próprio refresh também falhar (refresh token realmente expirado/inválido). Concorrência entre múltiplas chamadas 401 quase simultâneas é resolvida com uma única promise de refresh compartilhada (dedup) — nunca duas chamadas a `/api/auth/refresh` em paralelo na mesma aba.

## Boundaries & Constraints

**Always:**
- O tempo de expiração do JWT de acesso (30min) e do refresh cookie (7 dias, com rotação) permanecem exatamente como estão no backend — nenhuma mudança de configuração de expiração, só o frontend passa a consumir o endpoint que já existe.
- Uma única função helper de refresh (`refreshAccessToken`, dedup via `useRef<Promise<string|null>|null>`) é compartilhada pelos 2 pontos de uso (interceptor de `fetch` e restauração de sessão) — nunca duas chamadas a `/api/auth/refresh` disparadas em paralelo na mesma aba, já que o backend deleta o refresh token antigo do store ao rotacionar (uma 2ª chamada concorrente usando o cookie já rotacionado falharia).
- A chamada de retry após um refresh bem-sucedido usa `originalFetch` (não o `window.fetch` já sobrescrito) — nunca aciona um 2º ciclo de refresh mesmo se a chamada repetida também retornar 401 por algum motivo.
- Se `POST /api/auth/refresh` falhar (401/erro de rede), o comportamento é idêntico ao atual: limpa a sessão local e redireciona pra `/login` (com a flag `session_expired` no caminho do interceptor, exatamente como hoje).
- `/api/auth/refresh` continua excluído do próprio interceptor de 401 (já cai em `isAuthCall`, inalterado) — nunca entra em recursão.

**Ask First:** Nenhuma.

**Never:** Não implementar refresh proativo por timer (só reativo, ao receber 401) — menor complexidade, resolve o sintoma relatado. Não mexer no botão "Sair" (`logout()`) nem no fluxo de SSO Keycloak (specs anteriores, já fechadas) — escopo restrito aos 2 pontos que hoje deslogam num 401/falha de sessão. Não resolver o caso de múltiplas abas simultâneas com tokens expirados ao mesmo tempo disparando refresh em paralelo — cada aba tem seu próprio dedup (só funciona dentro da mesma aba); coordenar entre abas exigiria um mecanismo novo (BroadcastChannel/Web Locks), fora de escopo. Não persistir refresh tokens no banco (`refreshTokenStore` em memória já é uma limitação conhecida e documentada do projeto) — fora de escopo desta spec.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Token expira em uso ativo | Chamada de API não-auth retorna 401, refresh cookie ainda válido | Refresh silencioso, chamada original repetida com token novo, usuário nem percebe | N/A |
| Token expira, refresh também expirado/inválido (7+ dias) | 401 na API, `POST /api/auth/refresh` também retorna 401 | Comportamento atual: limpa sessão, `session_expired=1`, redireciona `/login` | N/A |
| Reabrir a aba/app depois de 30min+ (sessão restaurada do sessionStorage) | `/api/auth/me` retorna 401 no mount | Tenta refresh; se sucesso, repete `/api/auth/me` com o token novo e segue carregando normalmente; se falhar, limpa sessão e redireciona `/login` | N/A |
| Várias chamadas de API falham com 401 quase ao mesmo tempo (token expirou com várias requisições em voo) | N chamadas 401 concorrentes | Só 1 `POST /api/auth/refresh` é disparado; as demais aguardam a mesma promise e reusam o token resultante | N/A |
| Retry pós-refresh também retorna 401 (cenário anômalo) | Token novo rejeitado imediatamente | Não tenta refresh de novo para essa chamada — retorna a resposta 401 do retry como está, sem loop | N/A |
| Múltiplas abas com token expirado simultaneamente | 2+ abas disparam refresh quase ao mesmo tempo | Só uma vence (cookie rotacionado 1x); a(s) outra(s) aba(s) recebe(m) refresh token já inválido e caem no fallback de logout desta aba (limitação conhecida, fora de escopo resolver) | Documentado como limitação aceita |

</frozen-after-approval>

## Code Map

- `frontend/src/contexts/AuthContext.tsx` -- novo `refreshPromiseRef` (useRef) + função `refreshAccessToken()` no corpo do componente (chama `POST /api/auth/refresh`, atualiza `tokenRef`/`sessionStorage`/`setToken` em caso de sucesso, dedup via ref); interceptor de `fetch` (~linha 47-72) passa a tentar refresh+retry antes de deslogar no 401; efeito de restauração de sessão (~linha 74-117) passa a tentar refresh+retry antes de deslogar no 401 de `/api/auth/me`

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/contexts/AuthContext.tsx` -- adicionar `const refreshPromiseRef = useRef<Promise<string | null> | null>(null);` junto dos outros refs
- [x] `frontend/src/contexts/AuthContext.tsx` -- adicionar `const refreshAccessToken = (): Promise<string | null> => {...}` no corpo do componente: se `refreshPromiseRef.current` já existe, retorna ele; senão, cria e armazena uma IIFE async que chama `fetch('/api/auth/refresh', {method:'POST'})`, em sucesso extrai `data.token`, atualiza `tokenRef.current`, `sessionStorage.setItem('token', ...)` e `setToken(...)`, retorna o token; em qualquer falha (não-ok ou exceção) retorna `null`; `finally` sempre zera `refreshPromiseRef.current = null` -- fonte única de refresh com dedup, usada pelos 2 pontos abaixo
- [x] `frontend/src/contexts/AuthContext.tsx` -- no interceptor de `fetch` (bloco do 401), antes de limpar sessão: `await refreshAccessToken()`; se retornar token, montar novos headers (Authorization + X-Company-ID) e `return originalFetch(input, {...init, headers: retryHeaders})`; se retornar `null`, manter o fallback atual (`sessionStorage.clear()`, `session_expired`, redirect) -- implementa o cenário principal + fallback da matriz
- [x] `frontend/src/contexts/AuthContext.tsx` -- no `.then` do `fetch('/api/auth/me', ...)` (restauração de sessão), no branch `res.status === 401`: tornar o callback `async`, chamar `await refreshAccessToken()`; se retornar token, repetir `fetch('/api/auth/me', {headers:{Authorization: Bearer ${newToken}}})` e retornar `.json()` dele se `ok`; em qualquer falha, manter o fallback atual (`sessionStorage.clear()`, redirect, throw) -- fecha o cenário de reabrir a aba depois do token expirar

**Acceptance Criteria:**
- Given um token de acesso expirado (30min+) mas refresh cookie válido, when qualquer chamada de API não-auth retorna 401, then a chamada é repetida com um token novo automaticamente, sem redirecionar pra `/login`.
- Given o refresh token também expirado/inválido, when uma chamada de API retorna 401, then o comportamento é idêntico ao atual (limpa sessão, `session_expired=1`, redireciona `/login`).
- Given a aba é reaberta/recarregada com um token expirado em `sessionStorage`, when a restauração de sessão detecta 401 em `/api/auth/me`, then tenta refresh e, em sucesso, carrega o perfil normalmente sem pedir login de novo.
- Given N chamadas de API falham com 401 dentro da janela de um único refresh em andamento, when isso acontece, then apenas 1 requisição a `/api/auth/refresh` é observada na rede.

## Design Notes

Por que usar o `fetch` global (já interceptado) para chamar `/api/auth/refresh` em vez de uma referência separada: `/api/auth/refresh` já cai em `isAuthCall` (`url.includes('/api/auth/')`) no interceptor existente, cujo bloco de tratamento de 401 exige `!isAuthCall` — ou seja, mesmo passando pelo `fetch` interceptado, uma falha do próprio refresh nunca re-aciona esse mesmo bloco. Isso permite reusar `refreshAccessToken()` tanto de dentro do interceptor quanto do efeito de restauração de sessão sem precisar expor `originalFetch` (que é local ao efeito do interceptor) para fora dele.

Por que a chamada de retry (após sucesso do refresh) usa `originalFetch` e não o `fetch` global: garante no máximo 1 ciclo de refresh por requisição original, mesmo no cenário anômalo do retry também retornar 401 — sem essa escolha, haveria risco de recursão.

## Verification

**Commands:**
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json` -- expected: sem erros novos em `AuthContext.tsx`

**Manual checks (if no CLI):**
- Sem backend rodando localmente nesta sessão pra testar contra um token expirado real. Validar após deploy: reduzir temporariamente (só em teste manual local, não commitado) o tempo de expiração do JWT pra ~1min, logar, esperar expirar, navegar/clicar em algo que chame a API, e confirmar que renova silenciosamente em vez de deslogar; deixar a sessão parada além do tempo de expiração e recarregar a página, confirmar que a restauração de sessão também renova sem pedir login.

## Suggested Review Order

**Fonte única de refresh (dedup + timeout + validação)**

- Ponto de entrada: dedup via ref compartilhada, guarda contra logout concorrente, timeout de 8s (`AbortController`) e validação de tipo do token — endurecido na revisão adversarial (2 rodadas).
  [`AuthContext.tsx:58`](../../frontend/src/contexts/AuthContext.tsx#L58)

**Interceptor de `fetch`: refresh-então-retry antes de deslogar**

- Cenário principal: 401 em chamada de API dispara refresh; sucesso repete a chamada original via `originalFetch` (nunca entra em loop); falha cai no fallback de sempre.
  [`AuthContext.tsx:105`](../../frontend/src/contexts/AuthContext.tsx#L105)

**Restauração de sessão no mount: mesmo tratamento pro `/api/auth/me`**

- Fecha o cenário mais comum da queixa original ("abro o app depois de um tempo e já preciso logar de novo").
  [`AuthContext.tsx:157`](../../frontend/src/contexts/AuthContext.tsx#L157)

- Guarda contra a race achada na revisão: não repopula `user`/sessionStorage se um logout aconteceu enquanto o refresh estava em voo.
  [`AuthContext.tsx:171`](../../frontend/src/contexts/AuthContext.tsx#L171)

