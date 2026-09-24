package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
)

// insertRFBReq cria uma solicitação de teste. status 'requested' = aguardando webhook.
func insertRFBReq(t *testing.T, db *sql.DB, companyID, tiquete, tipo, status string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, api_versao)
		VALUES ($1, '00000000', $2, $3, $4, 'v2') RETURNING id
	`, companyID, tiquete, status, tipo).Scan(&id); err != nil {
		t.Fatalf("falha ao criar rfb_requests: %v", err)
	}
	return id
}

func postWebhook(t *testing.T, db *sql.DB, payload map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	t.Setenv("RFB_WEBHOOK_SECRET", "") // sem segredo: handler pula HMAC (coberto por outro caminho)
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/rfb/webhook", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	RFBWebhookHandler(db)(rec, req)
	return rec
}

type reqRow struct {
	Status, ErrCode, ErrMsg, TiqDownload, URL string
	Exp                                       sql.NullTime
}

func readReqRow(t *testing.T, db *sql.DB, id string) reqRow {
	t.Helper()
	var r reqRow
	var ec, em, td, u sql.NullString
	if err := db.QueryRow(`
		SELECT status, error_code, error_message, tiquete_download, url_assinada, url_assinada_expira_em
		FROM rfb_requests WHERE id = $1`, id).Scan(&r.Status, &ec, &em, &td, &u, &r.Exp); err != nil {
		t.Fatalf("ler request: %v", err)
	}
	r.ErrCode, r.ErrMsg, r.TiqDownload, r.URL = ec.String, em.String, td.String, u.String
	return r
}

// O webhook dispara o processor em goroutine; a empresa de teste não tem credenciais RFB,
// então a goroutine termina rápido em CRED_NOT_FOUND (sem rede). Espera isso para o
// cleanup não correr contra ela.
func waitProcessorSettled(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if readReqRow(t, db, id).Status == "error" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("processor não assentou em 5s")
		}
		time.Sleep(30 * time.Millisecond)
	}
}

// holdProcessor dá à empresa uma credencial RFB ativa e aponta o token para um servidor que
// bloqueia até release(): o processor disparado pelo webhook trava em 'downloading' (após
// gravar url/tíquete), permitindo inspecionar o que o webhook persistiu antes de o erro
// terminal (que zera url_assinada) rodar. release() destrava (o token vira 500 → TOKEN_ERROR).
func holdProcessor(t *testing.T, db *sql.DB, companyID string) (release func()) {
	t.Helper()
	ch := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-ch
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Setenv("RFB_API_URL", srv.URL)
	t.Setenv("RFB_TOKEN_URL", srv.URL+"/token")
	if _, err := db.Exec(`
		INSERT INTO rfb_credentials (company_id, cnpj_matriz, client_id, client_secret, ambiente, ativo)
		VALUES ($1, $2, $3, 'secret-teste', 'producao', true)
	`, companyID, fmt.Sprintf("%08d000199", time.Now().UnixNano()%100000000), "cid-wh-"+companyID); err != nil {
		t.Fatalf("criar credencial: %v", err)
	}
	var once sync.Once
	release = func() { once.Do(func() { close(ch) }) }
	t.Cleanup(func() { release(); srv.Close() })
	return release
}

func waitStatus(t *testing.T, db *sql.DB, id, want string) reqRow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		row := readReqRow(t, db, id)
		if row.Status == want {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %q não alcançado em 5s (atual %q)", want, row.Status)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestRFBWebhook_V1_TiqueteDownload(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBReq(t, db, companyID, "tiq-wh-v1", "debito", "requested")

	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-wh-v1", "tiqueteDownload": "tiq-dl-v1"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "processing") {
		t.Fatalf("esperava 200/processing, veio %d %s", rec.Code, rec.Body.String())
	}
	waitProcessorSettled(t, db, id)
	row := readReqRow(t, db, id)
	if row.TiqDownload != "tiq-dl-v1" || row.URL != "" {
		t.Errorf("v1: esperava tiquete_download salvo e sem url, veio %+v", row)
	}
}

func TestRFBWebhook_V2_UrlAssinada(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	release := holdProcessor(t, db, companyID)
	id := insertRFBReq(t, db, companyID, "tiq-wh-v2", "credito", "requested")

	rec := postWebhook(t, db, map[string]interface{}{
		"tiqueteSolicitacao":  "tiq-wh-v2",
		"urlAssinada":         "https://arquivos.rfb.example/f?sig=SEGREDO",
		"urlAssinadaExpiraEm": "2030-05-01T12:00:00Z",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "processing") {
		t.Fatalf("esperava 200/processing, veio %d %s", rec.Code, rec.Body.String())
	}
	row := waitStatus(t, db, id, "downloading")
	if row.URL != "https://arquivos.rfb.example/f?sig=SEGREDO" || !row.Exp.Valid || row.Exp.Time.UTC().Year() != 2030 {
		t.Errorf("v2: url/expiração não persistidas: %+v", row)
	}
	if row.TiqDownload != "" {
		t.Errorf("v2 sem tiqueteDownload não deve gravar tiquete_download, veio %q", row.TiqDownload)
	}
	release()
	waitProcessorSettled(t, db, id)
	if after := readReqRow(t, db, id); after.URL != "" || after.Exp.Valid {
		t.Errorf("erro terminal (TOKEN_ERROR) deve zerar url_assinada*: %+v", after)
	}
}

// urlAssinada inválida sem nada mais utilizável: linha requested/error vira error URL_INVALIDA
// (não fica esperando webhook até o watchdog) e nenhum download é disparado.
func TestRFBWebhook_V2_RejeitaUrlInsegura(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	for i, u := range []string{
		"http://arquivos.rfb.example/f?sig=x",
		"https://10.0.0.5/f?sig=x",
		"https://127.0.0.1/f",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/f",
		"https://[64:ff9b::7f00:1]/f",
		"https://[2002:7f00:1::1]/f",
		"https://198.51.100.7/f",
		"https://arquivos.rfb.example:8443/f",
		"https://user:pw@arquivos.rfb.example/f",
	} {
		id := insertRFBReq(t, db, companyID, fmt.Sprintf("tiq-bad-%d", i), "debito", "requested")
		rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": fmt.Sprintf("tiq-bad-%d", i), "urlAssinada": u})
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "invalid urlAssinada") {
			t.Errorf("%s: esperava 200/invalid urlAssinada, veio %d %s", u, rec.Code, rec.Body.String())
		}
		row := readReqRow(t, db, id)
		if row.Status != "error" || row.ErrCode != "URL_INVALIDA" || row.URL != "" {
			t.Errorf("%s: esperava error/URL_INVALIDA sem url, veio %+v", u, row)
		}
	}

	// linha em webhook_received NÃO é rebaixada por um webhook com URL inválida
	idW := insertRFBReq(t, db, companyID, "tiq-bad-w", "debito", "webhook_received")
	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-bad-w", "urlAssinada": "http://x.example/f"})
	if s := readReqRow(t, db, idW).Status; s != "webhook_received" {
		t.Errorf("webhook_received não deve mudar por URL inválida, veio %s", s)
	}
}

// URL inválida NÃO descarta um tiqueteDownload válido do mesmo payload.
func TestRFBWebhook_UrlInvalidaMantemTiqueteDownload(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	release := holdProcessor(t, db, companyID)
	id := insertRFBReq(t, db, companyID, "tiq-mix", "debito", "requested")

	rec := postWebhook(t, db, map[string]interface{}{
		"tiqueteSolicitacao": "tiq-mix", "tiqueteDownload": "tiq-dl-mix", "urlAssinada": "http://127.0.0.1/f?sig=x",
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "processing") {
		t.Fatalf("esperava seguir por tiqueteDownload (processing), veio %d %s", rec.Code, rec.Body.String())
	}
	row := waitStatus(t, db, id, "downloading")
	if row.TiqDownload != "tiq-dl-mix" || row.URL != "" || row.ErrCode != "" {
		t.Errorf("esperava tiquete_download salvo e URL descartada, veio %+v", row)
	}
	release()
	waitProcessorSettled(t, db, id)
}

// urlAssinada só vale para linhas api_versao='v2': em linha v1 é ignorada (com log).
func TestRFBWebhook_UrlAssinadaEmLinhaV1Ignorada(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	release := holdProcessor(t, db, companyID)

	// só urlAssinada → nada utilizável → URL_INVALIDA
	idA := insertRFBReq(t, db, companyID, "tiq-v1-a", "debito", "requested")
	db.Exec(`UPDATE rfb_requests SET api_versao = 'v1' WHERE id = $1`, idA)
	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-v1-a", "urlAssinada": "https://arquivos.rfb.example/f?sig=x"})
	if !strings.Contains(rec.Body.String(), "invalid urlAssinada") {
		t.Errorf("v1 só com urlAssinada: esperava warning, veio %s", rec.Body.String())
	}
	if row := readReqRow(t, db, idA); row.Status != "error" || row.ErrCode != "URL_INVALIDA" || row.URL != "" {
		t.Errorf("v1 só com urlAssinada: esperava error/URL_INVALIDA sem url, veio %+v", row)
	}

	// urlAssinada + tiqueteDownload → segue pelo tíquete, URL não é gravada
	idB := insertRFBReq(t, db, companyID, "tiq-v1-b", "debito", "requested")
	db.Exec(`UPDATE rfb_requests SET api_versao = 'v1' WHERE id = $1`, idB)
	rec = postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-v1-b", "tiqueteDownload": "dl-b", "urlAssinada": "https://arquivos.rfb.example/f?sig=x"})
	if !strings.Contains(rec.Body.String(), "processing") {
		t.Fatalf("v1 com tiqueteDownload: esperava processing, veio %s", rec.Body.String())
	}
	row := waitStatus(t, db, idB, "downloading")
	if row.URL != "" || row.TiqDownload != "dl-b" {
		t.Errorf("v1: url deve ser ignorada e tiquete_download salvo, veio %+v", row)
	}
	release()
	waitProcessorSettled(t, db, idB)
}

// Webhook duplicado com a linha já em downloading/completed/reprocessing não dispara 2º download.
func TestRFBWebhook_DuplicadoNaoRedispara(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	for _, st := range []string{"downloading", "completed", "reprocessing"} {
		id := insertRFBReq(t, db, companyID, "tiq-dup-"+st, "debito", st)
		rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-dup-" + st, "tiqueteDownload": "dl", "urlAssinada": "https://arquivos.rfb.example/f"})
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "processing") {
			t.Errorf("%s: não deveria despachar download, veio %d %s", st, rec.Code, rec.Body.String())
		}
		time.Sleep(100 * time.Millisecond) // uma goroutine indevida teria virado 'error' (CRED_NOT_FOUND)
		row := readReqRow(t, db, id)
		if row.Status != st || row.URL != "" || row.TiqDownload != "" {
			t.Errorf("%s: linha não deve ser alterada, veio %+v", st, row)
		}
	}
}

func TestRFBWebhook_ExpiracaoIlegivelUsaTTLConservador(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	release := holdProcessor(t, db, companyID)

	// ilegível → ~1h (nunca NULL)
	idA := insertRFBReq(t, db, companyID, "tiq-exp-a", "debito", "requested")
	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-exp-a", "urlAssinada": "https://arquivos.rfb.example/a", "urlAssinadaExpiraEm": "quando der"})
	rowA := waitStatus(t, db, idA, "downloading")
	if !rowA.Exp.Valid || time.Until(rowA.Exp.Time) < 50*time.Minute || time.Until(rowA.Exp.Time) > 70*time.Minute {
		t.Errorf("expiração ilegível deveria virar ~agora+1h, veio %+v", rowA.Exp)
	}

	// sem offset: assume America/Sao_Paulo (UTC-3)
	idB := insertRFBReq(t, db, companyID, "tiq-exp-b", "debito", "requested")
	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-exp-b", "urlAssinada": "https://arquivos.rfb.example/b", "urlAssinadaExpiraEm": "2030-05-01T12:00:00"})
	rowB := waitStatus(t, db, idB, "downloading")
	if !rowB.Exp.Valid || rowB.Exp.Time.UTC().Hour() != 15 {
		t.Errorf("sem offset deveria ser BRT (15:00Z), veio %+v", rowB.Exp)
	}

	// já há expiração gravada e o novo webhook não traz nenhuma → não sobrescreve com NULL
	idC := insertRFBReq(t, db, companyID, "tiq-exp-c", "debito", "requested")
	db.Exec(`UPDATE rfb_requests SET url_assinada_expira_em = '2031-01-01T00:00:00Z' WHERE id = $1`, idC)
	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-exp-c", "urlAssinada": "https://arquivos.rfb.example/c"})
	rowC := waitStatus(t, db, idC, "downloading")
	if !rowC.Exp.Valid || rowC.Exp.Time.UTC().Year() != 2031 {
		t.Errorf("expiração existente não pode ser sobrescrita por NULL, veio %+v", rowC.Exp)
	}
	release()
	for _, id := range []string{idA, idB, idC} {
		waitProcessorSettled(t, db, id)
	}
}

// codigoErro + urlAssinada/tiqueteDownload no mesmo payload: sucesso vence (documentado).
func TestRFBWebhook_ErroJuntoDeUrlTrataComoSucesso(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	release := holdProcessor(t, db, companyID)
	id := insertRFBReq(t, db, companyID, "tiq-both", "debito", "requested")
	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-both", "codigoErro": "X1", "mensagemErro": "aviso", "urlAssinada": "https://arquivos.rfb.example/f"})
	if !strings.Contains(rec.Body.String(), "processing") {
		t.Fatalf("esperava processing, veio %s", rec.Body.String())
	}
	row := waitStatus(t, db, id, "downloading")
	if row.ErrCode != "" || row.URL == "" {
		t.Errorf("sucesso deve limpar erro e guardar url: %+v", row)
	}
	release()
	waitProcessorSettled(t, db, id)
}

func TestRFBWebhook_Erro_ComTiquete(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBReq(t, db, companyID, "tiq-err", "debito", "requested")
	done := insertRFBReq(t, db, companyID, "tiq-err-done", "debito", "completed")
	dling := insertRFBReq(t, db, companyID, "tiq-err-dl", "debito", "downloading")

	longCode := strings.Repeat("E", 80)
	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-err", "codigoErro": longCode, "mensagemErro": "CNPJ sem procuração"})
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d", rec.Code)
	}
	row := readReqRow(t, db, id)
	if row.Status != "error" || row.ErrCode != strings.Repeat("E", 50) || row.ErrMsg != "CNPJ sem procuração" {
		t.Errorf("esperava error/E*50/mensagem, veio %+v", row)
	}

	// completed/downloading/webhook_received não são sobrescritos por um erro tardio
	wr := insertRFBReq(t, db, companyID, "tiq-err-wr", "debito", "webhook_received")
	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-err-wr", "codigoErro": "X", "mensagemErro": "y"})
	if s := readReqRow(t, db, wr).Status; s != "webhook_received" {
		t.Errorf("webhook_received foi rebaixado por erro tardio: %s", s)
	}
	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-err-done", "codigoErro": "X", "mensagemErro": "y"})
	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-err-dl", "codigoErro": "X", "mensagemErro": "y"})
	if s := readReqRow(t, db, done).Status; s != "completed" {
		t.Errorf("completed foi sobrescrito: %s", s)
	}
	if s := readReqRow(t, db, dling).Status; s != "downloading" {
		t.Errorf("downloading foi sobrescrito: %s", s)
	}
}

func TestRFBWebhook_Erro_SemTiquete(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBReq(t, db, companyID, "tiq-sem", "debito", "requested")

	rec := postWebhook(t, db, map[string]interface{}{"codigoErro": "E1", "mensagemErro": "algo falhou"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "without tiqueteSolicitacao") {
		t.Fatalf("esperava 200 com aviso, veio %d %s", rec.Code, rec.Body.String())
	}
	if s := readReqRow(t, db, id).Status; s != "requested" {
		t.Errorf("nenhuma request deveria mudar, status=%s", s)
	}
}

// codigoErro/mensagemErro multibyte na fronteira de truncamento gravam sem erro de UTF-8;
// o erro também limpa url_assinada.
func TestRFBWebhook_Erro_MultibyteNaFronteiraEUrlLimpa(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBReq(t, db, companyID, "tiq-mb", "debito", "requested")
	db.Exec(`UPDATE rfb_requests SET url_assinada = 'https://arquivos.rfb.example/f', url_assinada_expira_em = now() WHERE id = $1`, id)

	code := strings.Repeat("E", 49) + "ãããã"   // 53 runes, o 50º é 'ã' (2 bytes)
	msg := strings.Repeat("a", 999) + "çççççç" // 1005 runes
	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-mb", "codigoErro": code, "mensagemErro": msg})
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d", rec.Code)
	}
	row := readReqRow(t, db, id)
	if row.Status != "error" {
		t.Fatalf("esperava error (gravação não pode falhar por UTF-8), veio %+v", row)
	}
	if utf8.RuneCountInString(row.ErrCode) != 50 || !utf8.ValidString(row.ErrCode) || !strings.HasSuffix(row.ErrCode, "ã") {
		t.Errorf("codigoErro: %d runes %q", utf8.RuneCountInString(row.ErrCode), row.ErrCode)
	}
	if utf8.RuneCountInString(row.ErrMsg) != 1000 || !utf8.ValidString(row.ErrMsg) {
		t.Errorf("mensagemErro: %d runes", utf8.RuneCountInString(row.ErrMsg))
	}
	if row.URL != "" || row.Exp.Valid {
		t.Errorf("erro deve limpar url_assinada*: %+v", row)
	}
}

// Campo presente com tipo errado é logado e tratado como ausente (não lido como vazio em silêncio).
func TestWebhookStringField_TipoErrado(t *testing.T) {
	p := map[string]interface{}{"a": "  x ", "b": float64(12), "c": map[string]interface{}{"k": "v"}, "d": nil}
	if webhookStringField(p, "a") != "x" || webhookStringField(p, "b") != "" || webhookStringField(p, "c") != "" ||
		webhookStringField(p, "d") != "" || webhookStringField(p, "ausente") != "" {
		t.Error("leitura de campos inesperada")
	}
	db := openTestDB(t)
	defer db.Close()
	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": float64(123), "tiqueteDownload": "d"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "missing") {
		t.Errorf("tiqueteSolicitacao numérico deve ser tratado como ausente: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRFBWebhook_SemNadaUtil(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "so-tiquete"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "missing") {
		t.Errorf("esperava 200/missing, veio %d %s", rec.Code, rec.Body.String())
	}
}

func TestRedactedWebhookBody_NaoVazaUrl(t *testing.T) {
	out := redactedWebhookBody(map[string]interface{}{
		"tiqueteSolicitacao": "T", "urlAssinada": "https://arquivos.rfb.example/f?sig=SEGREDO",
	})
	if strings.Contains(out, "SEGREDO") || strings.Contains(out, "/f?") || !strings.Contains(out, "arquivos.rfb.example") {
		t.Errorf("payload redigido inadequado: %s", out)
	}
}

func TestRedactedWebhookBody_RecursivoQualquerUrl(t *testing.T) {
	out := redactedWebhookBody(map[string]interface{}{
		"tiqueteSolicitacao": "T",
		"link":               "HTTPS://cdn.rfb.example/a?sig=SEGREDO1",
		"aninhado":           map[string]interface{}{"x": "http://storage.example/b?token=SEGREDO2", "lista": []interface{}{"https://c.example/?k=SEGREDO3", "ok"}},
	})
	for _, seg := range []string{"SEGREDO1", "SEGREDO2", "SEGREDO3", "sig=", "token="} {
		if strings.Contains(out, seg) {
			t.Errorf("payload redigido vaza %q: %s", seg, out)
		}
	}
	if !strings.Contains(out, "cdn.rfb.example") || !strings.Contains(out, `"ok"`) {
		t.Errorf("host e valores comuns deveriam permanecer: %s", out)
	}
	if got := redactURLsInText(`{"urlAssinada":"https://a.example/x?sig=SEGREDO`); strings.Contains(got, "SEGREDO") {
		t.Errorf("redactURLsInText vaza: %s", got)
	}
}

// ── Ressolicitar e download manual de créditos (precisam de usuário dono da empresa) ──

func setupOwner(t *testing.T, db *sql.DB, companyID string) (userID string, cleanup func()) {
	t.Helper()
	if err := db.QueryRow(`
		INSERT INTO users (email, password_hash, full_name) VALUES ('teste-rfb-v2@example.invalid', 'x', 'Teste RFB v2') RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("criar usuário: %v", err)
	}
	if _, err := db.Exec(`UPDATE companies SET owner_id = $1 WHERE id = $2`, userID, companyID); err != nil {
		t.Fatalf("definir owner: %v", err)
	}
	return userID, func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) }
}

func authedReq(userID, companyID, path string, payload map[string]string) *http.Request {
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("X-Company-ID", companyID)
	ctx := context.WithValue(req.Context(), ClaimsKey, jwt.MapClaims{"user_id": userID})
	return req.WithContext(ctx)
}

func TestRessolicitar_LimpaUrlAssinada(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	userID, cleanupUser := setupOwner(t, db, companyID)
	defer cleanupUser()

	var id string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, api_versao, error_code, url_assinada, url_assinada_expira_em)
		VALUES ($1, '00000000', 'tiq-res', 'error', 'debito', 'v2', 'NOT_READY', 'https://arquivos.rfb.example/f?sig=x', now()) RETURNING id
	`, companyID).Scan(&id); err != nil {
		t.Fatalf("criar request: %v", err)
	}

	// Empresa sem credenciais RFB: o service falha antes de chamar a RFB e a linha volta a
	// 'error' — mas o claim atômico já tem que ter zerado tiquete e url_assinada*.
	rec := httptest.NewRecorder()
	RessolicitarHandler(db)(rec, authedReq(userID, companyID, "/api/rfb/apuracao/ressolicitar", map[string]string{"request_id": id}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (sem credenciais), veio %d %s", rec.Code, rec.Body.String())
	}
	var tiq, url sql.NullString
	var exp sql.NullTime
	if err := db.QueryRow(`SELECT tiquete, url_assinada, url_assinada_expira_em FROM rfb_requests WHERE id = $1`, id).Scan(&tiq, &url, &exp); err != nil {
		t.Fatalf("ler request: %v", err)
	}
	if tiq.Valid || url.Valid || exp.Valid {
		t.Errorf("Ressolicitar deve limpar tiquete e url_assinada*, veio tiquete=%v url=%v exp=%v", tiq, url, exp)
	}
}

func TestRessolicitar_LimiteDiarioDebitosPorVersao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	userID, cleanupUser := setupOwner(t, db, companyID)
	defer cleanupUser()
	t.Setenv("RFB_API_VERSION_RESTRITA", "")

	var id string
	db.QueryRow(`INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, error_code) VALUES ($1,'00000000','tiq-lim-err','error','debito','TIMEOUT') RETURNING id`, companyID).Scan(&id)
	rearm := func() {
		db.Exec(`UPDATE rfb_requests SET status='error', error_code='TIMEOUT', tiquete='tiq-lim-err' WHERE id = $1`, id)
	}
	call := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		RessolicitarHandler(db)(rec, authedReq(userID, companyID, "/api/rfb/apuracao/ressolicitar", map[string]string{"request_id": id}))
		return rec
	}

	// v1 (default): teto 2. Com 1 outro débito no dia passa do limite (400 sem credenciais);
	// com 2 → 429 "máximo 2".
	t.Setenv("RFB_API_VERSION", "")
	insertRFBReq(t, db, companyID, "tiq-lim-a", "debito", "completed")
	if rec := call(); rec.Code != http.StatusBadRequest {
		t.Fatalf("v1 com 1 outro débito: esperava passar do limite (400), veio %d %s", rec.Code, rec.Body.String())
	}
	rearm()
	insertRFBReq(t, db, companyID, "tiq-lim-b", "debito", "completed")
	if rec := call(); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "máximo 2") {
		t.Fatalf("v1 com 2 outros débitos: esperava 429 'máximo 2', veio %d %s", rec.Code, rec.Body.String())
	}

	// v2: teto 4. Com 2 outros passa; com 4 → 429 "máximo 4".
	t.Setenv("RFB_API_VERSION", "v2")
	rearm()
	if rec := call(); rec.Code != http.StatusBadRequest {
		t.Fatalf("v2 com 2 outros débitos: esperava passar do limite (400), veio %d %s", rec.Code, rec.Body.String())
	}
	rearm()
	insertRFBReq(t, db, companyID, "tiq-lim-c", "debito", "completed")
	insertRFBReq(t, db, companyID, "tiq-lim-d", "debito", "completed")
	if rec := call(); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "máximo 4") {
		t.Fatalf("v2 com 4 outros débitos: esperava 429 'máximo 4', veio %d %s", rec.Code, rec.Body.String())
	}
}

// O handler manual respeita o teto efetivo pela versão (antes: `todayCount >= 2` fixo
// contradizia a mensagem "máximo 4").
func TestSolicitarApuracaoHandler_RespeitaLimiteEfetivo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	userID, cleanupUser := setupOwner(t, db, companyID)
	defer cleanupUser()
	t.Setenv("RFB_API_VERSION_RESTRITA", "")

	// gateway que falha no token: se o handler passar do limite, vira 502 (nunca 429)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	t.Setenv("RFB_API_URL", srv.URL)
	t.Setenv("RFB_TOKEN_URL", srv.URL+"/token")
	if _, err := db.Exec(`
		INSERT INTO rfb_credentials (company_id, cnpj_matriz, client_id, client_secret, ambiente, ativo)
		VALUES ($1, $2, $3, 'secret-teste', 'producao', true)
	`, companyID, fmt.Sprintf("%08d000199", time.Now().UnixNano()%100000000), "cid-sol-"+companyID); err != nil {
		t.Fatalf("criar credencial: %v", err)
	}
	call := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		SolicitarApuracaoHandler(db)(rec, authedReq(userID, companyID, "/api/rfb/apuracao/solicitar", map[string]string{}))
		return rec
	}
	for i := 0; i < 2; i++ {
		insertRFBReq(t, db, companyID, fmt.Sprintf("tiq-sol-%d", i), "debito", "completed")
	}

	// v1: 2 débitos hoje → 429 "máximo 2"
	t.Setenv("RFB_API_VERSION", "")
	if rec := call(); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "máximo 2") {
		t.Fatalf("v1 com 2 débitos: esperava 429 'máximo 2', veio %d %s", rec.Code, rec.Body.String())
	}
	// v2: os mesmos 2 débitos já não bloqueiam (teto 4) — chega até o token da RFB (502)
	t.Setenv("RFB_API_VERSION", "v2")
	if rec := call(); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("v2 com 2 débitos não deveria dar 429: %d %s", rec.Code, rec.Body.String())
	}
	// agora há 3 (2 completed + 1 TOKEN_ERROR); mais 1 → 4 no dia → 429 "máximo 4"
	insertRFBReq(t, db, companyID, "tiq-sol-x", "debito", "completed")
	if rec := call(); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "máximo 4") {
		t.Fatalf("v2 com 4 débitos: esperava 429 'máximo 4', veio %d %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadManualCreditos_AceitaV2SemTiqueteDownload(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	userID, cleanupUser := setupOwner(t, db, companyID)
	defer cleanupUser()

	v1 := insertRFBReq(t, db, companyID, "tiq-c-v1", "credito", "error")
	db.Exec(`UPDATE rfb_requests SET api_versao = 'v1' WHERE id = $1`, v1)
	v2 := insertRFBReq(t, db, companyID, "tiq-c-v2", "credito", "error") // api_versao v2, sem tiquete_download nem url

	rec := httptest.NewRecorder()
	DownloadManualCreditosHandler(db)(rec, authedReq(userID, companyID, "/api/rfb/creditos/download", map[string]string{"request_id": v1}))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("v1 sem tiquete_download deve continuar recusando (400), veio %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	DownloadManualCreditosHandler(db)(rec, authedReq(userID, companyID, "/api/rfb/creditos/download", map[string]string{"request_id": v2}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "downloading") {
		t.Fatalf("v2 sem tiquete_download deve aceitar, veio %d %s", rec.Code, rec.Body.String())
	}
	waitProcessorSettled(t, db, v2) // goroutine termina em CRED_NOT_FOUND
}
