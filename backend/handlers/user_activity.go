package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	jwt "github.com/golang-jwt/jwt/v5"
)

// LogActivityHandler — POST /api/activity/log
// Qualquer usuário autenticado registra visita a um módulo com duração em segundos.
func LogActivityHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusBadRequest, "Empresa não encontrada ou sem acesso", err, "[UserActivity]")
			return
		}

		var body struct {
			Module          string `json:"module"`
			DurationSeconds int    `json:"duration_seconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Module == "" {
			jsonErr(w, http.StatusBadRequest, "campo module obrigatório")
			return
		}
		if body.DurationSeconds < 0 {
			body.DurationSeconds = 0
		}

		if _, err := db.Exec(`
			INSERT INTO user_activity_logs (user_id, company_id, module, duration_seconds)
			VALUES ($1, $2, $3, $4)
		`, userID, companyID, body.Module, body.DurationSeconds); err != nil {
			log.Printf("[Activity] insert error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao registrar atividade")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

type userActivityRow struct {
	UserID               string   `json:"user_id"`
	UserName             string   `json:"user_name"`
	Email                string   `json:"email"`
	CompanyName          string   `json:"company_name"`
	Modules              []string `json:"modules"`
	VisitCount           int      `json:"visit_count"`
	TotalDurationSeconds int      `json:"total_duration_seconds"`
	LastVisitedAt        string   `json:"last_visited_at"`
}

type moduleActivityRow struct {
	Module               string `json:"module"`
	UserCount            int    `json:"user_count"`
	VisitCount           int    `json:"visit_count"`
	TotalDurationSeconds int    `json:"total_duration_seconds"`
	LastVisitedAt        string `json:"last_visited_at"`
}

// ListUserActivityHandler — GET /api/admin/user-activity
// Retorna resumo de atividade agrupado por usuário para a empresa atual.
func ListUserActivityHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		userID := claims["user_id"].(string)
		role, _ := claims["role"].(string)
		if role != "admin" {
			jsonErr(w, http.StatusForbidden, "Acesso restrito a administradores")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusBadRequest, "Empresa não encontrada ou sem acesso", err, "[UserActivity]")
			return
		}

		rows, err := db.Query(`
			SELECT
				u.id,
				u.full_name,
				u.email,
				COALESCE(c.name, '') AS company_name,
				COALESCE(STRING_AGG(DISTINCT ual.module, '|'), '') AS modules,
				COUNT(*)::int AS visit_count,
				COALESCE(SUM(ual.duration_seconds), 0)::int AS total_duration_seconds,
				TO_CHAR(MAX(ual.visited_at) AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS last_visited_at
			FROM user_activity_logs ual
			JOIN users u ON u.id = ual.user_id
			LEFT JOIN companies c ON c.id = ual.company_id
			WHERE ual.company_id = $1
			GROUP BY u.id, u.full_name, u.email, c.name
			ORDER BY MAX(ual.visited_at) DESC NULLS LAST
		`, companyID)
		if err != nil {
			log.Printf("[Activity] list users error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao buscar atividade")
			return
		}
		defer rows.Close()

		items := []userActivityRow{}
		for rows.Next() {
			var row userActivityRow
			var modulesStr string
			if err := rows.Scan(
				&row.UserID, &row.UserName, &row.Email, &row.CompanyName,
				&modulesStr, &row.VisitCount, &row.TotalDurationSeconds, &row.LastVisitedAt,
			); err != nil {
				log.Printf("[Activity] scan error: %v", err)
				continue
			}
			if modulesStr != "" {
				row.Modules = strings.Split(modulesStr, "|")
			} else {
				row.Modules = []string{}
			}
			items = append(items, row)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}
}

// ListModuleActivityHandler — GET /api/admin/user-activity/modules
// Retorna resumo de atividade agrupado por módulo para a empresa atual.
func ListModuleActivityHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		userID := claims["user_id"].(string)
		role, _ := claims["role"].(string)
		if role != "admin" {
			jsonErr(w, http.StatusForbidden, "Acesso restrito a administradores")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusBadRequest, "Empresa não encontrada ou sem acesso", err, "[UserActivity]")
			return
		}

		rows, err := db.Query(`
			SELECT
				ual.module,
				COUNT(DISTINCT ual.user_id)::int AS user_count,
				COUNT(*)::int AS visit_count,
				COALESCE(SUM(ual.duration_seconds), 0)::int AS total_duration_seconds,
				TO_CHAR(MAX(ual.visited_at) AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS last_visited_at
			FROM user_activity_logs ual
			WHERE ual.company_id = $1
			GROUP BY ual.module
			ORDER BY COUNT(*) DESC
		`, companyID)
		if err != nil {
			log.Printf("[Activity] list modules error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao buscar atividade por módulo")
			return
		}
		defer rows.Close()

		items := []moduleActivityRow{}
		for rows.Next() {
			var row moduleActivityRow
			if err := rows.Scan(
				&row.Module, &row.UserCount, &row.VisitCount,
				&row.TotalDurationSeconds, &row.LastVisitedAt,
			); err != nil {
				continue
			}
			items = append(items, row)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}
}
