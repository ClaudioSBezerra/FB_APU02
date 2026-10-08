package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// insertRfbResumoTeste insere uma linha rfb_resumo de teste (requer um rfb_requests
// associado, FK obrigatória — mesmo padrão de insertRfbCredito em
// rfb_pagamentos_fornecedores_test.go).
func insertRfbResumoTeste(t *testing.T, db *sql.DB, companyID, periodo string, valorCBSNaoExtinto float64) {
	t.Helper()
	insertRfbResumoCompletoTeste(t, db, companyID, periodo, valorCBSNaoExtinto, 0, 0)
}

// insertRfbResumoCompletoTeste insere uma linha rfb_resumo de teste com total_debitos e
// valor_cbs_total também preenchidos — usado pelos testes dos campos novos (ver spec
// apuracao-cbs-dashboard-real.md).
func insertRfbResumoCompletoTeste(t *testing.T, db *sql.DB, companyID, periodo string, valorCBSNaoExtinto float64, totalDebitos int, valorCBSTotal float64) {
	t.Helper()
	var requestID string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, status, tipo) VALUES ($1, '12345678', 'completed', 'debito') RETURNING id
	`, companyID).Scan(&requestID); err != nil {
		t.Fatalf("falha ao criar rfb_requests de teste: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO rfb_resumo (request_id, company_id, data_apuracao, valor_cbs_nao_extinto, total_debitos, valor_cbs_total)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, requestID, companyID, periodo, valorCBSNaoExtinto, totalDebitos, valorCBSTotal); err != nil {
		t.Fatalf("falha ao inserir rfb_resumo de teste: %v", err)
	}
}

// insertRfbCreditosResumoTeste insere uma linha rfb_creditos_resumo de teste.
func insertRfbCreditosResumoTeste(t *testing.T, db *sql.DB, companyID, periodo string, valorCBSExtinto float64) {
	t.Helper()
	insertRfbCreditosResumoCompletoTeste(t, db, companyID, periodo, valorCBSExtinto, 0, 0)
}

// insertRfbCreditosResumoCompletoTeste insere uma linha rfb_creditos_resumo de teste com
// total_creditos e valor_cbs_total também preenchidos — usado pelos testes dos campos novos
// (ver spec apuracao-cbs-dashboard-real.md).
func insertRfbCreditosResumoCompletoTeste(t *testing.T, db *sql.DB, companyID, periodo string, valorCBSExtinto float64, totalCreditos int, valorCBSTotal float64) {
	t.Helper()
	var requestID string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, status, tipo) VALUES ($1, '12345678', 'completed', 'credito') RETURNING id
	`, companyID).Scan(&requestID); err != nil {
		t.Fatalf("falha ao criar rfb_requests de teste: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO rfb_creditos_resumo (request_id, company_id, data_apuracao, valor_cbs_extinto, total_creditos, valor_cbs_total)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, requestID, companyID, periodo, valorCBSExtinto, totalCreditos, valorCBSTotal); err != nil {
		t.Fatalf("falha ao inserir rfb_creditos_resumo de teste: %v", err)
	}
}

func getResumoMensalRFB(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, periodo string) *httptest.ResponseRecorder {
	t.Helper()
	path := fmt.Sprintf("/api/rfb/resumo-mensal?periodo=%s", url.QueryEscape(periodo))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	RFBResumoMensalHandler(db)(rec, req)
	return rec
}

// TestRFBResumoMensalHandler_ComAmbasAsLinhas_CalculaSaldoAproximado cobre o caso feliz:
// rfb_resumo e rfb_creditos_resumo preenchidos para o período — saldo_a_pagar_aproximado =
// valor_cbs_nao_extinto - valor_cbs_extinto, aproximado sempre true.
func TestRFBResumoMensalHandler_ComAmbasAsLinhas_CalculaSaldoAproximado(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	defer db.Exec(`DELETE FROM rfb_resumo WHERE company_id = $1`, companyID)
	defer db.Exec(`DELETE FROM rfb_creditos_resumo WHERE company_id = $1`, companyID)
	defer db.Exec(`DELETE FROM rfb_requests WHERE company_id = $1`, companyID)

	insertRfbResumoCompletoTeste(t, db, companyID, "03/2026", 1000.00, 12, 2500.00)
	insertRfbCreditosResumoCompletoTeste(t, db, companyID, "03/2026", 300.00, 5, 1800.00)

	rec := getResumoMensalRFB(t, db, authed, "03/2026")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var resp RFBResumoMensal
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	if !resp.Aproximado {
		t.Errorf("esperava aproximado=true, obteve false")
	}
	if resp.SaldoAPagarAproximado != 700.00 {
		t.Errorf("esperava saldo_a_pagar_aproximado=700.00 (1000-300), obteve %.2f", resp.SaldoAPagarAproximado)
	}
	if resp.StatusPeriodo == "" {
		t.Errorf("esperava status_periodo preenchido")
	}
	if resp.DataFechamento == "" {
		t.Errorf("esperava data_fechamento preenchida")
	}
	if resp.TotalDebitos != 12 {
		t.Errorf("esperava total_debitos=12, obteve %d", resp.TotalDebitos)
	}
	if resp.TotalCreditos != 5 {
		t.Errorf("esperava total_creditos=5, obteve %d", resp.TotalCreditos)
	}
	if resp.ValorCBSTotalDebito != 2500.00 {
		t.Errorf("esperava valor_cbs_total_debito=2500.00, obteve %.2f", resp.ValorCBSTotalDebito)
	}
	if resp.ValorCBSTotalCredito != 1800.00 {
		t.Errorf("esperava valor_cbs_total_credito=1800.00, obteve %.2f", resp.ValorCBSTotalCredito)
	}
}

// TestRFBResumoMensalHandler_SemLinhaDeCreditos_NaoQuebra cobre a Acceptance Criteria #3
// da spec: empresa sem nenhuma linha em rfb_creditos_resumo pro período — não quebra
// (COALESCE 0) e aproximado continua true.
func TestRFBResumoMensalHandler_SemLinhaDeCreditos_NaoQuebra(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	defer db.Exec(`DELETE FROM rfb_resumo WHERE company_id = $1`, companyID)
	defer db.Exec(`DELETE FROM rfb_requests WHERE company_id = $1`, companyID)

	insertRfbResumoCompletoTeste(t, db, companyID, "04/2026", 500.00, 8, 900.00)
	// Nenhuma linha em rfb_creditos_resumo para 04/2026.

	rec := getResumoMensalRFB(t, db, authed, "04/2026")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var resp RFBResumoMensal
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	if !resp.Aproximado {
		t.Errorf("esperava aproximado=true mesmo sem linha de créditos, obteve false")
	}
	if resp.SaldoAPagarAproximado != 500.00 {
		t.Errorf("esperava saldo_a_pagar_aproximado=500.00 (500-0 via COALESCE), obteve %.2f", resp.SaldoAPagarAproximado)
	}
	// Campos novos do lado de débito vêm da linha existente.
	if resp.TotalDebitos != 8 {
		t.Errorf("esperava total_debitos=8, obteve %d", resp.TotalDebitos)
	}
	if resp.ValorCBSTotalDebito != 900.00 {
		t.Errorf("esperava valor_cbs_total_debito=900.00, obteve %.2f", resp.ValorCBSTotalDebito)
	}
	// Campos novos do lado de crédito ficam em zero — não há linha em rfb_creditos_resumo
	// para este período (Acceptance Criteria #3 da spec, estendida aos campos novos).
	if resp.TotalCreditos != 0 {
		t.Errorf("esperava total_creditos=0 (sem linha de créditos), obteve %d", resp.TotalCreditos)
	}
	if resp.ValorCBSTotalCredito != 0 {
		t.Errorf("esperava valor_cbs_total_credito=0 (sem linha de créditos), obteve %.2f", resp.ValorCBSTotalCredito)
	}
}

// TestRFBResumoMensalHandler_SemNenhumaLinha_RetornaZeros cobre o caso totalmente vazio
// (nem rfb_resumo nem rfb_creditos_resumo) — não deve quebrar.
func TestRFBResumoMensalHandler_SemNenhumaLinha_RetornaZeros(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec := getResumoMensalRFB(t, db, authed, "05/2026")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var resp RFBResumoMensal
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	if resp.SaldoAPagarAproximado != 0 {
		t.Errorf("esperava saldo_a_pagar_aproximado=0, obteve %.2f", resp.SaldoAPagarAproximado)
	}
	if !resp.Aproximado {
		t.Errorf("esperava aproximado=true")
	}
	if resp.TotalDebitos != 0 || resp.TotalCreditos != 0 {
		t.Errorf("esperava total_debitos=0 e total_creditos=0, obteve %d/%d", resp.TotalDebitos, resp.TotalCreditos)
	}
	if resp.ValorCBSTotalDebito != 0 || resp.ValorCBSTotalCredito != 0 {
		t.Errorf("esperava valor_cbs_total_debito=0 e valor_cbs_total_credito=0, obteve %.2f/%.2f", resp.ValorCBSTotalDebito, resp.ValorCBSTotalCredito)
	}
}

// TestRFBResumoMensalHandler_PeriodoInvalido_Retorna400 confirma a validação de formato
// ANTES de usar o valor na query (regex ^(0[1-9]|1[0-2])/[0-9]{4}$).
func TestRFBResumoMensalHandler_PeriodoInvalido_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	casos := []string{"2026-03", "13/2026", "00/2026", "1/2026", "03/26", "", "03/2026; DROP TABLE rfb_resumo;"}
	for _, periodo := range casos {
		rec := getResumoMensalRFB(t, db, authed, periodo)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("periodo=%q: esperava 400, obteve %d: %s", periodo, rec.Code, rec.Body.String())
		}
	}
}
