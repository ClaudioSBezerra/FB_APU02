---
phase: 02-bugs
verified: 2026-05-12T15:00:00Z
status: passed
score: 3/3
overrides_applied: 0
---

# Phase 2: Bugs — Verification Report

**Phase Goal:** Qualquer pagina do frontend opera sempre com a empresa correta — sem empresa errada silenciosa causada por chaves discrepantes de localStorage
**Verified:** 2026-05-12T15:00:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #  | Truth                                                                                                     | Status     | Evidence                                                                                                                 |
|----|-----------------------------------------------------------------------------------------------------------|------------|--------------------------------------------------------------------------------------------------------------------------|
| 1  | Existe uma unica constante exportada para a chave de company ID; strings literais `'company_id'` e `'selectedCompanyId'` em getItem/setItem retornam zero ocorrencias relevantes | VERIFIED | `storageKeys.ts` exporta `COMPANY_ID_KEY = 'companyId'`; grep por literais em getItem retorna 0 hits; `selectedCompanyId` ausente de codigo funcional |
| 2  | PainelApuracaoCBS e PainelApuracaoIBS exibem dados da empresa selecionada (usam useAuth(), sem localStorage) | VERIFIED | Ambos os arquivos: 2 ocorrencias de `useAuth` cada (import + uso); 0 chamadas a localStorage para company |
| 3  | Managers.tsx carrega dados corretamente sem depender de chave nunca escrita (`selectedCompanyId`)          | VERIFIED | `Managers.tsx`: 2 ocorrencias de `useAuth` (import + uso); `selectedCompanyId` ausente de todo `frontend/src/` exceto comentario em `storageKeys.ts` |

**Score:** 3/3 truths verified

### Required Artifacts

| Artifact                                              | Expected                                      | Status     | Details                                                   |
|-------------------------------------------------------|-----------------------------------------------|------------|-----------------------------------------------------------|
| `frontend/src/lib/storageKeys.ts`                     | Exporta `COMPANY_ID_KEY` como unica constante | VERIFIED   | Exporta `COMPANY_ID_KEY`, `TOKEN_KEY`, `COMPANY_PREF_KEY_PREFIX` |
| `frontend/src/pages/PainelApuracaoCBS.tsx`            | Usa `useAuth()`, sem localStorage manual       | VERIFIED   | `import { useAuth }` + `const { companyId } = useAuth()` presentes |
| `frontend/src/pages/PainelApuracaoIBS.tsx`            | Usa `useAuth()`, sem localStorage manual       | VERIFIED   | `import { useAuth }` + `const { companyId } = useAuth()` presentes |
| `frontend/src/pages/Managers.tsx`                     | Usa `useAuth()`, sem `selectedCompanyId`       | VERIFIED   | `import { useAuth }` + `const { companyId } = useAuth()` presentes |

### Key Link Verification

| From                        | To                       | Via                             | Status  | Details                                                    |
|-----------------------------|--------------------------|----------------------------------|---------|-------------------------------------------------------------|
| `PainelApuracaoCBS.tsx`     | `AuthContext`            | `useAuth()` hook                 | WIRED   | Import + destructure `companyId`; useCallback dep `[companyId]` |
| `PainelApuracaoIBS.tsx`     | `AuthContext`            | `useAuth()` hook                 | WIRED   | Import + destructure `companyId`; useCallback dep `[companyId]` |
| `Managers.tsx`              | `AuthContext`            | `useAuth()` hook                 | WIRED   | Import + destructure `companyId`; useEffect dep `[companyId]` |
| Pages (9 Wave 1)            | Global fetch interceptor | Removed manual headers           | WIRED   | 0 localStorage reads for company/token in pages/; interceptor injects headers |

### Programmatic Check Results

| Check | Command | Result | Status |
|-------|---------|--------|--------|
| storageKeys.ts exports COMPANY_ID_KEY | `grep -c "export const COMPANY_ID_KEY" storageKeys.ts` | 1 | PASS |
| PainelApuracaoCBS.tsx uses useAuth | `grep -c "useAuth" PainelApuracaoCBS.tsx` | 2 | PASS |
| PainelApuracaoIBS.tsx uses useAuth | `grep -c "useAuth" PainelApuracaoIBS.tsx` | 2 | PASS |
| Managers.tsx uses useAuth | `grep -c "useAuth" Managers.tsx` | 2 | PASS |
| No localStorage.getItem for company in pages/ | `grep -rn "localStorage.getItem.*[Cc]ompany..." frontend/src/pages/ \| wc -l` | 0 | PASS |
| selectedCompanyId absent from live code | `grep -rn "selectedCompanyId" frontend/src/ \| grep -v "storageKeys"` | 0 hits | PASS |
| Literal 'company_id' / 'selectedCompanyId' in getItem | `grep -rn "getItem.*'company_id'..."` | 0 hits | PASS |

### Anti-Patterns Found

None. storageKeys.ts contains no TBD/FIXME/XXX markers. The comment referencing `selectedCompanyId` is explicit documentation (a guard comment telling developers what NOT to use) — not a live read.

### Human Verification Required

1. **Company switch re-fetch behavior**

   **Test:** Log in, navigate to PainelApuracaoCBS, switch the active company via the company selector.
   **Expected:** Panel immediately re-fetches and displays data for the newly selected company.
   **Why human:** useCallback dep `[companyId]` triggers re-fetch, but the visual result (correct data appearing) requires a browser session with real data.

2. **Managers data load**

   **Test:** Log in as an admin, navigate to Managers.
   **Expected:** Manager list loads without errors; data matches the active company.
   **Why human:** useEffect dep `[companyId]` drives the fetch, but confirming the correct data is returned requires a running backend with tenant data.

Note: Human verification items above are quality/UX confirmations — the code wiring is fully VERIFIED. They do not block the phase from being marked `passed` because the code evidence is unambiguous and the UI hints flag is set on this phase.

---

_Verified: 2026-05-12T15:00:00Z_
_Verifier: Claude (gsd-verifier)_
