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
	Debitos  []RFBDebito  `json:"debitos"`
	Creditos []RFBCredito `json:"creditos"`
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
	SituacaoDebito     FlexString      `json:"situacao"`
	FormasExtincao     json.RawMessage `json:"formasExtincao"`
	Eventos            json.RawMessage `json:"eventos"`
}

// rfbAggBucket holds aggregated totals for one tipo_apuracao bucket (count + monetary sums).
type rfbAggBucket struct {
	Count           int
	ValorTotal      float64
	ValorExtinto    float64
	ValorNaoExtinto float64
}

// aggregateRfbDebitosSQL agrega rfb_debitos por tipo_apuracao (corrente/ajuste/extemporaneo)
// para company_id+data_apuracao, dentro da tx corrente. Considera SOMENTE linhas com
// chave_dfe preenchida — essas são as únicas deduplicadas via UPSERT ON CONFLICT
// (company_id, chave_dfe); refletem o estado real acumulado da tabela para o período,
// independente do payload da chamada atual ser completo ou parcial (API incremental).
// Linhas sem chave_dfe NÃO têm dedup e por isso ficam de fora — o chamador deve somar
// separadamente, em memória, apenas os itens sem chave da chamada atual (ver
// spec-rfb-resumo-agregacao-sql.md, Spec Change Log / Loopback 1).
func aggregateRfbDebitosSQL(tx *sql.Tx, companyID, dataApuracao string) (corrente, ajuste, extemporaneo rfbAggBucket, err error) {
	rows, err := tx.Query(`
		SELECT tipo_apuracao,
		       COUNT(*),
		       COALESCE(SUM(valor_cbs_total), 0),
		       COALESCE(SUM(valor_cbs_extinto), 0),
		       COALESCE(SUM(valor_cbs_nao_extinto), 0)
		FROM rfb_debitos
		WHERE company_id = $1 AND data_apuracao = $2
		  AND chave_dfe IS NOT NULL AND chave_dfe <> ''
		GROUP BY tipo_apuracao
	`, companyID, dataApuracao)
	if err != nil {
		return corrente, ajuste, extemporaneo, fmt.Errorf("aggregate rfb_debitos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tipo string
		var b rfbAggBucket
		if scanErr := rows.Scan(&tipo, &b.Count, &b.ValorTotal, &b.ValorExtinto, &b.ValorNaoExtinto); scanErr != nil {
			return corrente, ajuste, extemporaneo, fmt.Errorf("scan rfb_debitos agg: %w", scanErr)
		}
		switch tipo {
		case "ajuste":
			ajuste = b
		case "extemporaneo":
			extemporaneo = b
		default:
			// "corrente" e qualquer tipo não mapeado caem no bucket corrente (fallback).
			corrente = b
		}
	}
	if err := rows.Err(); err != nil {
		return corrente, ajuste, extemporaneo, fmt.Errorf("iterate rfb_debitos agg: %w", err)
	}
	return corrente, ajuste, extemporaneo, nil
}

// aggregateRfbCreditosSQL agrega rfb_creditos por tipo_apuracao para company_id+data_apuracao,
// dentro da tx corrente, considerando somente linhas com chave_dfe preenchida (mesma lógica
// de dedup de aggregateRfbDebitosSQL). Bucket créditos: tipo_apuracao <> 'ajuste' (incluindo
// "extemporaneo" e a estrutura plana, que é sempre inserida como "corrente") conta em
// total_corrente — decisão do humano confirmada na spec, não é regressão.
func aggregateRfbCreditosSQL(tx *sql.Tx, companyID, dataApuracao string) (corrente, ajuste rfbAggBucket, err error) {
	// rfb_creditos.tipo_apuracao é VARCHAR(50) SEM NOT NULL (migration 096_rfb_creditos.sql,
	// diferente de rfb_debitos.tipo_apuracao que é NOT NULL) — COALESCE evita que um valor
	// NULL quebre o rows.Scan (o que abortaria a transação inteira via rollback).
	rows, err := tx.Query(`
		SELECT COALESCE(tipo_apuracao, ''),
		       COUNT(*),
		       COALESCE(SUM(valor_cbs_total), 0),
		       COALESCE(SUM(valor_cbs_extinto), 0),
		       COALESCE(SUM(valor_cbs_nao_extinto), 0)
		FROM rfb_creditos
		WHERE company_id = $1 AND data_apuracao = $2
		  AND chave_dfe IS NOT NULL AND chave_dfe <> ''
		GROUP BY COALESCE(tipo_apuracao, '')
	`, companyID, dataApuracao)
	if err != nil {
		return corrente, ajuste, fmt.Errorf("aggregate rfb_creditos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tipo string
		var b rfbAggBucket
		if scanErr := rows.Scan(&tipo, &b.Count, &b.ValorTotal, &b.ValorExtinto, &b.ValorNaoExtinto); scanErr != nil {
			return corrente, ajuste, fmt.Errorf("scan rfb_creditos agg: %w", scanErr)
		}
		if tipo == "ajuste" {
			ajuste = b
		} else {
			corrente.Count += b.Count
			corrente.ValorTotal += b.ValorTotal
			corrente.ValorExtinto += b.ValorExtinto
			corrente.ValorNaoExtinto += b.ValorNaoExtinto
		}
	}
	if err := rows.Err(); err != nil {
		return corrente, ajuste, fmt.Errorf("iterate rfb_creditos agg: %w", err)
	}
	return corrente, ajuste, nil
}

// ProcessarDownloadRFB downloads and processes the RFB CBS assessment JSON.
// It saves the raw JSON, normalizes debits into rfb_debitos, and creates a summary in rfb_resumo.
// All DB writes (debits + summary) are wrapped in a single transaction to prevent partial imports.
// An atomic status update prevents two goroutines from processing the same request concurrently.
func ProcessarDownloadRFB(db *sql.DB, rfbClient *RFBClient, requestID string) error {
	log.Printf("[RFB Processor] Starting download processing for request %s", requestID)

	// 1. Fetch request details and company credentials
	var companyID, tiquete, cnpjBase, ambiente string
	var tiqueteDownload *string
	err := db.QueryRow(`
		SELECT r.company_id, r.tiquete, r.cnpj_base, r.tiquete_download, COALESCE(r.ambiente, 'producao')
		FROM rfb_requests r
		WHERE r.id = $1
	`, requestID).Scan(&companyID, &tiquete, &cnpjBase, &tiqueteDownload, &ambiente)
	if err != nil {
		return fmt.Errorf("failed to fetch request: %w", err)
	}

	var clientID, clientSecret string
	err = db.QueryRow(`
		SELECT client_id, client_secret FROM rfb_credentials
		WHERE company_id = $1 AND ativo = true
	`, companyID).Scan(&clientID, &clientSecret)
	if err != nil {
		updateRequestError(db, requestID, "CRED_NOT_FOUND", "Credenciais RFB não encontradas ou inativas")
		return fmt.Errorf("failed to fetch credentials: %w", err)
	}

	// Ambiente da SOLICITAÇÃO, não do cadastro da credencial: se o cadastro mudar entre
	// solicitar e baixar, o download precisa ir no mesmo prefixo (rtc/prr-rtc) do tíquete.
	log.Printf("[RFB Processor] Request %s — ambiente da solicitação: %s", requestID, ambiente)
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

	// Créditos embutidos na mesma resposta (a RFB manda a NF-e de entrada do comprador
	// junto com a apuração de débitos)
	var totalCreditosCorrente, totalCreditosAjuste int
	var valorCreditosTotal, valorCreditosExtinto, valorCreditosNaoExtinto float64
	// Período dos créditos embutidos, derivado dos próprios itens de crédito — pode
	// divergir de dataApuracao (débitos), ex.: resposta só com créditos extemporâneos
	// e débitos correntes de outro período. Usado para agregar/gravar rfb_creditos_resumo
	// separadamente de rfb_resumo.
	var dataApuracaoCreditos string

	// Acumuladores em memória SOMENTE das linhas sem chave_dfe desta chamada — sem
	// chave não há UPSERT/dedup, então a agregação SQL (que só enxerga linhas com
	// chave_dfe) não pode contá-las; ver aggregateRfbDebitosSQL/aggregateRfbCreditosSQL.
	var semChaveCorrente, semChaveAjuste, semChaveExtemporaneo rfbAggBucket
	var semChaveCreditosCorrente, semChaveCreditosAjuste rfbAggBucket

	if apuracao.ApuracaoCorrente != nil {
		for _, d := range apuracao.ApuracaoCorrente.Debitos {
			if err := insertDebito(tx, requestID, companyID, "corrente", d); err != nil {
				log.Printf("[RFB Processor] Error inserting corrente debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveCorrente.Count++
					semChaveCorrente.ValorTotal += d.ValorCBSTotal
					semChaveCorrente.ValorExtinto += d.ValorCBSExtinto
					semChaveCorrente.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.ApuracaoCorrente.Creditos {
			if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
				log.Printf("[RFB Processor] Error inserting corrente credito (chave=%s): %v", c.ChaveDfe, err)
			} else {
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					semChaveCreditosCorrente.Count++
					semChaveCreditosCorrente.ValorTotal += c.ValorCBSTotal
					semChaveCreditosCorrente.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosCorrente.ValorNaoExtinto += c.ValorCBSNaoExtinto
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
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveAjuste.Count++
					semChaveAjuste.ValorTotal += d.ValorCBSTotal
					semChaveAjuste.ValorExtinto += d.ValorCBSExtinto
					semChaveAjuste.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.ApuracaoAjuste.Creditos {
			if err := insertCredito(tx, requestID, companyID, "ajuste", c); err != nil {
				log.Printf("[RFB Processor] Error inserting ajuste credito (chave=%s): %v", c.ChaveDfe, err)
			} else {
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					semChaveCreditosAjuste.Count++
					semChaveCreditosAjuste.ValorTotal += c.ValorCBSTotal
					semChaveCreditosAjuste.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosAjuste.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
	}

	if apuracao.DebitosExtemporaneos != nil {
		for _, d := range apuracao.DebitosExtemporaneos.Debitos {
			if err := insertDebito(tx, requestID, companyID, "extemporaneo", d); err != nil {
				log.Printf("[RFB Processor] Error inserting extemporaneo debit (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveExtemporaneo.Count++
					semChaveExtemporaneo.ValorTotal += d.ValorCBSTotal
					semChaveExtemporaneo.ValorExtinto += d.ValorCBSExtinto
					semChaveExtemporaneo.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.DebitosExtemporaneos.Creditos {
			if err := insertCredito(tx, requestID, companyID, "extemporaneo", c); err != nil {
				log.Printf("[RFB Processor] Error inserting extemporaneo credito (chave=%s): %v", c.ChaveDfe, err)
			} else {
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					// Créditos extemporâneos entram no bucket corrente (mesma regra de
					// aggregateRfbCreditosSQL: tipo_apuracao <> 'ajuste' → total_corrente).
					semChaveCreditosCorrente.Count++
					semChaveCreditosCorrente.ValorTotal += c.ValorCBSTotal
					semChaveCreditosCorrente.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosCorrente.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
	}

	// Fallback: se nenhum débito de nenhum bloco (corrente/ajuste/extemporaneo) forneceu
	// dataApuracao, tenta extrair de qualquer crédito embutido (mesmos três blocos) —
	// cobre o caso de resposta só com itens extemporâneos, que antes deixava dataApuracao
	// vazio e fazia a agregação SQL filtrar por data_apuracao='' (período errado).
	if dataApuracao == "" && apuracao.ApuracaoCorrente != nil {
		for _, c := range apuracao.ApuracaoCorrente.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				break
			}
		}
	}
	if dataApuracao == "" && apuracao.ApuracaoAjuste != nil {
		for _, c := range apuracao.ApuracaoAjuste.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				break
			}
		}
	}
	if dataApuracao == "" && apuracao.DebitosExtemporaneos != nil {
		for _, c := range apuracao.DebitosExtemporaneos.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				break
			}
		}
	}

	// Período dos créditos embutidos: se nenhum crédito processado forneceu dataApuracao
	// (ex.: resposta sem créditos nesta chamada), reaproveita o período dos débitos como
	// melhor esforço — mantém rfb_creditos_resumo alinhado a rfb_resumo quando não há
	// sinal próprio dos créditos.
	if dataApuracaoCreditos == "" {
		dataApuracaoCreditos = dataApuracao
	}

	// Agregação SQL sobre rfb_debitos/rfb_creditos (linhas com chave_dfe, já deduplicadas
	// via UPSERT) + acumulador em memória das linhas sem chave desta chamada. Roda dentro
	// da mesma tx, depois de todos os insertDebito/insertCredito, antes do commit — reflete
	// o estado real acumulado da tabela para o período, não só o delta da chamada atual
	// (prep para consulta incremental da RFB a partir de out/2026).
	sqlCorrente, sqlAjuste, sqlExtemporaneo, aggErr := aggregateRfbDebitosSQL(tx, companyID, dataApuracao)
	if aggErr != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao agregar resumo de débitos: "+aggErr.Error())
		return fmt.Errorf("failed to aggregate rfb_debitos summary: %w", aggErr)
	}
	totalCorrente = sqlCorrente.Count + semChaveCorrente.Count
	totalAjuste = sqlAjuste.Count + semChaveAjuste.Count
	totalExtemporaneo = sqlExtemporaneo.Count + semChaveExtemporaneo.Count
	valorTotal = sqlCorrente.ValorTotal + sqlAjuste.ValorTotal + sqlExtemporaneo.ValorTotal +
		semChaveCorrente.ValorTotal + semChaveAjuste.ValorTotal + semChaveExtemporaneo.ValorTotal
	valorExtinto = sqlCorrente.ValorExtinto + sqlAjuste.ValorExtinto + sqlExtemporaneo.ValorExtinto +
		semChaveCorrente.ValorExtinto + semChaveAjuste.ValorExtinto + semChaveExtemporaneo.ValorExtinto
	valorNaoExtinto = sqlCorrente.ValorNaoExtinto + sqlAjuste.ValorNaoExtinto + sqlExtemporaneo.ValorNaoExtinto +
		semChaveCorrente.ValorNaoExtinto + semChaveAjuste.ValorNaoExtinto + semChaveExtemporaneo.ValorNaoExtinto
	totalDebitos := totalCorrente + totalAjuste + totalExtemporaneo

	sqlCredCorrente, sqlCredAjuste, aggCredErr := aggregateRfbCreditosSQL(tx, companyID, dataApuracaoCreditos)
	if aggCredErr != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao agregar resumo de créditos embutidos: "+aggCredErr.Error())
		return fmt.Errorf("failed to aggregate rfb_creditos summary: %w", aggCredErr)
	}
	totalCreditosCorrente = sqlCredCorrente.Count + semChaveCreditosCorrente.Count
	totalCreditosAjuste = sqlCredAjuste.Count + semChaveCreditosAjuste.Count
	valorCreditosTotal = sqlCredCorrente.ValorTotal + sqlCredAjuste.ValorTotal +
		semChaveCreditosCorrente.ValorTotal + semChaveCreditosAjuste.ValorTotal
	valorCreditosExtinto = sqlCredCorrente.ValorExtinto + sqlCredAjuste.ValorExtinto +
		semChaveCreditosCorrente.ValorExtinto + semChaveCreditosAjuste.ValorExtinto
	valorCreditosNaoExtinto = sqlCredCorrente.ValorNaoExtinto + sqlCredAjuste.ValorNaoExtinto +
		semChaveCreditosCorrente.ValorNaoExtinto + semChaveCreditosAjuste.ValorNaoExtinto
	totalCreditos := totalCreditosCorrente + totalCreditosAjuste

	log.Printf("[RFB Processor] Resumo agregado (SQL+memória) | request %s | período débitos %s: %d débitos (%d corrente, %d ajuste, %d extemporaneo), CBS R$ %.2f | período créditos %s: %d créditos embutidos (%d corrente, %d ajuste), CBS R$ %.2f",
		requestID, dataApuracao, totalDebitos, totalCorrente, totalAjuste, totalExtemporaneo, valorTotal,
		dataApuracaoCreditos, totalCreditos, totalCreditosCorrente, totalCreditosAjuste, valorCreditosTotal)

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

	// Upsert créditos resumo se houver créditos na resposta
	if totalCreditos > 0 {
		if _, credErr := tx.Exec(`
			INSERT INTO rfb_creditos_resumo (request_id, company_id, data_apuracao, total_creditos,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto, total_corrente, total_ajuste)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (company_id, data_apuracao)
			DO UPDATE SET request_id = $1, total_creditos = $4,
				valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
				total_corrente = $8, total_ajuste = $9
		`, requestID, companyID, dataApuracaoCreditos, totalCreditos,
			valorCreditosTotal, valorCreditosExtinto, valorCreditosNaoExtinto,
			totalCreditosCorrente, totalCreditosAjuste); credErr != nil {
			log.Printf("[RFB Processor] WARNING: failed to upsert credits summary: %v", credErr)
		} else {
			log.Printf("[RFB Processor] Credits found: %d (%d corrente, %d ajuste), CBS total: %.2f",
				totalCreditos, totalCreditosCorrente, totalCreditosAjuste, valorCreditosTotal)
		}
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

	if finalStatus == "completed" {
		go TriggerSAPSync(db, requestID)
	}

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
		ON CONFLICT (company_id, chave_dfe) WHERE chave_dfe IS NOT NULL AND chave_dfe != ''
		DO UPDATE SET
			request_id            = EXCLUDED.request_id,
			tipo_apuracao         = EXCLUDED.tipo_apuracao,
			data_dfe_emissao      = EXCLUDED.data_dfe_emissao,
			data_apuracao         = EXCLUDED.data_apuracao,
			valor_cbs_total       = EXCLUDED.valor_cbs_total,
			valor_cbs_extinto     = EXCLUDED.valor_cbs_extinto,
			valor_cbs_nao_extinto = EXCLUDED.valor_cbs_nao_extinto,
			situacao_debito       = EXCLUDED.situacao_debito,
			formas_extincao       = EXCLUDED.formas_extincao,
			eventos               = EXCLUDED.eventos
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
	log.Printf("[RFB Reprocess] ============================================================")
	log.Printf("[RFB Reprocess] Iniciando reprocessamento | request: %s", requestID)
	log.Printf("[RFB Reprocess] ============================================================")

	var companyID string
	var rawJSON *string
	err := db.QueryRow(`
		SELECT company_id, raw_json FROM rfb_requests WHERE id = $1
	`, requestID).Scan(&companyID, &rawJSON)
	if err != nil {
		log.Printf("[RFB Reprocess] ERRO ao buscar request: %v", err)
		return fmt.Errorf("failed to fetch request: %w", err)
	}
	if rawJSON == nil || *rawJSON == "" {
		log.Printf("[RFB Reprocess] ERRO: raw_json não encontrado para request %s — não é possível reprocessar sem o JSON original", requestID)
		return fmt.Errorf("no raw_json stored for request %s — cannot reprocess without raw data", requestID)
	}

	jsonSizeMB := float64(len(*rawJSON)) / 1024 / 1024
	log.Printf("[RFB Reprocess] JSON original encontrado (%.2f MB) | company: %s", jsonSizeMB, companyID)

	// 1. Atomic status claim — prevent concurrent reprocess runs
	res, err := db.Exec(`
		UPDATE rfb_requests SET status = 'reprocessing', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status NOT IN ('downloading', 'reprocessing')
	`, requestID)
	if err != nil {
		return fmt.Errorf("failed to claim reprocess status: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		log.Printf("[RFB Reprocess] Request %s já está sendo processado em outra goroutine — abortando", requestID)
		return nil
	}
	log.Printf("[RFB Reprocess] Status → reprocessing")

	// 2. Parse JSON FIRST — before touching any existing data.
	log.Printf("[RFB Reprocess] Etapa 1/4: Interpretando JSON...")
	var apuracao RFBApuracaoJSON
	if err := json.Unmarshal([]byte(*rawJSON), &apuracao); err != nil {
		log.Printf("[RFB Reprocess] ERRO no parse do JSON: %v", err)
		updateRequestError(db, requestID, "PARSE_ERROR", "Falha ao reprocessar JSON: "+err.Error())
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	grupos := 0
	if apuracao.ApuracaoCorrente != nil {
		grupos++
	}
	if apuracao.ApuracaoAjuste != nil {
		grupos++
	}
	if apuracao.DebitosExtemporaneos != nil {
		grupos++
	}
	log.Printf("[RFB Reprocess] JSON interpretado com sucesso | grupos encontrados: %d (corrente=%v, ajuste=%v, extemporaneo=%v)",
		grupos,
		apuracao.ApuracaoCorrente != nil,
		apuracao.ApuracaoAjuste != nil,
		apuracao.DebitosExtemporaneos != nil,
	)

	// 3. Transaction: delete old debits then insert new ones atomically.
	log.Printf("[RFB Reprocess] Etapa 2/4: Limpando dados anteriores...")
	tx, err := db.Begin()
	if err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao iniciar transação: "+err.Error())
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var deletedDebitos, deletedCreditos int64
	resD, err := tx.Exec(`DELETE FROM rfb_debitos WHERE request_id = $1`, requestID)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao limpar débitos anteriores: "+err.Error())
		return fmt.Errorf("failed to delete existing debits: %w", err)
	}
	deletedDebitos, _ = resD.RowsAffected()

	resC, err := tx.Exec(`DELETE FROM rfb_creditos WHERE request_id = $1`, requestID)
	if err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao limpar créditos anteriores: "+err.Error())
		return fmt.Errorf("failed to delete existing credits: %w", err)
	}
	deletedCreditos, _ = resC.RowsAffected()
	log.Printf("[RFB Reprocess] Removidos: %d débitos e %d créditos anteriores", deletedDebitos, deletedCreditos)

	var totalCorrente, totalAjuste, totalExtemporaneo, insertErrors int
	var valorTotal, valorExtinto, valorNaoExtinto float64
	var dataApuracao string
	var totalCreditosCorrente, totalCreditosAjuste int
	var valorCreditosTotal, valorCreditosExtinto, valorCreditosNaoExtinto float64
	// Período dos créditos embutidos, derivado dos próprios itens de crédito — pode
	// divergir de dataApuracao (débitos). Ver comentário equivalente em ProcessarDownloadRFB.
	var dataApuracaoCreditos string

	// Acumuladores em memória SOMENTE das linhas sem chave_dfe desta chamada — ver
	// aggregateRfbDebitosSQL/aggregateRfbCreditosSQL e Spec Change Log / Loopback 1.
	var semChaveCorrente, semChaveAjuste, semChaveExtemporaneo rfbAggBucket
	var semChaveCreditosCorrente, semChaveCreditosAjuste rfbAggBucket

	log.Printf("[RFB Reprocess] Etapa 3/4: Inserindo registros...")

	if apuracao.ApuracaoCorrente != nil {
		nd := len(apuracao.ApuracaoCorrente.Debitos)
		log.Printf("[RFB Reprocess] ApuracaoCorrente: %d débitos (créditos embutidos serão contados no loop)", nd)
		insDeb, insCred := 0, 0
		for _, d := range apuracao.ApuracaoCorrente.Debitos {
			if err := insertDebito(tx, requestID, companyID, "corrente", d); err != nil {
				log.Printf("[RFB Reprocess] ERRO débito corrente (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				insDeb++
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveCorrente.Count++
					semChaveCorrente.ValorTotal += d.ValorCBSTotal
					semChaveCorrente.ValorExtinto += d.ValorCBSExtinto
					semChaveCorrente.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.ApuracaoCorrente.Creditos {
			if err := insertCredito(tx, requestID, companyID, "corrente", c); err != nil {
				log.Printf("[RFB Reprocess] ERRO crédito corrente (chave=%s): %v", c.ChaveDfe, err)
			} else {
				insCred++
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					semChaveCreditosCorrente.Count++
					semChaveCreditosCorrente.ValorTotal += c.ValorCBSTotal
					semChaveCreditosCorrente.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosCorrente.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
		log.Printf("[RFB Reprocess] ApuracaoCorrente inserida: %d/%d débitos, %d créditos CBS", insDeb, nd, insCred)
	}

	if apuracao.ApuracaoAjuste != nil {
		nd := len(apuracao.ApuracaoAjuste.Debitos)
		log.Printf("[RFB Reprocess] ApuracaoAjuste: %d débitos", nd)
		insDeb, insCred := 0, 0
		for _, d := range apuracao.ApuracaoAjuste.Debitos {
			if err := insertDebito(tx, requestID, companyID, "ajuste", d); err != nil {
				log.Printf("[RFB Reprocess] ERRO débito ajuste (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				insDeb++
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveAjuste.Count++
					semChaveAjuste.ValorTotal += d.ValorCBSTotal
					semChaveAjuste.ValorExtinto += d.ValorCBSExtinto
					semChaveAjuste.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.ApuracaoAjuste.Creditos {
			if err := insertCredito(tx, requestID, companyID, "ajuste", c); err != nil {
				log.Printf("[RFB Reprocess] ERRO crédito ajuste (chave=%s): %v", c.ChaveDfe, err)
			} else {
				insCred++
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					semChaveCreditosAjuste.Count++
					semChaveCreditosAjuste.ValorTotal += c.ValorCBSTotal
					semChaveCreditosAjuste.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosAjuste.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
		log.Printf("[RFB Reprocess] ApuracaoAjuste inserida: %d/%d débitos, %d créditos CBS", insDeb, nd, insCred)
	}

	if apuracao.DebitosExtemporaneos != nil {
		nd := len(apuracao.DebitosExtemporaneos.Debitos)
		log.Printf("[RFB Reprocess] DebitosExtemporaneos: %d débitos", nd)
		insDeb, insCred := 0, 0
		for _, d := range apuracao.DebitosExtemporaneos.Debitos {
			if err := insertDebito(tx, requestID, companyID, "extemporaneo", d); err != nil {
				log.Printf("[RFB Reprocess] ERRO débito extemporaneo (chave=%s): %v", d.ChaveDfe, err)
				insertErrors++
			} else {
				insDeb++
				if dataApuracao == "" && d.DataApuracao != "" {
					dataApuracao = d.DataApuracao
				}
				if string(d.ChaveDfe) == "" {
					semChaveExtemporaneo.Count++
					semChaveExtemporaneo.ValorTotal += d.ValorCBSTotal
					semChaveExtemporaneo.ValorExtinto += d.ValorCBSExtinto
					semChaveExtemporaneo.ValorNaoExtinto += d.ValorCBSNaoExtinto
				}
			}
		}
		for _, c := range apuracao.DebitosExtemporaneos.Creditos {
			if err := insertCredito(tx, requestID, companyID, "extemporaneo", c); err != nil {
				log.Printf("[RFB Reprocess] ERRO crédito extemporaneo (chave=%s): %v", c.ChaveDfe, err)
			} else {
				insCred++
				if dataApuracaoCreditos == "" && c.DataApuracao != "" {
					dataApuracaoCreditos = c.DataApuracao
				}
				if string(c.ChaveDfe) == "" {
					// Créditos extemporâneos entram no bucket corrente (mesma regra de
					// aggregateRfbCreditosSQL: tipo_apuracao <> 'ajuste' → total_corrente).
					semChaveCreditosCorrente.Count++
					semChaveCreditosCorrente.ValorTotal += c.ValorCBSTotal
					semChaveCreditosCorrente.ValorExtinto += c.ValorCBSExtinto
					semChaveCreditosCorrente.ValorNaoExtinto += c.ValorCBSNaoExtinto
				}
			}
		}
		log.Printf("[RFB Reprocess] DebitosExtemporaneos inseridos: %d/%d débitos, %d créditos CBS", insDeb, nd, insCred)
	}

	// Fallback: se nenhum débito de nenhum bloco forneceu dataApuracao, tenta extrair de
	// qualquer crédito embutido (corrente/ajuste/extemporaneo) — cobre resposta só com
	// itens extemporâneos.
	if dataApuracao == "" && apuracao.ApuracaoCorrente != nil {
		for _, c := range apuracao.ApuracaoCorrente.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				log.Printf("[RFB Reprocess] dataApuracao obtido dos créditos correntes: %s", dataApuracao)
				break
			}
		}
	}
	if dataApuracao == "" && apuracao.ApuracaoAjuste != nil {
		for _, c := range apuracao.ApuracaoAjuste.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				log.Printf("[RFB Reprocess] dataApuracao obtido dos créditos ajuste: %s", dataApuracao)
				break
			}
		}
	}
	if dataApuracao == "" && apuracao.DebitosExtemporaneos != nil {
		for _, c := range apuracao.DebitosExtemporaneos.Creditos {
			if c.DataApuracao != "" {
				dataApuracao = c.DataApuracao
				log.Printf("[RFB Reprocess] dataApuracao obtido dos créditos extemporâneos: %s", dataApuracao)
				break
			}
		}
	}

	// Período dos créditos embutidos: se nenhum crédito processado forneceu dataApuracao,
	// reaproveita o período dos débitos como melhor esforço.
	if dataApuracaoCreditos == "" {
		dataApuracaoCreditos = dataApuracao
	}

	// Agregação SQL (linhas com chave_dfe, já deduplicadas via UPSERT) + acumulador em
	// memória das linhas sem chave desta chamada — mesma lógica de ProcessarDownloadRFB.
	// Roda dentro da mesma tx que fez o DELETE+reinsert acima, antes do commit, refletindo
	// o estado real da tabela para o período (não só as linhas deste request_id).
	sqlCorrente, sqlAjuste, sqlExtemporaneo, aggErr := aggregateRfbDebitosSQL(tx, companyID, dataApuracao)
	if aggErr != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao agregar resumo de débitos: "+aggErr.Error())
		return fmt.Errorf("failed to aggregate rfb_debitos summary: %w", aggErr)
	}
	totalCorrente = sqlCorrente.Count + semChaveCorrente.Count
	totalAjuste = sqlAjuste.Count + semChaveAjuste.Count
	totalExtemporaneo = sqlExtemporaneo.Count + semChaveExtemporaneo.Count
	valorTotal = sqlCorrente.ValorTotal + sqlAjuste.ValorTotal + sqlExtemporaneo.ValorTotal +
		semChaveCorrente.ValorTotal + semChaveAjuste.ValorTotal + semChaveExtemporaneo.ValorTotal
	valorExtinto = sqlCorrente.ValorExtinto + sqlAjuste.ValorExtinto + sqlExtemporaneo.ValorExtinto +
		semChaveCorrente.ValorExtinto + semChaveAjuste.ValorExtinto + semChaveExtemporaneo.ValorExtinto
	valorNaoExtinto = sqlCorrente.ValorNaoExtinto + sqlAjuste.ValorNaoExtinto + sqlExtemporaneo.ValorNaoExtinto +
		semChaveCorrente.ValorNaoExtinto + semChaveAjuste.ValorNaoExtinto + semChaveExtemporaneo.ValorNaoExtinto
	totalDebitos := totalCorrente + totalAjuste + totalExtemporaneo

	sqlCredCorrente, sqlCredAjuste, aggCredErr := aggregateRfbCreditosSQL(tx, companyID, dataApuracaoCreditos)
	if aggCredErr != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao agregar resumo de créditos embutidos: "+aggCredErr.Error())
		return fmt.Errorf("failed to aggregate rfb_creditos summary: %w", aggCredErr)
	}
	totalCreditosCorrente = sqlCredCorrente.Count + semChaveCreditosCorrente.Count
	totalCreditosAjuste = sqlCredAjuste.Count + semChaveCreditosAjuste.Count
	valorCreditosTotal = sqlCredCorrente.ValorTotal + sqlCredAjuste.ValorTotal +
		semChaveCreditosCorrente.ValorTotal + semChaveCreditosAjuste.ValorTotal
	valorCreditosExtinto = sqlCredCorrente.ValorExtinto + sqlCredAjuste.ValorExtinto +
		semChaveCreditosCorrente.ValorExtinto + semChaveCreditosAjuste.ValorExtinto
	valorCreditosNaoExtinto = sqlCredCorrente.ValorNaoExtinto + sqlCredAjuste.ValorNaoExtinto +
		semChaveCreditosCorrente.ValorNaoExtinto + semChaveCreditosAjuste.ValorNaoExtinto
	totalCreditos := totalCreditosCorrente + totalCreditosAjuste

	log.Printf("[RFB Reprocess] Etapa 4/4: Atualizando resumos (SQL+memória) | período débitos: %s | período créditos: %s", dataApuracao, dataApuracaoCreditos)
	log.Printf("[RFB Reprocess]   Débitos : %d total (%d corrente, %d ajuste, %d extemporaneo) | CBS R$ %.2f",
		totalDebitos, totalCorrente, totalAjuste, totalExtemporaneo, valorTotal)
	log.Printf("[RFB Reprocess]   Créditos: %d total (%d corrente+extemp, %d ajuste) | CBS R$ %.2f",
		totalCreditos, totalCreditosCorrente, totalCreditosAjuste, valorCreditosTotal)

	if _, err = tx.Exec(`
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
		totalCorrente, totalAjuste, totalExtemporaneo); err != nil {
		tx.Rollback()
		updateRequestError(db, requestID, "DB_ERROR", "Falha ao salvar resumo: "+err.Error())
		return fmt.Errorf("failed to upsert summary: %w", err)
	}
	log.Printf("[RFB Reprocess] Resumo de débitos atualizado (rfb_resumo)")

	if totalCreditos > 0 {
		if _, credErr := tx.Exec(`
			INSERT INTO rfb_creditos_resumo (request_id, company_id, data_apuracao, total_creditos,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto, total_corrente, total_ajuste)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (company_id, data_apuracao)
			DO UPDATE SET request_id = $1, total_creditos = $4,
				valor_cbs_total = $5, valor_cbs_extinto = $6, valor_cbs_nao_extinto = $7,
				total_corrente = $8, total_ajuste = $9
		`, requestID, companyID, dataApuracaoCreditos, totalCreditos,
			valorCreditosTotal, valorCreditosExtinto, valorCreditosNaoExtinto,
			totalCreditosCorrente, totalCreditosAjuste); credErr != nil {
			log.Printf("[RFB Reprocess] AVISO: falha ao atualizar resumo de créditos: %v", credErr)
		} else {
			log.Printf("[RFB Reprocess] Resumo de créditos atualizado (rfb_creditos_resumo)")
		}
	} else {
		log.Printf("[RFB Reprocess] Nenhum crédito encontrado — rfb_creditos_resumo não atualizado")
	}

	if err := tx.Commit(); err != nil {
		updateRequestError(db, requestID, "DB_ERROR", "Falha no commit: "+err.Error())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	updateRequestStatus(db, requestID, "completed")
	go TriggerSAPSync(db, requestID)

	log.Printf("[RFB Reprocess] ============================================================")
	if insertErrors > 0 {
		log.Printf("[RFB Reprocess] AVISO: %d débitos falharam na inserção", insertErrors)
	}
	log.Printf("[RFB Reprocess] CONCLUÍDO | request: %s | status → completed", requestID)
	log.Printf("[RFB Reprocess]   %d débitos | %d créditos | período: %s | CBS R$ %.2f",
		totalDebitos, totalCreditos, dataApuracao, valorTotal)
	log.Printf("[RFB Reprocess] ============================================================")
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
