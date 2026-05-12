package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type CGIBSCredential struct {
	ID                 string    `json:"id"`
	CompanyID          string    `json:"company_id"`
	CNPJMatriz         string    `json:"cnpj_matriz"`
	ClientID           string    `json:"client_id"`
	ClientSecret       string    `json:"client_secret"`
	Ambiente           string    `json:"ambiente"`
	Ativo              bool      `json:"ativo"`
	AgendamentoAtivo   bool      `json:"agendamento_ativo"`
	HorarioAgendamento string    `json:"horario_agendamento"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func GetCGIBSCredentialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
			return
		}

		var cred CGIBSCredential
		var horario string
		err = db.QueryRow(`
			SELECT id, company_id, cnpj_matriz, client_id, client_secret,
			       COALESCE(ambiente,'piloto'), ativo,
			       COALESCE(agendamento_ativo, false),
			       COALESCE(TO_CHAR(horario_agendamento,'HH24:MI'), '06:00'),
			       created_at, updated_at
			FROM cgibs_credentials WHERE company_id = $1
		`, companyID).Scan(
			&cred.ID, &cred.CompanyID, &cred.CNPJMatriz, &cred.ClientID, &cred.ClientSecret,
			&cred.Ambiente, &cred.Ativo, &cred.AgendamentoAtivo, &horario,
			&cred.CreatedAt, &cred.UpdatedAt,
		)
		cred.HorarioAgendamento = horario

		if err == sql.ErrNoRows {
			json.NewEncoder(w).Encode(map[string]interface{}{"credential": nil})
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar credencial CGIBS", err, "[CGIBSCredentials]")
			return
		}
		if len(cred.ClientSecret) > 4 {
			cred.ClientSecret = strings.Repeat("*", len(cred.ClientSecret)-4) + cred.ClientSecret[len(cred.ClientSecret)-4:]
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"credential": cred})
	}
}

func SaveCGIBSCredentialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
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

		req.CNPJMatriz = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(
			strings.TrimSpace(req.CNPJMatriz), ".", ""), "/", ""), "-", "")
		req.ClientID = strings.TrimSpace(req.ClientID)
		req.ClientSecret = strings.TrimSpace(req.ClientSecret)
		if req.Ambiente != "piloto" && req.Ambiente != "producao" {
			req.Ambiente = "piloto"
		}
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

		var id string
		err = db.QueryRow(`
			INSERT INTO cgibs_credentials (company_id, cnpj_matriz, client_id, client_secret, ambiente, ativo)
			VALUES ($1, $2, $3, $4, $5, true)
			ON CONFLICT (company_id)
			DO UPDATE SET cnpj_matriz=$2, client_id=$3, client_secret=$4, ambiente=$5, ativo=true, updated_at=NOW()
			RETURNING id
		`, companyID, req.CNPJMatriz, req.ClientID, req.ClientSecret, req.Ambiente).Scan(&id)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao salvar credencial CGIBS", err, "[CGIBSCredentials]")
			return
		}

		var cred CGIBSCredential
		var horario string
		db.QueryRow(`
			SELECT id, company_id, cnpj_matriz, client_id, client_secret,
			       COALESCE(ambiente,'piloto'), ativo,
			       COALESCE(agendamento_ativo, false),
			       COALESCE(TO_CHAR(horario_agendamento,'HH24:MI'), '06:00'),
			       created_at, updated_at
			FROM cgibs_credentials WHERE id = $1
		`, id).Scan(
			&cred.ID, &cred.CompanyID, &cred.CNPJMatriz, &cred.ClientID, &cred.ClientSecret,
			&cred.Ambiente, &cred.Ativo, &cred.AgendamentoAtivo, &horario,
			&cred.CreatedAt, &cred.UpdatedAt,
		)
		cred.HorarioAgendamento = horario
		if len(cred.ClientSecret) > 4 {
			cred.ClientSecret = strings.Repeat("*", len(cred.ClientSecret)-4) + cred.ClientSecret[len(cred.ClientSecret)-4:]
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"credential": cred,
			"message":    "Credenciais CGIBS salvas com sucesso",
		})
	}
}

func UpdateCGIBSScheduleHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
			return
		}

		var req struct {
			AgendamentoAtivo   bool   `json:"agendamento_ativo"`
			HorarioAgendamento string `json:"horario_agendamento"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if len(req.HorarioAgendamento) != 5 || req.HorarioAgendamento[2] != ':' {
			req.HorarioAgendamento = "06:00"
		}
		db.Exec(`
			UPDATE cgibs_credentials
			SET agendamento_ativo=$1, horario_agendamento=$2::TIME, updated_at=NOW()
			WHERE company_id=$3
		`, req.AgendamentoAtivo, req.HorarioAgendamento, companyID)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"agendamento_ativo":   req.AgendamentoAtivo,
			"horario_agendamento": req.HorarioAgendamento,
			"message":             "Agendamento atualizado com sucesso",
		})
	}
}

func DeleteCGIBSCredentialHandler(db *sql.DB) http.HandlerFunc {
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
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
			return
		}
		result, err := db.Exec("DELETE FROM cgibs_credentials WHERE company_id=$1", companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao excluir credencial CGIBS", err, "[CGIBSCredentials]")
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
