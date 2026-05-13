---
status: complete
date: 2026-05-13
quick_id: 20260513-dashboard-resumo-fiscal
tags: [dashboard, ibs, cbs, frontend, backend, fiscal]
---

# Dashboard de Resumo Fiscal — Summary

Adicionada página "Resumo Fiscal" com endpoint REST e UI React exibindo KPI cards IBS/CBS, gráfico horizontal de documentos por tipo e painel de créditos/débitos filtrado por mês/ano.

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `backend/handlers/dashboard.go` | Created | Handler `DashboardResumoHandler` — 3 queries separadas, campos derivados IBS/CBS |
| `backend/main.go` | Modified | Rota `GET /api/dashboard/resumo` registrada com `withAuth` |
| `frontend/src/pages/DashboardResumo.tsx` | Created | Página com 5 KPI cards, gráfico Recharts horizontal e painel créditos/débitos |
| `frontend/src/App.tsx` | Modified | Import + Route path="/painel/resumo-fiscal" adicionados |
| `frontend/src/lib/navigation.ts` | Modified | Tab "Resumo Fiscal" adicionada ao módulo `painel` |

## Commits

| Hash | Description |
|------|-------------|
| de2bdba | feat(dashboard): criar handler DashboardResumoHandler com 3 queries e campos derivados IBS/CBS |
| 61a5c21 | feat(dashboard): registrar rota /api/dashboard/resumo em main.go |
| d241adc | feat(dashboard): criar página DashboardResumo com KPI cards, gráfico e painel IBS/CBS |
| 88c7758 | feat(dashboard): registrar rota /painel/resumo-fiscal em App.tsx e tab no módulo painel |

## Key Decisions

### 1. 3 queries separadas vs UNION

Optado por 3 db.QueryRow separadas (uma por tabela) em vez de UNION ALL por três razões:
- cte_entradas usa v_rec em vez de v_nf — um UNION exigiria alias artificial
- Cada bloco é retornado individualmente no JSON (nfe_entradas, nfe_saidas, cte_entradas)
- Código mais legível; overhead de 3 queries simples é negligenciável

### 2. aliquota_efetiva_ibs como ponteiro *float64 / null no JSON

Usar *float64 no Go garante que o campo sai como null no JSON quando o denominador (v_bc_ibs_cbs de nfe_saidas) é zero, evitando divisão por zero. No TypeScript o tipo é number | null, e o componente renderiza '--' para null.

### 3. Toggle Quantidade/Valores sem re-fetch

O estado chartMode controla apenas como os dados já carregados são formatados no array passado ao BarChart. Não há nova requisição ao backend ao trocar o modo — count, v_nf e v_rec já chegam na mesma resposta JSON.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Tipo do formatter do Recharts Tooltip**
- Found during: Task 3, verificação TypeScript
- Issue: formatter={(value: number) => ...} — Recharts tipifica o value como number | undefined
- Fix: Alterado para (value: number | undefined) => { const v = value ?? 0; ... }
- Files modified: frontend/src/pages/DashboardResumo.tsx
- Commit: d241adc

## Known Stubs

None — todos os campos são derivados de queries reais ao banco de dados.

## Self-Check: PASSED

- [x] backend/handlers/dashboard.go existe
- [x] frontend/src/pages/DashboardResumo.tsx existe
- [x] Backend compila: go build ./... limpo
- [x] TypeScript: sem erros em arquivos criados/modificados por este plano
- [x] Rota /api/dashboard/resumo em main.go: 1 ocorrencia
- [x] Rota /painel/resumo-fiscal em App.tsx + navigation.ts: 2 ocorrencias
