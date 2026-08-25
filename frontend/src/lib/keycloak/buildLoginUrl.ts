/**
 * buildLoginUrl — builds the Keycloak OAuth2/OIDC authorization URL with PKCE.
 *
 * Uses @noble/hashes for SHA-256 (works without HTTPS / secure context).
 *  - code_verifier: 43–128 chars of random URL-safe base64
 *  - code_challenge: SHA-256 hash of the verifier (S256 method)
 *  - state: crypto.randomUUID() for CSRF protection
 *
 * The verifier and state are stored in sessionStorage so handleCallback
 * can retrieve them on redirect.
 *
 * A config do Keycloak (realm URL, client_id, redirect_uri, scopes) é buscada em
 * RUNTIME do backend (/api/auth/sso/config), não lida de env var de build (VITE_IAM_*)
 * — a mesma imagem de frontend é compartilhada entre servidores com configs diferentes
 * (ex: Hostinger multi-cliente sem SSO, AWS dedicado à Ferreira Costa com SSO); cravar
 * a config no build vazaria o botão pra quem não devia ter.
 */

import { sha256 } from '@noble/hashes/sha2.js';

export const SESSION_KEY_VERIFIER = 'iam_pkce_verifier';
export const SESSION_KEY_STATE = 'iam_oauth_state';
export const SESSION_KEY_AUTH_VIA_SSO = 'auth_via_sso';
export const SESSION_KEY_LOGOUT_SUPPRESS_REDIRECT = 'sso_logout_suppress_redirect';

export interface IAMConfig {
  enabled: boolean;
  base_url?: string;
  client_id?: string;
  redirect_uri?: string;
  scopes?: string;
}

let cachedConfig: IAMConfig | null = null;

/** Busca (e cacheia em memória) a config de SSO deste servidor. Nunca lança — em
 * qualquer falha de rede/parse, retorna enabled:false (fail-safe: esconde o botão
 * em vez de quebrar a tela de login). */
export async function fetchIAMConfig(): Promise<IAMConfig> {
  if (cachedConfig) return cachedConfig;
  try {
    const res = await fetch('/api/auth/sso/config');
    if (!res.ok) return { enabled: false }; // não cacheia falha — próxima chamada tenta de novo
    const data = (await res.json()) as IAMConfig;
    cachedConfig = data;
    return data;
  } catch {
    return { enabled: false };
  }
}

function base64UrlEncode(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let str = '';
  bytes.forEach((b) => (str += String.fromCharCode(b)));
  return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
}

async function generateCodeVerifier(): Promise<string> {
  const random = new Uint8Array(48); // 48 bytes → 64 chars base64url
  crypto.getRandomValues(random);
  return base64UrlEncode(random.buffer as ArrayBuffer);
}

// RFC 4122 v4 UUID using getRandomValues (works without HTTPS)
function generateUUID(): string {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0'));
  return `${h.slice(0, 4).join('')}-${h.slice(4, 6).join('')}-${h.slice(6, 8).join('')}-${h.slice(8, 10).join('')}-${h.slice(10).join('')}`;
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(verifier);
  const hash = sha256(data);
  return base64UrlEncode(hash.buffer as ArrayBuffer);
}

/**
 * Generates PKCE params, persists verifier + state in sessionStorage, and
 * returns the full Keycloak authorization URL.
 *
 * Throws if o backend deste servidor não tem o SSO configurado (`enabled:false` ou
 * campos essenciais ausentes) — o chamador (botão de login) só deve invocar isto
 * depois de já ter confirmado `fetchIAMConfig().enabled === true`.
 */
export async function buildLoginUrl(): Promise<string> {
  const config = await fetchIAMConfig();
  if (!config.enabled || !config.base_url || !config.client_id || !config.redirect_uri) {
    throw new Error('SSO Keycloak não configurado neste servidor.');
  }

  const verifier = await generateCodeVerifier();
  const challenge = await generateCodeChallenge(verifier);
  const state = generateUUID();

  sessionStorage.setItem(SESSION_KEY_VERIFIER, verifier);
  sessionStorage.setItem(SESSION_KEY_STATE, state);

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: config.client_id,
    redirect_uri: config.redirect_uri,
    scope: config.scopes || 'openid profile email',
    state,
    code_challenge: challenge,
    code_challenge_method: 'S256',
  });

  return `${config.base_url}/protocol/openid-connect/auth?${params.toString()}`;
}
