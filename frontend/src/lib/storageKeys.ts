// Storage keys — single source of truth for all browser storage keys used in the frontend.
//
// NEVER use string literals 'companyId', 'company_id', 'selectedCompanyId' or 'token'
// directly in page or component code. Always import the appropriate constant from here.
//
// Storage locations:
//   sessionStorage — token, companyId, user, environment, group, company, cnpj
//   localStorage   — pref_company_{userId} (persists across logout)

/** Key for the active company ID stored in sessionStorage. */
export const COMPANY_ID_KEY = 'companyId';

/** Key for the JWT access token stored in sessionStorage. */
export const TOKEN_KEY = 'token';

/** Prefix for the per-user persistent company preference stored in localStorage.
 *  Full key = COMPANY_PREF_KEY_PREFIX + userId
 */
export const COMPANY_PREF_KEY_PREFIX = 'pref_company_';
