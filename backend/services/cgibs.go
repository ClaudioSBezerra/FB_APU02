package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrCGIBSNaoConfigurado é devolvido por Habilitar quando CGIBS_HABILITACAO_URL não está
// configurada — ainda não temos a URL real confirmada pela CGIBS (seções 4.3/4.4 do MOC não
// extraíram como texto, ver spec-cgibs-conta-corrente-plano.md). Nenhuma chamada de rede é
// tentada quando este erro é retornado.
var ErrCGIBSNaoConfigurado = errors.New("CGIBS_NAO_CONFIGURADO: CGIBS_HABILITACAO_URL não configurada")

// cgibsHTTPClient tem timeout de 30s — mesmo padrão do cliente RFB (ver httpClient em
// NewRFBClient, services/rfb.go).
var cgibsHTTPClient = &http.Client{Timeout: 30 * time.Second}

// cgibsMaxJSONBodyBytes limita o corpo de resposta da API de Habilitação (mesma ordem de
// grandeza de rfbMaxJSONBodyBytes).
const cgibsMaxJSONBodyBytes = 1 << 20

// cgibsHabilitacaoRequest é o corpo enviado à API de Habilitação do Contribuinte (MOC 5.1).
//
// Autenticação por credencial no corpo (ClientID+ClientSecret), NÃO OAuth2 Bearer como a
// RFB: inferido da doc — todo endpoint especificado no MOC lista ClientID/ClientSecret como
// parâmetros obrigatórios do próprio corpo/tabela de parâmetros, diferente da RFB (token
// OAuth2 obtido uma vez via /token e reutilizado como Bearer nas chamadas seguintes). Se a
// CGIBS usar Bearer na prática, ajustar esta função é um ponto único de mudança (mesmo
// princípio já usado em rfb.go — ver Design Notes da spec).
type cgibsHabilitacaoRequest struct {
	ClientID       string `json:"ClientID"`
	ClientSecret   string `json:"ClientSecret"`
	QueroHabilitar string `json:"QueroHabilitar"`
	WebhookURL     string `json:"WebhookURL"`
	TokenContrib   string `json:"TokenContrib"`
}

// cgibsHabilitacaoResponse é a resposta esperada da API de Habilitação do Contribuinte.
// Habilitado é tratado como string (não bool) porque a doc não confirma o tipo exato do
// campo — o chamador (handler) decide o que conta como sucesso.
type cgibsHabilitacaoResponse struct {
	Habilitado      string `json:"Habilitado"`
	DataHabilitacao string `json:"DataHabilitacao"`
}

// cgibsDataHabilitacaoLayouts são os formatos tentados, em ordem, para DataHabilitacao —
// o formato exato não está confirmado pela CGIBS (mesma limitação de extração do MOC
// documentada em spec-cgibs-conta-corrente-plano.md). RFC3339 primeiro (mais comum em
// API JSON), depois formatos de data/hora sem timezone e data BR.
var cgibsDataHabilitacaoLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"02/01/2006",
}

// parseCGIBSDataHabilitacao tenta parsear DataHabilitacao nos formatos conhecidos.
// Best-effort: não bloqueia Habilitar em caso de falha (ver comentário em Habilitar).
func parseCGIBSDataHabilitacao(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range cgibsDataHabilitacaoLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Habilitar chama a API de Habilitação do Contribuinte da CGIBS (MOC 5.1) para registrar o
// contribuinte e a URL de webhook. Lê CGIBS_HABILITACAO_URL do ambiente; se vazia, retorna
// ErrCGIBSNaoConfigurado SEM tentar nenhuma chamada de rede.
//
// Devolve o valor bruto de "Habilitado" (string — o chamador decide o que conta como
// sucesso) e a data de habilitação (melhor esforço: se a resposta trouxer um valor que não
// conseguimos parsear num formato conhecido, devolve time.Time zero sem erro — o chamador
// decide o fallback, ex. usar o horário local de recebimento).
func Habilitar(clientID, clientSecret, webhookURL, tokenContrib string) (habilitado string, dataHabilitacao time.Time, err error) {
	endpoint := strings.TrimSpace(os.Getenv("CGIBS_HABILITACAO_URL"))
	if endpoint == "" {
		return "", time.Time{}, ErrCGIBSNaoConfigurado
	}

	payload := cgibsHabilitacaoRequest{
		ClientID:       clientID,
		ClientSecret:   clientSecret,
		QueroHabilitar: "HAB",
		WebhookURL:     webhookURL,
		TokenContrib:   tokenContrib,
	}
	payloadJSON, merr := json.Marshal(payload)
	if merr != nil {
		return "", time.Time{}, fmt.Errorf("cgibs habilitar: erro ao montar corpo: %w", merr)
	}

	log.Printf("[CGIBS] Habilitando contribuinte: POST %s (webhook: %s)", endpoint, webhookURL)

	req, rerr := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(payloadJSON)))
	if rerr != nil {
		return "", time.Time{}, fmt.Errorf("cgibs habilitar: erro ao criar requisição: %w", rerr)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, derr := cgibsHTTPClient.Do(req)
	if derr != nil {
		return "", time.Time{}, fmt.Errorf("cgibs habilitar: requisição falhou: %w", derr)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, cgibsMaxJSONBodyBytes))
	log.Printf("[CGIBS] Resposta da Habilitação (HTTP %d): %s", resp.StatusCode, TruncateRunes(string(respBody), 500))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("cgibs habilitar: HTTP %d: %s", resp.StatusCode, TruncateRunes(string(respBody), 500))
	}

	var parsed cgibsHabilitacaoResponse
	if uerr := json.Unmarshal(respBody, &parsed); uerr != nil {
		return "", time.Time{}, fmt.Errorf("cgibs habilitar: erro ao parsear resposta: %w", uerr)
	}

	habilitado = parsed.Habilitado
	if t, ok := parseCGIBSDataHabilitacao(parsed.DataHabilitacao); ok {
		dataHabilitacao = t
	} else if parsed.DataHabilitacao != "" {
		log.Printf("[CGIBS] AVISO: DataHabilitacao %q não reconhecida em nenhum formato conhecido — devolvendo zero (chamador decide o fallback)", parsed.DataHabilitacao)
	}

	log.Printf("[CGIBS] Habilitação processada: Habilitado=%q DataHabilitacao=%q", habilitado, parsed.DataHabilitacao)
	return habilitado, dataHabilitacao, nil
}

// ErrCGIBSObterArquivoNaoConfigurado é devolvido por ObterArquivo quando
// CGIBS_OBTER_ARQUIVO_URL não está configurada — mesmo padrão de ErrCGIBSNaoConfigurado
// (Habilitar). Nenhuma chamada de rede é tentada quando este erro é retornado.
var ErrCGIBSObterArquivoNaoConfigurado = errors.New("CGIBS_NAO_CONFIGURADO: CGIBS_OBTER_ARQUIVO_URL não configurada")

// cgibsObterArquivoMaxBodyBytes limita o corpo de resposta da API Obter Arquivo (MOC 5.3) —
// o arquivo devolvido (extrato_cc de um lote de operações) pode ser maior que a resposta de
// Habilitação; teto generoso (16 MB) sobre um arquivo individual de conta corrente fiscal.
const cgibsObterArquivoMaxBodyBytes = 16 << 20

// cgibsObterArquivoRequest é o corpo enviado à API Obter Arquivo (MOC 5.3). Mesma suposição de
// autenticação documentada em cgibsHabilitacaoRequest (ClientID/ClientSecret no corpo, não
// Bearer) — a seção 5.3 do MOC não extraiu como texto (ver spec-cgibs-conta-corrente-plano.md).
type cgibsObterArquivoRequest struct {
	ClientID         string `json:"ClientID"`
	ClientSecret     string `json:"ClientSecret"`
	IDSolicitacao    int64  `json:"IDSolicitacao"`
	NumeroSequencial int64  `json:"NumeroSequencial"`
}

// ObterArquivo chama a API Obter Arquivo da CGIBS (MOC 5.3) para baixar um arquivo específico
// (numeroSequencial) de uma solicitação (idSolicitacao). Lê CGIBS_OBTER_ARQUIVO_URL do
// ambiente; se vazia, retorna ErrCGIBSObterArquivoNaoConfigurado SEM tentar nenhuma chamada de
// rede — mesmo padrão de Habilitar.
//
// A resposta HTTP 200 É o próprio arquivo (JSON cru do ANEXO I — header/parametrosgeracao/
// operacoes/extrato_cc), sem envelope — devolvida como bytes crus, sem nenhuma tentativa de
// decodificar aqui; quem faz o parse é ProcessarArquivoCGIBS (cgibs_parser.go).
func ObterArquivo(clientID, clientSecret string, idSolicitacao, numeroSequencial int64) ([]byte, error) {
	endpoint := strings.TrimSpace(os.Getenv("CGIBS_OBTER_ARQUIVO_URL"))
	if endpoint == "" {
		return nil, ErrCGIBSObterArquivoNaoConfigurado
	}

	payload := cgibsObterArquivoRequest{
		ClientID:         clientID,
		ClientSecret:     clientSecret,
		IDSolicitacao:    idSolicitacao,
		NumeroSequencial: numeroSequencial,
	}
	payloadJSON, merr := json.Marshal(payload)
	if merr != nil {
		return nil, fmt.Errorf("cgibs obter arquivo: erro ao montar corpo: %w", merr)
	}

	log.Printf("[CGIBS] Obtendo arquivo: POST %s (idSolicitacao=%d numeroSequencial=%d)", endpoint, idSolicitacao, numeroSequencial)

	req, rerr := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(payloadJSON)))
	if rerr != nil {
		return nil, fmt.Errorf("cgibs obter arquivo: erro ao criar requisição: %w", rerr)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, derr := cgibsHTTPClient.Do(req)
	if derr != nil {
		return nil, fmt.Errorf("cgibs obter arquivo: requisição falhou: %w", derr)
	}
	defer resp.Body.Close()

	// Lê 1 byte a mais do que o limite: se o corpo real for maior que
	// cgibsObterArquivoMaxBodyBytes (inclusive exatamente no limite, caso em que não dá pra
	// distinguir "corpo de tamanho exato" de "corpo maior truncado" sem essa checagem), devolve
	// erro explícito em vez de passar um JSON silenciosamente truncado pro parser (item 12 da
	// revisão adversarial).
	respBody, berr := io.ReadAll(io.LimitReader(resp.Body, cgibsObterArquivoMaxBodyBytes+1))
	if berr != nil {
		return nil, fmt.Errorf("cgibs obter arquivo: erro ao ler corpo da resposta: %w", berr)
	}
	if len(respBody) > cgibsObterArquivoMaxBodyBytes {
		return nil, fmt.Errorf("cgibs obter arquivo: corpo da resposta atingiu o limite de %d bytes (possível truncamento) — arquivo não processado", cgibsObterArquivoMaxBodyBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cgibs obter arquivo: HTTP %d: %s", resp.StatusCode, TruncateRunes(string(respBody), 500))
	}

	log.Printf("[CGIBS] Arquivo obtido (idSolicitacao=%d numeroSequencial=%d): %d bytes", idSolicitacao, numeroSequencial, len(respBody))
	return respBody, nil
}
