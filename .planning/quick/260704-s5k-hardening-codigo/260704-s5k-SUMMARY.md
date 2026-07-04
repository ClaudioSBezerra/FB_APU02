---
phase: quick-260704-s5k
plan: 01
subsystem: backend/security
tags: [security, hardening, rate-limit, password-policy, upload-limit]
status: complete

provides:
  - "Política de senha (min 8) no cadastro"
  - "Rate limit em reset-password (IP) e change-password (userID)"
  - "http.MaxBytesReader nos uploads (configurável via MAX_UPLOAD_BYTES, default 512 MiB)"
  - "Aviso de JWT_SECRET curto (< 32 bytes) no boot"

key-files:
  modified:
    - backend/handlers/auth.go
    - backend/handlers/middleware.go
    - backend/handlers/xml_upload.go
    - backend/handlers/pagamentos_fornecedores.go

key-decisions:
  - "Todas as mudanças aditivas — nenhuma derruba prod no deploy"
  - "JWT_SECRET curto vira WARNING (não fatal) para não crashar prod caso o valor real em uso seja curto"
  - "Limite de upload generoso (512 MiB) e configurável para não quebrar importações grandes de pastas de XML"

requirements-completed: [SEC-HARDENING-CODE-01]

duration: ~20min
completed: 2026-07-04
---

# Quick Task 260704-s5k: Batch de hardening de segurança (código)

## Accomplishments

1. **Política de senha no cadastro** — `RegisterHandler` agora exige min 8 chars
   (alinhado a reset/change). Verificado: senha curta → HTTP 400.
2. **Rate limit** — `ResetPasswordRL` (5/h por IP) e `ChangePasswordRL`
   (5/15min por userID). Verificado: 6ª tentativa de change-password → HTTP 429.
3. **Limite de upload** — `http.MaxBytesReader` nos 3 handlers de xml_upload e no
   de pagamentos; helper `maxUploadBytes()` (env `MAX_UPLOAD_BYTES`, default 512 MiB).
4. **JWT_SECRET curto** — `ValidateJWTSecret` loga WARNING se < 32 bytes.

## Verification

- `go build/vet/test` ✅
- Funcional local: cadastro senha curta → 400 ✅; change-password 6ª → 429 ✅

## Fora deste batch — guia de ops (pendente, exige coordenação)

Riscos que precisam de ação nos servidores (não podem ir só no código sem quebrar):
- Rotacionar JWT_SECRET (valor `super-secure-...-2026` está commitado em
  `.env.production`/`.env.hostinger` — não é o usado em prod, mas deve ser
  scrubado do repo) e setar ENCRYPTION_KEY distinta.
- TLS no cliente AWS (hoje HTTP puro) + COOKIE_SECURE=true.
- RFB_WEBHOOK_SECRET setado (webhook público falha-aberto sem ele).
- Refresh token/blacklist em Redis (hoje em memória — some no restart).
- Dockerfile do backend não-root; gates de aprovação no runner de deploy;
  docker.sock/Prometheus expostos; CSP unsafe-inline.
