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
	ID                  string    `json:"id"`
	CompanyID           string    `json:"company_id"`
	CNPJMatriz          string    `json:"cnpj_matriz"`
	ClientID            string    `json:"client_id"`
	ClientSecret        string    `json:"client_secret"`
	Ambiente            string    `json:"ambiente"`
	Ativo               bool      `json:"ativo"`
	AgendamentoAtivo    bool      `json:"agendamento_ativo"`
	HorarioAgendamento  string    `json:"horario_agendamento"` // HH:MM
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
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
		var horario string
		err = db.QueryRow(`
			SELECT id, company_id, cnpj_matriz, client_id, client_secret, COALESCE(ambiente, 'producao'), ativo,
			       COALESCE(agendamento_ativo, false), COALESCE(TO_CHAR(horario_agendamento, 'HH24:MI'), '06:00'),
			       created_at, updated_at
			FROM rfb_credentials
			WHERE company_id = $1
		`, companyID).Scan(&cred.ID, &cred.CompanyID, &cred.CNPJMatriz, &cred.ClientID, &cred.ClientSecret, &cred.Ambiente, &cred.Ativo,
			&cred.AgendamentoAtivo, &horario, &cred.CreatedAt, &cred.UpdatedAt)
		cred.HorarioAgendamento = horario

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
		var horario string
		err = db.QueryRow(`
			SELECT id, company_id, cnpj_matriz, client_id, client_secret, COALESCE(ambiente, 'producao'), ativo,
			       COALESCE(agendamento_ativo, false), COALESCE(TO_CHAR(horario_agendamento, 'HH24:MI'), '06:00'),
			       created_at, updated_at
			FROM rfb_credentials WHERE id = $1
		`, id).Scan(&cred.ID, &cred.CompanyID, &cred.CNPJMatriz, &cred.ClientID, &cred.ClientSecret, &cred.Ambiente, &cred.Ativo,
			&cred.AgendamentoAtivo, &horario, &cred.CreatedAt, &cred.UpdatedAt)
		cred.HorarioAgendamento = horario
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

// UpdateRFBScheduleHandler updates agendamento_ativo and horario_agendamento for the credential
func UpdateRFBScheduleHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if r.Method != http.MethodPatch {
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
			AgendamentoAtivo   bool   `json:"agendamento_ativo"`
			HorarioAgendamento string `json:"horario_agendamento"` // HH:MM
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate HH:MM format
		if len(req.HorarioAgendamento) != 5 || req.HorarioAgendamento[2] != ':' {
			req.HorarioAgendamento = "06:00"
		}

		_, err = db.Exec(`
			UPDATE rfb_credentials
			SET agendamento_ativo = $1, horario_agendamento = $2::TIME, updated_at = CURRENT_TIMESTAMP
			WHERE company_id = $3
		`, req.AgendamentoAtivo, req.HorarioAgendamento, companyID)
		if err != nil {
			http.Error(w, "Erro ao atualizar agendamento: "+err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"agendamento_ativo":   req.AgendamentoAtivo,
			"horario_agendamento": req.HorarioAgendamento,
			"message":             "Agendamento atualizado com sucesso",
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
