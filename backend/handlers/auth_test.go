package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Testes adicionais para handlers de autenticação.
// Separado de login_ratelimit_test.go para manter foco temático.
// db=nil é seguro pois todos os testes cobrem paths pré-DB.

func TestRegisterHandlerInvalidJSON(t *testing.T) {
	handler := RegisterHandler(nil)
	body := []byte("{not-valid-json")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("esperava HTTP 400 para JSON inválido, obteve %d", rr.Code)
	}
}

func TestRegisterHandlerMissingFields(t *testing.T) {
	handler := RegisterHandler(nil)
	body := []byte(`{"email":"","password":"","full_name":"","company_name":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("esperava HTTP 400 para campos vazios, obteve %d", rr.Code)
	}
}

func TestRegisterHandlerRateLimited(t *testing.T) {
	const testIP = "10.0.0.99" // IP diferente do usado em login_ratelimit_test.go (10.0.0.1)
	RegisterRL.Reset(testIP)
	// Esgotar todas as 10 tentativas do rate limiter (RegisterRL tem max=10)
	for i := 0; i < 10; i++ {
		if !RegisterRL.Allow(testIP) {
			break
		}
	}
	handler := RegisterHandler(nil)
	body := []byte(`{"email":"test@example.com","password":"Secret123!","full_name":"Test User","company_name":"Test Co"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = testIP + ":9999"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("esperava HTTP 429 após rate limit esgotado, obteve %d", rr.Code)
	}
}

func TestRefreshHandlerMethodNotAllowed(t *testing.T) {
	handler := RefreshHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/refresh", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

func TestRefreshHandlerNoCookie(t *testing.T) {
	handler := RefreshHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401 sem cookie, obteve %d", rr.Code)
	}
}

func TestRefreshHandlerInvalidCookie(t *testing.T) {
	handler := RefreshHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "nonexistent-token-value-xyz"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401 com cookie inválido, obteve %d", rr.Code)
	}
}
