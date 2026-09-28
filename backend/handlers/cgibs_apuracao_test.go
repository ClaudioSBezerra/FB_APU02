package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// postSolicitarCGIBS chama SolicitarCGIBSApuracaoHandler com o corpo {data_ini, data_fim},
// autenticado via `authed` (mesmo padrão de postHabilitar em cgibs_habilitar_test.go).
func postSolicitarCGIBS(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, dataIni, dataFim string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"data_ini":%q,"data_fim":%q}`, dataIni, dataFim)
	req := httptest.NewRequest(http.MethodPost, "/api/cgibs/apuracao/solicitar", strings.NewReader(body))
	req = authed(req)
	rec := httptest.NewRecorder()
	SolicitarCGIBSApuracaoHandler(db)(rec, req)
	return rec
}

func TestSolicitarCGIBSApuracaoHandler_SemCredencial_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec := postSolicitarCGIBS(t, db, authed, "2026-01-01", "2026-01-31")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 sem credencial cadastrada, obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSolicitarCGIBSApuracaoHandler_CredencialNaoHabilitada_NaoTentaRede cobre o boundary da
// spec: uma credencial ativa mas ainda NÃO habilitada (habilitado=false) bloqueia a solicitação
// com erro claro, SEM tentar a chamada de rede — confirmado aqui verificando que o fake server
// nunca é chamado.
func TestSolicitarCGIBSApuracaoHandler_CredencialNaoHabilitada_NaoTentaRede(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, false) // ativo=true, habilitado=false

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2026-01-01", "2026-01-31")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (credencial não habilitada), obteve %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("não deveria ter chamado a API CGIBS — credencial ainda não habilitada")
	}
}

// TestSolicitarCGIBSApuracaoHandler_ComFakeServer_CriaSolicitacao confirma o caminho feliz: com
// credencial habilitada, o handler chama services.NovaSolicitacao de verdade (não mais sempre
// 503) e persiste a solicitação criada em cgibs_solicitacoes.
func TestSolicitarCGIBSApuracaoHandler_ComFakeServer_CriaSolicitacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true) // habilitado=true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var captured map[string]interface{}
		json.NewDecoder(r.Body).Decode(&captured)
		if captured["CNPJ"] != "12345678" {
			t.Errorf("esperava CNPJ=12345678 (raiz do cnpj_matriz), veio %v", captured["CNPJ"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"TokenContrib":        "token-x",
			"TipoSolicitacao":     "diferencial",
			"SituacaoSolicitacao": "solicitada",
			"IDSolicitacao":       777,
			"DataTransacaoIni":    "2026-01-01",
			"DataTransacaoFim":    "2026-01-31",
			"Resultado":           "sucesso",
		})
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2026-01-01", "2026-01-31")
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperava 201, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM cgibs_solicitacoes
		WHERE company_id=$1 AND id_solicitacao_externo=777 AND situacao_solicitacao='solicitada'
	`, companyID).Scan(&count); err != nil {
		t.Fatalf("erro ao consultar cgibs_solicitacoes: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava 1 solicitação gravada com id_solicitacao_externo=777, obteve %d", count)
	}
}

// TestSolicitarCGIBSApuracaoHandler_ErroDeRede_GravaLinhaComErro confirma que uma falha de
// rede/API (não mais o 503 fixo antigo) vira um erro claro (502) E grava uma linha em
// cgibs_solicitacoes com error_message preenchido (revisão adversarial, item 2) — sem isso, a
// falha ficava invisível pro usuário e ClearErrorsCGIBSHandler era um no-op morto, já que nada
// preenchia error_message.
func TestSolicitarCGIBSApuracaoHandler_ErroDeRede_GravaLinhaComErro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2026-01-01", "2026-01-31")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("esperava 502, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	var errorMessage sql.NullString
	if err := db.QueryRow(`
		SELECT COUNT(*), MAX(error_message) FROM cgibs_solicitacoes WHERE company_id=$1
	`, companyID).Scan(&count, &errorMessage); err != nil {
		t.Fatalf("erro ao consultar cgibs_solicitacoes: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava 1 solicitação gravada com a falha, obteve %d", count)
	}
	if !errorMessage.Valid || errorMessage.String == "" {
		t.Error("esperava error_message preenchido na linha gravada após falha de rede")
	}
}

// TestClearErrorsCGIBSHandler_RemoveLinhaComErro confirma que ClearErrorsCGIBSHandler de fato
// remove uma solicitação com error_message preenchido — antes do patch do item 2, nada gravava
// error_message, então este handler nunca tinha o que limpar (no-op morto).
func TestClearErrorsCGIBSHandler_RemoveLinhaComErro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	if _, err := db.Exec(`
		INSERT INTO cgibs_solicitacoes (company_id, cnpj_base, situacao_solicitacao, error_message)
		VALUES ($1, '12345678', 'solicitada', 'falha simulada de teste')
	`, companyID); err != nil {
		t.Fatalf("falha ao inserir solicitação de teste com erro: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/cgibs/apuracao/clear-errors", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	ClearErrorsCGIBSHandler(db)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM cgibs_solicitacoes WHERE company_id=$1 AND error_message IS NOT NULL`, companyID).Scan(&count)
	if count != 0 {
		t.Error("esperava que ClearErrorsCGIBSHandler removesse a solicitação com erro")
	}
}

// TestSolicitarCGIBSApuracaoHandler_SolicitacaoEmAndamento_Retorna409 cobre o boundary do item 3
// da revisão adversarial: uma 2ª chamada enquanto já existe uma solicitação 'solicitada' sem
// erro (ainda não resolvida pela CGIBS) deve ser recusada com 409, sem criar uma 2ª linha nem
// chamar a API de novo (protege contra duplo clique / solicitação repetida).
func TestSolicitarCGIBSApuracaoHandler_SolicitacaoEmAndamento_Retorna409(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)
	insertCGIBSSolicitacaoTeste(t, db, companyID, "solicitada", nil) // em andamento: sem error_message

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2026-01-01", "2026-01-31")
	if rec.Code != http.StatusConflict {
		t.Fatalf("esperava 409 (solicitação já em andamento), obteve %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("não deveria ter chamado a API CGIBS — já existe solicitação em andamento")
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM cgibs_solicitacoes WHERE company_id=$1`, companyID).Scan(&count)
	if count != 1 {
		t.Fatalf("esperava continuar com apenas 1 solicitação (a pré-existente), obteve %d", count)
	}
}

// TestSolicitarCGIBSApuracaoHandler_SolicitacaoAnteriorComErro_PermiteNova confirma o outro lado
// do boundary do item 3: uma solicitação anterior que já falhou (error_message preenchido) NÃO
// conta como "em andamento" — o usuário deve conseguir tentar de novo.
func TestSolicitarCGIBSApuracaoHandler_SolicitacaoAnteriorComErro_PermiteNova(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)

	if _, err := db.Exec(`
		INSERT INTO cgibs_solicitacoes (company_id, cnpj_base, situacao_solicitacao, error_message)
		VALUES ($1, '12345678', 'solicitada', 'falha anterior')
	`, companyID); err != nil {
		t.Fatalf("falha ao inserir solicitação de teste com erro: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"TokenContrib":        "token-x",
			"TipoSolicitacao":     "diferencial",
			"SituacaoSolicitacao": "solicitada",
			"IDSolicitacao":       888,
			"DataTransacaoIni":    "2026-01-01",
			"DataTransacaoFim":    "2026-01-31",
			"Resultado":           "sucesso",
		})
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2026-01-01", "2026-01-31")
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperava 201 (solicitação anterior com erro não bloqueia nova tentativa), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// insertCGIBSSolicitacaoTeste insere uma linha cgibs_solicitacoes de teste diretamente.
func insertCGIBSSolicitacaoTeste(t *testing.T, db *sql.DB, companyID, situacao string, idSolicitacaoExterno interface{}) (id string) {
	t.Helper()
	if err := db.QueryRow(`
		INSERT INTO cgibs_solicitacoes (company_id, cnpj_base, id_solicitacao_externo, situacao_solicitacao)
		VALUES ($1, '12345678', $2, $3)
		RETURNING id
	`, companyID, idSolicitacaoExterno, situacao).Scan(&id); err != nil {
		t.Fatalf("falha ao inserir cgibs_solicitacoes de teste: %v", err)
	}
	return id
}

// TestDetalheCGIBSHandler_Solicitada_ChamaCancelamentoAntesDeDeletar cobre o boundary da spec:
// uma solicitação 'solicitada' (ainda não gerada) com id_solicitacao_externo preenchido deve
// disparar CancelarSolicitacao antes do delete local.
func TestDetalheCGIBSHandler_Solicitada_ChamaCancelamentoAntesDeDeletar(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)

	var cancelCalled bool
	var capturedIDSolicitacao float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancelCalled = true
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		capturedIDSolicitacao, _ = body["IDSolicitacao"].(float64)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"Resultado": "cancelado"})
	}))
	defer srv.Close()
	t.Setenv("CGIBS_CANCELAMENTO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoTeste(t, db, companyID, "solicitada", 999)

	req := httptest.NewRequest(http.MethodDelete, "/api/cgibs/apuracao/"+solicitacaoID, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	DetalheCGIBSHandler(db)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperava 204, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if !cancelCalled {
		t.Fatal("esperava que CancelarSolicitacao fosse chamado (situação 'solicitada' com id_solicitacao_externo preenchido)")
	}
	if capturedIDSolicitacao != 999 {
		t.Errorf("esperava IDSolicitacao=999 na chamada de cancelamento, obteve %v", capturedIDSolicitacao)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM cgibs_solicitacoes WHERE id=$1`, solicitacaoID).Scan(&count)
	if count != 0 {
		t.Error("esperava que a solicitação tivesse sido deletada localmente")
	}
}

// TestDetalheCGIBSHandler_SemIDExterno_NaoChamaCancelamento cobre o outro lado do boundary:
// uma solicitação que nunca chegou a ser aceita pela CGIBS (id_solicitacao_externo NULL) é
// deletada local direto, sem tentar cancelar.
func TestDetalheCGIBSHandler_SemIDExterno_NaoChamaCancelamento(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"Resultado":"cancelado"}`))
	}))
	defer srv.Close()
	t.Setenv("CGIBS_CANCELAMENTO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoTeste(t, db, companyID, "solicitada", nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/cgibs/apuracao/"+solicitacaoID, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	DetalheCGIBSHandler(db)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperava 204, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("não deveria ter chamado CancelarSolicitacao — id_solicitacao_externo nunca foi preenchido")
	}
}

// TestDetalheCGIBSHandler_JaGerada_NaoChamaCancelamento cobre o outro lado do boundary:
// situacao_solicitacao já avançou para 'gerada' — delete local direto, sem tentar cancelar
// (a CGIBS já processou, não há mais o que cancelar).
func TestDetalheCGIBSHandler_JaGerada_NaoChamaCancelamento(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"Resultado":"cancelado"}`))
	}))
	defer srv.Close()
	t.Setenv("CGIBS_CANCELAMENTO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoTeste(t, db, companyID, "gerada", 555)

	req := httptest.NewRequest(http.MethodDelete, "/api/cgibs/apuracao/"+solicitacaoID, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	DetalheCGIBSHandler(db)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperava 204, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("não deveria ter chamado CancelarSolicitacao — situação já avançou para 'gerada'")
	}
}

// insertCGIBSOperacaoTeste insere uma linha cgibs_operacoes de teste diretamente.
func insertCGIBSOperacaoTeste(t *testing.T, db *sql.DB, companyID, chaveAcesso, cnpjFornecedor string) (operacaoID string) {
	t.Helper()
	if err := db.QueryRow(`
		INSERT INTO cgibs_operacoes (company_id, chave_acesso, cnpj_fornecedor)
		VALUES ($1, $2, $3)
		RETURNING id
	`, companyID, chaveAcesso, cnpjFornecedor).Scan(&operacaoID); err != nil {
		t.Fatalf("falha ao inserir cgibs_operacoes de teste: %v", err)
	}
	return operacaoID
}

// insertCGIBSLancamentoTeste insere uma linha cgibs_lancamentos de teste diretamente.
func insertCGIBSLancamentoTeste(t *testing.T, db *sql.DB, companyID, operacaoID string, lancamentoIDExterno int64, debitoEmAberto float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO cgibs_lancamentos (company_id, operacao_id, lancamento_id_externo, dth_lancto, debito_em_aberto)
		VALUES ($1, $2, $3, NOW(), $4)
	`, companyID, operacaoID, lancamentoIDExterno, debitoEmAberto); err != nil {
		t.Fatalf("falha ao inserir cgibs_lancamentos de teste: %v", err)
	}
}

func getOperacoesCGIBS(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/cgibs/operacoes", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	ListarOperacoesCGIBSHandler(db)(rec, req)
	return rec
}

func getLancamentosCGIBS(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, operacaoID string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/cgibs/operacoes/" + operacaoID + "/lancamentos"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	LancamentosOperacaoCGIBSHandler(db)(rec, req)
	return rec
}

// TestListarOperacoesCGIBSHandler_EscopadoPorEmpresa confirma que uma empresa não vê a
// operação (linha do ledger de conta corrente fiscal) de outra empresa.
func TestListarOperacoesCGIBSHandler_EscopadoPorEmpresa(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyA, authedA, cleanupA := setupCGIBSHabilitarTest(t, db)
	defer cleanupA()
	companyB, _, cleanupB := setupCGIBSHabilitarTest(t, db)
	defer cleanupB()

	insertCGIBSOperacaoTeste(t, db, companyA, strings.Repeat("1", 44), "11111111")
	insertCGIBSOperacaoTeste(t, db, companyB, strings.Repeat("2", 44), "22222222")

	recA := getOperacoesCGIBS(t, db, authedA)
	if recA.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recA.Code, recA.Body.String())
	}
	var bodyA map[string]interface{}
	if err := json.NewDecoder(recA.Body).Decode(&bodyA); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	operacoesA, _ := bodyA["operacoes"].([]interface{})
	if len(operacoesA) != 1 {
		t.Fatalf("esperava 1 operação para empresa A, obteve %d: %+v", len(operacoesA), operacoesA)
	}
	op, _ := operacoesA[0].(map[string]interface{})
	if op["chave_acesso"] != strings.Repeat("1", 44) {
		t.Errorf("empresa A viu operação que não é sua: %+v", op)
	}
}

// TestLancamentosOperacaoCGIBSHandler_EscopadoPorEmpresa confirma que uma empresa não consegue
// ler o extrato (lançamentos) de uma operação que pertence a outra empresa, mesmo sabendo o
// {id} — o escopo de tenant é confirmado pelo SQL (join com cgibs_operacoes filtrado por
// company_id), não pela URL.
func TestLancamentosOperacaoCGIBSHandler_EscopadoPorEmpresa(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyA, authedA, cleanupA := setupCGIBSHabilitarTest(t, db)
	defer cleanupA()
	_, authedB, cleanupB := setupCGIBSHabilitarTest(t, db)
	defer cleanupB()

	opA := insertCGIBSOperacaoTeste(t, db, companyA, strings.Repeat("3", 44), "33333333")
	insertCGIBSLancamentoTeste(t, db, companyA, opA, 1, 100.50)

	// Empresa B tenta ler o extrato da operação de A (usando o {id} real) — deve vir vazio.
	recB := getLancamentosCGIBS(t, db, authedB, opA)
	if recB.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recB.Code, recB.Body.String())
	}
	var bodyB map[string]interface{}
	if err := json.NewDecoder(recB.Body).Decode(&bodyB); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	lancB, _ := bodyB["lancamentos"].([]interface{})
	if len(lancB) != 0 {
		t.Fatalf("empresa B não deveria ver lançamentos da operação de empresa A, obteve %d", len(lancB))
	}

	// Empresa A lê normalmente.
	recA := getLancamentosCGIBS(t, db, authedA, opA)
	if recA.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recA.Code, recA.Body.String())
	}
	var bodyA map[string]interface{}
	if err := json.NewDecoder(recA.Body).Decode(&bodyA); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	lancA, _ := bodyA["lancamentos"].([]interface{})
	if len(lancA) != 1 {
		t.Fatalf("esperava 1 lançamento para empresa A, obteve %d", len(lancA))
	}
}

// TestLancamentosOperacaoCGIBSHandler_IDMalformado_Retorna400 cobre o item 5 da revisão
// adversarial: um {id} que não é UUID deve devolver 400 claro, não o erro cru do Postgres
// ("invalid input syntax for uuid") virando 500.
func TestLancamentosOperacaoCGIBSHandler_IDMalformado_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec := getLancamentosCGIBS(t, db, authed, "abc")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 para id malformado, obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDetalheCGIBSHandler_IDMalformado_Retorna400 cobre o mesmo boundary do item 5 no outro
// endpoint que recebe {id} pela URL (DELETE /api/cgibs/apuracao/{id}).
func TestDetalheCGIBSHandler_IDMalformado_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/cgibs/apuracao/abc", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	DetalheCGIBSHandler(db)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 para id malformado, obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// ─── Revisão adversarial #2 ─────────────────────────────────────────────────

// TestSolicitarCGIBSApuracaoHandler_ResultadoIndicaFalha_GravaErroNaoCriaSucesso cobre o item 1
// da 2ª revisão: um HTTP 2xx cujo campo Resultado indica falha de negócio (não contém "sucesso")
// não pode ser tratado como uma solicitação criada com êxito — deve gravar a falha (mesmo
// caminho já usado para falha de rede/HTTP) e responder 502, nunca 201.
func TestSolicitarCGIBSApuracaoHandler_ResultadoIndicaFalha_GravaErroNaoCriaSucesso(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"TokenContrib":        "token-x",
			"TipoSolicitacao":     "diferencial",
			"SituacaoSolicitacao": "solicitada",
			"IDSolicitacao":       0,
			"DataTransacaoIni":    "2026-01-01",
			"DataTransacaoFim":    "2026-01-31",
			"Resultado":           "CNPJ não habilitado para a API Apuração Assistida",
		})
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2026-01-01", "2026-01-31")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("esperava 502 (Resultado indica falha de negócio), obteve %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	var errorMessage sql.NullString
	if err := db.QueryRow(`
		SELECT COUNT(*), MAX(error_message) FROM cgibs_solicitacoes
		WHERE company_id=$1 AND id_solicitacao_externo IS NULL
	`, companyID).Scan(&count, &errorMessage); err != nil {
		t.Fatalf("erro ao consultar cgibs_solicitacoes: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava 1 solicitação gravada como falha (sem id_solicitacao_externo), obteve %d", count)
	}
	if !errorMessage.Valid || !strings.Contains(errorMessage.String, "não habilitado") {
		t.Errorf("esperava error_message com o texto de Resultado da CGIBS, obteve %q", errorMessage.String)
	}
}

// TestDetalheCGIBSHandler_Enviada_ChamaCancelamentoAntesDeDeletar cobre o item 2 da 2ª revisão:
// situacao_solicitacao='enviada' (não só 'solicitada') com id_solicitacao_externo preenchido
// também deve tentar o cancelamento remoto antes do delete local.
func TestDetalheCGIBSHandler_Enviada_ChamaCancelamentoAntesDeDeletar(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)

	var cancelCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancelCalled = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"Resultado": "cancelado"})
	}))
	defer srv.Close()
	t.Setenv("CGIBS_CANCELAMENTO_URL", srv.URL)

	solicitacaoID := insertCGIBSSolicitacaoTeste(t, db, companyID, "enviada", 1001)

	req := httptest.NewRequest(http.MethodDelete, "/api/cgibs/apuracao/"+solicitacaoID, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	DetalheCGIBSHandler(db)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperava 204, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if !cancelCalled {
		t.Fatal("esperava que CancelarSolicitacao fosse chamado para situação 'enviada' com id_solicitacao_externo preenchido")
	}
}

// insertCGIBSArquivoTeste insere uma linha cgibs_arquivos de teste vinculada a uma solicitação.
func insertCGIBSArquivoTeste(t *testing.T, db *sql.DB, companyID, solicitacaoID string, numeroSequencial int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO cgibs_arquivos (company_id, solicitacao_id, numero_sequencial, nome_arquivo)
		VALUES ($1, $2, $3, 'arquivo-teste.json')
	`, companyID, solicitacaoID, numeroSequencial); err != nil {
		t.Fatalf("falha ao inserir cgibs_arquivos de teste: %v", err)
	}
}

// TestDetalheCGIBSHandler_ComArquivoVinculado_Recusa409 cobre o item 3 da 2ª revisão: uma
// solicitação com pelo menos 1 arquivo vinculado (cgibs_arquivos) não pode ser deletada — o
// DELETE arrastaria via CASCADE o arquivo (e seu raw_json de auditoria) e, indiretamente, os
// dados já processados no ledger. Recusado com 409 independente da situação da solicitação.
func TestDetalheCGIBSHandler_ComArquivoVinculado_Recusa409(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	solicitacaoID := insertCGIBSSolicitacaoTeste(t, db, companyID, "gerada", 2002)
	insertCGIBSArquivoTeste(t, db, companyID, solicitacaoID, 1)

	req := httptest.NewRequest(http.MethodDelete, "/api/cgibs/apuracao/"+solicitacaoID, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	DetalheCGIBSHandler(db)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("esperava 409 (arquivo vinculado), obteve %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM cgibs_solicitacoes WHERE id=$1`, solicitacaoID).Scan(&count)
	if count != 1 {
		t.Error("esperava que a solicitação NÃO tivesse sido deletada — havia arquivo vinculado")
	}
}

// TestSolicitarCGIBSApuracaoHandler_DatasIncoerentesNaResposta_UsaParOriginal cobre o item 4 da
// 2ª revisão: se a CGIBS devolver DataTransacaoIni não parseável (vira fallback = dataIni
// pedido) e uma DataTransacaoFim válida porém anterior ao dataIni pedido, o par resultante
// violaria data_transacao_ini<=data_transacao_fim se só um dos dois fosse substituído — o
// handler deve descartar AMBOS os valores da CGIBS e gravar o par original pedido inteiro.
func TestSolicitarCGIBSApuracaoHandler_DatasIncoerentesNaResposta_UsaParOriginal(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"TokenContrib":        "token-x",
			"TipoSolicitacao":     "diferencial",
			"SituacaoSolicitacao": "solicitada",
			"IDSolicitacao":       3003,
			// Não reconhecível por parseCGIBSSolicitacaoDate -> vira zero -> fallback local
			// (dataIni pedido = 2026-02-01).
			"DataTransacaoIni": "não-é-uma-data-valida",
			// Válida, mas anterior ao dataIni pedido -- se só esta fosse usada (mantendo o
			// fallback acima para Ini), o par gravado violaria ini<=fim.
			"DataTransacaoFim": "2026-01-15",
			"Resultado":        "sucesso",
		})
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2026-02-01", "2026-02-28")
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperava 201, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var dataIni, dataFim time.Time
	if err := db.QueryRow(`
		SELECT data_transacao_ini, data_transacao_fim FROM cgibs_solicitacoes
		WHERE company_id=$1 AND id_solicitacao_externo=3003
	`, companyID).Scan(&dataIni, &dataFim); err != nil {
		t.Fatalf("erro ao consultar cgibs_solicitacoes: %v", err)
	}
	wantIni := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	wantFim := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
	if !dataIni.Equal(wantIni) || !dataFim.Equal(wantFim) {
		t.Errorf("esperava o par original pedido (ini=%v fim=%v), obteve ini=%v fim=%v — a mistura de datas da CGIBS não foi descartada",
			wantIni, wantFim, dataIni, dataFim)
	}
}

// TestSolicitarCGIBSApuracaoHandler_PeriodoMaiorQueUmAno_Retorna400 cobre o item 6 da 2ª
// revisão: um período maior que 1 ano é rejeitado antes de qualquer chamada à API externa.
func TestSolicitarCGIBSApuracaoHandler_PeriodoMaiorQueUmAno_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2020-01-01", "2022-01-02")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (período > 1 ano), obteve %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("não deveria ter chamado a API CGIBS — período maior que 1 ano deve ser rejeitado antes")
	}
}

// TestSolicitarCGIBSApuracaoHandler_DataFimNoFuturo_Retorna400 cobre o outro lado do item 6:
// data_fim distante no futuro é rejeitada antes de qualquer chamada à API externa.
func TestSolicitarCGIBSApuracaoHandler_DataFimNoFuturo_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	dataIni := time.Now().Format("2006-01-02")
	dataFim := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	rec := postSolicitarCGIBS(t, db, authed, dataIni, dataFim)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (data_fim no futuro distante), obteve %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("não deveria ter chamado a API CGIBS — data_fim no futuro distante deve ser rejeitada antes")
	}
}

// TestSolicitarCGIBSApuracaoHandler_ColisaoIDSolicitacaoExterno_Retorna409 cobre o item 7 da 2ª
// revisão: se o IDSolicitacao devolvido pela CGIBS já existir localmente (colisão em
// uq_cgibs_solicitacoes_externo), o handler deve responder 409 claro, não um 500 cru de
// constraint, e não deve duplicar a linha.
func TestSolicitarCGIBSApuracaoHandler_ColisaoIDSolicitacaoExterno_Retorna409(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, true)
	// Pré-existente com situação 'gerada' — não conta como "em andamento" (que exige
	// situacao_solicitacao='solicitada'), então não bloqueia via o 409 do item 3 da 1ª revisão.
	insertCGIBSSolicitacaoTeste(t, db, companyID, "gerada", 5555)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"TokenContrib":        "token-x",
			"TipoSolicitacao":     "diferencial",
			"SituacaoSolicitacao": "solicitada",
			"IDSolicitacao":       5555,
			"DataTransacaoIni":    "2026-01-01",
			"DataTransacaoFim":    "2026-01-31",
			"Resultado":           "sucesso",
		})
	}))
	defer srv.Close()
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	rec := postSolicitarCGIBS(t, db, authed, "2026-01-01", "2026-01-31")
	if rec.Code != http.StatusConflict {
		t.Fatalf("esperava 409 (colisão de id_solicitacao_externo), obteve %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM cgibs_solicitacoes WHERE company_id=$1 AND id_solicitacao_externo=5555`, companyID).Scan(&count)
	if count != 1 {
		t.Fatalf("esperava continuar com apenas 1 linha para id_solicitacao_externo=5555, obteve %d", count)
	}
}
