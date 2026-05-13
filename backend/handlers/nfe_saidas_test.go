package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Testa paths pré-DB de NfeSaidasListHandler. db=nil é seguro porque OPTIONS check,
// method check e auth check ocorrem antes de qualquer db.Query.

func TestNfeSaidasListHandlerMethodNotAllowed(t *testing.T) {
	handler := NfeSaidasListHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/nfe-saidas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

func TestNfeSaidasListHandlerOptionsPreflight(t *testing.T) {
	handler := NfeSaidasListHandler(nil)
	req := httptest.NewRequest(http.MethodOptions, "/api/nfe-saidas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("esperava HTTP 200 em OPTIONS, obteve %d", rr.Code)
	}
}

func TestNfeSaidasListHandlerUnauthorized(t *testing.T) {
	handler := NfeSaidasListHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nfe-saidas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}
