<!-- refreshed: 2026-05-12 -->
# Architecture

**Analysis Date:** 2026-05-12

## System Overview

```text
┌───────────────────────────────────────────────────────────────────┐
│                     Browser (React SPA)                           │
│   `frontend/src/App.tsx`  — React Router + AuthProvider           │
│   `frontend/src/pages/`   — 40+ page components                   │
│   `frontend/src/contexts/` — AuthContext, FilialContext            │
└─────────────────────┬─────────────────────────────────────────────┘
                      │  HTTPS / JSON (Bearer JWT + X-Company-ID)
                      │  served by Nginx (`frontend/nginx.conf`)
                      ▼
┌───────────────────────────────────────────────────────────────────┐
│              Go HTTP API (`backend/main.go`)  :8081               │
│   SecurityMiddleware → AuthMiddleware → withDB/withAuth wrappers  │
│   `backend/handlers/`  — flat handler package (one file/domain)   │
│   `backend/services/`  — RFB OAuth2 client, scheduler, email      │
│   `backend/middleware/` — TenantContext (RLS session variables)    │
└──────────┬─────────────────────────┬─────────────────────────────┘
           │                         │
           ▼                         ▼
┌──────────────────┐     ┌──────────────────────────────────────────┐
│  PostgreSQL 15   │     │   ERP Bridge (Python daemon on AWS)      │
│  `migrations/`   │     │   `erp-bridge-aws/bridge.py`             │
│  104 migrations  │     │   Oracle ERP → POST /api/erp-bridge/...  │
│  Materialized    │     │   auth: X-API-Key (not JWT)              │
│  views + RLS     │     └──────────────────────────────────────────┘
└──────────────────┘
           │
    Redis (session / optional cache — declared in docker-compose,
           not yet wired in Go code for caching)
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| `main.go` | Server bootstrap, route registration, DB init + migrations, graceful shutdown | `backend/main.go` |
| `handlers/` | HTTP handler functions — one file per feature domain | `backend/handlers/` |
| `services/` | External API clients (RFB OAuth2), background scheduler, email | `backend/services/` |
| `middleware/` | TenantContext struct + WithTenant DB transaction helper | `backend/middleware/tenant.go` |
| `migrations/` | Ordered SQL migrations auto-applied on startup | `backend/migrations/` |
| `AuthContext` | JWT token storage, fetch interceptor, session restore | `frontend/src/contexts/AuthContext.tsx` |
| `FilialContext` | Branch list per company, multi-branch filter state | `frontend/src/contexts/FilialContext.tsx` |
| `App.tsx` | React Router config, ProtectedRoute, AdminRoute, AppLayout | `frontend/src/App.tsx` |
| `pages/` | Feature pages (40+), consume backend APIs via native fetch | `frontend/src/pages/` |
| `lib/navigation.ts` | Module/tab structure definition — single source of navigation truth | `frontend/src/lib/navigation.ts` |
| `bridge.py` | Python daemon — queries Oracle ERP, pushes documents to API | `erp-bridge-aws/bridge.py` |

## Pattern Overview

**Overall:** Monolith with thin service layer — single Go binary serves both the REST API and the React SPA static files from `./static/`. The frontend is a separate Vite/React SPA served by Nginx in Docker and also bundled into the Go binary in production.

**Key Characteristics:**
- Multi-tenant via `company_id` column on every data table; tenant identity resolved per-request using JWT claims + `X-Company-ID` header via `GetEffectiveCompanyID()`
- No ORM — all database queries are raw SQL via `database/sql` + `lib/pq`
- Handler factory pattern: handlers are functions returning `http.HandlerFunc`, receiving `*sql.DB` as argument — `func SomeHandler(db *sql.DB) http.HandlerFunc`
- Authentication: short-lived JWT (30 min) in `Authorization: Bearer` header + long-lived refresh token in `HttpOnly` cookie (7 days), rotated on use
- Frontend uses a global `window.fetch` interceptor (monkey-patched in `AuthContext`) to automatically inject `Authorization` and `X-Company-ID` headers on every API call

## Layers

**Presentation (Frontend):**
- Purpose: React SPA — routing, UI rendering, user interactions
- Location: `frontend/src/`
- Contains: Pages, shared components, context providers, utility libs
- Depends on: Go REST API via fetch
- Used by: End users via browser

**HTTP Transport (Go handlers):**
- Purpose: Route requests, enforce auth/authz, parse input, return JSON responses
- Location: `backend/handlers/`
- Contains: One `.go` file per feature domain (e.g., `nfe_entradas.go`, `rfb_apuracao.go`)
- Depends on: `*sql.DB` passed at registration time from `main.go`
- Used by: `main.go` route registration

**Services (Go background):**
- Purpose: External API integration (Receita Federal CBS API), email, background scheduling
- Location: `backend/services/`
- Contains: `rfb.go` (HTTP client + OAuth2 token cache), `rfb_scheduler.go` (cron-style ticker), `rfb_processor.go`, `rfb_creditos_processor.go`, `email.go`
- Depends on: `*sql.DB`, environment variables
- Used by: Handlers (direct calls) and `main.go` (goroutine: `go services.StartRFBScheduler(getDB)`)

**Database:**
- Purpose: Persistent state — fiscal documents, users, credentials, apuração results
- Location: `backend/migrations/` (104 `.sql` files applied sequentially at startup)
- Contains: Tables for `nfe_entradas`, `nfe_saidas`, `cte_entradas`, `rfb_requests`, `cgibs_requests`, `erp_bridge_*`, `users`, `companies`, `environments`, `enterprise_groups`, `user_environments`, `rfb_credentials`, materialized view `mv_mercadorias`
- Depends on: PostgreSQL 15

**ERP Bridge (External daemon):**
- Purpose: Pull fiscal documents from Oracle ERP and push to API
- Location: `erp-bridge-aws/bridge.py`
- Contains: Oracle DB queries, XML multipart upload (oracle_xml mode) or JSON batch (sap_s4hana mode), local SQLite tracker, daemon loop
- Depends on: `oracledb`, `requests`, `pyyaml`; calls `POST /api/erp-bridge/import/batch` and `POST /api/erp-bridge/parceiros/sync`

## Data Flow

### Primary Request Path (authenticated API call)

1. Browser fetch with `Authorization: Bearer <token>` + `X-Company-ID` header (injected by `AuthContext` interceptor — `frontend/src/contexts/AuthContext.tsx:47-78`)
2. `SecurityMiddleware` validates CORS origin, sets security headers (`backend/handlers/middleware.go:92-118`)
3. `withAuth` wrapper calls `AuthMiddleware` — validates JWT signature, checks blacklist, injects `ClaimsKey` into `context.Context` (`backend/handlers/auth.go:207-259`)
4. Handler function called — extracts `user_id` from claims, calls `GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))` to resolve `company_id` (`backend/handlers/auth.go:323-391`)
5. Raw SQL query scoped to `company_id` — returns JSON response

### ERP Bridge Import Path

1. `bridge.py` daemon polls Oracle ERP, aggregates documents
2. POST to `POST /api/erp-bridge/import/batch` with `X-API-Key` header (no JWT)
3. `ERPBridgeBatchImportHandler` validates API key against DB, routes documents to `nfe_saidas`, `nfe_entradas`, or `cte_entradas` tables based on `direct` + `modelo` fields (`backend/handlers/erp_bridge_batch.go`)
4. Heartbeat: `POST /api/erp-bridge/heartbeat` — updates `daemon_last_seen` in `erp_bridge_config` table

### RFB Scheduler Path

1. `StartRFBScheduler` goroutine launched at startup (`backend/main.go:228`)
2. 1-minute ticker fires; queries `rfb_credentials` for companies with `agendamento_ativo = true` and matching `horario_agendamento`
3. For each company: `SolicitarApuracaoParaEmpresa` — OAuth2 token fetch → RFB API call → persist tiquete in `rfb_requests` (`backend/services/rfb_scheduler.go`)
4. Webhook callback: `POST /api/rfb/webhook` (public endpoint) — RFB posts result; handler processes and stores data

### User Registration Path

1. `POST /api/auth/register` — creates user, environment, enterprise_group, company in a single DB transaction (`backend/handlers/auth.go:456-597`)
2. Returns JWT + refresh cookie; user auto-provisioned with isolated tenant context

**State Management (Frontend):**
- JWT + user profile: `localStorage` + React state in `AuthContext`
- Active company: `localStorage` (with `pref_company_<userId>` key for persistence across logout) + React state
- Selected branches (filiais): `localStorage` (keyed by `companyId`) + React state in `FilialContext`
- Server data: TanStack Query (`@tanstack/react-query`) — `queryClient` defined globally in `App.tsx`

## Key Abstractions

**Handler Factory:**
- Purpose: Dependency injection of `*sql.DB` at route registration time
- Examples: `backend/handlers/nfe_entradas.go`, `backend/handlers/auth.go`
- Pattern: `func SomethingHandler(db *sql.DB) http.HandlerFunc { return func(w, r) { ... } }`

**withDB / withAuth Closures (main.go):**
- Purpose: Inline wrappers that guard routes with DB availability and auth checks
- Examples: `backend/main.go:276-298` — `withDB` and `withAuth` defined as local closures in `main()`
- Pattern: `http.HandleFunc("/api/...", withAuth(handlers.SomeHandler, "admin"))`

**GetEffectiveCompanyID:**
- Purpose: Resolves which `company_id` to use for the current request (header override → owner fallback → member fallback)
- Location: `backend/handlers/auth.go:323`
- Used by: Every handler that queries company-scoped data

**TenantContext / WithTenant:**
- Purpose: Sets PostgreSQL session variables `app.tenant_id` and `app.user_id` for RLS policy enforcement within a transaction
- Location: `backend/middleware/tenant.go`
- Note: Defined but not yet widely applied in handlers — most handlers still use explicit `WHERE company_id = $1` filtering

**ModuleConfig (frontend navigation):**
- Purpose: Single source of truth for sidebar modules and their tab routes
- Location: `frontend/src/lib/navigation.ts`
- Pattern: `modules` record maps module IDs to labels + tab arrays; `getActiveModule(pathname)` resolves current module from URL

## Entry Points

**Go Backend:**
- Location: `backend/main.go`
- Triggers: `go run main.go` / Docker container start
- Responsibilities: Load `.env`, validate JWT secret, start DB connection (async), run migrations on connect, register all HTTP routes, launch RFB scheduler goroutine, serve frontend static files from `./static/`

**React Frontend:**
- Location: `frontend/src/main.tsx`
- Triggers: Browser load / Vite dev server
- Responsibilities: Mount `<App />` into DOM

**ERP Bridge Daemon:**
- Location: `erp-bridge-aws/bridge.py`
- Triggers: `systemctl start erp-bridge` (via `erp-bridge-aws/erp-bridge.service`) or CLI invocation
- Responsibilities: Parse args, connect Oracle ERP, extract fiscal documents, push to API, manage local SQLite tracker

## Architectural Constraints

- **Threading:** Go standard library HTTP server — concurrent goroutines per request; DB pool at 50 open / 15 idle connections; rate limiters use `sync.Mutex`; refresh token store and token blacklist use `sync.Map`
- **Global state:** `db *sql.DB` and `dbMutex sync.RWMutex` are package-level globals in `main.go`; `refreshTokenStore` and `tokenBlacklist` are `sync.Map` globals in `backend/handlers/auth.go:68-71`; `rfbTokenCache` and `rfbRateLimitCache` are module-level globals in `backend/services/rfb.go`
- **Migration auto-run:** All `*.sql` files in `backend/migrations/` run sequentially at startup via `onDBConnected()`; `.disabled` suffix skips a file; order is alphabetical/numeric — gaps in numbering are intentional (e.g., skips 003, 004, 011)
- **Fetch monkey-patching:** `AuthContext` replaces `window.fetch` globally — all API calls from the frontend go through the interceptor regardless of how they're made
- **Circular imports:** None detected — `handlers` imports `services`; `services` has no dependency on `handlers`; `middleware` has no dependency on `handlers` or `services`

## Anti-Patterns

### withDB/withAuth defined as closures in main()

**What happens:** `withDB` and `withAuth` are local closures inside `main()` in `backend/main.go:276-298`, repeated inline for some multi-method routes
**Why it's wrong:** DB-nil guard logic is duplicated in 6+ inline `http.HandleFunc` blocks throughout main; pattern is inconsistent
**Do this instead:** Move `withDB` and `withAuth` to `backend/handlers/middleware.go` as exported helpers, as `DBMiddleware` already exists there at line 204

### Refresh tokens stored in-memory only

**What happens:** `refreshTokenStore sync.Map` in `backend/handlers/auth.go:68` holds all active refresh tokens in process memory
**Why it's wrong:** All sessions are invalidated on server restart / new container deployment; incompatible with horizontal scaling
**Do this instead:** Persist refresh tokens to the `users` or a dedicated `refresh_tokens` table (already done for verification tokens — see `verification_tokens` table in migrations)

### TenantContext not applied to handlers

**What happens:** `backend/middleware/tenant.go` defines `WithTenant` and `TenantContext` but handlers use explicit `WHERE company_id = $1` queries instead
**Why it's wrong:** RLS policies cannot enforce isolation automatically; each handler must manually call `GetEffectiveCompanyID` and pass the result to every query
**Do this instead:** Wire `TenantContext` into `AuthMiddleware` and use `WithTenant` in handlers so PostgreSQL RLS policies can filter automatically

## Error Handling

**Strategy:** Handlers return HTTP error status codes with JSON body `{"error": "message"}` via the `jsonErr` helper defined in `backend/handlers/config.go:9-19`. Some older handlers use `http.Error` (plain text). Errors are logged with `log.Printf` including context (userID, companyID, operation).

**Patterns:**
- `sql.ErrNoRows` is handled explicitly and returns 404 or a business-specific message
- Service errors (RFB API, email) are logged and a 500 is returned — never swallow silently
- DB transaction rollback via `defer tx.Rollback()` in all mutation handlers
- Rate limit errors return `http.StatusTooManyRequests` (429)

## Cross-Cutting Concerns

**Logging:** `log.Printf` and `log.Println` from stdlib — prefixed with `[Module]` convention (e.g., `[Login]`, `[RFB Scheduler]`, `[ERP Bridge]`); no structured logger
**Validation:** Input validation is per-handler, inline; no shared validation layer; password minimum 8 chars enforced in `ResetPasswordHandler` and `ChangePasswordHandler`
**Authentication:** JWT HS256 + `bcrypt` cost 14; token expiry 30 min; refresh cookie `HttpOnly`, `SameSite=Strict`, `Secure` when behind HTTPS proxy; rate limiting on login (5/15min), register (10/hr), forgot-password (3/hr)

---

*Architecture analysis: 2026-05-12*
