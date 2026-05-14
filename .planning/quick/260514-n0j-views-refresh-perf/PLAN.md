---
quick_id: 260514-n0j
slug: views-refresh-perf
description: Refresh automático de mv_malha_fina_resumo após delete/import e otimizações de performance no dashboard
date: 2026-05-14
---

# Quick Task: Views Refresh + Performance

## Goal
Garantir que `mv_malha_fina_resumo` seja atualizada automaticamente após deletar ou importar dados, e eliminar subconsultas N² no dashboard com LEFT JOIN + covering indexes.

## Must-haves
- [ ] mv_malha_fina_resumo refreshada em goroutine após LimparDadosApuracaoHandler
- [ ] mv_malha_fina_resumo refreshada em goroutine após LimpezaBaseHandler DELETE
- [ ] mv_malha_fina_resumo refreshada em goroutine após ERPBridgeBatchImportHandler
- [ ] dashboard.go: NOT IN (subquery) → LEFT JOIN para nfe_entradas e cte_entradas
- [ ] Migration 105: covering indexes para SUM/COUNT do dashboard + índice em forn_simples.cnpj
- [ ] go build ./... sem erros

## Tasks

### TASK-1: admin.go — refresh após LimparDadosApuracaoHandler
Adicionar goroutine após o upsert de reset_tracker (antes do json.NewEncoder).

### TASK-2: limpeza_base.go — refresh após DELETE
Adicionar goroutine após o loop de DELETEs (antes do json.NewEncoder).

### TASK-3: erp_bridge_batch.go — refresh após import
Adicionar goroutine após log.Printf("[BatchImport]...") e antes de json.NewEncoder(w).Encode(result).

### TASK-4: dashboard.go — NOT IN → LEFT JOIN
- nfe_entradas: LEFT JOIN forn_simples fs ON fs.cnpj = ne.forn_cnpj / WHERE fs.cnpj IS NULL
- cte_entradas: LEFT JOIN forn_simples fs ON fs.cnpj = ce.emit_cnpj / WHERE fs.cnpj IS NULL

### TASK-5: Migration 105_dashboard_covering_indexes.sql
Covering indexes para (company_id, mes_ano) INCLUDE value columns nas 3 tabelas + idx_forn_simples_cnpj.
