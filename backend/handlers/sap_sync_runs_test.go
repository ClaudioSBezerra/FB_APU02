package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Testa paths pré-DB de SAPSyncRunsListHandler. db=nil é seguro porque o
// check de claims/método ocorre antes de qualquer chamada ao banco — mesmo
// padrão de sap_credentials_test.go.

func TestSAPSyncRunsListHandlerUnauthorized(t *testing.T) {
	handler := SAPSyncRunsListHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/sap/sync-runs", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestSAPSyncRunsListHandlerMethodNotAllowed(t *testing.T) {
	handler := SAPSyncRunsListHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/sap/sync-runs", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}
