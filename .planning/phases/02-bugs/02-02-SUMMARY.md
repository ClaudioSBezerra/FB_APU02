---
phase: 02-bugs
plan: 02
status: complete
requirements: [BUG-02, BUG-03]
subsystem: frontend
tags: [bug-fix, auth, useAuth, localStorage, company-scoping]
completed_at: "2026-05-12T14:36:00Z"
duration_minutes: 3
tasks_completed: 2
tasks_total: 2
files_modified:
  - frontend/src/pages/PainelApuracaoCBS.tsx
  - frontend/src/pages/PainelApuracaoIBS.tsx
  - frontend/src/pages/Managers.tsx
key_decisions:
  - "useCallback dependency set to [companyId] in CBS/IBS panels so re-fetch triggers on company switch"
  - "Managers.tsx fetchManagers kept as plain async function; useEffect([companyId]) handles re-fetch"
  - "eslint-disable comment added on Managers useEffect to suppress react-hooks/exhaustive-deps for fetchManagers"
---

# Phase 02 Plan 02: Fix BUG-02 / BUG-03 — CBS, IBS, Managers migrados para useAuth()

**One-liner:** Eliminados localStorage.getItem errados em 3 páginas; migradas para useAuth() + interceptor global do AuthContext.

## What Was Broken

### BUG-02 — PainelApuracaoCBS.tsx e PainelApuracaoIBS.tsx

Ambas as páginas liam:
- `localStorage.getItem("token")` — vazio desde Phase 1 (JWT nunca escrito em localStorage)
- `localStorage.getItem("company_id")` — chave nunca escrita por nenhum componente

Resultado: fetch enviava `Authorization: Bearer ` e `X-Company-ID: ` vazios. Como os headers estavam presentes (mesmo vazios), o interceptor global do AuthContext os via e **não sobrescrevia** (o interceptor só injeta quando o header está ausente). O backend recebia empresa vazia = dados sem filtro de empresa ou erro.

### BUG-03 — Managers.tsx

Lia:
- `localStorage.getItem('token')` — vazio
- `localStorage.getItem('selectedCompanyId')` — chave nunca escrita por nenhum componente

Mesmo problema: todos os fetch calls (fetchManagers, handleSubmit, handleDelete) enviavam X-Company-ID e Authorization vazios. A tela nunca mostrava dados corretos.

## What Was Fixed

### Task 1 — PainelApuracaoCBS.tsx e PainelApuracaoIBS.tsx

1. Adicionado `import { useAuth } from "@/contexts/AuthContext"`
2. Adicionado `const { companyId } = useAuth()` no topo do componente
3. Removidas as linhas `const token = localStorage.getItem("token") || ""` e `const companyID = localStorage.getItem("company_id") || ""`
4. Removidos os headers `Authorization` e `X-Company-ID` do fetch — o interceptor injeta automaticamente
5. `useCallback` agora usa `[companyId]` como dependência, garantindo re-fetch quando o usuário troca de empresa

### Task 2 — Managers.tsx

1. Adicionado `import { useAuth } from '@/contexts/AuthContext'`
2. Adicionado `const { companyId } = useAuth()` no topo
3. Removidas todas as leituras de localStorage em `fetchManagers`, `handleSubmit` e `handleDelete`
4. Removidos headers `Authorization` e `X-Company-ID` de todos os fetch calls (interceptor cuida)
5. `Content-Type: application/json` mantido em handleSubmit (necessário para POST/PUT com body)
6. `useEffect` atualizado para `[companyId]` como dependência — re-fetch automático ao trocar empresa

## Verification Results

```
grep localStorage em 3 arquivos: CLEAN (nenhuma ocorrência)

grep useAuth:
  PainelApuracaoCBS.tsx:5:  import { useAuth }
  PainelApuracaoCBS.tsx:46: const { companyId } = useAuth()
  PainelApuracaoIBS.tsx:5:  import { useAuth }
  PainelApuracaoIBS.tsx:52: const { companyId } = useAuth()
  Managers.tsx:2:           import { useAuth }
  Managers.tsx:16:          const { companyId } = useAuth()

grep selectedCompanyId em frontend/src/:
  storageKeys.ts:3: comentário "NEVER use ... 'selectedCompanyId'" — apenas documentação, não uso

npx tsc --noEmit: PASSED (zero erros)
```

## Commits

| Task | Commit | Message |
|------|--------|---------|
| Task 1 + Task 2 | ebacb90 | fix(bug-02-03): PainelApuracaoCBS/IBS e Managers migrados para useAuth() — empresa correta |

## Deviations from Plan

None — plan executed exactly as written.

## Self-Check: PASSED

- [x] frontend/src/pages/PainelApuracaoCBS.tsx exists and modified
- [x] frontend/src/pages/PainelApuracaoIBS.tsx exists and modified
- [x] frontend/src/pages/Managers.tsx exists and modified
- [x] Commit ebacb90 exists
- [x] Zero localStorage references in 3 pages
- [x] useAuth imported and used in all 3
- [x] selectedCompanyId absent from functional code
- [x] TypeScript compiles clean
