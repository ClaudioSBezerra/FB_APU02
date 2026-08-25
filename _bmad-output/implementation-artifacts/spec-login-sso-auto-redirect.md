---
title: 'Login: auto-redirect pro SSO Keycloak no servidor da Ferreira Costa'
type: 'feature'
created: '2026-08-25'
status: 'done'
review_loop_iteration: 1
context: []
baseline_commit: '40f7961'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** No servidor da Ferreira Costa (`fctax.fcxlabs.com`), o usuário ainda precisa clicar manualmente em "Entrar com SSO Ferreira Costa" na tela de login por senha. Marlos (gestor de TI da Ferreira Costa) pediu que, nesse servidor, o SSO Keycloak seja o fluxo automático — sem passar pela tela de senha.

**Approach:** `Login.tsx` já busca `fetchIAMConfig()` no mount pra decidir se mostra o botão de SSO. Reusar esse mesmo `enabled` (nenhuma lógica nova de detecção de domínio/hostname) para, quando `true`, redirecionar automaticamente pro Keycloak assim que a config chega — sem esperar clique. Um query param de fallback (`?password`) desativa o auto-redirect pra permitir login por senha nesse mesmo servidor quando necessário.

**Renegociado (review_loop_iteration 1, achado real de revisão adversarial):** o auto-redirect também dispara em navegações internas do app pra `/login` sem `?password`, não só em acesso direto. Duas delas foram avaliadas com o usuário:
- **Logout** (`AuthContext.tsx`) navega pra `/login` puro hoje. Decisão do usuário: **manter como está, não mexer em `AuthContext.tsx`** — no servidor da Ferreira Costa, "sair" do FB_APU02 pode re-autenticar silenciosamente via sessão Keycloak/AD ainda ativa (não há chamada de `end_session` do Keycloak nesse projeto). Comportamento aceito, fora de escopo.
- **`AuthError.tsx`** ("Voltar para o login" após falha no Keycloak) também aponta pra `/login` puro hoje, causando um loop de redirect sem saída visível. Decisão do usuário: **corrigir**, trocando o link pra `/login?password`.

## Boundaries & Constraints

**Always:**
- Login por senha continua existindo e funcional — nunca é removido do código, só deixa de ser o caminho automático quando SSO está habilitado (herdado da spec original: SSO é sempre opção paralela, nunca substitui).
- Detecção de "é o servidor da Ferreira Costa" usa exclusivamente `fetchIAMConfig().enabled` (já existente em `frontend/src/lib/keycloak/buildLoginUrl.ts`) — nenhuma checagem de `window.location.hostname` ou lógica de domínio nova.
- Se `fetchIAMConfig()` falhar (erro de rede) ou `buildLoginUrl()` lançar erro depois de `enabled:true`, cair para o formulário de login normal — nunca travar a tela em estado de carregamento indefinidamente. Isso inclui o caso de `fetchIAMConfig()` nunca resolver nem rejeitar (endpoint pendurado): um timeout local (ex: 5s) força a queda pro formulário.
- Acessar `/login?password` (qualquer valor ou vazio) desativa o auto-redirect nessa carga de página e mostra o formulário normal (com botão de SSO ainda visível ao lado, se `enabled`).
- O link "Voltar para o login" em `AuthError.tsx` aponta pra `/login?password`, não `/login` puro — evita o loop de redirect após falha no Keycloak.
- Qualquer erro ao montar a URL do Keycloak (`buildLoginUrl()` lançando) é logado (`console.error`) antes de cair pro formulário — nunca falha silenciosa.
- Mudança restrita a `frontend/src/pages/Login.tsx` e `frontend/src/pages/AuthError.tsx` (só o href do link). Nenhuma alteração em backend, em `buildLoginUrl.ts`/`handleCallback.ts`, em `AuthContext.tsx`, ou em outras páginas (`Register.tsx`, etc.).

**Ask First:** Nenhuma — decisões de detecção, fallback, logout e AuthError já validadas com o usuário nesta sessão (incluindo a renegociação do review_loop_iteration 1).

**Never:** Não introduzir checagem de hostname/domínio como sinal de decisão. Não remover ou esconder permanentemente o formulário de senha do código. Não implementar `end_session`/logout do Keycloak nem alterar `AuthContext.tsx` — fora de escopo por decisão do usuário.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Servidor Ferreira Costa, acesso normal | `GET /login`, `fetchIAMConfig()` resolve `enabled:true` | Tela mostra um loading breve com texto acessível (ex: "Redirecionando para login corporativo...") e redireciona automaticamente pro Keycloak (`buildLoginUrl()`) | N/A |
| Servidor Hostinger (sem SSO) | `GET /login`, `fetchIAMConfig()` resolve `enabled:false` | Formulário de login por senha normal, sem redirect, botão de SSO ausente (comportamento equivalente ao anterior, com um spinner breve inevitável — ver Design Notes) | N/A |
| Fallback explícito | `GET /login?password`, independente do valor de `enabled` | Nunca redireciona; mostra formulário normal (com botão de SSO se `enabled:true`) | N/A |
| Falha ao buscar config | `fetchIAMConfig()` rejeita ou demora — a função já é fail-safe e resolve `{enabled:false}` em qualquer erro | Cai automaticamente no formulário normal, sem loading infinito | Nenhum erro visível ao usuário |
| `fetchIAMConfig()` nunca resolve (endpoint pendurado) | Requisição fica pendente indefinidamente | Timeout local (5s) força queda pro formulário normal | Nenhum erro visível ao usuário |
| `enabled:true` mas `buildLoginUrl()` lança erro | Ex: falha de rede entre a config chegar e montar a URL do Keycloak | Sai do estado de loading e mostra o formulário normal (não trava a tela) | `console.error` com a mensagem do erro |
| Falha no login do Keycloak (ex: AXIS, credencial inválida) | Usuário cai em `/auth/error`, clica em "Voltar para o login" | Vai pra `/login?password` — formulário normal, sem novo auto-redirect | N/A |
| Logout no servidor da Ferreira Costa | Usuário autenticado clica em Sair | Navega pra `/login` puro (inalterado) — se a sessão Keycloak/AD ainda estiver ativa no navegador, pode reautenticar automaticamente (aceito, fora de escopo) | N/A |

</frozen-after-approval>

## Code Map

- `frontend/src/pages/Login.tsx` -- principal arquivo alterado. Query param de fallback, estado de "decidindo/redirecionando", `useEffect` de auto-redirect reusando `fetchIAMConfig`/`buildLoginUrl` já importados, timeout de fail-safe, log de erro, texto acessível no loading.
- `frontend/src/pages/AuthError.tsx` -- alteração de 1 linha: link "Voltar para o login" passa a apontar pra `/login?password` em vez de `/login`.

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/pages/Login.tsx` -- ler `window.location.search` (ou `useSearchParams` do react-router-dom, já usado no projeto) pra `hasPasswordFallback` -- decide se o auto-redirect roda nesta carga de página
- [x] `frontend/src/pages/Login.tsx` -- novo estado `autoRedirecting` (default: `!hasPasswordFallback`) -- controla se renderiza um loading mínimo ou o formulário completo
- [x] `frontend/src/pages/Login.tsx` -- no `useEffect` existente que chama `fetchIAMConfig()`: se `!hasPasswordFallback && config.enabled`, chamar `buildLoginUrl()` e `window.location.href = url`; em qualquer falha (config, timeout, ou buildLoginUrl), `setAutoRedirecting(false)` pra revelar o formulário, com `console.error` no caso de `buildLoginUrl()` falhar -- implementa o fluxo principal + os fail-safes da matriz
- [x] `frontend/src/pages/Login.tsx` -- timeout local (5s) que força `setAutoRedirecting(false)` se `fetchIAMConfig()` nunca resolver nem rejeitar -- evita travar a tela indefinidamente num endpoint pendurado
- [x] `frontend/src/pages/Login.tsx` -- quando `autoRedirecting` for `true`, renderizar um estado de carregamento simples (spinner central + texto "Redirecionando para login corporativo...", `role="status"`/`aria-live`) em vez da tela cheia -- evita flash do formulário de senha antes do redirect e dá sinal pra leitor de tela
- [x] `frontend/src/pages/AuthError.tsx` -- trocar o `href`/`to` do link "Voltar para o login" de `/login` pra `/login?password` -- evita loop de redirect após falha no Keycloak

**Acceptance Criteria:**
- Given `enabled:true` e sem `?password`, when a tela de login carrega, then o navegador é redirecionado pro Keycloak sem exigir clique.
- Given `enabled:true` e com `?password` na URL, when a tela carrega, then o formulário de senha aparece normalmente, com o botão de SSO também visível.
- Given `enabled:false`, when a tela de login carrega (com ou sem `?password`), then o comportamento é equivalente ao de hoje (formulário de senha, sem botão de SSO, sem redirect).
- Given `enabled:true` mas `buildLoginUrl()` falha, when isso acontece, then o usuário vê o formulário de login normal (com o erro logado no console) em vez de uma tela travada.
- Given `fetchIAMConfig()` nunca resolve, when 5s se passam, then o formulário normal aparece em vez de um spinner permanente.
- Given uma falha de login no Keycloak, when o usuário clica em "Voltar para o login" na tela de erro, then ele vê o formulário normal, sem novo auto-redirect.

## Spec Change Log

- **Iteration 1 (intent_gap, revisão adversarial Blind Hunter + Edge Case Hunter):** achado real: navegações internas do app pra `/login` sem `?password` (logout em `AuthContext.tsx`, link "Voltar para o login" em `AuthError.tsx`) também disparam o auto-redirect, criando um loop sem saída na tela de erro do Keycloak e um "logout fantasma" (reautenticação silenciosa via sessão Keycloak/AD ainda ativa). A intenção original só cobria acesso direto a `/login` e restringia a mudança só a `Login.tsx`, o que impedia corrigir o loop de `AuthError.tsx` sem renegociar o escopo. Emendado: escopo ampliado pra incluir `frontend/src/pages/AuthError.tsx` (só o link); `AuthContext.tsx`/logout permanece explicitamente fora de escopo por decisão do usuário. **KEEP:** toda a lógica de `Login.tsx` da iteração anterior (detecção via `fetchIAMConfig().enabled`, fallback `?password`, fail-safes de erro) segue válida e deve ser re-derivada sem mudanças de abordagem — só adicionar o timeout de 5s, o log de erro, e o texto acessível no loading, que também vieram da mesma revisão.

## Design Notes

A detecção de `enabled` é assíncrona (round-trip pro backend), então é impossível decidir "mostra form vs redireciona" de forma síncrona no primeiro render. Consequência: **todo servidor**, incluindo o Hostinger (`enabled:false`), passa por um spinner breve (mesma duração da chamada a `/api/auth/sso/config`, tipicamente <100ms) antes do formulário aparecer -- não é mais um render síncrono imediato como antes desta spec. Isso é um trade-off aceito, não um bug: consequência direta de reusar `fetchIAMConfig().enabled` em vez de checagem de hostname (decisão já validada com o usuário), e evita o problema pior -- o formulário de senha piscar na tela da Ferreira Costa antes do redirect.

## Verification

**Commands:**
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json` -- expected: sem erros novos em `Login.tsx`/`AuthError.tsx` (erros pré-existentes em outros arquivos não fazem parte deste escopo)

**Manual checks (if no CLI):**
- Sem os 2 servidores reais disponíveis nesta sessão pra testar lado a lado antes do deploy. Validar manualmente depois do deploy: `https://fctax.fcxlabs.com/login` deve redirecionar direto pro Keycloak; `https://fctax.fcxlabs.com/login?password` deve mostrar o formulário; `https://apuracao.fbtax.cloud/login` deve continuar sem nenhuma mudança visível; forçar uma falha de login no Keycloak e confirmar que "Voltar para o login" não entra em loop.

## Suggested Review Order

**Auto-redirect principal**

- Ponto de entrada: decide se essa carga de página pula o formulário — reusa o `enabled` já existente, sem checagem de hostname.
  [`Login.tsx:30-31`](../../frontend/src/pages/Login.tsx#L30)

- Fluxo assíncrono central: busca a config, atualiza o botão manual de SSO independente do timeout, e só então decide entre redirecionar ou revelar o formulário.
  [`Login.tsx:47`](../../frontend/src/pages/Login.tsx#L47)

- Fail-safes empilhados: timeout de 5s (linha 40) e o recheck de `settled` (linha 53) evitam travar a tela ou forçar um redirect depois que o formulário já foi revelado.
  [`Login.tsx:40`](../../frontend/src/pages/Login.tsx#L40)

- Guarda de corrida adicionada na 2ª rodada de revisão: recheca `settled` logo antes de navegar, caso o timeout já tenha vencido enquanto `buildLoginUrl()` estava em voo.
  [`Login.tsx:63`](../../frontend/src/pages/Login.tsx#L63)

- Estado de carregamento visível: spinner com `role="status"`/`aria-live`, evita o flash do formulário de senha na Ferreira Costa.
  [`Login.tsx:118`](../../frontend/src/pages/Login.tsx#L118)

**Loop de erro do Keycloak**

- Achado da 1ª rodada de revisão: sem isso, uma falha de login no Keycloak prendia o usuário num loop de redirect sem saída visível.
  [`AuthError.tsx:22`](../../frontend/src/pages/AuthError.tsx#L22)
