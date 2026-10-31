package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Arquivo v2 no formato real (captura de 30/10): raiz com ni de 14 dígitos.
func corpoDebitoV2(ni, cbs string) string {
	return fmt.Sprintf(`{"tiqueteSolicitacao":"x","ni":"%s","niConsumidor":"39421503104","geradoEm":"2026-10-30T20:45:05-03:00",`+
		`"apuracao":[{"pa":"10/2026","debitos":[{"origem":0,"documento":91,"chave":"CH-CONF-1","emissao":"2026-10-20T16:41:14-03:00",`+
		`"registro":"2026-10-20T17:13:25.981863-03:00","atualizacao":"2026-10-22T06:28:20.776-03:00","cbs":%s}]}]}`, ni, cbs)
}

func contarDebitos(t *testing.T, db *sql.DB, requestID string) int {
	t.Helper()
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM rfb_debitos WHERE request_id = $1`, requestID).Scan(&n)
	return n
}

func TestProcessarDownload_V2_NIDivergenteNaoGravaNada(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, corpoDebitoV2("11111111000199", `{"apurado":100,"saldoDevedor":100}`))
	})
	exp := time.Now().Add(time.Hour)
	// insertRFBRequestV2 grava cnpj_base '00000000': o ni 11111111… é de outro contribuinte.
	id := insertRFBRequestV2(t, db, companyID, "tiq-ni-div", "debito", "producao", "v2", "", fake.URL+"/arq", &exp)

	if err := ProcessarDownloadRFB(db, NewRFBClient(), id); err == nil || !strings.Contains(err.Error(), "NI_DIVERGENTE") {
		t.Fatalf("esperava erro NI_DIVERGENTE, veio %v", err)
	}
	status, ec, _, _ := readRequest(t, db, id)
	var raw sql.NullString
	db.QueryRow(`SELECT raw_json FROM rfb_requests WHERE id = $1`, id).Scan(&raw)
	if status != "error" || ec != "NI_DIVERGENTE" || raw.Valid {
		t.Errorf("esperava error/NI_DIVERGENTE sem raw_json, veio %s/%s raw=%v", status, ec, raw.Valid)
	}
	if n := contarDebitos(t, db, id); n != 0 {
		t.Errorf("nenhum débito de outro contribuinte pode ser gravado, gravou %d", n)
	}
}

func TestProcessarDownload_V2_NIConfere14DigitosEPgtoAntecipado(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	withSSRFBypass(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, corpoDebitoV2("00000000000191", `{"apurado":100,"extinto":10,"pgtoAntecipado":5,"saldoDevedor":85}`))
	})
	exp := time.Now().Add(time.Hour)
	id := insertRFBRequestV2(t, db, companyID, "tiq-ni-ok", "debito", "producao", "v2", "", fake.URL+"/arq", &exp)

	if err := ProcessarDownloadRFB(db, NewRFBClient(), id); err != nil {
		t.Fatalf("download: %v", err)
	}
	waitAsyncSAPSync()
	if status, ec, em, _ := readRequest(t, db, id); status != "completed" {
		t.Fatalf("ni de 14 dígitos com a mesma raiz deve processar: %s (%s %s)", status, ec, em)
	}
	var extinto, naoExtinto float64
	db.QueryRow(`SELECT valor_cbs_extinto, valor_cbs_nao_extinto FROM rfb_debitos WHERE request_id = $1`, id).Scan(&extinto, &naoExtinto)
	if extinto != 15 || naoExtinto != 85 {
		t.Errorf("extinto deve somar pgtoAntecipado (10+5=15) e saldo 85, veio %.2f/%.2f", extinto, naoExtinto)
	}
}

func TestReprocessar_V2_NIDivergente(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBRequestV2(t, db, companyID, "tiq-ni-rep", "debito", "producao", "v2", "", "", nil)
	db.Exec(`UPDATE rfb_requests SET status = 'error', raw_json = $1 WHERE id = $2`,
		corpoDebitoV2("22222222000100", `{"apurado":50,"saldoDevedor":50}`), id)

	if err := ReprocessarRawJSON(db, id); err == nil || !strings.Contains(err.Error(), "NI_DIVERGENTE") {
		t.Fatalf("reprocessamento deve barrar ni divergente, veio %v", err)
	}
	if _, ec, _, _ := readRequest(t, db, id); ec != "NI_DIVERGENTE" {
		t.Errorf("esperava NI_DIVERGENTE, veio %s", ec)
	}
	var raw sql.NullString
	db.QueryRow(`SELECT raw_json FROM rfb_requests WHERE id = $1`, id).Scan(&raw)
	if !raw.Valid {
		t.Errorf("no reprocessamento o raw_json é a evidência e deve ser mantido")
	}
	if n := contarDebitos(t, db, id); n != 0 {
		t.Errorf("reprocessamento não pode gravar débitos de outro contribuinte, gravou %d", n)
	}
}

// A abertura grava o tEASegundos (débitos) e -1 vira NULL (créditos).
func TestSolicitar_V2_GravaTEASegundos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("RFB_API_VERSION", "v2")
	newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		if strings.Contains(r.URL.Path, "/debitos/") {
			fmt.Fprint(w, `{"tiqueteSolicitacao":"tiq-tea-deb","tEASegundos":120}`)
		} else {
			fmt.Fprint(w, `{"tiqueteSolicitacao":"tiq-tea-cred","tEASegundos":-1}`)
		}
	})
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	if err := SolicitarApuracaoParaEmpresa(db, companyID, LimiteDebitosDiaRFBv2); err != nil {
		t.Fatalf("solicitar: %v", err)
	}
	waitCreditoRows(t, db, companyID, 1)
	var deb, cred sql.NullInt64
	db.QueryRow(`SELECT tea_segundos FROM rfb_requests WHERE company_id = $1 AND tipo = 'debito'`, companyID).Scan(&deb)
	db.QueryRow(`SELECT tea_segundos FROM rfb_requests WHERE company_id = $1 AND tipo = 'credito'`, companyID).Scan(&cred)
	if !deb.Valid || deb.Int64 != 120 || cred.Valid {
		t.Errorf("esperava débito=120 e crédito=NULL, veio %v/%v", deb, cred)
	}
}

// v2 com estimativa: não consulta a situação antes do tempo estimado (o webhook deve chegar antes).
func TestPoller_V2RespeitaTEASegundos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	fake := newV2Fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"situacao":"EM_PROCESSAMENTO"}`)
	})
	id := insertRFBRequestRequested(t, db, companyID, "tiq-tea-poll", "debito", "v2", 3*time.Minute)
	db.Exec(`UPDATE rfb_requests SET tea_segundos = 600 WHERE id = $1`, id) // 10 min estimados
	p := novoRFBPoller()

	t0 := time.Now()
	pollarSituacaoRFB(db, p, relogio(t0))                    // idade 3 min < 10 min
	pollarSituacaoRFB(db, p, relogio(t0.Add(5*time.Minute))) // idade 8 min < 10 min
	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 0 {
		t.Errorf("antes do tEA não consulta, consultou %d", got)
	}
	pollarSituacaoRFB(db, p, relogio(t0.Add(8*time.Minute))) // idade 11 min
	if got := fake.count("GET /apuracao-cbs/v2/situacao/"); got != 1 {
		t.Errorf("passado o tEA consulta, esperava 1, houve %d", got)
	}
	if a, ok := fake.authFor("/apuracao-cbs/v2/situacao/tiq-tea-poll"); !ok || a == "" {
		t.Errorf("consulta de situação sem Authorization")
	}
}

// Sem ni e com tíquete de outra solicitação: os dois sinais juntos rejeitam o arquivo.
func TestValidarArquivoV2_SemNIComTiqueteDeOutraSolicitacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBRequestV2(t, db, companyID, "tiq-meu", "debito", "producao", "v2", "", "", nil)

	if validarArquivoV2(db, "teste", id, "00000000", "tiq-meu", "", "tiq-de-outro", true) {
		t.Fatalf("arquivo sem ni e com tíquete de outra solicitação deveria ser rejeitado")
	}
	if _, ec, _, _ := readRequest(t, db, id); ec != "NI_DIVERGENTE" {
		t.Errorf("esperava NI_DIVERGENTE, veio %s", ec)
	}
	// Só o tíquete divergente (com ni que confere) apenas loga.
	if !validarArquivoV2(db, "teste", id, "00000000", "tiq-meu", "00000000000191", "tiq-de-outro", true) {
		t.Errorf("ni que confere não deve ser rejeitado só pelo tíquete")
	}
}

// ni e tíquete como número na raiz não podem derrubar o parse do arquivo.
func TestRFBApuracaoV2JSON_RaizNumerica(t *testing.T) {
	var a RFBApuracaoV2JSON
	if err := json.Unmarshal([]byte(`{"tiqueteSolicitacao":123,"ni":38149716000128,"geradoEm":null,"apuracao":[]}`), &a); err != nil {
		t.Fatalf("raiz numérica não pode falhar o parse: %v", err)
	}
	if string(a.NI) != "38149716000128" || string(a.GeradoEm) != "" {
		t.Errorf("valores inesperados: ni=%q geradoEm=%q", a.NI, a.GeradoEm)
	}
}

func TestExtintoDebitoV2(t *testing.T) {
	soma := RFBDebitoV2{Chave: "a", CBS: RFBCBSV2Debito{Apurado: 100, Extinto: 10, PgtoAntecipado: 5, SaldoDevedor: 85}}
	if got := extintoDebitoV2(soma); got != 15 {
		t.Errorf("categorias distintas: esperava 15, veio %.2f", got)
	}
	jaIncluso := RFBDebitoV2{Chave: "b", CBS: RFBCBSV2Debito{Apurado: 100, Extinto: 15, PgtoAntecipado: 5, SaldoDevedor: 85}}
	if got := extintoDebitoV2(jaIncluso); got != 15 {
		t.Errorf("extinto já incluindo o pagamento antecipado não pode contar duas vezes: veio %.2f", got)
	}
	semPgto := RFBDebitoV2{Chave: "c", CBS: RFBCBSV2Debito{Apurado: 100, Extinto: 30, SaldoDevedor: 70}}
	if got := extintoDebitoV2(semPgto); got != 30 {
		t.Errorf("sem pagamento antecipado: esperava 30, veio %.2f", got)
	}
}
