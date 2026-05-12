---
phase: 01-seguranca
plan: "03"
subsystem: frontend-auth
tags: [security, jwt, sessionstorage, localstorage, xss]
requirements: [SEC-03]

dependency_graph:
  requires: []
  provides: [AuthContext com JWT em sessionStorage]
  affects: [frontend/src/contexts/AuthContext.tsx]

tech_stack:
  added: []
  patterns: [sessionStorage para dados de sessão, localStorage apenas para preferências persistentes]

key_files:
  modified:
    - frontend/src/contexts/AuthContext.tsx

decisions:
  - "sessionStorage.clear() no logout — prefs pref_company_* nunca estiveram em sessionStorage, logo não precisam de restauração"
  - "Dois sessionStorage.getItem('token') são corretos: um no restore de sessão, outro em switchCompany"
  - "Risco residual T-03-02 aceito: sessionStorage ainda acessível via XSS, mas mitigação completa (httpOnly cookie) é SEC-V2-01"

metrics:
  duration: "3 min"
  completed_date: "2026-05-12T13:07:49Z"
  tasks_completed: 1
  tasks_total: 1
  files_changed: 1
---

# Phase 1 Plan 03: Migrar JWT para sessionStorage — Summary

## One-liner

Migração cirúrgica do JWT de `localStorage` para `sessionStorage` em `AuthContext.tsx`, encerrando a sessão automaticamente ao fechar o browser e reduzindo a janela de exposição a ataques XSS persistentes (SEC-03).

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Migrar localStorage para sessionStorage em AuthContext.tsx | e762182 | frontend/src/contexts/AuthContext.tsx |

## What Was Built

- **Token e dados de sessão** (`token`, `user`, `environment`, `group`, `company`, `companyId`, `cnpj`, `session_expired`) migrados de `localStorage` para `sessionStorage`
- **Logout simplificado**: de um loop de preservação de prefs + `localStorage.clear()` para um único `sessionStorage.clear()` — as prefs `pref_company_*` ficam intactas no `localStorage` onde sempre estiveram
- **Handler de 401 simplificado**: idem — `sessionStorage.clear()` sem loop
- **Preferências de empresa** (`pref_company_{userId}`) mantidas exclusivamente em `localStorage` (persistência intencional entre sessões)
- **Interceptor de fetch** continua funcionando normalmente via `tokenRef` e `companyIdRef`

## Acceptance Criteria Verification

| Critério | Resultado |
|----------|-----------|
| `localStorage.getItem/setItem('token')` = 0 ocorrências | PASS (0) |
| `sessionStorage.setItem('token')` = exatamente 1 | PASS (1, no bloco `login`) |
| `sessionStorage.getItem('token')` >= 1 | PASS (2: restore + switchCompany) |
| `pref_company` em sessionStorage = 0 | PASS (0) |
| Build TypeScript sem erros | PASS (vite build com sucesso) |

## Deviations from Plan

None - plano executado exatamente como especificado.

## Known Stubs

None.

## Threat Flags

Nenhuma nova superfície de ataque introduzida. Ameaças T-03-02 e T-03-04 documentadas no plano como `accept` — risco residual conhecido.

## Self-Check: PASSED

- [x] `frontend/src/contexts/AuthContext.tsx` existe e modificado
- [x] Commit `e762182` existe em `worktree-agent-ade08f7ffec928768`
- [x] Build frontend passou sem erros TypeScript
