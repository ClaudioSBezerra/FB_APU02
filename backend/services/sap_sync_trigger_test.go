package services

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"fb_apu02/crypto"

	"github.com/lib/pq"
)

// TestMain remove as esperas reais de retry/backoff para toda a suíte deste
// pacote e sobe a taxa padrão de rate-limit para não pacear os testes de
// integração — os testes exercitam a lógica real (Story 2.2), mas não
// precisam esperar segundos de verdade para isso. O pacing em si (pacerSleepFunc)
// permanece real, verificado isoladamente por TestCallPacer_PaceiaChamadasSucessivas
// (que constrói seu próprio callPacer, sem depender de SAP_SYNC_RATE_LIMIT_PER_MIN).
func TestMain(m *testing.M) {
	backoffSleep = func(time.Duration) {}
	os.Setenv("SAP_SYNC_RATE_LIMIT_PER_MIN", "6000000")
	os.Exit(m.Run())
}

// insertSAPCredential cria uma linha sap_credentials válida (client_secret
// criptografado de verdade) para os testes que exercitam o fluxo completo de
// TriggerSAPSync até a chamada ao SAP (Story 2.2).
func insertSAPCredential(t *testing.T, db *sql.DB, companyID string, bukrsList []string, baseURL string) {
	t.Helper()
	encSecret, err := crypto.EncryptField("dummy-secret-de-teste")
	if err != nil {
		t.Fatalf("falha ao criptografar client_secret de teste: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sap_credentials (company_id, client_id, client_secret, base_url, bukrs_list, ativo)
		VALUES ($1, 'cid-teste', $2, $3, $4, true)
	`, companyID, encSecret, baseURL, pq.Array(bukrsList)); err != nil {
		t.Fatalf("falha ao criar sap_credentials de teste: %v", err)
	}
}

// openTestDB conecta ao Postgres real de desenvolvimento (mesmo padrão usado
// pelo backend em produção — sem ORM, raw SQL). Sem harness de mock de banco
// no projeto (ver deferred-work.md das Stories 1.1/1.2); testa direto contra
// Postgres, pulando quando indisponível.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/fiscal_db?sslmode=disable"
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skipf("DB indisponível para teste de integração: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("DB indisponível para teste de integração: %v", err)
	}
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'sap_sync_runs')`).Scan(&exists); err != nil || !exists {
		t.Skip("tabela sap_sync_runs não existe — rode a migration 116 antes deste teste")
	}
	return db
}

// setupTestCompany cria a cadeia environment -> enterprise_group -> company
// necessária para satisfazer as FKs de sap_credentials/rfb_requests/rfb_creditos,
// retornando o companyID e uma função de limpeza.
func setupTestCompany(t *testing.T, db *sql.DB) (companyID string, cleanup func()) {
	t.Helper()

	var envID, groupID string
	if err := db.QueryRow(`INSERT INTO environments (name) VALUES ('teste-sap-sync-2.1') RETURNING id`).Scan(&envID); err != nil {
		t.Fatalf("falha ao criar environment de teste: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO enterprise_groups (environment_id, name) VALUES ($1, 'grupo-teste-sap-sync-2.1') RETURNING id`, envID).Scan(&groupID); err != nil {
		t.Fatalf("falha ao criar enterprise_group de teste: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO companies (group_id, name) VALUES ($1, 'empresa-teste-sap-sync-2.1') RETURNING id`, groupID).Scan(&companyID); err != nil {
		t.Fatalf("falha ao criar company de teste: %v", err)
	}

	cleanup = func() {
		db.Exec(`DELETE FROM environments WHERE id = $1`, envID) // cascade limpa enterprise_groups/companies/sap_credentials/rfb_requests/rfb_creditos/sap_sync_runs
	}
	return companyID, cleanup
}

func insertTestRFBRequest(t *testing.T, db *sql.DB, companyID string) string {
	t.Helper()
	var requestID string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, status) VALUES ($1, '12345678', 'completed') RETURNING id
	`, companyID).Scan(&requestID); err != nil {
		t.Fatalf("falha ao criar rfb_requests de teste: %v", err)
	}
	return requestID
}

func insertTestCredito(t *testing.T, db *sql.DB, requestID, companyID, chaveDFe string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO rfb_creditos (request_id, company_id, chave_dfe) VALUES ($1, $2, $3)
	`, requestID, companyID, chaveDFe); err != nil {
		t.Fatalf("falha ao criar rfb_creditos de teste: %v", err)
	}
}

func countSyncRuns(t *testing.T, db *sql.DB, companyID, requestID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sap_sync_runs WHERE company_id = $1 AND request_id = $2`, companyID, requestID).Scan(&count); err != nil {
		t.Fatalf("falha ao contar sap_sync_runs: %v", err)
	}
	return count
}

func TestTriggerSAPSync_SemCredencialSAP_NaoDispara(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	requestID := insertTestRFBRequest(t, db, companyID)
	insertTestCredito(t, db, requestID, companyID, "chave-teste-001")

	// Sem linha em sap_credentials — deve encerrar sem nenhum registro (AC #2, #6)
	TriggerSAPSync(db, requestID)

	if got := countSyncRuns(t, db, companyID, requestID); got != 0 {
		t.Errorf("esperava 0 execuções sap_sync_runs sem sap_credentials, obteve %d", got)
	}
}

func TestTriggerSAPSync_SemCreditosNovos_NaoDispara(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	insertSAPCredential(t, db, companyID, []string{"1000"}, "http://127.0.0.1:1")

	requestID := insertTestRFBRequest(t, db, companyID)
	// Nenhum crédito inserido para este requestID — deve encerrar sem nenhum registro (AC #2)
	TriggerSAPSync(db, requestID)

	if got := countSyncRuns(t, db, companyID, requestID); got != 0 {
		t.Errorf("esperava 0 execuções sap_sync_runs sem créditos novos, obteve %d", got)
	}
}

func TestTriggerSAPSync_CaminhoFeliz_CriaUmaExecucaoPorBukrs(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	fakeSAP := newFakeSAPServer(t)
	insertSAPCredential(t, db, companyID, []string{"1000", "2000"}, fakeSAP.URL)

	requestID := insertTestRFBRequest(t, db, companyID)
	insertTestCredito(t, db, requestID, companyID, "35240612345678000190550010000000011000000010")

	TriggerSAPSync(db, requestID)

	if got := countSyncRuns(t, db, companyID, requestID); got != 2 {
		t.Fatalf("esperava 1 execução por BUKRS configurado (2 BUKRS), obteve %d", got)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM sap_sync_runs WHERE company_id = $1 AND bukrs = '1000'`, companyID).Scan(&status); err != nil {
		t.Fatalf("falha ao ler status da execução: %v", err)
	}
	if status != "concluido" {
		t.Errorf("esperava status 'concluido' (fake SAP server respondeu 200 com sucesso), obteve %q", status)
	}
}

func TestTriggerSAPSync_Idempotente_NaoDuplicaAoReprocessarMesmoRequest(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	insertSAPCredential(t, db, companyID, []string{"1000"}, "http://127.0.0.1:1")

	requestID := insertTestRFBRequest(t, db, companyID)
	insertTestCredito(t, db, requestID, companyID, "chave-teste-003")

	// Simula reentrega do webhook: dispara duas vezes para o mesmo requestID (AC #5)
	TriggerSAPSync(db, requestID)
	TriggerSAPSync(db, requestID)

	if got := countSyncRuns(t, db, companyID, requestID); got != 1 {
		t.Errorf("esperava exatamente 1 execução após 2 disparos para o mesmo requestID (idempotência), obteve %d", got)
	}
}

// TestSyncBukrs_ConcorrenciaReal_NaoDuplicaViaConstraintUnique reproduz a
// condição de corrida apontada na revisão de código (2 goroutines reais
// disputando a mesma chave de idempotência) — antes do patch (SELECT-then-INSERT
// sem UNIQUE), este teste falharia de forma intermitente com 2 linhas criadas.
func TestSyncBukrs_ConcorrenciaReal_NaoDuplicaViaConstraintUnique(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	requestID := insertTestRFBRequest(t, db, companyID)
	chaves := []string{"chave-teste-concorrencia"}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// clientID/clientSecretEnc/baseURL fictícios — este teste valida apenas a
			// idempotência a nível de banco (constraint UNIQUE), não a chamada ao SAP.
			syncBukrs(db, companyID, "1000", requestID, "fake-client", "fake-secret-invalido", "http://localhost:0", chaves)
		}()
	}
	wg.Wait()

	if got := countSyncRuns(t, db, companyID, requestID); got != 1 {
		t.Errorf("esperava exatamente 1 execução após 10 chamadas concorrentes para o mesmo lote, obteve %d (constraint UNIQUE deveria ter bloqueado as demais)", got)
	}
}

// TestTriggerSAPSync_ChaveDFeNula_NaoAbortaFetch reproduz o achado de edge case:
// chave_dfe é nullable no schema de rfb_creditos — uma linha com chave_dfe NULL
// não pode abortar o fetch inteiro e impedir a sincronização das demais chaves.
func TestTriggerSAPSync_ChaveDFeNula_NaoAbortaFetch(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	insertSAPCredential(t, db, companyID, []string{"1000"}, "http://127.0.0.1:1")

	requestID := insertTestRFBRequest(t, db, companyID)
	insertTestCredito(t, db, requestID, companyID, "chave-teste-valida")
	if _, err := db.Exec(`
		INSERT INTO rfb_creditos (request_id, company_id, chave_dfe) VALUES ($1, $2, NULL)
	`, requestID, companyID); err != nil {
		t.Fatalf("falha ao criar rfb_creditos de teste com chave_dfe NULL: %v", err)
	}

	TriggerSAPSync(db, requestID)

	if got := countSyncRuns(t, db, companyID, requestID); got != 1 {
		t.Fatalf("esperava 1 execução (fetch não deveria abortar por causa da linha com chave_dfe NULL), obteve %d", got)
	}
}

func TestTriggerSAPSync_RequestIDInexistente_NaoPropagaErro(t *testing.T) {
	db := openTestDB(t)
	requestID := "00000000-0000-0000-0000-000000000000"
	// requestID inexistente — deve apenas logar e retornar, nunca panic/erro propagado (AC #3)
	TriggerSAPSync(db, requestID)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sap_sync_runs WHERE request_id = $1`, requestID).Scan(&count); err != nil {
		t.Fatalf("falha ao contar sap_sync_runs: %v", err)
	}
	if count != 0 {
		t.Errorf("esperava 0 execuções sap_sync_runs para requestID inexistente, obteve %d", count)
	}
}

// TestTriggerSAPSync_PersisteContagensEErroDetalhe cobre a Story 2.4 (AC #1):
// após uma sincronização real (fim a fim, via TriggerSAPSync), as contagens
// por paymentStatus e o detalhe da execução devem estar persistidos em
// sap_sync_runs, não apenas logados.
func TestTriggerSAPSync_PersisteContagensEErroDetalhe(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "fake-token", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DFeKeys []string `json:"dfeKeys"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		items := make([]map[string]interface{}, 0, len(req.DFeKeys))
		for i, k := range req.DFeKeys {
			status := []string{"PAGO_TOTAL", "PAGO_PARCIAL", "EM_ABERTO"}[i%3]
			items = append(items, map[string]interface{}{"dfeKey": k, "paymentStatus": status, "matchType": "CHAVE"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	insertSAPCredential(t, db, companyID, []string{"1000"}, srv.URL)

	requestID := insertTestRFBRequest(t, db, companyID)
	insertTestCredito(t, db, requestID, companyID, "35240612345678001905500100000000110000000002")
	insertTestCredito(t, db, requestID, companyID, "35240612345678001905500100000000110000000019")
	insertTestCredito(t, db, requestID, companyID, "35240612345678001905500100000000110000000026")

	TriggerSAPSync(db, requestID)

	var status, erroDetalhe string
	var pagoTotal, pagoParcial, emAberto int
	if err := db.QueryRow(`
		SELECT status, erro_detalhe, chaves_pago_total, chaves_pago_parcial, chaves_em_aberto
		FROM sap_sync_runs WHERE company_id = $1 AND bukrs = '1000'
	`, companyID).Scan(&status, &erroDetalhe, &pagoTotal, &pagoParcial, &emAberto); err != nil {
		t.Fatalf("falha ao ler sap_sync_runs: %v", err)
	}
	if status != "concluido" {
		t.Fatalf("esperava status=concluido, obteve %q", status)
	}
	if pagoTotal != 1 || pagoParcial != 1 || emAberto != 1 {
		t.Errorf("esperava 1 chave em cada contagem (PAGO_TOTAL/PAGO_PARCIAL/EM_ABERTO), obteve pagoTotal=%d pagoParcial=%d emAberto=%d", pagoTotal, pagoParcial, emAberto)
	}
	if erroDetalhe == "" {
		t.Error("esperava erro_detalhe preenchido (Story 2.4 passou a persistir o detail, antes só era logado)")
	}
}
