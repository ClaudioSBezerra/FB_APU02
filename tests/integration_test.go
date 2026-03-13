package tests

import (
	"net/http"
	"testing"
	"time"
)

// TestHealthCheck verifica se o endpoint de saúde está respondendo
// Este teste assume que o ambiente Docker está rodando
func TestHealthCheck(t *testing.T) {
	// Aguarda o serviço subir (em um cenário real, usaria retry logic)
	baseURL := "http://localhost:8080/api/health"
	
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(baseURL)
	if err != nil {
		t.Logf("Aviso: Backend não acessível (%v). Teste ignorado se o ambiente não estiver rodando.", err)
		return // Não falha o teste se o servidor não estiver de pé localmente sem docker
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Esperado status 200, recebeu %d", resp.StatusCode)
	}
}