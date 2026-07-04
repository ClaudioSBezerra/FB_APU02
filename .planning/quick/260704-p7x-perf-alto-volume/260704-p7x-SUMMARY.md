---
phase: quick-260704-p7x
plan: 01
subsystem: backend/db
tags: [performance, postgres, erp-bridge, rfb, materialized-view, autovacuum]
status: complete

provides:
  - "Batch import do ERP Bridge em transação única (prepared statements + savepoints por documento)"
  - "services.RequestMVRefresh — debounce/coalescing de REFRESH MATERIALIZED VIEW CONCURRENTLY"
  - "Migração 113: índices de listagem/estado em rfb_requests + autovacuum agressivo (rfb_requests, rfb_creditos, parceiros)"
  - "services.StartRawJSONJanitor — retenção opt-in de raw_json via RFB_RAW_JSON_RETENTION_DAYS"

key-files:
  created:
    - backend/migrations/113_perf_alto_volume.sql
    - backend/services/mv_refresh.go
    - backend/services/rfb_janitor.go
  modified:
    - backend/handlers/erp_bridge_batch.go
    - backend/handlers/xml_upload.go
    - backend/main.go

key-decisions:
  - "SAVEPOINT por documento no batch preserva a semântica de erro individual (contadores inserted/ignored/errors idênticos); falha no upsert de parceiro tem savepoint próprio para não desfazer o insert do doc"
  - "Debounce da MV com cooldown de 2 min, leading edge imediato e trailing refresh garantido; ações manuais/admin continuam com refresh imediato"
  - "Retenção de raw_json desabilitada por padrão (opt-in via env) — sistema fiscal não apaga dados sem decisão explícita; só afeta status completed"
  - "gofmt não aplicado nos arquivos tocados além das minhas linhas — violações pré-existentes ficam fora do diff"

requirements-completed: [PERF-ALTO-VOLUME-01]

duration: ~35min
completed: 2026-07-04
---

# Quick Task 260704-p7x: Armazenamento e performance para alto volume

## Accomplishments

1. **Batch import ERP Bridge ~10-50x menos fsyncs** (`fd04532`): o loop de
   documentos que fazia 2 autocommits por doc (insert + parceiro) agora roda
   em transação única com prepared statements; savepoints mantêm o
   comportamento de erro por documento e o commit único paga um fsync por lote.
2. **Debounce do refresh da malha fina** (`0e56bfb`): importações em chunks
   não disparam mais dezenas de `REFRESH MATERIALIZED VIEW` enfileirados —
   colapsam em 1 refresh a cada 2 min + refresh final.
3. **Migração 113** (`6b36521`): índice `(company_id, created_at DESC)` para a
   listagem do painel RFB, índice parcial de requests em andamento
   (scheduler/badge travado/futura trava por CNPJ) e autovacuum agressivo nas
   tabelas de alto churn que ficaram fora da migração 068.
4. **Retenção opt-in de raw_json** (`22a98ff`): janitor de 12h limpa o JSON
   bruto de apurações concluídas com mais de `RFB_RAW_JSON_RETENTION_DAYS`
   dias. Desligado por padrão.

## Verification

- `go build ./...` ✅ | `go vet ./...` ✅ (limpo) | `go test ./...` ✅ (handlers, middleware ok)
- `gofmt` — arquivos novos limpos; violações listadas são pré-existentes (verificado por diff)

## Task Commits

1. `6b36521` perf(db): migração 113
2. `0e56bfb` perf(import): debounce do refresh da MV
3. `fd04532` perf(erp-bridge): batch em transação única
4. `22a98ff` feat(rfb): retenção opt-in de raw_json

## Notes / Next Steps

- Para ativar a retenção em produção: definir `RFB_RAW_JSON_RETENTION_DAYS`
  (ex.: 90) no environment do Coolify/compose.
- O índice parcial `idx_rfb_requests_em_andamento` já deixa pronta a base para
  a trava por CNPJ (fix estrutural do "Erro ao enfileirar" — pendente à parte).
- Upload XML segue com insert por arquivo (volume menor); se virar gargalo,
  aplicar o mesmo padrão de transação do batch.
