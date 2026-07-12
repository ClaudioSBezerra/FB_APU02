package services

import (
	"database/sql"
	"log"
)

// consecutiveFailuresToAlert é o número de falhas transitórias seguidas
// (status='falha') que dispara o alerta por e-mail (AC #3). O alerta dispara
// somente na transição exata para esse número — não a cada falha subsequente
// — para não gerar ruído repetido a cada nova falha (4ª, 5ª...).
const consecutiveFailuresToAlert = 3

// Indireção para permitir espiar o disparo real do alerta em teste, sem
// depender de SMTP configurado — mesmo padrão de override usado em
// backoffSleep/pacerSleepFunc no restante do pacote services.
var sendCredentialAlertFunc = SendSAPSyncCredentialAlert
var sendFailureAlertFunc = SendSAPSyncFailureAlert

// maybeAlertOnFailure dispara alertas por e-mail conforme o status final de
// uma execução de sap_sync_runs (Story 2.4, AC #3/#4). Chamada depois de
// finishRun já ter persistido o status. Falha ao enviar e-mail é isolada e
// apenas logada — nunca propagada, mesmo princípio de isolamento de toda a
// Epic 2.
func maybeAlertOnFailure(db *sql.DB, companyID, bukrs, status, detail string) {
	switch status {
	case "falha_credencial":
		alertCredentialFailure(db, companyID, bukrs, detail)
	case "falha":
		alertIfConsecutiveFailuresReached(db, companyID, bukrs, detail)
	}
}

// alertCredentialFailure dispara o alerta imediato de credencial apenas na
// transição para falha_credencial (mesma semântica anti-ruído do alerta de
// falhas consecutivas) — sem isso, toda apuração concluída para uma empresa
// com credencial quebrada reenviaria o mesmo e-mail (Review Finding).
func alertCredentialFailure(db *sql.DB, companyID, bukrs, detail string) {
	consecutive, err := countConsecutiveByStatus(db, companyID, bukrs, "falha_credencial", 2)
	if err != nil {
		log.Printf("[SAP Sync Alert] Erro ao consultar histórico de credencial para company_id=%s bukrs=%s: %v", companyID, bukrs, err)
		return
	}
	if consecutive != 1 {
		return
	}

	recipients, companyName, err := findCompanyAdminEmails(db, companyID)
	if err != nil {
		log.Printf("[SAP Sync Alert] Erro ao buscar destinatários para company_id=%s: %v", companyID, err)
		return
	}
	if len(recipients) == 0 {
		log.Printf("[SAP Sync Alert] Nenhum admin encontrado para company_id=%s — alerta de credencial não enviado", companyID)
		return
	}
	if err := sendCredentialAlertFunc(recipients, companyName, bukrs, detail); err != nil {
		log.Printf("[SAP Sync Alert] Erro ao enviar alerta de credencial para company_id=%s bukrs=%s: %v", companyID, bukrs, err)
	}
}

// countConsecutiveByStatus conta quantas execuções seguidas (a partir da mais
// recente) têm o status dado para o mesmo (company_id, bukrs), parando no
// primeiro status diferente. limit deve ser 1 a mais que o maior valor de
// interesse para o chamador conseguir distinguir "exatamente N" de "mais que N".
func countConsecutiveByStatus(db *sql.DB, companyID, bukrs, status string, limit int) (int, error) {
	rows, err := db.Query(`
		SELECT status FROM sap_sync_runs
		WHERE company_id = $1 AND bukrs = $2
		ORDER BY iniciado_em DESC
		LIMIT $3
	`, companyID, bukrs, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	consecutive := 0
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			return 0, err
		}
		if st != status {
			break
		}
		consecutive++
	}
	return consecutive, rows.Err()
}

// countConsecutiveFailures conta falhas transitórias consecutivas — extraída
// para ser testável isoladamente, sem depender de configuração de SMTP.
func countConsecutiveFailures(db *sql.DB, companyID, bukrs string) (int, error) {
	return countConsecutiveByStatus(db, companyID, bukrs, "falha", consecutiveFailuresToAlert+1)
}

// alertIfConsecutiveFailuresReached só dispara o alerta quando a contagem de
// falhas consecutivas for exatamente consecutiveFailuresToAlert.
func alertIfConsecutiveFailuresReached(db *sql.DB, companyID, bukrs, detail string) {
	consecutive, err := countConsecutiveFailures(db, companyID, bukrs)
	if err != nil {
		log.Printf("[SAP Sync Alert] Erro ao consultar histórico para company_id=%s bukrs=%s: %v", companyID, bukrs, err)
		return
	}

	if consecutive != consecutiveFailuresToAlert {
		return
	}

	recipients, companyName, err := findCompanyAdminEmails(db, companyID)
	if err != nil {
		log.Printf("[SAP Sync Alert] Erro ao buscar destinatários para company_id=%s: %v", companyID, err)
		return
	}
	if len(recipients) == 0 {
		log.Printf("[SAP Sync Alert] Nenhum admin encontrado para company_id=%s — alerta de falhas consecutivas não enviado", companyID)
		return
	}
	if err := sendFailureAlertFunc(recipients, companyName, bukrs, consecutive, detail); err != nil {
		log.Printf("[SAP Sync Alert] Erro ao enviar alerta de falhas consecutivas para company_id=%s bukrs=%s: %v", companyID, bukrs, err)
	}
}

// findCompanyAdminEmails busca o nome da empresa e os e-mails de todos os
// usuários com role='admin' no ambiente do grupo dessa empresa — sem
// precedente de código no projeto para "descobrir destinatários de alerta",
// adaptado do EXISTS de autorização em backend/handlers/admin.go:131-140.
func findCompanyAdminEmails(db *sql.DB, companyID string) (emails []string, companyName string, err error) {
	if err = db.QueryRow(`SELECT name FROM companies WHERE id = $1`, companyID).Scan(&companyName); err != nil {
		return nil, "", err
	}

	rows, err := db.Query(`
		SELECT DISTINCT u.email
		FROM user_environments ue
		JOIN enterprise_groups eg ON ue.environment_id = eg.environment_id
		JOIN companies c ON c.group_id = eg.id
		JOIN users u ON u.id = ue.user_id
		WHERE c.id = $1 AND ue.role = 'admin'
	`, companyID)
	if err != nil {
		return nil, companyName, err
	}
	defer rows.Close()

	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, companyName, err
		}
		emails = append(emails, email)
	}
	return emails, companyName, rows.Err()
}
