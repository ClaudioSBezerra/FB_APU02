package services

import (
	"database/sql"
	"testing"
)

// insertTestAdminUser cria um usuário com role='admin' vinculado ao ambiente
// da empresa de teste (via companies -> enterprise_groups -> environment_id),
// para exercitar findCompanyAdminEmails com dados reais em vez de só o caso
// vazio. Remove o usuário criado ao final do teste.
func insertTestAdminUser(t *testing.T, db *sql.DB, companyID, email string) {
	t.Helper()
	var envID string
	if err := db.QueryRow(`
		SELECT eg.environment_id FROM companies c
		JOIN enterprise_groups eg ON c.group_id = eg.id
		WHERE c.id = $1
	`, companyID).Scan(&envID); err != nil {
		t.Fatalf("falha ao obter environment_id da empresa de teste: %v", err)
	}
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (email, password_hash, full_name) VALUES ($1, 'x', 'Admin Teste') RETURNING id
	`, email).Scan(&userID); err != nil {
		t.Fatalf("falha ao criar usuário de teste: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_environments (user_id, environment_id, role) VALUES ($1, $2, 'admin')
	`, userID, envID); err != nil {
		t.Fatalf("falha ao vincular usuário admin ao ambiente de teste: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
}

// insertSyncRun insere uma linha de sap_sync_runs já finalizada, para montar
// o histórico necessário aos testes de contagem de falhas consecutivas.
func insertSyncRun(t *testing.T, db *sql.DB, companyID, bukrs, status string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO sap_sync_runs (company_id, bukrs, chaves_enviadas, status, concluido_em)
		VALUES ($1, $2, '{}', $3, NOW())
	`, companyID, bukrs, status); err != nil {
		t.Fatalf("falha ao inserir sap_sync_runs de teste: %v", err)
	}
}

func TestCountConsecutiveFailures_TerceiraFalhaSeguida(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")

	got, err := countConsecutiveFailures(db, companyID, "1000")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != 3 {
		t.Errorf("esperava 3 falhas consecutivas, obteve %d", got)
	}
}

func TestCountConsecutiveFailures_InterrompidaPorSucesso(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "concluido") // quebra a sequência
	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")

	got, err := countConsecutiveFailures(db, companyID, "1000")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != 2 {
		t.Errorf("esperava 2 falhas consecutivas (interrompidas pelo 'concluido' mais antigo), obteve %d", got)
	}
}

func TestCountConsecutiveFailures_BukrsDiferentesNaoSeMisturam(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")
	// BUKRS 2000 da mesma empresa nunca falhou — não deve contar junto.
	insertSyncRun(t, db, companyID, "2000", "concluido")

	got1000, err := countConsecutiveFailures(db, companyID, "1000")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got1000 != 3 {
		t.Errorf("esperava 3 falhas consecutivas para bukrs=1000, obteve %d", got1000)
	}

	got2000, err := countConsecutiveFailures(db, companyID, "2000")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got2000 != 0 {
		t.Errorf("esperava 0 falhas consecutivas para bukrs=2000 (só teve sucesso), obteve %d", got2000)
	}
}

// withSpiedFailureAlert substitui sendFailureAlertFunc por um espião durante o
// teste e restaura o original ao final — permite confirmar que o disparo real
// aconteceu (ou não), sem exigir SMTP configurado.
func withSpiedFailureAlert(t *testing.T) *int {
	t.Helper()
	calls := 0
	original := sendFailureAlertFunc
	sendFailureAlertFunc = func(recipients []string, companyName, bukrs string, consecutiveFailures int, lastError string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { sendFailureAlertFunc = original })
	return &calls
}

func withSpiedCredentialAlert(t *testing.T) *int {
	t.Helper()
	calls := 0
	original := sendCredentialAlertFunc
	sendCredentialAlertFunc = func(recipients []string, companyName, bukrs, lastError string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { sendCredentialAlertFunc = original })
	return &calls
}

func TestAlertIfConsecutiveFailuresReached_NaoAlertaAntesDaTerceira(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertTestAdminUser(t, db, companyID, "admin-2-4-nao-antes@teste.com")
	calls := withSpiedFailureAlert(t)

	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")

	alertIfConsecutiveFailuresReached(db, companyID, "1000", "erro de teste")

	if *calls != 0 {
		t.Errorf("esperava 0 disparos de alerta com 2 falhas consecutivas, obteve %d", *calls)
	}
}

func TestAlertIfConsecutiveFailuresReached_DisparaExatamenteNaTerceira(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertTestAdminUser(t, db, companyID, "admin-2-4-terceira@teste.com")
	calls := withSpiedFailureAlert(t)

	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")

	alertIfConsecutiveFailuresReached(db, companyID, "1000", "erro de teste")

	if *calls != 1 {
		t.Errorf("esperava exatamente 1 disparo de alerta na 3ª falha consecutiva, obteve %d", *calls)
	}
}

func TestAlertIfConsecutiveFailuresReached_NaoRealertaNaQuartaFalha(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertTestAdminUser(t, db, companyID, "admin-2-4-quarta@teste.com")
	calls := withSpiedFailureAlert(t)

	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha")
	insertSyncRun(t, db, companyID, "1000", "falha") // 4ª consecutiva

	got, err := countConsecutiveFailures(db, companyID, "1000")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != 4 {
		t.Fatalf("esperava 4 falhas consecutivas, obteve %d", got)
	}
	// alertIfConsecutiveFailuresReached só age quando a contagem é EXATAMENTE 3
	// (ver constante consecutiveFailuresToAlert) — com 4, não deve re-alertar.
	alertIfConsecutiveFailuresReached(db, companyID, "1000", "erro de teste")

	if *calls != 0 {
		t.Errorf("esperava 0 disparos de alerta na 4ª falha consecutiva (já alertado na 3ª), obteve %d", *calls)
	}
}

func TestAlertCredentialFailure_DisparaApenasNaTransicao(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertTestAdminUser(t, db, companyID, "admin-2-4-credencial@teste.com")
	calls := withSpiedCredentialAlert(t)

	// 1ª execução com falha_credencial: dispara (transição).
	insertSyncRun(t, db, companyID, "1000", "falha_credencial")
	alertCredentialFailure(db, companyID, "1000", "erro de teste")
	if *calls != 1 {
		t.Fatalf("esperava 1 disparo na 1ª falha_credencial, obteve %d", *calls)
	}

	// 2ª execução seguida com falha_credencial (mesma credencial ainda quebrada,
	// próxima apuração): não deve reenviar o mesmo alerta.
	insertSyncRun(t, db, companyID, "1000", "falha_credencial")
	alertCredentialFailure(db, companyID, "1000", "erro de teste")
	if *calls != 1 {
		t.Errorf("esperava continuar em 1 disparo na 2ª falha_credencial consecutiva (sem re-alertar), obteve %d", *calls)
	}
}

func TestAlertCredentialFailure_RealertaAposSucessoIntermediario(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertTestAdminUser(t, db, companyID, "admin-2-4-credencial-recupera@teste.com")
	calls := withSpiedCredentialAlert(t)

	insertSyncRun(t, db, companyID, "1000", "falha_credencial")
	alertCredentialFailure(db, companyID, "1000", "erro de teste")

	// Credencial corrigida, sincronização volta a funcionar...
	insertSyncRun(t, db, companyID, "1000", "concluido")

	// ...e depois quebra de novo: é uma NOVA transição, deve alertar de novo.
	insertSyncRun(t, db, companyID, "1000", "falha_credencial")
	alertCredentialFailure(db, companyID, "1000", "erro de teste")

	if *calls != 2 {
		t.Errorf("esperava 2 disparos (uma transição por streak de falha_credencial), obteve %d", *calls)
	}
}

func TestFindCompanyAdminEmails_RetornaVazioSemAdmins(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	emails, companyName, err := findCompanyAdminEmails(db, companyID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if companyName == "" {
		t.Error("esperava companyName preenchido")
	}
	if len(emails) != 0 {
		t.Errorf("esperava 0 e-mails (empresa de teste sem usuários associados), obteve %d", len(emails))
	}
}

func TestFindCompanyAdminEmails_RetornaEmailsQuandoAdminsExistem(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertTestAdminUser(t, db, companyID, "admin-2-4-busca@teste.com")

	emails, _, err := findCompanyAdminEmails(db, companyID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(emails) != 1 || emails[0] != "admin-2-4-busca@teste.com" {
		t.Errorf("esperava [admin-2-4-busca@teste.com], obteve %v", emails)
	}
}

func TestMaybeAlertOnFailure_StatusConcluidoNaoFazNada(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	failureCalls := withSpiedFailureAlert(t)
	credentialCalls := withSpiedCredentialAlert(t)

	maybeAlertOnFailure(db, companyID, "1000", "concluido", "")
	maybeAlertOnFailure(db, companyID, "1000", "em_andamento", "")

	if *failureCalls != 0 || *credentialCalls != 0 {
		t.Errorf("esperava 0 disparos para status concluido/em_andamento, obteve falha=%d credencial=%d", *failureCalls, *credentialCalls)
	}
}
