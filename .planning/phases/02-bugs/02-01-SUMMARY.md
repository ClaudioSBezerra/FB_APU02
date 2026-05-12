---
phase: 02-bugs
plan: 01
subsystem: frontend
tags: [bug-fix, storage, authentication, fetch-interceptor]
dependency_graph:
  requires: []
  provides: [storageKeys-constants, clean-fetch-headers]
  affects: [frontend/src/pages/RFB*.tsx, frontend/src/pages/CGIBS*.tsx, frontend/src/pages/LimparDadosApuracao.tsx]
tech_stack:
  added: [frontend/src/lib/storageKeys.ts]
  patterns: [global-fetch-interceptor-delegation]
key_files:
  created:
    - frontend/src/lib/storageKeys.ts
  modified:
    - frontend/src/pages/RFBCredentials.tsx
    - frontend/src/pages/RFBApuracao.tsx
    - frontend/src/pages/RFBDebitos.tsx
    - frontend/src/pages/RFBCreditosCBS.tsx
    - frontend/src/pages/LimparDadosApuracao.tsx
    - frontend/src/pages/CGIBSPainel.tsx
    - frontend/src/pages/CGIBSApuracao.tsx
    - frontend/src/pages/CGIBSDebitos.tsx
    - frontend/src/pages/CGIBSCredentials.tsx
decisions:
  - "storageKeys.ts exporta COMPANY_ID_KEY, TOKEN_KEY e COMPANY_PREF_KEY_PREFIX como unica fonte de verdade para chaves de storage"
  - "Paginas delegam Authorization e X-Company-ID ao interceptor global em AuthContext.tsx; nao importam COMPANY_ID_KEY diretamente pois nao precisam do valor em logica de UI"
metrics:
  duration: ~8min
  completed: 2026-05-12
  tasks_completed: 2
  files_modified: 10
---

# Phase 02 Plan 01: BUG-01 — storageKeys.ts + Remocao de Leituras Manuais de localStorage

**One-liner:** Criar constante unificada COMPANY_ID_KEY e remover 9 paginas que liam token/companyId do localStorage (errado) em vez de delegar ao interceptor global de fetch.

## What Was Built

### Task 1 — frontend/src/lib/storageKeys.ts

Novo arquivo com tres constantes exportadas:
- `COMPANY_ID_KEY = 'companyId'` — chave unica para company ID em sessionStorage
- `TOKEN_KEY = 'token'` — chave do JWT em sessionStorage
- `COMPANY_PREF_KEY_PREFIX = 'pref_company_'` — prefixo para preferencia persistente em localStorage

### Task 2 — 9 paginas corrigidas

Padrao de correcao aplicado em cada pagina:
1. Removidos `localStorage.getItem('token')` e `localStorage.getItem('companyId')` / `localStorage.getItem('company_id')`
2. Removidos headers manuais `Authorization: Bearer ...` e `X-Company-ID: ...`
3. Removidos helpers `getHeaders()` onde existiam (RFBApuracao, RFBDebitos, CGIBSApuracao, CGIBSDebitos, CGIBSCredentials)
4. Em CGIBSPainel.tsx: removidas as variaveis `token` e `companyID` capturadas no nivel do componente; dependencias `[token, companyID]` do `useCallback` removidas

Paginas corrigidas:
- `RFBCredentials.tsx` — 4 fetch calls (fetchCredential, handleSave, handleDelete, handleSaveSchedule)
- `RFBApuracao.tsx` — helper getHeaders() + 7 usos (fetchRequests, fetchCredentials banner, solicitar, delete, clearErrors, reprocess, download)
- `RFBDebitos.tsx` — helper getHeaders() (useCallback) + openDanfe() + 3 fetch calls
- `RFBCreditosCBS.tsx` — fetchCreditos (token + companyId inline)
- `LimparDadosApuracao.tsx` — handleLimpar (token + companyId inline)
- `CGIBSPainel.tsx` — variaveis token/companyID no nivel do componente + fetchData headers + deps do useCallback
- `CGIBSApuracao.tsx` — helper getHeaders() + 4 fetch calls
- `CGIBSDebitos.tsx` — helper getHeaders() + 2 fetch calls
- `CGIBSCredentials.tsx` — helper getHeaders() + 4 fetch calls

## Verification

```
grep -rn "localStorage.getItem('token')..." [9 pages] -> CLEAN (0 matches)
npx tsc --noEmit -> PASSED (0 errors)
```

## Deviations from Plan

None — plan executed exactly as written.

Nota: O plano menciona "8 paginas" no output mas lista 9 arquivos. Foram corrigidas todas as 9 paginas listadas na secao `<files>`.

Nota adicional: Nenhuma das 9 paginas precisou importar `useAuth()` para logica de UI — todas usavam `companyId` exclusivamente em headers de fetch. A remocao completa foi suficiente em todos os casos.

## Known Stubs

None.

## Threat Flags

None — nenhuma nova superficie de rede ou caminho de autenticacao introduzida. Esta correcao apenas remove codigo que enviava headers vazios.

## Self-Check: PASSED

- `frontend/src/lib/storageKeys.ts` — FOUND
- Commit a4d3841 — FOUND
- `npx tsc --noEmit` — PASSED (no output = no errors)
- grep localStorage CLEAN on all 9 pages — VERIFIED
