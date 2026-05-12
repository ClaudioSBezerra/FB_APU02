---
plan: 03-05
status: complete
date: 2026-05-12
---

## Summary

Documented architectural decision about `backend/middleware/tenant.go` in `.planning/PROJECT.md`.

**Decision:** Defer `WithTenant` adoption to v2.

**Key reasons:**
- Stabilization milestone scope — no architectural refactors
- 40+ handlers would need to switch from `*sql.DB` to `*sql.Tx`
- BUG-01/02/03 already corrected the company ID issues (risk mitigated)
- `GetEffectiveCompanyID` provides explicit, testable tenant validation

**v2 priority handlers:** nfe_entradas.go, nfe_saidas.go, rfb_apuracao.go, cgibs_apuracao.go

## Verification

- `grep -c "Diferido para v2" .planning/PROJECT.md`: ≥1 ✓
