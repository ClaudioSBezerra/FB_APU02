package middleware

import (
	"context"
	"database/sql"
	"fmt"
)

// TenantContext carries the resolved tenant identity for every authenticated request.
// Propagated via context.Context; handlers retrieve it with GetTenantContext.
type TenantContext struct {
	UserID     string // UUID of the authenticated user
	TenantID   string // UUID of the environment (canonical tenant identifier)
	CompanyID  string // UUID of the active company selected for this request
	Role       string // Global role: "admin" | "user"
	TenantRole string // Role within the tenant: "admin" | "editor" | "viewer"
}

// tenantContextKey is an unexported type — prevents collision with any other
// package that uses a plain string as a context key.
type tenantContextKey struct{}

// TenantKey is the context key used to store and retrieve TenantContext values.
var TenantKey = tenantContextKey{}

// GetTenantContext retrieves the TenantContext stored in ctx.
// Returns a zero-value TenantContext if none is present (never panics).
func GetTenantContext(ctx context.Context) TenantContext {
	tc, _ := ctx.Value(TenantKey).(TenantContext)
	return tc
}

// WithTenant opens a PostgreSQL transaction, sets the session variables
// app.tenant_id and app.user_id so that RLS policies can filter automatically,
// then calls fn with the transaction. The transaction is committed on success
// and rolled back on any error (including panics, via defer).
//
// Uses set_config(..., true) — equivalent to SET LOCAL — with parameterised
// arguments to avoid SQL injection.
func WithTenant(db *sql.DB, ctx context.Context, fn func(tx *sql.Tx) error) error {
	tc := GetTenantContext(ctx)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck — rollback on any failure path

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
