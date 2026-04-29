package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// RFBCreditosJSON handles both flat {creditos:[...]} and grouped structures
// since the exact response shape of /creditos-cbs/v1/ may vary.
type RFBCreditosJSON struct {
	Creditos         []RFBCredito      `json:"creditos"`
	ApuracaoCorrente *RFBGrupoCreditos `json:"apuracaoCorrente"`
	ApuracaoAjuste   *RFBGrupoCreditos `json:"apuracaoAjuste"`
}

type RFBGrupoCreditos struct {
	Creditos []RFBCredito `json:"creditos"`
}

type RFBCredito struct {
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
	SituacaoCredito    FlexString      `json:"situacaoCredito"`
	FormasExtincao     json.RawMessage `json:"formasExtincao"`
	Eventos            json.RawMessage `json:"eventos"`
}

// ProcessarDownloadCreditosRFB downloads and processes the RFB CBS credits JSON.
// Mirrors ProcessarDownloadRFB but writes to rfb_creditos + rfb_creditos_resumo.
func ProcessarDownloadCreditosRFB(db *sql.DB, rfbClient *RFBClient, requestID string) error {
	log.Printf("[RFB Creditos] Starting download for request %s", requestID)

	var companyID, tiquete, cnpjBase string
	var tiqueteDownload *string
	err := db.QueryRow(`
		SELECT r.company_id, r.tiquete, r.cnpj_base, r.tiquete_download
		FROM rfb_requests r WHERE r.id = $1
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

	tiqueteParaDownload := tiquete
	if tiqueteDownload != nil && *tiqueteDownload != "" {
		tiqueteParaDownload = *tiqueteDownload
		log.Printf("[RFB Creditos] Using tiqueteDownload '%s'", tiqueteParaDownload)
	}

	// Atomic status claim
	res, err := db.Exec(`
		UPDATE rfb_requests SET status = 'downloading', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status NOT IN ('downloading', 'completed', 'reprocessing')
	`, requestID)
	if err != nil {
		return fmt.Errorf("failed to claim request status: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		log.Printf("[RFB Creditos] Request %s already being processed — skipping", requestID)
		return nil
	}

	token, err := rfbClient.GetToken(clientID, clientSecret)
	if err != nil {
		updateRequestError(db, requestID, "TOKEN_ERROR", err.Error())
		return fmt.Errorf("failed to get token: %w", err)
	}

	rawJSON, err := rfbClient.DownloadArquivo(token, tiqueteParaDownload)
	if err != nil {
		updateRequestError(db, requestID, "DOWNLOAD_ERROR", err.Error())
		return fmt.Errorf("failed to download: %w", err)
	}

	log.Printf("[RFB Creditos] Saving raw JSON (%d KB) for request %s", len(rawJSON)/1024, requestID)
	if _, saveErr := db.Exec(`
		UPDATE rfb_requests SET raw_json = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2
	`, string(rawJSON), requestID); saveErr != nil {
		log.Printf("[RFB Creditos] WARNING: Failed to save raw JSON: %v (continuing)", saveErr)
	}

	var creditos RFBCreditosJSON
	if err := json.Unmarshal(rawJSON, &creditos); err != nil {
		updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao interpretar JSON de créditos: "+err.Error())
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var totalCorrente, totalAjuste, insertErrors int
	var valorTotal, valorExtinto, valorNaoExtinto float64
	var dataApuracao string

	// Flat structure: {creditos: [...]}
	for _, c := range creditos.Creditos {
		if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
			log.Printf("[RFB Creditos] Error inserting flat credito (chave=%s): %v", c.ChaveDfe, err)
			insertErrors++
		} else {
			totalCorrente++
			valorTotal += c.ValorCBSTotal
			valorExtinto += c.ValorCBSExtinto
			valorNaoExtinto += c.ValorCBSNaoExtinto
			if dataApuracao == "" && c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
			}
		}
	}

	// Grouped structure: {apuracaoCorrente: {creditos: [...]}, apuracaoAjuste: {...}}
	if creditos.ApuracaoCorrente != nil {
		for _, c := range creditos.ApuracaoCorrente.Creditos {
			if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
				log.Printf("[RFB Creditos] Error inserting corrente credito (chave=%s): %v", c.ChaveDfe, err)
				insertErrors++
			} else {
				totalCorrente++
				valorTotal += c.ValorCBSTotal
				valorExtinto += c.ValorCBSExtinto
				valorNaoExtinto += c.ValorCBSNaoExtinto
				if dataApuracao == "" && c.DataApuracao != "" {
					dataApuracao = c.DataApuracao
				}
			}
		}
	}

	if creditos.ApuracaoAjuste != nil {
		for _, c := range creditos.ApuracaoAjuste.Creditos {
			if err := insertCredito(tx, requestID, companyID, "ajuste", c); err != nil {
				log.Printf("[RFB Creditos] Error inserting ajuste credito (chave=%s): %v", c.ChaveDfe, err)
				insertErrors++
			} else {
				totalAjuste++
				valorTotal += c.ValorCBSTotal
				valorExtinto += c.ValorCBSExtinto
				valorNaoExtinto += c.ValorCBSNaoExtinto
			}
		}
	}

	totalCreditos := totalCorrente + totalAjuste

	_, err = tx.Exec(`
		INSERT INTO rfb_creditos_resumo (request_id, company_id, data_apuracao, total_creditos,
			valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto, total_corrente, total_ajuste)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (company_id, data_apuracao)
		DO UPDATE SET request_id = $1, total_creditos = $4,
			valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
			total_corrente = $8, total_ajuste = $9
	`, requestID, companyID, dataApuracao, totalCreditos,
		valorTotal, valorExtinto, valorNaoExtinto, totalCorrente, totalAjuste)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao salvar resumo de créditos: "+err.Error())
		return fmt.Errorf("failed to upsert credits summary: %w", err)
	}

	if err := tx.Commit(); err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha no commit: "+err.Error())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	updateRequestStatus(db, requestID, "completed")
	if insertErrors > 0 {
		log.Printf("[RFB Creditos] WARN: %d credits failed to insert", insertErrors)
	}
	log.Printf("[RFB Creditos] Request %s completed: %d credits (%d corrente, %d ajuste, %d errors), CBS total: %.2f",
		requestID, totalCreditos, totalCorrente, totalAjuste, insertErrors, valorTotal)
	return nil
}

func insertCredito(exec dbExecutor, requestID, companyID, tipoApuracao string, c RFBCredito) error {
	formasExtincao := sql.NullString{}
	if len(c.FormasExtincao) > 0 && string(c.FormasExtincao) != "null" {
		formasExtincao = sql.NullString{String: string(c.FormasExtincao), Valid: true}
	}
	eventos := sql.NullString{}
	if len(c.Eventos) > 0 && string(c.Eventos) != "null" {
		eventos = sql.NullString{String: string(c.Eventos), Valid: true}
	}

	var dataEmissao *time.Time
	if c.DataDfeEmissao != nil {
		dataEmissao = c.DataDfeEmissao.T
	}

	_, err := exec.Exec(`
		INSERT INTO rfb_creditos (request_id, company_id, tipo_apuracao,
			modelo_dfe, numero_dfe, chave_dfe, data_dfe_emissao, data_apuracao,
			ni_emitente, ni_adquirente,
			valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
			situacao_credito, formas_extincao, eventos)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (company_id, chave_dfe) WHERE chave_dfe IS NOT NULL AND chave_dfe != ''
		DO UPDATE SET
			request_id            = EXCLUDED.request_id,
			tipo_apuracao         = EXCLUDED.tipo_apuracao,
			data_dfe_emissao      = EXCLUDED.data_dfe_emissao,
			data_apuracao         = EXCLUDED.data_apuracao,
			valor_cbs_total       = EXCLUDED.valor_cbs_total,
			valor_cbs_extinto     = EXCLUDED.valor_cbs_extinto,
			valor_cbs_nao_extinto = EXCLUDED.valor_cbs_nao_extinto,
			situacao_credito      = EXCLUDED.situacao_credito,
			formas_extincao       = EXCLUDED.formas_extincao,
			eventos               = EXCLUDED.eventos
	`, requestID, companyID, tipoApuracao,
		string(c.ModeloDfe), string(c.NumeroDfe), string(c.ChaveDfe), dataEmissao, c.DataApuracao,
		string(c.NiEmitente), string(c.NiAdquirente),
		c.ValorCBSTotal, c.ValorCBSExtinto, c.ValorCBSNaoExtinto,
		string(c.SituacaoCredito), formasExtincao, eventos)
	return err
}
