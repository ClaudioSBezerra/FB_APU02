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

// insertCGIBSLancamentoComData insere um lançamento de teste com dth_lancto e os 7 valores de
// conta corrente fiscal controlados explicitamente — insertCGIBSLancamentoTeste (já existente
// em cgibs_apuracao_test.go) fixa dth_lancto=NOW() e só permite controlar debito_em_aberto, o
// que não serve para testar o filtro por período nem a soma dos outros 6 campos.
func insertCGIBSLancamentoComData(t *testing.T, db *sql.DB, companyID, operacaoID string, lancamentoIDExterno int64, dthLancto time.Time, recursoDisp, recursoATransferir, creditoApropriar, creditoNaoUtil, creditoUtil, debitoAberto, debitoExtinto float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO cgibs_lancamentos (
			company_id, operacao_id, lancamento_id_externo, dth_lancto,
			recurso_financeiro_disponivel, recurso_financeiro_a_transferir,
			credito_a_apropriar, credito_nao_utilizado, credito_utilizado,
			debito_em_aberto, debito_extinto
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, companyID, operacaoID, lancamentoIDExterno, dthLancto,
		recursoDisp, recursoATransferir, creditoApropriar, creditoNaoUtil, creditoUtil, debitoAberto, debitoExtinto,
	); err != nil {
		t.Fatalf("falha ao inserir cgibs_lancamentos de teste: %v", err)
	}
}

func getResumoCGIBS(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, dataIni, dataFim string) *httptest.ResponseRecorder {
	t.Helper()
	path := fmt.Sprintf("/api/cgibs/resumo?data_ini=%s&data_fim=%s", dataIni, dataFim)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	ResumoCGIBSHandler(db)(rec, req)
	return rec
}

// TestResumoCGIBSHandler_AgregaLancamentosDoPeriodo cobre o item 5 da 2ª revisão: o resumo
// agregado por período deve bater com a soma esperada de 2+ lançamentos de teste dentro do
// período consultado, e ignorar lançamentos fora dele.
func TestResumoCGIBSHandler_AgregaLancamentosDoPeriodo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	op1 := insertCGIBSOperacaoTeste(t, db, companyID, strings.Repeat("1", 44), "11111111")
	op2 := insertCGIBSOperacaoTeste(t, db, companyID, strings.Repeat("2", 44), "22222222")

	dentro1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	dentro2 := time.Date(2026, 3, 20, 15, 0, 0, 0, time.UTC)
	// Bem depois do fim do período, sem ambiguidade de fuso horário: dth_lancto é
	// TIMESTAMP WITH TIME ZONE e o filtro usa $3::date (avaliado no fuso da sessão —
	// America/Sao_Paulo neste projeto, ver CLAUDE.md), então um instante a poucas horas da
	// meia-noite UTC poderia cair de um lado ou outro dependendo do fuso da sessão. Alguns
	// dias de folga elimina essa ambiguidade no teste.
	fora := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)

	insertCGIBSLancamentoComData(t, db, companyID, op1, 1, dentro1, 100.00, 10.00, 5.00, 1.00, 4.00, 50.00, 20.00)
	insertCGIBSLancamentoComData(t, db, companyID, op2, 2, dentro2, 200.00, 20.00, 15.00, 2.00, 13.00, 75.00, 30.00)
	insertCGIBSLancamentoComData(t, db, companyID, op1, 3, fora, 9999.00, 9999.00, 9999.00, 9999.00, 9999.00, 9999.00, 9999.00)

	rec := getResumoCGIBS(t, db, authed, "2026-03-01", "2026-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var resumo CGIBSResumoPeriodo
	if err := json.NewDecoder(rec.Body).Decode(&resumo); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}

	if resumo.TotalOperacoes != 2 {
		t.Errorf("esperava total_operacoes=2 (op1 e op2 têm lançamento dentro do período), obteve %d", resumo.TotalOperacoes)
	}
	checks := map[string]struct{ got, want float64 }{
		"recurso_financeiro_disponivel":   {resumo.RecursoFinanceiroDisponivel, 300.00},
		"recurso_financeiro_a_transferir": {resumo.RecursoFinanceiroATransferir, 30.00},
		"credito_a_apropriar":             {resumo.CreditoAApropriar, 20.00},
		"credito_nao_utilizado":           {resumo.CreditoNaoUtilizado, 3.00},
		"credito_utilizado":               {resumo.CreditoUtilizado, 17.00},
		"debito_em_aberto":                {resumo.DebitoEmAberto, 125.00},
		"debito_extinto":                  {resumo.DebitoExtinto, 50.00},
	}
	for campo, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: esperava %.2f, obteve %.2f (lançamento fora do período não deveria ter sido somado)", campo, c.want, c.got)
		}
	}
}

// TestResumoCGIBSHandler_PeriodoSemLancamentos_RetornaZeros cobre o item 5 da 2ª revisão: um
// período sem nenhum lançamento deve devolver zeros (nunca NULL/erro) — mesmo princípio já
// usado na agregação SQL da RFB (COALESCE em todo SUM).
func TestResumoCGIBSHandler_PeriodoSemLancamentos_RetornaZeros(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	op1 := insertCGIBSOperacaoTeste(t, db, companyID, strings.Repeat("9", 44), "99999999")
	insertCGIBSLancamentoComData(t, db, companyID, op1, 1, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), 1, 1, 1, 1, 1, 1, 1)

	rec := getResumoCGIBS(t, db, authed, "2026-06-01", "2026-06-30")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var resumo CGIBSResumoPeriodo
	if err := json.NewDecoder(rec.Body).Decode(&resumo); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	if resumo.TotalOperacoes != 0 || resumo.RecursoFinanceiroDisponivel != 0 || resumo.DebitoEmAberto != 0 {
		t.Errorf("esperava todos os campos zerados para período sem lançamentos, obteve %+v", resumo)
	}
}

// TestResumoCGIBSHandler_AlertaDecadencia_LancamentoAntigoDisparaAlerta cobre a Task 2/
// Acceptance #2 da spec-apuracao-mensal-cbs-ibs.md: uma operação com saldo de
// credito_nao_utilizado > 0 cujo lançamento mais antigo tem mais de 4 anos deve aparecer em
// alertas_decadencia — mesmo quando esse lançamento está fora do range data_ini/data_fim
// pedido (a query de alertas não usa esse filtro, ver comentário de alertasDecadenciaCGIBS).
func TestResumoCGIBSHandler_AlertaDecadencia_LancamentoAntigoDisparaAlerta(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	chaveAntiga := strings.Repeat("7", 44)
	opAntiga := insertCGIBSOperacaoTeste(t, db, companyID, chaveAntiga, "77777777")
	// Lançamento de 4 anos e meio atrás, com credito_nao_utilizado positivo — dispara o
	// alerta (mais de 4 anos, dentro da janela de antecedência de 1 ano antes dos 5).
	antigo := time.Now().AddDate(-4, -6, 0)
	insertCGIBSLancamentoComData(t, db, companyID, opAntiga, 1, antigo, 0, 0, 0, 50.00, 0, 0, 0)

	chaveRecente := strings.Repeat("8", 44)
	opRecente := insertCGIBSOperacaoTeste(t, db, companyID, chaveRecente, "88888888")
	// Lançamento recente com saldo positivo — não deve aparecer no alerta.
	insertCGIBSLancamentoComData(t, db, companyID, opRecente, 1, time.Now().AddDate(0, -1, 0), 0, 0, 0, 30.00, 0, 0, 0)

	chaveZerada := strings.Repeat("9", 44)
	opZerada := insertCGIBSOperacaoTeste(t, db, companyID, chaveZerada, "99999999")
	// Lançamento antigo, mas saldo líquido de credito_nao_utilizado é zero (apropriação
	// seguida de utilização total) — não deve alertar (saldo real atual não é positivo).
	bemAntigo := time.Now().AddDate(-5, 0, 0)
	insertCGIBSLancamentoComData(t, db, companyID, opZerada, 1, bemAntigo, 0, 0, 0, 40.00, 0, 0, 0)
	insertCGIBSLancamentoComData(t, db, companyID, opZerada, 2, time.Now().AddDate(0, -1, 0), 0, 0, 0, -40.00, 0, 0, 0)

	// Período pedido não inclui nenhum dos lançamentos acima — confirma que a query de
	// alertas independe do range data_ini/data_fim.
	rec := getResumoCGIBS(t, db, authed, "2026-01-01", "2026-01-02")
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var resumo CGIBSResumoPeriodo
	if err := json.NewDecoder(rec.Body).Decode(&resumo); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}

	achouAntiga, achouRecente, achouZerada := false, false, false
	for _, a := range resumo.AlertasDecadencia {
		switch a.ChaveAcesso {
		case chaveAntiga:
			achouAntiga = true
			if a.Valor != 50.00 {
				t.Errorf("esperava valor=50.00 para a operação antiga, obteve %.2f", a.Valor)
			}
			// Lançamento com 4 anos e 6 meses de idade: faltam ~6 meses (~180 dias) para
			// completar os 5 anos de decadência — deve ser positivo e dentro da janela de
			// alerta de 1 ano de antecedência (0 < dias_restantes <= ~365).
			if a.DiasRestantes <= 0 || a.DiasRestantes > 366 {
				t.Errorf("esperava dias_restantes positivo e dentro de ~1 ano (lançamento com 4 anos e 6 meses, faltam ~6 meses para os 5 anos), obteve %d", a.DiasRestantes)
			}
		case chaveRecente:
			achouRecente = true
		case chaveZerada:
			achouZerada = true
		}
	}
	if !achouAntiga {
		t.Errorf("esperava operação com lançamento de >4 anos e saldo positivo em alertas_decadencia, obteve %+v", resumo.AlertasDecadencia)
	}
	if achouRecente {
		t.Errorf("BUG: operação só com lançamento recente não deveria aparecer em alertas_decadencia")
	}
	if achouZerada {
		t.Errorf("BUG: operação com saldo líquido zerado (apesar do lançamento antigo) não deveria aparecer em alertas_decadencia")
	}
}

// TestResumoCGIBSHandler_DataInvalida_Retorna400 confirma a validação básica de formato.
func TestResumoCGIBSHandler_DataInvalida_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec := getResumoCGIBS(t, db, authed, "01-03-2026", "2026-03-31")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 para data_ini malformada, obteve %d: %s", rec.Code, rec.Body.String())
	}
}
