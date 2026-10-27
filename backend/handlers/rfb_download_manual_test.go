package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Issue #18: "Download Manual/Tentar Recuperar" sem tiquete_download na v1 não tem como dar certo
// (o tíquete da solicitação não é baixável) — responde 409 na hora e não altera a linha, que
// antes era sobrescrita (TIMEOUT -> DOWNLOAD_ERROR).
func TestDownloadManualHandler_V1SemTiqueteDownloadResponde409ESemAlterarALinha(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupApuracaoNFSeTest(t, db)
	defer cleanup()

	var id string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, ambiente, api_versao, error_code, error_message)
		VALUES ($1, '00000000', 'solic-manual', 'error', 'debito', 'producao', 'v1', 'TIMEOUT', 'Solicitação abortada automaticamente') RETURNING id
	`, companyID).Scan(&id); err != nil {
		t.Fatalf("criar linha: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"request_id": id})
	req := authed(httptest.NewRequest(http.MethodPost, "/api/rfb/apuracao/download", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	DownloadManualHandler(db)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("esperava 409, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if msg := rec.Body.String(); !strings.Contains(msg, "webhook") || strings.HasPrefix(strings.TrimSpace(msg), "{") {
		t.Errorf("esperava texto simples explicando que o webhook não chegou, veio: %q", msg)
	}

	var status, code string
	var msg sql.NullString
	db.QueryRow(`SELECT status, error_code, error_message FROM rfb_requests WHERE id = $1`, id).Scan(&status, &code, &msg)
	if status != "error" || code != "TIMEOUT" || !strings.Contains(msg.String, "abortada automaticamente") {
		t.Errorf("a linha foi alterada: status=%q code=%q msg=%q", status, code, msg.String)
	}
}

func postDownloadManual(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, id string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"request_id": id})
	req := authed(httptest.NewRequest(http.MethodPost, "/api/rfb/apuracao/download", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	DownloadManualHandler(db)(rec, req)
	return rec
}

// Linha já concluída sem tiquete_download mantém a mensagem de "já realizado" (a checagem de
// status vem antes do 409 novo) e a de erro terminal orienta a fazer nova solicitação.
func TestDownloadManualHandler_MensagensPorStatus(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupApuracaoNFSeTest(t, db)
	defer cleanup()

	mk := func(status string) string {
		var id string
		if err := db.QueryRow(`
			INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, ambiente, api_versao)
			VALUES ($1, '00000000', 'solic-st', $2, 'debito', 'producao', 'v1') RETURNING id
		`, companyID, status).Scan(&id); err != nil {
			t.Fatalf("criar linha %s: %v", status, err)
		}
		return id
	}

	if rec := postDownloadManual(t, db, authed, mk("completed")); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "já realizado") {
		t.Errorf("completed: esperava 409 'já realizado', veio %d %q", rec.Code, rec.Body.String())
	}
	if rec := postDownloadManual(t, db, authed, mk("error")); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "nova solicitação") {
		t.Errorf("error: esperava 409 orientando nova solicitação, veio %d %q", rec.Code, rec.Body.String())
	}
	if rec := postDownloadManual(t, db, authed, mk("requested")); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Aguarde a RFB") {
		t.Errorf("requested: esperava 409 'Aguarde a RFB', veio %d %q", rec.Code, rec.Body.String())
	}
}
