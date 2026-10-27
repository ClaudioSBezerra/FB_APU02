package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ErrCompanyNotAuthorized é devolvido quando o usuário pede (via X-Company-ID) uma empresa
// que não é dele — sem fallback silencioso para a empresa padrão.
var ErrCompanyNotAuthorized = errors.New("company not authorized")

// ─── Acesso por empresa ───────────────────────────────────────────────────────

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

// companyAccess: o usuário é dono da empresa OU membro do ambiente dela (mesma regra de
// GetEffectiveCompanyID). Distingue "sem acesso" (false, nil) de falha de infraestrutura
// (false, err) — erro de banco não pode virar "empresa não autorizada", senão o frontend
// descarta a empresa do usuário por causa de um timeout.
func companyAccess(db *sql.DB, userID, companyID string) (bool, error) {
	// UUID inválido nunca chega ao Postgres (evita ruído de erro de cast no log)
	if !isUUID(userID) || !isUUID(companyID) {
		return false, nil
	}
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS(
			SELECT 1
			FROM companies c
			LEFT JOIN enterprise_groups eg ON c.group_id = eg.id
			LEFT JOIN user_environments ue ON eg.environment_id = ue.environment_id
			WHERE c.id = $1::uuid
			AND (c.owner_id = $2::uuid OR ue.user_id = $2::uuid)
		)
	`, companyID, userID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// userCanAccessCompany: versão booleana, falha fechada (erro de banco conta como "sem acesso").
func userCanAccessCompany(db *sql.DB, userID, companyID string) bool {
	ok, err := companyAccess(db, userID, companyID)
	return err == nil && ok
}

// CompanyScope valida o header X-Company-ID uma única vez para todas as rotas
// autenticadas: empresa que não é do usuário vira 403, em vez de cair no fallback.
// Usa 403 (e não 401) de propósito — 401 aciona o refresh/logout do interceptor do frontend.
func CompanyScope(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		companyID := r.Header.Get("X-Company-ID")
		if companyID != "" {
			ok, err := companyAccess(db, GetUserIDFromContext(r), companyID)
			if err != nil {
				log.Printf("[TenantGuard] erro ao validar X-Company-ID: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro ao validar empresa")
				return
			}
			if !ok {
				log.Printf("[TenantGuard] X-Company-ID negado user=%s company=%s ip=%s path=%s",
					GetUserIDFromContext(r), companyID, GetClientIP(r), r.URL.Path)
				jsonErr(w, http.StatusForbidden, "Empresa não autorizada para este usuário", map[string]string{"code": "company_not_authorized"})
				return
			}
		}
		next(w, r)
	}
}

// ─── Hierarquia Ambiente → Grupo → Empresa ───────────────────────────────────

func isPlatformAdmin(r *http.Request) bool {
	claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
	if !ok {
		return false
	}
	role, _ := claims["role"].(string)
	return role == "admin"
}

// userInEnvironment: o usuário tem vínculo com o ambiente.
func userInEnvironment(db *sql.DB, userID, envID string) bool {
	if !isUUID(userID) || !isUUID(envID) {
		return false
	}
	var exists bool
	err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM user_environments WHERE user_id = $1::uuid AND environment_id = $2::uuid)`,
		userID, envID,
	).Scan(&exists)
	return err == nil && exists
}

// userAdminInEnvironment: o usuário é admin do ambiente (user_environments.role = 'admin').
func userAdminInEnvironment(db *sql.DB, userID, envID string) bool {
	if !isUUID(userID) || !isUUID(envID) {
		return false
	}
	var exists bool
	err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM user_environments WHERE user_id = $1::uuid AND environment_id = $2::uuid AND role = 'admin')`,
		userID, envID,
	).Scan(&exists)
	return err == nil && exists
}

func environmentOfGroup(db *sql.DB, groupID string) (string, bool) {
	if !isUUID(groupID) {
		return "", false
	}
	var envID string
	err := db.QueryRow(`SELECT environment_id::text FROM enterprise_groups WHERE id = $1::uuid`, groupID).Scan(&envID)
	return envID, err == nil
}

func environmentOfCompany(db *sql.DB, companyID string) (string, bool) {
	if !isUUID(companyID) {
		return "", false
	}
	var envID string
	err := db.QueryRow(`
		SELECT eg.environment_id::text
		FROM companies c JOIN enterprise_groups eg ON c.group_id = eg.id
		WHERE c.id = $1::uuid
	`, companyID).Scan(&envID)
	return envID, err == nil
}

// canUseEnvironment: admin da plataforma OU membro do ambiente.
func canUseEnvironment(db *sql.DB, r *http.Request, envID string) bool {
	return isPlatformAdmin(r) || userInEnvironment(db, GetUserIDFromContext(r), envID)
}

// canAdminEnvironment: admin da plataforma OU admin do ambiente (exclusões).
func canAdminEnvironment(db *sql.DB, r *http.Request, envID string) bool {
	return isPlatformAdmin(r) || userAdminInEnvironment(db, GetUserIDFromContext(r), envID)
}

// denyTenant responde 403 e registra a tentativa. Não diferencia "não existe" de "não é seu"
// para não revelar a existência de recursos de outros tenants.
func denyTenant(w http.ResponseWriter, r *http.Request, action, target string) {
	log.Printf("[TenantGuard] %s negado user=%s alvo=%s ip=%s", action, GetUserIDFromContext(r), target, GetClientIP(r))
	jsonErr(w, http.StatusForbidden, "Acesso negado", map[string]string{"code": "tenant_forbidden"})
}

// registrationDisabled: ALLOW_REGISTRATION=false|0 desliga o cadastro público (default: ligado,
// para não afetar o deploy de mercado geral).
func registrationDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ALLOW_REGISTRATION"))) {
	case "false", "0", "no", "off":
		return true
	}
	return false
}

// authorizeDelete decide a exclusão de grupo/empresa: admin do ambiente ou da plataforma.
// Recurso inexistente → 404 só para o admin da plataforma; para os demais continua 403 (não
// revela existência). Devolve false quando já respondeu.
func authorizeDelete(db *sql.DB, w http.ResponseWriter, r *http.Request, action, id, envID string, found bool) bool {
	if !found {
		if isPlatformAdmin(r) {
			jsonErr(w, http.StatusNotFound, "Recurso não encontrado")
		} else {
			denyTenant(w, r, action, id)
		}
		return false
	}
	if !canAdminEnvironment(db, r, envID) {
		denyTenant(w, r, action, id)
		return false
	}
	return true
}

// LogRegistrationState registra no startup se o cadastro público está ligado — o .env de produção
// já veio vazio/regenerado antes (Coolify), então o estado efetivo precisa ficar visível no log.
func LogRegistrationState() {
	if registrationDisabled() {
		log.Println("[Config] Cadastro público DESABILITADO (ALLOW_REGISTRATION)")
		return
	}
	log.Printf("[Config] Cadastro público HABILITADO (ALLOW_REGISTRATION=%q; defina 'false' em deploy dedicado)", os.Getenv("ALLOW_REGISTRATION"))
}
