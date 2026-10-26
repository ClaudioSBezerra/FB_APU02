package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Helpers — NFS-e como 4ª fonte do Painel de Apuração (spec-nfse-apuracao-ibscbs.md)
// ---------------------------------------------------------------------------

// openApuracaoNFSeTestDB abre o Postgres de teste e pula se as colunas IBS/CBS da NFS-e
// (migration 135) não existirem.
func openApuracaoNFSeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'nfse_saidas' AND column_name = 'ibs_total')`).Scan(&exists); err != nil || !exists {
		db.Close()
		t.Skip("coluna nfse_saidas.ibs_total não existe — rode as migrations 133-135 antes deste teste")
	}
	return db
}

// setupApuracaoNFSeTest cria empresa + usuário dono (GetEffectiveCompanyID) e devolve o
// wrapper que injeta ClaimsKey + X-Company-ID — mesmo padrão de setupNFSeCertTest, sem a
// parte de certificado.
func setupApuracaoNFSeTest(t *testing.T, db *sql.DB) (companyID string, authed func(*http.Request) *http.Request, cleanup func()) {
	t.Helper()
	companyID, cleanupCompany := setupTestCompany(t, db)

	var userID string
	email := fmt.Sprintf("apuracao-nfse-%d@teste.local", time.Now().UnixNano())
	if err := db.QueryRow(`INSERT INTO users (email, password_hash, full_name) VALUES ($1, 'x', 'teste') RETURNING id`, email).Scan(&userID); err != nil {
		cleanupCompany()
		t.Fatalf("falha ao criar user de teste: %v", err)
	}
	if _, err := db.Exec(`UPDATE companies SET owner_id = $1 WHERE id = $2`, userID, companyID); err != nil {
		t.Fatalf("falha ao definir owner da company de teste: %v", err)
	}

	authed = func(r *http.Request) *http.Request {
		ctx := context.WithValue(r.Context(), ClaimsKey, jwt.MapClaims{"user_id": userID})
		r = r.WithContext(ctx)
		r.Header.Set("X-Company-ID", companyID)
		return r
	}
	cleanup = func() {
		// nfse_*, rfb_* e rfb_requests caem por ON DELETE CASCADE de companies.
		cleanupCompany()
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}
	return
}

// insertNFSeIBSCBS insere uma NFS-e com os valores IBS/CBS da migration 135. ibsTotal/cbs nil
// = grupo IBSCBS ausente no XML (colunas NULL).
func insertNFSeIBSCBS(t *testing.T, db *sql.DB, table, companyID, chave, mesAno string, ibsUF, ibsMun, ibsTotal, cbs *float64, cancelada bool) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO `+table+` (company_id, identificador_nfse, cnpj_prestador, cnpj_tomador, data_emissao, mes_ano,
		                       valor_servicos, ibs_uf, ibs_mun, ibs_total, cbs, cancelada)
		VALUES ($1, $2, '11111111000191', '22222222000172', CURRENT_DATE, $3, 1000, $4, $5, $6, $7, $8)
	`, companyID, chave, mesAno, ibsUF, ibsMun, ibsTotal, cbs, cancelada)
	if err != nil {
		t.Fatalf("falha ao inserir %s de teste: %v", table, err)
	}
}

func f64(v float64) *float64 { return &v }

func getApuracaoPainel(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, mesAno string) apuracaoPainelResponse {
	t.Helper()
	path := "/api/apuracao/painel"
	if mesAno != "" {
		path += "?mes_ano=" + mesAno
	}
	req := authed(httptest.NewRequest(http.MethodGet, path, nil))
	rec := httptest.NewRecorder()
	ApuracaoPainelHandler(db)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d — corpo: %s", rec.Code, rec.Body.String())
	}
	var body apuracaoPainelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v, corpo: %s", err, rec.Body.String())
	}
	return body
}

func quase(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// ---------------------------------------------------------------------------
// Testes — pré-DB
// ---------------------------------------------------------------------------

func TestApuracaoPainelHandlerMethodNotAllowed(t *testing.T) {
	rr := httptest.NewRecorder()
	ApuracaoPainelHandler(nil)(rr, httptest.NewRequest(http.MethodPost, "/api/apuracao/painel", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

func TestApuracaoPainelHandlerUnauthorized(t *testing.T) {
	rr := httptest.NewRecorder()
	ApuracaoPainelHandler(nil)(rr, httptest.NewRequest(http.MethodGet, "/api/apuracao/painel", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Testes — I/O Matrix (spec-nfse-apuracao-ibscbs.md), Postgres de teste necessário
// ---------------------------------------------------------------------------

// Cenários: "Nota 30 prestada 10/2026" (débito NFS-e somado ao total de débitos), "Nota
// tomada" (crédito NFS-e), "Nota cancelada" (fora das somas) e "Sem grupo IBSCBS" (soma 0,
// não quebra).
func TestApuracaoPainel_NFSe_MatrizDebitoCreditoCanceladaSemIBSCBS(t *testing.T) {
	db := openApuracaoNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupApuracaoNFSeTest(t, db)
	defer cleanup()

	// Nota 30 prestada: CBS 209,52 e IBS 23,28 (UF 23,28 + Mun 0).
	insertNFSeIBSCBS(t, db, "nfse_saidas", companyID, "26116062212345678000199000000000003026100000000001", "10/2026",
		f64(23.28), f64(0), f64(23.28), f64(209.52), false)
	// Prestada cancelada — não pode entrar.
	insertNFSeIBSCBS(t, db, "nfse_saidas", companyID, "26116062212345678000199000000000003126100000000002", "10/2026",
		f64(5), f64(5), f64(10), f64(90), true)
	// Prestada sem grupo IBSCBS (NULL) — soma 0, mas conta como nota.
	insertNFSeIBSCBS(t, db, "nfse_saidas", companyID, "26116062212345678000199000000000003226100000000003", "10/2026",
		nil, nil, nil, nil, false)
	// Tomada: CBS 100, IBS 11 (UF 10 + Mun 1).
	insertNFSeIBSCBS(t, db, "nfse_entradas", companyID, "26116062299999999000199000000000000126100000000004", "10/2026",
		f64(10), f64(1), f64(11), f64(100), false)

	body := getApuracaoPainel(t, db, authed, "")

	if body.MesSelecionado != "10/2026" {
		t.Fatalf("esperava 10/2026 como período (só NFS-e na empresa), obteve %q (meses=%v)", body.MesSelecionado, body.MesesDisponiveis)
	}

	cbs := body.CBS
	if !quase(cbs.DebitoNfseTotal, 209.52) || cbs.QtdNfsePrestadas != 2 {
		t.Errorf("CBS débito NFS-e: esperava 209,52 em 2 notas (cancelada fora), obteve %.2f em %d", cbs.DebitoNfseTotal, cbs.QtdNfsePrestadas)
	}
	if !quase(cbs.CreditoNfseTotal, 100) || cbs.QtdNfseTomadas != 1 {
		t.Errorf("CBS crédito NFS-e: esperava 100 em 1 nota, obteve %.2f em %d", cbs.CreditoNfseTotal, cbs.QtdNfseTomadas)
	}
	if !quase(cbs.DebitoGeral, 209.52) || !quase(cbs.CreditoGeral, 100) || !quase(cbs.SaldoTotal, 109.52) {
		t.Errorf("CBS totais: esperava débito 209,52 / crédito 100 / saldo 109,52, obteve %.2f / %.2f / %.2f", cbs.DebitoGeral, cbs.CreditoGeral, cbs.SaldoTotal)
	}
	if cbs.DebitoTotal != 0 || cbs.QtdSaidas != 0 {
		t.Errorf("NF-e saídas não pode receber NFS-e: esperava 0, obteve %.2f em %d", cbs.DebitoTotal, cbs.QtdSaidas)
	}

	ibs := body.IBS
	if !quase(ibs.DebitoNfseTotal, 23.28) || !quase(ibs.DebitoNfseUF, 23.28) || ibs.DebitoNfseMun != 0 {
		t.Errorf("IBS débito NFS-e: esperava total 23,28 (UF 23,28, Mun 0), obteve total %.2f UF %.2f Mun %.2f", ibs.DebitoNfseTotal, ibs.DebitoNfseUF, ibs.DebitoNfseMun)
	}
	if !quase(ibs.CreditoNfseTotal, 11) || !quase(ibs.CreditoNfseUF, 10) || !quase(ibs.CreditoNfseMun, 1) {
		t.Errorf("IBS crédito NFS-e: esperava 11 (10/1), obteve %.2f (%.2f/%.2f)", ibs.CreditoNfseTotal, ibs.CreditoNfseUF, ibs.CreditoNfseMun)
	}
	if !quase(ibs.DebitoGeral, 23.28) || !quase(ibs.CreditoGeral, 11) || !quase(ibs.SaldoTotal, 12.28) {
		t.Errorf("IBS totais: esperava 23,28 / 11 / 12,28, obteve %.2f / %.2f / %.2f", ibs.DebitoGeral, ibs.CreditoGeral, ibs.SaldoTotal)
	}
	if !quase(ibs.SaldoUF, 13.28) || !quase(ibs.SaldoMun, -1) {
		t.Errorf("IBS saldo UF/Mun: esperava 13,28 / -1, obteve %.2f / %.2f", ibs.SaldoUF, ibs.SaldoMun)
	}
}

// Período só com NFS-e cancelada não aparece na lista de períodos (mesma regra das somas).
func TestApuracaoPainel_NFSe_SoCanceladaNaoGeraPeriodo(t *testing.T) {
	db := openApuracaoNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupApuracaoNFSeTest(t, db)
	defer cleanup()

	insertNFSeIBSCBS(t, db, "nfse_saidas", companyID, "26116062212345678000199000000000004026100000000001", "09/2026",
		f64(1), f64(1), f64(2), f64(20), true)

	body := getApuracaoPainel(t, db, authed, "")
	for _, m := range body.MesesDisponiveis {
		if m == "09/2026" {
			t.Errorf("09/2026 só tem NFS-e cancelada e não deveria aparecer, meses=%v", body.MesesDisponiveis)
		}
	}
}
