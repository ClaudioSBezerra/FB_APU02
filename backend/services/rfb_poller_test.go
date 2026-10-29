package services

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── Puros ──

func TestRFBSituacao_Efetivo(t *testing.T) {
	casos := []struct {
		nome string
		s    RFBSituacao
		want string
	}{
		{"contrato publicado", RFBSituacao{Situacao: "CONCLUIDA"}, "CONCLUIDA"},
		{"formato legado", RFBSituacao{Estado: "em_processamento"}, "EM_PROCESSAMENTO"},
		{"prefere situacao", RFBSituacao{Situacao: " expirada ", Estado: "PENDENTE"}, "EXPIRADA"},
		{"vazio", RFBSituacao{}, ""},
	}
	for _, c := range casos {
		if got := c.s.Efetivo(); got != c.want {
			t.Errorf("%s: esperava %q, veio %q", c.nome, c.want, got)
		}
	}
}

func TestRFBBuildURL_DownloadURL(t *testing.T) {
	c := NewRFBClient() // v1 por padrão: a URL de download v2 independe da versão do cliente
	if got, want := c.buildURL(rfbOpDownloadURL, "tiq 1"), c.baseURL+"/apuracao-cbs/v2/download/tiq%201"; got != want {
		t.Errorf("produção: esperava %s, veio %s", want, got)
	}
	c.SetAmbiente("producao_restrita")
	if got, want := c.buildURL(rfbOpDownloadURL, "t"), c.baseURL+"/apuracao-cbs-prr/v2/download/t"; got != want {
		t.Errorf("restrita: esperava %s, veio %s", want, got)
	}
}

func TestObterURLDownload(t *testing.T) {
	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apuracao-cbs/v2/download/ok":
			if r.Header.Get("Authorization") != "Bearer tok-x" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"urlAssinada":"https://s3.exemplo/arq?sig=1","urlAssinadaExpiraEm":"2030-01-01T00:00:00Z"}`)
		case "/apuracao-cbs/v2/download/sem-url":
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"codigoErro":"ARQUIVO_INDISPONIVEL","mensagemErro":"Arquivo de retorno indisponível"}`)
		}
	})
	c := NewRFBClient()

	u, exp, err := c.ObterURLDownload("tok-x", "ok")
	if err != nil || u != "https://s3.exemplo/arq?sig=1" || exp != "2030-01-01T00:00:00Z" {
		t.Fatalf("200: url=%q exp=%q err=%v", u, exp, err)
	}
	var he *RFBHTTPError
	if _, _, err := c.ObterURLDownload("tok-x", "nao-existe"); !errors.As(err, &he) || he.Status != 404 || he.Op != "download" {
		t.Errorf("404 deveria ser *RFBHTTPError{download,404}, veio %v", err)
	}
	if _, _, err := c.ObterURLDownload("tok-x", "sem-url"); err == nil {
		t.Errorf("200 sem urlAssinada deveria falhar")
	}
}

func TestBaixar_ContratoPublicado(t *testing.T) {
	withSSRFBypass(t)
	var fake *v2Fake
	fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apuracao-cbs/v2/situacao/t-conc", "/apuracao-cbs/v2/situacao/t-conc-sem-arq":
			fmt.Fprint(w, `{"situacao":"CONCLUIDA"}`)
		case "/apuracao-cbs/v2/situacao/t-exp":
			fmt.Fprint(w, `{"situacao":"EXPIRADA","codigoErro":"EXPIRADA","mensagemErro":"Tempo limite de processamento excedido (240 minutos)."}`)
		case "/apuracao-cbs/v2/download/t-conc":
			fmt.Fprintf(w, `{"urlAssinada":"%s/arq","urlAssinadaExpiraEm":"2099-01-01T00:00:00Z"}`, fake.URL)
		case "/arq":
			fmt.Fprint(w, "arquivo")
		default: // inclui download/t-conc-sem-arq
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"codigoErro":"ARQUIVO_INDISPONIVEL"}`)
		}
	})
	c := v2Client(t, "producao")

	if data, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "t-conc", APIVersao: "v2"}); err != nil || string(data) != "arquivo" {
		t.Errorf("CONCLUIDA deveria baixar via endpoint de download: data=%q err=%v", data, err)
	}
	var de *RFBDownloadError
	_, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "t-conc-sem-arq", APIVersao: "v2"})
	if !errors.As(err, &de) || de.Code != RFBErrNotReady {
		t.Errorf("CONCLUIDA com download 404 deveria ser NOT_READY, veio %v", err)
	}
	_, err = BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "t-exp", APIVersao: "v2"})
	if !errors.As(err, &de) || de.Code != "EXPIRADA" {
		t.Errorf("EXPIRADA deveria devolver o código da RFB, veio %v", err)
	}
}

func TestRFBPoller_BackoffEEstado(t *testing.T) {
	if rfbPollIntervalo(10*time.Minute) != 2*time.Minute ||
		rfbPollIntervalo(time.Hour) != 5*time.Minute ||
		rfbPollIntervalo(3*time.Hour) != 10*time.Minute {
		t.Errorf("backoff por idade inesperado")
	}
	p := novoRFBPoller()
	t0 := time.Now()
	if !p.devido("a", t0) {
		t.Errorf("solicitação nova deve estar devida")
	}
	p.agendar("a", t0, rfbPollIntervalo(10*time.Minute))
	if p.devido("a", t0.Add(time.Minute)) || !p.devido("a", t0.Add(2*time.Minute)) {
		t.Errorf("agendada para +2 min: não devida em +1, devida em +2")
	}
	p.desistir("a")
	if p.devido("a", t0.Add(24*time.Hour)) {
		t.Errorf("solicitação que desistiu nunca mais é devida")
	}
	p.podar(map[string]bool{})
	if !p.devido("a", t0) {
		t.Errorf("podar deve esquecer solicitações fora da janela")
	}
}

func TestRFBPollSemAcesso(t *testing.T) {
	v2 := rfbPollItem{id: "x", tipo: "debito", apiVersao: "v2", ambiente: "producao"}
	v1 := rfbPollItem{id: "y", tipo: "debito", apiVersao: "v1", ambiente: "producao"}
	for _, st := range []int{401, 403, 404} {
		p := novoRFBPoller()
		if !rfbPollSemAcesso(&RFBHTTPError{Op: "situacao", Status: st, Body: "b"}, v2, p) || p.devido("x", time.Now()) {
			t.Errorf("v2 HTTP %d deveria ser diagnóstico e parar de consultar", st)
		}
		// v1: trata (true) mas NÃO desiste — segue tentando a cada 10 min.
		if !rfbPollSemAcesso(&RFBHTTPError{Op: "situacao", Status: st, Body: "b"}, v1, p) || !p.devido("y", time.Now()) {
			t.Errorf("v1 HTTP %d deveria ser tratado sem desistir", st)
		}
	}
	p := novoRFBPoller()
	if rfbPollSemAcesso(&RFBHTTPError{Op: "situacao", Status: 503}, v2, p) || !p.devido("x", time.Now()) {
		t.Errorf("5xx é transitório: não desiste")
	}
	if rfbPollSemAcesso(errors.New("timeout"), v2, p) {
		t.Errorf("erro de rede não é diagnóstico de acesso")
	}
}

func TestRFBPoller_LogSoQuandoARespostaMuda(t *testing.T) {
	p := novoRFBPoller()
	if !p.mudou("a", "situacao", "403|x") {
		t.Errorf("primeira resposta deve logar")
	}
	if p.mudou("a", "situacao", "403|x") {
		t.Errorf("a mesma resposta não deve logar de novo")
	}
	if !p.mudou("a", "situacao", "200|ok") {
		t.Errorf("resposta diferente deve logar")
	}
	if !p.mudou("a", "download", "403|x") {
		t.Errorf("canais independentes: situação e download logam cada um a sua")
	}
}

func TestRFBPollIntervaloPara(t *testing.T) {
	if rfbPollIntervaloPara(rfbPollItem{apiVersao: "v1"}, time.Minute) != 10*time.Minute ||
		rfbPollIntervaloPara(rfbPollItem{apiVersao: "v1"}, 3*time.Hour) != 10*time.Minute {
		t.Errorf("v1: 10 min fixos")
	}
	if rfbPollIntervaloPara(rfbPollItem{apiVersao: "v2"}, time.Minute) != 2*time.Minute {
		t.Errorf("v2: backoff por idade")
	}
}

// ── Integração (banco) ──

// insertRFBRequestRequested cria uma solicitação 'requested' sem fonte de download, como a
// v1 deixa quando o webhook não chega. criadaHa recua created_at.
func insertRFBRequestRequested(t *testing.T, db *sql.DB, companyID, tiquete, tipo, apiVersao string, criadaHa time.Duration) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, ambiente, api_versao, created_at, updated_at)
		VALUES ($1, '00000000', $2, 'requested', $3, 'producao', $4, NOW() - $5::interval, NOW()) RETURNING id
	`, companyID, tiquete, tipo, apiVersao, fmt.Sprintf("%d seconds", int(criadaHa.Seconds()))).Scan(&id); err != nil {
		t.Fatalf("falha ao criar rfb_requests requested: %v", err)
	}
	return id
}

func TestPoller_ConcluidaSemWebhookBaixaEProcessa(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	var fake *v2Fake
	fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apuracao-cbs/v2/situacao/tiq-poll-1":
			fmt.Fprint(w, `{"situacao":"CONCLUIDA"}`)
		case "/apuracao-cbs/v2/download/tiq-poll-1":
			fmt.Fprintf(w, `{"urlAssinada":"%s/arquivo?sig=1","urlAssinadaExpiraEm":"2099-01-01T00:00:00Z"}`, fake.URL)
		case "/arquivo":
			fmt.Fprint(w, v2DebitoBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	// Solicitação da rota LEGADA (v1) sem webhook: o poller é o que a destrava.
	id := insertRFBRequestRequested(t, db, companyID, "tiq-poll-1", "debito", "v1", 10*time.Minute)

	pollarSituacaoRFB(db, novoRFBPoller(), time.Now)
	waitAsyncSAPSync()

	if status, ec, em, _ := readRequest(t, db, id); status != "completed" {
		t.Fatalf("esperava completed sem webhook, veio %s (%s %s)", status, ec, em)
	}
	if a, ok := fake.authFor("/arquivo"); !ok || a != "" {
		t.Errorf("GET da urlAssinada não pode levar Authorization (ok=%v auth=%q)", ok, a)
	}
}

func TestPoller_AcessoNegadoNaoMexeNaLinhaEDesiste(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"You cannot consume this service"}`)
	})
	id := insertRFBRequestRequested(t, db, companyID, "tiq-poll-2", "debito", "v2", 10*time.Minute)
	p := novoRFBPoller()

	agora := time.Now()
	pollarSituacaoRFB(db, p, relogio(agora))
	pollarSituacaoRFB(db, p, relogio(agora.Add(30*time.Minute)))

	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 1 {
		t.Errorf("403 deve consultar uma única vez e desistir, consultou %d: %v", got, fake.snapshot())
	}
	if status, ec, _, _ := readRequest(t, db, id); status != "requested" || ec != "" {
		t.Errorf("linha deve ficar intacta, veio %s/%s", status, ec)
	}
}

func TestPoller_EmAndamentoRespeitaBackoff(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"situacao":"EM_PROCESSAMENTO"}`)
	})
	id := insertRFBRequestRequested(t, db, companyID, "tiq-poll-3", "debito", "v2", 10*time.Minute)
	p := novoRFBPoller()

	agora := time.Now()
	pollarSituacaoRFB(db, p, relogio(agora))
	pollarSituacaoRFB(db, p, relogio(agora.Add(time.Minute))) // ainda dentro do backoff de 2 min
	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 1 {
		t.Errorf("dentro do backoff não consulta de novo, consultou %d", got)
	}
	pollarSituacaoRFB(db, p, relogio(agora.Add(3*time.Minute)))
	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 2 {
		t.Errorf("após o backoff deve consultar de novo, consultou %d", got)
	}
	if status, _, _, _ := readRequest(t, db, id); status != "requested" {
		t.Errorf("em andamento não altera a linha, veio %s", status)
	}
}

func TestPoller_ErroDaRFBViraErroNaLinha(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"situacao":"ERRO","codigoErro":"E-RFB-9","mensagemErro":"falha interna na RFB"}`)
	})
	id := insertRFBRequestRequested(t, db, companyID, "tiq-poll-4", "debito", "v1", 10*time.Minute)

	pollarSituacaoRFB(db, novoRFBPoller(), time.Now)

	if status, ec, em, _ := readRequest(t, db, id); status != "error" || ec != "E-RFB-9" || em != "falha interna na RFB" {
		t.Errorf("esperava error/E-RFB-9, veio %s/%s/%s", status, ec, em)
	}
}

func TestPoller_ConcluidaMasDownload404Reagenda(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apuracao-cbs/v2/situacao/tiq-poll-5" {
			fmt.Fprint(w, `{"situacao":"CONCLUIDA"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"codigoErro":"ARQUIVO_INDISPONIVEL"}`)
	})
	id := insertRFBRequestRequested(t, db, companyID, "tiq-poll-5", "debito", "v2", 10*time.Minute)
	p := novoRFBPoller()

	agora := time.Now()
	pollarSituacaoRFB(db, p, relogio(agora))
	pollarSituacaoRFB(db, p, relogio(agora.Add(3*time.Minute)))

	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 2 {
		t.Errorf("download 404 não é definitivo: deve reconsultar, consultou %d", got)
	}
	if status, ec, _, _ := readRequest(t, db, id); status != "requested" || ec != "" {
		t.Errorf("linha deve ficar intacta, veio %s/%s", status, ec)
	}
}

// relogio devolve um relógio fixo para os testes controlarem o backoff.
func relogio(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestObterURLDownload_401RenovaTokenERepete1x(t *testing.T) {
	var chamadas int
	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		if r.Header.Get("Authorization") != "Bearer tok-v2" { // token do fake de /token
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"codigoErro":"NAO_AUTORIZADO","mensagemErro":"consumidor inválido"}`)
			return
		}
		fmt.Fprint(w, `{"urlAssinada":"https://s3.exemplo/arq"}`)
	})
	c := NewRFBClient()
	if _, err := c.GetToken("cid-401-download", "sec"); err != nil { // registra as credenciais no client
		t.Fatalf("token: %v", err)
	}
	u, _, err := c.ObterURLDownload("token-vencido", "tiq")
	if err != nil || u != "https://s3.exemplo/arq" || chamadas != 2 {
		t.Errorf("deveria renovar e repetir 1x: url=%q err=%v chamadas=%d", u, err, chamadas)
	}
}

func TestBaixar_ConcluidaSemArquivoCaiNoTiqueteDeDownload(t *testing.T) {
	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apuracao-cbs/v2/situacao/t-1":
			fmt.Fprint(w, `{"situacao":"CONCLUIDA"}`)
		case "/rtc/download/v1/tdl-1":
			fmt.Fprint(w, "via-tiquete-de-download")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c := v2Client(t, "producao")
	data, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "t-1", TiqueteDownload: "tdl-1", APIVersao: "v2"})
	if err != nil || string(data) != "via-tiquete-de-download" {
		t.Errorf("404 do download não pode pular o degrau do tíquete de download: data=%q err=%v", data, err)
	}
}

func TestPoller_CredencialAusenteDesisteSemLogRepetido(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup() // sem insertRFBCredential de propósito
	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	id := insertRFBRequestRequested(t, db, companyID, "tiq-poll-6", "debito", "v1", 10*time.Minute)
	p := novoRFBPoller()

	agora := time.Now()
	pollarSituacaoRFB(db, p, relogio(agora))
	if p.devido(id, agora.Add(time.Hour)) {
		t.Errorf("sem credencial ativa o poller deve desistir da linha")
	}
	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 0 {
		t.Errorf("sem credencial não há consulta à RFB, houve %d", got)
	}
}

// O webhook pode concluir a linha enquanto o poller espera a resposta da RFB: as escritas do
// poller não podem sobrescrever o estado nem deixar uma urlAssinada em linha 'completed'.
func TestPoller_WebhookConcluiDuranteAConsulta_NaoSobrescreve(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	var id string
	concluirPeloWebhook := func() {
		if _, err := db.Exec(`UPDATE rfb_requests SET status = 'completed' WHERE id = $1`, id); err != nil {
			t.Errorf("simular webhook: %v", err)
		}
	}
	var fake *v2Fake
	fake = newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apuracao-cbs/v2/situacao/tiq-corrida-erro":
			concluirPeloWebhook()
			fmt.Fprint(w, `{"situacao":"ERRO","codigoErro":"E-X","mensagemErro":"x"}`)
		case "/apuracao-cbs/v2/situacao/tiq-corrida-url":
			fmt.Fprint(w, `{"situacao":"CONCLUIDA"}`)
		case "/apuracao-cbs/v2/download/tiq-corrida-url":
			concluirPeloWebhook()
			fmt.Fprintf(w, `{"urlAssinada":"%s/arq"}`, fake.URL)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	withSSRFBypass(t)

	for _, tiq := range []string{"tiq-corrida-erro", "tiq-corrida-url"} {
		id = insertRFBRequestRequested(t, db, companyID, tiq, "debito", "v1", 10*time.Minute)
		pollarSituacaoRFB(db, novoRFBPoller(), time.Now)
		status, ec, _, url := readRequest(t, db, id)
		if status != "completed" || ec != "" || url.Valid {
			t.Errorf("%s: linha concluída pelo webhook deve ficar intacta, veio status=%s code=%q url=%v", tiq, status, ec, url.Valid)
		}
		db.Exec(`DELETE FROM rfb_requests WHERE id = $1`, id)
	}
}

// v1 sem resposta: reconsulta a situação E sonda o download a cada 10 min, sem desistir nem tocar na linha.
func TestPoller_V1SemResposta_Reconsulta10MinESondaDownload(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/apuracao-cbs/v2/situacao/"):
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"You cannot consume this service"}`)
		case strings.HasPrefix(r.URL.Path, "/rtc/download/v1/"):
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"mensagem":"Tíquete inexistente ou download já realizado"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	id := insertRFBRequestRequested(t, db, companyID, "tiq-v1-sonda", "debito", "v1", 10*time.Minute)
	p := novoRFBPoller()

	t0 := time.Now()
	pollarSituacaoRFB(db, p, relogio(t0))
	pollarSituacaoRFB(db, p, relogio(t0.Add(9*time.Minute))) // ainda dentro dos 10 min
	pollarSituacaoRFB(db, p, relogio(t0.Add(10*time.Minute)))
	pollarSituacaoRFB(db, p, relogio(t0.Add(20*time.Minute)))

	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 3 {
		t.Errorf("esperava 3 consultas de situação (t0, +10, +20), houve %d: %v", got, fake.snapshot())
	}
	if got := fake.count("GET /rtc/download/v1/"); got != 3 {
		t.Errorf("esperava 3 sondas de download v1, houve %d", got)
	}
	if status, ec, _, url := readRequest(t, db, id); status != "requested" || ec != "" || url.Valid {
		t.Errorf("linha deve ficar intacta, veio %s/%s url=%v", status, ec, url.Valid)
	}
}

// A sonda recebe o arquivo: processa sem baixar de novo (o tíquete v1 é de uso único).
func TestPoller_V1SondaRecebeArquivo_ProcessaSemBaixarDeNovo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	var downloads int
	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/apuracao-cbs/v2/situacao/"):
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"You cannot consume this service"}`)
		case r.URL.Path == "/rtc/download/v1/tiq-v1-arq":
			downloads++
			fmt.Fprint(w, v2DebitoBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	id := insertRFBRequestRequested(t, db, companyID, "tiq-v1-arq", "debito", "v1", 10*time.Minute)

	pollarSituacaoRFB(db, novoRFBPoller(), time.Now)
	waitAsyncSAPSync()

	if status, ec, em, _ := readRequest(t, db, id); status != "completed" {
		t.Fatalf("esperava completed, veio %s (%s %s)", status, ec, em)
	}
	if downloads != 1 {
		t.Errorf("o tíquete v1 é de uso único: esperava 1 download, houve %d (%v)", downloads, fake.snapshot())
	}
}

// Linha v1 cai em PENDENTE (situação responde 200): segue a cadência de 10 min, sem sonda.
func TestPoller_V1SituacaoRespondeNaoFinal_Cadencia10Min(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"situacao":"PENDENTE"}`)
	})
	insertRFBRequestRequested(t, db, companyID, "tiq-v1-pend", "debito", "v1", 10*time.Minute)
	p := novoRFBPoller()

	t0 := time.Now()
	pollarSituacaoRFB(db, p, relogio(t0))
	pollarSituacaoRFB(db, p, relogio(t0.Add(3*time.Minute)))
	pollarSituacaoRFB(db, p, relogio(t0.Add(10*time.Minute)))
	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 2 {
		t.Errorf("v1: consulta em t0 e em +10 min (não em +3), houve %d", got)
	}
	if got := fake.count("GET /rtc/download/v1/"); got != 0 {
		t.Errorf("situação respondeu 200: não precisa sondar o download, houve %d", got)
	}
}

// 200 que não é o arquivo (HTML/"processando"): não vira PARSE_ERROR, a linha segue 'requested' e o
// que veio fica em raw_json (o tíquete de uso único já foi gasto pelo GET).
func TestPoller_V1SondaCorpoNaoJSON_GuardaEMantemRequested(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/apuracao-cbs/v2/situacao/"):
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"You cannot consume this service"}`)
		case r.URL.Path == "/rtc/download/v1/tiq-v1-html":
			fmt.Fprint(w, "<html>processando</html>")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	id := insertRFBRequestRequested(t, db, companyID, "tiq-v1-html", "debito", "v1", 10*time.Minute)

	pollarSituacaoRFB(db, novoRFBPoller(), time.Now)

	status, ec, _, _ := readRequest(t, db, id)
	var raw sql.NullString
	db.QueryRow(`SELECT raw_json FROM rfb_requests WHERE id = $1`, id).Scan(&raw)
	if status != "requested" || ec != "" || raw.String != "<html>processando</html>" {
		t.Errorf("esperava requested sem erro e raw_json guardado, veio %s/%s raw=%q", status, ec, raw.String)
	}
}

// A sonda não gasta o tíquete se o webhook/download manual já assumiu a linha.
func TestSondarDownloadV1_LinhaJaAssumidaNaoBaixa(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, v2DebitoBody) })
	id := insertRFBRequestRequested(t, db, companyID, "tiq-v1-assumida", "debito", "v1", 10*time.Minute)
	db.Exec(`UPDATE rfb_requests SET status = 'webhook_received', tiquete_download = 'tdl' WHERE id = $1`, id)

	it := rfbPollItem{id: id, companyID: companyID, tiquete: "tiq-v1-assumida", tipo: "debito", ambiente: "producao", apiVersao: "v1"}
	sondarDownloadV1(db, novoRFBPoller(), it, NewRFBClient(), "tok")

	if got := fake.count("GET /rtc/download/v1/"); got != 0 {
		t.Errorf("linha já assumida: não pode baixar (gastaria o tíquete), houve %d GET", got)
	}
}

// Com o arquivo já em mãos o processador não exige credencial nem token: não perde o dado.
func TestProcessarDownloadComArquivo_SemCredencialNemToken(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup() // sem insertRFBCredential de propósito
	id := insertRFBRequestRequested(t, db, companyID, "tiq-v1-sem-cred", "debito", "v1", 10*time.Minute)

	if err := ProcessarDownloadRFBComArquivo(db, NewRFBClient(), id, []byte(v2DebitoBody)); err != nil {
		t.Fatalf("com arquivo em mãos não deveria falhar por credencial: %v", err)
	}
	waitAsyncSAPSync()
	if status, ec, em, _ := readRequest(t, db, id); status != "completed" {
		t.Errorf("esperava completed, veio %s (%s %s)", status, ec, em)
	}
}
