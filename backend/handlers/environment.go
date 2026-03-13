package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// Structures
type Environment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

type EnterpriseGroup struct {
	ID            string `json:"id"`
	EnvironmentID string `json:"environment_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	CreatedAt     string `json:"created_at"`
}

type Company struct {
	ID      string `json:"id"`
	GroupID string `json:"group_id"`
	// CNPJ      string `json:"cnpj"` // Deprecated
	Name      string `json:"name"`
	TradeName string `json:"trade_name"` // Fantasia
	CreatedAt string `json:"created_at"`
}

// --- Environment Handlers ---

func GetEnvironmentsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		role := claims["role"].(string)

		log.Printf("[GetEnvironments] User: %s, Role: %s", userID, role)

		var rows *sql.Rows
		var err error

		if role == "admin" {
			// Platform Admin sees all environments
			rows, err = db.Query("SELECT id, name, COALESCE(description, ''), created_at FROM environments ORDER BY name")
		} else {
			// Regular users see only assigned environments
			rows, err = db.Query(`
				SELECT e.id, e.name, COALESCE(e.description, ''), e.created_at 
				FROM environments e
				JOIN user_environments ue ON e.id = ue.environment_id
				WHERE ue.user_id = $1
				ORDER BY e.name
			`, userID)
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var envs []Environment
		for rows.Next() {
			var e Environment
			if err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.CreatedAt); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			envs = append(envs, e)
		}

		if envs == nil {
			envs = []Environment{}
		}
		json.NewEncoder(w).Encode(envs)
	}
}

func CreateEnvironmentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var e Environment
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err := db.QueryRow(
			"INSERT INTO environments (name, description) VALUES ($1, $2) RETURNING id, created_at",
			e.Name, e.Description,
		).Scan(&e.ID, &e.CreatedAt)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(e)
	}
}

func UpdateEnvironmentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Expects ID in URL or Body. For simplicity, we take from body now or just update based on ID
		var e Environment
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err := db.Exec(
			"UPDATE environments SET name = $1, description = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3",
			e.Name, e.Description, e.ID,
		)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(e)
	}
}

func DeleteEnvironmentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing id parameter", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("DELETE FROM environments WHERE id = $1", id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// --- Group Handlers ---

func GetGroupsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		envID := r.URL.Query().Get("environment_id")
		query := "SELECT id, environment_id, name, COALESCE(description, ''), created_at FROM enterprise_groups"
		args := []interface{}{}

		if envID != "" {
			query += " WHERE environment_id = $1"
			args = append(args, envID)
		}
		query += " ORDER BY name"

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var groups []EnterpriseGroup
		for rows.Next() {
			var g EnterpriseGroup
			if err := rows.Scan(&g.ID, &g.EnvironmentID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			groups = append(groups, g)
		}

		if groups == nil {
			groups = []EnterpriseGroup{}
		}
		json.NewEncoder(w).Encode(groups)
	}
}

func CreateGroupHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var g EnterpriseGroup
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err := db.QueryRow(
			"INSERT INTO enterprise_groups (environment_id, name, description) VALUES ($1, $2, $3) RETURNING id, created_at",
			g.EnvironmentID, g.Name, g.Description,
		).Scan(&g.ID, &g.CreatedAt)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(g)
	}
}

func DeleteGroupHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing id parameter", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("DELETE FROM enterprise_groups WHERE id = $1", id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// --- Company Handlers ---

func GetCompaniesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := r.URL.Query().Get("group_id")
		query := "SELECT id, group_id, name, COALESCE(trade_name, ''), created_at FROM companies"
		args := []interface{}{}

		if groupID != "" {
			query += " WHERE group_id = $1"
			args = append(args, groupID)
		}
		query += " ORDER BY name"

		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var companies []Company
		for rows.Next() {
			var c Company
			if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.TradeName, &c.CreatedAt); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			companies = append(companies, c)
		}

		if companies == nil {
			companies = []Company{}
		}
		json.NewEncoder(w).Encode(companies)
	}
}

func CreateCompanyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c Company
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Basic validation
		if c.Name == "" || c.GroupID == "" {
			http.Error(w, "Missing required fields (name, group_id)", http.StatusBadRequest)
			return
		}

		// Resolve owner: use group's environment owner (first user linked to the environment)
		var ownerID *string
		err := db.QueryRow(`
			SELECT ue.user_id
			FROM enterprise_groups eg
			JOIN user_environments ue ON ue.environment_id = eg.environment_id
			WHERE eg.id = $1
			ORDER BY ue.created_at ASC
			LIMIT 1
		`, c.GroupID).Scan(&ownerID)
		if err != nil {
			ownerID = nil // no owner found, leave NULL (still visible via group query)
		}

		err = db.QueryRow(
			"INSERT INTO companies (group_id, name, trade_name, owner_id) VALUES ($1, $2, $3, $4) RETURNING id, created_at",
			c.GroupID, c.Name, c.TradeName, ownerID,
		).Scan(&c.ID, &c.CreatedAt)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(c)
	}
}

func DeleteCompanyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing id parameter", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("DELETE FROM companies WHERE id = $1", id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
