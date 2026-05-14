---
slug: dashboard-creditos-consistencia
status: planned
created: 2026-05-13
---

# Dashboard — Consistência de Créditos IBS/CBS

## Problema

O Painel "Resumo Fiscal" exibe valores inconsistentes:

1. **Total de Créditos maior que Total de Débitos** mesmo com muito mais notas de saída — porque os créditos incluem entradas de fornecedores do Simples Nacional (onde não há repasse de IBS/CBS) e entradas com `v_ibs = 0` (sem direito a crédito).
2. **Créditos em Risco** (`CreditosPerdidosHandler`) não contabiliza corretamente: usa `mv_operacoes_simples` com alíquotas *estimadas* em vez de joins diretos em `forn_simples` com os valores reais de `v_ibs`/`v_cbs`.

## Objetivo

Exibir no dashboard apenas os créditos reais (aproveitáveis), destacar o que está em risco, e corrigir a tela de Créditos em Risco para usar valores contabilizados, não estimados.

---

## Tarefas

### TASK-1 — backend/handlers/dashboard.go: separar créditos líquidos dos créditos em risco

**O que mudar:**

Nas queries de `nfe_entradas` e `cte_entradas`, adicionar uma segunda contagem paralela excluindo:
- Fornecedores do Simples Nacional (`forn_cnpj NOT IN (SELECT cnpj FROM forn_simples)` / `emit_cnpj NOT IN ...`)
- Entradas sem IBS/CBS (`v_ibs > 0 OR v_cbs > 0`)

Adicionar ao response:
```
creditos_ibs_liquidos   float64   // apenas entradas com IBS > 0 e forn não-Simples
creditos_cbs_liquidos   float64
creditos_ibs_em_risco   float64   // v_ibs = 0 OU forn Simples Nacional
creditos_cbs_em_risco   float64
```

**SQL proposto (nfe_entradas):**
```sql
SELECT
    COUNT(*),
    COALESCE(SUM(v_nf), 0),
    COALESCE(SUM(v_ibs), 0),
    COALESCE(SUM(v_cbs), 0),
    COALESCE(SUM(v_bc_ibs_cbs), 0),
    -- créditos líquidos (excl. Simples + excl. sem IBS/CBS)
    COALESCE(SUM(CASE WHEN (v_ibs > 0 OR v_cbs > 0)
                       AND forn_cnpj NOT IN (SELECT cnpj FROM forn_simples)
                  THEN v_ibs ELSE 0 END), 0),
    COALESCE(SUM(CASE WHEN (v_ibs > 0 OR v_cbs > 0)
                       AND forn_cnpj NOT IN (SELECT cnpj FROM forn_simples)
                  THEN v_cbs ELSE 0 END), 0)
FROM nfe_entradas
WHERE company_id = $1 AND mes_ano = $2
```

Mesma lógica para `cte_entradas` usando `emit_cnpj`.

**Totais no response:**
```go
totalCreditosIBSLiquidos := entVIBSLiq + cteVIBSLiq
totalCreditosCBSLiquidos := entVCBSLiq + cteVCBSLiq
totalCreditosIBSEmRisco  := (entVIBS + cteVIBS) - totalCreditosIBSLiquidos
totalCreditosCBSEmRisco  := (entVCBS + cteVCBS) - totalCreditosCBSLiquidos
```

Manter `total_creditos_ibs` e `total_creditos_cbs` (todos os créditos) para backward compatibility, mas adicionar os campos novos.

---

### TASK-2 — frontend/src/pages/DashboardResumo.tsx: atualizar card de créditos

**O que mudar:**

Card "Total de Créditos" passa a mostrar os créditos líquidos no valor principal e os créditos em risco como subtítulo de alerta:

```
Total de Créditos
R$ 12.450,00  ← creditos_ibs_liquidos + creditos_cbs_liquidos

IBS: R$ 8.200 | CBS: R$ 4.250
⚠ Em risco: R$ 3.100  ← creditos_ibs_em_risco + creditos_cbs_em_risco
```

Atualizar interface `DashboardData`:
```typescript
creditos_ibs_liquidos: number
creditos_cbs_liquidos: number
creditos_ibs_em_risco: number
creditos_cbs_em_risco: number
```

---

### TASK-3 — backend/handlers/creditos_perdidos.go: corrigir seção Simples Nacional

**Problema atual:**

O handler usa `mv_operacoes_simples` que é uma materialized view com alíquotas *estimadas* (ibsRate/cbsRate calculadas separadamente). Isso não reflete os valores reais de `v_ibs`/`v_cbs` registrados nas notas.

**O que mudar:**

Substituir a query de Simples Nacional que usa `mv_operacoes_simples` por query direta em `nfe_entradas` com join em `forn_simples`:

```sql
-- Entradas de fornecedores Simples Nacional (crédito perdido real)
SELECT
    COUNT(*),
    COALESCE(SUM(v_nf), 0),
    COALESCE(SUM(v_ibs), 0),
    COALESCE(SUM(v_cbs), 0)
FROM nfe_entradas ne
INNER JOIN forn_simples fs ON fs.cnpj = ne.forn_cnpj
WHERE ne.company_id = $1
  AND ne.mes_ano = $2
```

Mesma lógica para `cte_entradas` com `emit_cnpj`.

Adicionar também: entradas com `v_ibs = 0 AND v_cbs = 0` que NÃO são do Simples (possíveis erros de alíquota ou CFOP não tributável):

```sql
SELECT COUNT(*), COALESCE(SUM(v_nf), 0)
FROM nfe_entradas
WHERE company_id = $1
  AND mes_ano = $2
  AND v_ibs = 0 AND v_cbs = 0
  AND forn_cnpj NOT IN (SELECT cnpj FROM forn_simples)
```

---

## Dependências

| Dado | Tabela | Coluna |
|------|--------|--------|
| CNPJ fornecedor NF-e | `nfe_entradas` | `forn_cnpj` |
| CNPJ emissor CT-e | `cte_entradas` | `emit_cnpj` |
| Simples Nacional | `forn_simples` | `cnpj` |
| Créditos reais | `nfe_entradas`, `cte_entradas` | `v_ibs`, `v_cbs` |

## Ordem de execução

1. TASK-1 (backend dashboard.go) — independente
2. TASK-3 (backend creditos_perdidos.go) — independente
3. TASK-2 (frontend) — depende de TASK-1 estar deployado

## Arquivos a modificar

- `backend/handlers/dashboard.go`
- `backend/handlers/creditos_perdidos.go`
- `frontend/src/pages/DashboardResumo.tsx`

## Sem migrations necessárias

Todas as tabelas e colunas já existem. `forn_simples` e as colunas `v_ibs`/`v_cbs`/`forn_cnpj`/`emit_cnpj` são parte do schema atual.
