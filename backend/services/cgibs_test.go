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

// TestObterArquivo_SemEnvConfigurada_NaoTentaRede confirma que, sem CGIBS_OBTER_ARQUIVO_URL,
// ObterArquivo devolve ErrCGIBSObterArquivoNaoConfigurado sem tentar nenhuma chamada de rede —
// mesmo padrão de Habilitar/ErrCGIBSNaoConfigurado.
func TestObterArquivo_SemEnvConfigurada_NaoTentaRede(t *testing.T) {
	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", "")

	body, err := ObterArquivo("client-id", "client-secret", 123, 1)

	if !errors.Is(err, ErrCGIBSObterArquivoNaoConfigurado) {
		t.Fatalf("esperava ErrCGIBSObterArquivoNaoConfigurado, obteve %v", err)
	}
	if body != nil {
		t.Errorf("esperava body nil, obteve %d bytes", len(body))
	}
}

// TestObterArquivo_ComFakeServer_MontaCorpoEDevolveBytesCrus confirma que ObterArquivo monta o
// corpo com os 4 campos documentados (ClientID, ClientSecret, IDSolicitacao, NumeroSequencial)
// e devolve a resposta 200 como bytes crus, sem tentar decodificar (quem decodifica é o
// parser).
func TestObterArquivo_ComFakeServer_MontaCorpoEDevolveBytesCrus(t *testing.T) {
	var capturedBody map[string]interface{}
	const respostaCrua = `{"nomearquivo":"arquivo1.json","operacoes":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("esperava POST, veio %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("erro ao decodificar corpo recebido: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(respostaCrua))
	}))
	defer srv.Close()

	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	body, err := ObterArquivo("cid-teste", "secret-teste", 456, 2)
	if err != nil {
		t.Fatalf("ObterArquivo retornou erro inesperado: %v", err)
	}

	if capturedBody["ClientID"] != "cid-teste" {
		t.Errorf("ClientID: esperava %q, veio %v", "cid-teste", capturedBody["ClientID"])
	}
	if capturedBody["ClientSecret"] != "secret-teste" {
		t.Errorf("ClientSecret: esperava %q, veio %v", "secret-teste", capturedBody["ClientSecret"])
	}
	if capturedBody["IDSolicitacao"] != float64(456) {
		t.Errorf("IDSolicitacao: esperava 456, veio %v", capturedBody["IDSolicitacao"])
	}
	if capturedBody["NumeroSequencial"] != float64(2) {
		t.Errorf("NumeroSequencial: esperava 2, veio %v", capturedBody["NumeroSequencial"])
	}

	if string(body) != respostaCrua {
		t.Errorf("esperava bytes crus %q, veio %q", respostaCrua, string(body))
	}
}

// TestObterArquivo_HTTPErro_DevolveErro confirma que um status HTTP de erro vira erro Go.
func TestObterArquivo_HTTPErro_DevolveErro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"erro":"falha interna"}`))
	}))
	defer srv.Close()

	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	_, err := ObterArquivo("cid", "secret", 1, 1)
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

// TestObterArquivo_CorpoNoLimiteDoLimitReader_DevolveErroExplicito cobre o item 12 da revisão
// adversarial: um corpo de resposta que atinge (ou excede) o limite de
// cgibsObterArquivoMaxBodyBytes não pode ser silenciosamente truncado e repassado pro parser —
// precisa virar erro explícito.
func TestObterArquivo_CorpoNoLimiteDoLimitReader_DevolveErroExplicito(t *testing.T) {
	// Corpo maior que o limite: preenchimento + um JSON válido no início não importa, o que
	// importa é o tamanho total ultrapassar cgibsObterArquivoMaxBodyBytes.
	oversized := make([]byte, cgibsObterArquivoMaxBodyBytes+1)
	for i := range oversized {
		oversized[i] = 'x'
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(oversized)
	}))
	defer srv.Close()

	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	body, err := ObterArquivo("cid", "secret", 1, 1)
	if err == nil {
		t.Fatal("esperava erro (corpo excede o limite), obteve nil")
	}
	if body != nil {
		t.Errorf("esperava body nil quando o corpo excede o limite, obteve %d bytes", len(body))
	}
}

// TestObterArquivo_CorpoDentroDoLimite_NaoErra confirma que um corpo exatamente no limite (não
// além dele) continua sendo aceito normalmente — a checagem do item 12 não pode rejeitar
// arquivos legítimos de tamanho máximo.
// TestNovaSolicitacao_SemEnvConfigurada_NaoTentaRede confirma que, sem
// CGIBS_NOVA_SOLICITACAO_URL, NovaSolicitacao devolve ErrCGIBSNovaSolicitacaoNaoConfigurada
// sem tentar nenhuma chamada de rede — mesmo padrão de Habilitar/ObterArquivo.
func TestNovaSolicitacao_SemEnvConfigurada_NaoTentaRede(t *testing.T) {
	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", "")

	dataIni := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dataFim := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	resp, err := NovaSolicitacao("client-id", "client-secret", "12345678", dataIni, dataFim)

	if !errors.Is(err, ErrCGIBSNovaSolicitacaoNaoConfigurada) {
		t.Fatalf("esperava ErrCGIBSNovaSolicitacaoNaoConfigurada, obteve %v", err)
	}
	if resp != (CGIBSNovaSolicitacaoResp{}) {
		t.Errorf("esperava resp zero-value, obteve %+v", resp)
	}
}

// TestNovaSolicitacao_ComFakeServer_MontaCorpoEParseiaResposta confirma que NovaSolicitacao
// monta o corpo exatamente com os 5 campos documentados (ClientID, ClientSecret, CNPJ,
// DataTransacaoIni, DataTransacaoFim — datas em AAAA-MM-DD) e parseia corretamente uma
// resposta de sucesso.
func TestNovaSolicitacao_ComFakeServer_MontaCorpoEParseiaResposta(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("esperava POST, veio %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("erro ao decodificar corpo recebido: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"TokenContrib":        "token-abc",
			"TipoSolicitacao":     "diferencial",
			"SituacaoSolicitacao": "solicitada",
			"IDSolicitacao":       12345,
			"DataTransacaoIni":    "2026-01-01",
			"DataTransacaoFim":    "2026-01-31",
			"Resultado":           "sucesso",
		})
	}))
	defer srv.Close()

	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	dataIni := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dataFim := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	resp, err := NovaSolicitacao("cid-teste", "secret-teste", "12345678", dataIni, dataFim)
	if err != nil {
		t.Fatalf("NovaSolicitacao retornou erro inesperado: %v", err)
	}

	if capturedBody["ClientID"] != "cid-teste" {
		t.Errorf("ClientID: esperava %q, veio %v", "cid-teste", capturedBody["ClientID"])
	}
	if capturedBody["ClientSecret"] != "secret-teste" {
		t.Errorf("ClientSecret: esperava %q, veio %v", "secret-teste", capturedBody["ClientSecret"])
	}
	if capturedBody["CNPJ"] != "12345678" {
		t.Errorf("CNPJ: esperava %q, veio %v", "12345678", capturedBody["CNPJ"])
	}
	if capturedBody["DataTransacaoIni"] != "2026-01-01" {
		t.Errorf("DataTransacaoIni: esperava %q, veio %v", "2026-01-01", capturedBody["DataTransacaoIni"])
	}
	if capturedBody["DataTransacaoFim"] != "2026-01-31" {
		t.Errorf("DataTransacaoFim: esperava %q, veio %v", "2026-01-31", capturedBody["DataTransacaoFim"])
	}

	if resp.TokenContrib != "token-abc" {
		t.Errorf("esperava TokenContrib=token-abc, veio %q", resp.TokenContrib)
	}
	if resp.TipoSolicitacao != "diferencial" {
		t.Errorf("esperava TipoSolicitacao=diferencial, veio %q", resp.TipoSolicitacao)
	}
	if resp.SituacaoSolicitacao != "solicitada" {
		t.Errorf("esperava SituacaoSolicitacao=solicitada, veio %q", resp.SituacaoSolicitacao)
	}
	if resp.IDSolicitacao != 12345 {
		t.Errorf("esperava IDSolicitacao=12345, veio %d", resp.IDSolicitacao)
	}
	if resp.Resultado != "sucesso" {
		t.Errorf("esperava Resultado=sucesso, veio %q", resp.Resultado)
	}
	if !resp.DataTransacaoIni.Equal(dataIni) {
		t.Errorf("esperava DataTransacaoIni=%v, veio %v", dataIni, resp.DataTransacaoIni)
	}
	if !resp.DataTransacaoFim.Equal(dataFim) {
		t.Errorf("esperava DataTransacaoFim=%v, veio %v", dataFim, resp.DataTransacaoFim)
	}
}

// TestNovaSolicitacao_HTTPErro_DevolveErro confirma que um status HTTP de erro vira erro Go.
func TestNovaSolicitacao_HTTPErro_DevolveErro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"erro":"falha interna"}`))
	}))
	defer srv.Close()

	t.Setenv("CGIBS_NOVA_SOLICITACAO_URL", srv.URL)

	_, err := NovaSolicitacao("cid", "secret", "12345678", time.Now(), time.Now())
	if err == nil {
		t.Fatal("esperava erro para HTTP 500, obteve nil")
	}
}

// TestCancelarSolicitacao_SemEnvConfigurada_NaoTentaRede confirma que, sem
// CGIBS_CANCELAMENTO_URL, CancelarSolicitacao devolve ErrCGIBSCancelamentoNaoConfigurado sem
// tentar nenhuma chamada de rede.
func TestCancelarSolicitacao_SemEnvConfigurada_NaoTentaRede(t *testing.T) {
	t.Setenv("CGIBS_CANCELAMENTO_URL", "")

	resultado, err := CancelarSolicitacao("client-id", "client-secret", 123)

	if !errors.Is(err, ErrCGIBSCancelamentoNaoConfigurado) {
		t.Fatalf("esperava ErrCGIBSCancelamentoNaoConfigurado, obteve %v", err)
	}
	if resultado != "" {
		t.Errorf("esperava resultado vazio, obteve %q", resultado)
	}
}

// TestCancelarSolicitacao_ComFakeServer_MontaCorpoEParseiaResposta confirma que
// CancelarSolicitacao monta o corpo com os 3 campos documentados (ClientID, ClientSecret,
// IDSolicitacao) e parseia corretamente o Resultado da resposta.
func TestCancelarSolicitacao_ComFakeServer_MontaCorpoEParseiaResposta(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("esperava POST, veio %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("erro ao decodificar corpo recebido: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"Resultado": "cancelado com sucesso"})
	}))
	defer srv.Close()

	t.Setenv("CGIBS_CANCELAMENTO_URL", srv.URL)

	resultado, err := CancelarSolicitacao("cid-teste", "secret-teste", 456)
	if err != nil {
		t.Fatalf("CancelarSolicitacao retornou erro inesperado: %v", err)
	}

	if capturedBody["ClientID"] != "cid-teste" {
		t.Errorf("ClientID: esperava %q, veio %v", "cid-teste", capturedBody["ClientID"])
	}
	if capturedBody["ClientSecret"] != "secret-teste" {
		t.Errorf("ClientSecret: esperava %q, veio %v", "secret-teste", capturedBody["ClientSecret"])
	}
	if capturedBody["IDSolicitacao"] != float64(456) {
		t.Errorf("IDSolicitacao: esperava 456, veio %v", capturedBody["IDSolicitacao"])
	}
	if resultado != "cancelado com sucesso" {
		t.Errorf("esperava Resultado='cancelado com sucesso', veio %q", resultado)
	}
}

// TestCancelarSolicitacao_HTTPErro_DevolveErro confirma que um status HTTP de erro vira erro Go.
func TestCancelarSolicitacao_HTTPErro_DevolveErro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"erro":"falha interna"}`))
	}))
	defer srv.Close()

	t.Setenv("CGIBS_CANCELAMENTO_URL", srv.URL)

	_, err := CancelarSolicitacao("cid", "secret", 1)
	if err == nil {
		t.Fatal("esperava erro para HTTP 500, obteve nil")
	}
}

func TestObterArquivo_CorpoDentroDoLimite_NaoErra(t *testing.T) {
	exact := make([]byte, cgibsObterArquivoMaxBodyBytes)
	for i := range exact {
		exact[i] = 'y'
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(exact)
	}))
	defer srv.Close()

	t.Setenv("CGIBS_OBTER_ARQUIVO_URL", srv.URL)

	body, err := ObterArquivo("cid", "secret", 1, 1)
	if err != nil {
		t.Fatalf("esperava sucesso (corpo exatamente no limite, sem exceder), obteve erro: %v", err)
	}
	if len(body) != cgibsObterArquivoMaxBodyBytes {
		t.Errorf("esperava %d bytes, obteve %d", cgibsObterArquivoMaxBodyBytes, len(body))
	}
}
