package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── Seam de teste do SSRF ──
// O servidor httptest é http://127.0.0.1, que a validação real recusa (só https, sem
// loopback). Os testes de download por urlAssinada ligam rfbSSRFTestBypass (atomic.Bool não
// exportado, só alterado aqui) e a desligam no Cleanup. Os testes de rejeição rodam com
// ele DESLIGADO, exercitando a validação de produção.
func withSSRFBypass(t *testing.T) {
	t.Helper()
	rfbSSRFTestBypass.Store(true)
	t.Cleanup(func() { rfbSSRFTestBypass.Store(false) })
}

// v2Fake é um gateway RFB de teste: token fixo + handler por teste; registra método+path
// e o header Authorization de cada chamada.
type v2Fake struct {
	mu    sync.Mutex
	calls []string
	auths map[string]string
	URL   string
}

func newV2Fake(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) *v2Fake {
	t.Helper()
	f := &v2Fake{auths: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.auths[r.URL.Path] = r.Header.Get("Authorization")
		f.mu.Unlock()
		if r.URL.Path == "/token" {
			fmt.Fprint(w, `{"access_token":"tok-v2","token_type":"Bearer","expires_in":3600}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	t.Setenv("RFB_API_URL", srv.URL)
	t.Setenv("RFB_TOKEN_URL", srv.URL+"/token")
	return f
}

func (f *v2Fake) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *v2Fake) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *v2Fake) authFor(path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.auths[path]
	return a, ok
}

func insertRFBRequestV2(t *testing.T, db *sql.DB, companyID, tiquete, tipo, ambiente, apiVersao, tiqDownload, urlAssinada string, exp *time.Time) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, ambiente, api_versao, tiquete_download, url_assinada, url_assinada_expira_em)
		VALUES ($1, '00000000', $2, 'webhook_received', $3, $4, $5, NULLIF($6,''), NULLIF($7,''), $8) RETURNING id
	`, companyID, tiquete, tipo, ambiente, apiVersao, tiqDownload, urlAssinada, exp).Scan(&id); err != nil {
		t.Fatalf("falha ao criar rfb_requests v2: %v", err)
	}
	return id
}

func readRequest(t *testing.T, db *sql.DB, id string) (status, errCode, errMsg string, url sql.NullString) {
	t.Helper()
	var ec, em sql.NullString
	if err := db.QueryRow(`SELECT status, error_code, error_message, url_assinada FROM rfb_requests WHERE id = $1`, id).
		Scan(&status, &ec, &em, &url); err != nil {
		t.Fatalf("ler request: %v", err)
	}
	return status, ec.String, em.String, url
}

const v2DebitoBody = `{"apuracao":[{"pa":"01/2026","debitos":[{"origem":1,"documento":55,"chave":"V2-URL","emissao":"2026-01-10T10:00:00","registro":"2026-01-20T08:00:00","atualizacao":"2026-02-01T00:00:00","cbs":{"apurado":100.00,"excedente":0,"inexigivel":0,"suspenso":0,"extinto":0,"saldoDevedor":100.00}}]}]}`

// ── Puros ──

func TestResolveRFBAPIVersion(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		name     string
		ambiente string
		env      map[string]string
		want     string
	}{
		{"default sem env", "producao", nil, "v1"},
		{"default restrita sem env", "producao_restrita", nil, "v1"},
		{"global v2 vale para producao", "producao", map[string]string{"RFB_API_VERSION": "v2"}, "v2"},
		{"global v2 vale para restrita", "producao_restrita", map[string]string{"RFB_API_VERSION": "v2"}, "v2"},
		{"restrita sobrescreve so a restrita", "producao_restrita", map[string]string{"RFB_API_VERSION_RESTRITA": "v2"}, "v2"},
		{"restrita nao afeta producao", "producao", map[string]string{"RFB_API_VERSION_RESTRITA": "v2"}, "v1"},
		{"restrita v1 sobrescreve global v2", "producao_restrita", map[string]string{"RFB_API_VERSION": "v2", "RFB_API_VERSION_RESTRITA": "v1"}, "v1"},
		{"valor invalido cai em v1", "producao", map[string]string{"RFB_API_VERSION": "v9"}, "v1"},
		{"case e espacos", "producao", map[string]string{"RFB_API_VERSION": " V2 "}, "v2"},
		{"restrita invalida mantem global v2", "producao_restrita", map[string]string{"RFB_API_VERSION": "v2", "RFB_API_VERSION_RESTRITA": "v9"}, "v2"},
		{"restrita invalida sem global vira v1", "producao_restrita", map[string]string{"RFB_API_VERSION_RESTRITA": "talvez"}, "v1"},
		{"restrita invalida nao afeta producao v2", "producao", map[string]string{"RFB_API_VERSION": "v2", "RFB_API_VERSION_RESTRITA": "v9"}, "v2"},
	}
	for _, tc := range cases {
		if got := resolveRFBAPIVersion(tc.ambiente, env(tc.env)); got != tc.want {
			t.Errorf("%s: esperava %s, veio %s", tc.name, tc.want, got)
		}
	}
}

func TestRFBBuildURL(t *testing.T) {
	mk := func(amb, ver string) *RFBClient {
		c := &RFBClient{baseURL: "https://rfb", pathPrefix: "rtc", ambiente: "producao", apiVersao: "v1"}
		c.SetAmbiente(amb)
		c.SetAPIVersao(ver)
		return c
	}
	cases := []struct {
		amb, ver string
		op       rfbOp
		want     string
	}{
		{"producao", "v1", rfbOpSolicitarDebito, "https://rfb/rtc/apuracao-cbs/v1/12345678"},
		{"producao", "v1", rfbOpSolicitarCredito, "https://rfb/rtc/creditos-cbs/v1/12345678"},
		{"producao_restrita", "v1", rfbOpSolicitarDebito, "https://rfb/prr-rtc/apuracao-cbs/v1/12345678"},
		{"producao_restrita", "v1", rfbOpDownload, "https://rfb/prr-rtc/download/v1/12345678"},
		{"producao", "v2", rfbOpSolicitarDebito, "https://rfb/apuracao-cbs/v2/debitos/12345678"},
		{"producao", "v2", rfbOpSolicitarCredito, "https://rfb/apuracao-cbs/v2/creditos/12345678"},
		{"producao_restrita", "v2", rfbOpSolicitarDebito, "https://rfb/apuracao-cbs-prr/v2/debitos/12345678"},
		{"producao_restrita", "v2", rfbOpSolicitarCredito, "https://rfb/apuracao-cbs-prr/v2/creditos/12345678"},
		{"producao", "v2", rfbOpSituacao, "https://rfb/apuracao-cbs/v2/situacao/12345678"},
		{"producao_restrita", "v2", rfbOpSituacao, "https://rfb/apuracao-cbs-prr/v2/situacao/12345678"},
		// download por tíquete é sempre o caminho v1 (fallback), mesmo com cliente v2
		{"producao", "v2", rfbOpDownload, "https://rfb/rtc/download/v1/12345678"},
	}
	for _, tc := range cases {
		if got := mk(tc.amb, tc.ver).buildURL(tc.op, "12345678"); got != tc.want {
			t.Errorf("%s/%s op=%d: esperava %s, veio %s", tc.amb, tc.ver, tc.op, tc.want, got)
		}
	}
}

func TestValidarURLAssinada_RejeitaInseguras(t *testing.T) {
	bad := []string{
		"http://arquivos.rfb.gov.br/x",
		"ftp://arquivos.rfb.gov.br/x",
		"https://127.0.0.1/x",
		"https://10.1.2.3/x",
		"https://192.168.0.5/x",
		"https://172.16.0.1/x",
		"https://169.254.169.254/latest/meta-data",
		"https://0.0.0.0/x",
		"https://[::1]/x",
		"https://[fd00::1]/x",
		"https://[::ffff:10.0.0.1]/x",
		"https://100.64.0.1/x",
		// faixas de documentação / IETF / benchmark
		"https://192.0.0.8/x",
		"https://192.0.2.1/x",
		"https://198.51.100.5/x",
		"https://203.0.113.9/x",
		"https://198.18.0.1/x",
		// NAT64 (64:ff9b::7f00:1 = 127.0.0.1), 6to4 (2002:7f00:1:: = 127.0.0.1), Teredo, site-local
		"https://[64:ff9b::7f00:1]/x",
		"https://[64:ff9b::808:808]/x",
		"https://[64:ff9b:1::1]/x",
		"https://[2002:7f00:1::1]/x",
		"https://[2001:0:4136:e378:8000:63bf:3fff:fdd2]/x",
		"https://[fec0::1]/x",
		"https://[::7f00:1]/x", // IPv4-compatível
		// porta != 443
		"https://arquivos.rfb.gov.br:8443/x",
		"https://8.8.8.8:444/x",
		"https://user:pw@arquivos.rfb.gov.br/x",
		"https:///semhost",
		"::::",
	}
	for _, u := range bad {
		if err := ValidarURLAssinada(u); err == nil {
			t.Errorf("esperava rejeição de %q", u)
		}
	}
	if err := ValidarURLAssinada("https://arquivos.rfb.gov.br/x?sig=abc"); err != nil {
		t.Errorf("https com host público deveria passar: %v", err)
	}
	if err := ValidarURLAssinada("https://8.8.8.8/x"); err != nil {
		t.Errorf("IP público deveria passar: %v", err)
	}
	if err := ValidarURLAssinada("https://arquivos.rfb.gov.br:443/x"); err != nil {
		t.Errorf("porta 443 explícita deveria passar: %v", err)
	}
}

// Allow-list opcional por RFB_URL_ASSINADA_HOSTS (sufixos de host).
func TestValidarURLAssinada_AllowList(t *testing.T) {
	t.Setenv("RFB_URL_ASSINADA_HOSTS", "rfb.gov.br, .storage.example")
	for _, u := range []string{"https://arquivos.rfb.gov.br/x", "https://rfb.gov.br/x", "https://b.storage.example/x"} {
		if err := ValidarURLAssinada(u); err != nil {
			t.Errorf("%s deveria passar na allow-list: %v", u, err)
		}
	}
	for _, u := range []string{"https://evil.com/x", "https://notrfb.gov.br/x", "https://rfb.gov.br.evil.com/x", "https://8.8.8.8/x"} {
		if err := ValidarURLAssinada(u); err == nil {
			t.Errorf("%s deveria ser barrada pela allow-list", u)
		}
	}
	t.Setenv("RFB_URL_ASSINADA_HOSTS", "")
	if err := ValidarURLAssinada("https://evil.com/x"); err != nil {
		t.Errorf("sem allow-list qualquer host público passa: %v", err)
	}
}

func TestSafeDialContext_RecusaPortaNao443(t *testing.T) {
	if _, err := safeDialContext(context.Background(), "tcp", "8.8.8.8:80"); err == nil || !strings.Contains(err.Error(), "porta") {
		t.Errorf("dial em porta 80 deveria ser barrado pela porta, veio %v", err)
	}
}

// Validação real (sem bypass): http:// e destinos internos são recusados no download,
// inclusive quando o nome resolve para loopback (dial guard) — e o erro nunca carrega a URL.
func TestDownloadURLAssinada_ValidacaoRealNoDownload(t *testing.T) {
	if rfbSSRFTestBypass.Load() {
		t.Fatal("bypass deveria estar desligado")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "segredo") }))
	defer srv.Close()

	for _, u := range []string{
		srv.URL + "/f?sig=SEGREDO123",          // http://127.0.0.1
		"https://127.0.0.1:1/f?sig=SEGREDO123", // https + loopback literal
		"https://localhost:1/f?sig=SEGREDO123", // nome → loopback: barrado no dial
		"https://169.254.169.254/latest/meta-data?sig=SEGREDO123",
	} {
		data, err := DownloadURLAssinada(u)
		if err == nil || data != nil {
			t.Errorf("esperava erro para %q, veio data=%v err=%v", u, data, err)
			continue
		}
		if strings.Contains(err.Error(), "SEGREDO123") || strings.Contains(err.Error(), "/f?") {
			t.Errorf("erro vaza a URL assinada: %v", err)
		}
	}

	if _, err := safeDialContext(context.Background(), "tcp", "localhost:443"); err == nil || !strings.Contains(err.Error(), "não permitido") {
		t.Errorf("dial em localhost deveria ser barrado, veio %v", err)
	}
}

// ── Solicitação (sem banco) ──

func TestRFBSolicitar_RespostaV1eV2(t *testing.T) {
	f := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		switch {
		case strings.HasPrefix(r.URL.Path, "/rtc/apuracao-cbs/v1/"):
			fmt.Fprint(w, `{"tiquete":"tiq-v1"}`)
		case strings.HasPrefix(r.URL.Path, "/apuracao-cbs/v2/debitos/"):
			fmt.Fprint(w, `{"tiqueteSolicitacao":"tiq-v2","tEASegundos":120}`)
		case strings.HasPrefix(r.URL.Path, "/apuracao-cbs-prr/v2/creditos/"):
			fmt.Fprint(w, `{"tiqueteSolicitacao":"tiq-v2-cred","tEASegundos":"120"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	c := NewRFBClient()
	c.SetAmbiente("producao")
	if got, err := c.SolicitarApuracao("tok", "12345678"); err != nil || got != "tiq-v1" {
		t.Fatalf("v1: esperava tiq-v1, veio %q err=%v", got, err)
	}
	c.SetAPIVersao("v2")
	if got, err := c.SolicitarApuracao("tok", "12345678"); err != nil || got != "tiq-v2" {
		t.Fatalf("v2 (tEASegundos número): esperava tiq-v2, veio %q err=%v", got, err)
	}
	c.SetAmbiente("producao_restrita")
	if got, err := c.SolicitarCredito("tok", "12345678"); err != nil || got != "tiq-v2-cred" {
		t.Fatalf("v2 créditos restrita (tEASegundos string): esperava tiq-v2-cred, veio %q err=%v", got, err)
	}
	for _, want := range []string{"POST /rtc/apuracao-cbs/v1/12345678", "POST /apuracao-cbs/v2/debitos/12345678", "POST /apuracao-cbs-prr/v2/creditos/12345678"} {
		found := false
		for _, c := range f.snapshot() {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("chamada %q não feita; chamadas: %v", want, f.snapshot())
		}
	}
}

func TestRFBConsultarSituacao(t *testing.T) {
	f := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"estado":"CONCLUIDA","urlAssinada":"https://x.example/f","urlAssinadaExpiraEm":"2030-01-01T00:00:00Z"}`)
	})
	c := NewRFBClient()
	c.SetAmbiente("producao_restrita")
	c.SetAPIVersao("v2")
	sit, err := c.ConsultarSituacao("tok", "TIQ1")
	if err != nil || sit.Estado != "CONCLUIDA" || sit.UrlAssinada != "https://x.example/f" {
		t.Fatalf("situacao: %+v err=%v", sit, err)
	}
	if got := f.count("GET /apuracao-cbs-prr/v2/situacao/TIQ1"); got != 1 {
		t.Errorf("esperava GET em /apuracao-cbs-prr/v2/situacao/TIQ1, chamadas: %v", f.snapshot())
	}
	if a, _ := f.authFor("/apuracao-cbs-prr/v2/situacao/TIQ1"); a != "Bearer tok" {
		t.Errorf("situacao deve levar Bearer, veio %q", a)
	}
}

// ── Solicitação (banco): versão gravada por solicitação ──

func TestRFBSolicitarApuracao_ApiVersaoDefaultV1(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("RFB_API_VERSION", "")
	t.Setenv("RFB_API_VERSION_RESTRITA", "")
	fake := newFakeRFB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao_restrita")

	if err := SolicitarApuracaoParaEmpresa(db, companyID, LimiteDebitosDiaRFBv1); err != nil {
		t.Fatalf("solicitar: %v", err)
	}
	waitCreditoRows(t, db, companyID, 1)
	if got := fake.count("POST /prr-rtc/apuracao-cbs/v1/"); got != 1 {
		t.Errorf("sem env de versão deve chamar a URL v1, chamadas: %v", fake.snapshot())
	}
	rows, _ := db.Query(`SELECT tipo, api_versao FROM rfb_requests WHERE company_id = $1`, companyID)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var tipo, v string
		rows.Scan(&tipo, &v)
		n++
		if v != "v1" {
			t.Errorf("%s: api_versao esperada v1, veio %s", tipo, v)
		}
	}
	if n != 2 {
		t.Errorf("esperava 2 linhas (débito + crédito), veio %d", n)
	}
}

func TestRFBSolicitarApuracao_RestritaV2GravaApiVersao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("RFB_API_VERSION", "")
	t.Setenv("RFB_API_VERSION_RESTRITA", "v2")
	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		if strings.Contains(r.URL.Path, "/debitos/") {
			fmt.Fprint(w, `{"tiqueteSolicitacao":"tiq-deb-v2","tEASegundos":60}`)
		} else {
			fmt.Fprint(w, `{"tiqueteSolicitacao":"tiq-cred-v2","tEASegundos":60}`)
		}
	})
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao_restrita")

	if err := SolicitarApuracaoParaEmpresa(db, companyID, LimiteDebitosDiaRFBv2); err != nil {
		t.Fatalf("solicitar: %v", err)
	}
	waitCreditoRows(t, db, companyID, 1)

	cnpj8 := ""
	db.QueryRow(`SELECT cnpj_base FROM rfb_requests WHERE company_id = $1 AND tipo = 'debito'`, companyID).Scan(&cnpj8)
	if got := fake.count("POST /apuracao-cbs-prr/v2/debitos/" + cnpj8); got != 1 {
		t.Errorf("esperava POST /apuracao-cbs-prr/v2/debitos/%s, chamadas: %v", cnpj8, fake.snapshot())
	}
	if got := fake.count("POST /apuracao-cbs-prr/v2/creditos/" + cnpj8); got != 1 {
		t.Errorf("esperava POST /apuracao-cbs-prr/v2/creditos/%s, chamadas: %v", cnpj8, fake.snapshot())
	}
	rows, _ := db.Query(`SELECT tipo, tiquete, api_versao, status FROM rfb_requests WHERE company_id = $1`, companyID)
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var tipo, tiq, v, st string
		rows.Scan(&tipo, &tiq, &v, &st)
		got[tipo] = tiq + "/" + v + "/" + st
	}
	if got["debito"] != "tiq-deb-v2/v2/requested" || got["credito"] != "tiq-cred-v2/v2/requested" {
		t.Errorf("linhas inesperadas: %v", got)
	}
}

// Linhas de erro também registram api_versao (ex.: rota v2 ainda não liberada → 404).
func TestRFBSolicitarApuracao_ErroGravaApiVersao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("RFB_API_VERSION", "v2")
	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"codigoErro":"X","mensagemErro":"falha"}`)
	})
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	if err := SolicitarApuracaoParaEmpresa(db, companyID, LimiteDebitosDiaRFBv2); err == nil {
		t.Fatal("esperava erro")
	}
	var v, st string
	if err := db.QueryRow(`SELECT api_versao, status FROM rfb_requests WHERE company_id = $1`, companyID).Scan(&v, &st); err != nil {
		t.Fatalf("linha de erro não gravada: %v", err)
	}
	if v != "v2" || st != "error" {
		t.Errorf("esperava v2/error, veio %s/%s", v, st)
	}
}

// ── Download v2 (banco) ──

func TestProcessarDownload_V2_UrlAssinadaSemBearer(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/arquivo" {
			fmt.Fprint(w, v2DebitoBody)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	exp := time.Now().Add(time.Hour)
	id := insertRFBRequestV2(t, db, companyID, "tiq-url-1", "debito", "producao", "v2", "", fake.URL+"/arquivo?sig=abc", &exp)

	if err := ProcessarDownloadRFB(db, NewRFBClient(), id); err != nil {
		t.Fatalf("download: %v", err)
	}
	waitAsyncSAPSync()

	if a, ok := fake.authFor("/arquivo"); !ok || a != "" {
		t.Errorf("GET da urlAssinada não pode levar Authorization (ok=%v auth=%q)", ok, a)
	}
	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 0 {
		t.Errorf("url válida não deveria consultar situação: %v", fake.snapshot())
	}
	status, _, _, url := readRequest(t, db, id)
	if status != "completed" {
		t.Fatalf("esperava completed, veio %s", status)
	}
	if url.Valid {
		t.Errorf("url_assinada deve ser NULL após completed, veio %q", url.String)
	}
	if total, _, _, _, _, _, _ := readRfbResumo(t, db, companyID, "01/2026"); total != 1 {
		t.Errorf("parser v2 deveria ter gerado resumo com 1 débito, veio %d", total)
	}
}

func TestProcessarDownload_V2_ExpiradaConsultaSituacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	// credencial registrada em Produção, mas a solicitação nasceu restrita: segmento -prr.
	insertRFBCredential(t, db, companyID, "producao")

	var fake *v2Fake
	fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apuracao-cbs-prr/v2/situacao/tiq-exp":
			fmt.Fprintf(w, `{"estado":"CONCLUIDA","urlAssinada":"%s/novo?sig=nova","urlAssinadaExpiraEm":"2099-01-01T00:00:00Z"}`, fake.URL)
		case "/novo":
			fmt.Fprint(w, v2DebitoBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	past := time.Now().Add(-time.Hour)
	id := insertRFBRequestV2(t, db, companyID, "tiq-exp", "debito", "producao_restrita", "v2", "", fake.URL+"/velho?sig=velha", &past)

	if err := ProcessarDownloadRFB(db, NewRFBClient(), id); err != nil {
		t.Fatalf("download: %v", err)
	}
	waitAsyncSAPSync()

	if got := fake.count("GET /velho"); got != 0 {
		t.Errorf("URL expirada não deve ser usada: %v", fake.snapshot())
	}
	if a, _ := fake.authFor("/apuracao-cbs-prr/v2/situacao/tiq-exp"); a != "Bearer tok-v2" {
		t.Errorf("situacao deve levar Bearer, veio %q", a)
	}
	if a, _ := fake.authFor("/novo"); a != "" {
		t.Errorf("URL renovada sem Authorization, veio %q", a)
	}
	if status, _, _, url := readRequest(t, db, id); status != "completed" || url.Valid {
		t.Errorf("esperava completed com url_assinada NULL, veio %s / %v", status, url)
	}
}

func TestProcessarDownload_V2_UrlRecusada403RenovaViaSituacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	var fake *v2Fake
	fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/velho":
			w.WriteHeader(http.StatusForbidden)
		case "/apuracao-cbs/v2/situacao/tiq-403":
			fmt.Fprintf(w, `{"estado":"CONCLUIDA","urlAssinada":"%s/novo"}`, fake.URL)
		case "/novo":
			fmt.Fprint(w, v2DebitoBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	exp := time.Now().Add(time.Hour)
	id := insertRFBRequestV2(t, db, companyID, "tiq-403", "debito", "producao", "v2", "", fake.URL+"/velho", &exp)
	if err := ProcessarDownloadRFB(db, NewRFBClient(), id); err != nil {
		t.Fatalf("download: %v", err)
	}
	waitAsyncSAPSync()
	if status, _, _, _ := readRequest(t, db, id); status != "completed" {
		t.Fatalf("esperava completed, veio %s (chamadas %v)", status, fake.snapshot())
	}
}

func TestProcessarDownload_V2_SituacaoPendenteEErro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tiq-pend"):
			fmt.Fprint(w, `{"estado":"EM_PROCESSAMENTO"}`)
		case strings.HasSuffix(r.URL.Path, "/tiq-erro"):
			fmt.Fprint(w, `{"estado":"ERRO","codigoErro":"E123","mensagemErro":"Falha interna na RFB"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	// debito e credito usam o mesmo helper
	for _, tipo := range []string{"debito", "credito"} {
		process := ProcessarDownloadRFB
		if tipo == "credito" {
			process = ProcessarDownloadCreditosRFB
		}

		idP := insertRFBRequestV2(t, db, companyID, "tiq-pend", tipo, "producao", "v2", "", "", nil)
		if err := process(db, NewRFBClient(), idP); err == nil {
			t.Errorf("%s pendente: esperava erro", tipo)
		}
		if st, code, _, _ := readRequest(t, db, idP); st != "error" || code != "NOT_READY" {
			t.Errorf("%s pendente: esperava error/NOT_READY, veio %s/%s", tipo, st, code)
		}

		idE := insertRFBRequestV2(t, db, companyID, "tiq-erro", tipo, "producao", "v2", "", "", nil)
		if err := process(db, NewRFBClient(), idE); err == nil {
			t.Errorf("%s erro: esperava erro", tipo)
		}
		if st, code, msg, _ := readRequest(t, db, idE); st != "error" || code != "E123" || msg != "Falha interna na RFB" {
			t.Errorf("%s erro: esperava error/E123/mensagem da RFB, veio %s/%s/%s", tipo, st, code, msg)
		}
	}
}

func TestProcessarDownload_V2_FallbackTiqueteDownloadV1(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rtc/download/v1/tiq-dl":
			fmt.Fprint(w, v2DebitoBody)
		default: // situacao 404 (ex.: rota ainda não liberada)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	id := insertRFBRequestV2(t, db, companyID, "tiq-sol", "debito", "producao", "v2", "tiq-dl", "", nil)
	if err := ProcessarDownloadRFB(db, NewRFBClient(), id); err != nil {
		t.Fatalf("download: %v", err)
	}
	waitAsyncSAPSync()
	if got := fake.count("GET /apuracao-cbs/v2/situacao/tiq-sol"); got != 1 {
		t.Errorf("deveria tentar situação antes do v1: %v", fake.snapshot())
	}
	if a, _ := fake.authFor("/rtc/download/v1/tiq-dl"); a != "Bearer tok-v2" {
		t.Errorf("download v1 leva Bearer, veio %q", a)
	}
	if st, _, _, _ := readRequest(t, db, id); st != "completed" {
		t.Errorf("esperava completed, veio %s", st)
	}
}

// Solicitação v1 continua baixando por /download/v1 mesmo com a config global já em v2.
func TestProcessarDownload_UsaApiVersaoDaSolicitacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("RFB_API_VERSION", "v2")
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")
	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rtc/download/v1/tiq-v1-dl" {
			fmt.Fprint(w, `nao-e-json`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	id := insertRFBRequestV2(t, db, companyID, "tiq-v1", "debito", "producao", "v1", "tiq-v1-dl", "", nil)
	_ = ProcessarDownloadRFB(db, NewRFBClient(), id) // termina em PARSE_ERROR; só importa o caminho
	if fake.count("GET /rtc/download/v1/tiq-v1-dl") != 1 || fake.count("GET /apuracao-cbs/v2/situacao/") != 0 {
		t.Errorf("solicitação v1 deve usar download v1 sem situação: %v", fake.snapshot())
	}
}

// ── Endurecimento (revisões adversariais) ──

func TestTruncateRunes(t *testing.T) {
	// 49 'E' + 'ã' (2 bytes) na fronteira de 50 runes: corte por bytes partiria o caractere.
	in := strings.Repeat("E", 49) + "ãããã"
	got := TruncateRunes(in, 50)
	if !utf8Valid(got) || len([]rune(got)) != 50 || !strings.HasSuffix(got, "ã") {
		t.Errorf("corte por runes inválido: %q (%d runes)", got, len([]rune(got)))
	}
	if got := TruncateRunes("curto", 50); got != "curto" {
		t.Errorf("texto curto não pode mudar: %q", got)
	}
	bad := "ok\xff\xfe" + "x\x00y"
	if out := TruncateRunes(bad, 100); !utf8Valid(out) || strings.ContainsRune(out, 0) {
		t.Errorf("UTF-8 inválido/NUL deveriam ser saneados: %q", out)
	}
	if TruncateRunes("abc", 0) != "" {
		t.Error("max 0 deve devolver vazio")
	}
	if out := rfbTruncBody(strings.Repeat("ç", 400)); len([]rune(out)) != 303 || !utf8Valid(out) {
		t.Errorf("rfbTruncBody: %d runes, valid=%v", len([]rune(out)), utf8Valid(out))
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "") == s }

func TestParseURLExpiraEm(t *testing.T) {
	if ParseURLExpiraEm("") != nil {
		t.Error("vazio deve ser nil")
	}
	for _, raw := range []string{"2030-05-01T12:00:00Z", "2030-05-01T12:00:00.123456789Z", "2030-05-01T09:00:00-03:00"} {
		got := ParseURLExpiraEm(raw)
		if got == nil || got.UTC().Hour() != 12 {
			t.Errorf("%s: esperava 12:00Z, veio %v", raw, got)
		}
	}
	// sem offset: assume America/Sao_Paulo (UTC-3) → 15:00Z
	for _, raw := range []string{"2030-05-01T12:00:00", "2030-05-01 12:00:00"} {
		got := ParseURLExpiraEm(raw)
		if got == nil || got.UTC().Hour() != 15 {
			t.Errorf("%s (sem offset): esperava 15:00Z, veio %v", raw, got)
		}
	}
	// ilegível: TTL conservador (~1h), nunca nil
	got := ParseURLExpiraEm("amanhã de manhã")
	if got == nil || time.Until(*got) < 50*time.Minute || time.Until(*got) > 70*time.Minute {
		t.Errorf("ilegível deveria dar ~1h de TTL, veio %v", got)
	}
}

func TestRFBBuildURL_EscapaIds(t *testing.T) {
	c := &RFBClient{baseURL: "https://rfb", pathPrefix: "rtc", ambiente: "producao", apiVersao: "v2"}
	if got := c.buildURL(rfbOpSituacao, "a/b c?x"); got != "https://rfb/apuracao-cbs/v2/situacao/a%2Fb%20c%3Fx" {
		t.Errorf("id sem escape: %s", got)
	}
	if got := c.buildURL(rfbOpDownload, "../x"); strings.Contains(got, "/../") {
		t.Errorf("path traversal no id: %s", got)
	}
}

// helper: servidor com token contado e handlers por path; devolve cliente v2 já autenticado.
func v2Client(t *testing.T, amb string) *RFBClient {
	t.Helper()
	c := NewRFBClient()
	c.SetAmbiente(amb)
	c.SetAPIVersao("v2")
	return c
}

func TestBaixar_UrlPersistida400_404_500_RenovaViaSituacao(t *testing.T) {
	withSSRFBypass(t)
	for _, st := range []int{400, 401, 403, 404, 410, 500, 503} {
		st := st
		t.Run(fmt.Sprintf("HTTP_%d", st), func(t *testing.T) {
			var renovada atomic.Bool
			var novoAposPersistir atomic.Bool
			var fake *v2Fake
			fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/velho":
					w.WriteHeader(st)
					fmt.Fprint(w, "<Error>SIGNATURE-SEGREDO</Error>")
				case "/apuracao-cbs/v2/situacao/tiq":
					fmt.Fprintf(w, `{"estado":"CONCLUIDA","urlAssinada":"%s/novo","urlAssinadaExpiraEm":"2099-01-01T00:00:00Z"}`, fake.URL)
				case "/novo":
					novoAposPersistir.Store(renovada.Load())
					fmt.Fprint(w, "conteudo")
				default:
					w.WriteHeader(404)
				}
			})
			exp := time.Now().Add(time.Hour)
			var persistida string
			data, err := BaixarArquivoRFB(v2Client(t, "producao"), "tok", RFBDownloadInput{
				Tiquete: "tiq", APIVersao: "v2", URLAssinada: fake.URL + "/velho", URLExpiraEm: &exp,
				OnURLRenovada: func(u string, e *time.Time) { persistida = u; renovada.Store(true) },
			})
			if err != nil || string(data) != "conteudo" {
				t.Fatalf("esperava renovar via situacao, veio data=%q err=%v (chamadas %v)", data, err, fake.snapshot())
			}
			if persistida != fake.URL+"/novo" || !novoAposPersistir.Load() {
				t.Errorf("URL renovada deve ser persistida ANTES de baixar (persistida=%q antes=%v)", persistida, novoAposPersistir.Load())
			}
		})
	}
}

func TestBaixar_UrlFalhaSemVazarCorpoNoErro(t *testing.T) {
	withSSRFBypass(t)
	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, "<Error>SIGNATURE-SEGREDO</Error>")
	})
	_, err := DownloadURLAssinada(fake.URL + "/x?sig=abc")
	var de *RFBDownloadError
	if !errors.As(err, &de) || de.Code != RFBErrURLExpirada {
		t.Fatalf("esperava URL_EXPIRADA, veio %v", err)
	}
	if strings.Contains(err.Error(), "SEGREDO") || strings.Contains(err.Error(), "sig=") {
		t.Errorf("erro persistido vaza corpo/URL: %v", err)
	}
}

func TestBaixar_MargemDe2MinutosNaExpiracao(t *testing.T) {
	withSSRFBypass(t)
	var fake *v2Fake
	fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apuracao-cbs/v2/situacao/tiq":
			fmt.Fprintf(w, `{"estado":"CONCLUIDA","urlAssinada":"%s/novo"}`, fake.URL)
		case "/novo":
			fmt.Fprint(w, "ok")
		default:
			fmt.Fprint(w, "velho")
		}
	})
	exp := time.Now().Add(time.Minute) // < 2 min: já conta como expirada
	data, err := BaixarArquivoRFB(v2Client(t, "producao"), "tok", RFBDownloadInput{
		Tiquete: "tiq", APIVersao: "v2", URLAssinada: fake.URL + "/velho", URLExpiraEm: &exp})
	if err != nil || string(data) != "ok" || fake.count("GET /velho") != 0 {
		t.Fatalf("URL a <2min de expirar não deve ser usada: data=%q err=%v chamadas=%v", data, err, fake.snapshot())
	}
}

func TestBaixar_SituacaoEstadoNormalizado(t *testing.T) {
	withSSRFBypass(t)
	var fake *v2Fake
	fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/t-conc"):
			fmt.Fprintf(w, `{"estado":" Concluida ","urlAssinada":"%s/f"}`, fake.URL)
		case strings.HasSuffix(r.URL.Path, "/t-pend"):
			fmt.Fprint(w, `{"estado":"em_processamento"}`)
		case strings.HasSuffix(r.URL.Path, "/t-erro"):
			fmt.Fprintf(w, `{"estado":" erro","codigoErro":"%s","mensagemErro":"%s"}`, strings.Repeat("é", 60), strings.Repeat("ã", 1200))
		case strings.HasSuffix(r.URL.Path, "/t-desc"):
			fmt.Fprint(w, `{"estado":"FILA_NOVA"}`)
		case r.URL.Path == "/f":
			fmt.Fprint(w, "arquivo")
		default:
			w.WriteHeader(404)
		}
	})
	c := v2Client(t, "producao")
	if data, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "t-conc", APIVersao: "v2"}); err != nil || string(data) != "arquivo" {
		t.Errorf("' Concluida ' deveria baixar: data=%q err=%v", data, err)
	}
	for _, tq := range []string{"t-pend", "t-desc"} {
		_, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: tq, APIVersao: "v2", TiqueteDownload: "nao-deveria-usar"})
		var de *RFBDownloadError
		if !errors.As(err, &de) || de.Code != RFBErrNotReady {
			t.Errorf("%s: esperava NOT_READY, veio %v", tq, err)
		}
	}
	_, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "t-erro", APIVersao: "v2"})
	var de *RFBDownloadError
	if !errors.As(err, &de) || len([]rune(de.Code)) != 50 || !utf8Valid(de.Code) || len([]rune(de.Message)) != 1000 {
		t.Errorf("ERRO: código deve ter 50 runes e mensagem 1000 (UTF-8 válido), veio %d/%d", len([]rune(de.Code)), len([]rune(de.Message)))
	}
}

func TestBaixar_LimiteDeTamanhoECorpoVazio(t *testing.T) {
	withSSRFBypass(t)
	old := rfbMaxDownloadBytes
	rfbMaxDownloadBytes = 16
	t.Cleanup(func() { rfbMaxDownloadBytes = old })

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/grande", "/rtc/download/v1/grande":
			fmt.Fprint(w, strings.Repeat("x", 100))
		case "/limite":
			fmt.Fprint(w, strings.Repeat("x", 16))
		case "/vazio", "/rtc/download/v1/vazio":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	})
	code := func(err error) string {
		var de *RFBDownloadError
		if errors.As(err, &de) {
			return de.Code
		}
		return "?"
	}
	if _, err := DownloadURLAssinada(fake.URL + "/grande"); code(err) != RFBErrTooLarge {
		t.Errorf("url acima do limite: esperava FILE_TOO_LARGE, veio %v", err)
	}
	if data, err := DownloadURLAssinada(fake.URL + "/limite"); err != nil || len(data) != 16 {
		t.Errorf("exatamente no limite deve passar: %v", err)
	}
	if _, err := DownloadURLAssinada(fake.URL + "/vazio"); err == nil || !strings.Contains(err.Error(), "vazia") {
		t.Errorf("url com corpo vazio: esperava erro claro, veio %v", err)
	}
	c := NewRFBClient()
	if _, err := c.DownloadArquivo("tok", "grande"); code(err) != RFBErrTooLarge {
		t.Errorf("v1 acima do limite: esperava FILE_TOO_LARGE, veio %v", err)
	}
	if _, err := c.DownloadArquivo("tok", "vazio"); err == nil || !strings.Contains(err.Error(), "vazia") {
		t.Errorf("v1 corpo vazio: esperava erro claro, veio %v", err)
	}
	// o tipo sobrevive ao helper de fallback
	_, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "grande", APIVersao: "v1"})
	if code(err) != RFBErrTooLarge {
		t.Errorf("BaixarArquivoRFB deve preservar FILE_TOO_LARGE, veio %v", err)
	}
}

func TestConsultarSituacao_401RenovaTokenERepete1x(t *testing.T) {
	var tokens, sit401, sitOK int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			n := atomic.AddInt32(&tokens, 1)
			fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"Bearer","expires_in":3600}`, n)
		case strings.Contains(r.URL.Path, "/situacao/"):
			if r.Header.Get("Authorization") == "Bearer tok-1" {
				atomic.AddInt32(&sit401, 1)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			atomic.AddInt32(&sitOK, 1)
			fmt.Fprint(w, `{"estado":"PENDENTE"}`)
		}
	}))
	defer srv.Close()
	t.Setenv("RFB_API_URL", srv.URL)
	t.Setenv("RFB_TOKEN_URL", srv.URL+"/token")

	c := v2Client(t, "producao")
	cid := fmt.Sprintf("cid-401-%d", time.Now().UnixNano())
	tok, err := c.GetToken(cid, "s")
	if err != nil || tok != "tok-1" {
		t.Fatalf("token inicial: %q %v", tok, err)
	}
	sit, err := c.ConsultarSituacao(tok, "T1")
	if err != nil || sit.Estado != "PENDENTE" {
		t.Fatalf("esperava sucesso após retry com token novo: %+v err=%v", sit, err)
	}
	if atomic.LoadInt32(&sit401) != 1 || atomic.LoadInt32(&sitOK) != 1 || atomic.LoadInt32(&tokens) != 2 {
		t.Errorf("esperava 1x 401, 1x OK, 2 tokens; veio %d/%d/%d", sit401, sitOK, tokens)
	}
	rfbTokenCache.mu.Lock()
	cached := rfbTokenCache.tokens[cid].token
	rfbTokenCache.mu.Unlock()
	if cached != "tok-2" {
		t.Errorf("cache deveria guardar o token novo, veio %q", cached)
	}

	// 401 persistente: só 1 retry (sem laço)
	atomic.StoreInt32(&sit401, 0)
	c2 := v2Client(t, "producao")
	c2.lastClientID, c2.lastClientSecret = "", ""
	if _, err := c2.ConsultarSituacao("tok-1", "T2"); err == nil {
		t.Error("sem credenciais conhecidas o 401 deve virar erro")
	}
}

// ── Banco ──

func TestProcessarDownload_ErroTerminalZeraUrlAssinada(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")
	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"estado":"ERRO","codigoErro":"E9","mensagemErro":"falhou"}`)
	})
	past := time.Now().Add(-time.Hour)
	id := insertRFBRequestV2(t, db, companyID, "tiq-zera", "debito", "producao", "v2", "", "https://arquivos.rfb.example/f?sig=SEGREDO", &past)
	if err := ProcessarDownloadRFB(db, NewRFBClient(), id); err == nil {
		t.Fatal("esperava erro")
	}
	var url sql.NullString
	var exp sql.NullTime
	var st string
	db.QueryRow(`SELECT status, url_assinada, url_assinada_expira_em FROM rfb_requests WHERE id = $1`, id).Scan(&st, &url, &exp)
	if st != "error" || url.Valid || exp.Valid {
		t.Errorf("erro terminal deve zerar url_assinada*: status=%s url=%v exp=%v", st, url, exp)
	}
}

func TestUpdateRequestError_TruncaPorRunes(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBRequestV2(t, db, companyID, "tiq-trunc", "debito", "producao", "v2", "", "", nil)
	updateRequestError(db, id, strings.Repeat("é", 80), "msg\xff inválida")
	st, code, msg, _ := readRequest(t, db, id)
	if st != "error" || len([]rune(code)) != 50 || !utf8Valid(msg) {
		t.Errorf("gravação UTF-8-safe falhou: %s %d %q", st, len([]rune(code)), msg)
	}
}

func TestProcessarDownload_V2_SituacaoPersisteUrlRenovadaAntesDoDownload(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")
	var id string
	var seen atomic.Value
	var fake *v2Fake
	fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apuracao-cbs/v2/situacao/tiq-pers":
			fmt.Fprintf(w, `{"estado":"CONCLUIDA","urlAssinada":"%s/novo","urlAssinadaExpiraEm":"2099-01-01T00:00:00Z"}`, fake.URL)
		case "/novo":
			var u sql.NullString
			db.QueryRow(`SELECT url_assinada FROM rfb_requests WHERE id = $1`, id).Scan(&u)
			seen.Store(u.String)
			fmt.Fprint(w, v2DebitoBody)
		default:
			w.WriteHeader(404)
		}
	})
	past := time.Now().Add(-time.Hour)
	id = insertRFBRequestV2(t, db, companyID, "tiq-pers", "debito", "producao", "v2", "", fake.URL+"/velho", &past)
	if err := ProcessarDownloadRFB(db, NewRFBClient(), id); err != nil {
		t.Fatalf("download: %v", err)
	}
	waitAsyncSAPSync()
	if got, _ := seen.Load().(string); got != fake.URL+"/novo" {
		t.Errorf("no momento do GET da URL renovada ela já devia estar persistida; veio %q", got)
	}
}
