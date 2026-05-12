# Coding Conventions

**Analysis Date:** 2026-05-12

## Naming Patterns

**Files (Go):**
- Snake_case for handler files grouped by domain: `rfb_apuracao.go`, `nfe_saidas.go`, `erp_bridge_batch.go`
- Single-word files for cross-cutting concerns: `auth.go`, `config.go`, `middleware.go`, `tenant.go`
- Test files co-located with source: `tenant_test.go` alongside `tenant.go`

**Files (TypeScript/React):**
- PascalCase for page and component files: `RFBCredentials.tsx`, `ConsultaNFesEntradas.tsx`, `MalhaFinaPanel.tsx`
- camelCase for utility modules: `utils.ts`, `formatFilial.ts`, `navigation.ts`, `logger.ts`
- `use-` prefix + kebab-case for hook files: `use-mobile.tsx`

**Functions (Go):**
- PascalCase for exported handlers following the pattern `{Domain}Handler`: `SolicitarApuracaoHandler`, `NfeSaidasListHandler`, `GetRFBCredentialHandler`
- PascalCase for exported utilities: `GetUserIDFromContext`, `CheckPasswordHash`, `GetEffectiveCompanyID`
- camelCase for unexported helpers: `jsonErr`, `getJWTSecret`, `getDB`, `newRateLimiter`
- Handler factories always return `http.HandlerFunc`: `func XyzHandler(db *sql.DB) http.HandlerFunc { return func(w, r) { ... } }`

**Functions (TypeScript):**
- camelCase for all functions: `formatCNPJ`, `formatDocumento`, `getActiveModule`, `fetchCredential`
- PascalCase for React components: `export default function RFBCredentials()`, `export default function Painel()`

**Variables (Go):**
- camelCase: `companyID`, `userID`, `envName`, `groupName`
- Acronyms uppercase in names: `CNPJBase`, `DBError`, `APIKey`, `userID`

**Variables (TypeScript):**
- camelCase: `companyId`, `setCredential`, `isLoading`, `errorMsg`
- Note: Go backend uses `company_id` (snake_case JSON) while frontend state uses `companyId` (camelCase)

**Types/Interfaces (Go):**
- PascalCase structs with JSON tags in snake_case: `type RFBRequest struct { CompanyID string \`json:"company_id"\` }`
- Context keys as unexported named types: `type contextKey string`, `type tenantContextKey struct{}`

**Types/Interfaces (TypeScript):**
- PascalCase interfaces declared inline in the file where used: `interface RFBCredential { ... }`
- Export interfaces that are reused across files: `export interface ModuleTab`, `export interface ModuleConfig`

**Constants (Go):**
- PascalCase for exported: `BackendVersion`, `FeatureSet`, `ClaimsKey`
- ALL_CAPS not used; PascalCase is standard throughout

**Context Keys (Go):**
- Unexported named types prevent key collisions across packages:
  - `handlers` package: `type contextKey string; const ClaimsKey contextKey = "claims"`
  - `middleware` package: `type tenantContextKey struct{}; var TenantKey = tenantContextKey{}`

## Code Style

**Formatting (Go):**
- Standard `gofmt` formatting assumed (no explicit config found)
- Imports grouped: stdlib → internal packages → external packages, with blank line separators
- Comments for non-obvious logic; section separators use `// ─── Section Name ───` style

**Formatting (TypeScript):**
- No `.prettierrc` or `.eslintrc` found at project level — formatting is informal
- TypeScript strict mode enabled (`tsconfig.app.json`: `"strict": true`, `"noUnusedLocals": true`, `"noUnusedParameters": true`)
- `target: ES2020`, `module: ESNext`, `moduleResolution: bundler`

**Linting:**
- Go: No `golangci-lint` config found; no lint step in CI workflows
- TypeScript: ESLint available (`npm run lint` in `package.json`) but no project-level config found
- TypeScript compiler acts as primary linter via strict mode

## Import Organization

**Go:**
1. Standard library packages (alphabetical)
2. Internal packages (`fb_apu02/handlers`, `fb_apu02/services`)
3. External/vendor packages (`github.com/golang-jwt/jwt/v5`, `golang.org/x/crypto`)
- Separated by blank lines between groups

**TypeScript:**
1. React and third-party libraries
2. `@/components/ui/*` (Radix UI wrappers via shadcn)
3. `@/components/*` (custom components)
4. `@/contexts/*`, `@/hooks/*`, `@/lib/*` (internal modules)
- No barrel `index.ts` files detected; each module imported directly

**Path Aliases (TypeScript):**
- `@/*` maps to `./src/*` (configured in `tsconfig.app.json` and `vite.config.ts`)
- Example: `import { cn } from '@/lib/utils'`

## Error Handling

**Go — HTTP Handlers:**
- Early return pattern: check error → write HTTP response → return immediately
- Two response styles coexist:
  1. `http.Error(w, msg, status)` — plain text; used in older handlers (`auth.go`)
  2. `jsonErr(w, status, msg)` — JSON `{"error": "..."}` body; preferred in newer handlers (`nfe_saidas.go`, `cte_entradas.go`, `user_activity.go`)
- Definition: `backend/handlers/config.go:9`
  ```go
  func jsonErr(w http.ResponseWriter, status int, msg string, extra ...map[string]string) {
      w.Header().Set("Content-Type", "application/json")
      w.WriteHeader(status)
      out := map[string]string{"error": msg}
      ...
      json.NewEncoder(w).Encode(out)
  }
  ```
- Database `sql.ErrNoRows` is checked explicitly before generic `err != nil`
- Transactions use `defer tx.Rollback()` immediately after `db.Begin()`, then `tx.Commit()` on success path

**Go — Wrapping errors:**
- `fmt.Errorf("begin tx: %w", err)` pattern used in `middleware/tenant.go` for wrappable errors
- `log.Printf("[HandlerName] description: %v", err)` before responding to client

**TypeScript — Async handlers:**
- `try/catch` in async event handlers; `catch (error: any)` with `error.message` extraction
- `toast.error(msg)` + local state `setErrorMsg(msg)` for user-facing errors
- API responses checked via `if (!res.ok) { throw new Error(...) }`

## Logging

**Backend (Go):**
- Framework: standard `log` package (`log.Printf`, `log.Println`, `log.Fatal`)
- Pattern: `log.Printf("[ComponentName] Description: %v", err)`
  - Examples: `[Login]`, `[Register]`, `[ResetPassword]`, `[PreferredCompany]`, `[ForgotPassword]`
- `fmt.Printf` / `fmt.Println` for startup messages and progress (non-error informational output)
- No structured logging library (no `zerolog`, `zap`, or `slog`)

**Frontend (TypeScript):**
- Custom `Logger` class defined in `frontend/src/lib/logger.ts`
- API: `logger.info(msg, data?, context?)`, `logger.error(msg, data?, context?)`, etc.
- In development: `console.groupCollapsed` with colorized level prefix
- In production: `JSON.stringify(entry)` to `console.error/warn/log`
- In practice: many pages use raw `console.error(error)` directly rather than `logger`; adoption of `logger` is inconsistent

## Comments

**Go:**
- Doc comments on exported functions and types: `// GetTenantContext retrieves the TenantContext...`
- Inline comments explain non-obvious decisions: `// Check blacklist before validating signature`
- Section dividers with `// ─── Section Name ───────` style for visual grouping within files
- Deprecated markers: `// Deprecated: Use GetEffectiveCompanyID instead`

**TypeScript:**
- JSDoc-style block comments on utility functions in `formatFilial.ts`:
  ```typescript
  /**
   * Formata CPF completo com pontuação
   * Entrada: "12345678901" → Saída: "123.456.789-01"
   */
  ```
- Inline comments in Portuguese for business-logic sections in page components
- No JSDoc on React component props or hook return types

## Function Design

**Go handler factories:**
- All handlers follow the closure factory pattern:
  ```go
  func XyzHandler(db *sql.DB) http.HandlerFunc {
      return func(w http.ResponseWriter, r *http.Request) {
          // handler body
      }
  }
  ```
- `db *sql.DB` is always the first (and usually only) parameter of the factory

**Go utility functions:**
- Small, focused utilities: `getEnv`, `getJWTSecret`, `isSecureCookie`, `GetClientIP`
- Context extraction helpers: `GetUserIDFromContext(r *http.Request) string`

**TypeScript components:**
- Single default export per file (page components)
- Local state and fetch logic contained within the component function (no custom hooks for page data)
- Interfaces declared at top of file, before the component function

**Size:**
- `backend/handlers/auth.go` is 1139 lines — largest Go source file; combines multiple handler responsibilities
- Most other handler files are 80–400 lines

## Module Design

**Go:**
- Package `handlers` contains all HTTP handler factories — no sub-packages
- Package `services` contains background services: `rfb_processor.go`, `rfb_scheduler.go`, `email.go`
- Package `middleware` contains context middleware: `tenant.go`
- No barrel files; each handler is imported directly by `main.go`

**TypeScript exports:**
- Pages: `export default function PageName()` (no named exports)
- Utilities: named exports only: `export function formatCNPJ(...)`, `export const modules = ...`
- Contexts: named exports for provider + hook: `export const AuthProvider`, `export const useAuth`
- No barrel `index.ts` files; each module imported by explicit path

## API Contract Conventions

**Request parsing (Go):**
- Always `json.NewDecoder(r.Body).Decode(&req)` for JSON bodies
- Company scoping always via `GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))`

**Response (Go):**
- Always set `w.Header().Set("Content-Type", "application/json")` before encoding
- Success: `json.NewEncoder(w).Encode(data)` — no explicit status 200 (implicit)
- Error: `jsonErr(w, status, msg)` for new handlers; `http.Error(w, msg, status)` in auth handlers

**Fetch (TypeScript):**
- Global `window.fetch` is monkey-patched in `AuthContext.tsx` to inject `Authorization: Bearer {token}` and `X-Company-ID` headers automatically on every API call
- Individual components do NOT manually add auth headers unless still using pre-interceptor pattern

---

*Convention analysis: 2026-05-12*
