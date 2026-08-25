---
title: 'Login: corrigir post_logout_redirect_uri pro valor exato cadastrado no Keycloak'
type: 'bugfix'
created: '2026-08-25'
status: 'done'
route: 'one-shot'
---

# Login: corrigir post_logout_redirect_uri pro valor exato cadastrado no Keycloak

## Intent

**Problem:** `spec-auth-sso-keycloak-logout.md` enviava `post_logout_redirect_uri=https://fctax.fcxlabs.com/login?password` ao Keycloak. O time do Marlos (TI Ferreira Costa) cadastrou no client `fb-apu02`, em "Valid Post Logout Redirect URIs", exatamente `https://fctax.fcxlabs.com/login` (sem `?password`) — Keycloak valida essa URI por igualdade exata, então o `end_session` seria rejeitado.

**Approach:** Trocar o valor enviado para `${window.location.origin}/login`, sem query string, batendo com o cadastro real. O `?password` original existia só pra evitar reabrir o auto-redirect de `Login.tsx` ao voltar do Keycloak — mas isso já é garantido de forma independente da query string pela flag `SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT` (gravada por `logout()` antes de navegar, lida-e-removida por `Login.tsx` no mount), que sobrevive à ida-e-volta pelo domínio do Keycloak porque `sessionStorage` é isolado por origem+aba, não por navegação. Confirmado por revisão adversarial (Blind Hunter) que a lógica de sobrevivência da flag está correta.

## Suggested Review Order

- Único ponto alterado: a URI exata que o Keycloak exige, com o motivo documentado inline (por que dispensa `?password`).
  [`AuthContext.tsx:209`](../../frontend/src/contexts/AuthContext.tsx#L209)

- Mecanismo que garante que o auto-redirect continua suprimido sem depender da query string — não alterado nesta correção, só reconfirmado pela revisão.
  [`Login.tsx:40`](../../frontend/src/pages/Login.tsx#L40)
