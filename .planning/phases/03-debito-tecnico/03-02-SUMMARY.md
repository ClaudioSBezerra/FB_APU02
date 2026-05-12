---
plan: 03-02
phase: 03-debito-tecnico
status: complete
date: 2026-05-12
subsystem: backend/handlers
tags: [debt, reliability, error-handling, db-exec]
decisions:
  - "Operacoes de cleanup/heartbeat usam log.Printf (best-effort): falha nao deve abortar resposta HTTP"
  - "Gravacao de API key usa sanitizeDBErr + return: resposta so deve ser enviada se chave foi persistida"
  - "Atualizacoes de agendamento CGIBS usam log.Printf: estado inconsistente e preferivel a HTTP 500 para operacoes de configuracao"
  - "INSERTs de run_items usam log.Printf por item: falha parcial nao deve abortar toda a importacao"
metrics:
  duration: "~15min"
  completed: 2026-05-12
  tasks_completed: 1
  files_modified: 5
key-files:
  modified:
    - backend/handlers/erp_bridge.go
    - backend/handlers/admin.go
    - backend/handlers/cgibs_apuracao.go
    - backend/handlers/cgibs_credentials.go
    - backend/handlers/erp_bridge_batch.go
---

# Phase 03 Plan 02: Correcao de db.Exec sem verificacao de erro (DEBT-02)

Corrigidas todas as chamadas `db.Exec` e `tx.Exec` que ignoravam silenciosamente o erro retornado em 5 arquivos de handlers.

## Summary

| Arquivo | Chamadas corrigidas | Estrategia |
|---------|--------------------|-----------| 
| erp_bridge.go | 12 | mixed (log.Printf best-effort + sanitizeDBErr para API key) |
| admin.go | 5 | log.Printf (goroutines + auto-provision) |
| cgibs_apuracao.go | 2 | log.Printf (operacoes de cleanup) |
| cgibs_credentials.go | 1 | log.Printf (UPDATE agendamento) |
| erp_bridge_batch.go | 1 | log.Printf (upsert parceiro helper) |

**Total:** 21 chamadas corrigidas

### Detalhes por arquivo

**erp_bridge.go:**
- Linhas 178-199: 6 UPDATEs de campos individuais de config → `log.Printf` (best-effort per-field)
- Linha 279: INSERT/UPDATE `ultimo_run_em` → `log.Printf` (best-effort)
- Linha 289: DELETE runs finalizados → `log.Printf` (best-effort cleanup)
- Linha 352: INSERT `run_items` (loop) → `log.Printf` por item (falha parcial nao aborta importacao)
- Linha 518: INSERT `erp_bridge_servidores` (loop) → `log.Printf` por servidor
- Linha 558: INSERT API key → `sanitizeDBErr + return` (critico: resposta so pode ser enviada se chave foi persistida)
- Linhas 766-771: UPDATE `daemon_last_seen` e cleanup de runs presos → `log.Printf` (heartbeat best-effort)
- Linhas 377, 382, 390: ja tinham `_, execErr =` — nao alteradas

**admin.go:**
- Linhas 185, 191, 197: fallback `db.Exec("REFRESH MATERIALIZED VIEW ...")` dentro de goroutines → `log.Printf` (sem acesso a `w`)
- Linhas 440, 442: INSERT `user_environments` e `companies` no auto-provision de usuario trial → `log.Printf` (sem transacao, rollback impossivel)

**cgibs_apuracao.go:**
- Linha 159: DELETE de registros com status='error' → `log.Printf` (cleanup best-effort)
- Linha 182: DELETE de request por ID → `log.Printf` (cleanup best-effort)
- `log` adicionado ao bloco de imports

**cgibs_credentials.go:**
- Linha 192: UPDATE `cgibs_credentials` (agendamento_ativo + horario) → `log.Printf` (resposta ja montada localmente, log suficiente)
- `log` adicionado ao bloco de imports

**erp_bridge_batch.go:**
- Linha 366: INSERT/UPDATE na tabela `parceiros` dentro de `upsertParceiro()` → `log.Printf` (helper sem `http.ResponseWriter`)

## Verification

- `go build ./...`: passou sem erros
- Chamadas `db.Exec`/`tx.Exec` sem verificacao de erro em handlers: 0 (exceto `auth.go:749` que e intencional)

## Self-Check: PASSED

- Commit `d934528` existe: FOUND
- Arquivos modificados presentes no commit: FOUND (5 files, 79 insertions, 31 deletions)
- Build passou: PASSED
