package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fb_apu02/services"

	jwt "github.com/golang-jwt/jwt/v5"
)

type RFBCreditoRequest struct {
	ID              string             `json:"id"`
	CompanyID       string             `json:"company_id"`
	CNPJBase        string             `json:"cnpj_base"`
	Tiquete         string             `json:"tiquete,omitempty"`
	TiqueteDownload *string            `json:"tiquete_download,omitempty"`
	Status          string             `json:"status"`
	Ambiente        string             `json:"ambiente"`
	ErrorCode       *string            `json:"error_code,omitempty"`
	ErrorMessage    *string            `json:"error_message,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Resumo          *RFBCreditoResumo  `json:"resumo,omitempty"`
}

type RFBCreditoResumo struct {
	ID                 string  `json:"id"`
	RequestID          string  `json:"request_id"`
	DataApuracao       string  `json:"data_apuracao"`
	TotalCreditos      int     `json:"total_creditos"`
	ValorCBSTotal      float64 `json:"valor_cbs_total"`
	ValorCBSExtinto    float64 `json:"valor_cbs_extinto"`
	ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
	TotalCorrente      int     `json:"total_corrente"`
	TotalAjuste        int     `json:"total_ajuste"`
}

// SolicitarCreditosHandler — POST /api/rfb/creditos/solicitar
func SolicitarCreditosHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBCreditos]")
			return
		}

		var todayCount int
		db.QueryRow(`
			SELECT COUNT(*) FROM rfb_requests
			WHERE company_id = $1
			  AND tipo = 'credito'
			  AND created_at >= CURRENT_DATE AT TIME ZONE 'America/Sao_Paulo'
		`, companyID).Scan(&todayCount)
		if todayCount >= 2 {
			http.Error(w, "Limite diário atingido (máximo 2 solicitações de créditos por dia)", http.StatusTooManyRequests)
			return
		}

		if err := services.SolicitarCreditoParaEmpresa(db, companyID); err != nil {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "credenciais RFB não encontradas"):
				http.Error(w, "Credenciais RFB não configuradas.", http.StatusBadRequest)
			case strings.Contains(msg, "RATE_LIMIT"):
				http.Error(w, msg, http.StatusTooManyRequests)
			case strings.Contains(msg, "TOKEN_ERROR"):
				http.Error(w, "Erro ao obter token da RFB: "+msg, http.StatusBadGateway)
			case strings.Contains(msg, "REQUEST_ERROR"):
				http.Error(w, "Erro ao solicitar créditos: "+msg, http.StatusBadGateway)
			default:
				log.Printf("[RFBCreditos] Erro inesperado: %v", err)
				http.Error(w, "Erro ao solicitar créditos. Tente novamente.", http.StatusInternalServerError)
			}
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "requested",
			"message": "Solicitação de créditos CBS enviada à Receita Federal. Aguarde o retorno via webhook.",
		})
	}
}

// StatusCreditosHandler — GET /api/rfb/creditos/status
func StatusCreditosHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBCreditos]")
			return
		}

		// Créditos são extraídos da mesma importação de débitos — filtra por tipo='debito' concluídos
		rows, err := db.Query(`
			SELECT r.id, r.company_id, r.cnpj_base, COALESCE(r.tiquete, ''), r.tiquete_download,
				r.status, r.ambiente,
				r.error_code, r.error_message, r.created_at, r.updated_at,
				res.id, res.request_id, COALESCE(res.data_apuracao, ''), res.total_creditos,
				res.valor_cbs_total, res.valor_cbs_extinto, res.valor_cbs_nao_extinto,
				res.total_corrente, res.total_ajuste
			FROM rfb_requests r
			LEFT JOIN rfb_creditos_resumo res ON res.request_id = r.id
			WHERE r.company_id = $1 AND r.tipo = 'debito' AND r.status = 'completed'
			ORDER BY r.created_at DESC
			LIMIT 20
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar créditos", err, "[RFBCreditos]")
			return
		}
		defer rows.Close()

		var requests []RFBCreditoRequest
		for rows.Next() {
			var req RFBCreditoRequest
			var resID, resReqID, resData sql.NullString
			var resTotalCred, resCorrente, resAjuste sql.NullInt64
			var resCBSTotal, resCBSExtinto, resCBSNaoExtinto sql.NullFloat64

			if err := rows.Scan(
				&req.ID, &req.CompanyID, &req.CNPJBase, &req.Tiquete, &req.TiqueteDownload,
				&req.Status, &req.Ambiente,
				&req.ErrorCode, &req.ErrorMessage, &req.CreatedAt, &req.UpdatedAt,
				&resID, &resReqID, &resData, &resTotalCred,
				&resCBSTotal, &resCBSExtinto, &resCBSNaoExtinto,
				&resCorrente, &resAjuste,
			); err != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao ler crédito", err, "[RFBCreditos]")
				return
			}
			if resID.Valid {
				req.Resumo = &RFBCreditoResumo{
					ID:                 resID.String,
					RequestID:          resReqID.String,
					DataApuracao:       resData.String,
					TotalCreditos:      int(resTotalCred.Int64),
					ValorCBSTotal:      resCBSTotal.Float64,
					ValorCBSExtinto:    resCBSExtinto.Float64,
					ValorCBSNaoExtinto: resCBSNaoExtinto.Float64,
					TotalCorrente:      int(resCorrente.Int64),
					TotalAjuste:        int(resAjuste.Int64),
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

// DownloadManualCreditosHandler — POST /api/rfb/creditos/download
func DownloadManualCreditosHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBCreditos]")
			return
		}

		var req struct {
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		var requestID, tiquete, status string
		var tiqueteDownload *string
		err = db.QueryRow(`
			SELECT id, COALESCE(tiquete, ''), status, tiquete_download
			FROM rfb_requests
			WHERE id = $1 AND company_id = $2 AND tipo = 'credito'
		`, req.RequestID, companyID).Scan(&requestID, &tiquete, &status, &tiqueteDownload)
		if err == sql.ErrNoRows {
			http.Error(w, "Solicitação não encontrada", http.StatusNotFound)
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar solicitação de crédito", err, "[RFBCreditos]")
			return
		}

		if tiqueteDownload == nil || *tiqueteDownload == "" {
			http.Error(w, "Tíquete de download ainda não recebido — aguarde o webhook", http.StatusBadRequest)
			return
		}
		if status == "completed" {
			http.Error(w, "Download já realizado", http.StatusConflict)
			return
		}
		if status == "downloading" {
			http.Error(w, "Download já está em andamento", http.StatusConflict)
			return
		}

		go func() {
			rfbClient := services.NewRFBClient()
			if err := services.ProcessarDownloadCreditosRFB(db, rfbClient, requestID); err != nil {
				log.Printf("[RFB Creditos Manual Download] Error for request %s: %v", requestID, err)
			}
		}()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "downloading",
			"message": "Download de créditos iniciado. Acompanhe o status.",
		})
	}
}

// DeleteCreditoRequestHandler — DELETE /api/rfb/creditos/{id}
func DeleteCreditoRequestHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBCreditos]")
			return
		}

		requestID := strings.TrimPrefix(r.URL.Path, "/api/rfb/creditos/")
		requestID = strings.TrimSpace(requestID)

		res, err := db.Exec(`
			DELETE FROM rfb_requests
			WHERE id = $1 AND company_id = $2 AND tipo = 'credito' AND status = 'error'
		`, requestID, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao excluir solicitação de crédito", err, "[RFBCreditos]")
			return
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			http.Error(w, "Registro não encontrado ou não pode ser removido", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ListarCreditosHandler — GET /api/rfb/creditos/lista
func ListarCreditosHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBCreditos]")
			return
		}

		q := r.URL.Query()
		situacao := q.Get("situacao")
		periodo := q.Get("periodo")
		niEmitente := q.Get("ni_emitente")
		pageStr := q.Get("page")
		page := 1
		if p, err2 := strconv.Atoi(pageStr); err2 == nil && p > 1 {
			page = p
		}
		const pageSize = 50
		offset := (page - 1) * pageSize

		args := []interface{}{companyID}
		where := []string{"company_id = $1"}
		idx := 2

		if situacao != "" {
			where = append(where, "situacao_credito = $"+strconv.Itoa(idx))
			args = append(args, situacao)
			idx++
		}
		if periodo != "" {
			where = append(where, "data_apuracao = $"+strconv.Itoa(idx))
			args = append(args, periodo)
			idx++
		}
		if niEmitente != "" {
			where = append(where, "ni_emitente ILIKE $"+strconv.Itoa(idx))
			args = append(args, "%"+niEmitente+"%")
			idx++
		}

		whereClause := strings.Join(where, " AND ")

		var total int
		db.QueryRow("SELECT COUNT(*) FROM rfb_creditos WHERE "+whereClause, args...).Scan(&total)

		args = append(args, pageSize, offset)
		limitIdx := strconv.Itoa(idx)
		offsetIdx := strconv.Itoa(idx + 1)

		rows, err := db.Query(`
			SELECT id, request_id, tipo_apuracao, modelo_dfe, numero_dfe, chave_dfe,
				data_dfe_emissao, data_apuracao, ni_emitente, ni_adquirente,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
				COALESCE(situacao_credito, ''), COALESCE(formas_extincao, ''), created_at
			FROM rfb_creditos
			WHERE `+whereClause+`
			ORDER BY data_dfe_emissao DESC NULLS LAST, created_at DESC
			LIMIT $`+limitIdx+` OFFSET $`+offsetIdx,
			args...,
		)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao listar créditos", err, "[RFBCreditos]")
			return
		}
		defer rows.Close()

		type CreditoItem struct {
			ID                 string   `json:"id"`
			RequestID          string   `json:"request_id"`
			TipoApuracao       string   `json:"tipo_apuracao"`
			ModeloDfe          string   `json:"modelo_dfe"`
			NumeroDfe          string   `json:"numero_dfe"`
			ChaveDfe           string   `json:"chave_dfe"`
			DataDfeEmissao     *string  `json:"data_dfe_emissao"`
			DataApuracao       string   `json:"data_apuracao"`
			NiEmitente         string   `json:"ni_emitente"`
			NiAdquirente       string   `json:"ni_adquirente"`
			ValorCBSTotal      float64  `json:"valor_cbs_total"`
			ValorCBSExtinto    float64  `json:"valor_cbs_extinto"`
			ValorCBSNaoExtinto float64  `json:"valor_cbs_nao_extinto"`
			SituacaoCredito    string   `json:"situacao_credito"`
			FormasExtincao     string   `json:"formas_extincao"`
			CreatedAt          time.Time `json:"created_at"`
		}

		var creditos []CreditoItem
		for rows.Next() {
			var c CreditoItem
			var dataEmissao sql.NullTime
			if err := rows.Scan(
				&c.ID, &c.RequestID, &c.TipoApuracao, &c.ModeloDfe, &c.NumeroDfe, &c.ChaveDfe,
				&dataEmissao, &c.DataApuracao, &c.NiEmitente, &c.NiAdquirente,
				&c.ValorCBSTotal, &c.ValorCBSExtinto, &c.ValorCBSNaoExtinto,
				&c.SituacaoCredito, &c.FormasExtincao, &c.CreatedAt,
			); err != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao ler item de crédito", err, "[RFBCreditos]")
				return
			}
			if dataEmissao.Valid {
				s := dataEmissao.Time.Format("2006-01-02")
				c.DataDfeEmissao = &s
			}
			creditos = append(creditos, c)
		}

		// Totais por situação
		type SituacaoTotais struct {
			Situacao    string  `json:"situacao"`
			Quantidade  int     `json:"quantidade"`
			ValorTotal  float64 `json:"valor_total"`
		}
		var totais []SituacaoTotais
		totaisRows, err2 := db.Query(`
			SELECT COALESCE(situacao_credito, 'SEM_SITUACAO'), COUNT(*), SUM(valor_cbs_total)
			FROM rfb_creditos
			WHERE company_id = $1
			GROUP BY situacao_credito
			ORDER BY situacao_credito
		`, companyID)
		if err2 == nil {
			defer totaisRows.Close()
			for totaisRows.Next() {
				var t SituacaoTotais
				totaisRows.Scan(&t.Situacao, &t.Quantidade, &t.ValorTotal)
				totais = append(totais, t)
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"creditos":   creditos,
			"total":      total,
			"page":       page,
			"page_size":  pageSize,
			"totais":     totais,
		})
	}
}

// ClearCreditosErrorsHandler — DELETE /api/rfb/creditos/clear-errors
func ClearCreditosErrorsHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBCreditos]")
			return
		}

		res, err := db.Exec(`
			DELETE FROM rfb_requests WHERE company_id = $1 AND tipo = 'credito' AND status = 'error'
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao limpar erros de créditos", err, "[RFBCreditos]")
			return
		}
		rows, _ := res.RowsAffected()
		json.NewEncoder(w).Encode(map[string]interface{}{"deleted": rows})
	}
}
