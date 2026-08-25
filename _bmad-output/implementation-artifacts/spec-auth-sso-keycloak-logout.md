---
title: 'Login: logout real via Keycloak end_session'
type: 'feature'
created: '2026-08-25'
status: 'done'
review_loop_iteration: 1
context: []
baseline_commit: 'b85fa0f'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** No servidor da Ferreira Costa (SSO habilitado), clicar em "Sair" navega pra `/login` puro, mas como a sessão do Keycloak/AD/Microsoft continua ativa no navegador, o auto-redirect da tela de login reautentica o usuário silenciosamente — o botão de logout não sai de fato, é preciso fechar o navegador inteiro para realmente encerrar a sessão. Confirmado ao vivo pelo usuário (2026-08-25).

**Approach:** Marcar (em `sessionStorage`) quando uma sessão foi estabelecida via SSO. No logout, se essa marca estiver presente, em vez de navegar direto pra `/login`, navegar pro endpoint `end_session` do Keycloak (RP-Initiated Logout: `client_id` + `post_logout_redirect_uri` apontando pra `/login?password`), encerrando a sessão no IdP antes de voltar pro formulário normal — quebrando o loop de reautenticação silenciosa. Sessões por senha continuam indo direto pra `/login` como hoje.

## Boundaries & Constraints

**Always:**
- Limpeza local da sessão (`sessionStorage.clear()` + reset dos states do `AuthContext`) sempre acontece de imediato no `logout()`, antes de qualquer chamada de rede ao Keycloak — o app nunca fica "preso" esperando o IdP responder.
- A marca de sessão-via-SSO é gravada em `sessionStorage` (mesmo storage já usado por todo o resto da sessão em `AuthContext.tsx`), setada em `AuthCallback.tsx` logo após `login(data)` bem-sucedido.
- Logout de sessão por senha (marca ausente) mantém o comportamento atual: navega direto pra `/login`, sem nenhuma chamada ao Keycloak.
- Se a marca de SSO estiver presente mas `fetchIAMConfig()` retornar `enabled:false` ou campos essenciais (`base_url`/`client_id`) ausentes (ex: servidor sem SSO configurado), cair no comportamento atual (`/login` direto) — nunca travar ou lançar erro visível ao usuário.
- `post_logout_redirect_uri` enviado ao Keycloak aponta pra `/login?password` (não `/login` puro) — evita um novo auto-redirect imediato ao voltar.
- Reusar `fetchIAMConfig()` já existente (`frontend/src/lib/keycloak/buildLoginUrl.ts`) para obter `base_url`/`client_id` — nenhuma configuração nova de build-time/env var.

**Ask First:** Nenhuma decisão de produto pendente, mas uma dependência operacional real: o client `fb-apu02` no Keycloak precisa ter `https://fctax.fcxlabs.com/login?password` cadastrado em "Valid Post Logout Redirect URIs" (mesmo tipo de cadastro que `redirect_uri` já exigiu para o login). Sem isso, o Keycloak pode rejeitar o `end_session` com uma tela de erro própria dele em vez de voltar pro nosso `/login?password` — a limpeza local da sessão já terá acontecido de qualquer forma. **Comunicar ao time do Marlos após o merge**, análogo ao que já foi feito para o `redirect_uri` de login.

**Never:** Não alterar o comportamento do interceptor de 401 (`AuthContext.tsx`, linha ~63-67) nem da restauração de sessão (linha ~101-104) — ambos navegam pra `/login` puro e já estão documentados como pendência separada em `deferred-work.md`. Não capturar/persistir o `id_token` do Keycloak (mantém o princípio já estabelecido: token do Keycloak só vive o tempo da troca, nunca é reusado depois). Não adicionar `id_token_hint` ao `end_session` nesta spec — se o realm exigir, vira um achado de revisão/deploy a ser tratado separadamente.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Logout de sessão SSO, servidor com SSO habilitado | `sessionStorage['auth_via_sso'] === '1'`, usuário clica Sair | Sessão local limpa imediatamente; navega pro `end_session` do Keycloak com `post_logout_redirect_uri=/login?password` | N/A |
| Logout de sessão por senha | Marca ausente, usuário clica Sair | Comportamento atual inalterado: navega direto pra `/login` | N/A |
| Logout de sessão SSO, mas `fetchIAMConfig()` retorna `enabled:false`/config incompleta | Marca presente, config indisponível | Sessão local limpa; navega pra `/login` (fallback, sem tentar montar URL do Keycloak) | N/A |
| Keycloak rejeita o `end_session` (ex: `post_logout_redirect_uri` não cadastrado) | Marca presente, config válida, mas Keycloak retorna erro na própria tela dele | Sessão local do FB_APU02 já estava limpa antes da navegação — usuário vê erro do Keycloak, mas não fica "preso logado" no FB_APU02 | Fora do nosso controle (tela é do Keycloak); documentar dependência operacional |

</frozen-after-approval>

## Code Map

- `frontend/src/lib/keycloak/buildLoginUrl.ts` -- exportar 2 novas constantes de chave: `SESSION_KEY_AUTH_VIA_SSO` e `SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT`, ao lado das já existentes `SESSION_KEY_VERIFIER`/`SESSION_KEY_STATE`
- `frontend/src/contexts/AuthContext.tsx` -- `login()` passa a aceitar um 2º parâmetro opcional `{ viaSSO?: boolean }` e grava `SESSION_KEY_AUTH_VIA_SSO` centralmente (sempre, nos dois sentidos); `logout()` ganha guarda contra dupla-invocação (`useRef`), lê a marca antes do `.clear()`, e — se veio de SSO — grava `SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT` (depois do `.clear()`) e só então dispara a busca de config com timeout de 5s antes de navegar
- `frontend/src/pages/AuthCallback.tsx` -- troca a chamada `login(data)` por `login(data, { viaSSO: true })`; remove a gravação direta da chave (centralizada em `AuthContext.tsx` agora)
- `frontend/src/pages/Login.tsx` -- no início do `useEffect` de auto-redirect, ler-e-remover `SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT`; se presente, tratar como `hasPasswordFallback` (pular o auto-redirect nesta carga de página) — fecha a race com o `ProtectedRoute` encontrada na revisão adversarial (2 revisores independentes)

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/lib/keycloak/buildLoginUrl.ts` -- adicionar `export const SESSION_KEY_AUTH_VIA_SSO = 'auth_via_sso';` e `export const SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT = 'sso_logout_suppress_redirect';` junto das constantes de chave já existentes -- fonte única das chaves, evita string literal duplicada entre arquivos (achado da revisão adversarial)
- [x] `frontend/src/contexts/AuthContext.tsx` -- importar as 2 novas constantes + `fetchIAMConfig`; mudar a assinatura de `login(data: any)` para `login(data: any, opts?: { viaSSO?: boolean })`, gravando `sessionStorage.setItem(SESSION_KEY_AUTH_VIA_SSO, opts?.viaSSO ? '1' : '0')` (sempre grava, nunca deixa resíduo de uma sessão anterior — fecha o achado de flag "vazando" de aba duplicada) -- centraliza a marcação num único ponto
- [x] `frontend/src/contexts/AuthContext.tsx` -- em `logout()`: adicionar `useRef` (`loggingOutRef`) e retornar imediatamente se já `true`, setar `true` no início -- evita que um segundo clique/chamada concorrente vença a navegação pendente da primeira (achado do Edge Case Hunter)
- [x] `frontend/src/contexts/AuthContext.tsx` -- em `logout()`: capturar `wasSSO` (via `SESSION_KEY_AUTH_VIA_SSO`) antes do `.clear()` como já previsto; manter a limpeza local síncrona como primeiro passo; se `!wasSSO`, manter `window.location.href = '/login'` inalterado; se `wasSSO`, logo após o `.clear()` gravar `sessionStorage.setItem(SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT, '1')`, então iniciar um `window.setTimeout` de 5s que força `window.location.href = '/login'` (mesmo fallback e mesmo destino já previstos na matriz), e chamar `fetchIAMConfig()` — se resolver antes do timeout, cancelar o timeout e, se `enabled && base_url && client_id`, navegar pro `end_session` (`client_id` + `post_logout_redirect_uri` = origin + `/login?password`); senão navegar pra `/login` -- implementa o fluxo principal com timeout simétrico ao já existente em `Login.tsx`, fechando o achado de ausência de timeout
- [x] `frontend/src/pages/AuthCallback.tsx` -- trocar `login(data)` por `login(data, { viaSSO: true })`; remover a linha antiga `sessionStorage.setItem('auth_via_sso', '1')`
- [x] `frontend/src/pages/Login.tsx` -- no topo do `useEffect` de auto-redirect (antes de checar `hasPasswordFallback`), ler `sessionStorage.getItem(SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT)` e, se presente, `sessionStorage.removeItem(...)` imediatamente (leitura única, evita suprimir permanentemente); tratar como equivalente a `hasPasswordFallback=true` para esta montagem -- fecha a race com o `ProtectedRoute`: mesmo que o `logout()` ainda esteja aguardando a config do Keycloak quando este componente montar via navegação client-side, ele não dispara um 2º redirect concorrente pro Keycloak

**Acceptance Criteria:**
- Given uma sessão estabelecida via SSO no servidor da Ferreira Costa, when o usuário clica em "Sair", then o navegador é levado ao `end_session` do Keycloak (URL contém `/protocol/openid-connect/logout`, `client_id=fb-apu02`, `post_logout_redirect_uri` codificado para `/login?password`) e a sessão local do FB_APU02 já foi limpa antes dessa navegação.
- Given uma sessão estabelecida por senha (mesmo em servidor com SSO habilitado), when o usuário clica em "Sair", then o comportamento é idêntico ao de hoje — navega direto pra `/login`, sem chamar o Keycloak.
- Given uma sessão SSO mas o servidor não tem SSO configurado/habilitado no momento do logout, when o usuário clica em "Sair", then cai no fallback `/login` sem erro visível, mesmo se a config nunca resolver (timeout de 5s).
- Given uma sessão SSO clicando em "Sair", when o `ProtectedRoute` re-renderiza `Login.tsx` via navegação client-side antes da config do Keycloak resolver, then `Login.tsx` NÃO dispara seu próprio auto-redirect pro Keycloak (login) nessa montagem.
- Given o usuário clica em "Sair" duas vezes rapidamente (ou dois componentes de navegação chamam `logout()` quase simultaneamente), when a segunda chamada ocorre enquanto a primeira ainda está em andamento, then ela é ignorada (no-op) e não interfere na navegação já em curso da primeira.
- Given uma aba com sessão SSO é duplicada e, nela, o usuário efetua login por senha, when o logout for chamado depois, then ele segue o caminho de sessão por senha (direto pra `/login`), não o de SSO — a marca é sempre reescrita em todo `login()`, nunca herdada de uma sessão anterior.

## Spec Change Log

- **Iteration 1 (bad_spec, revisão adversarial Blind Hunter + Edge Case Hunter, achado real e reproduzido por ambos independentemente):** a 1ª implementação limpava o estado do `AuthContext` de forma síncrona e só então aguardava `fetchIAMConfig()` de forma assíncrona antes de decidir o destino do redirect. Isso cria uma race real com o `ProtectedRoute` (`App.tsx`), que reage à limpeza síncrona do `isAuthenticated` navegando pra `/login` via SPA *antes* da config do Keycloak resolver — nesse meio-tempo, `Login.tsx` monta e dispara seu próprio auto-redirect pro Keycloak *login*, competindo com o redirect pro Keycloak *logout* pretendido e podendo reproduzir o exato bug que esta spec corrige (reautenticação silenciosa). O Edge Case Hunter também confirmou 2 achados reais adicionais: (a) uma segunda chamada a `logout()` durante a espera intermediária (duplo clique, 2 componentes de navegação) pode vencer a corrida contra a primeira e cancelar o redirect pro Keycloak; (b) a chave `sessionStorage['auth_via_sso']` era gravada só em `AuthCallback.tsx`, então uma aba duplicada com sessão SSO ativa que depois logasse por senha manteria a marca indevidamente, fazendo um futuro logout por senha seguir o caminho de SSO por engano. Emendado: (1) nova chave `SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT` gravada por `logout()` e lida-e-removida por `Login.tsx` no início do seu efeito de auto-redirect, tratada como equivalente a `hasPasswordFallback` — fecha a race com o `ProtectedRoute`; (2) `logout()` ganha guarda de dupla-invocação via `useRef`; (3) a marcação de origem SSO passa a ser feita centralmente dentro de `login()` (novo parâmetro `opts?.viaSSO`), sempre reescrita em toda chamada — nunca herdada de uma sessão anterior na mesma aba; (4) timeout de 5s simétrico ao já existente em `Login.tsx` adicionado à espera de `fetchIAMConfig()` em `logout()`, fechando a ausência de fallback por timeout apontada pelo Blind Hunter. **KEEP:** toda a lógica da iteração anterior segue válida e deve ser re-derivada sem mudança de abordagem conceitual — marcar sessão via SSO, limpar localmente antes de qualquer chamada de rede, reusar `fetchIAMConfig()`, `post_logout_redirect_uri=/login?password`, fallback pra `/login` sem config, e o comportamento de sessão por senha inalterado. Os achados sobre `id_token_hint` ausente e a dependência operacional de cadastro no Keycloak (`Valid Post Logout Redirect URIs`) já estavam documentados no `Ask First`/`Never` desta spec e permanecem como estavam, sem mudança — são riscos operacionais aceitos, não bugs de código. Achados sobre cache permanente de `fetchIAMConfig()` sem invalidação e ausência de guarda contra `base_url` com barra final são pré-existentes em `buildLoginUrl.ts` (não introduzidos por esta spec) — registrados em `deferred-work.md`, fora do escopo desta re-derivação.

## Design Notes

Por que marcar a sessão em vez de sempre chamar `end_session` quando SSO está habilitado no servidor: um usuário pode logar por senha mesmo num servidor com SSO habilitado (opção paralela, nunca removida). Se todo logout desse servidor dependesse do Keycloak responder, uma instabilidade do IdP quebraria o logout de quem nunca teve sessão lá. Marcando só quem de fato passou pelo fluxo SSO, o logout de sessões por senha continua 100% independente do Keycloak.

**Por que a marca de supressão em `Login.tsx` é necessária (achado da revisão adversarial, iteração 1):** `logout()` limpa o estado do `AuthContext` de forma síncrona (via `setUser(null)` etc.), o que faz `isAuthenticated` virar `false` imediatamente. O `ProtectedRoute` (`App.tsx`) reage a isso navegando (client-side, sem reload de página) pra `/login` — o que pode acontecer *antes* da chamada assíncrona a `fetchIAMConfig()` dentro de `logout()` terminar, já que essa chamada pode exigir um round-trip de rede real (o cache em memória de `fetchIAMConfig()` é zerado a cada carregamento de página, e nem toda sessão revisita `/login` antes de sair). Sem a marca de supressão, `Login.tsx` montaria nesse meio-tempo e disparar seu próprio auto-redirect pro Keycloak *login* — competindo com o redirect pro Keycloak *logout* que o `logout()` está prestes a disparar, reproduzindo o exato bug que esta spec corrige. A marca é lida e removida uma única vez por `Login.tsx`, então não suprime auto-redirects futuros legítimos.

## Verification

**Commands:**
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json` -- expected: sem erros novos em `AuthContext.tsx`/`AuthCallback.tsx`

**Manual checks (if no CLI):**
- Sem os 2 servidores reais disponíveis nesta sessão para testar o `end_session` de ponta a ponta contra o Keycloak real. Validar após deploy: logar via SSO em `fctax.fcxlabs.com`, clicar em Sair, confirmar que cai no Keycloak e depois retorna pra `/login?password` sem reautenticar sozinho; login por senha nesse mesmo servidor, sair, confirmar que vai direto pra `/login` sem tocar no Keycloak.

## Suggested Review Order

**Marcação de origem SSO (fonte única, sem resíduo entre sessões)**

- Entrada: `login()` agora aceita `opts.viaSSO` e sempre regrava a marca — nunca herda de uma sessão anterior na mesma aba.
  [`AuthContext.tsx:126`](../../frontend/src/contexts/AuthContext.tsx#L126)

- Único chamador que passa `viaSSO: true` — todo o resto (senha, registro) usa o default `false`.
  [`AuthCallback.tsx:21`](../../frontend/src/pages/AuthCallback.tsx#L21)

- Chaves de sessionStorage centralizadas, ao lado das já existentes do fluxo PKCE.
  [`buildLoginUrl.ts:23`](../../frontend/src/lib/keycloak/buildLoginUrl.ts#L23)

**Logout real via Keycloak `end_session`**

- Guarda de dupla-invocação: um segundo clique/chamada concorrente não pode vencer a navegação já em curso.
  [`AuthContext.tsx:169`](../../frontend/src/contexts/AuthContext.tsx#L169)

- Limpeza local síncrona sempre primeiro; decisão de destino (`/login` vs Keycloak) só depois.
  [`AuthContext.tsx:188`](../../frontend/src/contexts/AuthContext.tsx#L188)

- Timeout de 5s simétrico ao de `Login.tsx` + `.catch()` — nenhum caminho deixa o app "preso" esperando o IdP.
  [`AuthContext.tsx:204`](../../frontend/src/contexts/AuthContext.tsx#L204)

**Fechamento da race com o `ProtectedRoute` (achado da revisão adversarial, 2 rodadas)**

- Flag de supressão gravada logo após a limpeza local — é o que impede `Login.tsx` de disparar seu próprio auto-redirect pro Keycloak *login* enquanto este `logout()` ainda aguarda o *end_session*.
  [`AuthContext.tsx:195`](../../frontend/src/contexts/AuthContext.tsx#L195)

- Leitura-e-remoção da flag guardada por `useRef`, imune ao duplo-invoke de efeitos do `React.StrictMode` em dev (achado confirmado por 2 revisores na iteração 2).
  [`Login.tsx:40`](../../frontend/src/pages/Login.tsx#L40)

- Ponto de uso: a flag suprime o auto-redirect nesta montagem, exatamente como `hasPasswordFallback`.
  [`Login.tsx:52`](../../frontend/src/pages/Login.tsx#L52)

