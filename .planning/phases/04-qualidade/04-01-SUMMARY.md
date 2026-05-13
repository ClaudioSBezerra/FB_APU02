---
phase: 04-qualidade
plan: "01"
subsystem: frontend/testing
status: complete
date: 2026-05-13
tags: [vitest, testing, frontend, utils]
dependency_graph:
  requires: []
  provides: [frontend-test-runner, utils-test-suite]
  affects: [frontend/package.json, frontend/vite.config.ts, frontend/tsconfig.app.json, frontend/src/lib/utils.test.ts]
tech_stack:
  added:
    - vitest@1.6.1 (devDependency — test runner)
  patterns:
    - Vitest integrado no vite.config.ts via triple-slash reference (sem arquivo vitest.config.ts separado)
    - environment=node para testes de funções puras (sem DOM/React)
key_files:
  modified:
    - frontend/package.json
    - frontend/package-lock.json
    - frontend/vite.config.ts
    - frontend/tsconfig.app.json
    - frontend/src/lib/utils.test.ts
decisions:
  - environment='node' escolhido em vez de 'jsdom' — testes cobrem funções puras (cn, formatCurrency), sem DOM; evita devDep jsdom mantendo footprint mínimo conforme milestone de estabilização
  - vitest integrado no vite.config.ts existente (triple-slash reference) em vez de criar vitest.config.ts separado — reduz arquivos de config
  - vitest run (modo CI sem watch) como script test — terminação determinística para CI
metrics:
  duration: "~5 minutos"
  completed: "2026-05-13"
  tasks_completed: 3
  files_changed: 5
---

# Phase 4 Plan 01: Configurar Vitest e Suite Mínima de Utils — Summary

Configured Vitest 1.6.1 as frontend test runner with 6 passing unit tests for pure utility functions (cn + formatCurrency).

## Installed

**vitest@1.6.1** (devDependency only — vitest@^1.6.0 resolveu para 1.6.1)

Nenhuma dependência adicional instalada: sem `@testing-library/react`, sem `jsdom`, sem `@vitest/coverage-v8`.

## Script

```json
"test": "vitest run"
```

Adicionado ao bloco `scripts` do `frontend/package.json`.

## Config

`frontend/vite.config.ts` — bloco test adicionado com:

```typescript
/// <reference types="vitest" />   // linha 1 — triple-slash reference
// ...
test: {
  globals: true,
  environment: 'node',
  include: ['src/**/*.{test,spec}.{ts,tsx}'],
},
```

**Environment choice:** `'node'` (não `jsdom`) — testes cobrem funções utilitárias puras (`cn`, `formatCurrency`) sem interação com DOM ou componentes React. Evita a devDep `jsdom` mantendo footprint mínimo.

`frontend/tsconfig.app.json` — adicionado `"types": ["vitest/globals"]` em `compilerOptions` para tipagem TypeScript de `expect`/`test`/`describe`.

## Tests Passing

```
src/lib/utils.test.ts (6 tests) 76ms
  ✓ cn merges class names correctly
  ✓ cn handles conditional classes
  ✓ cn merges tailwind classes
  ✓ formatCurrency formats positive numbers in BRL
  ✓ formatCurrency formats zero
  ✓ formatCurrency formats negative numbers
```

**Total: 6 testes, todos PASS**

Nota: testes de `formatCurrency` usam regex `/1[.,]234[.,]56/` e `/99[.,]90/` para robustez entre versões do ICU (formatação pt-BR usa vírgula como separador decimal — `R$ 1.234,56`). O ICU do Node.js 20+ no ambiente de desenvolvimento confirmou os separadores corretos.

## Build Verification

`npm run build`: passou sem erros. 1691 módulos transformados, build em 10.79s. O aviso de chunk size (`> 500 kB`) é pré-existente e não relacionado às alterações deste plano.

## Deviations from Plan

None — plano executado exatamente como descrito.

## Commits

| Task | Commit | Descrição |
|------|--------|-----------|
| 1 | 7bf62d4 | chore(04-01): instalar vitest@^1.6.1 como devDependency e adicionar script test |
| 2 | a83ef5c | chore(04-01): configurar bloco test do Vitest no vite.config.ts |
| 3 | bd7288a | test(04-01): expandir suite utils.test.ts com testes para formatCurrency |

## Self-Check: PASSED

- [x] `frontend/package.json` — script test e devDep vitest presentes
- [x] `frontend/vite.config.ts` — triple-slash line 1, bloco test com environment=node
- [x] `frontend/tsconfig.app.json` — types: ["vitest/globals"]
- [x] `frontend/src/lib/utils.test.ts` — 6 testes passando
- [x] `npm test` — exit code 0
- [x] `npm run build` — exit code 0
- [x] Commits 7bf62d4, a83ef5c, bd7288a existem no git log
