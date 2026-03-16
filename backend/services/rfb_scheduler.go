package services

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

// SolicitarApuracaoParaEmpresa executa uma solicitação de apuração CBS para a empresa.
// Usada pelo scheduler (limite: 1/dia, preserva 1 slot manual) e pelo handler HTTP.
func SolicitarApuracaoParaEmpresa(db *sql.DB, companyID string) error {
	log.Printf("[RFB Scheduler] SolicitarApuracaoParaEmpresa: companyID=%s", companyID)

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

	// 2. Verificar slot automático (máx 1/dia, deixa 1 para uso manual)
	// Usa fuso de Brasília para que "hoje" seja correto independente do clock do servidor
	var todayCount int
	db.QueryRow(`
		SELECT COUNT(*) FROM rfb_requests
		WHERE company_id = $1
		  AND status != 'error'
		  AND created_at >= CURRENT_DATE AT TIME ZONE 'America/Sao_Paulo'
	`, companyID).Scan(&todayCount)
	if todayCount >= 1 {
		return fmt.Errorf("slot automático já utilizado hoje para company_id=%s (count=%d)", companyID, todayCount)
	}

	// 3. Extrair CNPJ base (8 dígitos)
	cnpjBase := cnpjMatriz
	if len(cnpjBase) > 8 {
		cnpjBase = cnpjBase[:8]
	}

	// 4. Obter token OAuth2
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

	// 5. Solicitar apuração CBS
	tiquete, err := rfbClient.SolicitarApuracao(token, cnpjBase)
	if err != nil {
		db.Exec(`
			INSERT INTO rfb_requests (company_id, cnpj_base, status, error_code, error_message)
			VALUES ($1, $2, 'error', 'REQUEST_ERROR', $3)
		`, companyID, cnpjBase, err.Error())
		return fmt.Errorf("REQUEST_ERROR: %w", err)
	}

	// 6. Persistir registro da solicitação
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

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now().In(loc)
		currentHHMM := now.Format("15:04")

		log.Printf("[RFB Scheduler] Tick %s — verificando empresas agendadas...", now.Format("2006-01-02 15:04"))

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
			log.Printf("[RFB Scheduler] Nenhuma empresa agendada para %s", currentHHMM)
			continue
		}

		log.Printf("[RFB Scheduler] %d empresa(s) agendada(s) para %s — disparando...", len(companies), currentHHMM)
		for _, companyID := range companies {
			cid := companyID
			go func() {
				log.Printf("[RFB Scheduler] Iniciando solicitação automática para companyID=%s", cid)
				if runErr := SolicitarApuracaoParaEmpresa(db, cid); runErr != nil {
					log.Printf("[RFB Scheduler] Erro ao executar agendamento para %s: %v", cid, runErr)
				}
			}()
		}
	}
}
