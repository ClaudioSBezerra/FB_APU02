package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
)

// RFBRequest represents a request to the RFB API
type RFBRequest struct {
	ID              string     `json:"id"`
	CompanyID       string     `json:"company_id"`
	CNPJBase        string     `json:"cnpj_base"`
	Tiquete         string     `json:"tiquete,omitempty"`
	TiqueteDownload *string    `json:"tiquete_download,omitempty"`
	Status          string     `json:"status"`
	Ambiente        string     `json:"ambiente"`
	ErrorCode       *string    `json:"error_code,omitempty"`
	ErrorMessage    *string    `json:"error_message,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	HasRawJSON      bool       `json:"has_raw_json"`
	Resumo          *RFBResumo `json:"resumo,omitempty"`
}

// RFBResumo represents the summary of a CBS assessment
type RFBResumo struct {
	ID                 string  `json:"id"`
	RequestID          string  `json:"request_id"`
	DataApuracao       string  `json:"data_apuracao"`
	TotalDebitos       int     `json:"total_debitos"`
	ValorCBSTotal      float64 `json:"valor_cbs_total"`
	ValorCBSExtinto    float64 `json:"valor_cbs_extinto"`
	ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
	TotalCorrente      int     `json:"total_corrente"`
	TotalAjuste        int     `json:"total_ajuste"`
	TotalExtemporaneo  int     `json:"total_extemporaneo"`
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

// SolicitarApuracaoHandler triggers a new CBS assessment request to the RFB API (manual — teto diário por versão da API: v1 = 2, v2 = 4)
func SolicitarApuracaoHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[SolicitarApuracao]")
			return
		}

		// Teto efetivo pela versão da API da credencial ativa (v1 = 2, v2 = 4).
		limiteDia := services.LimiteDebitosDiaRFBEmpresa(db, companyID)

		// Manual requests: count débito attempts today (scheduler + manual, including errors)
		var todayCount int
		db.QueryRow(`
			SELECT COUNT(*) FROM rfb_requests
			WHERE company_id = $1
			  AND tipo = 'debito'
			  AND created_at >= CURRENT_DATE AT TIME ZONE 'America/Sao_Paulo'
		`, companyID).Scan(&todayCount)
		if todayCount >= limiteDia {
			http.Error(w, fmt.Sprintf("Limite diário atingido (máximo %d solicitações de débitos por dia — inclui tentativas automáticas com erro)", limiteDia), http.StatusTooManyRequests)
			return
		}

		if err := services.SolicitarApuracaoParaEmpresa(db, companyID, limiteDia); err != nil {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "credenciais RFB não encontradas"):
				http.Error(w, "Credenciais RFB não configuradas. Configure em Conectar Receita Federal > Credenciais API.", http.StatusBadRequest)
			case strings.HasPrefix(msg, "DAILY_LIMIT"):
				http.Error(w, fmt.Sprintf("Limite diário atingido (máximo %d solicitações de débitos por dia)", limiteDia), http.StatusTooManyRequests)
			case strings.Contains(msg, "RATE_LIMIT"):
				http.Error(w, msg, http.StatusTooManyRequests)
			case strings.Contains(msg, "TOKEN_ERROR"):
				http.Error(w, "Erro ao obter token da RFB: "+msg, http.StatusBadGateway)
			case strings.Contains(msg, "REQUEST_ERROR"):
				http.Error(w, "Erro ao solicitar apuração: "+msg, http.StatusBadGateway)
			default:
				log.Printf("[SolicitarApuracao] Erro inesperado: %v", err)
				http.Error(w, "Erro ao solicitar apuração. Tente novamente.", http.StatusInternalServerError)
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[DownloadManual]")
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
		var requestID, tiquete, status, dlTipo string
		var tiqueteDownload *string
		err = db.QueryRow(`
			SELECT id, COALESCE(tiquete, ''), status, tiquete_download, COALESCE(tipo, 'debito') FROM rfb_requests
			WHERE id = $1 AND company_id = $2
		`, req.RequestID, companyID).Scan(&requestID, &tiquete, &status, &tiqueteDownload, &dlTipo)
		if err == sql.ErrNoRows {
			http.Error(w, "Solicitação não encontrada", http.StatusNotFound)
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar solicitação", err, "[DownloadManual]")
			return
		}

		if tiquete == "" {
			http.Error(w, "Solicitação sem tíquete - não é possível fazer download", http.StatusBadRequest)
			return
		}

		// Sem tiqueteDownload (webhook nunca chegou): tenta mesmo assim com o
		// tiqueteSolicitacao original — ProcessarDownloadRFB/BaixarArquivoRFB já sabem
		// cair pra esse fallback (v1) ou consultar a situação/urlAssinada (v2). A RFB pode ter concluído o
		// processamento e só falhado em avisar via webhook; se não tiver nada
		// pronto, a RFB responde com erro claro e a linha volta pra 'error'.
		usandoFallback := tiqueteDownload == nil || *tiqueteDownload == ""
		if usandoFallback {
			log.Printf("[DownloadManual] request %s sem tiqueteDownload — tentando recuperar com tiqueteSolicitacao", requestID)
		}

		if status == "completed" {
			http.Error(w, "Download já realizado para esta solicitação", http.StatusConflict)
			return
		}

		if status == "downloading" {
			http.Error(w, "Download já está em andamento", http.StatusConflict)
			return
		}

		// Trigger download in background, routing by tipo
		dlTipoCopy := dlTipo
		go func() {
			rfbClient := services.NewRFBClient()
			if dlTipoCopy == "credito" {
				if err := services.ProcessarDownloadCreditosRFB(db, rfbClient, requestID); err != nil {
					log.Printf("[RFB Manual Download] Error processing credits request %s: %v", requestID, err)
				}
				return
			}
			if err := services.ProcessarDownloadRFB(db, rfbClient, requestID); err != nil {
				log.Printf("[RFB Manual Download] Error processing request %s: %v", requestID, err)
			}
		}()

		message := "Download iniciado. Acompanhe o status na lista de solicitações."
		if usandoFallback {
			message = "Tentativa de recuperação iniciada (sem confirmação prévia da RFB) — pode falhar se o processamento realmente não tiver concluído. Acompanhe o status na lista de solicitações."
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "downloading",
			"message": message,
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[DeleteRequest]")
			return
		}
		requestID := strings.TrimPrefix(r.URL.Path, "/api/rfb/apuracao/")
		requestID = strings.TrimSpace(requestID)

		res, err := db.Exec(`
			DELETE FROM rfb_requests
			WHERE id = $1 AND company_id = $2 AND status = 'error'
		`, requestID, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao remover solicitação", err, "[DeleteRequest]")
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[ClearErrors]")
			return
		}
		res, err := db.Exec(`DELETE FROM rfb_requests WHERE company_id = $1 AND status = 'error'`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao remover registros de erro", err, "[ClearErrors]")
			return
		}
		rows, _ := res.RowsAffected()
		json.NewEncoder(w).Encode(map[string]interface{}{"deleted": rows})
	}
}

// AbortRequestHandler aborta manualmente uma solicitação presa em estado intermediário.
func AbortRequestHandler(db *sql.DB) http.HandlerFunc {
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
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}
		var req struct {
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RequestID == "" {
			jsonErr(w, http.StatusBadRequest, "request_id obrigatório")
			return
		}
		res, err := db.Exec(`
			UPDATE rfb_requests
			SET status = 'error', error_code = 'MANUAL_ABORT',
			    error_message = 'Solicitação abortada manualmente pelo usuário',
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND company_id = $2
			  AND status IN ('pending', 'requested', 'webhook_received', 'downloading', 'reprocessing')
		`, req.RequestID, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao abortar solicitação", err, "[AbortRequest]")
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			jsonErr(w, http.StatusNotFound, "Solicitação não encontrada ou já concluída")
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "aborted"})
	}
}

// RessolicitarHandler reenvia à API RFB uma solicitação que ficou em erro (sem raw_json):
// faz um claim atômico da linha, chama o service correto (débito/crédito) e substitui a
// linha antiga pela nova solicitação real criada pelo service. No caminho feliz a linha
// nunca fica presa em 'pending' (achado de revisão: a versão anterior só resetava o status
// e nunca rechamava a RFB de fato, deixando a linha órfã até o watchdog de 5h abortá-la com
// timeout falso); se a reversão para 'error' em si falhar (DB indisponível), o erro é logado.
// A linha antiga NUNCA é reaproveitada com novo tíquete: o service faz INSERT de uma linha nova
// com a api_versao/ambiente da NOVA solicitação (ResolveRFBAPIVersion no momento do reenvio) e
// a antiga é descartada — então não há api_versao "velha" grudada numa solicitação nova.
func RessolicitarHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
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
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}
		var req struct {
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RequestID == "" {
			jsonErr(w, http.StatusBadRequest, "request_id obrigatório")
			return
		}

		// Claim atômico: só avança se a linha ainda está em erro e sem dados baixados —
		// evita reenvio duplicado em duplo clique.
		var tipo string
		err = db.QueryRow(`
			UPDATE rfb_requests
			SET status = 'pending', error_code = NULL, error_message = NULL,
			    tiquete = NULL, tiquete_download = NULL, raw_json = NULL,
			    url_assinada = NULL, url_assinada_expira_em = NULL,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $1 AND company_id = $2
			  AND status = 'error' AND raw_json IS NULL
			RETURNING COALESCE(tipo, 'debito')
		`, req.RequestID, companyID).Scan(&tipo)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "Solicitação não encontrada, não está em erro, ou já possui dados baixados")
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar solicitação", err, "[Ressolicitar]")
			return
		}

		// fail reverte a linha para 'error' com o motivo real desta tentativa — usado só quando
		// o service NÃO chegou a criar nenhuma linha nova pra esta tentativa (falhou antes de
		// qualquer chamada HTTP à RFB).
		fail := func(status int, code, publicMsg string) {
			if _, execErr := db.Exec(`
				UPDATE rfb_requests SET status = 'error', error_code = $3, error_message = $4, updated_at = CURRENT_TIMESTAMP
				WHERE id = $1 AND company_id = $2
			`, req.RequestID, companyID, code, publicMsg); execErr != nil {
				log.Printf("[Ressolicitar] Falha ao reverter linha %s para error: %v", req.RequestID, execErr)
			}
			jsonErr(w, status, publicMsg)
		}

		// discard remove a linha antiga (claim) sem reverter — usado quando o service JÁ criou
		// uma linha nova com o resultado real desta tentativa (sucesso ou erro). Sem isso, toda
		// tentativa que falhasse por um motivo que o service já registra (TOKEN_ERROR, RATE_LIMIT,
		// REQUEST_ERROR, ENDPOINT_INDISPONIVEL) deixava a linha antiga revertida-pra-erro AO LADO
		// da linha nova recém-criada — duplicando o registro a cada tentativa (achado de revisão).
		discard := func(status int, publicMsg string) {
			if _, execErr := db.Exec(`DELETE FROM rfb_requests WHERE id = $1 AND company_id = $2 AND status = 'pending'`, req.RequestID, companyID); execErr != nil {
				log.Printf("[Ressolicitar] Falha ao remover linha antiga %s: %v", req.RequestID, execErr)
			}
			jsonErr(w, status, publicMsg)
		}

		tipoLabel := "débitos"
		if tipo == "credito" {
			tipoLabel = "créditos"
		}
		// status != 'pending' exclui a própria linha que está sendo reenviada agora (claim
		// atômico não altera created_at) — sem isso, a linha se autocontava e bloqueava seu
		// próprio reenvio (achado de revisão). error_code != 'ENDPOINT_INDISPONIVEL' exclui
		// tentativas automáticas diárias que só constatam que a RFB ainda não liberou o
		// endpoint — não é uma tentativa real, não deveria consumir a cota de reenvio manual.
		var todayCount int
		db.QueryRow(`
			SELECT COUNT(*) FROM rfb_requests
			WHERE company_id = $1 AND tipo = $2 AND status != 'pending'
			  AND COALESCE(error_code, '') != 'ENDPOINT_INDISPONIVEL'
			  AND created_at >= CURRENT_DATE AT TIME ZONE 'America/Sao_Paulo'
		`, companyID, tipo).Scan(&todayCount)
		// Débitos: teto da RFB pela versão da API da credencial ativa (v1 = 2, v2 = 4);
		// créditos: LimiteCreditosDiaRFB (2/dia, sem limite da RFB conhecido).
		limiteDia := services.LimiteCreditosDiaRFB
		if tipo != "credito" {
			limiteDia = services.LimiteDebitosDiaRFBEmpresa(db, companyID)
		}
		if todayCount >= limiteDia {
			fail(http.StatusTooManyRequests, "DAILY_LIMIT", fmt.Sprintf("Limite diário atingido (máximo %d solicitações de %s por dia)", limiteDia, tipoLabel))
			return
		}

		var solicitarErr error
		if tipo == "credito" {
			solicitarErr = services.SolicitarCreditoParaEmpresa(db, companyID)
		} else {
			solicitarErr = services.SolicitarApuracaoParaEmpresa(db, companyID, limiteDia)
		}
		if solicitarErr != nil {
			msg := solicitarErr.Error()
			switch {
			case strings.Contains(msg, "credenciais RFB não encontradas"):
				// Falha antes de qualquer chamada à RFB — nenhuma linha nova foi criada.
				fail(http.StatusBadRequest, "NO_CREDENTIALS", "Credenciais RFB não configuradas. Configure em Conectar Receita Federal > Credenciais API.")
			case strings.HasPrefix(msg, "DAILY_LIMIT"):
				// Teto diário rechecado dentro de SolicitarApuracaoParaEmpresa (mesmo limite efetivo
				// limiteDia acima; só dispara numa corrida entre duas solicitações simultâneas).
				// Falha antes de qualquer INSERT — nenhuma linha nova foi criada.
				fail(http.StatusTooManyRequests, "DAILY_LIMIT", fmt.Sprintf("Limite diário atingido (máximo %d solicitações de débitos por dia)", limiteDia))
			case strings.Contains(msg, "RATE_LIMIT"):
				// service já fez INSERT de uma linha nova com este erro — descarta a antiga.
				discard(http.StatusTooManyRequests, msg)
			case strings.Contains(msg, "TOKEN_ERROR"):
				discard(http.StatusBadGateway, "Erro ao obter token da RFB: "+msg)
			case strings.Contains(msg, "REQUEST_ERROR"):
				discard(http.StatusBadGateway, "Erro ao resolicitar: "+msg)
			case strings.Contains(msg, "endpoint não disponível"):
				// Condição conhecida e temporária (RFB ainda não ativou o endpoint de créditos neste
				// ambiente), não uma falha real — service já criou a linha nova com
				// error_code=ENDPOINT_INDISPONIVEL (frontend estiliza como Alerta, não Erro).
				discard(http.StatusServiceUnavailable, "A Receita Federal ainda não liberou o endpoint de créditos CBS para este ambiente. Tente novamente mais tarde.")
			default:
				log.Printf("[Ressolicitar] Erro inesperado: %v", solicitarErr)
				fail(http.StatusInternalServerError, "UNKNOWN_ERROR", "Erro ao resolicitar. Tente novamente.")
			}
			return
		}

		// Sucesso: o service já criou uma linha nova (INSERT com tiquete fresco) — remove a antiga.
		if res, delErr := db.Exec(`DELETE FROM rfb_requests WHERE id = $1 AND company_id = $2 AND status = 'pending'`, req.RequestID, companyID); delErr != nil {
			log.Printf("[Ressolicitar] Falha ao remover linha antiga %s: %v", req.RequestID, delErr)
		} else if rows, _ := res.RowsAffected(); rows == 0 {
			log.Printf("[Ressolicitar] Linha antiga %s não encontrada em 'pending' na hora de limpar (concorrência?)", req.RequestID)
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "requested",
			"message": "Solicitação reenviada à Receita Federal. Aguarde o retorno via webhook.",
		})
	}
}

// ReprocessHandler re-parses the raw JSON already stored in the DB for a request.
// Useful when download succeeded but JSON parse failed (e.g. datetime format issue).
func ReprocessHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[Reprocess]")
			return
		}

		var req struct {
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Verify ownership and get tipo
		var reprocessTipo string
		err = db.QueryRow(`SELECT COALESCE(tipo, 'debito') FROM rfb_requests WHERE id = $1 AND company_id = $2`,
			req.RequestID, companyID).Scan(&reprocessTipo)
		if err != nil {
			http.Error(w, "Solicitação não encontrada", http.StatusNotFound)
			return
		}

		reprocessID := req.RequestID
		reprocessTipoCopy := reprocessTipo
		go func() {
			if reprocessTipoCopy == "credito" {
				if err := services.ReprocessarRawJSONCreditosRFB(db, reprocessID); err != nil {
					log.Printf("[RFB Reprocess] Error (credito): %v", err)
				}
				return
			}
			if err := services.ReprocessarRawJSON(db, reprocessID); err != nil {
				log.Printf("[RFB Reprocess] Error: %v", err)
			}
		}()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "reprocessing",
			"message": "Reprocessamento iniciado a partir do JSON salvo.",
		})
	}
}

// verifyWebhookSignature validates the HMAC-SHA256 signature sent by RFB.
// Expects header X-RFB-Signature: sha256=<hex> computed over the raw body.
// If RFB_WEBHOOK_SECRET is not set, validation is skipped (dev fallback with warning).
func verifyWebhookSignature(r *http.Request, body []byte) bool {
	secret := os.Getenv("RFB_WEBHOOK_SECRET")
	if secret == "" {
		log.Println("[RFB Webhook] WARNING: RFB_WEBHOOK_SECRET not set — skipping signature validation")
		return true
	}
	sig := r.Header.Get("X-RFB-Signature")
	if sig == "" {
		return false
	}
	// Accept both "sha256=<hex>" and raw hex
	if len(sig) > 7 && sig[:7] == "sha256=" {
		sig = sig[7:]
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// RFBWebhookHandler receives callbacks from the RFB API (PUBLIC - no JWT auth)
func RFBWebhookHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Hipótese principal (log de acesso do nginx: HEAD da RFB no mesmo segundo em
		// que aceita nossa solicitação, em 5 dias diferentes, sem nenhum POST real
		// chegar em nenhum deles): a RFB usa esse HEAD pra validar a URL do webhook
		// antes de agendar a entrega real, e descartava a entrega ao receber 405 daqui.
		// Correlação temporal forte, mas não confirmada pela RFB — se o POST real
		// continuar não chegando após esta mudança, revisitar essa hipótese.
		if r.Method == http.MethodHead {
			log.Printf("[RFB Webhook] HEAD recebido (provável checagem de vivacidade da RFB, não é o callback real)")
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, webhookMaxBodyBytes))
		if err != nil {
			log.Printf("[RFB Webhook] Error reading body: %v", err)
			http.Error(w, "Error reading body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Validate HMAC signature
		if !verifyWebhookSignature(r, body) {
			log.Printf("[RFB Webhook] Invalid signature from %s", GetClientIP(r))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		log.Printf("[RFB Webhook] ===== CALLBACK RECEIVED =====")
		log.Printf("[RFB Webhook] Method: %s | RemoteAddr: %s", r.Method, r.RemoteAddr)
		// Headers e corpo NÃO são logados crus: headers carregam Authorization/assinatura/cookie
		// e o corpo v2 traz a urlAssinada (credencial temporária). Abaixo loga-se o payload
		// redigido (qualquer valor http(s):// reduzido ao host).

		// Parse webhook payload - try to extract tiquete
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("[RFB Webhook] Error parsing JSON: %v (body: %d bytes, prefixo: %q)", err, len(body), redactURLsInText(services.TruncateRunes(string(body), 200)))
			// Return 200 even on parse error so RFB doesn't retry indefinitely
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "invalid JSON"})
			return
		}
		log.Printf("[RFB Webhook] Body: %s", redactedWebhookBody(payload))

		// v1: tiqueteSolicitacao + tiqueteDownload (baixa por /download/v1/{tiqueteDownload}).
		// v2 sucesso: tiqueteSolicitacao + urlAssinada + urlAssinadaExpiraEm (sem tiqueteDownload).
		// v2 erro: codigoErro + mensagemErro (tiqueteSolicitacao NÃO é garantido).
		tiqueteSolicitacao := webhookStringField(payload, "tiqueteSolicitacao")
		tiqueteDownload := webhookStringField(payload, "tiqueteDownload")
		urlAssinada := webhookStringField(payload, "urlAssinada")
		urlExpiraStr := webhookStringField(payload, "urlAssinadaExpiraEm")
		codigoErro := webhookStringField(payload, "codigoErro")
		mensagemErro := webhookStringField(payload, "mensagemErro")

		// Precedência explícita: SUCESSO vence erro. Se o payload trouxer codigoErro/mensagemErro
		// junto de urlAssinada/tiqueteDownload, trata-se como sucesso (há o que baixar) e loga aviso.
		temErro := codigoErro != "" || mensagemErro != ""
		temDownload := urlAssinada != "" || tiqueteDownload != ""
		if temErro && temDownload {
			log.Printf("[RFB Webhook] AVISO: payload com codigoErro/mensagemErro E urlAssinada/tiqueteDownload (tiquete %s, codigoErro=%q) — tratado como SUCESSO",
				tiqueteSolicitacao, services.TruncateRunes(codigoErro, 50))
		}

		// ── Ramo de erro (RFB avisa falha de processamento) ──
		if temErro && !temDownload {
			if tiqueteSolicitacao == "" {
				log.Printf("[RFB Webhook] AVISO: erro da RFB sem tiqueteSolicitacao (não correlacionável): codigoErro=%q mensagemErro=%q",
					services.TruncateRunes(codigoErro, 50), services.TruncateRunes(mensagemErro, 300))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "error payload without tiqueteSolicitacao"})
				return
			}
			if codigoErro == "" {
				codigoErro = "RFB_ERRO"
			}
			codigoErro = services.TruncateRunes(codigoErro, 50)
			mensagemErro = services.TruncateRunes(mensagemErro, 1000)
			// webhook_received fica de fora: já recebeu o resultado bom (url/tíquete) e um erro
			// duplicado/tardio não pode derrubá-lo enquanto o download é despachado.
			rows, err := db.Query(`
				UPDATE rfb_requests
				SET status = 'error', error_code = $1, error_message = $2,
				    url_assinada = NULL, url_assinada_expira_em = NULL, updated_at = CURRENT_TIMESTAMP
				WHERE tiquete = $3 AND status NOT IN ('completed', 'downloading', 'reprocessing', 'webhook_received')
				RETURNING id, COALESCE(tipo, 'debito')
			`, codigoErro, mensagemErro, tiqueteSolicitacao)
			n := 0
			if err != nil {
				log.Printf("[RFB Webhook] Error marking request as error: %v", err)
			} else {
				var tipos []string
				for rows.Next() {
					var id, tp string
					if rows.Scan(&id, &tp) == nil {
						n++
						tipos = append(tipos, tp)
					}
				}
				rows.Close()
				if n > 1 {
					log.Printf("[RFB Webhook] AVISO: tiquete %s casou %d solicitações (tipos %v) — todas marcadas como error", tiqueteSolicitacao, n, tipos)
				}
			}
			switch {
			case err != nil:
			case n == 0:
				log.Printf("[RFB Webhook] Erro da RFB para tiquete %s sem solicitação elegível (já concluída/em andamento ou desconhecida)", tiqueteSolicitacao)
			default:
				log.Printf("[RFB Webhook] Solicitação do tiquete %s marcada como error (%s)", tiqueteSolicitacao, codigoErro)
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "rfb error recorded"})
			return
		}

		log.Printf("[RFB Webhook] tiqueteSolicitacao: %s | tiqueteDownload: %s | urlAssinada: %t",
			tiqueteSolicitacao, tiqueteDownload, urlAssinada != "")

		if tiqueteSolicitacao == "" || (tiqueteDownload == "" && urlAssinada == "") {
			log.Printf("[RFB Webhook] Missing required fields in payload: %s", redactedWebhookBody(payload))
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "missing tiqueteSolicitacao and (tiqueteDownload or urlAssinada)"})
			return
		}

		// Find the request by tiqueteSolicitacao. Aceita além de 'requested' também 'error'
		// (linha já marcada como erro pelo watchdog de 5h ou por uma tentativa manual de
		// "Tentar Recuperar") — a RFB pode responder atrasada, depois desses estados terem
		// sido setados; sem isso o webhook tardio era descartado silenciosamente ("request
		// not found"), perdendo um resultado que a RFB efetivamente entregou. Exclui só os
		// estados que já processaram ou estão processando o resultado.
		var requestID, reqTipo, reqApiVersao, reqStatus string
		err = db.QueryRow(`
			SELECT id, COALESCE(tipo, 'debito'), COALESCE(api_versao, 'v1'), status FROM rfb_requests
			WHERE tiquete = $1 AND status NOT IN ('completed', 'downloading', 'reprocessing')
		`, tiqueteSolicitacao).Scan(&requestID, &reqTipo, &reqApiVersao, &reqStatus)
		if err != nil {
			log.Printf("[RFB Webhook] Request not found for tiqueteSolicitacao %s: %v", tiqueteSolicitacao, err)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "request not found"})
			return
		}

		// urlAssinada só existe na API v2 e vem de um endpoint público: aceita apenas para linhas
		// api_versao='v2', só https/443 e host fora de faixas internas (SSRF). Sem DNS aqui; a
		// resolução é revalidada no dial do download. URL inválida descarta SÓ a URL — um
		// tiqueteDownload válido no mesmo payload continua sendo usado.
		urlRejeitada := ""
		if urlAssinada != "" {
			switch {
			case reqApiVersao != services.RFBAPIVersaoV2:
				log.Printf("[RFB Webhook] urlAssinada IGNORADA: solicitação %s é api_versao=%s (só v2 usa urlAssinada) — host %s",
					requestID, reqApiVersao, urlHostForLog(urlAssinada))
				urlRejeitada = "urlAssinada em solicitação " + reqApiVersao + " (só v2)"
				urlAssinada = ""
			default:
				if verr := services.ValidarURLAssinada(urlAssinada); verr != nil {
					log.Printf("[RFB Webhook] urlAssinada rejeitada (%v) — host %s", verr, urlHostForLog(urlAssinada))
					urlRejeitada = verr.Error()
					urlAssinada = ""
				}
			}
		}
		if urlAssinada != "" && strings.TrimSpace(os.Getenv("RFB_WEBHOOK_SECRET")) == "" {
			log.Printf("[RFB Webhook] ALERTA DE SEGURANÇA: urlAssinada aceita com RFB_WEBHOOK_SECRET VAZIO — webhook sem HMAC + URL externa permite envenenamento de dados (qualquer um pode apontar o download para um arquivo próprio). Configure RFB_WEBHOOK_SECRET.")
		}

		if tiqueteDownload == "" && urlAssinada == "" {
			// Nada utilizável: só urlAssinada e ela foi descartada.
			if reqStatus == "requested" || reqStatus == "error" {
				res, uerr := db.Exec(`
					UPDATE rfb_requests
					SET status = 'error', error_code = $1, error_message = $2,
					    url_assinada = NULL, url_assinada_expira_em = NULL, updated_at = CURRENT_TIMESTAMP
					WHERE id = $3 AND status IN ('requested', 'error')
				`, services.RFBErrURLInvalida, services.TruncateRunes("urlAssinada do webhook descartada: "+urlRejeitada, 1000), requestID)
				if uerr != nil {
					log.Printf("[RFB Webhook] Error marking URL_INVALIDA: %v", uerr)
				} else if n, _ := res.RowsAffected(); n > 0 {
					log.Printf("[RFB Webhook] Solicitação %s marcada como error (URL_INVALIDA)", requestID)
				}
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "invalid urlAssinada"})
			return
		}

		// Expiração: ausente → NULL (COALESCE preserva o já gravado); presente mas ilegível →
		// TTL conservador (ParseURLExpiraEm), nunca NULL.
		var urlExpiraEm sql.NullTime
		if urlAssinada != "" {
			if t := services.ParseURLExpiraEm(urlExpiraStr); t != nil {
				urlExpiraEm = sql.NullTime{Time: *t, Valid: true}
			}
		}

		// Save tiqueteDownload/urlAssinada and update status. Limpa error_code/error_message — se a
		// linha estava em 'error' (webhook atrasado, ver comentário acima), o erro antigo (ex:
		// TIMEOUT) não deve continuar aparecendo junto do novo status na tela. Campos ausentes no
		// payload preservam o valor já gravado (webhook duplicado/parcial não apaga nada; a
		// expiração nunca é sobrescrita por NULL). A guarda de status + RowsAffected impede que um
		// webhook duplicado, chegando com a linha já baixando/concluída, dispare um 2º download.
		res, err := db.Exec(`
			UPDATE rfb_requests
			SET status = 'webhook_received',
			    tiquete_download = COALESCE(NULLIF($1::text, ''), tiquete_download),
			    url_assinada = CASE WHEN $2::text <> '' THEN $2::text ELSE url_assinada END,
			    url_assinada_expira_em = CASE WHEN $2::text <> '' THEN COALESCE($3::timestamptz, url_assinada_expira_em) ELSE url_assinada_expira_em END,
			    error_code = NULL, error_message = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE id = $4 AND status NOT IN ('completed', 'downloading', 'reprocessing')
		`, tiqueteDownload, urlAssinada, urlExpiraEm, requestID)
		if err != nil {
			// 500 para a RFB reenviar: sem persistir url/tíquete não há o que baixar.
			log.Printf("[RFB Webhook] Error updating status/download info: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			log.Printf("[RFB Webhook] Request %s já concluída/em download/reprocesso — webhook duplicado, download NÃO redisparado", requestID)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "request already processing or completed"})
			return
		}

		log.Printf("[RFB Webhook] Request %s updated — download info saved, triggering download", requestID)

		// Dispatch to the correct processor based on tipo
		reqID := requestID
		reqTipoCopy := reqTipo
		go func() {
			rfbClient := services.NewRFBClient()
			if reqTipoCopy == "credito" {
				if err := services.ProcessarDownloadCreditosRFB(db, rfbClient, reqID); err != nil {
					log.Printf("[RFB Webhook] Error processing credits for request %s: %v", reqID, err)
					return
				}
				// mv_malha_fina_resumo agora também depende de rfb_creditos (nfe-entradas,
				// cte) — sem este refresh, um download só-de-créditos nunca invalidaria
				// a MV para esses tipos (achado de revisão do fix rfb_debitos→rfb_creditos).
				RefreshMalhaFinaMV(db)
				return
			}
			if err := services.ProcessarDownloadRFB(db, rfbClient, reqID); err != nil {
				log.Printf("[RFB Webhook] Error processing download for request %s: %v", reqID, err)
				return
			}
			RefreshMalhaFinaMV(db)
		}()

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "processing"})
	}
}

// webhookMaxBodyBytes limita o corpo do webhook (endpoint público).
const webhookMaxBodyBytes = 1 << 20

// webhookStringField lê um campo string do payload. Campo presente com tipo errado
// (número, objeto, lista, bool) é LOGADO e tratado como ausente — não some em silêncio.
// null é tratado como ausente sem aviso.
func webhookStringField(payload map[string]interface{}, key string) string {
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	sv, ok := v.(string)
	if !ok {
		log.Printf("[RFB Webhook] AVISO: campo %q com tipo inesperado %T (esperado string) — ignorado", key, v)
		return ""
	}
	return strings.TrimSpace(sv)
}

// StatusApuracaoHandler returns the list of RFB requests for the company
func StatusApuracaoHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[StatusApuracao]")
			return
		}

		rows, err := db.Query(`
			SELECT r.id, r.company_id, r.cnpj_base, COALESCE(r.tiquete, ''), r.tiquete_download,
				r.status, r.ambiente,
				r.error_code, r.error_message, r.created_at, r.updated_at,
				(r.raw_json IS NOT NULL) AS has_raw_json,
				res.id, res.request_id, COALESCE(res.data_apuracao, ''), res.total_debitos,
				res.valor_cbs_total, res.valor_cbs_extinto, res.valor_cbs_nao_extinto,
				res.total_corrente, res.total_ajuste, res.total_extemporaneo
			FROM rfb_requests r
			LEFT JOIN rfb_resumo res ON res.request_id = r.id
			WHERE r.company_id = $1 AND COALESCE(r.tipo, 'debito') = 'debito'
			ORDER BY r.created_at DESC
			LIMIT 20
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar solicitações", err, "[StatusApuracao]")
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
				&req.ID, &req.CompanyID, &req.CNPJBase, &req.Tiquete, &req.TiqueteDownload,
				&req.Status, &req.Ambiente,
				&req.ErrorCode, &req.ErrorMessage, &req.CreatedAt, &req.UpdatedAt,
				&req.HasRawJSON,
				&resID, &resReqID, &resData, &resTotalDebitos,
				&resCBSTotal, &resCBSExtinto, &resCBSNaoExtinto,
				&resCorrente, &resAjuste, &resExtemp,
			); err != nil {
				log.Printf("[StatusApuracao] Erro ao ler solicitação: %v", err)
				continue
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

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[DetalheApuracao]")
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
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao remover solicitação", err, "[DetalheApuracao]")
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar solicitação", err, "[DetalheApuracao]")
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
			ORDER BY d.tipo_apuracao,
				CASE WHEN d.data_apuracao ~ '^(0[1-9]|1[0-2])/[0-9]{4}$' THEN TO_DATE(d.data_apuracao, 'MM/YYYY') END
			LIMIT $%d OFFSET $%d`, idx, idx+1)
		pageArgs := append(args, pageSize, offset)
		debitRows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar débitos", err, "[DetalheApuracao]")
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

// redactedWebhookBody serializa o payload do webhook redigindo, recursivamente, qualquer
// valor string que comece com http(s):// (não só a chave urlAssinada): vira "<url redigida host=...>".
// Nunca logar a URL completa (credencial temporária).
func redactedWebhookBody(payload map[string]interface{}) string {
	b, _ := json.Marshal(redactWebhookValue(payload))
	return string(b)
}

func redactWebhookValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = redactWebhookValue(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = redactWebhookValue(val)
		}
		return out
	case string:
		l := strings.ToLower(strings.TrimSpace(t))
		if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
			return "<url redigida host=" + urlHostForLog(strings.TrimSpace(t)) + ">"
		}
		return t
	}
	return v
}

var urlInTextRe = regexp.MustCompile(`(?i)https?://[^\s"'<>]*`)

// redactURLsInText troca URLs em texto livre (ex.: JSON inválido) por um marcador.
func redactURLsInText(s string) string {
	return urlInTextRe.ReplaceAllString(s, "<url redigida>")
}

// urlHostForLog devolve só o host de uma URL (para logs), ou "?" se ilegível.
func urlHostForLog(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "?"
}
