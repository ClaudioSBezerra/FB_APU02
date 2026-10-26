# Build Stage — cache bust: 2026-03-25b
# Pinado em patch exato (22/10, migração de segurança) — Go 1.22 saiu de
# suporte (só 1.25/1.26 recebem patch de segurança hoje; a tag
# "golang:1.22-alpine" está congelada desde fev/2025, sem nenhum fix
# novo possível nela). Subir pra 1.26.10 corrige 15 vulnerabilidades de
# biblioteca padrão achadas via `govulncheck` (net/http, crypto/tls,
# crypto/x509, net/url) — acompanha o bump do go.mod (go 1.22.0 -> 1.26.1)
# e a regeneração de vendor/ (go mod vendor).
FROM golang:1.26.10-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app

# Copy go.mod and go.sum from the backend directory
COPY backend/go.mod backend/go.sum ./

# Copy vendor directory
COPY backend/vendor ./vendor

# Debug: Check vendor directory
RUN ls -la vendor && cat vendor/modules.txt

# Copy the rest of the backend source code
COPY backend/ .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -v -ldflags="-w -s" -o server .

# Run Stage
FROM alpine:latest
RUN apk --no-cache add tzdata ca-certificates openssl && \
    ln -sf /usr/share/zoneinfo/America/Sao_Paulo /etc/localtime
# Usuário sem privilégio (21/10, migração de segurança) — o backend grava
# em disco de verdade (uploads/, backend/handlers/admin.go). WORKDIR
# continua /root/ de propósito — só o dono muda (chown).
RUN adduser -D -u 1000 appuser && chown appuser:appuser /root
WORKDIR /root/
COPY --from=builder --chown=appuser:appuser /app/server .
COPY --from=builder --chown=appuser:appuser /app/migrations ./migrations
USER appuser
EXPOSE 8081
CMD ["./server"]
# Force Rebuild v1.0.3 - tzdata para RFB Scheduler