# Build Stage — cache bust: 2026-03-25b
FROM golang:1.22-alpine AS builder
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
RUN apk --no-cache add tzdata ca-certificates && \
    ln -sf /usr/share/zoneinfo/America/Sao_Paulo /etc/localtime
WORKDIR /root/
COPY --from=builder /app/server .
COPY --from=builder /app/migrations ./migrations
EXPOSE 8081
CMD ["./server"]
# Force Rebuild v1.0.3 - tzdata para RFB Scheduler