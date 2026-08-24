/**
 * handleCallback — troca o `code` do callback OIDC por uma sessão própria do FB_APU02.
 *
 * Diferente do template original da skill (que guarda o token do Keycloak na SPA via
 * tokenStore/userStore para uso contínuo): aqui o token do Keycloak só vive o tempo desta
 * chamada. Depois de validado pelo backend (POST /api/auth/sso/keycloak, protegido por
 * iam.IAMAuthMiddleware), a resposta já vem no mesmo formato do login por senha — quem
 * chama esta função (AuthCallback.tsx) repassa o resultado direto pra `useAuth().login()`,
 * reaproveitando 100% do gerenciamento de sessão que já existe (AuthContext).
 */

import { SESSION_KEY_VERIFIER, SESSION_KEY_STATE, fetchIAMConfig } from './buildLoginUrl';

export class CallbackError extends Error {
  constructor(message: string, public readonly code: string) {
    super(message);
  }
}

export async function handleCallback(searchParams: URLSearchParams): Promise<any> {
  const code = searchParams.get('code');
  const state = searchParams.get('state');
  const errorParam = searchParams.get('error');

  if (errorParam) {
    throw new CallbackError(`Keycloak retornou erro: ${errorParam}`, 'keycloak_error');
  }
  if (!code || !state) {
    throw new CallbackError('Callback sem code/state.', 'missing_params');
  }

  const savedState = sessionStorage.getItem(SESSION_KEY_STATE);
  const verifier = sessionStorage.getItem(SESSION_KEY_VERIFIER);
  sessionStorage.removeItem(SESSION_KEY_STATE);
  sessionStorage.removeItem(SESSION_KEY_VERIFIER);

  if (!verifier || !savedState || savedState !== state) {
    throw new CallbackError('State inválido (possível CSRF) ou sessão de login expirada.', 'invalid_state');
  }

  const config = await fetchIAMConfig();
  if (!config.enabled || !config.base_url || !config.client_id || !config.redirect_uri) {
    throw new CallbackError('SSO Keycloak não configurado neste servidor.', 'iam_not_configured');
  }

  // 1. Troca code por access_token do Keycloak (fluxo OIDC padrão, direto no realm)
  const tokenResp = await fetch(`${config.base_url}/protocol/openid-connect/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'authorization_code',
      client_id: config.client_id,
      redirect_uri: config.redirect_uri,
      code,
      code_verifier: verifier,
    }),
  });
  if (!tokenResp.ok) {
    throw new CallbackError('Falha ao trocar código por token no Keycloak.', 'token_exchange_failed');
  }
  const tokenData = await tokenResp.json();
  const accessToken = tokenData.access_token as string | undefined;
  if (!accessToken) {
    throw new CallbackError('Resposta do Keycloak sem access_token.', 'token_exchange_failed');
  }

  // 2. Troca o access_token do Keycloak pela sessão própria do FB_APU02 — o token do
  // Keycloak não é usado novamente depois deste ponto.
  const sessionResp = await fetch('/api/auth/sso/keycloak', {
    method: 'POST',
    headers: { Authorization: `Bearer ${accessToken}` },
  });
  if (!sessionResp.ok) {
    const text = await sessionResp.text();
    let message = text;
    try {
      const parsed = JSON.parse(text);
      if (typeof parsed?.error === 'string') message = parsed.error;
    } catch {
      // corpo não era JSON — usa o texto cru mesmo
    }
    throw new CallbackError(message || 'Falha ao concluir login via Keycloak.', 'sso_exchange_failed');
  }
  return sessionResp.json();
}
