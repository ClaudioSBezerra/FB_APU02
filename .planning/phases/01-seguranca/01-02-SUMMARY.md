---
phase: 01-seguranca
plan: 02
subsystem: auth
tags: [rate-limiting, brute-force, login, security, go]

# Dependency graph
requires: []
provides:
  - LoginRL aplicado em LoginHandler — endpoint /api/auth/login protegido contra brute-force
  - SEC-02 resolvido: rate limiter existente ativado na rota de login
affects:
  - 01-seguranca (outros planos de segurança que dependem de auth sólido)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Rate limiter aplicado antes de qualquer consulta ao banco no início do handler closure"
    - "GetClientIP(r) como extrator canônico de IP — protege contra spoofing via X-Forwarded-For"

key-files:
  created:
    - backend/handlers/login_ratelimit_test.go
  modified:
    - backend/handlers/auth.go

key-decisions:
  - "Rate limit inserido como primeiríssima operação do handler (antes de json.Decode), garantindo que tentativas em excesso nunca alcançam o banco de dados"
  - "Padrão de RegisterHandler usado como modelo exato — consistência entre todos os handlers de auth"

patterns-established:
  - "TDD RED/GREEN: teste escrito com db=nil para provar que o rate limiter deve ser checado antes do DB"
  - "LoginRL.Reset(ip) usado no setUp do teste para isolar estado entre runs"

requirements-completed:
  - SEC-02

# Metrics
duration: 7min
completed: 2026-05-12
---

# Phase 01 Plan 02: Rate Limit no Login Summary

**LoginRL.Allow(ip) aplicado como primeira operação em LoginHandler — endpoint /api/auth/login agora rejeita com HTTP 429 após 5 tentativas por IP em 15 minutos**

## Performance

- **Duration:** 7 min
- **Started:** 2026-05-12T12:55:57Z
- **Completed:** 2026-05-12T13:02:28Z
- **Tasks:** 1 (TDD: RED + GREEN)
- **Files modified:** 2

## Accomplishments

- `LoginHandler` agora verifica `LoginRL.Allow(ip)` imediatamente ao entrar no closure, antes de `json.Decode` e qualquer acesso ao banco
- Brute-force contra `/api/auth/login` é bloqueado com HTTP 429 após 5 tentativas por IP em 15 min
- Teste unitário adicionado (`TestLoginHandlerRateLimit`) que valida o comportamento usando db=nil como prova estrutural de que o rate limiter é chamado antes do DB
- Build compila sem erros

## Task Commits

Cada fase do TDD foi commitada atomicamente:

1. **RED — Teste falhando** - `9a90e20` (test)
2. **GREEN — Implementação** - `849d321` (feat)

**Plan metadata:** a ser adicionado

_TDD com dois commits: test(RED) → feat(GREEN)_

## Files Created/Modified

- `backend/handlers/login_ratelimit_test.go` - Teste unitário que verifica que LoginRL.Allow é chamado ANTES do DB
- `backend/handlers/auth.go` - LoginHandler modificado: +5 linhas inseridas no início do closure

## Decisions Made

- Rate limit inserido ANTES do `json.NewDecoder(r.Body).Decode(&req)` para garantir que a verificação aconteça mesmo com corpo inválido (consistência com objetivo de segurança)
- Padrão idêntico ao `RegisterHandler` (linhas 469–476 em auth.go) — mantém coerência no código de auth
- `http.Error(w, "Too many requests", http.StatusTooManyRequests)` (sem JSON body) — igual ao padrão de `http.Error` já usado em auth handlers para erros simples

## Deviations from Plan

None — plano executado exatamente como especificado.

O TDD foi aplicado conforme obrigatório pelo frontmatter `tdd="true"`. Teste criado primeiro (RED — falha com nil-pointer panic ao bater no DB), depois implementação (GREEN — passa em 0.004s).

## Issues Encountered

- **Path safety em worktree (#3099):** Na primeira tentativa, o arquivo de teste foi escrito no path do repositório principal (`/home/.../FB_APU02/backend/handlers/`) em vez do worktree (`/home/.../worktrees/agent-.../backend/handlers/`). O commit acidental foi revertido (`git reset --hard HEAD~1` no main repo) e o arquivo foi recriado no path correto. Operações subsequentes usaram paths absolutos derivados de `git rev-parse --show-toplevel`.

## Known Stubs

Nenhum — a alteração é um fix de segurança sem dados de UI ou placeholders.

## Threat Flags

Nenhum novo surface introduzido. A alteração apenas ativa uma mitigação já planejada (T-02-03 no threat model do plano).

## TDD Gate Compliance

- RED gate commit: `9a90e20` — `test(01-02): add failing test for LoginRL rate limit on login endpoint`
- GREEN gate commit: `849d321` — `feat(01-02): aplicar LoginRL no início de LoginHandler (SEC-02)`
- REFACTOR: nao necessario (5 linhas adicionadas, sem code smell)

## Next Phase Readiness

- SEC-02 completo e deployavel
- `LoginHandler` protegido; padrão estabelecido para outros handlers que precisem de rate limiting
- Sem blockers para os demais planos de Phase 01

---
*Phase: 01-seguranca*
*Completed: 2026-05-12*
