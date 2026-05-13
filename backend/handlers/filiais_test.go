package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Testa paths pré-DB de GetFiliaisHandler. db=nil é seguro porque o check de claims
// ocorre antes de qualquer chamada ao banco.

func TestGetFiliaisHandlerUnauthorized(t *testing.T) {
	handler := GetFiliaisHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/filiais", nil)
	// sem ClaimsKey no context — handler deve retornar 401
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestGetFiliaisHandlerWrongClaimsType(t *testing.T) {
	handler := GetFiliaisHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/filiais", nil)
	ctx := context.WithValue(req.Context(), ClaimsKey, "not-a-jwt-mapclaims")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}
