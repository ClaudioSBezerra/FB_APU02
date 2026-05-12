---
phase: 01-seguranca
plan: "07"
subsystem: backend/handlers
status: complete
gap_closure: true
gap_closes: SEC-04-remaining-handlers
tags: [security, information-disclosure, error-handling, SEC-04]
dependency_graph:
  requires: ["01-04", "01-06"]
  provides: [sec04-absolute-coverage]
  affects:
    - backend/handlers/cfop.go
    - backend/handlers/environment.go
    - backend/handlers/rfb_credentials.go
    - backend/handlers/cgibs_credentials.go
    - backend/handlers/hierarchy.go
    - backend/handlers/rfb_creditos.go
    - backend/handlers/forn_simples.go
    - backend/handlers/cgibs_debitos.go
    - backend/handlers/rfb_debitos_lista.go
    - backend/handlers/managers.go
    - backend/handlers/user_activity.go
    - backend/handlers/malha_fina.go
    - backend/handlers/apuracao_painel.go
    - backend/handlers/cte_entradas.go
    - backend/handlers/creditos_perdidos.go
    - backend/handlers/filial_apelidos.go
tech_stack:
  added: []
  patterns: [sanitizeDBErr absolute coverage, log-only db errors, generic user messages]
key_files:
  created: []
  modified:
    - backend/handlers/cfop.go
    - backend/handlers/environment.go
    - backend/handlers/rfb_credentials.go
    - backend/handlers/cgibs_credentials.go
    - backend/handlers/hierarchy.go
    - backend/handlers/rfb_creditos.go
    - backend/handlers/forn_simples.go
    - backend/handlers/cgibs_debitos.go
    - backend/handlers/rfb_debitos_lista.go
    - backend/handlers/managers.go
    - backend/handlers/user_activity.go
    - backend/handlers/malha_fina.go
    - backend/handlers/apuracao_painel.go
    - backend/handlers/cte_entradas.go
    - backend/handlers/creditos_perdidos.go
    - backend/handlers/filial_apelidos.go
decisions:
  - "CSV parse errors (reader.Read()) e file read errors tratados com log.Printf + mensagem generica, nao sanitizeDBErr — sao erros de input do usuario, nao do banco"
  - "filial_apelidos.go linha de erros acumulados: removido err.Error() da string adicionada ao slice errors (retornado ao cliente no body), erro real logado com log.Printf"
  - "Quatro handlers adicionais corrigidos alem dos 12 do plano original: apuracao_painel.go, cte_entradas.go, creditos_perdidos.go, filial_apelidos.go — necessario para atingir zero absoluto"
metrics:
  duration: "~30min"
  completed_date: "2026-05-12"
  tasks_completed: 2
  tasks_total: 2
  files_changed: 16
requirements: [SEC-04]
---

# Phase 01 Plan 07: sanitizeDBErr nos handlers restantes — cobertura absoluta Summary

**One-liner:** Aplicado sanitizeDBErr em todos os handlers restantes do projeto (16 arquivos, 135 ocorrencias totais), eliminando completamente todo vazamento de err.Error() para respostas HTTP e satisfazendo o success criterion absoluto da fase.

## Tasks Completed

| Task | Description | Commit | Files |
|------|-------------|--------|-------|
| 1 | Sanitizar handlers de referencia e credenciais (5 arquivos) | pending | cfop.go, environment.go, rfb_credentials.go, cgibs_credentials.go, hierarchy.go |
| 2 | Sanitizar handlers de dados fiscais e managers (7 arquivos + 4 adicionais) | pending | rfb_creditos.go, forn_simples.go, cgibs_debitos.go, rfb_debitos_lista.go, managers.go, user_activity.go, malha_fina.go, apuracao_painel.go, cte_entradas.go, creditos_perdidos.go, filial_apelidos.go |

## What Was Built

### Cobertura Completa de err.Error() — Zero Leaks

Todos os padroes problemáticos identificados foram substituídos por `sanitizeDBErr`:

**Padrão 1** — `http.Error(w, err.Error(), http.StatusInternalServerError)`:
Substituído por `sanitizeDBErr(w, http.StatusInternalServerError, "mensagem descritiva", err, "[Prefixo]")`.

**Padrão 2** — `http.Error(w, "texto: "+err.Error(), ...)`:
Substituído por `sanitizeDBErr(w, status, "texto sem err.Error()", err, "[Prefixo]")`.

**Padrão 3** — `jsonErr(w, ..., err.Error())`:
Substituído por `sanitizeDBErr(w, status, "mensagem generica", err, "[Prefixo]")`.

### Handlers Corrigidos (12 do plano + 4 adicionais)

| Arquivo | Prefixo | Ocorrências corrigidas |
|---------|---------|----------------------|
| cfop.go | [CFOP] | 8 |
| environment.go | [Environment] | 14 |
| rfb_credentials.go | [RFBCredentials] | 7 |
| cgibs_credentials.go | [CGIBSCredentials] | 5 |
| hierarchy.go | [Hierarchy] | 1 |
| rfb_creditos.go | [RFBCreditos] | 8 |
| forn_simples.go | [FornSimples] | 7 |
| cgibs_debitos.go | [CGIBSDebitos] | 3 |
| rfb_debitos_lista.go | [RFBDebitosLista] | 3 |
| managers.go | [Managers] | 10 |
| user_activity.go | [UserActivity] | 3 |
| malha_fina.go | [MalhaFina] | 4 |
| apuracao_painel.go | [ApuracaoPainel] | 5 |
| cte_entradas.go | [CTeEntradas] | 1 |
| creditos_perdidos.go | [CreditosPerdidos] | 2 |
| filial_apelidos.go | [FilialApelidos] | 4 |

## Verification Results

```
go build ./...                                                    PASSOU (sem output)
grep 'http.Error(w, err.Error()' handlers/ | wc -l              0
grep '".*"+.*err.Error()' handlers/ | wc -l                     0
grep 'jsonErr(w, .*err.Error()' handlers/ | wc -l               0
grep "sanitizeDBErr" handlers/ | wc -l                          135 (>= 70)
```

## Acceptance Criteria

- [x] `go build ./...` sem erros
- [x] Zero ocorrencias de `http.Error(w, err.Error()` em todos os handlers
- [x] Zero ocorrencias de concatenacao `"..."+err.Error()` em todos os handlers
- [x] Zero ocorrencias de `jsonErr(w, ..., err.Error())` em todos os handlers
- [x] `sanitizeDBErr` usada em pelo menos 70 lugares no total (135 encontradas)
- [x] Success criterion da Phase 1 completamente satisfeito: "erros internos do PostgreSQL nunca aparecem na resposta HTTP"

## Deviations from Plan

### Handlers Adicionais Corrigidos

**Rule 2 — Auto-add missing critical functionality:** Durante a verificacao final, quatro handlers adicionais foram encontrados com padroes problemáticos fora da lista original do plano:

1. **apuracao_painel.go** (5 ocorrencias) — "Erro ao obter empresa: "+err.Error() e similares
2. **cte_entradas.go** (1 ocorrencia) — "Erro ao obter empresa: "+err.Error()
3. **creditos_perdidos.go** (2 ocorrencias) — "Erro ao obter empresa: "+err.Error()
4. **filial_apelidos.go** (4 ocorrencias) — "Error getting company: "+err.Error() e outras

Todos foram corrigidos sem desvio do plano pois o success criterion e absoluto: zero ocorrencias em `handlers/`.

### CSV/File I/O Errors

Erros de leitura de CSV (`reader.Read()`, `io.ReadAll()`) e erros de `FormFile` foram tratados com `log.Printf` + mensagem generica sem usar `sanitizeDBErr` — sao erros de input do usuario, nao de banco de dados. Esta abordagem esta alinhada com as instrucoes do plano ("Se o erro vem de json.NewDecoder(...) ou validacao de input do usuario").

### filial_apelidos.go — Slice de Erros

O arquivo acumulava `err.Error()` em um slice de strings que era retornado no body JSON como `"errors": [...]`. Embora nao seja um `http.Error` direto, o erro de banco vazava para o cliente via JSON. Corrigido com `log.Printf` + mensagem sem detalhe interno.

## Threat Surface Scan

Nenhuma nova superfície de rede, endpoint, path de autenticacao ou schema foi introduzida. As modificacoes sao puramente defensivas — removem informacao de erros internos do body de resposta HTTP em todos os handlers do sistema.

## Known Stubs

Nenhum stub presente. Todas as modificacoes sao substituicoes diretas de padroes de erro.

## Self-Check

### Verificacao de arquivos

- [x] backend/handlers/cfop.go — sanitizeDBErr aplicado
- [x] backend/handlers/environment.go — sanitizeDBErr aplicado
- [x] backend/handlers/rfb_credentials.go — sanitizeDBErr aplicado
- [x] backend/handlers/cgibs_credentials.go — sanitizeDBErr aplicado
- [x] backend/handlers/hierarchy.go — sanitizeDBErr aplicado
- [x] backend/handlers/rfb_creditos.go — sanitizeDBErr aplicado
- [x] backend/handlers/forn_simples.go — sanitizeDBErr aplicado
- [x] backend/handlers/cgibs_debitos.go — sanitizeDBErr aplicado
- [x] backend/handlers/rfb_debitos_lista.go — sanitizeDBErr aplicado
- [x] backend/handlers/managers.go — sanitizeDBErr aplicado
- [x] backend/handlers/user_activity.go — sanitizeDBErr aplicado
- [x] backend/handlers/malha_fina.go — sanitizeDBErr aplicado
- [x] backend/handlers/apuracao_painel.go — sanitizeDBErr aplicado
- [x] backend/handlers/cte_entradas.go — sanitizeDBErr aplicado
- [x] backend/handlers/creditos_perdidos.go — sanitizeDBErr aplicado
- [x] backend/handlers/filial_apelidos.go — sanitizeDBErr aplicado

## Self-Check: PASSED
