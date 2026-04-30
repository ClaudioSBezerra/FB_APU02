package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// RFBCreditosJSON espelha RFBApuracaoJSON para o endpoint /creditos-cbs/v1/.
// Aceita tanto o campo "creditos" quanto "debitos" em cada grupo, pois a RFB
// pode retornar as NF-e de entrada do comprador em qualquer um dos dois campos.
type RFBCreditosJSON struct {
	ApuracaoCorrente     *RFBGrupoCreditos `json:"apuracaoCorrente"`
	ApuracaoAjuste       *RFBGrupoCreditos `json:"apuracaoAjuste"`
	DebitosExtemporaneos *RFBGrupoCreditos `json:"debitosExtemporaneos"`
	// Fallback: array plano caso a RFB use estrutura diferente
	Creditos []RFBCredito `json:"creditos"`
	Debitos  []RFBCredito `json:"debitos"`
}

// RFBGrupoCreditos espelha RFBGrupoDebitos com ambos os campos possíveis.
type RFBGrupoCreditos struct {
	Creditos []RFBCredito `json:"creditos"`
	Debitos  []RFBCredito `json:"debitos"` // comprador: NF-e entrada pode vir como "debitos"
}

// RFBCredito espelha RFBDebito — mesmos campos, mesma lógica de parse.
// SituacaoCredito mapeia "situacao" (igual ao campo de débitos no JSON real da RFB).
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
	SituacaoCredito    FlexString      `json:"situacao"`
	FormasExtincao     json.RawMessage `json:"formasExtincao"`
	Eventos            json.RawMessage `json:"eventos"`
}

// ProcessarDownloadCreditosRFB baixa e processa o JSON de créditos CBS do endpoint /creditos-cbs/v1/.
// Espelha ProcessarDownloadRFB: mesma estrutura de grupos, mesma lógica transacional, mesmos logs.
func ProcessarDownloadCreditosRFB(db *sql.DB, rfbClient *RFBClient, requestID string) error {
	log.Printf("[RFB Creditos] ============================================================")
	log.Printf("[RFB Creditos] Iniciando download | request: %s", requestID)
	log.Printf("[RFB Creditos] ============================================================")

	var companyID, tiquete, cnpjBase string
	var tiqueteDownload *string
	err := db.QueryRow(`
		SELECT r.company_id, r.tiquete, r.cnpj_base, r.tiquete_download
		FROM rfb_requests r WHERE r.id = $1
	`, requestID).Scan(&companyID, &tiquete, &cnpjBase, &tiqueteDownload)
	if err != nil {
		log.Printf("[RFB Creditos] ERRO ao buscar request: %v", err)
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
		log.Printf("[RFB Creditos] Usando tiqueteDownload '%s'", tiqueteParaDownload)
	} else {
		log.Printf("[RFB Creditos] AVISO: tiqueteDownload não definido, usando tiqueteSolicitacao '%s'", tiquete)
	}

	res, err := db.Exec(`
		UPDATE rfb_requests SET status = 'downloading', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status NOT IN ('downloading', 'completed', 'reprocessing')
	`, requestID)
	if err != nil {
		return fmt.Errorf("failed to claim request status: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		log.Printf("[RFB Creditos] Request %s já está sendo processado — abortando", requestID)
		return nil
	}
	log.Printf("[RFB Creditos] Status → downloading | cnpjBase: %s | company: %s", cnpjBase, companyID)

	log.Printf("[RFB Creditos] Etapa 1/4: Obtendo token OAuth2...")
	token, err := rfbClient.GetToken(clientID, clientSecret)
	if err != nil {
		log.Printf("[RFB Creditos] ERRO ao obter token: %v", err)
		updateRequestError(db, requestID, "TOKEN_ERROR", err.Error())
		return fmt.Errorf("failed to get token: %w", err)
	}

	log.Printf("[RFB Creditos] Etapa 2/4: Baixando arquivo JSON (tiquete: %s)...", tiqueteParaDownload)
	rawJSON, err := rfbClient.DownloadArquivo(token, tiqueteParaDownload)
	if err != nil {
		log.Printf("[RFB Creditos] ERRO no download: %v", err)
		updateRequestError(db, requestID, "DOWNLOAD_ERROR", err.Error())
		return fmt.Errorf("failed to download: %w", err)
	}
	log.Printf("[RFB Creditos] JSON baixado (%.2f MB)", float64(len(rawJSON))/1024/1024)

	if _, saveErr := db.Exec(`
		UPDATE rfb_requests SET raw_json = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2
	`, string(rawJSON), requestID); saveErr != nil {
		log.Printf("[RFB Creditos] AVISO: falha ao salvar raw_json: %v (processamento continua)", saveErr)
	} else {
		log.Printf("[RFB Creditos] raw_json salvo com sucesso")
	}

	log.Printf("[RFB Creditos] Etapa 3/4: Interpretando JSON...")
	var creditos RFBCreditosJSON
	if err := json.Unmarshal(rawJSON, &creditos); err != nil {
		log.Printf("[RFB Creditos] ERRO no parse: %v", err)
		updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao interpretar JSON de créditos: "+err.Error())
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	grupos := 0
	if creditos.ApuracaoCorrente != nil {
		grupos++
	}
	if creditos.ApuracaoAjuste != nil {
		grupos++
	}
	if creditos.DebitosExtemporaneos != nil {
		grupos++
	}
	log.Printf("[RFB Creditos] JSON interpretado | grupos: %d (corrente=%v, ajuste=%v, extemporaneo=%v) | flat creditos: %d | flat debitos: %d",
		grupos,
		creditos.ApuracaoCorrente != nil,
		creditos.ApuracaoAjuste != nil,
		creditos.DebitosExtemporaneos != nil,
		len(creditos.Creditos),
		len(creditos.Debitos),
	)

	tx, err := db.Begin()
	if err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var totalCorrente, totalAjuste, totalExtemporaneo, totalFlat, insertErrors int
	var valorTotal, valorExtinto, valorNaoExtinto float64
	var dataApuracao string

	log.Printf("[RFB Creditos] Etapa 4/4: Inserindo registros...")

	// Grupo corrente — processa "creditos" e "debitos" (buyer NF-e pode vir em qualquer campo)
	if creditos.ApuracaoCorrente != nil {
		nc := len(creditos.ApuracaoCorrente.Creditos)
		nd := len(creditos.ApuracaoCorrente.Debitos)
		log.Printf("[RFB Creditos] ApuracaoCorrente: %d créditos + %d débitos (perspectiva comprador)", nc, nd)
		for _, c := range creditos.ApuracaoCorrente.Creditos {
			if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
				log.Printf("[RFB Creditos] ERRO crédito corrente (chave=%s): %v", c.ChaveDfe, err)
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
		for _, c := range creditos.ApuracaoCorrente.Debitos {
			if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
				log.Printf("[RFB Creditos] ERRO débito-comprador corrente (chave=%s): %v", c.ChaveDfe, err)
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
		log.Printf("[RFB Creditos] ApuracaoCorrente inserida: %d registros", totalCorrente)
	}

	// Grupo ajuste
	if creditos.ApuracaoAjuste != nil {
		nc := len(creditos.ApuracaoAjuste.Creditos)
		nd := len(creditos.ApuracaoAjuste.Debitos)
		log.Printf("[RFB Creditos] ApuracaoAjuste: %d créditos + %d débitos", nc, nd)
		ajusteAntes := totalAjuste
		for _, c := range creditos.ApuracaoAjuste.Creditos {
			if err := insertCredito(tx, requestID, companyID, "ajuste", c); err != nil {
				log.Printf("[RFB Creditos] ERRO crédito ajuste (chave=%s): %v", c.ChaveDfe, err)
				insertErrors++
			} else {
				totalAjuste++
				valorTotal += c.ValorCBSTotal
				valorExtinto += c.ValorCBSExtinto
				valorNaoExtinto += c.ValorCBSNaoExtinto
			}
		}
		for _, c := range creditos.ApuracaoAjuste.Debitos {
			if err := insertCredito(tx, requestID, companyID, "ajuste", c); err != nil {
				log.Printf("[RFB Creditos] ERRO débito-comprador ajuste (chave=%s): %v", c.ChaveDfe, err)
				insertErrors++
			} else {
				totalAjuste++
				valorTotal += c.ValorCBSTotal
				valorExtinto += c.ValorCBSExtinto
				valorNaoExtinto += c.ValorCBSNaoExtinto
			}
		}
		log.Printf("[RFB Creditos] ApuracaoAjuste inserida: %d registros", totalAjuste-ajusteAntes)
	}

	// Extemporâneos
	if creditos.DebitosExtemporaneos != nil {
		nc := len(creditos.DebitosExtemporaneos.Creditos)
		nd := len(creditos.DebitosExtemporaneos.Debitos)
		log.Printf("[RFB Creditos] DebitosExtemporaneos: %d créditos + %d débitos", nc, nd)
		extAntes := totalExtemporaneo
		for _, c := range creditos.DebitosExtemporaneos.Creditos {
			if err := insertCredito(tx, requestID, companyID, "extemporaneo", c); err != nil {
				log.Printf("[RFB Creditos] ERRO crédito extemporaneo (chave=%s): %v", c.ChaveDfe, err)
				insertErrors++
			} else {
				totalExtemporaneo++
				valorTotal += c.ValorCBSTotal
				valorExtinto += c.ValorCBSExtinto
				valorNaoExtinto += c.ValorCBSNaoExtinto
			}
		}
		for _, c := range creditos.DebitosExtemporaneos.Debitos {
			if err := insertCredito(tx, requestID, companyID, "extemporaneo", c); err != nil {
				log.Printf("[RFB Creditos] ERRO débito-comprador extemporaneo (chave=%s): %v", c.ChaveDfe, err)
				insertErrors++
			} else {
				totalExtemporaneo++
				valorTotal += c.ValorCBSTotal
				valorExtinto += c.ValorCBSExtinto
				valorNaoExtinto += c.ValorCBSNaoExtinto
			}
		}
		log.Printf("[RFB Creditos] DebitosExtemporaneos inseridos: %d registros", totalExtemporaneo-extAntes)
	}

	// Fallback: estrutura plana
	for _, c := range creditos.Creditos {
		if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
			log.Printf("[RFB Creditos] ERRO crédito flat (chave=%s): %v", c.ChaveDfe, err)
			insertErrors++
		} else {
			totalFlat++
			valorTotal += c.ValorCBSTotal
			valorExtinto += c.ValorCBSExtinto
			valorNaoExtinto += c.ValorCBSNaoExtinto
			if dataApuracao == "" && c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
			}
		}
	}
	for _, c := range creditos.Debitos {
		if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
			log.Printf("[RFB Creditos] ERRO débito-flat (chave=%s): %v", c.ChaveDfe, err)
			insertErrors++
		} else {
			totalFlat++
			valorTotal += c.ValorCBSTotal
			valorExtinto += c.ValorCBSExtinto
			valorNaoExtinto += c.ValorCBSNaoExtinto
			if dataApuracao == "" && c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
			}
		}
	}
	if totalFlat > 0 {
		log.Printf("[RFB Creditos] Estrutura plana: %d registros", totalFlat)
	}

	totalCreditos := totalCorrente + totalAjuste + totalExtemporaneo + totalFlat

	log.Printf("[RFB Creditos] Totais: %d créditos | CBS R$ %.2f | extinto R$ %.2f | não extinto R$ %.2f | período: %s",
		totalCreditos, valorTotal, valorExtinto, valorNaoExtinto, dataApuracao)

	if totalCreditos > 0 {
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
		log.Printf("[RFB Creditos] Resumo atualizado (rfb_creditos_resumo)")
	} else {
		log.Printf("[RFB Creditos] Nenhum crédito encontrado no JSON — rfb_creditos_resumo não atualizado")
	}

	if err := tx.Commit(); err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha no commit: "+err.Error())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	updateRequestStatus(db, requestID, "completed")

	log.Printf("[RFB Creditos] ============================================================")
	if insertErrors > 0 {
		log.Printf("[RFB Creditos] AVISO: %d registros falharam na inserção", insertErrors)
	}
	log.Printf("[RFB Creditos] CONCLUÍDO | request: %s | %d créditos | CBS R$ %.2f | status → completed",
		requestID, totalCreditos, valorTotal)
	log.Printf("[RFB Creditos] ============================================================")
	return nil
}

// ReprocessarRawJSONCreditosRFB re-parses the raw JSON stored for a tipo='credito' request.
func ReprocessarRawJSONCreditosRFB(db *sql.DB, requestID string) error {
	log.Printf("[RFB Creditos Reprocess] ============================================================")
	log.Printf("[RFB Creditos Reprocess] Iniciando reprocessamento | request: %s", requestID)

	var companyID string
	var rawJSON *string
	if err := db.QueryRow(`SELECT company_id, raw_json FROM rfb_requests WHERE id = $1`,
		requestID).Scan(&companyID, &rawJSON); err != nil {
		return fmt.Errorf("failed to fetch request: %w", err)
	}
	if rawJSON == nil || *rawJSON == "" {
		return fmt.Errorf("no raw_json stored for request %s", requestID)
	}

	log.Printf("[RFB Creditos Reprocess] JSON encontrado (%.2f MB) | company: %s",
		float64(len(*rawJSON))/1024/1024, companyID)

	res, err := db.Exec(`
		UPDATE rfb_requests SET status = 'reprocessing', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status NOT IN ('downloading', 'reprocessing')
	`, requestID)
	if err != nil {
		return fmt.Errorf("failed to claim reprocess status: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		log.Printf("[RFB Creditos Reprocess] Request %s já sendo processado — abortando", requestID)
		return nil
	}

	var creditos RFBCreditosJSON
	if err := json.Unmarshal([]byte(*rawJSON), &creditos); err != nil {
		updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao reprocessar JSON de créditos: "+err.Error())
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	resD, err := tx.Exec(`DELETE FROM rfb_creditos WHERE request_id = $1`, requestID)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao limpar créditos anteriores: "+err.Error())
		return fmt.Errorf("failed to delete existing credits: %w", err)
	}
	deleted, _ := resD.RowsAffected()
	log.Printf("[RFB Creditos Reprocess] Removidos: %d créditos anteriores", deleted)

	var totalCorrente, totalAjuste, totalExtemporaneo, totalFlat, insertErrors int
	var valorTotal, valorExtinto, valorNaoExtinto float64
	var dataApuracao string

	insertOne := func(tipoApuracao string, c RFBCredito) {
		if err := insertCredito(tx, requestID, companyID, tipoApuracao, c); err != nil {
			log.Printf("[RFB Creditos Reprocess] ERRO (tipo=%s chave=%s): %v", tipoApuracao, c.ChaveDfe, err)
			insertErrors++
			return
		}
		valorTotal += c.ValorCBSTotal
		valorExtinto += c.ValorCBSExtinto
		valorNaoExtinto += c.ValorCBSNaoExtinto
		if dataApuracao == "" && c.DataApuracao != "" {
			dataApuracao = c.DataApuracao
		}
	}

	if creditos.ApuracaoCorrente != nil {
		for _, c := range creditos.ApuracaoCorrente.Creditos {
			insertOne("corrente", c)
			totalCorrente++
		}
		for _, c := range creditos.ApuracaoCorrente.Debitos {
			insertOne("corrente", c)
			totalCorrente++
		}
	}
	if creditos.ApuracaoAjuste != nil {
		for _, c := range creditos.ApuracaoAjuste.Creditos {
			insertOne("ajuste", c)
			totalAjuste++
		}
		for _, c := range creditos.ApuracaoAjuste.Debitos {
			insertOne("ajuste", c)
			totalAjuste++
		}
	}
	if creditos.DebitosExtemporaneos != nil {
		for _, c := range creditos.DebitosExtemporaneos.Creditos {
			insertOne("extemporaneo", c)
			totalExtemporaneo++
		}
		for _, c := range creditos.DebitosExtemporaneos.Debitos {
			insertOne("extemporaneo", c)
			totalExtemporaneo++
		}
	}
	for _, c := range creditos.Creditos {
		insertOne("corrente", c)
		totalFlat++
	}
	for _, c := range creditos.Debitos {
		insertOne("corrente", c)
		totalFlat++
	}

	totalCreditos := totalCorrente + totalAjuste + totalExtemporaneo + totalFlat
	log.Printf("[RFB Creditos Reprocess] Totais: %d créditos | CBS R$ %.2f | período: %s",
		totalCreditos, valorTotal, dataApuracao)

	if totalCreditos > 0 {
		if _, err = tx.Exec(`
			INSERT INTO rfb_creditos_resumo (request_id, company_id, data_apuracao, total_creditos,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto, total_corrente, total_ajuste)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (company_id, data_apuracao)
			DO UPDATE SET request_id = $1, total_creditos = $4,
				valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
				total_corrente = $8, total_ajuste = $9
		`, requestID, companyID, dataApuracao, totalCreditos,
			valorTotal, valorExtinto, valorNaoExtinto, totalCorrente, totalAjuste); err != nil {
			tx.Rollback()
			updateRequestError(db, requestID, "DB_ERROR", "Falha ao salvar resumo: "+err.Error())
			return fmt.Errorf("failed to upsert credits summary: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha no commit: "+err.Error())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	updateRequestStatus(db, requestID, "completed")
	log.Printf("[RFB Creditos Reprocess] CONCLUÍDO | %d créditos | status → completed", totalCreditos)
	log.Printf("[RFB Creditos Reprocess] ============================================================")
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
