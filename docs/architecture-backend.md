# Arquitetura — Backend Go (API)

**Parte:** `backend/`  
**Tipo:** REST API — Go 1.22, `net/http` stdlib, PostgreSQL 15  
**Padrão:** Handler Factory Pattern — sem framework de roteamento externo

---

## Sumário Executivo

O backend é uma API REST em Go puro (`net/http`) sem framework de roteamento externo (sem Echo, Gin ou Chi). Todos os handlers seguem o padrão **factory function**: uma função que recebe `*sql.DB` e retorna um `http.HandlerFunc`. A injeção de dependência ocorre no momento do registro das rotas em `main.go`, não em runtime.

O banco de dados é PostgreSQL 15 acessado via `database/sql` + `lib/pq` — sem ORM. Toda a lógica de multi-tenancy é via `WHERE company_id = $1` explícito em cada query (sem RLS generalizado, embora o middleware de TenantContext exista e esteja pronto para uso futuro).

---

## Stack

| Categoria | Tecnologia | Versão |
|-----------|-----------|--------|
| Linguagem | Go | 1.22.0 |
| HTTP server | `net/http` stdlib | — |
| Database driver | `github.com/lib/pq` | v1.11.2 |
| Auth (JWT) | `github.com/golang-jwt/jwt/v5` | v5.3.1 |
| Password | `golang.org/x/crypto` bcrypt | v0.17.0 |
| Config | `github.com/joho/godotenv` | v1.5.1 |
| Text | `golang.org/x/text` (Unicode norm) | v0.14.0 |
| Banco de dados | PostgreSQL | 15 |

---

## Padrão Arquitetural

### Handler Factory Pattern

```go
// Assinatura padrão de todo handler
func NomeHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // lógica aqui
    }
}
```

### Registro de Rotas em `main.go`

```go
// Closures definidas no main() — DI local
withDB := func(h http.HandlerFunc) http.HandlerFunc { ... }
withAuth := func(h http.HandlerFunc, role string) http.HandlerFunc { ... }

// Registro
http.HandleFunc("/api/rfb/apuracao/status", withAuth(handlers.StatusApuracaoHandler(db), ""))
http.HandleFunc("/api/admin/users", withAuth(handlers.ListUsersHandler(db), "admin"))
```

### Middleware Global

```
SecurityMiddleware (CORS + security headers)
    └── withDB (verifica DB disponível)
        └── withAuth (AuthMiddleware — JWT + role check)
            └── Handler(db)
```

---

## Estrutura de Pacotes

```
backend/
├── main.go                 ← Bootstrap completo (500+ linhas)
├── handlers/               ← Handlers HTTP por domínio
├── services/               ← Integrações externas + goroutines background
├── middleware/             ← TenantContext (RLS helper)
└── migrations/             ← 113 arquivos SQL auto-executados
```

### `main.go` — Responsabilidades

1. Carrega `.env` via `godotenv.Load()` (dev only)
2. Valida `JWT_SECRET` (comprimento mínimo 32 bytes em prod)
3. Inicia conexão PostgreSQL de forma **assíncrona** (polling com backoff)
4. Ao conectar: executa migrations (onDBConnected) + registra todas as rotas
5. Lança goroutines background:
   - `go services.StartRFBScheduler(getDB)` — agendamento automático CBS
   - `go services.StartRawJSONJanitor(getDB)` — limpeza de dados antigos
6. Serve frontend compilado de `./static/` (embed)
7. Graceful shutdown (SIGTERM, 30s timeout)

---

## Configuração via Variáveis de Ambiente

| Variável | Obrigatória | Padrão | Descrição |
|----------|-------------|--------|-----------|
| `DATABASE_URL` | Sim | — | Conexão PostgreSQL (postgres://...) |
| `JWT_SECRET` | Sim | — | Secret HS256 (mín. 32 bytes em prod) |
| `ENCRYPTION_KEY` | Não | `JWT_SECRET` | Chave para credenciais RFB/CGIBS |
| `PORT` | Não | `8081` | Porta HTTP |
| `ALLOWED_ORIGINS` | Não | Lista padrão | CORS — CSV de origens permitidas |
| `COOKIE_SECURE` | Não | Detectado por header | `true` em prod (HttpOnly Secure) |
| `RFB_API_URL` | Não | `https://api.receitafederal.gov.br` | Base URL da API RFB |
| `RFB_TOKEN_URL` | Não | URL padrão | OAuth2 token URL da RFB |
| `RFB_WEBHOOK_URL` | Sim (prod) | — | URL de callback registrada na RFB |
| `APP_URL` | Não | — | URL pública da aplicação |
| `SMTP_*` | Não | — | Configurações de e-mail |

---

## Autenticação e Segurança

### JWT (Access Token)

- **Algoritmo:** HS256
- **Duração:** 30 minutos
- **Claims:** `user_id` (UUID string), `role` (string), `exp` (Unix timestamp)
- **Transporte:** `Authorization: Bearer {token}`
- **Revogação:** `tokenBlacklist sync.Map` — verificado **antes** da validação da assinatura

### Refresh Token

- **Geração:** 64 bytes hex via `crypto/rand`
- **Duração:** 7 dias
- **Armazenamento:** `refreshTokenStore sync.Map` em **memória** (perdido em restart de container)
- **Cookie:** `HttpOnly=true, SameSite=Strict, Secure=dynamic, Path=/api/auth/`
- **Rotação:** Obrigatória — token antigo deletado e novo emitido em cada uso

### Rate Limiting

| Endpoint | Limite | Janela |
|----------|--------|--------|
| Login | 5 tentativas | 15 min / IP |
| Register | 10 | 1 hora / IP |
| Forgot Password | 3 | 1 hora / IP |
| Reset Password | 5 | 1 hora / IP |
| Change Password | 5 | 15 min / UserID |

> **Importante:** Rate limiters em memória (`sync.Mutex`) — não distribuídos. Não funcionam corretamente com múltiplas instâncias.

### Security Headers (SecurityMiddleware)

```
X-Frame-Options: DENY
X-Content-Type-Options: nosniff
X-XSS-Protection: 1; mode=block
Strict-Transport-Security: max-age=31536000; includeSubDomains
Content-Security-Policy: default-src 'self'; ...
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: geolocation=(), microphone=(), camera=()
```

### ERP Bridge Authentication

- `X-API-Key` header → SHA256 hex → comparado com `api_key_hash` em `erp_bridge_config`
- Endpoints: `/api/erp-bridge/import/batch`, `/api/erp-bridge/parceiros/sync`, `/api/erp-bridge/heartbeat`

### Webhook RFB

- HMAC-SHA256 do payload usando `JWT_SECRET`
- Verificado antes de qualquer processamento

---

## Multi-tenancy

**Mecanismo:** `company_id` UUID em todas as tabelas de dados fiscais + `WHERE company_id = $1` explícito em cada query.

**Resolução do `company_id`** (função `GetEffectiveCompanyID` em `handlers/auth.go:323`):
1. Header `X-Company-ID` (override — verificado se o usuário tem acesso)
2. `preferred_company_id` onde o usuário é owner
3. `preferred_company_id` onde o usuário é member

**TenantContext** (`middleware/tenant.go`): Definido para uso futuro com RLS — `SET LOCAL app.tenant_id` + `app.user_id` na transação. **Ainda não aplicado nos handlers de produção.**

---

## Serviços Background

### RFB Scheduler (`services/rfb_scheduler.go`)

```
go services.StartRFBScheduler(getDB)  // goroutine em main()
    │
    ├── Aguarda DB disponível (polling 5s)
    ├── Goroutine filho: ticker 5min → AbortStuckRFBRequests (timeout > 5h)
    └── Ticker 1min:
        ├── Busca rfb_credentials WHERE agendamento_ativo=true AND horario==agora (BRT)
        └── Para cada empresa:
            └── goroutine: SolicitarApuracaoParaEmpresa(db, companyID)
                ├── Verifica rate-limit (memória + persistência via error_message)
                ├── Verifica slot do dia (max 1 solicitação automática/dia)
                ├── OAuth2 GetToken (cacheado por client_id com margem 5min)
                ├── SolicitarApuracao → RFB API
                └── INSERT rfb_requests status='requested'
```

### RFB Client (`services/rfb.go`)

- `GetToken`: OAuth2 `client_credentials` — token cacheado por `client_id`
- `IsRateLimited` / `SetRateLimitUntil`: Cache em memória — persistido via `error_message` no banco (sobrevive a restarts)
- Timeout HTTP: 30 segundos
- Ambientes: `producao_restrita` usa path prefix `prr-rtc`; demais usam `rtc`

### Raw JSON Janitor (`services/rfb_janitor.go`)

- `go services.StartRawJSONJanitor(getDB)` — goroutine em `main()`
- Limpa coluna `raw_json` de `rfb_requests` antigos para economizar espaço no banco

---

## Pool de Conexões PostgreSQL

| Configuração | Valor |
|-------------|-------|
| `SetMaxOpenConns` | 50 |
| `SetMaxIdleConns` | 15 |
| `SetConnMaxLifetime` | 15 minutos |

---

## Migrations

- Executadas automaticamente em `onDBConnected()` ao inicializar o backend
- Tabela de controle: `schema_migrations (filename PK, executed_at)`
- Ordem: alfabética/numérica — `001_`, `002_`, ..., `113_`
- Gaps intencionais: 003, 004, 011 não existem
- Sufixo `b` para correções sem quebrar sequência: `021b_ensure_admin_user.sql`
- Arquivos `.disabled` são ignorados pelo loader

---

## Convenções de Código

### Tratamento de Erros

```go
// Padrão novo (handlers refatorados):
func jsonErr(w http.ResponseWriter, status int, msg string) { ... }   // config.go
func sanitizeDBErr(w, status, msg, err, prefix) { ... }               // config.go — loga interno, retorna genérico

// Padrão legado (auth.go):
http.Error(w, err.Error(), status)  // Pode expor mensagem interna ao cliente
```

### Logging

```go
log.Printf("[HandlerName] Description: %v", err)  // antes de responder ao cliente
```

### Transações

```go
tx, err := db.Begin()
if err != nil { return }
defer tx.Rollback()
// ... operações ...
tx.Commit()
```

### Contexto e Extração de Claims

```go
userID := handlers.GetUserIDFromContext(r)
companyID := handlers.GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
```

---

## Anti-Padrões Conhecidos (Débito Técnico)

| Anti-padrão | Localização | Impacto |
|-------------|-------------|---------|
| `withDB`/`withAuth` definidas como closures em `main()` | `main.go:276-298` | Dificulta teste isolado |
| Refresh tokens em memória apenas | `auth.go:68` | Perda em restart do container |
| TenantContext não aplicado nos handlers | `middleware/tenant.go` | RLS não enforçado automaticamente |
| Rate limiters não distribuídos | `handlers/middleware.go` | Inefetivo com múltiplas instâncias |
