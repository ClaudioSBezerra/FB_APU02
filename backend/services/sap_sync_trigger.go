package services

import (
	"database/sql"
	"log"

	"fb_apu02/crypto"

	"github.com/lib/pq"
)

// TriggerSAPSync é chamada de forma assíncrona (goroutine) pelos pontos de
// conclusão da apuração RFB (rfb_processor.go, rfb_creditos_processor.go) logo
// após updateRequestStatus(db, requestID, "completed"). Nunca deve propagar
// erro ao chamador — qualquer falha aqui é isolada e apenas logada, para que
// a apuração RFB em si nunca seja afetada por um problema de sincronização SAP.
//
// Escopo desta story (2.1): apenas o gatilho, o gating por empresa/BUKRS
// habilitado e o registro de execução em sap_sync_runs com checagem de
// idempotência. A chamada real ao SAP (SearchPayments, lotes, retry) é
// escopo da Story 2.2.
func TriggerSAPSync(db *sql.DB, requestID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SAP Sync] PANIC recuperado ao processar requestID=%s: %v", requestID, r)
		}
	}()

	var companyID string
	if err := db.QueryRow(`SELECT company_id FROM rfb_requests WHERE id = $1`, requestID).Scan(&companyID); err != nil {
		log.Printf("[SAP Sync] Não foi possível resolver company_id para requestID=%s: %v", requestID, err)
		return
	}

	var ativo bool
	var bukrsList []string
	var clientID, clientSecretEnc string
	var baseURL sql.NullString
	err := db.QueryRow(`
		SELECT ativo, bukrs_list, client_id, client_secret, base_url FROM sap_credentials WHERE company_id = $1
	`, companyID).Scan(&ativo, pq.Array(&bukrsList), &clientID, &clientSecretEnc, &baseURL)
	if err == sql.ErrNoRows {
		return // empresa sem integração SAP habilitada — 100% fluxo CSV
	}
	if err != nil {
		log.Printf("[SAP Sync] Erro ao buscar sap_credentials para company_id=%s: %v", companyID, err)
		return
	}
	if !ativo || len(bukrsList) == 0 {
		return // integração inativa ou sem BUKRS mapeado
	}
	if !baseURL.Valid || baseURL.String == "" {
		log.Printf("[SAP Sync] Base URL não configurada para company_id=%s — sincronização SAP ignorada", companyID)
		return
	}

	chaves, err := fetchChavesRecemGravadas(db, requestID)
	if err != nil {
		log.Printf("[SAP Sync] Erro ao buscar créditos recém-gravados para requestID=%s: %v", requestID, err)
		return
	}
	if len(chaves) == 0 {
		return // apuração concluída sem nenhum crédito novo — nada a sincronizar
	}

	for _, bukrs := range bukrsList {
		b := bukrs // captura de cópia local — evita compartilhamento da variável de loop
		syncBukrsSafe(db, companyID, b, requestID, clientID, clientSecretEnc, baseURL.String, chaves)
	}
}

// fetchChavesRecemGravadas retorna as chaves de DF-e (NF-e/CT-e) tocadas pela
// apuração requestID. Ver Dev Notes da story 2.1: request_id em rfb_creditos é
// sobrescrito a cada upsert, então isto reflete "chaves tocadas por este
// request" (novas ou reprocessadas), não estritamente "linhas novas" — é a
// definição operacional adotada por esta story dada a limitação do schema atual.
// chave_dfe é nullable no schema (índice parcial de rfb_creditos já exclui
// NULL/vazio) — linhas com chave_dfe NULL são ignoradas em vez de abortar o Scan.
func fetchChavesRecemGravadas(db *sql.DB, requestID string) ([]string, error) {
	rows, err := db.Query(`SELECT chave_dfe FROM rfb_creditos WHERE request_id = $1`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chaves []string
	for rows.Next() {
		var chave sql.NullString
		if err := rows.Scan(&chave); err != nil {
			return nil, err
		}
		if chave.Valid && chave.String != "" {
			chaves = append(chaves, chave.String)
		}
	}
	return chaves, rows.Err()
}

// syncBukrsSafe isola falhas (incluindo panics) por BUKRS individual — um
// problema ao processar um BUKRS não pode impedir o processamento dos demais
// BUKRS configurados para a mesma empresa.
func syncBukrsSafe(db *sql.DB, companyID, bukrs, requestID, clientID, clientSecretEnc, baseURL string, chaves []string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SAP Sync] PANIC recuperado ao processar bukrs=%s company_id=%s: %v", bukrs, companyID, r)
		}
	}()
	syncBukrs(db, companyID, bukrs, requestID, clientID, clientSecretEnc, baseURL, chaves)
}

// syncBukrs registra a execução de sincronização em sap_sync_runs para um
// BUKRS específico e chama o SAP (SearchPayments, Story 2.2). A idempotência
// (AC #5 da Story 2.1) é garantida pela constraint UNIQUE(company_id, bukrs,
// request_id) no banco — não por um SELECT prévio, que deixaria uma janela de
// corrida entre goroutines concorrentes para o mesmo lote. Falhas de banco
// aqui são apenas logadas — nunca propagadas.
func syncBukrs(db *sql.DB, companyID, bukrs, requestID, clientID, clientSecretEnc, baseURL string, chaves []string) {
	var runID string
	err := db.QueryRow(`
		INSERT INTO sap_sync_runs (company_id, bukrs, request_id, chaves_enviadas, status)
		VALUES ($1, $2, $3, $4, 'em_andamento')
		ON CONFLICT (company_id, bukrs, request_id) DO NOTHING
		RETURNING id
	`, companyID, bukrs, requestID, pq.Array(chaves)).Scan(&runID)
	if err == sql.ErrNoRows {
		log.Printf("[SAP Sync] Execução já existente para company_id=%s bukrs=%s request_id=%s — ignorando disparo repetido", companyID, bukrs, requestID)
		return
	}
	if err != nil {
		log.Printf("[SAP Sync] Erro ao registrar execução para company_id=%s bukrs=%s: %v", companyID, bukrs, err)
		return
	}
	log.Printf("[SAP Sync] Disparando sincronização para company_id=%s bukrs=%s (%d chaves)", companyID, bukrs, len(chaves))

	clientSecret, decErr := crypto.DecryptField(clientSecretEnc)
	if decErr != nil {
		log.Printf("[SAP Sync] Erro ao decriptar client_secret para company_id=%s bukrs=%s: %v", companyID, bukrs, decErr)
		finishRun(db, runID, "falha_credencial", "client_secret não pôde ser decriptado", 0, 0, 0, 0)
		maybeAlertOnFailure(db, companyID, bukrs, "falha_credencial", "client_secret não pôde ser decriptado")
		return
	}

	result := ProcessSAPSync(db, companyID, bukrs, clientID, clientSecret, baseURL, chaves)
	log.Printf("[SAP Sync] Execução run_id=%s finalizada: status=%s detail=%s", runID, result.Status, result.Detail)
	finishRun(db, runID, result.Status, result.Detail, result.ChavesPagoTotal, result.ChavesPagoParcial, result.ChavesEmAberto, result.ChavesNaoLocalizado)
	maybeAlertOnFailure(db, companyID, bukrs, result.Status, result.Detail)
}

// finishRun grava o status final (concluido | falha | falha_credencial) de
// uma execução de sap_sync_runs, junto das contagens por paymentStatus e do
// detalhe de erro (Story 2.4).
func finishRun(db *sql.DB, runID, status, detail string, pagoTotal, pagoParcial, emAberto, naoLocalizado int) {
	if _, err := db.Exec(`
		UPDATE sap_sync_runs SET status = $2, concluido_em = NOW(), erro_detalhe = $3,
			chaves_pago_total = $4, chaves_pago_parcial = $5, chaves_em_aberto = $6, chaves_nao_localizado = $7
		WHERE id = $1
	`, runID, status, detail, pagoTotal, pagoParcial, emAberto, naoLocalizado); err != nil {
		log.Printf("[SAP Sync] Erro ao finalizar execução run_id=%s (status=%s, detail=%s): %v", runID, status, detail, err)
	}
}
