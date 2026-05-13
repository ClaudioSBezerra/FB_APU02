package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Testa paths pré-DB de NfeEntradasListHandler. db=nil é seguro porque OPTIONS check,
// method check e auth check ocorrem antes de qualquer db.Query.

func TestNfeEntradasListHandlerMethodNotAllowed(t *testing.T) {
	handler := NfeEntradasListHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/nfe-entradas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

func TestNfeEntradasListHandlerOptionsPreflight(t *testing.T) {
	handler := NfeEntradasListHandler(nil)
	req := httptest.NewRequest(http.MethodOptions, "/api/nfe-entradas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("esperava HTTP 200 em OPTIONS, obteve %d", rr.Code)
	}
}

func TestNfeEntradasListHandlerUnauthorized(t *testing.T) {
	handler := NfeEntradasListHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nfe-entradas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}
