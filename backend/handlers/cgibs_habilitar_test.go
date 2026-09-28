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

	"fb_apu02/crypto"

	"github.com/golang-jwt/jwt/v5"
)

// setupCGIBSHabilitarTest cria empresa + usuário dono (necessário para GetEffectiveCompanyID)
// e devolve um wrapper que injeta ClaimsKey + X-Company-ID na requisição, mesmo padrão de
// setupRFBLeituraTest (rfb_data_apuracao_test.go). cleanup também apaga cgibs_credentials
// explicitamente: ao contrário das tabelas novas da migration 131 (company_id UUID com
// ON DELETE CASCADE), cgibs_credentials.company_id é TEXT sem FK (migration 102) — o cascade
// do DELETE em environments não alcança essa tabela.
func setupCGIBSHabilitarTest(t *testing.T, db *sql.DB) (companyID string, authed func(*http.Request) *http.Request, cleanup func()) {
	t.Helper()
	companyID, cleanupCompany := setupTestCompany(t, db)

	var userID string
	email := fmt.Sprintf("cgibs-habilitar-%d@teste.local", time.Now().UnixNano())
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
		db.Exec(`DELETE FROM cgibs_credentials WHERE company_id = $1`, companyID)
		cleanupCompany()
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}
	return
}

// insertCGIBSCred insere uma linha cgibs_credentials de teste com client_secret já
// criptografado (mesmo formato gravado por SaveCGIBSCredentialHandler). tokenContribPlain
// vazio deixa a coluna NULL (simula credencial nunca habilitada). habilitado controla a coluna
// homônima (item 9 da revisão adversarial — matchCGIBSCredential agora exige habilitado=true,
// não só ativo=true); quando true, data_habilitacao é preenchida para satisfazer o CHECK
// cgibs_credentials_habilitado_chk (migration 131).
func insertCGIBSCred(t *testing.T, db *sql.DB, companyID, cnpjMatriz, tokenContribPlain string, ativo, habilitado bool) {
	t.Helper()
	encSecret, err := crypto.EncryptField("secret-cgibs-teste")
	if err != nil {
		t.Fatalf("falha ao criptografar client_secret de teste: %v", err)
	}
	var tokenParam interface{}
	if tokenContribPlain != "" {
		encToken, terr := crypto.EncryptField(tokenContribPlain)
		if terr != nil {
			t.Fatalf("falha ao criptografar token_contrib de teste: %v", terr)
		}
		tokenParam = encToken
	}
	var dataHabilitacaoParam interface{}
	if habilitado {
		dataHabilitacaoParam = time.Now()
	}
	if _, err := db.Exec(`
		INSERT INTO cgibs_credentials (company_id, cnpj_matriz, client_id, client_secret, ambiente, ativo, token_contrib, habilitado, data_habilitacao)
		VALUES ($1, $2, 'cid-cgibs-teste', $3, 'piloto', $4, $5, $6, $7)
	`, companyID, cnpjMatriz, encSecret, ativo, tokenParam, habilitado, dataHabilitacaoParam); err != nil {
		t.Fatalf("falha ao inserir cgibs_credentials de teste: %v", err)
	}
}

func cgibsFakeHabilitacaoServer(t *testing.T, habilitado string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"Habilitado":      habilitado,
			"DataHabilitacao": "2026-09-28T12:00:00Z",
		})
	}))
}

// cgibsFakeHabilitacaoServerSequence devolve respostas diferentes em chamadas sucessivas (na
// ordem dada em habilitados) — simula um caso real de 1ª chamada com sucesso seguida de uma
// falha transitória (item 1 da revisão adversarial). Chamadas além do número de valores
// repetem o último.
func cgibsFakeHabilitacaoServerSequence(t *testing.T, habilitados ...string) *httptest.Server {
	t.Helper()
	calls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := calls
		if idx >= len(habilitados) {
			idx = len(habilitados) - 1
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"Habilitado":      habilitados[idx],
			"DataHabilitacao": "2026-09-28T12:00:00Z",
		})
	}))
}

func postHabilitar(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/cgibs/credentials/habilitar", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	HabilitarCGIBSHandler(db)(rec, req)
	return rec
}

func readCGIBSCredRow(t *testing.T, db *sql.DB, companyID string) (habilitado bool, dataHabilitacao sql.NullTime, webhookURL sql.NullString, tokenContribEnc sql.NullString) {
	t.Helper()
	if err := db.QueryRow(`
		SELECT habilitado, data_habilitacao, webhook_url, token_contrib
		FROM cgibs_credentials WHERE company_id = $1
	`, companyID).Scan(&habilitado, &dataHabilitacao, &webhookURL, &tokenContribEnc); err != nil {
		t.Fatalf("falha ao ler cgibs_credentials: %v", err)
	}
	return
}

func TestHabilitarCGIBSHandler_SemCredencial_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	_ = companyID

	t.Setenv("CGIBS_HABILITACAO_URL", "http://127.0.0.1:0") // não deveria nem ser lido

	rec := postHabilitar(t, db, authed)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 sem credencial cadastrada, obteve %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHabilitarCGIBSHandler_SemEnvConfigurada_Retorna503(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, false)

	t.Setenv("CGIBS_HABILITACAO_URL", "")

	rec := postHabilitar(t, db, authed)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("esperava 503 sem CGIBS_HABILITACAO_URL, obteve %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHabilitarCGIBSHandler_CaminhoFeliz_PersisteCampos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, false)

	srv := cgibsFakeHabilitacaoServer(t, "sucesso")
	defer srv.Close()
	t.Setenv("CGIBS_HABILITACAO_URL", srv.URL)
	t.Setenv("CGIBS_WEBHOOK_URL", "https://exemplo.local/api/cgibs/webhook")

	rec := postHabilitar(t, db, authed)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	habilitado, dataHabilitacao, webhookURL, tokenContribEnc := readCGIBSCredRow(t, db, companyID)
	if !habilitado {
		t.Error("esperava habilitado=true persistido")
	}
	if !dataHabilitacao.Valid {
		t.Error("esperava data_habilitacao preenchida")
	}
	if !webhookURL.Valid || webhookURL.String != "https://exemplo.local/api/cgibs/webhook" {
		t.Errorf("esperava webhook_url=https://exemplo.local/api/cgibs/webhook, veio %+v", webhookURL)
	}
	if !tokenContribEnc.Valid || tokenContribEnc.String == "" {
		t.Fatal("esperava token_contrib gerado e persistido")
	}
	tokenPlain := crypto.DecryptFieldWithFallback(tokenContribEnc.String)
	if len(tokenPlain) != 64 { // 32 bytes hex-encoded
		t.Errorf("esperava token_contrib de 64 chars (hex de 32 bytes), veio %d chars", len(tokenPlain))
	}
}

func TestHabilitarCGIBSHandler_NaoSobrescreveTokenExistente(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	const tokenOriginal = "token-ja-existente-nao-pode-mudar"
	insertCGIBSCred(t, db, companyID, "12345678000199", tokenOriginal, true, false)

	srv := cgibsFakeHabilitacaoServer(t, "sucesso")
	defer srv.Close()
	t.Setenv("CGIBS_HABILITACAO_URL", srv.URL)

	// 1ª chamada
	rec1 := postHabilitar(t, db, authed)
	if rec1.Code != http.StatusOK {
		t.Fatalf("1ª chamada: esperava 200, obteve %d: %s", rec1.Code, rec1.Body.String())
	}
	_, _, _, tokenAfter1 := readCGIBSCredRow(t, db, companyID)
	if crypto.DecryptFieldWithFallback(tokenAfter1.String) != tokenOriginal {
		t.Fatalf("1ª chamada: token_contrib foi alterado — esperava manter %q", tokenOriginal)
	}

	// 2ª chamada — mesmo token_contrib já existente, não deve gerar um novo
	rec2 := postHabilitar(t, db, authed)
	if rec2.Code != http.StatusOK {
		t.Fatalf("2ª chamada: esperava 200, obteve %d: %s", rec2.Code, rec2.Body.String())
	}
	_, _, _, tokenAfter2 := readCGIBSCredRow(t, db, companyID)
	if crypto.DecryptFieldWithFallback(tokenAfter2.String) != tokenOriginal {
		t.Errorf("2ª chamada: token_contrib foi sobrescrito — esperava manter %q, veio %q",
			tokenOriginal, crypto.DecryptFieldWithFallback(tokenAfter2.String))
	}
}

// TestHabilitarCGIBSHandler_RespostaNaoSucessoNaoApagaHabilitacaoPrevia cobre o item 1 da
// revisão adversarial: uma 2ª chamada de Habilitar cuja resposta NÃO é "sucesso" (ex.: falha
// transitória) não pode apagar habilitado/data_habilitacao já confirmados por uma chamada
// anterior bem-sucedida.
func TestHabilitarCGIBSHandler_RespostaNaoSucessoNaoApagaHabilitacaoPrevia(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, false)

	srv := cgibsFakeHabilitacaoServerSequence(t, "sucesso", "falha")
	defer srv.Close()
	t.Setenv("CGIBS_HABILITACAO_URL", srv.URL)

	// 1ª chamada: sucesso, confirma a habilitação.
	rec1 := postHabilitar(t, db, authed)
	if rec1.Code != http.StatusOK {
		t.Fatalf("1ª chamada: esperava 200, obteve %d: %s", rec1.Code, rec1.Body.String())
	}
	habilitado1, dataHabilitacao1, _, _ := readCGIBSCredRow(t, db, companyID)
	if !habilitado1 || !dataHabilitacao1.Valid {
		t.Fatalf("1ª chamada: esperava habilitado=true e data_habilitacao preenchida, veio habilitado=%v data_habilitacao=%+v",
			habilitado1, dataHabilitacao1)
	}
	dataOriginal := dataHabilitacao1.Time

	// 2ª chamada: resposta não-sucesso — habilitado/data_habilitacao já confirmados NÃO podem
	// regredir.
	rec2 := postHabilitar(t, db, authed)
	if rec2.Code != http.StatusOK {
		t.Fatalf("2ª chamada: esperava 200, obteve %d: %s", rec2.Code, rec2.Body.String())
	}
	var body2 map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&body2); err != nil {
		t.Fatalf("erro ao decodificar resposta da 2ª chamada: %v", err)
	}
	if body2["habilitado"] != true {
		t.Errorf("2ª chamada: resposta deveria refletir o estado final do banco (habilitado=true), veio %v", body2["habilitado"])
	}

	habilitado2, dataHabilitacao2, _, _ := readCGIBSCredRow(t, db, companyID)
	if !habilitado2 {
		t.Fatal("2ª chamada (resposta não-sucesso): habilitado foi apagado — deveria permanecer true")
	}
	if !dataHabilitacao2.Valid || !dataHabilitacao2.Time.Equal(dataOriginal) {
		t.Errorf("2ª chamada: data_habilitacao foi alterada — esperava manter %v, veio %v", dataOriginal, dataHabilitacao2.Time)
	}
}

// TestHabilitarCGIBSHandler_CredencialAlteradaDuranteChamada_Retorna409 cobre o item 3 da
// revisão adversarial: a chamada de rede (services.Habilitar) acontece fora de qualquer
// transação/lock — se a credencial for editada enquanto a chamada está em andamento, o
// resultado da chamada (feito com dados antigos) não pode ser persistido silenciosamente.
func TestHabilitarCGIBSHandler_CredencialAlteradaDuranteChamada_Retorna409(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()
	insertCGIBSCred(t, db, companyID, "12345678000199", "", true, false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simula uma edição concorrente (ex.: admin troca client_id) enquanto a chamada de rede
		// está em andamento — só é observável em teste porque a chamada acontece FORA da
		// transação de persistência (item 3).
		if _, err := db.Exec(`UPDATE cgibs_credentials SET client_id = 'client-id-trocado-durante-chamada' WHERE company_id = $1`, companyID); err != nil {
			t.Fatalf("falha ao simular edição concorrente: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"Habilitado":      "sucesso",
			"DataHabilitacao": "2026-09-28T12:00:00Z",
		})
	}))
	defer srv.Close()
	t.Setenv("CGIBS_HABILITACAO_URL", srv.URL)

	rec := postHabilitar(t, db, authed)
	if rec.Code != http.StatusConflict {
		t.Fatalf("esperava 409 quando a credencial muda durante a chamada, obteve %d: %s", rec.Code, rec.Body.String())
	}

	habilitado, _, _, _ := readCGIBSCredRow(t, db, companyID)
	if habilitado {
		t.Error("resultado da chamada não deveria ter sido persistido — credencial mudou durante a requisição")
	}
}
