---
phase: 260515-j7y
plan: 01
subsystem: rfb-conciliacao
tags: [rfb, pagamentos-fornecedores, cbs, conciliacao, frontend, backend]
dependency_graph:
  requires: [pagamentos_fornecedores table (migration 110), rfb_creditos table (migration 096)]
  provides: [GET /api/rfb/pagamentos-fornecedores, RFBPagamentosFornecedores page]
  affects: [navigation.ts rfb module, App.tsx route tree]
tech_stack:
  added: []
  patterns: [CTE SQL cross-join, handler factory pattern, useCallback+companyId dependency]
key_files:
  created:
    - backend/handlers/rfb_pagamentos_fornecedores.go
    - frontend/src/pages/RFBPagamentosFornecedores.tsx
  modified:
    - backend/main.go
    - frontend/src/lib/navigation.ts
    - frontend/src/App.tsx
decisions:
  - Summary cards always show full-month totals (status filter only affects the paginated list)
  - CTE SQL uses LEFT JOINs in ON clause (not WHERE) so rows without nfe/cte still appear
  - statusFilter='' means "all" in the SQL (avoids a separate code path)
metrics:
  duration: "4m 17s"
  completed: "2026-05-15T17:00:00Z"
  tasks_completed: 2
  files_changed: 5
---

# Phase 260515-j7y Plan 01: RFB Pgtos Fornecedores — Painel de Conciliação CBS Summary

RFB conciliation panel cross-referencing supplier payment records (pagamentos_fornecedores) with CBS credit extinction status in rfb_creditos, surfacing the "paid but CBS not extinto" risk pattern in a single screen with 4 summary cards, expandable rows showing individual parcelas, Excel export and multi-tenant switching.

## What Was Built

### Backend — GET /api/rfb/pagamentos-fornecedores

New handler `RFBPagamentosFornecedoresHandler` in `backend/handlers/rfb_pagamentos_fornecedores.go`.

**Response shape:**
```json
{
  "sumario": {
    "total_pago": 0.0,
    "total_notas": 0,
    "cbs_extinto": 0.0,
    "notas_extinto": 0,
    "cbs_pendente": 0.0,
    "notas_pendente": 0,
    "notas_sem_dados": 0
  },
  "items": [
    {
      "chave_doc": "...",
      "tipo_doc": "NFE|CTE",
      "forn_cnpj": "...",
      "forn_nome": "...",
      "num_parcelas": 1,
      "total_pago": 0.0,
      "primeira_parcela": "2026-01-15",
      "ultima_parcela": "2026-01-15",
      "valor_nota": 0.0,
      "valor_cbs_nota": 0.0,
      "valor_ibs_nota": 0.0,
      "valor_cbs_nao_extinto": 0.0,
      "situacao_credito": null,
      "status_conciliacao": "pendente|extinto|sem_dados"
    }
  ],
  "page": 1,
  "page_size": 50,
  "total": 0
}
```

**Query params:** `mes_ano` (YYYY-MM), `status` (pendente|extinto|sem_dados|all), `forn_cnpj` (digits only), `page`, `page_size` (max 200).

**SQL design:** Single `WITH pagamentos_agg AS (...), conciliacao AS (...)` CTE run three times — once for the summary (no status filter), once for COUNT (with status filter for pagination), once for the paginated list. The LEFT JOINs to `nfe_entradas` and `cte_entradas` use `tipo_doc = 'NFE'/'CTE'` predicates inside the ON clause so unmatched rows still appear with COALESCE-zero values.

**Route:** Registered in `main.go` with `withAuth(..., "")` — any authenticated user, consistent with all other `/api/rfb/*` list endpoints.

### Frontend — RFBPagamentosFornecedores page

New page at `frontend/src/pages/RFBPagamentosFornecedores.tsx`.

- **4 summary cards:** Total Pago (blue), CBS Extinto (green), CBS Pendente (red/double-border — risk card), Sem dados RFB (gray)
- **Filter panel:** `<input type="month">` for mes_ano, `<select>` for status, text input for CNPJ, Aplicar button
- **Table:** 9 columns — chevron, Fornecedor (name + CNPJ), Tipo, Chave Doc (truncated with full key in title), Parcelas, Total Pago, Valor CBS, Status badge, CBS Pendente (red when > 0, dash otherwise)
- **Row expansion:** Click row to toggle. Fetches `/api/pagamentos-fornecedores?chave_doc=<key>&page_size=200` on first expand, caches result to avoid duplicate fetches
- **Excel export:** Fetches with `page_size=9999` then produces `conciliacao-cbs-YYYYMMDD.xlsx` via xlsx library
- **Multi-tenancy:** `companyId` from `useAuth()` is in the `useCallback` dependency array — page re-fetches automatically on company switch
- **No manual auth headers:** global fetch interceptor in AuthContext handles Authorization + X-Company-ID

### Navigation change

`frontend/src/lib/navigation.ts` line 74: removed `disabled: true` from the `Pgtos Fornecedores` tab in the `rfb` module. Tab now renders as a clickable Link.

### Route change

`frontend/src/App.tsx`: replaced `<ComingSoon title="Pagamentos CBS a Fornecedores" />` with `<RFBPagamentosFornecedores />` at route `/rfb/pagamentos-fornecedores`.

## Commits

| Task | Commit | Description |
|------|--------|-------------|
| 1 — Backend handler + route | dc553c3 | feat(260515-j7y-01): backend handler + route GET /api/rfb/pagamentos-fornecedores |
| 2 — Frontend page + nav/route | 378f373 | feat(260515-j7y-01): frontend page RFBPagamentosFornecedores + navigation + route |

## Deviations from Plan

None — plan executed exactly as written.

Column names verified from migrations before implementation:
- `nfe_entradas`: `v_nf`, `v_cbs`, `v_ibs` — confirmed in migration 059
- `cte_entradas`: `v_prest`, `v_cbs`, `v_ibs` — confirmed in migration 060
- `rfb_creditos`: `valor_cbs_nao_extinto`, `situacao_credito`, `chave_dfe` — confirmed in migration 096

## Known Stubs

None — all data is wired from real API calls. The `valor_nota`, `valor_cbs_nota`, `valor_ibs_nota` fields will be 0 for notes not yet imported (no NF-e/CT-e record), which is correct behavior (LEFT JOIN resolves to 0 via COALESCE).

## Threat Flags

None — endpoint follows the same multi-tenant scoping as all other `/api/rfb/*` handlers. No new trust boundaries introduced.

## Self-Check: PASSED

- `backend/handlers/rfb_pagamentos_fornecedores.go` exists: FOUND
- `frontend/src/pages/RFBPagamentosFornecedores.tsx` exists: FOUND
- Commit dc553c3 exists: FOUND
- Commit 378f373 exists: FOUND
- `go build ./...` clean: PASSED
- `npx tsc --noEmit` clean: PASSED
- No file deletions in commits: PASSED
