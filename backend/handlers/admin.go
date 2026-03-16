package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ResetCompanyDataRequest struct
type ResetCompanyDataRequest struct {
	CompanyID string `json:"company_id"`
}

// LimparDadosApuracaoHandler deletes IBS/CBS XML import data for the active company (admin only).
// Clears: nfe_saidas, nfe_entradas, cte_entradas, dfe_xml. RFB imports are preserved.
func LimparDadosApuracaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		role := claims["role"].(string)
		if role != "admin" {
			http.Error(w, "Forbidden: apenas administradores podem executar esta operação", http.StatusForbidden)
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			http.Error(w, "Erro ao identificar empresa: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[LimparApuracao] Admin %s limpando dados de apuração da empresa %s", userID, companyID)

		type tableResult struct {
			table   string
			deleted int64
		}
		results := []tableResult{}

		tables := []string{"nfe_saidas", "nfe_entradas", "cte_entradas", "dfe_xml"}
		for _, t := range tables {
			res, err := db.Exec("DELETE FROM "+t+" WHERE company_id = $1", companyID)
			if err != nil {
				log.Printf("[LimparApuracao] Erro ao limpar %s: %v", t, err)
				http.Error(w, "Erro ao limpar "+t+": "+err.Error(), http.StatusInternalServerError)
				return
			}
			n, _ := res.RowsAffected()
			results = append(results, tableResult{t, n})
			log.Printf("[LimparApuracao] %s: %d registros removidos", t, n)
		}

		totals := map[string]int64{}
		for _, r := range results {
			totals[r.table] = r.deleted
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Dados de apuração removidos com sucesso",
			"totals":  totals,
		})
	}
}

// ResetCompanyDataHandler deletes all import jobs for a specific Company ID
// It allows users to clean their own company data, or admins to clean any company.
func ResetCompanyDataHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ResetCompanyDataRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.CompanyID == "" {
			http.Error(w, "Company ID is required", http.StatusBadRequest)
			return
		}

		// Get User Context
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		role := claims["role"].(string)

		// Authorization Check: Must be Admin OR Environment Admin for the company
		if role != "admin" {
			var exists bool
			// Check if user has 'admin' role in the environment that owns the company
			err := db.QueryRow(`
				SELECT EXISTS(
					SELECT 1 
					FROM companies c
					JOIN enterprise_groups eg ON c.group_id = eg.id
					JOIN user_environments ue ON ue.environment_id = eg.environment_id
					WHERE ue.user_id = $1 
					  AND c.id = $2 
					  AND ue.role = 'admin'
				)`, userID, req.CompanyID).Scan(&exists)

			if err != nil {
				log.Printf("Error checking permission: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if !exists {
				http.Error(w, "Forbidden: You do not have permission to reset this company's data", http.StatusForbidden)
				return
			}
		}

		log.Printf("ResetCompanyData: User %s deleting data for CompanyID %s", userID, req.CompanyID)

		// Segurança: deletar arquivos físicos antes de remover os registros do banco
		fileRows, fileErr := db.Query("SELECT filename FROM import_jobs WHERE company_id = $1 AND filename != ''", req.CompanyID)
		if fileErr == nil {
			defer fileRows.Close()
			for fileRows.Next() {
				var fname string
				if fileRows.Scan(&fname) == nil && fname != "" {
					fpath := filepath.Join("uploads", fname)
					if err := os.Remove(fpath); err != nil && !os.IsNotExist(err) {
						log.Printf("ResetCompanyData: Warning: could not delete file %s: %v", fpath, err)
					} else if err == nil {
						log.Printf("ResetCompanyData: Deleted file %s from storage", fpath)
					}
				}
			}
		}

		// Execute Deletion
		res, err := db.Exec("DELETE FROM import_jobs WHERE company_id = $1", req.CompanyID)
		if err != nil {
			log.Printf("Error deleting jobs for CompanyID %s: %v", req.CompanyID, err)
			http.Error(w, "Failed to delete company data", http.StatusInternalServerError)
			return
		}

		rowsDeleted, _ := res.RowsAffected()
		log.Printf("ResetCompanyData: Deleted %d jobs for CompanyID %s", rowsDeleted, req.CompanyID)

		// Trigger Refresh to clear dashboard data
		go func() {
			log.Printf("ResetCompanyData: Triggering view refresh for CompanyID %s...", req.CompanyID)

			// Refresh mv_mercadorias_agregada
			if _, err := db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY mv_mercadorias_agregada"); err != nil {
				log.Printf("ResetCompanyData: Concurrent refresh failed for mv_mercadorias_agregada, trying standard: %v", err)
				db.Exec("REFRESH MATERIALIZED VIEW mv_mercadorias_agregada")
			}

			// Refresh mv_operacoes_simples (Simples Nacional)
			if _, err := db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY mv_operacoes_simples"); err != nil {
				log.Printf("ResetCompanyData: Concurrent refresh failed for mv_operacoes_simples, trying standard: %v", err)
				db.Exec("REFRESH MATERIALIZED VIEW mv_operacoes_simples")
			}

			// Refresh mv_compras_fornecedores (todos os fornecedores)
			if _, err := db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY mv_compras_fornecedores"); err != nil {
				log.Printf("ResetCompanyData: Concurrent refresh failed for mv_compras_fornecedores, trying standard: %v", err)
				db.Exec("REFRESH MATERIALIZED VIEW mv_compras_fornecedores")
			}

			log.Printf("ResetCompanyData: View refresh completed for CompanyID %s", req.CompanyID)
		}()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":      "Company data deleted successfully",
			"jobs_deleted": rowsDeleted,
		})
	}
}

// RefreshViewsHandler triggers a refresh of all materialized views
func RefreshViewsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get User Context
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)

		log.Printf("RefreshViews: User %s requested view refresh", userID)

		// Refresh Mercadorias View
		start := time.Now()

		// Refresh mv_mercadorias_agregada
		_, err := db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY mv_mercadorias_agregada")
		if err != nil {
			log.Printf("Concurrent refresh failed for mv_mercadorias_agregada, trying standard: %v", err)
			_, err = db.Exec("REFRESH MATERIALIZED VIEW mv_mercadorias_agregada")
			if err != nil {
				log.Printf("Error refreshing mv_mercadorias_agregada: %v", err)
				http.Error(w, "Failed to refresh mv_mercadorias_agregada: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Refresh mv_operacoes_simples (Simples Nacional)
		_, err = db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY mv_operacoes_simples")
		if err != nil {
			log.Printf("Concurrent refresh failed for mv_operacoes_simples, trying standard: %v", err)
			_, err = db.Exec("REFRESH MATERIALIZED VIEW mv_operacoes_simples")
			if err != nil {
				log.Printf("Error refreshing mv_operacoes_simples: %v", err)
				http.Error(w, "Failed to refresh mv_operacoes_simples: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Refresh mv_compras_fornecedores (todos os fornecedores)
		_, err = db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY mv_compras_fornecedores")
		if err != nil {
			log.Printf("Concurrent refresh failed for mv_compras_fornecedores, trying standard: %v", err)
			_, err = db.Exec("REFRESH MATERIALIZED VIEW mv_compras_fornecedores")
			if err != nil {
				log.Printf("Error refreshing mv_compras_fornecedores: %v", err)
				http.Error(w, "Failed to refresh mv_compras_fornecedores: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		duration := time.Since(start)
		log.Printf("RefreshViews: Completed in %v", duration)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":     "Views refreshed successfully",
			"duration_ms": duration.Milliseconds(),
		})
	}
}

// ResetDatabaseHandler deletes all records from import_jobs,
// which cascades to all related SPED data tables (participants, regs, aggregations).
// It preserves system configuration tables like cfop and tabela_aliquotas.
func ResetDatabaseHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		log.Println("Admin: Initiating full database reset (clearing imported data)...")

		// Execute the deletion in a transaction for safety
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Error starting transaction: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Optimize: Use TRUNCATE CASCADE for instant clearing of large datasets.
		// TRUNCATE is much faster than DELETE because it doesn't scan tables or log individual row deletions.
		// CASCADE ensures all dependent tables (reg_*, aggregations) are also cleared.
		_, err = tx.Exec("TRUNCATE TABLE import_jobs CASCADE")
		if err != nil {
			log.Printf("Error truncating import_jobs: %v", err)
			// Fallback to DELETE if TRUNCATE fails (e.g. permissions)
			_, err = tx.Exec("DELETE FROM import_jobs")
			if err != nil {
				log.Printf("Error deleting import_jobs (fallback): %v", err)
				http.Error(w, "Failed to reset database", http.StatusInternalServerError)
				return
			}
		}

		// Apelidos de filiais (NOT cascaded by import_jobs — delete explicitly)
		if _, err := tx.Exec("DELETE FROM filial_apelidos"); err != nil {
			log.Printf("Warning: could not clear filial_apelidos: %v", err)
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Error committing transaction: %v", err)
			http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		log.Printf("Database reset successful (TRUNCATE).")

		// REFRESH VIEWS: Ensure views are empty after truncating data
		log.Println("Admin: Refreshing Materialized Views after reset...")

		// Refresh mv_mercadorias_agregada
		if _, err := db.Exec("REFRESH MATERIALIZED VIEW mv_mercadorias_agregada"); err != nil {
			log.Printf("Error refreshing mv_mercadorias_agregada after reset: %v", err)
		} else {
			log.Println("Admin: mv_mercadorias_agregada refreshed successfully (Empty).")
		}

		// Refresh mv_operacoes_simples (Simples Nacional)
		if _, err := db.Exec("REFRESH MATERIALIZED VIEW mv_operacoes_simples"); err != nil {
			log.Printf("Error refreshing mv_operacoes_simples after reset: %v", err)
		} else {
			log.Println("Admin: mv_operacoes_simples refreshed successfully (Empty).")
		}

		// Refresh mv_compras_fornecedores (todos os fornecedores)
		if _, err := db.Exec("REFRESH MATERIALIZED VIEW mv_compras_fornecedores"); err != nil {
			log.Printf("Error refreshing mv_compras_fornecedores after reset: %v", err)
		} else {
			log.Println("Admin: mv_compras_fornecedores refreshed successfully (Empty).")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":      "Database reset successfully",
			"jobs_deleted": -1, // TRUNCATE doesn't return count
		})
	}
}

// CreateUserRequest struct
type CreateUserRequest struct {
	FullName      string `json:"full_name"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	Role          string `json:"role"`
	EnvironmentID string `json:"environment_id"` // Optional: link to existing environment
	GroupID       string `json:"group_id"`        // Optional: link to existing group
	CompanyID     string `json:"company_id"`      // Optional: link to existing company
}

// CreateUserHandler creates a new user directly (Admin only)
func CreateUserHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if req.Email == "" || req.Password == "" || req.FullName == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// Hash Password
		hash, err := HashPassword(req.Password)
		if err != nil {
			http.Error(w, "Error hashing password", http.StatusInternalServerError)
			return
		}

		// Default Role
		if req.Role == "" {
			req.Role = "user"
		}

		// Insert User
		trialEnds := time.Now().Add(time.Hour * 24 * 14) // 14 days
		var userID string
		err = db.QueryRow(`
			INSERT INTO users (email, password_hash, full_name, trial_ends_at, is_verified, role)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`, req.Email, hash, req.FullName, trialEnds, true, req.Role).Scan(&userID)

		if err != nil {
			log.Printf("Error creating user: %v", err)
			http.Error(w, "Error creating user (email might be taken)", http.StatusConflict)
			return
		}

		if req.EnvironmentID != "" {
			// Link to existing hierarchy
			_, err = db.Exec("INSERT INTO user_environments (user_id, environment_id, role) VALUES ($1, $2, 'user')", userID, req.EnvironmentID)
			if err != nil {
				log.Printf("Error linking user to environment: %v", err)
			}

			// If company_id provided, always set owner_id (admin explicitly chose the company)
			if req.CompanyID != "" {
				_, err = db.Exec("UPDATE companies SET owner_id = $1 WHERE id = $2", userID, req.CompanyID)
				if err != nil {
					log.Printf("Error setting company owner: %v", err)
				}
			}
		} else {
			// Auto-provision new hierarchy (original behavior)
			var envID string
			err = db.QueryRow("INSERT INTO environments (name, description) VALUES ($1, 'Ambiente Padrão') RETURNING id", "Ambiente de "+req.FullName).Scan(&envID)
			if err == nil {
				var groupID string
				db.QueryRow("INSERT INTO enterprise_groups (environment_id, name, description) VALUES ($1, 'Grupo Padrão', 'Grupo Inicial') RETURNING id", envID).Scan(&groupID)
				db.Exec("INSERT INTO user_environments (user_id, environment_id, role) VALUES ($1, $2, 'admin')", userID, envID)
				if groupID != "" {
					db.Exec("INSERT INTO companies (group_id, name, trade_name, owner_id) VALUES ($1, $2, $2, $3)", groupID, "Empresa de "+req.FullName, userID)
				}
			}
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"message": "User created successfully", "id": userID})
	}
}

// AdminUser extends User with hierarchy info for admin listing
type AdminUser struct {
	ID              string    `json:"id"`
	Email           string    `json:"email"`
	FullName        string    `json:"full_name"`
	IsVerified      bool      `json:"is_verified"`
	TrialEndsAt     time.Time `json:"trial_ends_at"`
	Role            string    `json:"role"`
	CreatedAt       string    `json:"created_at"`
	EnvironmentID   *string   `json:"environment_id"`
	EnvironmentName *string   `json:"environment_name"`
	GroupID         *string   `json:"group_id"`
	GroupName       *string   `json:"group_name"`
	CompanyID       *string   `json:"company_id"`
	CompanyName     *string   `json:"company_name"`
}

// ListUsersHandler returns all users with hierarchy info (Admin only)
func ListUsersHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`
			SELECT DISTINCT ON (u.id)
			       u.id, u.email, u.full_name, u.is_verified, u.trial_ends_at, u.role, u.created_at,
			       e.id, e.name,
			       eg.id, eg.name,
			       c.id, c.name
			FROM users u
			LEFT JOIN user_environments ue ON u.id = ue.user_id
			LEFT JOIN environments e ON ue.environment_id = e.id
			LEFT JOIN enterprise_groups eg ON eg.environment_id = e.id
			LEFT JOIN companies c ON c.group_id = eg.id
			ORDER BY u.id, (c.owner_id = u.id) DESC NULLS LAST, c.created_at ASC NULLS LAST
		`)
		if err != nil {
			log.Printf("ListUsers error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var users []AdminUser
		for rows.Next() {
			var u AdminUser
			if err := rows.Scan(&u.ID, &u.Email, &u.FullName, &u.IsVerified, &u.TrialEndsAt, &u.Role, &u.CreatedAt,
				&u.EnvironmentID, &u.EnvironmentName,
				&u.GroupID, &u.GroupName,
				&u.CompanyID, &u.CompanyName); err != nil {
				log.Printf("ListUsers scan error: %v", err)
				continue
			}
			users = append(users, u)
		}

		if users == nil {
			users = []AdminUser{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
	}
}

// PromoteUserRequest struct
type PromoteUserRequest struct {
	Role       string `json:"role"`        // 'admin' or 'user'
	ExtendDays int    `json:"extend_days"` // Days to add to trial
	IsOfficial bool   `json:"is_official"` // If true, sets trial to 2099
}

// PromoteUserHandler updates user role or trial (Admin only)
func PromoteUserHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("id")
		if userID == "" {
			http.Error(w, "User ID required", http.StatusBadRequest)
			return
		}

		var req PromoteUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Update logic
		if req.Role != "" {
			_, err := db.Exec("UPDATE users SET role = $1 WHERE id = $2", req.Role, userID)
			if err != nil {
				http.Error(w, "Failed to update role", http.StatusInternalServerError)
				return
			}
		}

		if req.IsOfficial {
			// Set to far future (Official Client)
			newEnd := time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
			_, err := db.Exec("UPDATE users SET trial_ends_at = $1 WHERE id = $2", newEnd, userID)
			if err != nil {
				http.Error(w, "Failed to update trial status", http.StatusInternalServerError)
				return
			}
		} else if req.ExtendDays > 0 {
			// Get current trial end
			var currentEnd time.Time
			err := db.QueryRow("SELECT trial_ends_at FROM users WHERE id = $1", userID).Scan(&currentEnd)
			if err != nil {
				http.Error(w, "User not found", http.StatusNotFound)
				return
			}

			// If expired, start from now. If not, add to existing.
			if currentEnd.Before(time.Now()) {
				currentEnd = time.Now()
			}
			newEnd := currentEnd.Add(time.Duration(req.ExtendDays) * 24 * time.Hour)

			_, err = db.Exec("UPDATE users SET trial_ends_at = $1 WHERE id = $2", newEnd, userID)
			if err != nil {
				http.Error(w, "Failed to update trial", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "User updated successfully"})
	}
}

// ReassignUserRequest struct
type ReassignUserRequest struct {
	UserID        string `json:"user_id"`
	EnvironmentID string `json:"environment_id"`
	GroupID       string `json:"group_id"`  // Optional
	CompanyID     string `json:"company_id"` // Optional
}

// ReassignUserHandler re-links a user to a different hierarchy (Admin only)
func ReassignUserHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ReassignUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if req.UserID == "" || req.EnvironmentID == "" {
			http.Error(w, "user_id and environment_id are required", http.StatusBadRequest)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Remove old company ownership (clear owner_id where this user is owner)
		_, err = tx.Exec(`
			UPDATE companies SET owner_id = NULL
			WHERE owner_id = $1
		`, req.UserID)
		if err != nil {
			log.Printf("ReassignUser: Error clearing old company owner: %v", err)
		}

		// Remove old environment link
		_, err = tx.Exec("DELETE FROM user_environments WHERE user_id = $1", req.UserID)
		if err != nil {
			log.Printf("ReassignUser: Error removing old env link: %v", err)
			http.Error(w, "Failed to remove old environment link", http.StatusInternalServerError)
			return
		}

		// Insert new environment link com preferred_company_id se fornecido
		if req.CompanyID != "" {
			_, err = tx.Exec(`
				INSERT INTO user_environments (user_id, environment_id, role, preferred_company_id)
				VALUES ($1, $2, 'user', $3)
			`, req.UserID, req.EnvironmentID, req.CompanyID)
		} else {
			_, err = tx.Exec("INSERT INTO user_environments (user_id, environment_id, role) VALUES ($1, $2, 'user')", req.UserID, req.EnvironmentID)
		}
		if err != nil {
			log.Printf("ReassignUser: Error inserting new env link: %v", err)
			http.Error(w, "Failed to link to new environment", http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, "Failed to commit changes", http.StatusInternalServerError)
			return
		}

		log.Printf("ReassignUser: User %s reassigned to environment %s, company %s", req.UserID, req.EnvironmentID, req.CompanyID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "User reassigned successfully"})
	}
}

// DeleteUserHandler deletes a user and all their data (Admin only)
func DeleteUserHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("id")
		if userID == "" {
			http.Error(w, "User ID required", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("DELETE FROM users WHERE id = $1", userID)
		if err != nil {
			http.Error(w, "Failed to delete user", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "User deleted successfully"})
	}
}
