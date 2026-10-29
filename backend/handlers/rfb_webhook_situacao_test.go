package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRFBWebhook_SucessoDocumentado_V2_DisparaDownload(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBReq(t, db, companyID, "tiq-doc-v2", "debito", "requested") // api_versao v2

	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-doc-v2", "situacao": "CONCLUIDA"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "processing") {
		t.Fatalf("esperava 200/processing, veio %d %s", rec.Code, rec.Body.String())
	}
	// Sem credencial RFB na empresa de teste o processador assenta em CRED_NOT_FOUND: prova que foi disparado.
	waitProcessorSettled(t, db, id)
	if row := readReqRow(t, db, id); row.ErrCode != "CRED_NOT_FOUND" {
		t.Errorf("download deveria ter sido disparado (CRED_NOT_FOUND), veio %+v", row)
	}
}

func TestRFBWebhook_SucessoDocumentado_V2_ComAllowlistDaRFB(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBReq(t, db, companyID, "tiq-doc-ip", "debito", "requested")
	t.Setenv("RFB_WEBHOOK_SECRET", "")
	t.Setenv("RFB_WEBHOOK_ALLOWED_IPS", "189.9.61.23/32")

	body, _ := json.Marshal(map[string]interface{}{"tiqueteSolicitacao": "tiq-doc-ip", "situacao": "CONCLUIDA"})
	req := httptest.NewRequest(http.MethodPost, "/api/rfb/webhook", bytes.NewReader(body))
	req.Header.Set("X-Forwarded-For", "189.9.61.23, 10.0.9.6")
	rec := httptest.NewRecorder()
	RFBWebhookHandler(db)(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "processing") {
		t.Fatalf("esperava 200/processing, veio %d %s", rec.Code, rec.Body.String())
	}
	waitProcessorSettled(t, db, id)
}

func TestRFBWebhook_SucessoDocumentado_V1_PreservaLinha(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBReq(t, db, companyID, "tiq-doc-v1", "debito", "requested")
	if _, err := db.Exec(`UPDATE rfb_requests SET api_versao = 'v1' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-doc-v1", "situacao": "CONCLUIDA"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "warning") {
		t.Fatalf("esperava 200/warning, veio %d %s", rec.Code, rec.Body.String())
	}
	if row := readReqRow(t, db, id); row.Status != "requested" || row.ErrCode != "" {
		t.Errorf("linha v1 deve ficar intacta (requested), veio %+v", row)
	}
}

func TestRFBWebhook_SituacaoNaoFinal_NaoMexe(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	id := insertRFBReq(t, db, companyID, "tiq-doc-pend", "debito", "requested")

	rec := postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-doc-pend", "situacao": "EM_PROCESSAMENTO"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "not final") {
		t.Fatalf("esperava 200/not final, veio %d %s", rec.Code, rec.Body.String())
	}
	if row := readReqRow(t, db, id); row.Status != "requested" {
		t.Errorf("situação não final não altera a linha, veio %+v", row)
	}
}

func TestRFBWebhook_SituacaoErroEExpirada_ViraErro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	idExp := insertRFBReq(t, db, companyID, "tiq-doc-exp", "debito", "requested")
	idErr := insertRFBReq(t, db, companyID, "tiq-doc-err", "debito", "requested")

	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-doc-exp", "situacao": "EXPIRADA"})
	postWebhook(t, db, map[string]interface{}{"tiqueteSolicitacao": "tiq-doc-err", "situacao": "ERRO"})

	if row := readReqRow(t, db, idExp); row.Status != "error" || row.ErrCode != "EXPIRADA" {
		t.Errorf("EXPIRADA: esperava error/EXPIRADA, veio %+v", row)
	}
	if row := readReqRow(t, db, idErr); row.Status != "error" || row.ErrCode != "RFB_ERRO" {
		t.Errorf("ERRO sem codigoErro: esperava error/RFB_ERRO, veio %+v", row)
	}
}
