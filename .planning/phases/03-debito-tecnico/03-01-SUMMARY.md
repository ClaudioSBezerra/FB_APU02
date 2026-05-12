---
phase: 03-debito-tecnico
plan: 01
subsystem: migrations
tags: [migrations, rename, idempotency, DEBT-01]
status: complete
date: 2026-05-12
requires: []
provides: [deterministic-migration-ordering]
affects: [backend/migrations]
tech-stack:
  added: []
  patterns: [git-mv-rename]
key-files:
  created:
    - backend/migrations/021b_ensure_admin_user.sql
    - backend/migrations/061b_user_environments_preferred_company.sql
  modified: []
decisions:
  - "Renomear com sufixo 'b' para preservar semântica do número original sem conflito"
  - "Migrations sao idempotentes — re-aplicacao em ambientes existentes e segura"
metrics:
  duration: "5 minutes"
  completed: 2026-05-12
  tasks_completed: 1
  tasks_total: 1
  files_changed: 2
---

# Phase 03 Plan 01: Renomear Migrations Duplicadas — Summary

Dois pares de arquivos de migration compartilhavam o mesmo prefixo numérico, tornando a ordem de execução não-determinística em filesystems que não garantem ordenação consistente. Os arquivos foram renomeados com sufixo `b` via `git mv` para eliminar a ambiguidade.

## Tasks Concluídas

| Task | Nome | Commit | Arquivos |
|------|------|--------|----------|
| 1 | Renomear migrations duplicadas com sufixo 'b' | f23dd4e | 021b_ensure_admin_user.sql, 061b_user_environments_preferred_company.sql |

## Verification

- `ls backend/migrations/ | grep "^021"`: 2 arquivos distintos — `021_create_mv_mercadorias.sql` e `021b_ensure_admin_user.sql` ✓
- `ls backend/migrations/ | grep "^061"`: 2 arquivos distintos — `061_add_filial_to_mv_operacoes_simples.sql` e `061b_user_environments_preferred_company.sql` ✓
- `go build ./...`: passou sem erros ✓
- Conteúdo idempotente confirmado:
  - `021b_ensure_admin_user.sql`: usa `IF EXISTS` para UPDATE/INSERT condicional — seguro para re-aplicar
  - `061b_user_environments_preferred_company.sql`: usa `ADD COLUMN IF NOT EXISTS` — seguro para re-aplicar

## Impacto em Ambientes Existentes

O sistema de migrations rastreia execuções pelo nome completo do arquivo (`schema_migrations.filename`). Em ambientes onde `021_ensure_admin_user.sql` e `061_user_environments_preferred_company.sql` já foram aplicados, as versões `021b_` e `061b_` serão tratadas como novas migrations e **serão re-executadas**. Isso é seguro porque o conteúdo é idempotente (conforme verificado acima).

## Deviations from Plan

### Estado preexistente incluído no commit

**Encontrado durante:** Commit da Task 1

**Situação:** O commit `f23dd4e` incluiu deletions de 5 arquivos em `backend/tools/` (`debug_detailed.go`, `debug_gilson.go`, `debug_query.go`, `debug_stats.go`, `verify_data.go`) que já estavam staged para deleção no índice git antes do início desta execução.

**Ação:** Não foram feitas alterações nesses arquivos — eles estavam no estado staged deletion preexistente. O commit os incluiu por estarem no índice. Isso não afeta a task nem o objetivo do plano.

**Arquivos afetados:** `backend/tools/*.go` (5 arquivos deletados — preexistente)

## Self-Check: PASSED

- `backend/migrations/021b_ensure_admin_user.sql`: FOUND ✓
- `backend/migrations/061b_user_environments_preferred_company.sql`: FOUND ✓
- Commit `f23dd4e`: FOUND ✓
- `backend/migrations/021_ensure_admin_user.sql`: removido corretamente ✓
- `backend/migrations/061_user_environments_preferred_company.sql`: removido corretamente ✓
