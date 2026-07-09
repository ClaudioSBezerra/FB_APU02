# Story 1.1: Criar struct TenantContext e helpers

Status: done

## Story

As a developer,
I want a TenantContext struct propagated via Go context,
so that all handlers can access tenant information without manual resolution.

## Acceptance Criteria

1. **Given** o arquivo `backend/middleware/tenant.go` é criado no package `middleware`  
   **When** a struct `TenantContext` é definida  
   **Then** contém exatamente os campos: `UserID`, `TenantID`, `CompanyID`, `Role`, `TenantRole` (todos `string`)

2. **Given** o arquivo existe  
   **When** o package é compilado  
   **Then** a chave de contexto `TenantKey` é exportada (tipo inexportado `tenantContextKey` como chave segura)

3. **Given** um `context.Context` com `TenantContext` armazenado  
   **When** `GetTenantContext(ctx)` é chamado  
   **Then** retorna o `TenantContext` preenchido

4. **Given** um `context.Context` sem `TenantContext`  
   **When** `GetTenantContext(ctx)` é chamado  
   **Then** retorna `TenantContext{}` zero-value (sem panic)

5. **Given** `WithTenant(db, ctx, fn)` é chamado com `fn` que executa com sucesso  
   **When** a função `fn` recebe a `*sql.Tx`  
   **Then** a sessão PostgreSQL tem `app.tenant_id` e `app.user_id` configurados via `set_config`  
   **And** o commit é realizado ao final

6. **Given** `WithTenant(db, ctx, fn)` é chamado e `fn` retorna erro  
   **When** o erro ocorre  
   **Then** o rollback é executado e o erro é propagado

7. **Given** o package `middleware` novo  
   **When** `go build ./...` é executado na raiz de `backend/`  
   **Then** compila sem erros e sem conflicts com o package `handlers`

## Tasks / Subtasks

- [ ] Task 1: Criar package e arquivo `backend/middleware/tenant.go` (AC: 1, 2, 3, 4)
  - [ ] 1.1 Criar diretório `backend/middleware/`
  - [ ] 1.2 Definir `package middleware` e imports (`context`, `database/sql`, `fmt`)
  - [ ] 1.3 Definir tipo inexportado `tenantContextKey struct{}` e var exportada `TenantKey = tenantContextKey{}`
  - [ ] 1.4 Definir struct `TenantContext` com campos: `UserID`, `TenantID`, `CompanyID`, `Role`, `TenantRole` (todos `string`)
  - [ ] 1.5 Implementar `GetTenantContext(ctx context.Context) TenantContext`

- [ ] Task 2: Implementar `WithTenant` (AC: 5, 6)
  - [ ] 2.1 Assinar: `func WithTenant(db *sql.DB, ctx context.Context, fn func(tx *sql.Tx) error) error`
  - [ ] 2.2 Chamar `GetTenantContext(ctx)` para obter `tc`
  - [ ] 2.3 Abrir transação com `db.BeginTx(ctx, nil)`
  - [ ] 2.4 Executar `set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true)` via `tx.ExecContext`
  - [ ] 2.5 Chamar `fn(tx)`, retornar erro se houver (defer rollback garante limpeza)
  - [ ] 2.6 Chamar `tx.Commit()` se `fn` teve sucesso

- [ ] Task 3: Escrever testes unitários em `backend/middleware/tenant_test.go` (AC: 3, 4, 5, 6)
  - [ ] 3.1 `TestGetTenantContext_WithValue` — ctx com TenantContext → retorna correto
  - [ ] 3.2 `TestGetTenantContext_Empty` — ctx sem valor → retorna zero-value, sem panic
  - [ ] 3.3 `TestWithTenant_Success` — mock DB/tx via `database/sql/driver` → commit chamado
  - [ ] 3.4 `TestWithTenant_FnError` — fn retorna erro → rollback, erro propagado

- [ ] Task 4: Validar build e testes (AC: 7)
  - [ ] 4.1 Executar `go build ./...` no diretório `backend/` — zero erros
  - [ ] 4.2 Executar `go test ./middleware/...` — todos os testes passam

## Dev Notes

### Guardrails Críticos

**CONFLITO DE NOME A EVITAR:**  
O arquivo `backend/handlers/auth.go` já define `type contextKey string` e `const ClaimsKey contextKey = "claims"`. O novo package `middleware` usa um tipo DIFERENTE (`tenantContextKey struct{}`), evitando colisão. NUNCA reutilize `contextKey` de `handlers` no novo package.

**Localização exata:**  
- Arquivo novo: `backend/middleware/tenant.go` (package `middleware`)  
- Arquivo de testes: `backend/middleware/tenant_test.go` (package `middleware`)  
- NÃO adicionar nada em `backend/handlers/middleware.go` (aquele é CORS/rate-limiter)

**`set_config` vs `SET LOCAL`:**  
Use `set_config('app.tenant_id', $1, true)` parametrizado — evita SQL injection. O parâmetro booleano `true` indica local-to-transaction (equivalente a `SET LOCAL`). Nunca use interpolação de string.

**Estrutura do `TenantKey`:**  
Use tipo struct vazio inexportado (`tenantContextKey struct{}`) como chave de contexto — padrão idiomático Go que evita colisões com outros packages que também usam string como chave.

**Módulo Go:** `fb_apu02` (ver `backend/go.mod`)  
**Go version:** 1.22  
**Database driver:** `github.com/lib/pq` (PostgreSQL)  
**JWT lib:** `github.com/golang-jwt/jwt/v5` (não necessário nesta story)

### Código de Referência da Arquitetura (ADR-004 + ADR-005)

```go
// backend/middleware/tenant.go
package middleware

import (
    "context"
    "database/sql"
    "fmt"
)

type TenantContext struct {
    UserID     string
    TenantID   string
    CompanyID  string
    Role       string
    TenantRole string
}

type tenantContextKey struct{}
var TenantKey = tenantContextKey{}

func GetTenantContext(ctx context.Context) TenantContext {
    tc, _ := ctx.Value(TenantKey).(TenantContext)
    return tc
}

func WithTenant(db *sql.DB, ctx context.Context, fn func(tx *sql.Tx) error) error {
    tc := GetTenantContext(ctx)

    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback()

    if _, err := tx.ExecContext(ctx,
        "SELECT set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true)",
        tc.TenantID, tc.UserID,
    ); err != nil {
        return fmt.Errorf("set tenant session: %w", err)
    }

    if err := fn(tx); err != nil {
        return err
    }

    return tx.Commit()
}
```

### Estrutura de Arquivos que Serão Tocados

| Arquivo | Ação | Nota |
|---------|------|------|
| `backend/middleware/tenant.go` | CRIAR (novo) | Package `middleware`, package path: `fb_apu02/middleware` |
| `backend/middleware/tenant_test.go` | CRIAR (novo) | Testes unitários |

**Nenhum outro arquivo deve ser modificado nesta story.** A integração com `main.go` é feita na Story 1.4.

### Testes — Abordagem para `WithTenant`

Para testar `WithTenant` sem um PostgreSQL real, use `github.com/DATA-DOG/go-sqlmock` se disponível, ou crie um helper que abre transação real em um DB in-memory. Como o projeto usa `lib/pq` (PostgreSQL real), o mais simples é testar `GetTenantContext` diretamente (não requer DB) e criar um teste de integração condicional para `WithTenant` que pula se `TEST_DB_URL` não estiver setado:

```go
func TestWithTenant_Integration(t *testing.T) {
    dsn := os.Getenv("TEST_DB_URL")
    if dsn == "" {
        t.Skip("TEST_DB_URL not set")
    }
    // ... teste real
}
```

### Contexto do Projeto

**Fase:** FASE 2 — Fundação Multi-Tenant (ver `_bmad-output/planning-artifacts/architecture-multi-tenancy.md`)  
**Esta story é a fundação** — as Stories 1.2 a 1.5 dependem de `TenantContext` e `WithTenant` estarem corretos.  
**Impacto atual:** Zero — nenhum handler existente é tocado. Package novo, isolado.  
**Rollback:** Deletar `backend/middleware/` — sem efeito colateral no código existente.

### References

- ADR-004: TenantContext struct definition — [Source: `_bmad-output/planning-artifacts/architecture-multi-tenancy.md#ADR-004`]
- ADR-005: WithTenant wrapper — [Source: `_bmad-output/planning-artifacts/architecture-multi-tenancy.md#ADR-005`]
- Contexto existente de contextKey — [Source: `backend/handlers/auth.go:22-24`]
- Middleware existente (CORS/rate-limiter, NÃO tocar) — [Source: `backend/handlers/middleware.go`]
- Module name — [Source: `backend/go.mod:3`]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

Story criada pelo workflow create-story em 2026-05-06. Análise completa de arquitetura e código existente realizada.

### File List
