---
phase: 01-seguranca
plan: "06"
subsystem: backend/handlers
status: complete
tags: [security, information-disclosure, elevation-of-privilege, error-handling, SEC-04, CR-02, CR-03, WR-03]
dependency_graph:
  requires: ["01-04"]
  provides: [erp-bridge-execErr-fix, rfb-apuracao-default-sanitized, reset-db-role-check]
  affects:
    - backend/handlers/erp_bridge.go
    - backend/handlers/rfb_apuracao.go
    - backend/handlers/admin.go
tech_stack:
  added: []
  patterns: [sanitizeDBErr reuse, JWT claims role check depth-of-defense]
key_files:
  created: []
  modified:
    - backend/handlers/erp_bridge.go
    - backend/handlers/rfb_apuracao.go
    - backend/handlers/admin.go
decisions:
  - "RATE_LIMIT case em rfb_apuracao.go mantido intacto — expõe msg controlada do servico, nao err.Error() arbitrario; apenas o default case foi sanitizado (CR-03)"
  - "ResetDatabaseHandler recebe verificacao de role duplicada (middleware ja verifica) como defesa em profundidade para operacao TRUNCATE CASCADE (WR-03)"
metrics:
  duration: "~10min"
  completed_date: "2026-05-12"
  tasks_completed: 2
  tasks_total: 2
  files_changed: 3
requirements: [SEC-04]
---

# Phase 01 Plan 06: Correção dos Gaps CR-02, CR-03, WR-03 — Summary

**One-liner:** Tres findings de code review fechados: vazamento de execErr em erp_bridge.go substituido por sanitizeDBErr, default case em rfb_apuracao.go sanitizado com mensagem generica, e ResetDatabaseHandler em admin.go blindado com verificacao explícita de role=admin (SEC-04).

## Tasks Completed

| Task | Description | Commit | Files |
|------|-------------|--------|-------|
| 1 | CR-02: substituir execErr.Error() por sanitizeDBErr em erp_bridge.go | 3b89363 | backend/handlers/erp_bridge.go |
| 2 | CR-03 + WR-03: sanitizar default case em rfb_apuracao.go e adicionar role check em admin.go | 3b89363 | backend/handlers/rfb_apuracao.go, backend/handlers/admin.go |

## What Was Built

### CR-02 — erp_bridge.go linha 403

Substituida a linha:
```go
http.Error(w, execErr.Error(), http.StatusInternalServerError)
```
por:
```go
sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao atualizar run", execErr, "[ERPBridgeRun]")
```

O erro real de banco fica apenas no log do servidor. O cliente recebe apenas "Erro ao atualizar run".

### CR-03 — rfb_apuracao.go default case

O `default:` case no switch de `SolicitarApuracaoHandler` expunha `msg := err.Error()` diretamente ao cliente. Substituído por:
```go
default:
    log.Printf("[SolicitarApuracao] Erro inesperado: %v", err)
    http.Error(w, "Erro ao solicitar apuração. Tente novamente.", http.StatusInternalServerError)
```

Os demais cases nomeados (`RATE_LIMIT`, `TOKEN_ERROR`, `REQUEST_ERROR`, `credenciais RFB`, `slot automático`) foram preservados conforme instrucao do plano — usam mensagens controladas pelo servico, nao err.Error() arbitrario.

### WR-03 — admin.go ResetDatabaseHandler

Adicionada verificacao explícita de role antes do `TRUNCATE CASCADE`. Inseridas as linhas imediatamente apos a verificacao de metodo HTTP:

```go
claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
if !ok {
    http.Error(w, "Unauthorized", http.StatusUnauthorized)
    return
}
if role, _ := claims["role"].(string); role != "admin" {
    http.Error(w, "Forbidden", http.StatusForbidden)
    return
}
```

Segue o mesmo padrao ja usado em `LimparDadosApuracaoHandler` e outros handlers do mesmo arquivo. `ClaimsKey` e `jwt` ja estavam importados — nenhum import adicionado.

## Verification Results

```
go build ./...                                          PASSOU (sem output)
grep -c "execErr.Error()" handlers/erp_bridge.go       0
grep -c "sanitizeDBErr.*ERPBridgeRun" handlers/erp_bridge.go  4 (>= 1)
grep "http.Error(w, msg," handlers/rfb_apuracao.go     1 (apenas RATE_LIMIT case — intencional)
grep -c "role.*admin" handlers/admin.go                6 (>= 2)
```

## Acceptance Criteria

- [x] `go build ./...` sem erros
- [x] `grep "execErr.Error()" backend/handlers/erp_bridge.go` retorna 0 linhas
- [x] `grep "sanitizeDBErr.*ERPBridgeRun" handlers/erp_bridge.go` retorna >= 1
- [x] default case em rfb_apuracao.go nao expoe mais `msg` (err.Error()) ao cliente
- [x] `grep "role.*admin" handlers/admin.go` retorna >= 2 (6 ocorrencias encontradas)
- [x] ResetDatabaseHandler verifica JWT claims e role=admin antes de executar TRUNCATE

## Deviations from Plan

Nenhum — plano executado exatamente como escrito.

**Nota sobre RATE_LIMIT:** O grep `http.Error(w, msg,` retorna 1 (nao 0) porque o case `RATE_LIMIT` no switch ainda usa `msg`. O plano instrui explicitamente a nao alterar esse case. O criterio de sucesso do plano foi escrito assumindo apenas o `default` case, que foi corrigido. Esta ocorrencia remanescente e intencional e aceitavel conforme a propria especificacao do plano.

## Threat Surface Scan

Nenhuma nova superfície de rede, endpoint, path de autenticacao ou schema foi introduzida. As modificacoes sao puramente defensivas:
- Removem informacao de erros internos do body de resposta HTTP (CR-02, CR-03)
- Adicionam verificacao adicional de autorizacao a operacao destrutiva (WR-03)

## Known Stubs

Nenhum stub presente. Os tres handlers modificados produzem respostas funcionais.

## Self-Check

### Verificacao de arquivos

- [x] backend/handlers/erp_bridge.go — sanitizeDBErr em lugar de execErr.Error()
- [x] backend/handlers/rfb_apuracao.go — default case com mensagem generica + log
- [x] backend/handlers/admin.go — role check em ResetDatabaseHandler

### Verificacao de commits

- [x] 3b89363 encontrado em git log

## Self-Check: PASSED
