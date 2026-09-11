package services

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRFB simula o gateway da RFB (token, apuração, créditos e download) e registra
// cada chamada recebida, para os testes conferirem em qual prefixo (rtc/prr-rtc) ela
// foi feita.
type fakeRFB struct {
	mu    sync.Mutex
	calls []string
}

func newFakeRFB(t *testing.T) *fakeRFB {
	t.Helper()
	f := &fakeRFB{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/token":
			fmt.Fprint(w, `{"access_token":"tok-teste-rfb","token_type":"Bearer","expires_in":3600}`)
		case strings.Contains(r.URL.Path, "/apuracao-cbs/v1/"):
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"tiquete":"tiq-teste"}`)
		case strings.Contains(r.URL.Path, "/download/v1/"):
			// Não é um JSON de apuração: o processamento para em PARSE_ERROR sem gravar
			// débitos — os testes só conferem o prefixo usado no download.
			fmt.Fprint(w, `nao-e-json`)
		default:
			// Inclui /creditos-cbs/v1/ — mesma resposta do gateway real enquanto a RFB
			// não libera a rota.
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"no Route matched with those values"}`)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RFB_API_URL", srv.URL)
	t.Setenv("RFB_TOKEN_URL", srv.URL+"/token")
	return f
}

// count retorna quantas chamadas começam com o prefixo dado (ex.: "POST /prr-rtc/apuracao-cbs/").
func (f *fakeRFB) count(prefix string) int {
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

func (f *fakeRFB) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// insertRFBCredential cria a credencial ativa da empresa de teste. client_id e CNPJ
// são únicos por execução porque o cache de token e o bloqueio de rate-limit do
// client RFB são globais do pacote.
func insertRFBCredential(t *testing.T, db *sql.DB, companyID, ambiente string) {
	t.Helper()
	cnpjBase := fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
	if _, err := db.Exec(`
		INSERT INTO rfb_credentials (company_id, cnpj_matriz, client_id, client_secret, ambiente, ativo)
		VALUES ($1, $2, $3, 'secret-teste', $4, true)
	`, companyID, cnpjBase+"000199", "cid-rfb-teste-"+companyID, ambiente); err != nil {
		t.Fatalf("falha ao criar rfb_credentials de teste: %v", err)
	}
}

// waitCreditoRows espera a goroutine de créditos disparada por SolicitarApuracaoParaEmpresa
// gravar sua linha — sem isso ela poderia rodar depois do cleanup da empresa de teste.
func waitCreditoRows(t *testing.T, db *sql.DB, companyID string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM rfb_requests WHERE company_id = $1 AND tipo = 'credito'`, companyID).Scan(&n)
		if n >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine de créditos não gravou %d linha(s) em 5s (achou %d)", want, n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRFBSolicitarApuracao_GravaAmbienteDaCredencial(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	fake := newFakeRFB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao_restrita")

	if err := SolicitarApuracaoParaEmpresa(db, companyID, LimiteDebitosDiaRFB); err != nil {
		t.Fatalf("SolicitarApuracaoParaEmpresa: %v", err)
	}
	waitCreditoRows(t, db, companyID, 1)

	if got := fake.count("POST /prr-rtc/apuracao-cbs/v1/"); got != 1 {
		t.Errorf("esperava 1 POST em /prr-rtc/apuracao-cbs/v1/, teve %d (chamadas: %v)", got, fake.snapshot())
	}

	rows, err := db.Query(`SELECT tipo, status, ambiente FROM rfb_requests WHERE company_id = $1`, companyID)
	if err != nil {
		t.Fatalf("falha ao ler rfb_requests: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var tipo, status, ambiente string
		if err := rows.Scan(&tipo, &status, &ambiente); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[tipo] = status + "/" + ambiente
	}
	want := map[string]string{
		"debito":  "requested/producao_restrita",
		"credito": "error/producao_restrita", // ENDPOINT_INDISPONIVEL também grava o ambiente
	}
	for tipo, w := range want {
		if got[tipo] != w {
			t.Errorf("linha %s: esperava %q, veio %q", tipo, w, got[tipo])
		}
	}
}

func TestRFBSolicitarApuracao_LimiteDiario(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	fake := newFakeRFB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	// Coleta automática do dia já feita (terminou em erro — ainda conta como slot usado).
	if _, err := db.Exec(`
		INSERT INTO rfb_requests (company_id, cnpj_base, status, tipo, error_code)
		VALUES ($1, '00000000', 'error', 'debito', 'TIMEOUT')
	`, companyID); err != nil {
		t.Fatalf("falha ao criar solicitação do dia: %v", err)
	}

	// Agendamento: recusa sem chamar a RFB (nem o token).
	err := SolicitarApuracaoParaEmpresa(db, companyID, LimiteDebitosDiaAgendamento)
	if err == nil || !strings.HasPrefix(err.Error(), "DAILY_LIMIT") {
		t.Fatalf("agendamento com 1 débito no dia: esperava DAILY_LIMIT, veio %v", err)
	}
	if got := fake.count("POST "); got != 0 {
		t.Fatalf("agendamento recusado não deveria chamar a RFB, fez: %v", fake.snapshot())
	}

	// Manual: a 2ª chamada do dia passa (o bug era cair no limite de 1 do agendamento).
	if err := SolicitarApuracaoParaEmpresa(db, companyID, LimiteDebitosDiaRFB); err != nil {
		t.Fatalf("manual com 1 débito no dia: esperava sucesso, veio %v", err)
	}
	waitCreditoRows(t, db, companyID, 1)
	if got := fake.count("POST /rtc/apuracao-cbs/v1/"); got != 1 {
		t.Fatalf("esperava 1 POST em /rtc/apuracao-cbs/v1/, chamadas: %v", fake.snapshot())
	}

	// Teto da RFB: a 3ª chamada é recusada sem chamar a RFB.
	err = SolicitarApuracaoParaEmpresa(db, companyID, LimiteDebitosDiaRFB)
	if err == nil || !strings.HasPrefix(err.Error(), "DAILY_LIMIT") {
		t.Fatalf("manual com 2 débitos no dia: esperava DAILY_LIMIT, veio %v", err)
	}
	if got := fake.count("POST /rtc/apuracao-cbs/v1/"); got != 1 {
		t.Fatalf("teto atingido não deveria chamar a RFB de novo, chamadas: %v", fake.snapshot())
	}
}

func TestRFBProcessarDownload_UsaAmbienteDaSolicitacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	fake := newFakeRFB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	// O cadastro foi trocado para Produção depois que as solicitações nasceram em
	// Produção Restrita — o download tem que seguir a solicitação, não o cadastro.
	insertRFBCredential(t, db, companyID, "producao")

	cases := []struct {
		tipo, tiquete string
		processar     func(*sql.DB, *RFBClient, string) error
	}{
		{"debito", "tiq-dl-debito", ProcessarDownloadRFB},
		{"credito", "tiq-dl-credito", ProcessarDownloadCreditosRFB},
	}
	for _, tc := range cases {
		var requestID string
		if err := db.QueryRow(`
			INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, ambiente)
			VALUES ($1, '00000000', $2, 'webhook_received', $3, 'producao_restrita') RETURNING id
		`, companyID, tc.tiquete, tc.tipo).Scan(&requestID); err != nil {
			t.Fatalf("%s: falha ao criar solicitação: %v", tc.tipo, err)
		}

		// Termina em PARSE_ERROR (o fake não devolve JSON de apuração) — irrelevante aqui.
		_ = tc.processar(db, NewRFBClient(), requestID)

		if got := fake.count("GET /prr-rtc/download/v1/" + tc.tiquete); got != 1 {
			t.Errorf("%s: esperava download em /prr-rtc/, chamadas: %v", tc.tipo, fake.snapshot())
		}
	}
	if got := fake.count("GET /rtc/download/v1/"); got != 0 {
		t.Errorf("nenhum download deveria ir para /rtc/, chamadas: %v", fake.snapshot())
	}
}
