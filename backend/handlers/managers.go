package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Manager represents a company manager who receives AI reports
type Manager struct {
	ID           string    `json:"id"`
	CompanyID    string    `json:"company_id"`
	NomeCompleto string    `json:"nome_completo"`
	Cargo        string    `json:"cargo"`
	Email        string    `json:"email"`
	Ativo        bool      `json:"ativo"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ListManagersHandler returns all managers for a company
func ListManagersHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			http.Error(w, "Error getting company: "+err.Error(), http.StatusInternalServerError)
			return
		}

		rows, err := db.Query(`
			SELECT id, company_id, nome_completo, cargo, email, ativo, created_at, updated_at
			FROM managers
			WHERE company_id = $1
			ORDER BY nome_completo ASC
		`, companyID)
		if err != nil {
			http.Error(w, "Error querying managers: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var managers []Manager
		for rows.Next() {
			var m Manager
			if err := rows.Scan(&m.ID, &m.CompanyID, &m.NomeCompleto, &m.Cargo, &m.Email, &m.Ativo, &m.CreatedAt, &m.UpdatedAt); err != nil {
				http.Error(w, "Error scanning manager: "+err.Error(), http.StatusInternalServerError)
				return
			}
			managers = append(managers, m)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"managers": managers,
			"count":    len(managers),
		})
	}
}

// CreateManagerHandler creates a new manager
func CreateManagerHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			http.Error(w, "Error getting company: "+err.Error(), http.StatusInternalServerError)
			return
		}

		var req struct {
			NomeCompleto string `json:"nome_completo"`
			Cargo        string `json:"cargo"`
			Email        string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validation
		req.NomeCompleto = strings.TrimSpace(req.NomeCompleto)
		req.Cargo = strings.TrimSpace(req.Cargo)
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))

		if req.NomeCompleto == "" {
			http.Error(w, "Nome completo é obrigatório", http.StatusBadRequest)
		} else if req.Cargo == "" {
			http.Error(w, "Cargo é obrigatório", http.StatusBadRequest)
			return
		}
		if req.Email == "" || !strings.Contains(req.Email, "@") {
			http.Error(w, "E-mail inválido", http.StatusBadRequest)
			return
		}

		// Check if email already exists for this company
		var exists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM managers WHERE company_id = $1 AND email = $2 AND ativo = true)", companyID, req.Email).Scan(&exists)
		if err == nil && exists {
			http.Error(w, "Já existe um gestor com este e-mail nesta empresa", http.StatusConflict)
			return
		}

		// Insert manager
		var id string
		err = db.QueryRow(`
			INSERT INTO managers (company_id, nome_completo, cargo, email, ativo)
			VALUES ($1, $2, $3, $4, true)
			RETURNING id
		`, companyID, req.NomeCompleto, req.Cargo, req.Email).Scan(&id)
		if err != nil {
			http.Error(w, "Error creating manager: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch created manager
		var m Manager
		err = db.QueryRow(`
			SELECT id, company_id, nome_completo, cargo, email, ativo, created_at, updated_at
			FROM managers WHERE id = $1
		`, id).Scan(&m.ID, &m.CompanyID, &m.NomeCompleto, &m.Cargo, &m.Email, &m.Ativo, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			http.Error(w, "Manager created but error fetching", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(m)
	}
}

// UpdateManagerHandler updates an existing manager
func UpdateManagerHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if r.Method != http.MethodPut && r.Method != http.MethodPatch {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			http.Error(w, "Error getting company: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Extract manager ID from URL path
		path := strings.TrimPrefix(r.URL.Path, "/api/managers/")
		managerID := strings.TrimSpace(path)
		if managerID == "" {
			http.Error(w, "Invalid manager ID", http.StatusBadRequest)
			return
		}

		var req struct {
			NomeCompleto string `json:"nome_completo"`
			Cargo        string `json:"cargo"`
			Email        string `json:"email"`
			Ativo        *bool  `json:"ativo"` // Pointer to distinguish zero value
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate manager belongs to user's company
		var existingCompanyID string
		err = db.QueryRow("SELECT company_id FROM managers WHERE id = $1", managerID).Scan(&existingCompanyID)
		if err == sql.ErrNoRows {
			http.Error(w, "Manager not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Error checking manager: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if existingCompanyID != companyID {
			http.Error(w, "Unauthorized", http.StatusForbidden)
			return
		}

		// Build update query dynamically
		updates := []string{}
		args := []interface{}{}
		argPos := 1

		if req.NomeCompleto != "" {
			req.NomeCompleto = strings.TrimSpace(req.NomeCompleto)
			updates = append(updates, fmt.Sprintf("nome_completo = $%d", argPos))
			args = append(args, req.NomeCompleto)
			argPos++
		}
		if req.Cargo != "" {
			req.Cargo = strings.TrimSpace(req.Cargo)
			updates = append(updates, fmt.Sprintf("cargo = $%d", argPos))
			args = append(args, req.Cargo)
			argPos++
		}
		if req.Email != "" {
			req.Email = strings.TrimSpace(strings.ToLower(req.Email))
			if !strings.Contains(req.Email, "@") {
				http.Error(w, "E-mail inválido", http.StatusBadRequest)
				return
			}
			updates = append(updates, fmt.Sprintf("email = $%d", argPos))
			args = append(args, req.Email)
			argPos++
		}
		if req.Ativo != nil {
			updates = append(updates, fmt.Sprintf("ativo = $%d", argPos))
			args = append(args, *req.Ativo)
			argPos++
		}

		if len(updates) == 0 {
			http.Error(w, "No fields to update", http.StatusBadRequest)
			return
		}

		args = append(args, managerID)
		query := fmt.Sprintf("UPDATE managers SET %s WHERE id = $%d", strings.Join(updates, ", "), argPos)

		_, err = db.Exec(query, args...)
		if err != nil {
			http.Error(w, "Error updating manager: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch updated manager
		var m Manager
		err = db.QueryRow(`
			SELECT id, company_id, nome_completo, cargo, email, ativo, created_at, updated_at
			FROM managers WHERE id = $1
		`, managerID).Scan(&m.ID, &m.CompanyID, &m.NomeCompleto, &m.Cargo, &m.Email, &m.Ativo, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			http.Error(w, "Manager updated but error fetching", http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(m)
	}
}

// DeleteManagerHandler soft deletes (deactivates) a manager
func DeleteManagerHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

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

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			http.Error(w, "Error getting company: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Extract manager ID from URL path
		path := strings.TrimPrefix(r.URL.Path, "/api/managers/")
		managerID := strings.TrimSpace(path)
		if managerID == "" {
			http.Error(w, "Invalid manager ID", http.StatusBadRequest)
			return
		}

		// Validate manager belongs to user's company
		var existingCompanyID string
		err = db.QueryRow("SELECT company_id FROM managers WHERE id = $1", managerID).Scan(&existingCompanyID)
		if err == sql.ErrNoRows {
			http.Error(w, "Manager not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Error checking manager: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if existingCompanyID != companyID {
			http.Error(w, "Unauthorized", http.StatusForbidden)
			return
		}

		// Soft delete (set ativo = false)
		_, err = db.Exec("UPDATE managers SET ativo = false WHERE id = $1", managerID)
		if err != nil {
			http.Error(w, "Error deleting manager: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// GetActiveManagersByCompany returns active managers for a company (used by AI report system)
func GetActiveManagersByCompany(db *sql.DB, companyID string) ([]Manager, error) {
	rows, err := db.Query(`
		SELECT id, company_id, nome_completo, cargo, email, ativo, created_at, updated_at
		FROM managers
		WHERE company_id = $1 AND ativo = true
		ORDER BY nome_completo ASC
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var managers []Manager
	for rows.Next() {
		var m Manager
		if err := rows.Scan(&m.ID, &m.CompanyID, &m.NomeCompleto, &m.Cargo, &m.Email, &m.Ativo, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		managers = append(managers, m)
	}

	return managers, nil
}
