---
plan: 03-04
phase: 03-debito-tecnico
status: complete
date: 2026-05-12
subsystem: security/git
tags: [security, gitignore, credentials, debt]
requires: []
provides: [backend/tools/ removed from git tracking]
affects: [.gitignore]
tech-stack-added: []
tech-stack-patterns: [git rm --cached]
key-files-created: []
key-files-modified: [.gitignore]
decisions:
  - "Usou git rm --cached para manter arquivos fisicos no filesystem local"
  - "Adicionou backend/tools/ ao .gitignore para prevenir re-rastreamento"
metrics:
  duration: 3min
  tasks-completed: 1
  files-changed: 1
---

# Phase 03 Plan 04: Remover backend/tools/ do rastreamento git (DEBT-04)

Removidos 5 arquivos de ferramentas de debug do rastreamento git via `git rm --cached`.
Os arquivos continham `postgres://postgres:postgres@localhost` hardcoded e nunca deveriam
estar no repositório.

## Summary

- 5 arquivos removidos do rastreamento git via `git rm --cached`
- `backend/tools/` adicionado ao `.gitignore` para prevenir re-rastreamento
- Arquivos fisicos preservados no filesystem local para uso de desenvolvimento

## Arquivos removidos do git

| Arquivo | Credencial encontrada |
|---------|----------------------|
| `backend/tools/debug_detailed.go` | `postgres://postgres:postgres@localhost` (fallback hardcoded) |
| `backend/tools/debug_gilson.go` | Ferramenta de debug |
| `backend/tools/debug_query.go` | Ferramenta de debug |
| `backend/tools/debug_stats.go` | Ferramenta de debug |
| `backend/tools/verify_data.go` | `postgres://postgres:postgres@localhost:5432/fb_apu01` (hardcoded) |

## Verification

- `git ls-files backend/tools/`: 0 linhas (PASS)
- Arquivos fisicos presentes: 5 arquivos (PASS)
- `grep "backend/tools" .gitignore`: encontrou entrada (PASS)
- `go build ./...`: BUILD OK (PASS)

## Deviations from Plan

None - plano executado exatamente como escrito.

## Known Issues (out of scope — deferred)

Durante a verificacao `git grep "postgres://postgres:postgres"`, foram encontrados
arquivos rastreados fora do escopo deste plano que tambem contem a credencial:

| Arquivo | Tipo | Observacao |
|---------|------|------------|
| `.env.FB_APU01` | Arquivo .env rastreado | Deveria estar no .gitignore — plano separado |
| `backend/fb_apu02` | Binario compilado rastreado | Deveria estar no .gitignore — plano separado |
| `backend/main.go` | Fallback de conexao dev | Intencional como fallback local |
| `docs/FASE_01_DOCUMENTACAO_COMPLETA.md` | Documentacao historica | Credencial de exemplo em doc |

Esses itens sao pre-existentes e estao fora do escopo do DEBT-04. Registrados
para acompanhamento em planos futuros.

## Self-Check: PASSED

- `.gitignore` modificado: commit 1e55b4f confirmado
- `git ls-files backend/tools/` retorna 0 linhas: PASSED
- Arquivos fisicos em `backend/tools/`: 5 presentes
- `go build ./...`: BUILD OK
