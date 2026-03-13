package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// dbExecutor abstracts *sql.DB and *sql.Tx so insertDebito works in both contexts.
type dbExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// FlexString unmarshals both JSON strings and JSON numbers into a Go string.
// Needed because the RFB API returns some fields (e.g. modeloDfe=55) as numbers.
type FlexString string

func (fs *FlexString) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*fs = FlexString(s)
	return nil
}

// RFBTime handles RFB datetime strings that may lack timezone suffix (e.g. "2026-03-01T08:30:09").
type RFBTime struct {
	T *time.Time
}

func (rt *RFBTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "null" || s == "" {
		rt.T = nil
		return nil
	}
	for _, format := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.999999999",
	} {
		if t, err := time.Parse(format, s); err == nil {
			rt.T = &t
			return nil
		}
	}
	// Log and ignore unparseable dates rather than failing the whole import
	log.Printf("[RFB Processor] WARNING: could not parse datetime '%s', storing as nil", s)
	rt.T = nil
	return nil
}

// RFB JSON structures matching the API response layout
type RFBApuracaoJSON struct {
	ApuracaoCorrente     *RFBGrupoDebitos `json:"apuracaoCorrente"`
	ApuracaoAjuste       *RFBGrupoDebitos `json:"apuracaoAjuste"`
	DebitosExtemporaneos *RFBGrupoDebitos `json:"debitosExtemporaneos"`
}

type RFBGrupoDebitos struct {
	Debitos []RFBDebito `json:"debitos"`
}

type RFBDebito struct {
	ModeloDfe          FlexString      `json:"modeloDfe"`
	NumeroDfe          FlexString      `json:"numeroDfe"`
	ChaveDfe           FlexString      `json:"chaveDfe"`
	DataDfeEmissao     *RFBTime        `json:"dataDfeEmissao"`
	DataDfeAutorizacao *RFBTime        `json:"dataDfeAutorizacao"`
	DataDfeRegistro    *RFBTime        `json:"dataDfeRegistro"`
	DataApuracao       string          `json:"dataApuracao"`
	NiEmitente         FlexString      `json:"niEmitente"`
	NiAdquirente       FlexString      `json:"niAdquirente"`
	ValorCBSTotal      float64         `json:"valorCBSTotal"`
	ValorCBSExtinto    float64         `json:"valorCBSExtinto"`
	ValorCBSNaoExtinto float64         `json:"valorCBSNaoExtinto"`
	SituacaoDebito     FlexString      `json:"situacaoDebito"`
	FormasExtincao     json.RawMessage `json:"formasExtincao"`
	Eventos            json.RawMessage `json:"eventos"`
}

// ProcessarDownloadRFB downloads and processes the RFB CBS assessment JSON.
// It saves the raw JSON, normalizes debits into rfb_debitos, and creates a summary in rfb_resumo.
// All DB writes (debits + summary) are wrapped in a single transaction to prevent partial imports.
// An atomic status update prevents two goroutines from processing the same request concurrently.
func ProcessarDownloadRFB(db *sql.DB, rfbClient *RFBClient, requestID string) error {
	log.Printf("[RFB Processor] Starting download processing for request %s", requestID)

	// 1. Fetch request details and company credentials
	var companyID, tiquete, cnpjBase string
	var tiqueteDownload *string
	err := db.QueryRow(`
		SELECT r.company_id, r.tiquete, r.cnpj_base, r.tiquete_download
		FROM rfb_requests r
		WHERE r.id = $1
	`, requestID).Scan(&companyID, &tiquete, &cnpjBase, &tiqueteDownload)
	if err != nil {
		return fmt.Errorf("failed to fetch request: %w", err)
	}

	var clientID, clientSecret, ambiente string
	err = db.QueryRow(`
		SELECT client_id, client_secret, COALESCE(ambiente, 'producao') FROM rfb_credentials
		WHERE company_id = $1 AND ativo = true
	`, companyID).Scan(&clientID, &clientSecret, &ambiente)
	if err != nil {
		updateRequestError(db, requestID, "CRED_NOT_FOUND", "Credenciais RFB não encontradas ou inativas")
		return fmt.Errorf("failed to fetch credentials: %w", err)
	}

	rfbClient.SetAmbiente(ambiente)

	// Use tiqueteDownload if provided by the webhook
	tiqueteParaDownload := tiquete
	if tiqueteDownload != nil && *tiqueteDownload != "" {
		tiqueteParaDownload = *tiqueteDownload
		log.Printf("[RFB Processor] Using tiqueteDownload '%s' (solicitacao: '%s')", tiqueteParaDownload, tiquete)
	} else {
		log.Printf("[RFB Processor] WARNING: tiqueteDownload not set, falling back to tiqueteSolicitacao '%s'", tiquete)
	}

	// 2. Atomic status claim — prevents concurrent webhook + manual download races.
	// Only proceeds if status is not already 'downloading', 'completed', or 'reprocessing'.
	res, err := db.Exec(`
		UPDATE rfb_requests SET status = 'downloading', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status NOT IN ('downloading', 'completed', 'reprocessing')
	`, requestID)
	if err != nil {
		return fmt.Errorf("failed to claim request status: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		log.Printf("[RFB Processor] Request %s already being processed by another goroutine — skipping", requestID)
		return nil
	}

	// 3. Get fresh OAuth2 token
	token, err := rfbClient.GetToken(clientID, clientSecret)
	if err != nil {
		updateRequestError(db, requestID, "TOKEN_ERROR", err.Error())
		return fmt.Errorf("failed to get token: %w", err)
	}

	// 4. Download the JSON file (single-use ticket!)
	rawJSON, err := rfbClient.DownloadArquivo(token, tiqueteParaDownload)
	if err != nil {
		updateRequestError(db, requestID, "DOWNLOAD_ERROR", err.Error())
		return fmt.Errorf("failed to download: %w", err)
	}

	// 5. Save raw JSON as TEXT immediately — data is preserved even if parse/insert fails.
	// Column is TEXT (not JSONB, migration 064), so no 268 MB size limit applies.
	log.Printf("[RFB Processor] Saving raw JSON (%d MB) for request %s", len(rawJSON)/1024/1024, requestID)
	_, saveErr := db.Exec(`
		UPDATE rfb_requests SET raw_json = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, string(rawJSON), requestID)
	if saveErr != nil {
		// Non-fatal: log but continue — if parse succeeds, debits are still loaded
		log.Printf("[RFB Processor] WARNING: Failed to save raw JSON for request %s: %v (processing continues)", requestID, saveErr)
	} else {
		log.Printf("[RFB Processor] Raw JSON saved successfully for request %s", requestID)
	}

	// 6. Parse JSON
	var apuracao RFBApuracaoJSON
	if err := json.Unmarshal(rawJSON, &apuracao); err != nil {
		updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao interpretar JSON da RFB: "+err.Error())
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	// 7. Insert debits and summary inside a single transaction.
	// If any step fails the whole import is rolled back — no partial data.
	tx, err := db.Begin()
	if err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var totalCorrente, totalAjuste, totalExtemporaneo, insertErrors int
	var valorTotal, valorExtinto, valorNaoExtinto float64
	var dataApuracao string

	if apuracao.ApuracaoCorrente != nil {
		for _, d := range apuracao.ApuracaoCorrente.Debitos {
			if err := insertDebito(tx, requestID, companyID, "corrente", d); err != nil {
				log.Printf("[RFB Processor] Error inserting corrente debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				totalCorrente++
				valorTotal += d.ValorCBSTotal
				valorExtinto += d.ValorCBSExtinto
				valorNaoExtinto += d.ValorCBSNaoExtinto
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
			}
		}
	}

	if apuracao.ApuracaoAjuste != nil {
		for _, d := range apuracao.ApuracaoAjuste.Debitos {
			if err := insertDebito(tx, requestID, companyID, "ajuste", d); err != nil {
				log.Printf("[RFB Processor] Error inserting ajuste debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				totalAjuste++
				valorTotal += d.ValorCBSTotal
				valorExtinto += d.ValorCBSExtinto
				valorNaoExtinto += d.ValorCBSNaoExtinto
			}
		}
	}

	if apuracao.DebitosExtemporaneos != nil {
		for _, d := range apuracao.DebitosExtemporaneos.Debitos {
			if err := insertDebito(tx, requestID, companyID, "extemporaneo", d); err != nil {
				log.Printf("[RFB Processor] Error inserting extemporaneo debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				totalExtemporaneo++
				valorTotal += d.ValorCBSTotal
				valorExtinto += d.ValorCBSExtinto
				valorNaoExtinto += d.ValorCBSNaoExtinto
			}
		}
	}

	totalDebitos := totalCorrente + totalAjuste + totalExtemporaneo

	// Upsert summary in the same transaction
	_, err = tx.Exec(`
		INSERT INTO rfb_resumo (request_id, company_id, data_apuracao, total_debitos,
			valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
			total_corrente, total_ajuste, total_extemporaneo)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (company_id, data_apuracao)
		DO UPDATE SET request_id = $1, total_debitos = $4,
			valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
			total_corrente = $8, total_ajuste = $9, total_extemporaneo = $10
	`, requestID, companyID, dataApuracao, totalDebitos,
		valorTotal, valorExtinto, valorNaoExtinto,
		totalCorrente, totalAjuste, totalExtemporaneo)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao salvar resumo: "+err.Error())
		return fmt.Errorf("failed to upsert summary: %w", err)
	}

	if err := tx.Commit(); err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha no commit da transação: "+err.Error())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 8. Mark request as completed
	finalStatus := "completed"
	if insertErrors > 0 {
		log.Printf("[RFB Processor] WARN: %d debits failed to insert (raw_json preserved — use reprocess to retry)", insertErrors)
	}
	updateRequestStatus(db, requestID, finalStatus)
	log.Printf("[RFB Processor] Request %s %s: %d debits (%d corrente, %d ajuste, %d extemporaneo, %d errors), CBS total: %.2f",
		requestID, finalStatus, totalDebitos, totalCorrente, totalAjuste, totalExtemporaneo, insertErrors, valorTotal)

	return nil
}

func insertDebito(exec dbExecutor, requestID, companyID, tipoApuracao string, d RFBDebito) error {
	formasExtincao := sql.NullString{}
	if len(d.FormasExtincao) > 0 && string(d.FormasExtincao) != "null" {
		formasExtincao = sql.NullString{String: string(d.FormasExtincao), Valid: true}
	}
	eventos := sql.NullString{}
	if len(d.Eventos) > 0 && string(d.Eventos) != "null" {
		eventos = sql.NullString{String: string(d.Eventos), Valid: true}
	}

	var dataEmissao *time.Time
	if d.DataDfeEmissao != nil {
		dataEmissao = d.DataDfeEmissao.T
	}

	_, err := exec.Exec(`
		INSERT INTO rfb_debitos (request_id, company_id, tipo_apuracao,
			modelo_dfe, numero_dfe, chave_dfe, data_dfe_emissao, data_apuracao,
			ni_emitente, ni_adquirente,
			valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
			situacao_debito, formas_extincao, eventos)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`, requestID, companyID, tipoApuracao,
		string(d.ModeloDfe), string(d.NumeroDfe), string(d.ChaveDfe), dataEmissao, d.DataApuracao,
		string(d.NiEmitente), string(d.NiAdquirente),
		d.ValorCBSTotal, d.ValorCBSExtinto, d.ValorCBSNaoExtinto,
		string(d.SituacaoDebito), formasExtincao, eventos)
	return err
}

// ReprocessarRawJSON re-parses the raw JSON already stored in the DB without calling the RFB API.
// Safe pattern: parse JSON FIRST, then atomically delete old debits and insert new ones in a transaction.
// If parse or any insert fails, old data is never deleted — no data loss.
func ReprocessarRawJSON(db *sql.DB, requestID string) error {
	log.Printf("[RFB Reprocess] Starting reprocess for request %s", requestID)

	var companyID string
	var rawJSON *string
	err := db.QueryRow(`
		SELECT company_id, raw_json FROM rfb_requests WHERE id = $1
	`, requestID).Scan(&companyID, &rawJSON)
	if err != nil {
		return fmt.Errorf("failed to fetch request: %w", err)
	}
	if rawJSON == nil || *rawJSON == "" {
		return fmt.Errorf("no raw_json stored for request %s — cannot reprocess without raw data", requestID)
	}

	log.Printf("[RFB Reprocess] Raw JSON found (%d MB), parsing...", len(*rawJSON)/1024/1024)

	// 1. Atomic status claim — prevent concurrent reprocess runs
	res, err := db.Exec(`
		UPDATE rfb_requests SET status = 'reprocessing', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status NOT IN ('downloading', 'reprocessing')
	`, requestID)
	if err != nil {
		return fmt.Errorf("failed to claim reprocess status: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		log.Printf("[RFB Reprocess] Request %s is already being processed — skipping", requestID)
		return nil
	}

	// 2. Parse JSON FIRST — before touching any existing data.
	// If parse fails, old debits remain intact.
	var apuracao RFBApuracaoJSON
	if err := json.Unmarshal([]byte(*rawJSON), &apuracao); err != nil {
		updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao reprocessar JSON: "+err.Error())
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	// 3. Transaction: delete old debits then insert new ones atomically.
	// Rollback keeps old data if anything goes wrong.
	tx, err := db.Begin()
	if err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if _, err = tx.Exec(`DELETE FROM rfb_debitos WHERE request_id = $1`, requestID); err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao limpar débitos anteriores: "+err.Error())
		return fmt.Errorf("failed to delete existing debits: %w", err)
	}

	var totalCorrente, totalAjuste, totalExtemporaneo, insertErrors int
	var valorTotal, valorExtinto, valorNaoExtinto float64
	var dataApuracao string

	if apuracao.ApuracaoCorrente != nil {
		for _, d := range apuracao.ApuracaoCorrente.Debitos {
			if err := insertDebito(tx, requestID, companyID, "corrente", d); err != nil {
				log.Printf("[RFB Reprocess] Error inserting corrente debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				totalCorrente++
				valorTotal += d.ValorCBSTotal
				valorExtinto += d.ValorCBSExtinto
				valorNaoExtinto += d.ValorCBSNaoExtinto
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
			}
		}
	}

	if apuracao.ApuracaoAjuste != nil {
		for _, d := range apuracao.ApuracaoAjuste.Debitos {
			if err := insertDebito(tx, requestID, companyID, "ajuste", d); err != nil {
				log.Printf("[RFB Reprocess] Error inserting ajuste debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				totalAjuste++
				valorTotal += d.ValorCBSTotal
				valorExtinto += d.ValorCBSExtinto
				valorNaoExtinto += d.ValorCBSNaoExtinto
			}
		}
	}

	if apuracao.DebitosExtemporaneos != nil {
		for _, d := range apuracao.DebitosExtemporaneos.Debitos {
			if err := insertDebito(tx, requestID, companyID, "extemporaneo", d); err != nil {
				log.Printf("[RFB Reprocess] Error inserting extemporaneo debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				totalExtemporaneo++
				valorTotal += d.ValorCBSTotal
				valorExtinto += d.ValorCBSExtinto
				valorNaoExtinto += d.ValorCBSNaoExtinto
			}
		}
	}

	totalDebitos := totalCorrente + totalAjuste + totalExtemporaneo

	_, err = tx.Exec(`
		INSERT INTO rfb_resumo (request_id, company_id, data_apuracao, total_debitos,
			valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
			total_corrente, total_ajuste, total_extemporaneo)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (company_id, data_apuracao)
		DO UPDATE SET request_id = $1, total_debitos = $4,
			valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
			total_corrente = $8, total_ajuste = $9, total_extemporaneo = $10
	`, requestID, companyID, dataApuracao, totalDebitos,
		valorTotal, valorExtinto, valorNaoExtinto,
		totalCorrente, totalAjuste, totalExtemporaneo)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao salvar resumo: "+err.Error())
		return fmt.Errorf("failed to upsert summary: %w", err)
	}

	if err := tx.Commit(); err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha no commit: "+err.Error())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	updateRequestStatus(db, requestID, "completed")
	if insertErrors > 0 {
		log.Printf("[RFB Reprocess] WARN: %d debits failed to insert out of total attempted", insertErrors)
	}
	log.Printf("[RFB Reprocess] Request %s completed: %d debits (%d corrente, %d ajuste, %d extemporaneo, %d errors), CBS total: %.2f",
		requestID, totalDebitos, totalCorrente, totalAjuste, totalExtemporaneo, insertErrors, valorTotal)
	return nil
}

func updateRequestStatus(db *sql.DB, requestID, status string) {
	_, err := db.Exec(`
		UPDATE rfb_requests SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2
	`, status, requestID)
	if err != nil {
		log.Printf("[RFB Processor] Error updating request %s status to %s: %v", requestID, status, err)
	}
}

func updateRequestError(db *sql.DB, requestID, code, message string) {
	_, err := db.Exec(`
		UPDATE rfb_requests SET status = 'error', error_code = $1, error_message = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`, code, message, requestID)
	if err != nil {
		log.Printf("[RFB Processor] Error updating request %s error: %v", requestID, err)
	}
}
