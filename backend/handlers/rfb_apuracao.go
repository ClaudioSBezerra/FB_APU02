package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
)

// RFBRequest represents a request to the RFB API
type RFBRequest struct {
	ID           string      `json:"id"`
	CompanyID    string      `json:"company_id"`
	CNPJBase     string      `json:"cnpj_base"`
	Tiquete      string      `json:"tiquete,omitempty"`
	Status       string      `json:"status"`
	Ambiente     string      `json:"ambiente"`
	ErrorCode    *string     `json:"error_code,omitempty"`
	ErrorMessage *string     `json:"error_message,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	Resumo       *RFBResumo  `json:"resumo,omitempty"`
}

// RFBResumo represents the summary of a CBS assessment
type RFBResumo struct {
	ID               string  `json:"id"`
	RequestID        string  `json:"request_id"`
	DataApuracao     string  `json:"data_apuracao"`
	TotalDebitos     int     `json:"total_debitos"`
	ValorCBSTotal    float64 `json:"valor_cbs_total"`
	ValorCBSExtinto  float64 `json:"valor_cbs_extinto"`
	ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
	TotalCorrente    int     `json:"total_corrente"`
	TotalAjuste      int     `json:"total_ajuste"`
	TotalExtemporaneo int    `json:"total_extemporaneo"`
}

// RFBDebitoRow represents a normalized debit row for the frontend
type RFBDebitoRow struct {
	ID                 string   `json:"id"`
	TipoApuracao       string   `json:"tipo_apuracao"`
	ModeloDfe          string   `json:"modelo_dfe"`
	Serie              string   `json:"serie"`
	NumeroDfe          string   `json:"numero_dfe"`
	ChaveDfe           string   `json:"chave_dfe"`
	DataDfeEmissao     *string  `json:"data_dfe_emissao"`
	DataApuracao       string   `json:"data_apuracao"`
	NiEmitente         string   `json:"ni_emitente"`
	NiAdquirente       string   `json:"ni_adquirente"`
	ValorDocumento     *float64 `json:"valor_documento"`
	ValorCBSTotal      float64  `json:"valor_cbs_total"`
	ValorCBSExtinto    float64  `json:"valor_cbs_extinto"`
	ValorCBSNaoExtinto float64  `json:"valor_cbs_nao_extinto"`
	SituacaoDebito     string   `json:"situacao_debito"`
}

// SolicitarApuracaoHandler triggers a new CBS assessment request to the RFB API (manual — uses up to 2 slots/day)
func SolicitarApuracaoHandler(db *sql.DB) http.HandlerFunc {
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

		// Manual requests: full 2/day limit (timezone-correct)
		var todayCount int
		db.QueryRow(`
			SELECT COUNT(*) FROM rfb_requests
			WHERE company_id = $1 AND status != 'error'
			  AND created_at >= CURRENT_DATE AT TIME ZONE 'America/Sao_Paulo'
		`, companyID).Scan(&todayCount)
		if todayCount >= 2 {
			http.Error(w, "Limite diário atingido (máximo 2 solicitações por dia)", http.StatusTooManyRequests)
			return
		}

		if err := services.SolicitarApuracaoParaEmpresa(db, companyID); err != nil {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "credenciais RFB não encontradas"):
				http.Error(w, "Credenciais RFB não configuradas. Configure em Conectar Receita Federal > Credenciais API.", http.StatusBadRequest)
			case strings.Contains(msg, "slot automático já utilizado"):
				http.Error(w, "Limite diário atingido (máximo 2 solicitações por dia)", http.StatusTooManyRequests)
			case strings.Contains(msg, "TOKEN_ERROR"):
				http.Error(w, "Erro ao obter token da RFB: "+msg, http.StatusBadGateway)
			case strings.Contains(msg, "REQUEST_ERROR"):
				http.Error(w, "Erro ao solicitar apuração: "+msg, http.StatusBadGateway)
			default:
				http.Error(w, msg, http.StatusInternalServerError)
			}
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "requested",
			"message": "Solicitação enviada à Receita Federal. Aguarde o retorno via webhook.",
		})
	}
}

// DownloadManualHandler manually triggers the download for a request that has a tiquete
func DownloadManualHandler(db *sql.DB) http.HandlerFunc {
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
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Verify request belongs to company and has a tiquete
		var requestID, tiquete, status string
		var tiqueteDownload *string
		err = db.QueryRow(`
			SELECT id, COALESCE(tiquete, ''), status, tiquete_download FROM rfb_requests
			WHERE id = $1 AND company_id = $2
		`, req.RequestID, companyID).Scan(&requestID, &tiquete, &status, &tiqueteDownload)
		if err == sql.ErrNoRows {
			http.Error(w, "Solicitação não encontrada", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Erro ao buscar solicitação: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if tiquete == "" {
			http.Error(w, "Solicitação sem tíquete - não é possível fazer download", http.StatusBadRequest)
			return
		}

		if tiqueteDownload == nil || *tiqueteDownload == "" {
			http.Error(w, "Tíquete de download ainda não recebido — aguarde o webhook da RFB confirmar o processamento", http.StatusBadRequest)
			return
		}

		if status == "completed" {
			http.Error(w, "Download já realizado para esta solicitação", http.StatusConflict)
			return
		}

		if status == "downloading" {
			http.Error(w, "Download já está em andamento", http.StatusConflict)
			return
		}

		// Trigger download in background
		go func() {
			rfbClient := services.NewRFBClient()
			if err := services.ProcessarDownloadRFB(db, rfbClient, requestID); err != nil {
				log.Printf("[RFB Manual Download] Error processing request %s: %v", requestID, err)
			}
		}()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "downloading",
			"message": "Download iniciado. Acompanhe o status na lista de solicitações.",
		})
	}
}

// DeleteRequestHandler removes a single RFB request (only if status = error).
func DeleteRequestHandler(db *sql.DB) http.HandlerFunc {
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		requestID := strings.TrimPrefix(r.URL.Path, "/api/rfb/apuracao/")
		requestID = strings.TrimSpace(requestID)

		res, err := db.Exec(`
			DELETE FROM rfb_requests
			WHERE id = $1 AND company_id = $2 AND status = 'error'
		`, requestID, companyID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			http.Error(w, "Registro não encontrado ou não pode ser removido (apenas erros podem ser excluídos)", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ClearErrorsHandler removes all error-status RFB requests for the company.
func ClearErrorsHandler(db *sql.DB) http.HandlerFunc {
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res, err := db.Exec(`DELETE FROM rfb_requests WHERE company_id = $1 AND status = 'error'`, companyID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows, _ := res.RowsAffected()
		json.NewEncoder(w).Encode(map[string]interface{}{"deleted": rows})
	}
}

// ReprocessHandler re-parses the raw JSON already stored in the DB for a request.
// Useful when download succeeded but JSON parse failed (e.g. datetime format issue).
func ReprocessHandler(db *sql.DB) http.HandlerFunc {
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
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Verify ownership
		var exists bool
		err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM rfb_requests WHERE id = $1 AND company_id = $2)`,
			req.RequestID, companyID).Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Solicitação não encontrada", http.StatusNotFound)
			return
		}

		go func() {
			if err := services.ReprocessarRawJSON(db, req.RequestID); err != nil {
				log.Printf("[RFB Reprocess] Error: %v", err)
			}
		}()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "reprocessing",
			"message": "Reprocessamento iniciado a partir do JSON salvo.",
		})
	}
}

// RFBWebhookHandler receives callbacks from the RFB API (PUBLIC - no JWT auth)
func RFBWebhookHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("[RFB Webhook] Error reading body: %v", err)
			http.Error(w, "Error reading body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		log.Printf("[RFB Webhook] ===== CALLBACK RECEIVED =====")
		log.Printf("[RFB Webhook] Method: %s | RemoteAddr: %s", r.Method, r.RemoteAddr)
		log.Printf("[RFB Webhook] Headers: %v", r.Header)
		log.Printf("[RFB Webhook] Body: %s", string(body))
		log.Printf("[RFB Webhook] ==============================")

		// Parse webhook payload - try to extract tiquete
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("[RFB Webhook] Error parsing JSON: %v", err)
			// Return 200 even on parse error so RFB doesn't retry indefinitely
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "invalid JSON"})
			return
		}

		log.Printf("[RFB Webhook] Parsed fields: %v", func() []string {
			keys := make([]string, 0, len(payload))
			for k := range payload {
				keys = append(keys, k)
			}
			return keys
		}())

		// RFB sends two distinct tíquetes:
		//   tiqueteSolicitacao = identifies the original request (stored in rfb_requests.tiquete)
		//   tiqueteDownload    = the tíquete to use when calling the download endpoint
		tiqueteSolicitacao, _ := payload["tiqueteSolicitacao"].(string)
		tiqueteDownload, _ := payload["tiqueteDownload"].(string)

		log.Printf("[RFB Webhook] tiqueteSolicitacao: %s | tiqueteDownload: %s",
			tiqueteSolicitacao, tiqueteDownload)

		if tiqueteSolicitacao == "" || tiqueteDownload == "" {
			log.Printf("[RFB Webhook] Missing required tíquetes in payload — fields: %v", payload)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "missing tiqueteSolicitacao or tiqueteDownload"})
			return
		}

		// Find the request by tiqueteSolicitacao
		var requestID string
		err = db.QueryRow(`
			SELECT id FROM rfb_requests WHERE tiquete = $1 AND status = 'requested'
		`, tiqueteSolicitacao).Scan(&requestID)
		if err != nil {
			log.Printf("[RFB Webhook] Request not found for tiqueteSolicitacao %s: %v", tiqueteSolicitacao, err)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "request not found"})
			return
		}

		// Save tiqueteDownload and update status
		_, err = db.Exec(`
			UPDATE rfb_requests
			SET status = 'webhook_received', tiquete_download = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
		`, tiqueteDownload, requestID)
		if err != nil {
			log.Printf("[RFB Webhook] Error updating status/tiqueteDownload: %v", err)
		}

		log.Printf("[RFB Webhook] Request %s updated — tiqueteDownload saved, triggering download", requestID)

		// Trigger async download and processing
		go func() {
			rfbClient := services.NewRFBClient()
			if err := services.ProcessarDownloadRFB(db, rfbClient, requestID); err != nil {
				log.Printf("[RFB Webhook] Error processing download for request %s: %v", requestID, err)
			}
		}()

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "processing"})
	}
}

// StatusApuracaoHandler returns the list of RFB requests for the company
func StatusApuracaoHandler(db *sql.DB) http.HandlerFunc {
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
			SELECT r.id, r.company_id, r.cnpj_base, COALESCE(r.tiquete, ''), r.status, r.ambiente,
				r.error_code, r.error_message, r.created_at, r.updated_at,
				res.id, res.request_id, COALESCE(res.data_apuracao, ''), res.total_debitos,
				res.valor_cbs_total, res.valor_cbs_extinto, res.valor_cbs_nao_extinto,
				res.total_corrente, res.total_ajuste, res.total_extemporaneo
			FROM rfb_requests r
			LEFT JOIN rfb_resumo res ON res.request_id = r.id
			WHERE r.company_id = $1
			ORDER BY r.created_at DESC
			LIMIT 20
		`, companyID)
		if err != nil {
			http.Error(w, "Error querying requests: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var requests []RFBRequest
		for rows.Next() {
			var req RFBRequest
			var resID, resReqID, resData sql.NullString
			var resTotalDebitos, resCorrente, resAjuste, resExtemp sql.NullInt64
			var resCBSTotal, resCBSExtinto, resCBSNaoExtinto sql.NullFloat64

			if err := rows.Scan(
				&req.ID, &req.CompanyID, &req.CNPJBase, &req.Tiquete, &req.Status, &req.Ambiente,
				&req.ErrorCode, &req.ErrorMessage, &req.CreatedAt, &req.UpdatedAt,
				&resID, &resReqID, &resData, &resTotalDebitos,
				&resCBSTotal, &resCBSExtinto, &resCBSNaoExtinto,
				&resCorrente, &resAjuste, &resExtemp,
			); err != nil {
				http.Error(w, "Error scanning request: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if resID.Valid {
				req.Resumo = &RFBResumo{
					ID:                 resID.String,
					RequestID:          resReqID.String,
					DataApuracao:       resData.String,
					TotalDebitos:       int(resTotalDebitos.Int64),
					ValorCBSTotal:      resCBSTotal.Float64,
					ValorCBSExtinto:    resCBSExtinto.Float64,
					ValorCBSNaoExtinto: resCBSNaoExtinto.Float64,
					TotalCorrente:      int(resCorrente.Int64),
					TotalAjuste:        int(resAjuste.Int64),
					TotalExtemporaneo:  int(resExtemp.Int64),
				}
			}
			requests = append(requests, req)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"requests": requests,
			"count":    len(requests),
		})
	}
}

// DetalheApuracaoHandler returns details of a specific RFB request (GET) or deletes it (DELETE).
func DetalheApuracaoHandler(db *sql.DB) http.HandlerFunc {
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

		// Extract request ID from path
		requestID := strings.TrimPrefix(r.URL.Path, "/api/rfb/apuracao/")
		requestID = strings.TrimSpace(requestID)

		// Handle DELETE — remove error records only
		if r.Method == http.MethodDelete {
			res, err := db.Exec(`DELETE FROM rfb_requests WHERE id = $1 AND company_id = $2 AND status = 'error'`,
				requestID, companyID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rows, _ := res.RowsAffected()
			if rows == 0 {
				http.Error(w, "Registro não encontrado ou não pode ser removido", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if requestID == "" || requestID == "status" || requestID == "solicitar" {
			http.Error(w, "Invalid request ID", http.StatusBadRequest)
			return
		}

		// Fetch request (verify company ownership)
		var req RFBRequest
		err = db.QueryRow(`
			SELECT id, company_id, cnpj_base, COALESCE(tiquete, ''), status, ambiente,
				error_code, error_message, created_at, updated_at
			FROM rfb_requests
			WHERE id = $1 AND company_id = $2
		`, requestID, companyID).Scan(&req.ID, &req.CompanyID, &req.CNPJBase, &req.Tiquete, &req.Status, &req.Ambiente,
			&req.ErrorCode, &req.ErrorMessage, &req.CreatedAt, &req.UpdatedAt)
		if err == sql.ErrNoRows {
			http.Error(w, "Solicitação não encontrada", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Error querying request: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch summary if available
		var resumo *RFBResumo
		var r2 RFBResumo
		err = db.QueryRow(`
			SELECT id, request_id, COALESCE(data_apuracao, ''), total_debitos,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
				total_corrente, total_ajuste, total_extemporaneo
			FROM rfb_resumo WHERE request_id = $1
		`, requestID).Scan(&r2.ID, &r2.RequestID, &r2.DataApuracao, &r2.TotalDebitos,
			&r2.ValorCBSTotal, &r2.ValorCBSExtinto, &r2.ValorCBSNaoExtinto,
			&r2.TotalCorrente, &r2.TotalAjuste, &r2.TotalExtemporaneo)
		if err == nil {
			resumo = &r2
		}

		// Query params — pagination + filters
		qp := r.URL.Query()
		page := 1
		pageSize := 100
		if p, e := strconv.Atoi(qp.Get("page")); e == nil && p > 0 {
			page = p
		}
		if ps, e := strconv.Atoi(qp.Get("page_size")); e == nil && ps > 0 && ps <= 500 {
			pageSize = ps
		}

		filterModelo := qp.Get("modelo")
		filterDataDe := qp.Get("data_de")
		filterDataAte := qp.Get("data_ate")
		filterChave := qp.Get("chave")
		filterAdquir := qp.Get("ni_adquirente")
		filterEmit := qp.Get("ni_emitente")

		// Build dynamic WHERE for rfb_debitos
		args := []interface{}{requestID}
		idx := 2
		where := "WHERE d.request_id = $1"

		if filterModelo != "" {
			where += fmt.Sprintf(
				" AND (d.modelo_dfe = $%d OR (COALESCE(d.modelo_dfe,'')='' AND length(d.chave_dfe)=44 AND SUBSTRING(d.chave_dfe,21,2)=$%d))",
				idx, idx)
			args = append(args, filterModelo)
			idx++
		}
		if filterDataDe != "" {
			where += fmt.Sprintf(" AND d.data_dfe_emissao >= $%d::date", idx)
			args = append(args, filterDataDe)
			idx++
		}
		if filterDataAte != "" {
			where += fmt.Sprintf(" AND d.data_dfe_emissao <= $%d::date", idx)
			args = append(args, filterDataAte)
			idx++
		}
		if filterChave != "" {
			where += fmt.Sprintf(" AND d.chave_dfe ILIKE $%d", idx)
			args = append(args, "%"+filterChave+"%")
			idx++
		}
		if filterAdquir != "" {
			where += fmt.Sprintf(" AND d.ni_adquirente LIKE $%d", idx)
			args = append(args, "%"+filterAdquir+"%")
			idx++
		}
		if filterEmit != "" {
			where += fmt.Sprintf(" AND d.ni_emitente = $%d", idx)
			args = append(args, filterEmit)
			idx++
		}

		// COUNT with filters
		var totalDebits int
		db.QueryRow("SELECT COUNT(*) FROM rfb_debitos d "+where, args...).Scan(&totalDebits)

		totalPages := (totalDebits + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}

		// Fetch debits — paginated
		// - numero_dfe: usa valor da RFB; se vazio, extrai da chave (pos 26-34)
		// - serie: extrai da chave (pos 23-25)
		// - valor_documento: JOIN com nfe_saidas pela chave para obter v_nf
		offset := (page - 1) * pageSize
		selectQ := `
			SELECT d.id,
				d.tipo_apuracao,
				CASE
				WHEN COALESCE(d.modelo_dfe, '') != '' THEN d.modelo_dfe
				WHEN length(d.chave_dfe) = 44 THEN SUBSTRING(d.chave_dfe, 21, 2)
				ELSE ''
			END AS modelo_dfe,
				CASE
					WHEN length(d.chave_dfe) = 44 THEN SUBSTRING(d.chave_dfe, 23, 3)
					ELSE ''
				END AS serie,
				CASE
					WHEN COALESCE(d.numero_dfe, '') != '' THEN d.numero_dfe
					WHEN length(d.chave_dfe) = 44 THEN LTRIM(SUBSTRING(d.chave_dfe, 26, 9), '0')
					ELSE ''
				END AS numero_dfe,
				COALESCE(d.chave_dfe, ''),
				CASE WHEN d.data_dfe_emissao IS NOT NULL THEN to_char(d.data_dfe_emissao, 'YYYY-MM-DD"T"HH24:MI:SS"Z"') END,
				COALESCE(d.data_apuracao, ''),
				COALESCE(d.ni_emitente, ''),
				COALESCE(d.ni_adquirente, ''),
				n.v_nf,
				COALESCE(d.valor_cbs_total, 0),
				COALESCE(d.valor_cbs_extinto, 0),
				COALESCE(d.valor_cbs_nao_extinto, 0),
				COALESCE(d.situacao_debito, '')
			FROM rfb_debitos d
			LEFT JOIN nfe_saidas n ON n.chave_nfe = d.chave_dfe AND n.company_id = d.company_id
			` + where + fmt.Sprintf(`
			ORDER BY d.tipo_apuracao, d.data_apuracao
			LIMIT $%d OFFSET $%d`, idx, idx+1)
		pageArgs := append(args, pageSize, offset)
		debitRows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			http.Error(w, "Error querying debits: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer debitRows.Close()

		var debitos []RFBDebitoRow
		modelCount := map[string]int{}
		for debitRows.Next() {
			var d RFBDebitoRow
			if err := debitRows.Scan(&d.ID, &d.TipoApuracao, &d.ModeloDfe,
				&d.Serie, &d.NumeroDfe,
				&d.ChaveDfe, &d.DataDfeEmissao, &d.DataApuracao,
				&d.NiEmitente, &d.NiAdquirente,
				&d.ValorDocumento,
				&d.ValorCBSTotal, &d.ValorCBSExtinto, &d.ValorCBSNaoExtinto,
				&d.SituacaoDebito); err != nil {
				log.Printf("[RFB Detail] Error scanning debit: %v", err)
				continue
			}
			modelCount[d.ModeloDfe]++
			debitos = append(debitos, d)
		}
		log.Printf("[RFB Detail] request=%s modelos encontrados: %v", requestID, modelCount)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"request": req,
			"resumo":  resumo,
			"debitos": debitos,
			"pagination": map[string]int{
				"page":        page,
				"page_size":   pageSize,
				"total":       totalDebits,
				"total_pages": totalPages,
			},
		})
	}
}
