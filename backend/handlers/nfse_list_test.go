package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// insertNFSeDoc insere uma linha de teste em nfse_entradas/nfse_saidas (schema idêntico nas
// duas tabelas, migration 133). Usado pelos testes de isolamento de tenant e filtro de
// competência abaixo — não exercita o parser real de services/nfse_adn.go.
func insertNFSeDoc(t *testing.T, db *sql.DB, table, companyID, identificador, cnpjPrestador, cnpjTomador, mesAno string, valorServicos, iss float64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO `+table+` (company_id, identificador_nfse, cnpj_prestador, cnpj_tomador, data_emissao, mes_ano, valor_servicos, iss)
		VALUES ($1, $2, $3, $4, CURRENT_DATE, $5, $6, $7)
	`, companyID, identificador, cnpjPrestador, cnpjTomador, mesAno, valorServicos, iss)
	if err != nil {
		t.Fatalf("falha ao inserir %s de teste: %v", table, err)
	}
}

func getNFSeList(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, path string, handlerFactory func(*sql.DB) http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	handlerFactory(db)(rec, req)
	return rec
}

type nfseListResponse struct {
	Total      int                `json:"total"`
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	TotalPages int                `json:"total_pages"`
	Totals     map[string]float64 `json:"totals"`
	Items      []nfseRow          `json:"items"`
}

func decodeNFSeList(t *testing.T, rec *httptest.ResponseRecorder) nfseListResponse {
	t.Helper()
	var body nfseListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v, corpo: %s", err, rec.Body.String())
	}
	return body
}

// ---------------------------------------------------------------------------
// Testes — pré-DB (sem necessidade de Postgres)
// ---------------------------------------------------------------------------

func TestNFSeEntradasListHandlerMethodNotAllowed(t *testing.T) {
	handler := NFSeEntradasListHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/nfse-entradas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

func TestNFSeEntradasListHandlerOptionsPreflight(t *testing.T) {
	handler := NFSeEntradasListHandler(nil)
	req := httptest.NewRequest(http.MethodOptions, "/api/nfse-entradas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("esperava HTTP 200 em OPTIONS, obteve %d", rr.Code)
	}
}

func TestNFSeEntradasListHandlerUnauthorized(t *testing.T) {
	handler := NFSeEntradasListHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nfse-entradas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestNFSeSaidasListHandlerMethodNotAllowed(t *testing.T) {
	handler := NFSeSaidasListHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/nfse-saidas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

func TestNFSeSaidasListHandlerUnauthorized(t *testing.T) {
	handler := NFSeSaidasListHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nfse-saidas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestNFSeEntradasCompetenciasHandlerUnauthorized(t *testing.T) {
	handler := NFSeEntradasCompetenciasHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nfse-entradas/competencias", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Testes — I/O Matrix (spec-nfse-frontend.md), Postgres de teste necessário.
// Pulados automaticamente (t.Skip via openNFSeTestDB) se o banco não estiver
// disponível ou a migration 133 não tiver sido aplicada.
// ---------------------------------------------------------------------------

// Cenário: "Lista sem nenhum documento capturado" — nfse_entradas/nfse_saidas vazias pra
// empresa; espera tabela vazia com totais zerados, sem erro.
func TestNFSeEntradasListHandler_SemDocumentos_ListaVaziaSemErro(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec := getNFSeList(t, db, authed, "/api/nfse-entradas", NFSeEntradasListHandler)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d — corpo: %s", rec.Code, rec.Body.String())
	}
	body := decodeNFSeList(t, rec)
	if body.Total != 0 || len(body.Items) != 0 {
		t.Errorf("esperava lista vazia, obteve total=%d items=%d", body.Total, len(body.Items))
	}
	for k, v := range body.Totals {
		if v != 0 {
			t.Errorf("esperava total %q zerado, obteve %v", k, v)
		}
	}
}

func TestNFSeSaidasListHandler_SemDocumentos_ListaVaziaSemErro(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec := getNFSeList(t, db, authed, "/api/nfse-saidas", NFSeSaidasListHandler)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d — corpo: %s", rec.Code, rec.Body.String())
	}
	body := decodeNFSeList(t, rec)
	if body.Total != 0 || len(body.Items) != 0 {
		t.Errorf("esperava lista vazia, obteve total=%d items=%d", body.Total, len(body.Items))
	}
}

// Cenário: "Filtro por competência sem resultado" — mês/ano sem documentos; espera tabela
// vazia, totais zerados, sem erro (mesmo com documentos existentes em OUTRA competência).
func TestNFSeEntradasListHandler_FiltroCompetenciaSemResultado_ListaVaziaSemErro(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	insertNFSeDoc(t, db, "nfse_entradas", companyID, "nfse-doc-1", "11111111000191", "22222222000172", "01/2026", 1000, 50)

	req := httptest.NewRequest(http.MethodGet, "/api/nfse-entradas?mes_ano=12/2099", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	NFSeEntradasListHandler(db)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d — corpo: %s", rec.Code, rec.Body.String())
	}
	body := decodeNFSeList(t, rec)
	if body.Total != 0 || len(body.Items) != 0 {
		t.Errorf("esperava lista vazia pro filtro de competência sem match, obteve total=%d items=%d", body.Total, len(body.Items))
	}
	if body.Totals["valor_servicos"] != 0 || body.Totals["iss"] != 0 {
		t.Errorf("esperava totais zerados, obteve %+v", body.Totals)
	}
}

// Cenário: isolamento entre tenants — empresa B nunca vê documento de NFS-e da empresa A,
// mesmo padrão de tenant scoping de GetEffectiveCompanyID já usado em nfe_entradas.go.
func TestNFSeEntradasListHandler_IsolamentoEntreEmpresas(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()

	companyA, authedA, cleanupA := setupNFSeCertTest(t, db)
	defer cleanupA()
	companyB, authedB, cleanupB := setupNFSeCertTest(t, db)
	defer cleanupB()

	insertNFSeDoc(t, db, "nfse_entradas", companyA, "nfse-empresa-a-1", "11111111000191", "22222222000172", "01/2026", 1000, 50)

	// Empresa A vê o próprio documento.
	recA := getNFSeList(t, db, authedA, "/api/nfse-entradas", NFSeEntradasListHandler)
	bodyA := decodeNFSeList(t, recA)
	if bodyA.Total != 1 {
		t.Fatalf("esperava empresa A ver 1 documento próprio, obteve total=%d", bodyA.Total)
	}

	// Empresa B NUNCA vê o documento da empresa A.
	recB := getNFSeList(t, db, authedB, "/api/nfse-entradas", NFSeEntradasListHandler)
	if recB.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200 pra empresa B, obteve %d — corpo: %s", recB.Code, recB.Body.String())
	}
	bodyB := decodeNFSeList(t, recB)
	if bodyB.Total != 0 || len(bodyB.Items) != 0 {
		t.Errorf("vazamento entre tenants: empresa B viu %d documento(s) da empresa A", bodyB.Total)
	}
	if companyA == companyB {
		t.Fatalf("setup inválido: companyA e companyB são a mesma empresa")
	}
}

func TestNFSeSaidasListHandler_IsolamentoEntreEmpresas(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()

	companyA, authedA, cleanupA := setupNFSeCertTest(t, db)
	defer cleanupA()
	companyB, authedB, cleanupB := setupNFSeCertTest(t, db)
	defer cleanupB()

	insertNFSeDoc(t, db, "nfse_saidas", companyA, "nfse-saida-empresa-a-1", "11111111000191", "22222222000172", "01/2026", 2000, 80)

	recA := getNFSeList(t, db, authedA, "/api/nfse-saidas", NFSeSaidasListHandler)
	bodyA := decodeNFSeList(t, recA)
	if bodyA.Total != 1 {
		t.Fatalf("esperava empresa A ver 1 documento próprio, obteve total=%d", bodyA.Total)
	}

	recB := getNFSeList(t, db, authedB, "/api/nfse-saidas", NFSeSaidasListHandler)
	bodyB := decodeNFSeList(t, recB)
	if bodyB.Total != 0 || len(bodyB.Items) != 0 {
		t.Errorf("vazamento entre tenants: empresa B viu %d documento(s) da empresa A", bodyB.Total)
	}
	if companyA == companyB {
		t.Fatalf("setup inválido: companyA e companyB são a mesma empresa")
	}
}

// sort_by fora da allowlist (nfseSafeSortCols) cai no fallback seguro (data_emissao) — nunca
// interpola o valor recebido direto na query. "cnpj_prestador" é uma coluna real da tabela, mas
// não está na allowlist, então deve se comportar exatamente como sort_by=data_emissao (mesma
// ordenação), não como erro nem como aceito sem validação.
func TestNFSeEntradasListHandler_SortByForaDaAllowlist_CaiNoFallback(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	insertNFSeDoc(t, db, "nfse_entradas", companyID, "nfse-sort-1", "11111111000191", "22222222000172", "01/2026", 1000, 50)
	insertNFSeDoc(t, db, "nfse_entradas", companyID, "nfse-sort-2", "33333333000154", "22222222000172", "02/2026", 2000, 80)

	recFallback := getNFSeList(t, db, authed, "/api/nfse-entradas?sort_by=cnpj_prestador", NFSeEntradasListHandler)
	if recFallback.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d — corpo: %s", recFallback.Code, recFallback.Body.String())
	}
	recExplicit := getNFSeList(t, db, authed, "/api/nfse-entradas?sort_by=data_emissao", NFSeEntradasListHandler)
	if recExplicit.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d — corpo: %s", recExplicit.Code, recExplicit.Body.String())
	}

	bodyFallback := decodeNFSeList(t, recFallback)
	bodyExplicit := decodeNFSeList(t, recExplicit)

	if len(bodyFallback.Items) != len(bodyExplicit.Items) {
		t.Fatalf("esperava mesmo número de itens entre fallback e sort_by=data_emissao, obteve %d vs %d", len(bodyFallback.Items), len(bodyExplicit.Items))
	}
	for i := range bodyExplicit.Items {
		if bodyFallback.Items[i].ID != bodyExplicit.Items[i].ID {
			t.Errorf("esperava sort_by=cnpj_prestador (fora da allowlist) ordenar igual a sort_by=data_emissao (fallback), diferença na posição %d: %q vs %q", i, bodyFallback.Items[i].ID, bodyExplicit.Items[i].ID)
		}
	}
}

// Competências: empresa sem documentos retorna lista vazia, nunca erro.
func TestNFSeEntradasCompetenciasHandler_SemDocumentos_ListaVazia(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/nfse-entradas/competencias", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	NFSeEntradasCompetenciasHandler(db)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperava HTTP 200, obteve %d — corpo: %s", rec.Code, rec.Body.String())
	}
	var meses []string
	if err := json.Unmarshal(rec.Body.Bytes(), &meses); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	if len(meses) != 0 {
		t.Errorf("esperava lista de competências vazia, obteve %v", meses)
	}
}
