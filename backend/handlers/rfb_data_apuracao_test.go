package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Testes dos pontos de leitura de data_apuracao traduzidos na migration 129: o dado
// é persistido em "mm/aaaa" mas o contrato externo da API continua "AAAAMM"
// (periodo) / "AAAA" (ano). Ver spec-rfb-cbs-v2-schema.md.

// setupRFBLeituraTest cria empresa + usuário dono (para GetEffectiveCompanyID) + um
// rfb_requests, e devolve o contexto de autenticação pronto para os handlers.
func setupRFBLeituraTest(t *testing.T, db *sql.DB) (companyID, requestID string, authed func(*http.Request) *http.Request, cleanup func()) {
	t.Helper()
	companyID, cleanupCompany := setupTestCompany(t, db)

	var userID string
	email := fmt.Sprintf("rfb-leitura-%d@teste.local", time.Now().UnixNano())
	if err := db.QueryRow(`INSERT INTO users (email, password_hash, full_name) VALUES ($1, 'x', 'teste') RETURNING id`, email).Scan(&userID); err != nil {
		cleanupCompany()
		t.Fatalf("falha ao criar user de teste: %v", err)
	}
	if _, err := db.Exec(`UPDATE companies SET owner_id = $1 WHERE id = $2`, userID, companyID); err != nil {
		t.Fatalf("falha ao definir owner da company de teste: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO rfb_requests (company_id, cnpj_base, status) VALUES ($1, '12345678', 'completed') RETURNING id`, companyID).Scan(&requestID); err != nil {
		t.Fatalf("falha ao criar rfb_requests de teste: %v", err)
	}

	authed = func(r *http.Request) *http.Request {
		ctx := context.WithValue(r.Context(), ClaimsKey, jwt.MapClaims{"user_id": userID})
		r = r.WithContext(ctx)
		r.Header.Set("X-Company-ID", companyID)
		return r
	}
	cleanup = func() {
		db.Exec(`DELETE FROM rfb_debitos WHERE company_id = $1`, companyID)
		db.Exec(`DELETE FROM rfb_requests WHERE company_id = $1`, companyID)
		cleanupCompany()
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}
	return
}

func insertDebitoLeitura(t *testing.T, db *sql.DB, requestID, companyID, tipo, chave, dataApuracao string, valor float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO rfb_debitos (request_id, company_id, tipo_apuracao, chave_dfe, data_apuracao, valor_cbs_total)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, requestID, companyID, tipo, chave, dataApuracao, valor); err != nil {
		t.Fatalf("falha ao inserir rfb_debitos de teste: %v", err)
	}
}

func chavesDebitos(t *testing.T, body []byte) []string {
	t.Helper()
	var resp struct {
		Debitos []struct {
			ChaveDfe     string `json:"chave_dfe"`
			DataApuracao string `json:"data_apuracao"`
		} `json:"debitos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("resposta inválida: %v (%s)", err, body)
	}
	out := make([]string, 0, len(resp.Debitos))
	for _, d := range resp.Debitos {
		out = append(out, d.ChaveDfe)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListarDebitosHandler_FiltroPeriodoEAnoTraduzemMMAAAA(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, requestID, authed, cleanup := setupRFBLeituraTest(t, db)
	defer cleanup()

	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "D-2026-01", "01/2026", 10)
	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "D-2026-03", "03/2026", 10)
	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "D-2025-12", "12/2025", 10)

	call := func(query string) []string {
		req := authed(httptest.NewRequest(http.MethodGet, "/api/rfb/debitos?"+query, nil))
		rr := httptest.NewRecorder()
		ListarDebitosHandler(db)(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}
		return chavesDebitos(t, rr.Body.Bytes())
	}

	// periodo AAAAMM encontra a linha "mm/aaaa"
	if got := call("periodo=202601"); !equalStrings(got, []string{"D-2026-01"}) {
		t.Fatalf("periodo=202601: esperava [D-2026-01], veio %v", got)
	}
	// ano AAAA pega só as linhas de 2026, ordenadas cronologicamente DESC (03/2026 antes de 01/2026)
	if got := call("ano=2026"); !equalStrings(got, []string{"D-2026-03", "D-2026-01"}) {
		t.Fatalf("ano=2026: esperava [D-2026-03 D-2026-01], veio %v", got)
	}
	// sem filtro: ORDER BY cronológico cross-year (12/2025 vem DEPOIS de 01/2026 em DESC —
	// ordenar a string "mm/aaaa" cru colocaria "12/2025" primeiro).
	if got := call(""); !equalStrings(got, []string{"D-2026-03", "D-2026-01", "D-2025-12"}) {
		t.Fatalf("sem filtro: esperava ordem cronológica DESC [D-2026-03 D-2026-01 D-2025-12], veio %v", got)
	}
}

func TestPeriodosDebitosHandler_RetornaAAAAMMEmOrdemCronologicaDesc(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, requestID, authed, cleanup := setupRFBLeituraTest(t, db)
	defer cleanup()

	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "P-1", "12/2025", 1)
	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "P-2", "01/2026", 1)
	insertDebitoLeitura(t, db, requestID, companyID, "extemporaneo", "P-3", "01/2026", 1) // duplicado de período

	req := authed(httptest.NewRequest(http.MethodGet, "/api/rfb/debitos/periodos", nil))
	rr := httptest.NewRecorder()
	PeriodosDebitosHandler(db)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Periodos []string `json:"periodos"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !equalStrings(resp.Periodos, []string{"202601", "202512"}) {
		t.Fatalf("esperava [202601 202512] (AAAAMM, cronológico DESC cross-year, distinct), veio %v", resp.Periodos)
	}
}

func TestDetalheApuracaoHandler_OrdenaPorDataApuracaoCronologica(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, requestID, authed, cleanup := setupRFBLeituraTest(t, db)
	defer cleanup()

	// mesmo tipo_apuracao: desempate é a data_apuracao ASC. "12/2025" < "01/2026" cronologicamente,
	// mas como string "01/2026" < "12/2025".
	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "DET-2026-01", "01/2026", 1)
	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "DET-2025-12", "12/2025", 1)

	req := authed(httptest.NewRequest(http.MethodGet, "/api/rfb/apuracao/"+requestID, nil))
	rr := httptest.NewRecorder()
	DetalheApuracaoHandler(db)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if got := chavesDebitos(t, rr.Body.Bytes()); !equalStrings(got, []string{"DET-2025-12", "DET-2026-01"}) {
		t.Fatalf("esperava ordem cronológica ASC [DET-2025-12 DET-2026-01], veio %v", got)
	}
}

func TestListarCreditosHandler_FiltroPeriodoTraduzMMAAAA(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, requestID, authed, cleanup := setupRFBLeituraTest(t, db)
	defer cleanup()
	defer db.Exec(`DELETE FROM rfb_creditos WHERE company_id = $1`, companyID)

	for chave, pa := range map[string]string{"C-2026-01": "01/2026", "C-2025-12": "12/2025"} {
		if _, err := db.Exec(`
			INSERT INTO rfb_creditos (request_id, company_id, tipo_apuracao, modelo_dfe, numero_dfe, chave_dfe,
				data_dfe_emissao, data_apuracao, ni_emitente, ni_adquirente,
				valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto, situacao_credito)
			VALUES ($1, $2, 'corrente', '55', '1', $3, '2026-01-10', $4, '', '', 5, 0, 5, 'X')
		`, requestID, companyID, chave, pa); err != nil {
			t.Fatalf("falha ao inserir rfb_creditos de teste: %v", err)
		}
	}

	req := authed(httptest.NewRequest(http.MethodGet, "/api/rfb/creditos?periodo=202601", nil))
	rr := httptest.NewRecorder()
	ListarCreditosHandler(db)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Creditos []struct {
			ChaveDfe     string `json:"chave_dfe"`
			DataApuracao string `json:"data_apuracao"`
		} `json:"creditos"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || len(resp.Creditos) != 1 || resp.Creditos[0].ChaveDfe != "C-2026-01" {
		t.Fatalf("periodo=202601: esperava só C-2026-01, veio total=%d %+v", resp.Total, resp.Creditos)
	}
}

// (ii) data_apuracao inválida no banco ('13/2026') não derruba lista/períodos com 500 e não aparece em períodos.
func TestRFBLeitura_PeriodoInvalidoNoBancoNaoQuebra(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, requestID, authed, cleanup := setupRFBLeituraTest(t, db)
	defer cleanup()

	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "OK-1", "01/2026", 1)
	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "BAD-1", "13/2026", 1)
	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "BAD-2", "00/2026", 1)

	req := authed(httptest.NewRequest(http.MethodGet, "/api/rfb/debitos/periodos", nil))
	rr := httptest.NewRecorder()
	PeriodosDebitosHandler(db)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("periodos: status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Periodos []string `json:"periodos"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !equalStrings(resp.Periodos, []string{"202601"}) {
		t.Fatalf("esperava só [202601], veio %v", resp.Periodos)
	}

	for _, q := range []string{"", "periodo=202601", "ano=2026"} {
		req := authed(httptest.NewRequest(http.MethodGet, "/api/rfb/debitos?"+q, nil))
		rr := httptest.NewRecorder()
		ListarDebitosHandler(db)(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("lista (%q): status %d: %s", q, rr.Code, rr.Body.String())
		}
		got := chavesDebitos(t, rr.Body.Bytes())
		if q == "" {
			// NULLS LAST: período malformado não sobe pro topo
			if len(got) != 3 || got[0] != "OK-1" {
				t.Fatalf("sem filtro: esperava OK-1 primeiro, veio %v", got)
			}
		} else if !equalStrings(got, []string{"OK-1"}) {
			t.Fatalf("lista (%q): esperava [OK-1], veio %v", q, got)
		}
	}
}

// (iv) periodo=2026-01 (com separador) encontra a linha '01/2026'.
func TestListarDebitosHandler_FiltroPeriodoAceitaHifen(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, requestID, authed, cleanup := setupRFBLeituraTest(t, db)
	defer cleanup()

	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "H-1", "01/2026", 1)
	insertDebitoLeitura(t, db, requestID, companyID, "corrente", "H-2", "02/2026", 1)

	req := authed(httptest.NewRequest(http.MethodGet, "/api/rfb/debitos?periodo=2026-01", nil))
	rr := httptest.NewRecorder()
	ListarDebitosHandler(db)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if got := chavesDebitos(t, rr.Body.Bytes()); !equalStrings(got, []string{"H-1"}) {
		t.Fatalf("periodo=2026-01: esperava [H-1], veio %v", got)
	}
}
