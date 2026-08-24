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
 */

import { sha256 } from '@noble/hashes/sha2.js';

export const SESSION_KEY_VERIFIER = 'iam_pkce_verifier';
export const SESSION_KEY_STATE = 'iam_oauth_state';

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
 * Throws if VITE_IAM_BASE_URL or VITE_IAM_CLIENT_ID are not configured.
 */
export async function buildLoginUrl(): Promise<string> {
  const iamBaseUrl = import.meta.env.VITE_IAM_BASE_URL as string | undefined;
  const clientId = import.meta.env.VITE_IAM_CLIENT_ID as string | undefined;
  const redirectUri = import.meta.env.VITE_IAM_REDIRECT_URI as string | undefined;
  const scopes = (import.meta.env.VITE_IAM_SCOPES as string | undefined) ?? 'openid profile email';

  if (!iamBaseUrl || !clientId || !redirectUri) {
    throw new Error(
      'IAM not configured: set VITE_IAM_BASE_URL, VITE_IAM_CLIENT_ID, and VITE_IAM_REDIRECT_URI',
    );
  }

  const verifier = await generateCodeVerifier();
  const challenge = await generateCodeChallenge(verifier);
  const state = generateUUID();

  sessionStorage.setItem(SESSION_KEY_VERIFIER, verifier);
  sessionStorage.setItem(SESSION_KEY_STATE, state);

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: clientId,
    redirect_uri: redirectUri,
    scope: scopes,
    state,
    code_challenge: challenge,
    code_challenge_method: 'S256',
  });

  return `${iamBaseUrl}/protocol/openid-connect/auth?${params.toString()}`;
}

/** Returns true if VITE_IAM_BASE_URL is configured (IAM mode active). */
export function isIAMEnabled(): boolean {
  return Boolean(
    import.meta.env.VITE_IAM_BASE_URL &&
    import.meta.env.VITE_IAM_CLIENT_ID &&
    import.meta.env.VITE_IAM_REDIRECT_URI,
  );
}
