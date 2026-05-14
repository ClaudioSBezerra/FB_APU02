---
slug: dashboard-creditos-consistencia
status: complete
completed: 2026-05-14
commit: ccc5924
---

# Resumo — Dashboard Consistência de Créditos

## O que foi feito

**TASK-1 — `dashboard.go`**
- Query de `nfe_entradas` expandida com 2 colunas CASE WHEN para calcular créditos líquidos (excluindo Simples Nacional via subquery em `forn_simples` e entradas com `v_ibs=0`)
- Mesma lógica aplicada a `cte_entradas` usando `emit_cnpj`
- 4 campos novos no response: `creditos_ibs_liquidos`, `creditos_cbs_liquidos`, `creditos_ibs_em_risco`, `creditos_cbs_em_risco`
- Campos originais `total_creditos_ibs/cbs` mantidos para backward compatibility

**TASK-2 — `DashboardResumo.tsx`**
- Card "Total de Créditos" renomeado para "Créditos Líquidos" — exibe apenas créditos aproveitáveis
- Alerta âmbar "⚠ Em risco: R$ X" aparece quando há créditos excluídos
- Tabela "Resumo de Créditos e Débitos" ganhou linha "Em Risco" em âmbar

**TASK-3 — `creditos_perdidos.go`**
- Seção Simples Nacional substituída: `mv_operacoes_simples` (alíquotas estimadas) → join direto em `forn_simples` com valores reais de `v_ibs`/`v_cbs`
- Adicionada query de CT-e de transportadoras Simples Nacional (antes inexistente)
- Scan agora lê `ibs_perdido, cbs_perdido` direto do banco; sem mais multiplicação por alíquota estimada

## Sem migrations

Todas as tabelas e colunas já existiam.
