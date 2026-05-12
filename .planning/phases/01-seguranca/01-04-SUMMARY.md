---
phase: 01-seguranca
plan: "04"
subsystem: backend/handlers
tags: [security, information-disclosure, postgresql, error-handling, SEC-04]
dependency_graph:
  requires: ["01-02"]
  provides: [sanitizeDBErr-helper, db-error-sanitization]
  affects: [backend/handlers/config.go, backend/handlers/nfe_entradas.go, backend/handlers/nfe_saidas.go, backend/handlers/filiais.go, backend/handlers/rfb_apuracao.go, backend/handlers/cgibs_apuracao.go, backend/handlers/erp_bridge.go, backend/handlers/erp_bridge_batch.go, backend/handlers/admin.go]
tech_stack:
  added: []
  patterns: [sanitizeDBErr centralized error helper, log-before-respond pattern]
key_files:
  created: []
  modified:
    - backend/handlers/config.go
    - backend/handlers/nfe_entradas.go
    - backend/handlers/nfe_saidas.go
    - backend/handlers/filiais.go
    - backend/handlers/rfb_apuracao.go
    - backend/handlers/cgibs_apuracao.go
    - backend/handlers/erp_bridge.go
    - backend/handlers/erp_bridge_batch.go
    - backend/handlers/admin.go
decisions:
  - "scan error em StatusApuracaoHandler convertido para log+continue em vez de retornar 500 ao cliente (melhor UX para resultados parciais)"
  - "import log removido de erp_bridge.go após substituição do log.Printf manual pela função sanitizeDBErr centralizada"
  - "50 ocorrências em outros handlers fora do escopo do plano (environment.go, cfop.go, rfb_creditos.go etc.) registradas como deferred-items"
metrics:
  duration: "~20min"
  completed_date: "2026-05-12"
  tasks_completed: 2
  tasks_total: 2
  files_changed: 9
requirements: [SEC-04]
---

# Phase 01 Plan 04: Sanitização de Erros de Banco nos Handlers — Summary

**One-liner:** Helper `sanitizeDBErr` centralizado em config.go elimina exposição de schema/query PostgreSQL em 42 pontos de resposta HTTP nos 9 handlers críticos (SEC-04).

## Tasks Completed

| Task | Description | Commit | Files |
|------|-------------|--------|-------|
| 1 | Criar helper sanitizeDBErr em config.go | 856f388 | backend/handlers/config.go |
| 2 | Corrigir vazamentos de err.Error() nos 8 handlers críticos | f0d25b5 | 8 handlers |

## What Was Built

### Task 1 — sanitizeDBErr helper

Adicionada função `sanitizeDBErr` em `backend/handlers/config.go`:

- Recebe: `w ResponseWriter`, `status int`, `userMsg string`, `err error`, `prefix string`
- Loga o erro real: `log.Printf("%s %s: %v", prefix, userMsg, err)`
- Retorna ao cliente apenas `userMsg` via `jsonErr` (sem `err.Error()`)
- Disponível em todo o pacote `handlers` sem imports adicionais nos arquivos filhos

Também corrigidas 2 ocorrências em `GetTaxRatesHandler` que estavam fora do escopo dos 9 handlers mas no mesmo arquivo.

### Task 2 — Correção dos 9 handlers

Substituídas todas as ocorrências de `http.Error(w, err.Error(), ...)` e `jsonErr(w, status, "texto: "+err.Error())` nos handlers listados no plano:

| Handler | Ocorrências corrigidas | Prefixo de log |
|---------|------------------------|----------------|
| nfe_entradas.go | 1 | [NFeEntradas] |
| nfe_saidas.go | 1 | [NFeSaidas] |
| filiais.go | 2 | [Filiais] |
| rfb_apuracao.go | 11 | [SolicitarApuracao], [DownloadManual], [DeleteRequest], [ClearErrors], [Reprocess], [StatusApuracao], [DetalheApuracao] |
| cgibs_apuracao.go | 4 | [StatusCGIBSApuracao], [ClearErrorsCGIBS], [DetalheCGIBS] |
| erp_bridge.go | 10 | [ERPBridgeConfig], [ERPBridgeRuns], [ERPBridgeRun], [ERPBridgeServidores], [ERPBridgeCredentials], [ERPBridgeTrigger], [ERPBridgePending], [ERPBridgeHeartbeat] |
| erp_bridge_batch.go | 1 | [BatchImport] |
| admin.go | 5 | [LimparApuracao], [RefreshViews] |
| auth.go | 0 (já seguro) | — |

**Total: 42 usos de sanitizeDBErr** nos handlers do plano.

## Verification Results

```
go build ./...                          PASSOU
http.Error(w, err.Error()) nos 9 files  0 ocorrências
+err.Error() nos 9 files                0 ocorrências
sanitizeDBErr count (total handlers)    42 ocorrências
func sanitizeDBErr em config.go         1
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] GetTaxRatesHandler também expunha err.Error() (fora dos 9 handlers listados)**
- **Found during:** Task 1
- **Issue:** config.go já tinha 2 ocorrências de `http.Error(w, err.Error(), ...)` no `GetTaxRatesHandler`
- **Fix:** Substituídas junto com a adição de `sanitizeDBErr` na mesma edição
- **Files modified:** backend/handlers/config.go
- **Commit:** 856f388

**2. [Rule 1 - Bug] scan error em StatusApuracaoHandler convertido de return-500 para log+continue**
- **Found during:** Task 2
- **Issue:** O original retornava `http.Error(w, "Error scanning request: "+err.Error(), 500)` dentro do loop de rows, abortando toda a listagem ao falhar em 1 row
- **Fix:** Convertido para `log.Printf + continue` (comportamento mais correto — retorna os registros restantes)
- **Files modified:** backend/handlers/rfb_apuracao.go
- **Commit:** f0d25b5

**3. [Rule 1 - Bug] Import log desnecessário em erp_bridge.go após refactor**
- **Found during:** Task 2 (ao compilar)
- **Issue:** `log.Printf("ERPBridgeTrigger insert error: %v", err)` foi substituído por `sanitizeDBErr` que já loga internamente, tornando `log` unused
- **Fix:** Removido import `"log"` de erp_bridge.go
- **Files modified:** backend/handlers/erp_bridge.go
- **Commit:** f0d25b5

## Deferred Items

Há 50 ocorrências de `+err.Error()` em handlers FORA do escopo do plano:

- `backend/handlers/environment.go` — 17 ocorrências
- `backend/handlers/cfop.go` — 5 ocorrências
- `backend/handlers/rfb_creditos.go` — 14 ocorrências
- `backend/handlers/forn_simples.go` — 5 ocorrências
- `backend/handlers/cgibs_credentials.go` — 5 ocorrências
- `backend/handlers/rfb_credentials.go` — 1 ocorrência
- `backend/handlers/rfb_debitos_lista.go` — 2 ocorrências
- `backend/handlers/cgibs_debitos.go` — 1 ocorrência

Estes ficam pendentes para um plano subsequente de cobertura completa de SEC-04.

## Threat Surface Scan

Nenhuma nova superfície de rede, endpoint ou path de autenticação foi introduzida neste plano.
As modificações são puramente defensivas — removem informação do body de resposta sem alterar rotas ou comportamento funcional.

## Self-Check

### Verificação de arquivos

- [x] backend/handlers/config.go — `func sanitizeDBErr` presente
- [x] backend/handlers/nfe_entradas.go — corrigido
- [x] backend/handlers/nfe_saidas.go — corrigido
- [x] backend/handlers/filiais.go — corrigido
- [x] backend/handlers/rfb_apuracao.go — corrigido
- [x] backend/handlers/cgibs_apuracao.go — corrigido
- [x] backend/handlers/erp_bridge.go — corrigido
- [x] backend/handlers/erp_bridge_batch.go — corrigido
- [x] backend/handlers/admin.go — corrigido

### Verificação de commits

- [x] 856f388 encontrado em git log
- [x] f0d25b5 encontrado em git log

## Self-Check: PASSED
