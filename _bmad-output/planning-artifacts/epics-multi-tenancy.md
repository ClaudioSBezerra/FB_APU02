---
stepsCompleted: [1, 2]
inputDocuments:
  - _bmad-output/planning-artifacts/prd-multi-tenancy.md
  - _bmad-output/planning-artifacts/architecture-multi-tenancy.md
---

# FBTax Cloud Multi-Tenancy - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for the Multi-Tenancy implementation (FASE 2), decomposing the requirements from the PRD and Architecture into implementable stories.

## Requirements Inventory

### Functional Requirements

- FR-001: Tenant Context Automatico
- FR-002: Row-Level Security no PostgreSQL
- FR-003: Desnormalizacao do Tenant ID
- FR-004: Planos Comerciais por Tenant
- FR-005: Audit Log de Acesso

### NonFunctional Requirements

- NFR-001: Performance (RLS < 5ms p99)
- NFR-002: Seguranca (isolamento verificavel por testes)
- NFR-003: Compatibilidade (zero downtime, retrocompativel)
- NFR-004: Observabilidade (logs com tenant_id)

### FR Coverage Map

| Requisito | Epic 1 | Epic 2 | Epic 3 | Epic 4 | Epic 5 |
|-----------|--------|--------|--------|--------|--------|
| FR-001 | X | | | | |
| FR-002 | | X | | | |
| FR-003 | | X | | | |
| FR-004 | | | | X | |
| FR-005 | | | | | X |
| NFR-001 | | X | X | | |
| NFR-002 | | X | X | | X |
| NFR-003 | X | X | X | X | |
| NFR-004 | | | | | X |

## Epic List

1. **Epic 1:** Tenant Context e JWT Expandido
2. **Epic 2:** Row-Level Security e Desnormalizacao
3. **Epic 3:** Refatoracao de Handlers para TenantContext
4. **Epic 4:** Planos Comerciais por Tenant
5. **Epic 5:** Audit Log e Observabilidade

---

## Epic 1: Tenant Context e JWT Expandido

Estabelecer a fundacao do multi-tenancy criando o TenantContext struct, expandindo o JWT com tenant_id/company_id, e implementando o TenantMiddleware que resolve contexto automaticamente em cada requisicao.

### Story 1.1: Criar struct TenantContext e helpers

As a developer,
I want a TenantContext struct propagated via Go context,
So that all handlers can access tenant information without manual resolution.

**Acceptance Criteria:**

**Given** a new file `backend/middleware/tenant.go` is created
**When** the TenantContext struct is defined
**Then** it contains fields: UserID, TenantID, CompanyID, Role, TenantRole (all string)
**And** a context key `TenantKey` is exported
**And** a helper `GetTenantContext(ctx) TenantContext` is available
**And** a helper `WithTenant(db, ctx, func(tx) error) error` wraps transactions with SET LOCAL

**Technical notes:**

- `WithTenant` must use `set_config('app.tenant_id', $1, true)` inside a transaction
- Use `set_config` instead of `SET LOCAL` for parameterized query safety

---

### Story 1.2: Expandir JWT com tenant_id e company_id

As a user,
I want my JWT token to contain my tenant and company context,
So that the system can resolve my permissions without extra database queries.

**Acceptance Criteria:**

**Given** a user authenticates via POST /api/login
**When** the JWT token is generated
**Then** the token claims include `tenant_id` (environment UUID) and `company_id` (default company UUID)
**And** the token claims include `tenant_role` (role from user_environments)
**And** existing claims `user_id`, `role`, `exp` remain unchanged

**Given** a user registers via POST /api/register
**When** the auto-provisioned environment is created
**Then** the JWT returned includes the new environment's UUID as `tenant_id`

**Technical notes:**

- Modify `GenerateToken()` in `backend/handlers/auth.go`
- Modify `LoginHandler()` to query tenant_role from `user_environments`
- Modify `RegisterHandler()` to include tenant_id in response token

---

### Story 1.3: Implementar TenantMiddleware

As a platform,
I want every authenticated request to have its tenant context resolved automatically,
So that handlers do not need manual tenant resolution code.

**Acceptance Criteria:**

**Given** an authenticated request with a valid JWT containing `tenant_id`
**When** the request passes through TenantMiddleware
**Then** a TenantContext is stored in the request context
**And** the handler can retrieve it via `GetTenantContext(r.Context())`

**Given** a request with `X-Company-ID` header
**When** the TenantMiddleware processes it
**Then** it validates that the requested company belongs to the user's tenant
**And** overrides CompanyID in TenantContext if valid
**And** returns HTTP 403 if the company does not belong to the tenant

**Given** a JWT without `tenant_id` (legacy token)
**When** the TenantMiddleware processes it
**Then** it falls back to resolving tenant via database (GetEffectiveCompanyID pattern)
**And** logs a deprecation warning

**Technical notes:**

- Add middleware in `backend/middleware/tenant.go`
- Wire into `backend/main.go` chain: `withAuth -> withTenant -> handler`

---

### Story 1.4: Integrar TenantMiddleware em main.go

As a platform,
I want the TenantMiddleware integrated into the request chain,
So that all authenticated routes automatically receive tenant context.

**Acceptance Criteria:**

**Given** the existing route registration in `backend/main.go`
**When** TenantMiddleware is added to the chain
**Then** all routes wrapped with `withAuth()` also pass through `withTenant()`
**And** the `withDB()` wrapper continues to work (async DB init)
**And** admin-only routes (admin panel) bypass tenant filtering but still resolve context
**And** health check and public routes are unaffected

**Technical notes:**

- Create `withTenant(db)` wrapper similar to `withDB()` and `withAuth()`
- Pattern: `withDB(withAuth(withTenant(handler), "user"))`

---

### Story 1.5: Atualizar frontend AuthContext

As a frontend developer,
I want the AuthContext to store tenant_id from the new JWT,
So that the frontend can display tenant information and pass it in requests.

**Acceptance Criteria:**

**Given** the user logs in and receives a JWT with `tenant_id`
**When** the AuthContext processes the login response
**Then** `tenant_id` is stored alongside `token`, `user`, `companyId`
**And** the company selector uses tenant context to validate options
**And** logout clears tenant_id

**Technical notes:**

- Modify `frontend/src/contexts/AuthContext.tsx`
- JWT decode already happens client-side for user info display

---

## Epic 2: Row-Level Security e Desnormalizacao

Implementar defesa em profundidade no banco de dados com RLS policies e desnormalizar environment_id nas tabelas de dados para queries performaticas.

### Story 2.1: Criar migration de desnormalizacao

As a database,
I want `environment_id` as a direct column on data tables,
So that RLS policies can filter efficiently without JOINs.

**Acceptance Criteria:**

**Given** the tables `companies`, `import_jobs`, and fiscal data tables
**When** the migration runs
**Then** each table has a new `environment_id UUID` column
**And** existing data is backfilled from `enterprise_groups.environment_id`
**And** `companies.environment_id` has a NOT NULL constraint after backfill
**And** indices are created: `idx_{table}_environment_id`
**And** foreign key references `environments(id)`

**Given** a rollback is needed
**When** the DOWN migration runs
**Then** the `environment_id` columns are dropped
**And** no data is lost

**Technical notes:**

- Use `CREATE INDEX CONCURRENTLY` to avoid table locks
- Backfill companies first, then derive for import_jobs and fiscal tables

---

### Story 2.2: Criar trigger de auto-preenchimento

As a database,
I want environment_id to be automatically set on INSERT,
So that application code does not need to explicitly set it.

**Acceptance Criteria:**

**Given** a new company is inserted with `group_id` but without `environment_id`
**When** the trigger fires
**Then** `environment_id` is derived from `enterprise_groups.environment_id`

**Given** a new import_job is inserted with `company_id` but without `environment_id`
**When** the trigger fires
**Then** `environment_id` is derived from `companies.environment_id`

**Technical notes:**

- BEFORE INSERT triggers on `companies` and `import_jobs`
- Fiscal data tables derive from company_id chain

---

### Story 2.3: Criar usuario de aplicacao nao-superuser

As a security administrator,
I want the application to connect to PostgreSQL with a non-superuser role,
So that RLS policies are enforced for all application queries.

**Acceptance Criteria:**

**Given** a new PostgreSQL role `fbtax_app` is created
**When** the application connects using this role
**Then** the role has SELECT, INSERT, UPDATE, DELETE on all application tables
**And** the role does NOT have SUPERUSER or BYPASSRLS privileges
**And** RLS policies are enforced for this role
**And** migrations continue to run with the existing superuser role

**Technical notes:**

- Migration creates role and grants permissions
- Connection string in `.env` updated for app role
- Migration runner uses separate superuser connection

---

### Story 2.4: Habilitar RLS e criar policies

As a database,
I want Row-Level Security policies on all tenant data tables,
So that queries automatically filter by the current tenant session variable.

**Acceptance Criteria:**

**Given** RLS is enabled on `enterprise_groups`, `companies`, `import_jobs`, and fiscal data tables
**When** a query is executed with `app.tenant_id` session variable set
**Then** only rows matching the tenant are returned
**And** INSERT/UPDATE/DELETE are restricted to tenant rows

**Given** `app.tenant_id` is NOT set (NULL)
**When** a query is executed
**Then** zero rows are returned (fail-closed behavior)

**Given** the superuser role executes a query
**When** RLS is configured with `FORCE ROW LEVEL SECURITY`
**Then** even the table owner is subject to RLS

**Technical notes:**

- Use `current_setting('app.tenant_id', true)::uuid` (true = return NULL if missing)
- NULL tenant_id matches zero rows (safe default)
- Apply to: `enterprise_groups`, `companies`, `import_jobs`, `reg_c100`, `reg_c170`, `reg_0150`, `reg_0200`

---

### Story 2.5: Atualizar Materialized Views com environment_id

As a reporting system,
I want Materialized Views to include `environment_id`,
So that tenant-scoped reports can filter efficiently.

**Acceptance Criteria:**

**Given** the materialized views `mv_mercadorias_agregada` and `mv_operacoes_simples`
**When** they are recreated
**Then** they include `environment_id` from the companies table
**And** indices on `(environment_id, company_id)` are created
**And** REFRESH MATERIALIZED VIEW continues to work

**Technical notes:**

- MVs do not support RLS; filtering must be explicit in queries
- Modify the CREATE MATERIALIZED VIEW statements
- Update the REFRESH logic in the worker

---

### Story 2.6: Testes de isolamento cross-tenant

As a security team,
I want automated tests that verify tenant isolation,
So that regressions in data isolation are caught immediately.

**Acceptance Criteria:**

**Given** two tenants (A and B) with data in the database
**When** tenant A's session queries data
**Then** zero rows from tenant B are returned
**And** INSERT into tenant B's data fails or is rejected

**Given** no tenant session is set
**When** a query is executed
**Then** zero rows are returned

**Given** the test suite runs in CI
**When** any RLS bypass is detected
**Then** the build fails

**Technical notes:**

- Create `backend/middleware/tenant_test.go`
- Use test database with two environments pre-seeded
- Test each table individually

---

## Epic 3: Refatoracao de Handlers para TenantContext

Migrar gradualmente cada handler para usar TenantContext e WithTenant() em vez de resolucao manual.

### Story 3.1: Refatorar ReportHandler

As a user viewing reports,
I want the report handler to use automatic tenant isolation,
So that report data is securely scoped.

**Acceptance Criteria:**

**Given** a request to GET /api/report
**When** the handler processes it
**Then** it uses `GetTenantContext()` instead of manual `GetEffectiveCompanyID()`
**And** it uses `WithTenant()` to execute queries within a tenant-scoped transaction
**And** Materialized View queries include `WHERE environment_id = tc.TenantID`
**And** existing API response format is unchanged

---

### Story 3.2: Refatorar UploadHandler

As a user uploading SPED files,
I want the upload handler to use automatic tenant isolation,
So that uploaded files are correctly associated with my tenant.

**Acceptance Criteria:**

**Given** a request to POST /api/upload
**When** the handler processes it
**Then** it uses `GetTenantContext()` for company resolution
**And** `import_jobs` are created with `environment_id` from TenantContext
**And** the upload response format is unchanged

---

### Story 3.3: Refatorar JobHandler

As a user checking import job status,
I want the job handler to use automatic tenant isolation,
So that I only see my own jobs.

**Acceptance Criteria:**

**Given** a request to GET /api/jobs
**When** the handler processes it
**Then** it uses `WithTenant()` and RLS filters jobs automatically
**And** the manual ownership check (`SELECT EXISTS...`) can be simplified
**And** the response format is unchanged

---

### Story 3.4: Refatorar EnvironmentHandler

As a user managing environments,
I want the environment handler adapted for TenantContext,
So that environment CRUD respects the new middleware chain.

**Acceptance Criteria:**

**Given** a non-admin user requests environments
**When** the handler processes the request with TenantContext
**Then** only the user's tenant environment is returned (via RLS)
**And** admin users still see all environments (bypass or elevated role)

---

### Story 3.5: Refatorar AuthHandler (GetUserCompanies)

As a user,
I want the companies list to use tenant context,
So that company resolution is consistent with the new architecture.

**Acceptance Criteria:**

**Given** a request to GET /api/user/companies
**When** the handler processes it
**Then** it uses TenantContext to scope the query
**And** companies outside the user's tenant are never returned
**And** the response format is unchanged

---

## Epic 4: Planos Comerciais por Tenant

Implementar controle de features e limites por tenant baseado em plano contratado.

### Story 4.1: Criar migration de tenant_plans

As a database,
I want a tenant_plans table,
So that each tenant has a plan with defined limits and features.

**Acceptance Criteria:**

**Given** the migration runs
**When** the table is created
**Then** it has columns: id, environment_id (unique), plan_type, max_companies, max_users, max_nfes_month, features (JSONB), starts_at, expires_at
**And** existing tenants receive a default 'trial' plan
**And** a rollback drops the table

---

### Story 4.2: Criar middleware de verificacao de limites

As a platform,
I want creation operations to check tenant plan limits,
So that tenants cannot exceed their plan.

**Acceptance Criteria:**

**Given** a tenant with plan_type='starter' (max_companies=3)
**When** the tenant attempts to create a 4th company
**Then** the request returns HTTP 402 with message indicating plan limit
**And** the response includes current usage and plan limits

**Given** a tenant with plan_type='enterprise'
**When** the tenant creates resources
**Then** no limit is enforced (unlimited plan)

---

### Story 4.3: Criar API de consulta de plano

As a frontend,
I want an API to query the current tenant's plan,
So that the UI can display limits and usage.

**Acceptance Criteria:**

**Given** an authenticated request to GET /api/tenant/plan
**When** the handler processes it
**Then** the response includes: plan_type, limits, current_usage, features, expires_at
**And** current_usage shows: companies_count, users_count, nfes_this_month

---

### Story 4.4: Exibir plano no frontend

As a user,
I want to see my plan information in the UI,
So that I know my limits and can plan upgrades.

**Acceptance Criteria:**

**Given** the user is logged in
**When** the dashboard or settings page loads
**Then** a plan summary widget shows: plan name, usage bars, expiration date
**And** if usage is above 80%, a warning is displayed
**And** a link to upgrade is available (placeholder for future billing)

---

## Epic 5: Audit Log e Observabilidade

Implementar registro de auditoria e observabilidade por tenant para compliance e debugging.

### Story 5.1: Criar migration de audit_log

As a database,
I want an audit_log table,
So that all security-relevant events are recorded.

**Acceptance Criteria:**

**Given** the migration runs
**When** the table is created
**Then** it has columns: id (bigserial), tenant_id, user_id, action, resource_type, resource_id, details (JSONB), ip_address (INET), user_agent, created_at
**And** indices on (tenant_id, created_at) and (user_id, created_at)
**And** RLS is NOT applied to audit_log (admin-only table)

---

### Story 5.2: Integrar audit log no TenantMiddleware

As a security system,
I want access denied events automatically logged,
So that cross-tenant access attempts are tracked.

**Acceptance Criteria:**

**Given** a request where X-Company-ID does not belong to the user's tenant
**When** TenantMiddleware denies access (403)
**Then** an audit log entry is created with action='access_denied'
**And** details include: requested_company_id, user's tenant_id, IP address

---

### Story 5.3: Adicionar tenant_id aos logs estruturados

As an operations team,
I want all application logs to include tenant_id,
So that log analysis can be scoped by tenant.

**Acceptance Criteria:**

**Given** any log line emitted during a request
**When** the request has a TenantContext
**Then** the log includes `tenant_id` and `company_id` fields
**And** log format is consistent (JSON or structured)

---

### Story 5.4: Criar API de consulta de audit log (admin)

As a platform admin,
I want to query the audit log,
So that I can investigate security events.

**Acceptance Criteria:**

**Given** an admin request to GET /api/admin/audit-log
**When** filters are provided (tenant_id, user_id, action, date_range)
**Then** matching audit entries are returned paginated
**And** non-admin users receive HTTP 403
