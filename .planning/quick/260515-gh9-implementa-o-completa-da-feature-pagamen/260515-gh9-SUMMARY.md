---
phase: 260515-gh9
plan: "01"
subsystem: pagamentos-fornecedores
tags: [csv-import, pagamentos, multi-tenant, migrations]
dependency_graph:
  requires: [companies, users]
  provides: [pagamentos_fornecedores, pagamentos_imports, /api/pagamentos-fornecedores]
  affects: [importacoes-nav, App.tsx routes]
tech_stack:
  added: []
  patterns: [handler-factory, csv-upload, on-conflict-do-nothing, pagination]
key_files:
  created:
    - backend/migrations/110_pagamentos_fornecedores.sql
    - backend/migrations/111_pagamentos_imports.sql
    - backend/handlers/pagamentos_fornecedores.go
    - frontend/src/pages/ImportarPagamentosFornecedores.tsx
  modified:
    - backend/main.go
    - frontend/src/App.tsx
    - frontend/src/lib/navigation.ts
decisions:
  - "withAuth role '' used (empty string) for all 5 pagamentos routes — consistent with NF-e/CT-e endpoints pattern in main.go"
  - "BOM character \\xef\\xbb\\xbf stripped via hex escape in Go string (literal BOM in source caused go build error)"
  - "Trailing-slash route /imports/ registered before /imports to ensure http.ServeMux prefix matching for DELETE {id}"
  - "mes_ano computed server-side from data_pagamento to avoid timezone/format mismatch from client"
  - "Verification script false-positive: grep 'disabled.*pagamentos-fornecedores' also matches rfb module entry; importacoes tab correctly has no disabled flag"
metrics:
  duration: "~25 min"
  completed: "2026-05-15"
  tasks_completed: 2
  files_created: 4
  files_modified: 3
---

# Phase 260515-gh9 Plan 01: Pagamentos a Fornecedores (CSV Import) Summary

**One-liner:** End-to-end feature de pagamentos a fornecedores com migrations PostgreSQL, 5 endpoints REST Go, importador CSV com validação e histórico de undo, e página React completa no menu Importações.

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Migrations 110/111 + handler Go + rotas | 5526209 | migrations/110, migrations/111, handlers/pagamentos_fornecedores.go, main.go |
| 2 | Página React + rota + tab nav | 39c138f | pages/ImportarPagamentosFornecedores.tsx, App.tsx, navigation.ts |

## What Was Built

### Backend

**Migrations:**
- `110_pagamentos_fornecedores.sql`: tabela com constraint `uq_pag_forn (company_id, chave_doc, data_pagamento, valor_pagamento)` e 4 índices (company_mes, chave, cnpj, import)
- `111_pagamentos_imports.sql`: histórico de batches com `gen_random_uuid()` como PK

**Handler `pagamentos_fornecedores.go` (5 funções):**
- `PagamentosFornecedoresImportHandler` — POST multipart/form-data, field "csv", max 10 MB. Valida header (5 campos obrigatórios), strip BOM. Por linha: valida chave_doc (44 dígitos para NFE/CTE, qualquer string para NFSE), forn_cnpj (14 dígitos após strip não-numéricos), data_pagamento (DD/MM/YYYY ou YYYY-MM-DD), valor_pagamento (float > 0). Usa transação com ON CONFLICT ON CONSTRAINT uq_pag_forn DO NOTHING para contar duplicados sem erro.
- `PagamentosFornecedoresListHandler` — GET com filtros opcionais mes_ano, forn_cnpj, chave_doc (ILIKE). Paginação page/page_size (default 50, max 200). Retorna `{items, page, page_size, total}`.
- `PagamentosImportsListHandler` — GET histórico de batches, LIMIT 100, ORDER BY importado_em DESC.
- `PagamentosImportsDeleteHandler` — DELETE /imports/{uuid}. Valida UUID via regex. Transação: apaga pagamentos do batch e depois o registro de batch.
- `PagamentosTemplateHandler` — GET retorna CSV com 8 colunas e 3 linhas de exemplo (1 NFE com 2 parcelas, 1 NFSE).

**Rotas em main.go** (role `""`, consistente com NF-e/CT-e):
```
/api/pagamentos-fornecedores/import   POST
/api/pagamentos-fornecedores/imports/ DELETE (trailing slash para captura de {id})
/api/pagamentos-fornecedores/imports  GET
/api/pagamentos-fornecedores/template GET
/api/pagamentos-fornecedores          GET
```

### Frontend

**`ImportarPagamentosFornecedores.tsx`** (~310 linhas):
- Card "Filtros" com inputs mes_ano (type=month), forn_cnpj, chave_doc + botões Aplicar/Limpar
- Card "Importar CSV" com botão template download (blob → `<a>` programático), seleção de arquivo, upload com resultado (importados verde / duplicados amarelo / erros vermelho com lista expandível)
- Card "Pagamentos importados" com tabela (data, tipo, chave truncada, CNPJ formatado XX.XXX.XXX/XXXX-XX, nome, valor BRL, num doc) + paginação anterior/próximo
- Card "Histórico de Imports" (colapsável com ChevronDown/Up) com tabela de batches e botão "Desfazer" (DELETE + confirm + refetch)
- Auth: nenhum header manual — interceptor global de AuthContext injeta automaticamente
- Estado: `pagRefreshKey` e `histRefreshKey` forçam re-fetch após import/undo sem TanStack Query

**`App.tsx`**: import + `<Route path="/importacoes/pagamentos-fornecedores" element={<ImportarPagamentosFornecedores />} />`

**`navigation.ts`**: tab `{ label: 'Pag. Fornecedores (CSV)', path: '/importacoes/pagamentos-fornecedores' }` inserida após CT-e Entradas, sem `disabled`.

## Decisions Made

1. **Role `""` em withAuth** — Todos os endpoints NF-e/CT-e existentes usam `withAuth(..., "")`. Mantida consistência; role `"user"` não é usado em nenhum endpoint de importação.

2. **BOM handling em Go** — Literal BOM (`﻿`) no meio de string literal Go causa erro de compilação `invalid BOM in the middle of the file`. Resolvido com `strings.TrimPrefix(h, "\xef\xbb\xbf")` (escape hexadecimal).

3. **Trailing slash para DELETE** — `http.ServeMux` faz prefix match: `/api/pagamentos-fornecedores/imports/` (com trailing slash) é registrada antes de `/imports` para garantir que DELETE para `/imports/{uuid}` seja roteado ao handler correto.

4. **mes_ano computado no backend** — Calculado via `dataPag.Format("2006-01")` a partir da data_pagamento parseada, garantindo consistência independente do formato do CSV do cliente.

5. **RefreshKey pattern em vez de TanStack Query** — A página não usa TanStack Query (padrão da codebase para páginas sem hooks customizados). Usa `pagRefreshKey`/`histRefreshKey` como dependência de `useEffect` para forçar re-fetch pós-import e pós-undo.

## Gancho para Feature Futura (Malha Fina / IBS/CBS)

A coluna `chave_doc` em `pagamentos_fornecedores` referencia diretamente a chave de acesso de NF-e/CT-e. No futuro, o cruzamento com `rfb_creditos.situacao_credito` poderá ser feito via:

```sql
SELECT p.chave_doc, p.valor_pagamento, p.data_pagamento,
       rc.situacao_credito, rc.valor_credito
FROM pagamentos_fornecedores p
JOIN rfb_creditos rc ON rc.chave_doc = p.chave_doc AND rc.company_id = p.company_id
WHERE p.company_id = $1
```

O índice `idx_pag_forn_chave (company_id, chave_doc)` suporta este JOIN sem full scan.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] BOM literal em string Go causava erro de compilação**
- **Found during:** Task 1 — `go build ./...`
- **Issue:** `handlers/pagamentos_fornecedores.go:79:70: invalid BOM in the middle of the file` — o caractere BOM (U+FEFF) estava embutido como literal na string `"﻿"` para strip de BOM em CSV headers.
- **Fix:** Substituído por escape hexadecimal `"\xef\xbb\xbf"` que é equivalente mas válido em Go source.
- **Files modified:** `backend/handlers/pagamentos_fornecedores.go`
- **Commit:** 5526209

## Known Stubs

None — todos os endpoints estão funcionais com dados reais.

## Threat Flags

Nenhuma nova superfície além do planejado. Todos os endpoints requerem autenticação via `withAuth` e isolamento multi-tenant via `GetEffectiveCompanyID`.

## Self-Check: PASSED

| Item | Status |
|------|--------|
| backend/migrations/110_pagamentos_fornecedores.sql | FOUND |
| backend/migrations/111_pagamentos_imports.sql | FOUND |
| backend/handlers/pagamentos_fornecedores.go | FOUND |
| frontend/src/pages/ImportarPagamentosFornecedores.tsx | FOUND |
| commit 5526209 (Task 1) | FOUND |
| commit 39c138f (Task 2) | FOUND |
