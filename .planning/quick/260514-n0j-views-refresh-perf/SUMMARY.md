---
quick_id: 260514-n0j
slug: views-refresh-perf
status: completed
commit: 9c49acf
date_completed: 2026-05-14
---

# Summary: Views Refresh + Performance

## What Was Done

All 5 tasks executed and committed atomically in `9c49acf`.

### TASK-1: admin.go — refresh após LimparDadosApuracaoHandler
Goroutine adicionada em `LimparDadosApuracaoHandler` após o bloco de reset_tracker upsert. Executa `REFRESH MATERIALIZED VIEW CONCURRENTLY mv_malha_fina_resumo` de forma assíncrona — não bloqueia a resposta HTTP.

### TASK-2: limpeza_base.go — refresh após DELETE
Mesma goroutine adicionada no case `http.MethodDelete` de `LimpezaBaseHandler`, após o bloco `if resetTracker { ... }`.

### TASK-3: erp_bridge_batch.go — refresh após import batch
Goroutine inserida após `log.Printf("[BatchImport] ...")` e antes de `json.NewEncoder(w).Encode(result)` em `ERPBridgeBatchImportHandler`.

### TASK-4: dashboard.go — NOT IN → LEFT JOIN
- **nfe_entradas**: `forn_cnpj NOT IN (SELECT cnpj FROM forn_simples)` × 2 → `LEFT JOIN forn_simples fs ON fs.cnpj = ne.forn_cnpj` com `fs.cnpj IS NULL`
- **cte_entradas**: mesmo padrão com alias `ce` e coluna `emit_cnpj`
- Resultado: subconsulta N² eliminada; o planner usa hash join executado uma única vez.

### TASK-5: Migration 105_dashboard_covering_indexes.sql
Novo arquivo `backend/migrations/105_dashboard_covering_indexes.sql` com 4 índices:
- `idx_nfe_entradas_dashboard_cover` — (company_id, mes_ano) INCLUDE (v_nf, v_ibs, v_cbs, v_bc_ibs_cbs, forn_cnpj)
- `idx_cte_entradas_dashboard_cover` — (company_id, mes_ano) INCLUDE (v_prest, v_ibs, v_cbs, v_bc_ibs_cbs, emit_cnpj)
- `idx_nfe_saidas_dashboard_cover` — (company_id, mes_ano) INCLUDE (v_nf, v_ibs, v_cbs, v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun)
- `idx_forn_simples_cnpj` — (cnpj)

## Verification

- `go build ./...` passou sem erros após todas as alterações.
- Todos os must-haves do PLAN.md satisfeitos.

## Files Changed

- `backend/handlers/admin.go`
- `backend/handlers/limpeza_base.go`
- `backend/handlers/erp_bridge_batch.go`
- `backend/handlers/dashboard.go`
- `backend/migrations/105_dashboard_covering_indexes.sql` (novo)
