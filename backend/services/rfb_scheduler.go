package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// restoreRateLimitFromDB recarrega o bloqueio de rate-limit do banco após restart do container.
// O error_message de registros RATE_LIMIT contém "retry_until=RFC3339|..." para persistência.
func restoreRateLimitFromDB(db *sql.DB, companyID, cnpjBase string) {
	var msg sql.NullString
	db.QueryRow(`
		SELECT error_message FROM rfb_requests
		WHERE company_id = $1 AND error_code = 'RATE_LIMIT'
		ORDER BY created_at DESC LIMIT 1
	`, companyID).Scan(&msg)
	if !msg.Valid {
		return
	}
	for _, part := range strings.Split(msg.String, "|") {
		if strings.HasPrefix(part, "retry_until=") {
			t, err := time.Parse(time.RFC3339, strings.TrimPrefix(part, "retry_until="))
			if err == nil && time.Now().Before(t) {
				SetRateLimitUntil(cnpjBase, t)
				brtLoc, _ := time.LoadLocation("America/Sao_Paulo")
				log.Printf("[RFB] Rate limit restaurado do banco para CNPJ %s — bloqueado até %s BRT",
					cnpjBase, t.In(brtLoc).Format("02/01 15:04"))
			}
			return
		}
	}
}

// SolicitarApuracaoParaEmpresa executa uma solicitação de apuração CBS para a empresa.
// Usada pelo scheduler (limite: 1/dia, preserva 1 slot manual) e pelo handler HTTP.
func SolicitarApuracaoParaEmpresa(db *sql.DB, companyID string) error {
	// 1. Carregar credenciais ativas
	var clientID, clientSecret, cnpjMatriz, ambiente string
	err := db.QueryRow(`
		SELECT client_id, client_secret, cnpj_matriz, COALESCE(ambiente, 'producao')
		FROM rfb_credentials
		WHERE company_id = $1 AND ativo = true
	`, companyID).Scan(&clientID, &clientSecret, &cnpjMatriz, &ambiente)
	if err == sql.ErrNoRows {
		return fmt.Errorf("credenciais RFB não encontradas para company_id=%s", companyID)
	}
	if err != nil {
		return fmt.Errorf("erro ao buscar credenciais: %w", err)
	}

	// 2. Extrair CNPJ base (8 dígitos)
	cnpjBase := cnpjMatriz
	if len(cnpjBase) > 8 {
		cnpjBase = cnpjBase[:8]
	}

	// 3. Verificar bloqueio de rate-limit da RFB (restaura do banco após restart)
	restoreRateLimitFromDB(db, companyID, cnpjBase)
	if blocked, until := IsRateLimited(cnpjBase); blocked {
		brtLoc, _ := time.LoadLocation("America/Sao_Paulo")
		return fmt.Errorf("RATE_LIMIT: RFB bloqueada por rate-limit para CNPJ %s — aguardar até %s BRT",
			cnpjBase, until.In(brtLoc).Format("02/01 15:04"))
	}

	// 4. Verificar slot do dia para débitos (max 1 automático, deixa 1 slot para manual)
	var todayCount int
	db.QueryRow(`
		SELECT COUNT(*) FROM rfb_requests
		WHERE company_id = $1
		  AND tipo = 'debito'
		  AND created_at >= CURRENT_DATE AT TIME ZONE 'America/Sao_Paulo'
	`, companyID).Scan(&todayCount)
	if todayCount >= 1 {
		return fmt.Errorf("slot automático já utilizado hoje para company_id=%s (count=%d)", companyID, todayCount)
	}

	// 5. Obter token OAuth2
	rfbClient := NewRFBClient()
	rfbClient.SetAmbiente(ambiente)
	token, err := rfbClient.GetToken(clientID, clientSecret)
	if err != nil {
		db.Exec(`
			INSERT INTO rfb_requests (company_id, cnpj_base, status, error_code, error_message)
			VALUES ($1, $2, 'error', 'TOKEN_ERROR', $3)
		`, companyID, cnpjBase, err.Error())
		return fmt.Errorf("TOKEN_ERROR: %w", err)
	}

	// 6. Solicitar apuração CBS
	tiquete, err := rfbClient.SolicitarApuracao(token, cnpjBase)
	if err != nil {
		errMsg := err.Error()
		errorCode := "REQUEST_ERROR"
		if strings.HasPrefix(errMsg, "RATE_LIMIT_429|") {
			errorCode = "RATE_LIMIT"
		}
		db.Exec(`
			INSERT INTO rfb_requests (company_id, cnpj_base, status, error_code, error_message)
			VALUES ($1, $2, 'error', $3, $4)
		`, companyID, cnpjBase, errorCode, errMsg)
		return fmt.Errorf("%s: %w", errorCode, err)
	}

	// 7. Persistir registro da solicitação
	var requestID string
	err = db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status)
		VALUES ($1, $2, $3, 'requested')
		RETURNING id
	`, companyID, cnpjBase, tiquete).Scan(&requestID)
	if err != nil {
		return fmt.Errorf("erro ao salvar solicitação: %w", err)
	}

	log.Printf("[RFB Scheduler] Solicitação criada: requestID=%s tiquete=%s companyID=%s",
		requestID, tiquete, companyID)

	// Solicitar créditos CBS em paralelo — falha não bloqueia o retorno de débitos
	go func() {
		if credErr := SolicitarCreditoParaEmpresa(db, companyID); credErr != nil {
			log.Printf("[RFB Scheduler] AVISO: falha ao solicitar créditos CBS para company_id=%s: %v", companyID, credErr)
		} else {
			log.Printf("[RFB Scheduler] Créditos CBS solicitados com sucesso para company_id=%s", companyID)
		}
	}()

	return nil
}

// SolicitarCreditoParaEmpresa executa uma solicitação de créditos CBS para a empresa.
// Usada pelo handler HTTP manual (limite: 2/dia separados dos débitos).
func SolicitarCreditoParaEmpresa(db *sql.DB, companyID string) error {
	var clientID, clientSecret, cnpjMatriz, ambiente string
	err := db.QueryRow(`
		SELECT client_id, client_secret, cnpj_matriz, COALESCE(ambiente, 'producao')
		FROM rfb_credentials
		WHERE company_id = $1 AND ativo = true
	`, companyID).Scan(&clientID, &clientSecret, &cnpjMatriz, &ambiente)
	if err == sql.ErrNoRows {
		return fmt.Errorf("credenciais RFB não encontradas para company_id=%s", companyID)
	}
	if err != nil {
		return fmt.Errorf("erro ao buscar credenciais: %w", err)
	}

	cnpjBase := cnpjMatriz
	if len(cnpjBase) > 8 {
		cnpjBase = cnpjBase[:8]
	}

	restoreRateLimitFromDB(db, companyID, cnpjBase)
	if blocked, until := IsRateLimited(cnpjBase); blocked {
		brtLoc, _ := time.LoadLocation("America/Sao_Paulo")
		return fmt.Errorf("RATE_LIMIT: RFB bloqueada para CNPJ %s — aguardar até %s BRT",
			cnpjBase, until.In(brtLoc).Format("02/01 15:04"))
	}

	rfbClient := NewRFBClient()
	rfbClient.SetAmbiente(ambiente)
	token, err := rfbClient.GetToken(clientID, clientSecret)
	if err != nil {
		db.Exec(`
			INSERT INTO rfb_requests (company_id, cnpj_base, status, tipo, error_code, error_message)
			VALUES ($1, $2, 'error', 'credito', 'TOKEN_ERROR', $3)
		`, companyID, cnpjBase, err.Error())
		return fmt.Errorf("TOKEN_ERROR: %w", err)
	}

	tiquete, err := rfbClient.SolicitarCredito(token, cnpjBase)
	if err != nil {
		errMsg := err.Error()
		// "no Route matched" = gateway 404: endpoint not yet live — skip DB record
		if strings.Contains(errMsg, "no Route matched") || strings.Contains(errMsg, "404") {
			log.Printf("[RFB Creditos] Endpoint /creditos-cbs/v1/ indisponível no gateway (HTTP 404) — aguardando liberação pela RFB")
			return fmt.Errorf("endpoint não disponível: %w", err)
		}
		errorCode := "REQUEST_ERROR"
		if strings.HasPrefix(errMsg, "RATE_LIMIT_429|") {
			errorCode = "RATE_LIMIT"
		}
		db.Exec(`
			INSERT INTO rfb_requests (company_id, cnpj_base, status, tipo, error_code, error_message)
			VALUES ($1, $2, 'error', 'credito', $3, $4)
		`, companyID, cnpjBase, errorCode, errMsg)
		return fmt.Errorf("%s: %w", errorCode, err)
	}

	var requestID string
	err = db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo)
		VALUES ($1, $2, $3, 'requested', 'credito')
		RETURNING id
	`, companyID, cnpjBase, tiquete).Scan(&requestID)
	if err != nil {
		return fmt.Errorf("erro ao salvar solicitação de créditos: %w", err)
	}

	log.Printf("[RFB Creditos] Solicitação criada: requestID=%s tiquete=%s companyID=%s",
		requestID, tiquete, companyID)
	return nil
}

// StartRFBScheduler inicia o loop de agendamento automático.
// Deve ser chamado como goroutine no startup. Usa dbFn para obter o DB após ele estar pronto.
func StartRFBScheduler(dbFn func() *sql.DB) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		log.Printf("[RFB Scheduler] Erro ao carregar timezone: %v — scheduler desativado", err)
		return
	}

	log.Println("[RFB Scheduler] Aguardando banco de dados...")
	var db *sql.DB
	for {
		db = dbFn()
		if db != nil {
			if pingErr := db.Ping(); pingErr == nil {
				break
			}
		}
		time.Sleep(5 * time.Second)
	}
	log.Println("[RFB Scheduler] Banco pronto. Scheduler RFB iniciado.")

	// Goroutine separada: aborta requests travadas > 5h, a cada 5 minutos
	stuckTicker := time.NewTicker(5 * time.Minute)
	go func() {
		defer stuckTicker.Stop()
		for range stuckTicker.C {
			AbortStuckRFBRequests(db)
		}
	}()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now().In(loc)
		currentHHMM := now.Format("15:04")

		rows, err := db.Query(`
			SELECT company_id FROM rfb_credentials
			WHERE ativo = true
			  AND agendamento_ativo = true
			  AND TO_CHAR(horario_agendamento, 'HH24:MI') = $1
		`, currentHHMM)
		if err != nil {
			log.Printf("[RFB Scheduler] Erro ao buscar empresas agendadas: %v", err)
			continue
		}

		var companies []string
		for rows.Next() {
			var cid string
			if scanErr := rows.Scan(&cid); scanErr == nil {
				companies = append(companies, cid)
			}
		}
		rows.Close()

		if len(companies) == 0 {
			continue
		}

		log.Printf("[RFB Scheduler] %s — %d empresa(s) agendada(s) — iniciando solicitações...", now.Format("2006-01-02 15:04"), len(companies))
		for _, companyID := range companies {
			cid := companyID
			go func() {
				if runErr := SolicitarApuracaoParaEmpresa(db, cid); runErr != nil {
					log.Printf("[RFB Scheduler] [ERRO] companyID=%s: %v", cid, runErr)
				} else {
					log.Printf("[RFB Scheduler] [OK] companyID=%s — solicitação concluída com sucesso", cid)
				}
			}()
		}
	}
}

// AbortStuckRFBRequests aborta solicitações presas em estados intermediários por mais de 5 horas.
func AbortStuckRFBRequests(db *sql.DB) {
	res, err := db.Exec(`
		UPDATE rfb_requests
		SET status        = 'error',
		    error_code    = 'TIMEOUT',
		    error_message = 'Solicitação abortada automaticamente: sem resposta por mais de 5 horas',
		    updated_at    = CURRENT_TIMESTAMP
		WHERE status IN ('requested', 'webhook_received', 'downloading', 'reprocessing')
		  AND updated_at < NOW() - INTERVAL '5 hours'
	`)
	if err != nil {
		log.Printf("[RFB Scheduler] Erro ao abortar requests travadas: %v", err)
		return
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		log.Printf("[RFB Scheduler] Abortadas %d solicitação(ões) travadas há mais de 5h", rows)
	}
}
