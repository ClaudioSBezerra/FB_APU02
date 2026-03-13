package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type RFBCredential struct {
	ID           string    `json:"id"`
	CompanyID    string    `json:"company_id"`
	CNPJMatriz   string    `json:"cnpj_matriz"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	Ambiente     string    `json:"ambiente"`
	Ativo        bool      `json:"ativo"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// GetRFBCredentialHandler returns the RFB credential for the user's company
func GetRFBCredentialHandler(db *sql.DB) http.HandlerFunc {
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

		var cred RFBCredential
		err = db.QueryRow(`
			SELECT id, company_id, cnpj_matriz, client_id, client_secret, COALESCE(ambiente, 'producao'), ativo, created_at, updated_at
			FROM rfb_credentials
			WHERE company_id = $1
		`, companyID).Scan(&cred.ID, &cred.CompanyID, &cred.CNPJMatriz, &cred.ClientID, &cred.ClientSecret, &cred.Ambiente, &cred.Ativo, &cred.CreatedAt, &cred.UpdatedAt)

		if err == sql.ErrNoRows {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"credential": nil,
			})
			return
		}
		if err != nil {
			http.Error(w, "Error querying credential: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Mask client_secret - show only last 4 chars
		if len(cred.ClientSecret) > 4 {
			cred.ClientSecret = strings.Repeat("*", len(cred.ClientSecret)-4) + cred.ClientSecret[len(cred.ClientSecret)-4:]
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"credential": cred,
		})
	}
}

// SaveRFBCredentialHandler creates or updates an RFB credential (UPSERT)
func SaveRFBCredentialHandler(db *sql.DB) http.HandlerFunc {
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
			CNPJMatriz   string `json:"cnpj_matriz"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			Ambiente     string `json:"ambiente"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validation
		req.CNPJMatriz = strings.TrimSpace(req.CNPJMatriz)
		req.ClientID = strings.TrimSpace(req.ClientID)
		req.ClientSecret = strings.TrimSpace(req.ClientSecret)
		if req.Ambiente != "producao_restrita" {
			req.Ambiente = "producao"
		}

		// Remove formatting chars from CNPJ
		req.CNPJMatriz = strings.ReplaceAll(req.CNPJMatriz, ".", "")
		req.CNPJMatriz = strings.ReplaceAll(req.CNPJMatriz, "/", "")
		req.CNPJMatriz = strings.ReplaceAll(req.CNPJMatriz, "-", "")

		if len(req.CNPJMatriz) != 14 {
			http.Error(w, "CNPJ Matriz deve ter 14 dígitos", http.StatusBadRequest)
			return
		}
		if req.ClientID == "" {
			http.Error(w, "Client ID é obrigatório", http.StatusBadRequest)
			return
		}
		if req.ClientSecret == "" {
			http.Error(w, "Client Secret é obrigatório", http.StatusBadRequest)
			return
		}

		// UPSERT - insert or update on conflict
		var id string
		err = db.QueryRow(`
			INSERT INTO rfb_credentials (company_id, cnpj_matriz, client_id, client_secret, ambiente, ativo)
			VALUES ($1, $2, $3, $4, $5, true)
			ON CONFLICT (company_id)
			DO UPDATE SET cnpj_matriz = $2, client_id = $3, client_secret = $4, ambiente = $5, ativo = true, updated_at = CURRENT_TIMESTAMP
			RETURNING id
		`, companyID, req.CNPJMatriz, req.ClientID, req.ClientSecret, req.Ambiente).Scan(&id)
		if err != nil {
			http.Error(w, "Error saving credential: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch saved credential
		var cred RFBCredential
		err = db.QueryRow(`
			SELECT id, company_id, cnpj_matriz, client_id, client_secret, COALESCE(ambiente, 'producao'), ativo, created_at, updated_at
			FROM rfb_credentials WHERE id = $1
		`, id).Scan(&cred.ID, &cred.CompanyID, &cred.CNPJMatriz, &cred.ClientID, &cred.ClientSecret, &cred.Ambiente, &cred.Ativo, &cred.CreatedAt, &cred.UpdatedAt)
		if err != nil {
			http.Error(w, "Credential saved but error fetching", http.StatusInternalServerError)
			return
		}

		// Mask client_secret in response
		if len(cred.ClientSecret) > 4 {
			cred.ClientSecret = strings.Repeat("*", len(cred.ClientSecret)-4) + cred.ClientSecret[len(cred.ClientSecret)-4:]
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"credential": cred,
			"message":    "Credenciais salvas com sucesso",
		})
	}
}

// DeleteRFBCredentialHandler removes the RFB credential for the user's company
func DeleteRFBCredentialHandler(db *sql.DB) http.HandlerFunc {
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

		result, err := db.Exec("DELETE FROM rfb_credentials WHERE company_id = $1", companyID)
		if err != nil {
			http.Error(w, "Error deleting credential: "+err.Error(), http.StatusInternalServerError)
			return
		}

		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Nenhuma credencial encontrada", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
