package services

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// extractRetryUntil procura um segmento "retry_until=RFC3339" numa mensagem de erro
// delimitada por "|" (formato usado tanto pelo 429 de apuração/créditos quanto pelo
// de token, ver rfb.go) e retorna o timestamp se ainda estiver no futuro.
func extractRetryUntil(msg string) (time.Time, bool) {
	for _, part := range strings.Split(msg, "|") {
		if strings.HasPrefix(part, "retry_until=") {
			t, err := time.Parse(time.RFC3339, strings.TrimPrefix(part, "retry_until="))
			if err == nil && time.Now().Before(t) {
				return t, true
			}
			return time.Time{}, false
		}
	}
	return time.Time{}, false
}

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
	if t, ok := extractRetryUntil(msg.String); ok {
		SetRateLimitUntil(cnpjBase, t)
		brtLoc, _ := time.LoadLocation("America/Sao_Paulo")
		log.Printf("[RFB] Rate limit restaurado do banco para CNPJ %s — bloqueado até %s BRT",
			cnpjBase, t.In(brtLoc).Format("02/01 15:04"))
	}
}

// applyTokenRateLimit registra localmente (e loga) o bloqueio de rate-limit vindo do
// endpoint de token — antes desse fix, um 429 de GetToken virava só um TOKEN_ERROR
// genérico, sem impedir a próxima tentativa (manual ou agendada) de bater no mesmo
// bloqueio imediatamente. Sem-op se o erro não tiver o prefixo RATE_LIMIT_429.
func applyTokenRateLimit(cnpjBase string, err error) {
	msg := err.Error()
	if !strings.HasPrefix(msg, "RATE_LIMIT_429|") {
		return
	}
	if t, ok := extractRetryUntil(msg); ok {
		SetRateLimitUntil(cnpjBase, t)
	}
}

// Limites diários de solicitações por empresa.
//
// Débitos manuais dependem da VERSÃO da API RFB usada na solicitação: a v1 documentava 2
// chamadas/dia; a v2 documenta 4. Subir para 4 antes de a empresa realmente usar a v2
// causaria 429 reais da RFB, então o teto efetivo vem de LimiteDebitosDiaRFBPara (versão
// resolvida por ResolveRFBAPIVersion) — nunca de uma constante única.
// O agendamento usa só 1 para sempre sobrar um slot para a chamada manual. Créditos ficam
// em 2/dia (sem limite RFB conhecido; aplicado pelo handler).
const (
	LimiteDebitosDiaAgendamento = 1
	LimiteDebitosDiaRFBv1       = 2
	LimiteDebitosDiaRFBv2       = 4
	LimiteCreditosDiaRFB        = 2
)

// LimiteDebitosDiaRFBPara devolve o teto diário de débitos manuais para o ambiente, segundo
// a versão da API resolvida (v1 = 2, v2 = 4).
func LimiteDebitosDiaRFBPara(ambiente string) int {
	if ResolveRFBAPIVersion(ambiente) == RFBAPIVersaoV2 {
		return LimiteDebitosDiaRFBv2
	}
	return LimiteDebitosDiaRFBv1
}

// LimiteDebitosDiaRFBEmpresa carrega o ambiente da credencial ativa da empresa e devolve o
// teto diário efetivo. Sem credencial ativa (ou erro de leitura) usa o ambiente "producao".
func LimiteDebitosDiaRFBEmpresa(db *sql.DB, companyID string) int {
	ambiente := "producao"
	var amb sql.NullString
	if err := db.QueryRow(`
		SELECT COALESCE(ambiente, 'producao') FROM rfb_credentials WHERE company_id = $1 AND ativo = true
	`, companyID).Scan(&amb); err == nil && amb.Valid && amb.String != "" {
		ambiente = amb.String
	}
	return LimiteDebitosDiaRFBPara(ambiente)
}

// SolicitarApuracaoParaEmpresa executa uma solicitação de apuração CBS para a empresa.
// limiteDiario é o total de solicitações de débito já feitas hoje (scheduler + manual,
// incluindo erros) a partir do qual a chamada é recusada com erro DAILY_LIMIT, sem
// chamar a RFB: o scheduler passa LimiteDebitosDiaAgendamento, os handlers manuais
// passam LimiteDebitosDiaRFBEmpresa (teto por versão da API).
func SolicitarApuracaoParaEmpresa(db *sql.DB, companyID string, limiteDiario int) error {
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

	// 4. Verificar limite diário de débitos (ver LimiteDebitosDia*; o teto vem do chamador)
	// status != 'pending' exclui a própria linha que o Ressolicitar está reenviando agora
	// (claim atômico deixa a linha em 'pending' sem alterar created_at) — sem isso, uma linha
	// sendo re-enviada hoje sempre se autocontava e bloqueava seu próprio reenvio (achado de revisão).
	var todayCount int
	db.QueryRow(`
		SELECT COUNT(*) FROM rfb_requests
		WHERE company_id = $1
		  AND tipo = 'debito'
		  AND status != 'pending'
		  AND created_at >= CURRENT_DATE AT TIME ZONE 'America/Sao_Paulo'
	`, companyID).Scan(&todayCount)
	if todayCount >= limiteDiario {
		return fmt.Errorf("DAILY_LIMIT: %d de %d solicitação(ões) de débito já feitas hoje para company_id=%s", todayCount, limiteDiario, companyID)
	}

	// 5. Obter token OAuth2
	rfbClient := NewRFBClient()
	rfbClient.SetAmbiente(ambiente)
	apiVersao := ResolveRFBAPIVersion(ambiente)
	rfbClient.SetAPIVersao(apiVersao)
	token, err := rfbClient.GetToken(clientID, clientSecret)
	if err != nil {
		applyTokenRateLimit(cnpjBase, err)
		errorCode := "TOKEN_ERROR"
		if strings.HasPrefix(err.Error(), "RATE_LIMIT_429|") {
			errorCode = "RATE_LIMIT"
		}
		db.Exec(`
			INSERT INTO rfb_requests (company_id, cnpj_base, status, error_code, error_message, ambiente, api_versao)
			VALUES ($1, $2, 'error', $3, $4, $5, $6)
		`, companyID, cnpjBase, errorCode, err.Error(), ambiente, apiVersao)
		return fmt.Errorf("%s: %w", errorCode, err)
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
			INSERT INTO rfb_requests (company_id, cnpj_base, status, error_code, error_message, ambiente, api_versao)
			VALUES ($1, $2, 'error', $3, $4, $5, $6)
		`, companyID, cnpjBase, errorCode, errMsg, ambiente, apiVersao)
		return fmt.Errorf("%s: %w", errorCode, err)
	}

	// 7. Persistir registro da solicitação
	var requestID string
	err = db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, ambiente, api_versao)
		VALUES ($1, $2, $3, 'requested', $4, $5)
		RETURNING id
	`, companyID, cnpjBase, tiquete, ambiente, apiVersao).Scan(&requestID)
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
// Usada pelo handler HTTP manual (limite: 2/dia separados dos débitos, aplicado pelo handler).
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
	apiVersao := ResolveRFBAPIVersion(ambiente)
	rfbClient.SetAPIVersao(apiVersao)
	token, err := rfbClient.GetToken(clientID, clientSecret)
	if err != nil {
		applyTokenRateLimit(cnpjBase, err)
		errorCode := "TOKEN_ERROR"
		if strings.HasPrefix(err.Error(), "RATE_LIMIT_429|") {
			errorCode = "RATE_LIMIT"
		}
		db.Exec(`
			INSERT INTO rfb_requests (company_id, cnpj_base, status, tipo, error_code, error_message, ambiente, api_versao)
			VALUES ($1, $2, 'error', 'credito', $3, $4, $5, $6)
		`, companyID, cnpjBase, errorCode, err.Error(), ambiente, apiVersao)
		return fmt.Errorf("%s: %w", errorCode, err)
	}

	tiquete, err := rfbClient.SolicitarCredito(token, cnpjBase)
	if err != nil {
		errMsg := err.Error()
		errorCode := "REQUEST_ERROR"
		switch {
		case strings.Contains(errMsg, "no Route matched") || strings.Contains(errMsg, "404"):
			// Gateway 404: endpoint ainda não liberado pela RFB para este ambiente — condição
			// conhecida e temporária, não uma falha real. Persiste a linha (antes: "skip DB
			// record") para a UI poder exibi-la como Alerta em vez de silêncio total; sem risco
			// de acúmulo: a goroutine de SolicitarApuracaoParaEmpresa roda no máximo
			// (no máximo o teto diário de débitos por dia), e o reenvio manual de créditos também
			// é limitado a LimiteCreditosDiaRFB/dia.
			log.Printf("[RFB Creditos] Endpoint de créditos (API %s) indisponível no gateway (HTTP 404) — aguardando liberação pela RFB", apiVersao)
			errorCode = "ENDPOINT_INDISPONIVEL"
		case strings.HasPrefix(errMsg, "RATE_LIMIT_429|"):
			errorCode = "RATE_LIMIT"
		}
		db.Exec(`
			INSERT INTO rfb_requests (company_id, cnpj_base, status, tipo, error_code, error_message, ambiente, api_versao)
			VALUES ($1, $2, 'error', 'credito', $3, $4, $5, $6)
		`, companyID, cnpjBase, errorCode, errMsg, ambiente, apiVersao)
		if errorCode == "ENDPOINT_INDISPONIVEL" {
			return fmt.Errorf("endpoint não disponível: %w", err)
		}
		return fmt.Errorf("%s: %w", errorCode, err)
	}

	var requestID string
	err = db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, ambiente, api_versao)
		VALUES ($1, $2, $3, 'requested', 'credito', $4, $5)
		RETURNING id
	`, companyID, cnpjBase, tiquete, ambiente, apiVersao).Scan(&requestID)
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
				if runErr := SolicitarApuracaoParaEmpresa(db, cid, LimiteDebitosDiaAgendamento); runErr != nil {
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
		WHERE status IN ('pending', 'requested', 'webhook_received', 'downloading', 'reprocessing')
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
