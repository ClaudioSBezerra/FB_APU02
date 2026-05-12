package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLoginHandlerRateLimit verifica que LoginRL.Allow(ip) é chamado no início de
// LoginHandler e que a 6ª tentativa do mesmo IP dentro de 15 minutos retorna HTTP 429.
//
// Estratégia: esgotamos as 5 permissões do LoginRL diretamente via Allow(),
// de modo que a próxima chamada ao handler retorne 429 SEM precisar de DB real.
func TestLoginHandlerRateLimit(t *testing.T) {
	const testIP = "10.0.0.1"

	// Resetar o rate limiter para garantir estado limpo
	LoginRL.Reset(testIP)

	// Esgotar as 5 tentativas permitidas consumindo diretamente o rate limiter
	for i := 0; i < 5; i++ {
		if !LoginRL.Allow(testIP) {
			t.Fatalf("Allow retornou false na tentativa %d (esperava true)", i+1)
		}
	}

	// A partir daqui, LoginRL.Allow(testIP) deve retornar false.
	// O handler deve verificar o rate limiter ANTES de qualquer outra operação.
	// Passamos db=nil — se o handler consultar o banco antes de checar o rate limiter,
	// ocorrerá um panic (nil pointer), e o teste falhará com erro de runtime.
	handler := LoginHandler(nil)

	body := []byte(`{"email":"test@test.com","password":"wrongpassword"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Simular o IP que foi esgotado (GetClientIP usa RemoteAddr quando não há X-Forwarded-For)
	req.RemoteAddr = testIP + ":12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("esperava HTTP 429 (Too Many Requests), obteve %d — LoginRL.Allow não está sendo chamado antes do DB", rr.Code)
	}
}
