<!-- GSD:project-start source:PROJECT.md -->
## Project

**FB_APU02 — Sistema de Apuração Fiscal**

Plataforma web de apuração fiscal tributária para o grupo Ferreira Costa. Processa arquivos SPED, NF-e e CT-e, apura créditos e débitos de PIS/COFINS, ICMS, IBS e CBS, integra com RFB e CGIBS, e oferece malha fina de divergências. Multi-tenant com hierarquia Ambiente → Grupo de Empresas → Empresa. Backend Go + Frontend React + PostgreSQL + ERP Bridge Python na AWS.

**Core Value:** Apuração fiscal correta e confiável por empresa, sem vazamento de dados entre tenants.

### Constraints

- **Stack:** Go + React/TypeScript + PostgreSQL — sem mudança de tecnologia
- **Backward compatibility:** Migrations existentes não podem ser alteradas, apenas adicionadas
- **Deploy:** Alterações de segurança urgentes (SEC-01, SEC-02) devem ser deployadas assim que prontas
- **Sem novas features:** Este milestone é exclusivamente de estabilização
<!-- GSD:project-end -->

<!-- GSD:stack-start source:codebase/STACK.md -->
## Technology Stack

## Languages
- Go 1.22 - Backend API server (`backend/`)
- TypeScript 5.2 - Frontend SPA (`frontend/src/`)
- Python 3 (zoneinfo, oracledb) - ERP Bridge daemon (`erp-bridge-aws/bridge.py`)
- SQL - Database migrations (`backend/migrations/*.sql`, 104+ files)
## Runtime
- Go: compiled binary, runs in Alpine Linux container
- Node.js: build-time only (Vite bundler), not present at runtime
- Python 3: runtime on AWS server for ERP bridge daemon
- Go: Go modules with vendored dependencies (`backend/vendor/`, `backend/go.mod`)
- Frontend: npm with lockfile (`frontend/package-lock.json`)
- Python: pip (requirements via inline comment: `pip install oracledb requests pyyaml`)
- `frontend/package-lock.json` — present
- `backend/go.sum` — present
## Frameworks
- Go standard library `net/http` — HTTP server (no external router framework)
- `database/sql` — Database access layer
- React 18.3.1 — UI framework
- React Router DOM 6.22.3 — Client-side routing
- TanStack React Query 5.90 — Server state management
- React Hook Form 7.71 + Zod 4.3.6 — Form validation
- Radix UI (full suite, ~25 packages) — Headless accessible components
- Tailwind CSS 3.4 + tailwindcss-animate — Styling
- shadcn/ui pattern (CVA + Radix + Tailwind)
- Lucide React 0.363 — Icons
- Recharts 3.7 — Data visualization charts
- Sonner 2.0 — Toast notifications
- Vite 5.2 with `@vitejs/plugin-react-swc` — Frontend bundler (SWC compiler)
- PostCSS + Autoprefixer — CSS processing
- Docker multi-stage builds — Container packaging
## Key Dependencies
- `github.com/golang-jwt/jwt/v5` v5.3.1 — JWT authentication (access + refresh tokens)
- `github.com/lib/pq` v1.11.2 — PostgreSQL driver
- `golang.org/x/crypto` v0.17.0 — bcrypt password hashing
- `golang.org/x/text` v0.14.0 — Unicode text normalization
- `github.com/joho/godotenv` v1.5.1 — `.env` file loading in development
- `react-router-dom` ^6.22.3 — SPA routing
- `@tanstack/react-query` ^5.90.20 — API data fetching and caching
- `zod` ^4.3.6 — Schema validation
- `xlsx` ^0.18.5 — Excel export functionality
- `date-fns` ^4.1.0 — Date formatting and manipulation
- `oracledb` — Oracle Database client (thin mode)
- `requests` — HTTP client for FBTax API calls
- `pyyaml` — YAML config parsing
- `sqlite3` (stdlib) — Local tracker database
## Configuration
- Backend reads from environment variables via `os.Getenv()`
- Development: `.env` file loaded via `godotenv`
- Production: injected by Coolify/Docker Compose environment blocks
- Key backend vars: `DATABASE_URL`, `JWT_SECRET`, `ENCRYPTION_KEY`, `SMTP_*`, `RFB_*`, `APP_URL`, `ALLOWED_ORIGINS`, `PORT`
- `frontend/vite.config.ts` — Vite dev server (port 3000) + API proxy to `http://localhost:8081`
- `frontend/tsconfig.app.json` — TypeScript strict mode, `@/*` path alias → `./src/*`
- `frontend/tailwind.config.js` — Tailwind configuration
- `backend/Dockerfile` — Multi-stage: `golang:1.22-alpine` builder → `alpine:latest` runner
- Container timezone set to `America/Sao_Paulo` via `tzdata` package in Dockerfile
## Platform Requirements
- Go 1.22+
- Node.js (for frontend build)
- PostgreSQL 15
- Redis (configured but not imported in Go source — present in docker-compose only)
- Docker + Docker Compose
- Docker with Docker Compose
- Coolify (PaaS orchestrator, external `coolify` Docker network)
- Traefik (reverse proxy, TLS via Let's Encrypt)
- Domains: `apuracao.fbtax.cloud`, `fctax.fcxlabs.com`
- Timezone: America/Sao_Paulo
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

## Naming Patterns
- Snake_case for handler files grouped by domain: `rfb_apuracao.go`, `nfe_saidas.go`, `erp_bridge_batch.go`
- Single-word files for cross-cutting concerns: `auth.go`, `config.go`, `middleware.go`, `tenant.go`
- Test files co-located with source: `tenant_test.go` alongside `tenant.go`
- PascalCase for page and component files: `RFBCredentials.tsx`, `ConsultaNFesEntradas.tsx`, `MalhaFinaPanel.tsx`
- camelCase for utility modules: `utils.ts`, `formatFilial.ts`, `navigation.ts`, `logger.ts`
- `use-` prefix + kebab-case for hook files: `use-mobile.tsx`
- PascalCase for exported handlers following the pattern `{Domain}Handler`: `SolicitarApuracaoHandler`, `NfeSaidasListHandler`, `GetRFBCredentialHandler`
- PascalCase for exported utilities: `GetUserIDFromContext`, `CheckPasswordHash`, `GetEffectiveCompanyID`
- camelCase for unexported helpers: `jsonErr`, `getJWTSecret`, `getDB`, `newRateLimiter`
- Handler factories always return `http.HandlerFunc`: `func XyzHandler(db *sql.DB) http.HandlerFunc { return func(w, r) { ... } }`
- camelCase for all functions: `formatCNPJ`, `formatDocumento`, `getActiveModule`, `fetchCredential`
- PascalCase for React components: `export default function RFBCredentials()`, `export default function Painel()`
- camelCase: `companyID`, `userID`, `envName`, `groupName`
- Acronyms uppercase in names: `CNPJBase`, `DBError`, `APIKey`, `userID`
- camelCase: `companyId`, `setCredential`, `isLoading`, `errorMsg`
- Note: Go backend uses `company_id` (snake_case JSON) while frontend state uses `companyId` (camelCase)
- PascalCase structs with JSON tags in snake_case: `type RFBRequest struct { CompanyID string \`json:"company_id"\` }`
- Context keys as unexported named types: `type contextKey string`, `type tenantContextKey struct{}`
- PascalCase interfaces declared inline in the file where used: `interface RFBCredential { ... }`
- Export interfaces that are reused across files: `export interface ModuleTab`, `export interface ModuleConfig`
- PascalCase for exported: `BackendVersion`, `FeatureSet`, `ClaimsKey`
- ALL_CAPS not used; PascalCase is standard throughout
- Unexported named types prevent key collisions across packages:
## Code Style
- Standard `gofmt` formatting assumed (no explicit config found)
- Imports grouped: stdlib → internal packages → external packages, with blank line separators
- Comments for non-obvious logic; section separators use `// ─── Section Name ───` style
- No `.prettierrc` or `.eslintrc` found at project level — formatting is informal
- TypeScript strict mode enabled (`tsconfig.app.json`: `"strict": true`, `"noUnusedLocals": true`, `"noUnusedParameters": true`)
- `target: ES2020`, `module: ESNext`, `moduleResolution: bundler`
- Go: No `golangci-lint` config found; no lint step in CI workflows
- TypeScript: ESLint available (`npm run lint` in `package.json`) but no project-level config found
- TypeScript compiler acts as primary linter via strict mode
## Import Organization
- Separated by blank lines between groups
- No barrel `index.ts` files detected; each module imported directly
- `@/*` maps to `./src/*` (configured in `tsconfig.app.json` and `vite.config.ts`)
- Example: `import { cn } from '@/lib/utils'`
## Error Handling
- Early return pattern: check error → write HTTP response → return immediately
- Two response styles coexist:
- Definition: `backend/handlers/config.go:9`
- Database `sql.ErrNoRows` is checked explicitly before generic `err != nil`
- Transactions use `defer tx.Rollback()` immediately after `db.Begin()`, then `tx.Commit()` on success path
- `fmt.Errorf("begin tx: %w", err)` pattern used in `middleware/tenant.go` for wrappable errors
- `log.Printf("[HandlerName] description: %v", err)` before responding to client
- `try/catch` in async event handlers; `catch (error: any)` with `error.message` extraction
- `toast.error(msg)` + local state `setErrorMsg(msg)` for user-facing errors
- API responses checked via `if (!res.ok) { throw new Error(...) }`
## Logging
- Framework: standard `log` package (`log.Printf`, `log.Println`, `log.Fatal`)
- Pattern: `log.Printf("[ComponentName] Description: %v", err)`
- `fmt.Printf` / `fmt.Println` for startup messages and progress (non-error informational output)
- No structured logging library (no `zerolog`, `zap`, or `slog`)
- Custom `Logger` class defined in `frontend/src/lib/logger.ts`
- API: `logger.info(msg, data?, context?)`, `logger.error(msg, data?, context?)`, etc.
- In development: `console.groupCollapsed` with colorized level prefix
- In production: `JSON.stringify(entry)` to `console.error/warn/log`
- In practice: many pages use raw `console.error(error)` directly rather than `logger`; adoption of `logger` is inconsistent
## Comments
- Doc comments on exported functions and types: `// GetTenantContext retrieves the TenantContext...`
- Inline comments explain non-obvious decisions: `// Check blacklist before validating signature`
- Section dividers with `// ─── Section Name ───────` style for visual grouping within files
- Deprecated markers: `// Deprecated: Use GetEffectiveCompanyID instead`
- JSDoc-style block comments on utility functions in `formatFilial.ts`:
- Inline comments in Portuguese for business-logic sections in page components
- No JSDoc on React component props or hook return types
## Function Design
- All handlers follow the closure factory pattern:
- `db *sql.DB` is always the first (and usually only) parameter of the factory
- Small, focused utilities: `getEnv`, `getJWTSecret`, `isSecureCookie`, `GetClientIP`
- Context extraction helpers: `GetUserIDFromContext(r *http.Request) string`
- Single default export per file (page components)
- Local state and fetch logic contained within the component function (no custom hooks for page data)
- Interfaces declared at top of file, before the component function
- `backend/handlers/auth.go` is 1139 lines — largest Go source file; combines multiple handler responsibilities
- Most other handler files are 80–400 lines
## Module Design
- Package `handlers` contains all HTTP handler factories — no sub-packages
- Package `services` contains background services: `rfb_processor.go`, `rfb_scheduler.go`, `email.go`
- Package `middleware` contains context middleware: `tenant.go`
- No barrel files; each handler is imported directly by `main.go`
- Pages: `export default function PageName()` (no named exports)
- Utilities: named exports only: `export function formatCNPJ(...)`, `export const modules = ...`
- Contexts: named exports for provider + hook: `export const AuthProvider`, `export const useAuth`
- No barrel `index.ts` files; each module imported by explicit path
## API Contract Conventions
- Always `json.NewDecoder(r.Body).Decode(&req)` for JSON bodies
- Company scoping always via `GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))`
- Always set `w.Header().Set("Content-Type", "application/json")` before encoding
- Success: `json.NewEncoder(w).Encode(data)` — no explicit status 200 (implicit)
- Error: `jsonErr(w, status, msg)` for new handlers; `http.Error(w, msg, status)` in auth handlers
- Global `window.fetch` is monkey-patched in `AuthContext.tsx` to inject `Authorization: Bearer {token}` and `X-Company-ID` headers automatically on every API call
- Individual components do NOT manually add auth headers unless still using pre-interceptor pattern
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

## System Overview
```text
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
- Multi-tenant via `company_id` column on every data table; tenant identity resolved per-request using JWT claims + `X-Company-ID` header via `GetEffectiveCompanyID()`
- No ORM — all database queries are raw SQL via `database/sql` + `lib/pq`
- Handler factory pattern: handlers are functions returning `http.HandlerFunc`, receiving `*sql.DB` as argument — `func SomeHandler(db *sql.DB) http.HandlerFunc`
- Authentication: short-lived JWT (30 min) in `Authorization: Bearer` header + long-lived refresh token in `HttpOnly` cookie (7 days), rotated on use
- Frontend uses a global `window.fetch` interceptor (monkey-patched in `AuthContext`) to automatically inject `Authorization` and `X-Company-ID` headers on every API call
## Layers
- Purpose: React SPA — routing, UI rendering, user interactions
- Location: `frontend/src/`
- Contains: Pages, shared components, context providers, utility libs
- Depends on: Go REST API via fetch
- Used by: End users via browser
- Purpose: Route requests, enforce auth/authz, parse input, return JSON responses
- Location: `backend/handlers/`
- Contains: One `.go` file per feature domain (e.g., `nfe_entradas.go`, `rfb_apuracao.go`)
- Depends on: `*sql.DB` passed at registration time from `main.go`
- Used by: `main.go` route registration
- Purpose: External API integration (Receita Federal CBS API), email, background scheduling
- Location: `backend/services/`
- Contains: `rfb.go` (HTTP client + OAuth2 token cache), `rfb_scheduler.go` (cron-style ticker), `rfb_processor.go`, `rfb_creditos_processor.go`, `email.go`
- Depends on: `*sql.DB`, environment variables
- Used by: Handlers (direct calls) and `main.go` (goroutine: `go services.StartRFBScheduler(getDB)`)
- Purpose: Persistent state — fiscal documents, users, credentials, apuração results
- Location: `backend/migrations/` (104 `.sql` files applied sequentially at startup)
- Contains: Tables for `nfe_entradas`, `nfe_saidas`, `cte_entradas`, `rfb_requests`, `cgibs_requests`, `erp_bridge_*`, `users`, `companies`, `environments`, `enterprise_groups`, `user_environments`, `rfb_credentials`, materialized view `mv_mercadorias`
- Depends on: PostgreSQL 15
- Purpose: Pull fiscal documents from Oracle ERP and push to API
- Location: `erp-bridge-aws/bridge.py`
- Contains: Oracle DB queries, XML multipart upload (oracle_xml mode) or JSON batch (sap_s4hana mode), local SQLite tracker, daemon loop
- Depends on: `oracledb`, `requests`, `pyyaml`; calls `POST /api/erp-bridge/import/batch` and `POST /api/erp-bridge/parceiros/sync`
## Data Flow
### Primary Request Path (authenticated API call)
### ERP Bridge Import Path
### RFB Scheduler Path
### User Registration Path
- JWT + user profile: `localStorage` + React state in `AuthContext`
- Active company: `localStorage` (with `pref_company_<userId>` key for persistence across logout) + React state
- Selected branches (filiais): `localStorage` (keyed by `companyId`) + React state in `FilialContext`
- Server data: TanStack Query (`@tanstack/react-query`) — `queryClient` defined globally in `App.tsx`
## Key Abstractions
- Purpose: Dependency injection of `*sql.DB` at route registration time
- Examples: `backend/handlers/nfe_entradas.go`, `backend/handlers/auth.go`
- Pattern: `func SomethingHandler(db *sql.DB) http.HandlerFunc { return func(w, r) { ... } }`
- Purpose: Inline wrappers that guard routes with DB availability and auth checks
- Examples: `backend/main.go:276-298` — `withDB` and `withAuth` defined as local closures in `main()`
- Pattern: `http.HandleFunc("/api/...", withAuth(handlers.SomeHandler, "admin"))`
- Purpose: Resolves which `company_id` to use for the current request (header override → owner fallback → member fallback)
- Location: `backend/handlers/auth.go:323`
- Used by: Every handler that queries company-scoped data
- Purpose: Sets PostgreSQL session variables `app.tenant_id` and `app.user_id` for RLS policy enforcement within a transaction
- Location: `backend/middleware/tenant.go`
- Note: Defined but not yet widely applied in handlers — most handlers still use explicit `WHERE company_id = $1` filtering
- Purpose: Single source of truth for sidebar modules and their tab routes
- Location: `frontend/src/lib/navigation.ts`
- Pattern: `modules` record maps module IDs to labels + tab arrays; `getActiveModule(pathname)` resolves current module from URL
## Entry Points
- Location: `backend/main.go`
- Triggers: `go run main.go` / Docker container start
- Responsibilities: Load `.env`, validate JWT secret, start DB connection (async), run migrations on connect, register all HTTP routes, launch RFB scheduler goroutine, serve frontend static files from `./static/`
- Location: `frontend/src/main.tsx`
- Triggers: Browser load / Vite dev server
- Responsibilities: Mount `<App />` into DOM
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
### Refresh tokens stored in-memory only
### TenantContext not applied to handlers
## Error Handling
- `sql.ErrNoRows` is handled explicitly and returns 404 or a business-specific message
- Service errors (RFB API, email) are logged and a 500 is returned — never swallow silently
- DB transaction rollback via `defer tx.Rollback()` in all mutation handlers
- Rate limit errors return `http.StatusTooManyRequests` (429)
## Cross-Cutting Concerns
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->
## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->



<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
