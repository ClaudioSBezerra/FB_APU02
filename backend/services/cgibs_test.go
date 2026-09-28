package services

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHabilitar_SemEnvConfigurada_NaoTentaRede confirma que, sem CGIBS_HABILITACAO_URL,
// Habilitar devolve ErrCGIBSNaoConfigurado sem tentar nenhuma chamada de rede (Boundaries
// da spec-cgibs-habilitacao-webhook.md). Não sobe nenhum servidor fake — se o código
// tentasse rede aqui, o teste travaria/falharia por conexão recusada, não por erro tipado.
func TestHabilitar_SemEnvConfigurada_NaoTentaRede(t *testing.T) {
	t.Setenv("CGIBS_HABILITACAO_URL", "")

	habilitado, dataHabilitacao, err := Habilitar("client-id", "client-secret", "https://exemplo/webhook", "token-contrib")

	if !errors.Is(err, ErrCGIBSNaoConfigurado) {
		t.Fatalf("esperava ErrCGIBSNaoConfigurado, obteve %v", err)
	}
	if habilitado != "" {
		t.Errorf("esperava habilitado vazio, obteve %q", habilitado)
	}
	if !dataHabilitacao.IsZero() {
		t.Errorf("esperava dataHabilitacao zero, obteve %v", dataHabilitacao)
	}
}

// TestHabilitar_ComFakeServer_MontaCorpoEParseiaResposta confirma que Habilitar monta o
// corpo exatamente com os 5 campos documentados (ClientID, ClientSecret, QueroHabilitar,
// WebhookURL, TokenContrib — autenticação por credencial no corpo, não Bearer) e parseia
// corretamente uma resposta de sucesso.
func TestHabilitar_ComFakeServer_MontaCorpoEParseiaResposta(t *testing.T) {
	var capturedBody map[string]string
	var capturedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("esperava POST, veio %s", r.Method)
		}
		capturedContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("erro ao decodificar corpo recebido: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"Habilitado":      "sucesso",
			"DataHabilitacao": "2026-09-28T10:00:00Z",
		})
	}))
	defer srv.Close()

	t.Setenv("CGIBS_HABILITACAO_URL", srv.URL)

	habilitado, dataHabilitacao, err := Habilitar("cid-teste", "secret-teste", "https://fbtax.cloud/api/cgibs/webhook", "token-abc")
	if err != nil {
		t.Fatalf("Habilitar retornou erro inesperado: %v", err)
	}

	if capturedContentType != "application/json" {
		t.Errorf("esperava Content-Type application/json, veio %q", capturedContentType)
	}
	if capturedBody["ClientID"] != "cid-teste" {
		t.Errorf("ClientID: esperava %q, veio %q", "cid-teste", capturedBody["ClientID"])
	}
	if capturedBody["ClientSecret"] != "secret-teste" {
		t.Errorf("ClientSecret: esperava %q, veio %q", "secret-teste", capturedBody["ClientSecret"])
	}
	if capturedBody["QueroHabilitar"] != "HAB" {
		t.Errorf("QueroHabilitar: esperava %q, veio %q", "HAB", capturedBody["QueroHabilitar"])
	}
	if capturedBody["WebhookURL"] != "https://fbtax.cloud/api/cgibs/webhook" {
		t.Errorf("WebhookURL: esperava %q, veio %q", "https://fbtax.cloud/api/cgibs/webhook", capturedBody["WebhookURL"])
	}
	if capturedBody["TokenContrib"] != "token-abc" {
		t.Errorf("TokenContrib: esperava %q, veio %q", "token-abc", capturedBody["TokenContrib"])
	}
	if _, hasAuthField := capturedBody["Authorization"]; hasAuthField {
		t.Errorf("corpo não deveria ter campo Authorization (autenticação é por credencial no corpo, não Bearer)")
	}

	if habilitado != "sucesso" {
		t.Errorf("esperava habilitado=sucesso, veio %q", habilitado)
	}
	want := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	if !dataHabilitacao.Equal(want) {
		t.Errorf("esperava dataHabilitacao=%v, veio %v", want, dataHabilitacao)
	}
}

// TestHabilitar_HTTPErro_DevolveErro confirma que um status HTTP de erro vira erro Go, sem
// panics nem sucesso silencioso.
func TestHabilitar_HTTPErro_DevolveErro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"erro":"falha interna"}`))
	}))
	defer srv.Close()

	t.Setenv("CGIBS_HABILITACAO_URL", srv.URL)

	_, _, err := Habilitar("cid", "secret", "https://exemplo/webhook", "token")
	if err == nil {
		t.Fatal("esperava erro para HTTP 500, obteve nil")
	}
}

// TestHabilitar_DataHabilitacaoNaoParseavel_NaoFalha confirma o comportamento "melhor
// esforço" documentado: uma DataHabilitacao em formato não reconhecido não derruba a
// chamada — Habilitar devolve o Habilitado recebido e uma dataHabilitacao zero, sem erro
// (o chamador decide o fallback).
func TestHabilitar_DataHabilitacaoNaoParseavel_NaoFalha(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"Habilitado":      "sucesso",
			"DataHabilitacao": "não-é-uma-data-valida",
		})
	}))
	defer srv.Close()

	t.Setenv("CGIBS_HABILITACAO_URL", srv.URL)

	habilitado, dataHabilitacao, err := Habilitar("cid", "secret", "https://exemplo/webhook", "token")
	if err != nil {
		t.Fatalf("esperava sucesso (melhor esforço), obteve erro: %v", err)
	}
	if habilitado != "sucesso" {
		t.Errorf("esperava habilitado=sucesso, veio %q", habilitado)
	}
	if !dataHabilitacao.IsZero() {
		t.Errorf("esperava dataHabilitacao zero (não parseável), veio %v", dataHabilitacao)
	}
}
