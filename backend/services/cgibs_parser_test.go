package services

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"fb_apu02/crypto"
)

// ─── Helpers de setup (services package — reaproveita openTestDB/setupTestCompany de
// sap_sync_trigger_test.go) ───

// insertCGIBSCredForParser insere uma credencial CGIBS ativa E habilitada (habilitado=true,
// data_habilitacao preenchida) — item 18 da revisão adversarial: ProcessarArquivoCGIBS agora
// exige habilitado=true além de ativo=true (mesma correção já aplicada em matchCGIBSCredential
// no webhook), então o setup de teste precisa refletir isso ou a credencial nunca é encontrada.
func insertCGIBSCredForParser(t *testing.T, db *sql.DB, companyID, clientID, clientSecretPlain string) {
	t.Helper()
	encSecret, err := crypto.EncryptField(clientSecretPlain)
	if err != nil {
		t.Fatalf("falha ao criptografar client_secret de teste: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO cgibs_credentials (company_id, cnpj_matriz, client_id, client_secret, ambiente, ativo, habilitado, data_habilitacao)
		VALUES ($1, '12345678000199', $2, $3, 'piloto', true, true, NOW())
	`, companyID, clientID, encSecret); err != nil {
		t.Fatalf("falha ao inserir cgibs_credentials de teste: %v", err)
	}
}

func insertCGIBSSolicitacaoForParser(t *testing.T, db *sql.DB, companyID, cnpjBase string, idSolicitacaoExterno int64) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO cgibs_solicitacoes (company_id, cnpj_base, id_solicitacao_externo, tipo_solicitacao, situacao_solicitacao)
		VALUES ($1, $2, $3, 'diferencial', 'gerada')
		RETURNING id
	`, companyID, cnpjBase, idSolicitacaoExterno).Scan(&id); err != nil {
		t.Fatalf("falha ao criar cgibs_solicitacoes de teste: %v", err)
	}
	return id
}

func insertCGIBSArquivoForParser(t *testing.T, db *sql.DB, companyID, solicitacaoID string, numeroSequencial int64) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO cgibs_arquivos (company_id, solicitacao_id, numero_sequencial, nome_arquivo, status)
		VALUES ($1, $2, $3, 'arquivo-teste.json', 'pendente')
		RETURNING id
	`, companyID, solicitacaoID, numeroSequencial).Scan(&id); err != nil {
		t.Fatalf("falha ao criar cgibs_arquivos de teste: %v", err)
	}
	return id
}

// setupCGIBSParserTest cria company + credencial ativa e devolve companyID/cleanup. O cleanup
// da company (cascade via environments) já limpa cgibs_solicitacoes/cgibs_arquivos/
// cgibs_operacoes/cgibs_lancamentos (company_id UUID com ON DELETE CASCADE, migration 131);
// cgibs_credentials precisa de DELETE explícito (company_id é TEXT sem FK, migration 102).
func setupCGIBSParserTest(t *testing.T, db *sql.DB) (companyID string, cleanup func()) {
	t.Helper()
	companyID, cleanupCompany := setupTestCompany(t, db)
	insertCGIBSCredForParser(t, db, companyID, "cid-parser-teste", "secret-parser-teste")
	cleanup = func() {
		db.Exec(`DELETE FROM cgibs_credentials WHERE company_id = $1`, companyID)
		cleanupCompany()
	}
	return companyID, cleanup
}

func cgibsFakeObterArquivoServer(t *testing.T, respBody []byte, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write(respBody)
	}))
}

func countCGIBSOperacoesTeste(t *testing.T, db *sql.DB, companyID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cgibs_operacoes WHERE company_id = $1`, companyID).Scan(&n); err != nil {
		t.Fatalf("falha ao contar cgibs_operacoes: %v", err)
	}
	return n
}

func countCGIBSLancamentosTeste(t *testing.T, db *sql.DB, companyID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cgibs_lancamentos WHERE company_id = $1`, companyID).Scan(&n); err != nil {
		t.Fatalf("falha ao contar cgibs_lancamentos: %v", err)
	}
	return n
}

// buildCGIBSArquivoPayload monta um arquivo de exemplo com 2 operações — a 1ª com extrato_cc
// (3 lançamentos), a 2ª sem extrato_cc — no formato do ANEXO I (Design Notes da spec). Quando
// lanc3ComoString é true, o 3º lançamento traz CREDITO_A_PROPRIAR como string ("123.45") em vez
// de número, exercitando FlexFloat.
func buildCGIBSArquivoPayload(chave1, chave2 string, lanc3ComoString bool) []byte {
	var creditoLanc3 interface{} = 123.45
	if lanc3ComoString {
		creditoLanc3 = "123.45"
	}
	makeLanc := func(id, mov int, credito interface{}) map[string]interface{} {
		return map[string]interface{}{
			"ID":         id,
			"DTH_LANCTO": "2026-09-27 10:00:10",
			"MOV":        mov,
			"RECURSO_FINANCEIRO_DISPONIVEL_PARA_TRANSFERENCIA": 0,
			"RECURSO_FINANCEIRO_A_TRANSFERIR":                  0,
			"CREDITO_A_PROPRIAR":                               credito,
			"CREDITO_NAO_UTILIZADO":                            0,
			"CREDITO_UTILIZADO":                                0,
			"DEBITO_EM_ABERTO":                                 150.00,
			"DEBITO_EXTINTO":                                   0,
		}
	}
	lista := []map[string]interface{}{
		makeLanc(1, 10, 150.00),
		makeLanc(2, 10, 50.00),
		makeLanc(3, 10, creditoLanc3),
	}
	payload := map[string]interface{}{
		"nomearquivo":     "arquivo1.json",
		"tiposolicitacao": "diferencial(delta)",
		"datageracao":     "2026-09-28 06:00:00",
		"parametrosgeracao": map[string]interface{}{
			"datainicial": "2026-09-27", "datafinal": "2026-09-28", "qtdoperacoes": 2,
		},
		"operacoes": []map[string]interface{}{
			{
				"ID": 123, "CHAVE_ACESSO": chave1,
				"DTH_EMISSAO":     "2026-09-27 10:00:00.000-03:00",
				"DTH_AUTORIZACAO": "2026-09-27 10:00:05.000-03:00",
				"CNPJ_FORNECEDOR": "12345678", "CNPJ_ADQUIRENTE": "98765432",
				"extrato_cc": map[string]interface{}{"hash": "abc123", "lista": lista},
			},
			{
				"ID": 456, "CHAVE_ACESSO": chave2,
				"DTH_EMISSAO":     "2026-09-27 11:00:00.000-03:00",
				"DTH_AUTORIZACAO": "2026-09-27 11:00:05.000-03:00",
				"CNPJ_FORNECEDOR": "12345678",
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

// TestProcessarArquivoCGIBS_DuasOperacoesUmaComExtratoCC_Grava2Operacoes3Lancamentos cobre o
// Acceptance Criteria principal da spec: arquivo com 2 operações (1 com 3 lançamentos, 1 sem
// extrato_cc) grava exatamente 2 cgibs_operacoes e 3 cgibs_lancamentos, arquivo vira 'baixado'.
func TestProcessarArquivoCGIBS_DuasOperacoesUmaComExtratoCC_Grava2Operacoes3Lancamentos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chave1 := strings.Repeat("1", 44)
	chave2 := strings.Repeat("2", 44)
	payload := buildCGIBSArquivoPayload(chave1, chave2, false)

	srv := cgibsFakeObterArquivoServer(t, payload, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9001)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err != nil {
		t.Fatalf("ProcessarArquivoCGIBS retornou erro inesperado: %v", err)
	}

	if got := countCGIBSOperacoesTeste(t, db, companyID); got != 2 {
		t.Errorf("esperava 2 cgibs_operacoes, obteve %d", got)
	}
	if got := countCGIBSLancamentosTeste(t, db, companyID); got != 3 {
		t.Errorf("esperava 3 cgibs_lancamentos, obteve %d", got)
	}

	var status string
	var baixadoEm sql.NullTime
	if err := db.QueryRow(`SELECT status, baixado_em FROM cgibs_arquivos WHERE id = $1`, arquivoID).Scan(&status, &baixadoEm); err != nil {
		t.Fatalf("erro ao ler cgibs_arquivos: %v", err)
	}
	if status != "baixado" {
		t.Errorf("esperava status=baixado, veio %q", status)
	}
	if !baixadoEm.Valid {
		t.Errorf("esperava baixado_em preenchido")
	}
}

// TestProcessarArquivoCGIBS_Reprocessar_NaoDuplicaLancamentos cobre a idempotência exigida
// pelo Acceptance Criteria: reprocessar o mesmo arquivo (reenvio) não duplica lançamentos
// (ON CONFLICT (operacao_id, lancamento_id_externo) DO NOTHING).
func TestProcessarArquivoCGIBS_Reprocessar_NaoDuplicaLancamentos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chave1 := strings.Repeat("3", 44)
	chave2 := strings.Repeat("4", 44)
	payload := buildCGIBSArquivoPayload(chave1, chave2, false)

	srv := cgibsFakeObterArquivoServer(t, payload, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9002)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err != nil {
		t.Fatalf("1ª chamada: erro inesperado: %v", err)
	}
	if err := ProcessarArquivoCGIBS(db, arquivoID); err != nil {
		t.Fatalf("2ª chamada (reprocessamento): erro inesperado: %v", err)
	}

	if got := countCGIBSOperacoesTeste(t, db, companyID); got != 2 {
		t.Errorf("esperava 2 cgibs_operacoes após reprocessar, obteve %d", got)
	}
	if got := countCGIBSLancamentosTeste(t, db, companyID); got != 3 {
		t.Errorf("esperava 3 cgibs_lancamentos após reprocessar (sem duplicar), obteve %d", got)
	}
}

// TestProcessarArquivoCGIBS_ValorMonetarioComoString_NaoQuebraParse cobre o Acceptance
// Criteria: um valor monetário vindo como string ("123.45") é parseado sem erro e gravado
// corretamente (FlexFloat).
func TestProcessarArquivoCGIBS_ValorMonetarioComoString_NaoQuebraParse(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chave1 := strings.Repeat("5", 44)
	chave2 := strings.Repeat("6", 44)
	payload := buildCGIBSArquivoPayload(chave1, chave2, true)

	srv := cgibsFakeObterArquivoServer(t, payload, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9003)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err != nil {
		t.Fatalf("ProcessarArquivoCGIBS retornou erro inesperado: %v", err)
	}

	var creditoAApropriar float64
	if err := db.QueryRow(`
		SELECT l.credito_a_apropriar FROM cgibs_lancamentos l
		JOIN cgibs_operacoes o ON o.id = l.operacao_id
		WHERE o.company_id = $1 AND o.chave_acesso = $2 AND l.lancamento_id_externo = 3
	`, companyID, chave1).Scan(&creditoAApropriar); err != nil {
		t.Fatalf("erro ao ler lançamento gravado: %v", err)
	}
	if creditoAApropriar != 123.45 {
		t.Errorf("esperava credito_a_apropriar=123.45, veio %v", creditoAApropriar)
	}
}

// TestProcessarArquivoCGIBS_FalhaAoObterArquivo_MarcaErro cobre o caso em que o download em si
// falha (URL não configurada, sem tentar rede) — status vira 'erro' com error_message, e
// raw_json permanece NULL (nunca houve resposta para salvar).
func TestProcessarArquivoCGIBS_FalhaAoObterArquivo_MarcaErro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", "")

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9004)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err == nil {
		t.Fatal("esperava erro (URL não configurada), obteve nil")
	}

	var status string
	var errorMessage, rawJSON sql.NullString
	if err := db.QueryRow(`SELECT status, error_message, raw_json FROM cgibs_arquivos WHERE id = $1`, arquivoID).Scan(&status, &errorMessage, &rawJSON); err != nil {
		t.Fatalf("erro ao ler cgibs_arquivos: %v", err)
	}
	if status != "erro" {
		t.Errorf("esperava status=erro, veio %q", status)
	}
	if !errorMessage.Valid || errorMessage.String == "" {
		t.Errorf("esperava error_message preenchido")
	}
	if rawJSON.Valid {
		t.Errorf("esperava raw_json NULL (download nunca ocorreu), veio %q", rawJSON.String)
	}
}

// TestProcessarArquivoCGIBS_DownloadOkParseFalha_PreservaRawJSON cobre o Acceptance Criteria:
// se o download teve sucesso mas o parse falhou, raw_json (o dado bruto recebido) não pode se
// perder — só o parse falha, status vira 'erro' com error_message preenchido.
func TestProcessarArquivoCGIBS_DownloadOkParseFalha_PreservaRawJSON(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	malformed := []byte(`{"nomearquivo": "arquivo-quebrado.json", "operacoes": [`) // JSON truncado de propósito

	srv := cgibsFakeObterArquivoServer(t, malformed, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9005)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err == nil {
		t.Fatal("esperava erro de parse, obteve nil")
	}

	var status string
	var errorMessage, rawJSON sql.NullString
	if err := db.QueryRow(`SELECT status, error_message, raw_json FROM cgibs_arquivos WHERE id = $1`, arquivoID).Scan(&status, &errorMessage, &rawJSON); err != nil {
		t.Fatalf("erro ao ler cgibs_arquivos: %v", err)
	}
	if status != "erro" {
		t.Errorf("esperava status=erro, veio %q", status)
	}
	if !errorMessage.Valid || errorMessage.String == "" {
		t.Errorf("esperava error_message preenchido")
	}
	if !rawJSON.Valid || rawJSON.String != string(malformed) {
		t.Errorf("esperava raw_json preservado com o corpo bruto recebido, veio %q", rawJSON.String)
	}
}

// TestProcessarArquivoCGIBS_OperacaoComChaveAcessoInvalida_NaoImpedeDemais cobre o Acceptance
// Criteria: uma operação com CHAVE_ACESSO de tamanho errado (violaria o CHECK length=44 da
// migration 131) não impede que as OUTRAS operações do mesmo arquivo sejam gravadas com
// sucesso — isolamento via SAVEPOINT.
func TestProcessarArquivoCGIBS_OperacaoComChaveAcessoInvalida_NaoImpedeDemais(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chaveValida := strings.Repeat("7", 44)
	chaveInvalida := strings.Repeat("9", 10) // tamanho errado (10, não 44)

	payload := map[string]interface{}{
		"nomearquivo":     "arquivo-misto.json",
		"tiposolicitacao": "diferencial(delta)",
		"datageracao":     "2026-09-28 06:00:00",
		"parametrosgeracao": map[string]interface{}{
			"datainicial": "2026-09-27", "datafinal": "2026-09-28", "qtdoperacoes": 2,
		},
		"operacoes": []map[string]interface{}{
			{"ID": 1, "CHAVE_ACESSO": chaveInvalida, "DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "CNPJ_FORNECEDOR": "12345678"},
			{"ID": 2, "CHAVE_ACESSO": chaveValida, "DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "CNPJ_FORNECEDOR": "12345678"},
		},
	}
	body, _ := json.Marshal(payload)

	srv := cgibsFakeObterArquivoServer(t, body, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9006)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err != nil {
		t.Fatalf("esperava sucesso (item isolado não aborta o arquivo), obteve erro: %v", err)
	}

	if got := countCGIBSOperacoesTeste(t, db, companyID); got != 1 {
		t.Errorf("esperava 1 cgibs_operacoes (só a válida), obteve %d", got)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM cgibs_arquivos WHERE id = $1`, arquivoID).Scan(&status); err != nil {
		t.Fatalf("erro ao ler status: %v", err)
	}
	if status != "baixado" {
		t.Errorf("esperava status=baixado (mesmo com 1 operação em erro), veio %q", status)
	}
}

// TestAbortCGIBSArquivo_RollbackAntesDeMarcarErro_NaoTrava cobre o item 1 da revisão
// adversarial (risco de deadlock/esgotamento do pool): simula exatamente o cenário de
// ProcessarArquivoCGIBS — uma tx que já fez SELECT ... FOR UPDATE na linha de cgibs_arquivos —
// e confirma que abortCGIBSArquivo(db, tx, ...) NÃO trava esperando o próprio lock que acabou de
// segurar: ele precisa fazer tx.Rollback() explicitamente ANTES de marcar erro numa conexão
// nova. Sem esse Rollback explícito (bug original), a conexão nova do markCGIBSArquivoErro
// ficaria bloqueada indefinidamente esperando o lock que só o defer Rollback (nunca executado,
// porque a própria chamada está pendurada) liberaria — deadlock. Usa timeout curto: o teste
// falha se travar.
func TestAbortCGIBSArquivo_RollbackAntesDeMarcarErro_NaoTrava(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9100)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("erro ao abrir tx: %v", err)
	}
	var status string
	if err := tx.QueryRow(`SELECT status FROM cgibs_arquivos WHERE id = $1 FOR UPDATE`, arquivoID).Scan(&status); err != nil {
		t.Fatalf("erro no SELECT FOR UPDATE: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- abortCGIBSArquivo(db, tx, arquivoID, "falha simulada de teste (item 1)")
	}()

	select {
	case <-done:
		// OK: retornou sem travar.
	case <-time.After(5 * time.Second):
		t.Fatal("abortCGIBSArquivo travou — Rollback não está sendo chamado antes de markCGIBSArquivoErro (item 1 da revisão adversarial: risco de deadlock/esgotamento do pool)")
	}

	var finalStatus string
	if err := db.QueryRow(`SELECT status FROM cgibs_arquivos WHERE id = $1`, arquivoID).Scan(&finalStatus); err != nil {
		t.Fatalf("erro ao ler status final: %v", err)
	}
	if finalStatus != "erro" {
		t.Errorf("esperava status=erro após abortCGIBSArquivo, veio %q", finalStatus)
	}
}

// TestProcessarArquivoCGIBS_DuasChamadasConcorrentes_NaoDuplicaOperacoesLancamentos cobre o
// item 2 da revisão adversarial: 2 chamadas concorrentes de ProcessarArquivoCGIBS para o MESMO
// arquivoID (ex.: webhook da CGIBS reentregue, disparando 2 goroutines) não devem duplicar
// operações/lançamentos — o claim atômico via `SELECT ... FOR UPDATE` serializa as 2 chamadas: a
// que perde a corrida encontra status != 'pendente' e sai sem reprocessar.
func TestProcessarArquivoCGIBS_DuasChamadasConcorrentes_NaoDuplicaOperacoesLancamentos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chave1 := strings.Repeat("a", 44)
	chave2 := strings.Repeat("b", 44)
	payload := buildCGIBSArquivoPayload(chave1, chave2, false)

	srv := cgibsFakeObterArquivoServer(t, payload, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9101)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- ProcessarArquivoCGIBS(db, arquivoID)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("chamada concorrente retornou erro inesperado: %v", err)
		}
	}

	if got := countCGIBSOperacoesTeste(t, db, companyID); got != 2 {
		t.Errorf("esperava 2 cgibs_operacoes (sem duplicar após 2 chamadas concorrentes), obteve %d", got)
	}
	if got := countCGIBSLancamentosTeste(t, db, companyID); got != 3 {
		t.Errorf("esperava 3 cgibs_lancamentos (sem duplicar após 2 chamadas concorrentes), obteve %d", got)
	}
}

// TestProcessarArquivoCGIBS_MOVeIDZeroLegitimos_LancamentoSemIDViraErroIsolado cobre o item 6
// da revisão adversarial: ID=0 (operação) e MOV=0 (lançamento) são valores legítimos e devem
// ser gravados como 0, não como NULL (ambiguidade "ausente" vs "zero" resolvida com ponteiro);
// já um lançamento SEM o campo ID (obrigatório pelo MOC) vira erro isolado via SAVEPOINT, sem
// abortar o restante do arquivo.
func TestProcessarArquivoCGIBS_MOVeIDZeroLegitimos_LancamentoSemIDViraErroIsolado(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chave := strings.Repeat("c", 44)
	payload := map[string]interface{}{
		"nomearquivo":     "arquivo-zeros.json",
		"tiposolicitacao": "diferencial(delta)",
		"datageracao":     "2026-09-28 06:00:00",
		"parametrosgeracao": map[string]interface{}{
			"datainicial": "2026-09-27", "datafinal": "2026-09-28", "qtdoperacoes": 1,
		},
		"operacoes": []map[string]interface{}{
			{
				"ID": 0, "CHAVE_ACESSO": chave,
				"DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "CNPJ_FORNECEDOR": "12345678",
				"extrato_cc": map[string]interface{}{"hash": "h1", "lista": []map[string]interface{}{
					{
						"ID": 1, "DTH_LANCTO": "2026-09-27 10:00:10", "MOV": 0,
						"RECURSO_FINANCEIRO_DISPONIVEL_PARA_TRANSFERENCIA": 0, "RECURSO_FINANCEIRO_A_TRANSFERIR": 0,
						"CREDITO_A_PROPRIAR": 10.0, "CREDITO_NAO_UTILIZADO": 0, "CREDITO_UTILIZADO": 0,
						"DEBITO_EM_ABERTO": 10.0, "DEBITO_EXTINTO": 0,
					},
					{
						// Sem "ID" -- campo obrigatório ausente, deve virar erro isolado.
						"DTH_LANCTO": "2026-09-27 11:00:10", "MOV": 20,
						"RECURSO_FINANCEIRO_DISPONIVEL_PARA_TRANSFERENCIA": 0, "RECURSO_FINANCEIRO_A_TRANSFERIR": 0,
						"CREDITO_A_PROPRIAR": 20.0, "CREDITO_NAO_UTILIZADO": 0, "CREDITO_UTILIZADO": 0,
						"DEBITO_EM_ABERTO": 20.0, "DEBITO_EXTINTO": 0,
					},
				}},
			},
		},
	}
	body, _ := json.Marshal(payload)

	srv := cgibsFakeObterArquivoServer(t, body, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9102)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err != nil {
		t.Fatalf("ProcessarArquivoCGIBS retornou erro inesperado: %v", err)
	}

	var operacaoID string
	var operacaoIDExterno sql.NullInt64
	if err := db.QueryRow(`
		SELECT id, operacao_id_externo FROM cgibs_operacoes WHERE company_id = $1 AND chave_acesso = $2
	`, companyID, chave).Scan(&operacaoID, &operacaoIDExterno); err != nil {
		t.Fatalf("erro ao ler operacao: %v", err)
	}
	if !operacaoIDExterno.Valid || operacaoIDExterno.Int64 != 0 {
		t.Errorf("esperava operacao_id_externo=0 (presente, não NULL), veio valid=%v value=%v", operacaoIDExterno.Valid, operacaoIDExterno.Int64)
	}

	if got := countCGIBSLancamentosTeste(t, db, companyID); got != 1 {
		t.Errorf("esperava 1 lançamento gravado (o sem ID vira erro isolado, não abortando o arquivo), obteve %d", got)
	}

	var movCodigo sql.NullInt64
	if err := db.QueryRow(`SELECT mov_codigo FROM cgibs_lancamentos WHERE operacao_id = $1`, operacaoID).Scan(&movCodigo); err != nil {
		t.Fatalf("erro ao ler mov_codigo: %v", err)
	}
	if !movCodigo.Valid || movCodigo.Int64 != 0 {
		t.Errorf("esperava mov_codigo=0 (presente, não NULL), veio valid=%v value=%v", movCodigo.Valid, movCodigo.Int64)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM cgibs_arquivos WHERE id = $1`, arquivoID).Scan(&status); err != nil {
		t.Fatalf("erro ao ler status: %v", err)
	}
	if status != "baixado" {
		t.Errorf("esperava status=baixado (falha isolada do lançamento não aborta o arquivo), veio %q", status)
	}
}

// TestProcessarArquivoCGIBS_ArquivoPosteriorComCamposVazios_NaoApagaDadosBonsAnteriores cobre o
// item 14 da revisão adversarial: um 2º arquivo (posterior, delta) para a MESMA operação
// (chave_acesso) trazendo DTH_EMISSAO/DTH_AUTORIZACAO ilegíveis e sem CNPJ_ADQUIRENTE/
// extrato_cc não pode apagar os valores bons já gravados pelo 1º arquivo — só
// ultimo_arquivo_id/updated_at sempre avançam pro arquivo mais recente.
func TestProcessarArquivoCGIBS_ArquivoPosteriorComCamposVazios_NaoApagaDadosBonsAnteriores(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chave := strings.Repeat("d", 44)

	payload1 := map[string]interface{}{
		"nomearquivo": "arquivo1.json", "tiposolicitacao": "diferencial(delta)", "datageracao": "2026-09-28 06:00:00",
		"parametrosgeracao": map[string]interface{}{"datainicial": "2026-09-27", "datafinal": "2026-09-28", "qtdoperacoes": 1},
		"operacoes": []map[string]interface{}{
			{
				"ID": 1, "CHAVE_ACESSO": chave,
				"DTH_EMISSAO":     "2026-09-27 10:00:00.000-03:00",
				"DTH_AUTORIZACAO": "2026-09-27 10:00:05.000-03:00",
				"CNPJ_FORNECEDOR": "12345678", "CNPJ_ADQUIRENTE": "98765432",
				"extrato_cc": map[string]interface{}{"hash": "hash-bom", "lista": []map[string]interface{}{}},
			},
		},
	}
	body1, _ := json.Marshal(payload1)
	srv1 := cgibsFakeObterArquivoServer(t, body1, http.StatusOK)
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv1.URL)
	solicitacaoID1 := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9103)
	arquivoID1 := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID1, 1)
	if err := ProcessarArquivoCGIBS(db, arquivoID1); err != nil {
		t.Fatalf("1º arquivo: erro inesperado: %v", err)
	}
	srv1.Close()

	payload2 := map[string]interface{}{
		"nomearquivo": "arquivo2.json", "tiposolicitacao": "diferencial(delta)", "datageracao": "2026-09-28 07:00:00",
		"parametrosgeracao": map[string]interface{}{"datainicial": "2026-09-28", "datafinal": "2026-09-28", "qtdoperacoes": 1},
		"operacoes": []map[string]interface{}{
			{
				"ID": 1, "CHAVE_ACESSO": chave,
				"DTH_EMISSAO": "data-ilegivel", "DTH_AUTORIZACAO": "",
				"CNPJ_FORNECEDOR": "12345678",
			},
		},
	}
	body2, _ := json.Marshal(payload2)
	srv2 := cgibsFakeObterArquivoServer(t, body2, http.StatusOK)
	defer srv2.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv2.URL)
	solicitacaoID2 := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9104)
	arquivoID2 := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID2, 1)
	if err := ProcessarArquivoCGIBS(db, arquivoID2); err != nil {
		t.Fatalf("2º arquivo: erro inesperado: %v", err)
	}

	var dthEmissao, dthAutorizacao sql.NullTime
	var cnpjAdquirente, extratoHash sql.NullString
	var ultimoArquivoID string
	if err := db.QueryRow(`
		SELECT dth_emissao, dth_autorizacao, cnpj_adquirente, extrato_hash, ultimo_arquivo_id
		FROM cgibs_operacoes WHERE company_id = $1 AND chave_acesso = $2
	`, companyID, chave).Scan(&dthEmissao, &dthAutorizacao, &cnpjAdquirente, &extratoHash, &ultimoArquivoID); err != nil {
		t.Fatalf("erro ao ler operacao: %v", err)
	}

	if !dthEmissao.Valid {
		t.Errorf("esperava dth_emissao preservado do 1º arquivo, veio NULL")
	}
	if !dthAutorizacao.Valid {
		t.Errorf("esperava dth_autorizacao preservado do 1º arquivo, veio NULL")
	}
	if !cnpjAdquirente.Valid || cnpjAdquirente.String != "98765432" {
		t.Errorf("esperava cnpj_adquirente preservado ('98765432'), veio valid=%v value=%q", cnpjAdquirente.Valid, cnpjAdquirente.String)
	}
	if !extratoHash.Valid || extratoHash.String != "hash-bom" {
		t.Errorf("esperava extrato_hash preservado ('hash-bom'), veio valid=%v value=%q", extratoHash.Valid, extratoHash.String)
	}
	if ultimoArquivoID != arquivoID2 {
		t.Errorf("esperava ultimo_arquivo_id avançado pro arquivo mais recente (%s), veio %s", arquivoID2, ultimoArquivoID)
	}
}

// TestProcessarArquivoCGIBS_LancamentoRepetidoEmArquivoPosterior_NaoDuplicaEContinuaSemErro
// cobre o item 7 da revisão adversarial: um lançamento com o mesmo (operacao_id,
// lancamento_id_externo) aparecendo num 2º arquivo (ex.: delta que re-envia algo já visto) cai
// no ON CONFLICT DO NOTHING — não duplica e não é tratado como erro.
func TestProcessarArquivoCGIBS_LancamentoRepetidoEmArquivoPosterior_NaoDuplicaEContinuaSemErro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chave := strings.Repeat("i", 44)
	makeLancPayload := func(nomeArquivo string, dataGeracao string) []byte {
		payload := map[string]interface{}{
			"nomearquivo": nomeArquivo, "tiposolicitacao": "diferencial(delta)", "datageracao": dataGeracao,
			"parametrosgeracao": map[string]interface{}{"datainicial": "2026-09-27", "datafinal": "2026-09-28", "qtdoperacoes": 1},
			"operacoes": []map[string]interface{}{
				{
					"ID": 1, "CHAVE_ACESSO": chave,
					"DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "CNPJ_FORNECEDOR": "12345678",
					"extrato_cc": map[string]interface{}{"hash": "h1", "lista": []map[string]interface{}{
						{
							"ID": 1, "DTH_LANCTO": "2026-09-27 10:00:10", "MOV": 10,
							"RECURSO_FINANCEIRO_DISPONIVEL_PARA_TRANSFERENCIA": 0, "RECURSO_FINANCEIRO_A_TRANSFERIR": 0,
							"CREDITO_A_PROPRIAR": 5.0, "CREDITO_NAO_UTILIZADO": 0, "CREDITO_UTILIZADO": 0,
							"DEBITO_EM_ABERTO": 5.0, "DEBITO_EXTINTO": 0,
						},
					}},
				},
			},
		}
		b, _ := json.Marshal(payload)
		return b
	}

	srv1 := cgibsFakeObterArquivoServer(t, makeLancPayload("arquivo1.json", "2026-09-28 06:00:00"), http.StatusOK)
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv1.URL)
	solicitacaoID1 := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9107)
	arquivoID1 := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID1, 1)
	if err := ProcessarArquivoCGIBS(db, arquivoID1); err != nil {
		t.Fatalf("1º arquivo: erro inesperado: %v", err)
	}
	srv1.Close()

	srv2 := cgibsFakeObterArquivoServer(t, makeLancPayload("arquivo2.json", "2026-09-28 07:00:00"), http.StatusOK)
	defer srv2.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv2.URL)
	solicitacaoID2 := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9108)
	arquivoID2 := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID2, 1)
	if err := ProcessarArquivoCGIBS(db, arquivoID2); err != nil {
		t.Fatalf("2º arquivo (lançamento repetido): erro inesperado: %v", err)
	}

	if got := countCGIBSLancamentosTeste(t, db, companyID); got != 1 {
		t.Errorf("esperava 1 lançamento (2º arquivo repetiu o mesmo lancamento_id_externo, ON CONFLICT DO NOTHING), obteve %d", got)
	}

	var status2 string
	if err := db.QueryRow(`SELECT status FROM cgibs_arquivos WHERE id = $1`, arquivoID2).Scan(&status2); err != nil {
		t.Fatalf("erro ao ler status do 2º arquivo: %v", err)
	}
	if status2 != "baixado" {
		t.Errorf("esperava status=baixado no 2º arquivo (conflito não é erro), veio %q", status2)
	}
}

// TestProcessarArquivoCGIBS_TodasOperacoesComChaveInvalida_MarcaErro cobre o item 15 da revisão
// adversarial: quando TODAS as operações do payload falham (nenhum dado real gravado), o arquivo
// não pode ficar marcado como 'baixado' silenciosamente — precisa virar 'erro' com
// error_message resumindo.
func TestProcessarArquivoCGIBS_TodasOperacoesComChaveInvalida_MarcaErro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	payload := map[string]interface{}{
		"nomearquivo": "arquivo-tudo-invalido.json", "tiposolicitacao": "diferencial(delta)", "datageracao": "2026-09-28 06:00:00",
		"parametrosgeracao": map[string]interface{}{"datainicial": "2026-09-27", "datafinal": "2026-09-28", "qtdoperacoes": 2},
		"operacoes": []map[string]interface{}{
			{"ID": 1, "CHAVE_ACESSO": strings.Repeat("e", 10), "DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "CNPJ_FORNECEDOR": "12345678"},
			{"ID": 2, "CHAVE_ACESSO": strings.Repeat("f", 10), "DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "CNPJ_FORNECEDOR": "12345678"},
		},
	}
	body, _ := json.Marshal(payload)

	srv := cgibsFakeObterArquivoServer(t, body, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9105)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err == nil {
		t.Fatal("esperava erro (todas as operações falharam), obteve nil")
	}

	var status string
	var errorMessage sql.NullString
	if err := db.QueryRow(`SELECT status, error_message FROM cgibs_arquivos WHERE id = $1`, arquivoID).Scan(&status, &errorMessage); err != nil {
		t.Fatalf("erro ao ler cgibs_arquivos: %v", err)
	}
	if status != "erro" {
		t.Errorf("esperava status=erro (todas as operações falharam), veio %q", status)
	}
	if !errorMessage.Valid || errorMessage.String == "" {
		t.Errorf("esperava error_message resumindo a falha total, veio vazio/NULL")
	}
	if got := countCGIBSOperacoesTeste(t, db, companyID); got != 0 {
		t.Errorf("esperava 0 cgibs_operacoes (todas inválidas), obteve %d", got)
	}
}

// TestProcessarArquivoCGIBS_FalhaParcial_MantemBaixadoComErrorMessageResumo cobre o item 15:
// falha PARCIAL (nem todas as operações falharam) mantém status='baixado' (dado real foi
// gravado), mas grava um resumo em error_message em vez de deixá-lo NULL silenciosamente.
func TestProcessarArquivoCGIBS_FalhaParcial_MantemBaixadoComErrorMessageResumo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupCGIBSParserTest(t, db)
	defer cleanup()

	chaveValida := strings.Repeat("g", 44)
	chaveInvalida := strings.Repeat("h", 10)
	payload := map[string]interface{}{
		"nomearquivo": "arquivo-parcial.json", "tiposolicitacao": "diferencial(delta)", "datageracao": "2026-09-28 06:00:00",
		"parametrosgeracao": map[string]interface{}{"datainicial": "2026-09-27", "datafinal": "2026-09-28", "qtdoperacoes": 2},
		"operacoes": []map[string]interface{}{
			{"ID": 1, "CHAVE_ACESSO": chaveInvalida, "DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "CNPJ_FORNECEDOR": "12345678"},
			{"ID": 2, "CHAVE_ACESSO": chaveValida, "DTH_EMISSAO": "2026-09-27 10:00:00.000-03:00", "CNPJ_FORNECEDOR": "12345678"},
		},
	}
	body, _ := json.Marshal(payload)

	srv := cgibsFakeObterArquivoServer(t, body, http.StatusOK)
	defer srv.Close()
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoForParser(t, db, companyID, "12345678", 9106)
	arquivoID := insertCGIBSArquivoForParser(t, db, companyID, solicitacaoID, 1)

	if err := ProcessarArquivoCGIBS(db, arquivoID); err != nil {
		t.Fatalf("esperava sucesso (falha parcial não aborta o arquivo), obteve erro: %v", err)
	}

	var status string
	var errorMessage sql.NullString
	if err := db.QueryRow(`SELECT status, error_message FROM cgibs_arquivos WHERE id = $1`, arquivoID).Scan(&status, &errorMessage); err != nil {
		t.Fatalf("erro ao ler cgibs_arquivos: %v", err)
	}
	if status != "baixado" {
		t.Errorf("esperava status=baixado (falha parcial, dado real gravado), veio %q", status)
	}
	if !errorMessage.Valid || errorMessage.String == "" {
		t.Errorf("esperava error_message resumindo a falha parcial mesmo com status=baixado, veio vazio/NULL")
	}
}

// TestParseCGIBSParserTime_SemOffset_AssumeHorarioDeBrasilia cobre o item 17 da revisão
// adversarial: um valor de data/hora sem offset explícito (formato de DTH_LANCTO) é interpretado
// como horário de Brasília (America/Sao_Paulo, UTC-3 fixo desde o fim do horário de verão em
// 2019), não UTC.
func TestParseCGIBSParserTime_SemOffset_AssumeHorarioDeBrasilia(t *testing.T) {
	got := parseCGIBSParserTime("2026-09-27 10:00:10")
	if got == nil {
		t.Fatal("esperava data/hora parseada, obteve nil")
	}
	utc := got.UTC()
	wantUTC := time.Date(2026, 9, 27, 13, 0, 10, 0, time.UTC)
	if !utc.Equal(wantUTC) {
		t.Errorf("esperava %s (10:00:10 America/Sao_Paulo = 13:00:10 UTC), veio %s", wantUTC.Format(time.RFC3339), utc.Format(time.RFC3339))
	}
}

// TestFlexInt64_UnmarshalJSON cobre os itens 3 e 4 da revisão adversarial: rejeita string com
// parte fracionária não-zero (em vez de truncar silenciosamente) e valida a faixa de int64 antes
// de converter um float, além do comportamento normal (número, string inteira, "123.0",
// vazio/null).
func TestFlexInt64_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		want    int64
		wantErr bool
	}{
		{"numero", "123", 123, false},
		{"string inteira", `"123"`, 123, false},
		{"string decimal exata", `"123.0"`, 123, false},
		{"vazio", `""`, 0, false},
		{"null", `null`, 0, false},
		{"fracionario nao aceito", `"123.5"`, 0, true},
		{"fora da faixa int64", `"1e400"`, 0, true},
		{"nao numerico", `"abc"`, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var fi FlexInt64
			err := json.Unmarshal([]byte(c.json), &fi)
			if c.wantErr {
				if err == nil {
					t.Fatalf("esperava erro para %q, obteve nil (valor=%d)", c.json, fi)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado para %q: %v", c.json, err)
			}
			if int64(fi) != c.want {
				t.Errorf("esperava %d, obteve %d", c.want, int64(fi))
			}
		})
	}
}

// TestFlexFloat_UnmarshalJSON cobre o item 5 da revisão adversarial: NaN/Inf/-Inf (que
// strconv.ParseFloat aceita como texto, mas não fazem sentido num saldo monetário) são
// rejeitados.
func TestFlexFloat_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		want    float64
		wantErr bool
	}{
		{"numero", "123.45", 123.45, false},
		{"string numerica", `"123.45"`, 123.45, false},
		{"vazio", `""`, 0, false},
		{"NaN string", `"NaN"`, 0, true},
		{"Inf string", `"Inf"`, 0, true},
		{"-Inf string", `"-Inf"`, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ff FlexFloat
			err := json.Unmarshal([]byte(c.json), &ff)
			if c.wantErr {
				if err == nil {
					t.Fatalf("esperava erro para %q, obteve nil (valor=%v)", c.json, ff)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado para %q: %v", c.json, err)
			}
			if float64(ff) != c.want {
				t.Errorf("esperava %v, obteve %v", c.want, float64(ff))
			}
		})
	}
}
