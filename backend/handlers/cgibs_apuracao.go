package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type CGIBSRequest struct {
	ID              string     `json:"id"`
	CompanyID       string     `json:"company_id"`
	CNPJBase        string     `json:"cnpj_base"`
	Tiquete         string     `json:"tiquete"`
	TiqueteDownload string     `json:"tiquete_download"`
	Status          string     `json:"status"`
	Ambiente        string     `json:"ambiente"`
	ErrorCode       string     `json:"error_code"`
	ErrorMessage    string     `json:"error_message"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Resumo          *CGIBSResumo `json:"resumo,omitempty"`
}

type CGIBSResumo struct {
	DataApuracao       string  `json:"data_apuracao"`
	TotalDebitos       int     `json:"total_debitos"`
	ValorIBSTotal      float64 `json:"valor_ibs_total"`
	ValorIBSUF         float64 `json:"valor_ibs_uf"`
	ValorIBSMun        float64 `json:"valor_ibs_mun"`
	ValorIBSNaoExtinto float64 `json:"valor_ibs_nao_extinto"`
	TotalCorrente      int     `json:"total_corrente"`
	TotalAjuste        int     `json:"total_ajuste"`
}

// StatusCGIBSApuracaoHandler — GET /api/cgibs/apuracao/status
func StatusCGIBSApuracaoHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[StatusCGIBSApuracao]")
			return
		}

		rows, err := db.Query(`
			SELECT r.id, r.company_id, r.cnpj_base,
			       COALESCE(r.tiquete,''), COALESCE(r.tiquete_download,''),
			       r.status, r.ambiente,
			       COALESCE(r.error_code,''), COALESCE(r.error_message,''),
			       r.created_at, r.updated_at
			FROM cgibs_requests r
			WHERE r.company_id = $1
			ORDER BY r.created_at DESC
			LIMIT 50
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar solicitações CGIBS", err, "[StatusCGIBSApuracao]")
			return
		}
		defer rows.Close()

		var requests []CGIBSRequest
		for rows.Next() {
			var req CGIBSRequest
			if err := rows.Scan(
				&req.ID, &req.CompanyID, &req.CNPJBase,
				&req.Tiquete, &req.TiqueteDownload,
				&req.Status, &req.Ambiente,
				&req.ErrorCode, &req.ErrorMessage,
				&req.CreatedAt, &req.UpdatedAt,
			); err != nil {
				continue
			}
			// Carregar resumo se completed
			if req.Status == "completed" {
				var res CGIBSResumo
				db.QueryRow(`
					SELECT COALESCE(data_apuracao,''), total_debitos,
					       valor_ibs_total, valor_ibs_uf, valor_ibs_mun, valor_ibs_nao_extinto,
					       total_corrente, total_ajuste
					FROM cgibs_resumo WHERE request_id=$1 LIMIT 1
				`, req.ID).Scan(
					&res.DataApuracao, &res.TotalDebitos,
					&res.ValorIBSTotal, &res.ValorIBSUF, &res.ValorIBSMun, &res.ValorIBSNaoExtinto,
					&res.TotalCorrente, &res.TotalAjuste,
				)
				req.Resumo = &res
			}
			requests = append(requests, req)
		}
		if requests == nil {
			requests = []CGIBSRequest{}
		}

		// Verificar se há credencial com agendamento
		var agendamentoAtivo bool
		var horarioAgendamento string
		db.QueryRow(`
			SELECT COALESCE(agendamento_ativo,false), COALESCE(TO_CHAR(horario_agendamento,'HH24:MI'),'06:00')
			FROM cgibs_credentials WHERE company_id=$1
		`, companyID).Scan(&agendamentoAtivo, &horarioAgendamento)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"requests":            requests,
			"agendamento_ativo":   agendamentoAtivo,
			"horario_agendamento": horarioAgendamento,
		})
	}
}

// SolicitarCGIBSApuracaoHandler — POST /api/cgibs/apuracao/solicitar
// API CGIBS em fase piloto — retorna erro informativo enquanto não disponível.
func SolicitarCGIBSApuracaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "API CGIBS em fase piloto",
			"detail": "A integração com o Comitê Gestor do IBS (CGIBS) ainda está em fase piloto. " +
				"O acesso está restrito a empresas selecionadas. A integração completa será " +
				"disponibilizada assim que o CGIBS liberar o acesso irrestrito.",
			"portal": "https://www.servicos.cgibs.gov.br/",
		})
	}
}

// ClearErrorsCGIBSHandler — DELETE /api/cgibs/apuracao/clear-errors
func ClearErrorsCGIBSHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[ClearErrorsCGIBS]")
			return
		}
		db.Exec(`DELETE FROM cgibs_requests WHERE company_id=$1 AND status='error'`, companyID)
		json.NewEncoder(w).Encode(map[string]string{"message": "Erros removidos"})
	}
}

// DetalheCGIBSHandler — DELETE /api/cgibs/apuracao/{id}
func DetalheCGIBSHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[DetalheCGIBS]")
			return
		}

		id := r.URL.Path[len("/api/cgibs/apuracao/"):]
		if r.Method == http.MethodDelete {
			db.Exec(`DELETE FROM cgibs_requests WHERE id=$1 AND company_id=$2`, id, companyID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
