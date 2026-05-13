---
plan: 04-03
phase: 04-qualidade
status: complete
date: 2026-05-13
subsystem: ci
tags: [ci, github-actions, testing, quality-gate]
dependency_graph:
  requires: [04-01, 04-02]
  provides: [CI test gate blocking deploy]
  affects: [.github/workflows/deploy-production.yml, .github/workflows/deploy-staging.yml]
tech_stack:
  added: [actions/setup-go@v5, actions/setup-node@v4]
  patterns: [job dependency via needs, always() with explicit success guard]
key_files:
  modified:
    - .github/workflows/deploy-production.yml
    - .github/workflows/deploy-staging.yml
decisions:
  - Duplicar bloco test entre 2 workflows vs extrair reusable workflow — optou-se por duplicar (30 linhas x2) por ser milestone de estabilizacao; refatorar para reusable workflow se +1 workflow for adicionado
  - Em production usar needs.test.result == 'success' explicito na condicao if porque sempre() bypass o comportamento padrao; em staging sem if — needs: test sozinho ja bloqueia por padrao
  - Job test roda em paralelo com backup (sem needs mutuo) — backup deve rodar mesmo que testes falhem para ter artefato de investigacao disponivel
metrics:
  duration: ~5min
  completed: 2026-05-13
  tasks_completed: 2
  tasks_total: 3
  files_modified: 2
---

# Phase 04 Plan 03: CI Test Gate Summary

Added `test` job to both CI deployment workflows — `go test ./...` + `npm test` must pass before any Docker image is built or pushed.

## Changes

### deploy-production.yml

- Added `test` job (6 steps): checkout → setup-go@v5 (1.22, cache=true) → `go test ./...` → setup-node@v4 (20, cache=npm) → `npm ci` → `npm test`
- `test` runs in parallel with `backup` (no mutual dependency)
- `build-and-push`: `needs: backup` → `needs: [backup, test]`; `if:` updated to require `needs.test.result == 'success'`
- `build-installer-images`: same changes as `build-and-push`
- `deploy-cliente-aws`, `notify`: unchanged

### deploy-staging.yml

- Added `test` job: identical 6-step block as production
- `build-images`: added `needs: test` (was previously dependency-free); no `if:` added — GitHub Actions skips dependents automatically when `needs` job fails without `always()`
- `deploy-to-staging`: unchanged (`needs: build-images` already handles cascading skip)

## Verification

- YAML valid (`yaml.safe_load`): production OK, staging OK
- `go test ./...` passes locally in backend/: confirmed via plan 04-02
- `npm test` (`vitest run`) passes locally in frontend/: confirmed via plan 04-01
- Commit hash: 49cf580

## Actions Used

| Action | Version | Purpose |
|--------|---------|---------|
| actions/checkout | @v4 | Repository checkout |
| actions/setup-go | @v5 | Go 1.22 with module cache |
| actions/setup-node | @v4 | Node 20 with npm cache |

## Pending

Human checkpoint (Task 3): Validate actual CI run on GitHub Actions. Verify:
- Job `test` appears in workflow graph
- `test` runs in parallel with `backup` (both dependency-free)
- Steps execute in order: checkout → setup-go → go test → setup-node → npm ci → npm test
- `build-and-push` and `build-installer-images` wait for `test` to complete
- Build jobs are blocked when `test` fails
- (Recommended) Break a test intentionally, push, confirm build is skipped, revert

## Deviations from Plan

None — plan executed exactly as written.

## Self-Check: PASSED

- .github/workflows/deploy-production.yml: FOUND
- .github/workflows/deploy-staging.yml: FOUND
- Commit 49cf580: FOUND
