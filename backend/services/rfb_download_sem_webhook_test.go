package services

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Download v1 sem webhook (issue #18): sem tiquete_download/url_assinada não há o que baixar, e o
// 401 de tíquete não pode ser rotulado como falha de token nem sobrescrever o erro da linha.

// fakeRFBCount responde token e download com o status/corpo dados e conta as chamadas de download.
func fakeRFBCount(t *testing.T, dlStatus int, dlBody string) (*RFBClient, *atomic.Int32) {
	t.Helper()
	var dlCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":3600}`))
		case strings.Contains(r.URL.Path, "/download/"):
			dlCalls.Add(1)
			w.WriteHeader(dlStatus)
			w.Write([]byte(dlBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RFB_API_URL", srv.URL)
	t.Setenv("RFB_TOKEN_URL", srv.URL+"/token")
	return NewRFBClient(), &dlCalls
}

func downloadCode(t *testing.T, err error) string {
	t.Helper()
	var de *RFBDownloadError
	if !errors.As(err, &de) {
		t.Fatalf("esperava *RFBDownloadError, veio %T: %v", err, err)
	}
	return de.Code
}

func TestDownloadArquivo_401DeTiqueteNaoEFalhaDeToken(t *testing.T) {
	c, _ := fakeRFBCount(t, http.StatusUnauthorized, `Tíquete inexistente ou download já realizado.`)
	_, err := c.DownloadArquivo("tok", "d-123")
	if err == nil {
		t.Fatal("esperava erro")
	}
	if got := downloadCode(t, err); got != RFBErrTicketInvalido {
		t.Errorf("code = %q, esperava %q", got, RFBErrTicketInvalido)
	}
	if strings.Contains(strings.ToLower(err.Error()), "token") {
		t.Errorf("a mensagem não deve culpar o token: %v", err)
	}
}

func TestDownloadArquivo_401SemTiqueteContinuaSendoAuth(t *testing.T) {
	c, _ := fakeRFBCount(t, http.StatusUnauthorized, `Invalid credentials`)
	_, err := c.DownloadArquivo("tok", "d-123")
	if err == nil || !strings.Contains(err.Error(), "token inválido ou expirado") {
		t.Fatalf("401 sem 'tíquete' deve manter o rótulo de auth, veio: %v", err)
	}
	var de *RFBDownloadError
	if errors.As(err, &de) && de.Code == RFBErrTicketInvalido {
		t.Errorf("401 de auth não pode virar %s", RFBErrTicketInvalido)
	}
}

func TestBaixarArquivoRFB_V1SemTiqueteDownloadNaoChamaARFB(t *testing.T) {
	c, calls := fakeRFBCount(t, http.StatusOK, `{"ok":true}`)
	_, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "solic-1", APIVersao: "v1"})
	if err == nil {
		t.Fatal("esperava erro")
	}
	if got := downloadCode(t, err); got != RFBErrNotReady {
		t.Errorf("code = %q, esperava %q", got, RFBErrNotReady)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("não deveria chamar o download com o tíquete da solicitação (chamadas: %d)", n)
	}
}

func TestBaixarArquivoRFB_V1ComTiqueteDownloadBaixaNormalmente(t *testing.T) {
	c, calls := fakeRFBCount(t, http.StatusOK, `{"ok":true}`)
	data, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "solic-1", TiqueteDownload: "dl-1", APIVersao: "v1"})
	if err != nil || string(data) != `{"ok":true}` {
		t.Fatalf("v1 com webhook deve seguir igual: data=%q err=%v", data, err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("esperava 1 chamada de download, veio %d", n)
	}
}

func TestSemFonteDeDownload(t *testing.T) {
	cases := []struct {
		nome string
		in   RFBDownloadInput
		want bool
	}{
		{"v1 sem nada", RFBDownloadInput{APIVersao: "v1"}, true},
		{"versão vazia (v1)", RFBDownloadInput{}, true},
		{"v1 com tiquete_download", RFBDownloadInput{APIVersao: "v1", TiqueteDownload: "d"}, false},
		{"v1 com url_assinada", RFBDownloadInput{APIVersao: "v1", URLAssinada: "https://x"}, false},
		{"v2 sem nada (tem situacao)", RFBDownloadInput{APIVersao: "v2"}, false},
	}
	for _, tc := range cases {
		if got := tc.in.SemFonteDeDownload(); got != tc.want {
			t.Errorf("%s: esperava %v, veio %v", tc.nome, tc.want, got)
		}
	}
}

// A linha TIMEOUT (webhook nunca chegou) não pode ser sobrescrita por tentativa de recuperação.
func TestProcessarDownload_V1SemTiqueteDownloadPreservaLinhaTimeout(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")
	c, calls := fakeRFBCount(t, http.StatusUnauthorized, `Tíquete inexistente ou download já realizado.`)

	for _, tipo := range []string{"debito", "credito"} {
		var id string
		if err := db.QueryRow(`
			INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, ambiente, api_versao, error_code, error_message)
			VALUES ($1, '00000000', $2, 'error', $3, 'producao', 'v1', 'TIMEOUT', 'Solicitação abortada automaticamente') RETURNING id
		`, companyID, "solic-"+tipo, tipo).Scan(&id); err != nil {
			t.Fatalf("%s: criar linha: %v", tipo, err)
		}

		var err error
		if tipo == "credito" {
			err = ProcessarDownloadCreditosRFB(db, c, id)
		} else {
			err = ProcessarDownloadRFB(db, c, id)
		}
		if err == nil || downloadCode(t, err) != RFBErrNotReady {
			t.Fatalf("%s: esperava NOT_READY, veio %v", tipo, err)
		}

		var status, code string
		var msg sql.NullString
		db.QueryRow(`SELECT status, error_code, error_message FROM rfb_requests WHERE id = $1`, id).Scan(&status, &code, &msg)
		if status != "error" || code != "TIMEOUT" || !strings.Contains(msg.String, "abortada automaticamente") {
			t.Errorf("%s: linha foi alterada: status=%q code=%q msg=%q", tipo, status, code, msg.String)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("nenhuma chamada de download deveria ocorrer, vieram %d", n)
	}
}

// 401 de auth que apenas ecoa o id do tíquete NÃO pode virar TICKET_INVALIDO (não renovaria o token).
func TestDownloadArquivo_401QueEcoaTiqueteMasEAuthContinuaSendoAuth(t *testing.T) {
	c, _ := fakeRFBCount(t, http.StatusUnauthorized, `Token inválido para o tíquete d-123`)
	_, err := c.DownloadArquivo("tok", "d-123")
	if err == nil || !strings.Contains(err.Error(), "token inválido ou expirado") {
		t.Fatalf("esperava falha de auth, veio: %v", err)
	}
	var de *RFBDownloadError
	if errors.As(err, &de) && de.Code == RFBErrTicketInvalido {
		t.Errorf("não pode ser %s", RFBErrTicketInvalido)
	}
}

// Havia urlAssinada e ela falhou: esse é o erro real, não "ainda não pronto".
func TestBaixarArquivoRFB_V1ComURLQuebradaSemTiqueteDownloadDevolveOErroDaURL(t *testing.T) {
	c, calls := fakeRFBCount(t, http.StatusOK, `{"ok":true}`)
	_, err := BaixarArquivoRFB(c, "tok", RFBDownloadInput{Tiquete: "solic-1", APIVersao: "v1", URLAssinada: "http://insegura.example/x"})
	if err == nil {
		t.Fatal("esperava erro")
	}
	if got := downloadCode(t, err); got == RFBErrNotReady {
		t.Errorf("o erro real da URL foi mascarado como %s", got)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("não deveria cair no download por tíquete da solicitação (chamadas: %d)", n)
	}
}
