---
quick_id: 260516-da8
task: Abortar solicitações RFB travadas + auto-abort scheduler (> 5 horas)
date: 2026-05-16
status: completed
commits:
  - e7193c7  feat(260516-da8-01): migration abort stuck rfb requests
  - e8fd335  feat(260516-da8-02): scheduler auto-abort rfb requests > 5h
  - c6e2ec0  feat(260516-da8-03): handlers abort + resolicitar + has_raw_json
  - 953e169  feat(260516-da8-04): frontend abort/resolicitar buttons + stuck badge
---

# Summary: Abortar Solicitações RFB Travadas

## O que foi construído

### Task 1 — Migration 112
`backend/migrations/112_abort_stuck_rfb_requests.sql` — Aborta imediatamente (deploy one-shot, idempotente) todas as requests RFB presas nos estados `requested`, `webhook_received`, `downloading`, `reprocessing` com `updated_at` anterior a 5 horas, setando `status='error'`, `error_code='TIMEOUT'`.

### Task 2 — Scheduler auto-abort
`backend/services/rfb_scheduler.go` — Adicionada função `AbortStuckRFBRequests(db *sql.DB)` que executa o mesmo UPDATE da migration. Chamada em goroutine separada com ticker de 5 minutos dentro de `StartRFBScheduler`, logo após o banco estar pronto.

### Task 3 — Handlers backend
`backend/handlers/rfb_apuracao.go`:
- Campo `HasRawJSON bool json:"has_raw_json"` adicionado ao struct `RFBRequest`
- SELECT em `StatusApuracaoHandler` inclui `(r.raw_json IS NOT NULL) AS has_raw_json`; `rows.Scan` atualizado correspondentemente
- `AbortRequestHandler` — aborta manualmente uma request em estado intermediário (`MANUAL_ABORT`), verifica `company_id` (anti cross-tenant)
- `RessolicitarHandler` — reseta para `pending` apenas requests em `error` com `raw_json IS NULL`, permitindo reenvio à API RFB

### Task 4 — Rotas main.go
`backend/main.go` — Duas novas rotas registradas **antes** do wildcard `/api/rfb/apuracao/`:
- `POST /api/rfb/apuracao/abort` → `AbortRequestHandler`
- `POST /api/rfb/apuracao/resolicitar` → `RessolicitarHandler`

### Task 5 — Frontend
`frontend/src/pages/RFBApuracao.tsx`:
- `has_raw_json: boolean` adicionado à interface `RFBRequest`
- Status `stuck` adicionado ao `statusConfig` (laranja)
- Estados `aborting` e `resoliciting` (string | null) para feedback de loading por request
- `handleAbort` e `handleResolicitar` — funções async com feedback via `setMessage` e `fetchRequests()`
- Lógica `isStuck` / `displayStatus` calculada no map: requests pendentes com `updated_at` > 5h exibem badge "Travado" laranja
- Botão "Abortar" (laranja) visível apenas para requests `isStuck`
- Botão "Re-solicitar" (azul) visível para `error` sem `has_raw_json`
- Botão "Reprocessar" visível para `error` com `has_raw_json` (antes aparecia para todos os erros)
- Ícone `XCircle` adicionado aos imports de `lucide-react`

## Verificações
- `go build ./...` — passou sem erros
- `npx tsc --noEmit` — passou sem erros

## Segurança
- Ambos handlers verificam `company_id` via `GetEffectiveCompanyID` — sem vazamento cross-tenant
- `AbortRequestHandler` só aceita estados intermediários — não destrói requests `completed`
- `RessolicitarHandler` só aceita `error` + `raw_json IS NULL` — não reseta requests com dados baixados
