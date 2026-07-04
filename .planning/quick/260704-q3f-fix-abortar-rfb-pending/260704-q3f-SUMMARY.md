---
phase: quick-260704-q3f
plan: 01
subsystem: backend
tags: [rfb, abort, bugfix, pending]
status: complete

provides:
  - "Abortar manual cobre status 'pending' (antes 404/no-op)"
  - "Auto-abort de 5h do scheduler cobre 'pending' — órfãos se resolvem sozinhos"
  - "Migração 114 limpa os pending já travados em produção no deploy"

key-files:
  created:
    - backend/migrations/114_abort_stuck_pending_rfb_requests.sql
  modified:
    - backend/handlers/rfb_apuracao.go
    - backend/services/rfb_scheduler.go

key-decisions:
  - "Fix mínimo no backend; frontend inalterado (derivação do badge 'Travado' já incluía pending — o gap era só nas cláusulas SQL de abort)"
  - "Redesign do fluxo Re-solicitar→pending órfão registrado como fora de escopo"

requirements-completed: [FIX-ABORTAR-RFB-01]

duration: ~15min
completed: 2026-07-04
---

# Quick Task 260704-q3f: Fix Abortar RFB "Travado"

## Verification

- `go build/vet/test` ✅
- Funcional (local): POST /api/rfb/apuracao/abort em request `pending` → HTTP 200,
  linha vira `error/MANUAL_ABORT`. Antes do fix: HTTP 404, sem efeito.
- Migração 114: rodou no startup local e abortou pending de 10h (`error/TIMEOUT`) ✅

## Pós-deploy (produção)

- A migração 114 deve abortar o request travado de 30/06 (CNPJ 10.230.480)
  automaticamente; ele passará a "Erro", podendo ser excluído ou re-solicitado.
