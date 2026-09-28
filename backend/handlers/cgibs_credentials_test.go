package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fb_apu02/crypto"
)

// postSaveCGIBSCredential chama SaveCGIBSCredentialHandler com o corpo JSON dado, autenticado
// via o wrapper `authed` (mesmo padrão de postHabilitar em cgibs_habilitar_test.go).
func postSaveCGIBSCredential(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("falha ao serializar corpo: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/cgibs/credentials", bytes.NewReader(b))
	req = authed(req)
	rec := httptest.NewRecorder()
	SaveCGIBSCredentialHandler(db)(rec, req)
	return rec
}

// getCGIBSCredential chama GetCGIBSCredentialHandler autenticado.
func getCGIBSCredential(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/cgibs/credentials", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	GetCGIBSCredentialHandler(db)(rec, req)
	return rec
}

// promoteCGIBSCredToHabilitado marca a credencial da empresa como habilitado=true com
// data_habilitacao/token_contrib preenchidos, simulando o resultado de uma Habilitação bem
// sucedida sem passar pelo fluxo real do HabilitarCGIBSHandler — usado para testar o reset em
// SaveCGIBSCredentialHandler (item 2) partindo de um estado "já habilitado".
func promoteCGIBSCredToHabilitado(t *testing.T, db *sql.DB, companyID string) {
	t.Helper()
	encToken, err := crypto.EncryptField("token-contrib-antes-do-save")
	if err != nil {
		t.Fatalf("falha ao criptografar token de teste: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE cgibs_credentials SET habilitado = true, data_habilitacao = $1, token_contrib = $2
		WHERE company_id = $3
	`, time.Now(), encToken, companyID); err != nil {
		t.Fatalf("falha ao promover credencial de teste a habilitado: %v", err)
	}
}

// TestSaveCGIBSCredentialHandler_MudaClientID_ResetaHabilitacao cobre o item 2 da revisão
// adversarial: editar client_id (credencial diferente perante a CGIBS) reseta
// habilitado/data_habilitacao/token_contrib — a habilitação antiga não é válida para a nova
// credencial.
func TestSaveCGIBSCredentialHandler_MudaClientID_ResetaHabilitacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec1 := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "secret-original",
		"ambiente":      "piloto",
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("save inicial: esperava 201, obteve %d: %s", rec1.Code, rec1.Body.String())
	}
	promoteCGIBSCredToHabilitado(t, db, companyID)

	habilitadoAntes, dataAntes, _, tokenAntes := readCGIBSCredRow(t, db, companyID)
	if !habilitadoAntes || !dataAntes.Valid || !tokenAntes.Valid {
		t.Fatalf("pré-condição falhou: esperava credencial promovida a habilitado, veio habilitado=%v data=%+v token=%+v",
			habilitadoAntes, dataAntes, tokenAntes)
	}

	rec2 := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-NOVO",
		"client_secret": "secret-original",
		"ambiente":      "piloto",
	})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("save com client_id novo: esperava 201, obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	habilitadoDepois, dataDepois, _, tokenDepois := readCGIBSCredRow(t, db, companyID)
	if habilitadoDepois {
		t.Error("trocar client_id deveria resetar habilitado para false")
	}
	if dataDepois.Valid {
		t.Error("trocar client_id deveria zerar data_habilitacao")
	}
	if tokenDepois.Valid {
		t.Error("trocar client_id deveria zerar token_contrib")
	}
}

// TestSaveCGIBSCredentialHandler_MudaAmbiente_ResetaHabilitacao cobre a mesma proteção do item
// 2 para o campo ambiente (piloto/producao).
func TestSaveCGIBSCredentialHandler_MudaAmbiente_ResetaHabilitacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec1 := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "secret-original",
		"ambiente":      "piloto",
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("save inicial: esperava 201, obteve %d: %s", rec1.Code, rec1.Body.String())
	}
	promoteCGIBSCredToHabilitado(t, db, companyID)

	rec2 := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "secret-original",
		"ambiente":      "producao",
	})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("save com ambiente novo: esperava 201, obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	habilitadoDepois, dataDepois, _, tokenDepois := readCGIBSCredRow(t, db, companyID)
	if habilitadoDepois || dataDepois.Valid || tokenDepois.Valid {
		t.Errorf("trocar ambiente deveria resetar habilitação — veio habilitado=%v data=%+v token=%+v",
			habilitadoDepois, dataDepois, tokenDepois)
	}
}

// TestSaveCGIBSCredentialHandler_MudaSoClientSecret_NaoResetaHabilitacao cobre a decisão
// documentada no item 2: rotacionar SÓ o client_secret (mesmo client_id/ambiente) é o mesmo
// contribuinte perante a CGIBS — não reseta a habilitação já confirmada.
func TestSaveCGIBSCredentialHandler_MudaSoClientSecret_NaoResetaHabilitacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec1 := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "secret-original",
		"ambiente":      "piloto",
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("save inicial: esperava 201, obteve %d: %s", rec1.Code, rec1.Body.String())
	}
	promoteCGIBSCredToHabilitado(t, db, companyID)
	_, dataAntes, _, tokenAntes := readCGIBSCredRow(t, db, companyID)

	rec2 := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "secret-ROTACIONADO",
		"ambiente":      "piloto",
	})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("save com secret rotacionado: esperava 201, obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	habilitadoDepois, dataDepois, _, tokenDepois := readCGIBSCredRow(t, db, companyID)
	if !habilitadoDepois {
		t.Error("rotacionar só client_secret NÃO deveria resetar habilitado")
	}
	if !dataDepois.Valid || !dataDepois.Time.Equal(dataAntes.Time) {
		t.Errorf("rotacionar só client_secret NÃO deveria alterar data_habilitacao — antes=%v depois=%+v", dataAntes.Time, dataDepois)
	}
	if !tokenDepois.Valid || tokenDepois.String != tokenAntes.String {
		t.Error("rotacionar só client_secret NÃO deveria alterar token_contrib")
	}
}

// TestSaveCGIBSCredentialHandler_ClientSecretVazio_MantemAnterior cobre o item 5: reenviar
// client_secret vazio/ausente mantém o valor já salvo em vez de sobrescrever com lixo.
func TestSaveCGIBSCredentialHandler_ClientSecretVazio_MantemAnterior(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec1 := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "secret-que-deve-sobreviver",
		"ambiente":      "piloto",
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("save inicial: esperava 201, obteve %d: %s", rec1.Code, rec1.Body.String())
	}

	// Reenvia sem client_secret (simula o frontend reenviando o formulário sem o usuário ter
	// digitado um novo segredo) — não deve rejeitar nem apagar o segredo salvo.
	rec2 := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "",
		"ambiente":      "piloto",
	})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("save com client_secret vazio: esperava 201 (mantém segredo salvo), obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	var secretEnc string
	if err := db.QueryRow(`SELECT client_secret FROM cgibs_credentials WHERE company_id = $1`,
		mustCGIBSCompanyIDFromRec(t, rec1)).Scan(&secretEnc); err != nil {
		t.Fatalf("falha ao ler client_secret persistido: %v", err)
	}
	if crypto.DecryptFieldWithFallback(secretEnc) != "secret-que-deve-sobreviver" {
		t.Errorf("client_secret foi sobrescrito por um save com secret vazio — esperava manter o original, veio %q",
			crypto.DecryptFieldWithFallback(secretEnc))
	}
}

// TestSaveCGIBSCredentialHandler_ClientSecretVazioSemCredencialExistente_Retorna400 confirma
// que client_secret continua obrigatório na criação (não há valor anterior para manter).
func TestSaveCGIBSCredentialHandler_ClientSecretVazioSemCredencialExistente_Retorna400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	rec := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "",
		"ambiente":      "piloto",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (client_secret obrigatório na 1ª gravação), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetCGIBSCredentialHandler_NaoExpoeSecretERetornaStatusHabilitacao cobre os itens 4 e 6:
// GET nunca devolve client_secret (cru ou mascarado) — só o booleano tem_client_secret — e
// devolve habilitado/data_habilitacao/webhook_url.
func TestGetCGIBSCredentialHandler_NaoExpoeSecretERetornaStatusHabilitacao(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupCGIBSHabilitarTest(t, db)
	defer cleanup()

	recSave := postSaveCGIBSCredential(t, db, authed, map[string]interface{}{
		"cnpj_matriz":   "12345678000199",
		"client_id":     "cid-original",
		"client_secret": "segredo-que-nunca-pode-vazar",
		"ambiente":      "piloto",
	})
	if recSave.Code != http.StatusCreated {
		t.Fatalf("save inicial: esperava 201, obteve %d: %s", recSave.Code, recSave.Body.String())
	}
	promoteCGIBSCredToHabilitado(t, db, companyID)

	rec := getCGIBSCredential(t, db, authed)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	raw := rec.Body.String()
	if strings.Contains(raw, "segredo-que-nunca-pode-vazar") {
		t.Fatal("resposta do GET contém o client_secret em texto puro — nunca deveria vazar")
	}

	var body struct {
		Credential struct {
			ClientSecret    *string `json:"client_secret"`
			TemClientSecret bool    `json:"tem_client_secret"`
			Habilitado      bool    `json:"habilitado"`
			DataHabilitacao *string `json:"data_habilitacao"`
			WebhookURL      string  `json:"webhook_url"`
		} `json:"credential"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	if body.Credential.ClientSecret != nil {
		t.Errorf("esperava campo client_secret ausente/null no JSON, veio %q", *body.Credential.ClientSecret)
	}
	if !body.Credential.TemClientSecret {
		t.Error("esperava tem_client_secret=true (há um secret salvo)")
	}
	if !body.Credential.Habilitado {
		t.Error("esperava habilitado=true refletido no GET")
	}
	if body.Credential.DataHabilitacao == nil {
		t.Error("esperava data_habilitacao preenchida no GET")
	}
}

// mustCGIBSCompanyIDFromRec extrai company_id do corpo de uma resposta de
// SaveCGIBSCredentialHandler (201) — usado quando o teste não guardou companyID separadamente.
func mustCGIBSCompanyIDFromRec(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Credential struct {
			CompanyID string `json:"company_id"`
		} `json:"credential"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("erro ao decodificar company_id da resposta: %v", err)
	}
	if body.Credential.CompanyID == "" {
		t.Fatal("company_id vazio na resposta do save")
	}
	return body.Credential.CompanyID
}
