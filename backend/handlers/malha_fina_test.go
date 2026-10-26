package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers — Malha Fina NFS-e x RFB (spec-nfse-apuracao-ibscbs.md)
// ---------------------------------------------------------------------------

// openMalhaFinaNFSeTestDB abre o Postgres de teste e pula se a mv_malha_fina_resumo ainda não
// tiver os blocos NFS-e (migration 136).
func openMalhaFinaNFSeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openApuracaoNFSeTestDB(t)
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_matviews WHERE matviewname = 'mv_malha_fina_resumo' AND definition LIKE '%nfse-saidas%')`).Scan(&ok); err != nil || !ok {
		db.Close()
		t.Skip("mv_malha_fina_resumo sem os blocos NFS-e — rode a migration 136 antes deste teste")
	}
	return db
}

// insertRFBDoc insere um documento na base RFB (rfb_debitos ou rfb_creditos) com um
// rfb_requests próprio. Só para teste — NFS-e de verdade nunca é gravada em rfb_* pelo
// sistema; aqui a linha simula o que a RFB devolveu no download.
func insertRFBDoc(t *testing.T, db *sql.DB, table, companyID, modelo, chave, niEmitente, niAdquirente string, cbsNaoExtinto float64) {
	t.Helper()
	tipo := "debito"
	if table == "rfb_creditos" {
		tipo = "credito"
	}
	var reqID string
	if err := db.QueryRow(`INSERT INTO rfb_requests (company_id, cnpj_base, status, tipo) VALUES ($1, '12345678', 'completed', $2) RETURNING id`, companyID, tipo).Scan(&reqID); err != nil {
		t.Fatalf("falha ao criar rfb_requests de teste: %v", err)
	}
	var err error
	if table == "rfb_debitos" {
		_, err = db.Exec(`
			INSERT INTO rfb_debitos (request_id, company_id, tipo_apuracao, modelo_dfe, chave_dfe, data_dfe_emissao,
			                         ni_emitente, ni_adquirente, valor_cbs_total, valor_cbs_nao_extinto, situacao_debito)
			VALUES ($1, $2, 'APURACAO', $3, $4, '2026-10-05', $5, $6, $7, $7, 'EM_ABERTO')
		`, reqID, companyID, modelo, chave, niEmitente, niAdquirente, cbsNaoExtinto)
	} else {
		_, err = db.Exec(`
			INSERT INTO rfb_creditos (request_id, company_id, modelo_dfe, chave_dfe, data_dfe_emissao,
			                          ni_emitente, ni_adquirente, valor_cbs_total, valor_cbs_nao_extinto, situacao_credito)
			VALUES ($1, $2, $3, $4, '2026-10-05', $5, $6, $7, $7, 'EM_ABERTO')
		`, reqID, companyID, modelo, chave, niEmitente, niAdquirente, cbsNaoExtinto)
	}
	if err != nil {
		t.Fatalf("falha ao inserir %s de teste: %v", table, err)
	}
}

func getMalhaFina(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, path string, h func(*sql.DB) http.HandlerFunc) MalhaFinaResponse {
	t.Helper()
	req := authed(httptest.NewRequest(http.MethodGet, path, nil))
	rec := httptest.NewRecorder()
	h(db)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: esperava HTTP 200, obteve %d — corpo: %s", path, rec.Code, rec.Body.String())
	}
	var body MalhaFinaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v, corpo: %s", err, rec.Body.String())
	}
	return body
}

func chavesMalha(items []MalhaFinaRow) map[string]string {
	m := map[string]string{}
	for _, it := range items {
		m[it.ChaveDFe] = it.StatusNota
	}
	return m
}

const (
	chaveNFSeAusente   = "26116062212345678000199000000000005026100000000001"
	chaveNFSePresente  = "26116062212345678000199000000000005126100000000002"
	chaveNFSeCancelada = "26116062212345678000199000000000005226100000000003"
	chaveNFeSaida44    = "26261012345678000199550010000000011000000011"
	chaveNFSeTomada    = "26116062299999999000199000000000000126100000000004"
)

// ---------------------------------------------------------------------------
// Testes — pré-DB
// ---------------------------------------------------------------------------

func TestMalhaFinaNFSeHandlersUnauthorized(t *testing.T) {
	for path, h := range map[string]func(*sql.DB) http.HandlerFunc{
		"/api/malha-fina/nfse-saidas":          MalhaFinaNFSeSaidasHandler,
		"/api/malha-fina/nfse-entradas":        MalhaFinaNFSeEntradasHandler,
		"/api/malha-fina/nfse-saidas/resumo":   MalhaFinaNFSeSaidasResumoHandler,
		"/api/malha-fina/nfse-entradas/resumo": MalhaFinaNFSeEntradasResumoHandler,
	} {
		rr := httptest.NewRecorder()
		h(nil)(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: esperava HTTP 401, obteve %d", path, rr.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// Testes — I/O Matrix (spec-nfse-apuracao-ibscbs.md), Postgres de teste necessário
// ---------------------------------------------------------------------------

// Cenários: "RFB tem NFS-e que não temos" (aparece), "RFB e nós temos a mesma" (não
// aparece), "Chave 44 (NF-e) na RFB" (continua só nos blocos NF-e) + cancelada na base
// própria (aparece como CANCELADA, mesmo padrão de NF-e/CT-e).
func TestMalhaFinaNFSeSaidas_Matriz(t *testing.T) {
	db := openMalhaFinaNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupApuracaoNFSeTest(t, db)
	defer cleanup()

	const empresa, cliente = "12345678000199", "99999999000199"
	insertRFBDoc(t, db, "rfb_debitos", companyID, "", chaveNFSeAusente, empresa, cliente, 50)
	insertRFBDoc(t, db, "rfb_debitos", companyID, "", chaveNFSePresente, empresa, cliente, 60)
	insertRFBDoc(t, db, "rfb_debitos", companyID, "", chaveNFSeCancelada, empresa, cliente, 70)
	insertRFBDoc(t, db, "rfb_debitos", companyID, "55", chaveNFeSaida44, empresa, cliente, 80)

	insertNFSeIBSCBS(t, db, "nfse_saidas", companyID, chaveNFSePresente, "10/2026", nil, nil, nil, f64(60), false)
	insertNFSeIBSCBS(t, db, "nfse_saidas", companyID, chaveNFSeCancelada, "10/2026", nil, nil, nil, f64(70), true)

	body := getMalhaFina(t, db, authed, "/api/malha-fina/nfse-saidas", MalhaFinaNFSeSaidasHandler)
	got := chavesMalha(body.Items)
	if got[chaveNFSeAusente] != "AUSENTE" {
		t.Errorf("NFS-e da RFB sem XML nosso deveria aparecer como AUSENTE, obteve %v", got)
	}
	if _, ok := got[chaveNFSePresente]; ok {
		t.Errorf("NFS-e presente em nfse_saidas não deveria aparecer na malha fina, obteve %v", got)
	}
	if got[chaveNFSeCancelada] != "CANCELADA" {
		t.Errorf("NFS-e cancelada na base própria deveria aparecer como CANCELADA, obteve %v", got)
	}
	if _, ok := got[chaveNFeSaida44]; ok {
		t.Errorf("chave 44 (NF-e) não pode aparecer no bloco NFS-e, obteve %v", got)
	}
	if body.Total != 2 || body.Totals.CanceladasCount != 1 || !quase(body.Totals.ValorCBSNaoExtinto, 120) {
		t.Errorf("totais NFS-e saídas: esperava 2 docs / 1 cancelada / R$ 120, obteve %d / %d / %.2f", body.Total, body.Totals.CanceladasCount, body.Totals.ValorCBSNaoExtinto)
	}

	// Chave 44 continua só no bloco NF-e.
	nfe := chavesMalha(getMalhaFina(t, db, authed, "/api/malha-fina/nfe-saidas", MalhaFinaNFeSaidasHandler).Items)
	if nfe[chaveNFeSaida44] != "AUSENTE" {
		t.Errorf("chave 44 deveria continuar no bloco NF-e saídas, obteve %v", nfe)
	}
	for _, c := range []string{chaveNFSeAusente, chaveNFSePresente, chaveNFSeCancelada} {
		if _, ok := nfe[c]; ok {
			t.Errorf("chave NFS-e %s não pode aparecer no bloco NF-e", c)
		}
	}

	// Resumo (MV) — mesmo refresh já usado pelos blocos NF-e/CT-e. Só a ausente conta (a MV,
	// como nos blocos existentes, considera só NOT EXISTS; a cancelada tem chave em nfse_saidas).
	RefreshMalhaFinaMV(db)
	var qtd int
	var valor float64
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantidade),0), COALESCE(SUM(valor_cbs_nao_extinto),0) FROM mv_malha_fina_resumo WHERE company_id = $1 AND tipo = 'nfse-saidas'`, companyID).Scan(&qtd, &valor); err != nil {
		t.Fatalf("falha ao consultar MV: %v", err)
	}
	if qtd != 1 || !quase(valor, 50) {
		t.Errorf("MV nfse-saidas: esperava 1 doc / R$ 50, obteve %d / %.2f", qtd, valor)
	}
	var qtdNFe int
	db.QueryRow(`SELECT COALESCE(SUM(quantidade),0) FROM mv_malha_fina_resumo WHERE company_id = $1 AND tipo = 'nfe-saidas'`, companyID).Scan(&qtdNFe)
	if qtdNFe != 1 {
		t.Errorf("MV nfe-saidas: esperava só a chave 44 (1 doc), obteve %d", qtdNFe)
	}

	rec := httptest.NewRecorder()
	MalhaFinaNFSeSaidasResumoHandler(db)(rec, authed(httptest.NewRequest(http.MethodGet, "/api/malha-fina/nfse-saidas/resumo", nil)))
	var resumo struct {
		Items []MalhaFinaResumoRow `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resumo); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("resumo NFS-e saídas: HTTP %d, erro %v, corpo %s", rec.Code, err, rec.Body.String())
	}
	if len(resumo.Items) != 1 || resumo.Items[0].NiEmitente != empresa || resumo.Items[0].Quantidade != 1 {
		t.Errorf("resumo NFS-e saídas: esperava 1 linha do emitente %s com 1 doc, obteve %+v", empresa, resumo.Items)
	}
}

// Entradas: rfb_creditos com chave 50 sem nfse_entradas aparece; com nfse_entradas não.
func TestMalhaFinaNFSeEntradas_Matriz(t *testing.T) {
	db := openMalhaFinaNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupApuracaoNFSeTest(t, db)
	defer cleanup()

	const prestador, empresa = "99999999000199", "12345678000199"
	insertRFBDoc(t, db, "rfb_creditos", companyID, "", chaveNFSeTomada, prestador, empresa, 30)
	insertRFBDoc(t, db, "rfb_creditos", companyID, "", chaveNFSePresente, prestador, empresa, 40)
	insertNFSeIBSCBS(t, db, "nfse_entradas", companyID, chaveNFSePresente, "10/2026", nil, nil, nil, f64(40), false)
	// Mesma chave presente só em nfse_SAIDAS não conta para o lado de entradas.
	insertNFSeIBSCBS(t, db, "nfse_saidas", companyID, chaveNFSeTomada, "10/2026", nil, nil, nil, f64(30), false)

	got := chavesMalha(getMalhaFina(t, db, authed, "/api/malha-fina/nfse-entradas?filial_cnpj="+empresa, MalhaFinaNFSeEntradasHandler).Items)
	if got[chaveNFSeTomada] != "AUSENTE" {
		t.Errorf("NFS-e tomada da RFB sem nfse_entradas deveria aparecer como AUSENTE, obteve %v", got)
	}
	if _, ok := got[chaveNFSePresente]; ok {
		t.Errorf("NFS-e presente em nfse_entradas não deveria aparecer, obteve %v", got)
	}

	// Lado de débito não enxerga rfb_creditos.
	if saidas := getMalhaFina(t, db, authed, "/api/malha-fina/nfse-saidas", MalhaFinaNFSeSaidasHandler); saidas.Total != 0 {
		t.Errorf("nfse-saidas não deveria listar documentos de rfb_creditos, obteve %d", saidas.Total)
	}
}
