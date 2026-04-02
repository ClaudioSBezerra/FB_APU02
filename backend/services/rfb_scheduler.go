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
	// Conta TODAS as tentativas do dia (inclusive erros) para não retentar
	// após 429 (rate limit) ou 400 — a RFB conta a tentativa independente do resultado.
	var todayCount int
	db.QueryRow(`
		SELECT COUNT(*) FROM rfb_requests
		WHERE company_id = $1
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
