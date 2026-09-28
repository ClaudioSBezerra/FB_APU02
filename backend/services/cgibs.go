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

// ErrCGIBSNovaSolicitacaoNaoConfigurada é devolvido por NovaSolicitacao quando
// CGIBS_NOVA_SOLICITACAO_URL não está configurada — mesmo padrão de ErrCGIBSNaoConfigurado
// (Habilitar) e ErrCGIBSObterArquivoNaoConfigurado (ObterArquivo). Nenhuma chamada de rede é
// tentada quando este erro é retornado.
var ErrCGIBSNovaSolicitacaoNaoConfigurada = errors.New("CGIBS_NAO_CONFIGURADO: CGIBS_NOVA_SOLICITACAO_URL não configurada")

// ErrCGIBSCancelamentoNaoConfigurado é devolvido por CancelarSolicitacao quando
// CGIBS_CANCELAMENTO_URL não está configurada — mesmo padrão acima.
var ErrCGIBSCancelamentoNaoConfigurado = errors.New("CGIBS_NAO_CONFIGURADO: CGIBS_CANCELAMENTO_URL não configurada")

// cgibsSolicitacaoDateLayouts são os formatos tentados, em ordem, para DataTransacaoIni/
// DataTransacaoFim na resposta de Nova Solicitação. AAAA-MM-DD (data pura) é o formato que NÓS
// enviamos no corpo do request (mesmo padrão de outros campos de data já visto no MOC — ver
// cgibsDataHabilitacaoLayouts) — os demais são fallback caso a CGIBS ecoe com hora/timezone.
var cgibsSolicitacaoDateLayouts = []string{
	"2006-01-02",
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"02/01/2006",
}

// parseCGIBSSolicitacaoDate tenta parsear DataTransacaoIni/DataTransacaoFim nos formatos
// conhecidos. Best-effort: não bloqueia NovaSolicitacao em caso de falha — mesmo padrão de
// parseCGIBSDataHabilitacao.
func parseCGIBSSolicitacaoDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range cgibsSolicitacaoDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// cgibsNovaSolicitacaoRequest é o corpo enviado à API de Nova Solicitação (MOC 5.5). Mesma
// suposição de autenticação documentada em cgibsHabilitacaoRequest (ClientID/ClientSecret no
// corpo, não Bearer).
type cgibsNovaSolicitacaoRequest struct {
	ClientID         string `json:"ClientID"`
	ClientSecret     string `json:"ClientSecret"`
	CNPJ             string `json:"CNPJ"`
	DataTransacaoIni string `json:"DataTransacaoIni"`
	DataTransacaoFim string `json:"DataTransacaoFim"`
}

// cgibsNovaSolicitacaoResponse é a resposta bruta esperada da API de Nova Solicitação — os
// campos de data chegam como string (formato não 100% confirmado pela CGIBS, ver
// cgibsSolicitacaoDateLayouts) e são parseados em NovaSolicitacao antes de devolver
// CGIBSNovaSolicitacaoResp ao chamador.
type cgibsNovaSolicitacaoResponse struct {
	TokenContrib        string `json:"TokenContrib"`
	TipoSolicitacao     string `json:"TipoSolicitacao"`
	SituacaoSolicitacao string `json:"SituacaoSolicitacao"`
	IDSolicitacao       int64  `json:"IDSolicitacao"`
	DataTransacaoIni    string `json:"DataTransacaoIni"`
	DataTransacaoFim    string `json:"DataTransacaoFim"`
	Resultado           string `json:"Resultado"`
}

// CGIBSNovaSolicitacaoResp é o resultado exportado de NovaSolicitacao, já com as datas
// parseadas (zero, sem erro, se a CGIBS devolver um formato não reconhecido — o chamador
// decide o fallback, mesmo princípio de Habilitar/dataHabilitacao).
type CGIBSNovaSolicitacaoResp struct {
	TokenContrib        string
	TipoSolicitacao     string
	SituacaoSolicitacao string
	IDSolicitacao       int64
	DataTransacaoIni    time.Time
	DataTransacaoFim    time.Time
	Resultado           string
}

// NovaSolicitacao chama a API de Nova Solicitação da CGIBS (MOC 5.5) para solicitar o extrato
// de conta corrente fiscal (diferencial ou manual) de um período. Lê CGIBS_NOVA_SOLICITACAO_URL
// do ambiente; se vazia, retorna ErrCGIBSNovaSolicitacaoNaoConfigurada SEM tentar nenhuma
// chamada de rede — mesmo padrão de Habilitar/ObterArquivo.
//
// dataIni/dataFim são formatadas como AAAA-MM-DD no corpo do request (mesmo padrão de outros
// campos de data já visto no MOC).
//
// SUPOSIÇÃO: cnpj é a raiz de 8 dígitos (CNPJ8), não o CNPJ completo de 14 dígitos — a seção 5.5
// do MOC não confirma o formato exato deste campo, mesma limitação de extração documentada em
// spec-cgibs-conta-corrente-plano.md. Se a CGIBS exigir os 14 dígitos, é aqui (e no chamador,
// SolicitarCGIBSApuracaoHandler) que ajustar.
func NovaSolicitacao(clientID, clientSecret, cnpj string, dataIni, dataFim time.Time) (resp CGIBSNovaSolicitacaoResp, err error) {
	endpoint := strings.TrimSpace(os.Getenv("CGIBS_NOVA_SOLICITACAO_URL"))
	if endpoint == "" {
		return CGIBSNovaSolicitacaoResp{}, ErrCGIBSNovaSolicitacaoNaoConfigurada
	}

	payload := cgibsNovaSolicitacaoRequest{
		ClientID:         clientID,
		ClientSecret:     clientSecret,
		CNPJ:             cnpj,
		DataTransacaoIni: dataIni.Format("2006-01-02"),
		DataTransacaoFim: dataFim.Format("2006-01-02"),
	}
	payloadJSON, merr := json.Marshal(payload)
	if merr != nil {
		return CGIBSNovaSolicitacaoResp{}, fmt.Errorf("cgibs nova solicitacao: erro ao montar corpo: %w", merr)
	}

	log.Printf("[CGIBS] Nova Solicitação: POST %s (cnpj=%s periodo=%s a %s)", endpoint, cnpj, payload.DataTransacaoIni, payload.DataTransacaoFim)

	req, rerr := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(payloadJSON)))
	if rerr != nil {
		return CGIBSNovaSolicitacaoResp{}, fmt.Errorf("cgibs nova solicitacao: erro ao criar requisição: %w", rerr)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, derr := cgibsHTTPClient.Do(req)
	if derr != nil {
		return CGIBSNovaSolicitacaoResp{}, fmt.Errorf("cgibs nova solicitacao: requisição falhou: %w", derr)
	}
	defer httpResp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, cgibsMaxJSONBodyBytes))
	log.Printf("[CGIBS] Resposta da Nova Solicitação (HTTP %d): %s", httpResp.StatusCode, TruncateRunes(string(respBody), 500))

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return CGIBSNovaSolicitacaoResp{}, fmt.Errorf("cgibs nova solicitacao: HTTP %d: %s", httpResp.StatusCode, TruncateRunes(string(respBody), 500))
	}

	var parsed cgibsNovaSolicitacaoResponse
	if uerr := json.Unmarshal(respBody, &parsed); uerr != nil {
		return CGIBSNovaSolicitacaoResp{}, fmt.Errorf("cgibs nova solicitacao: erro ao parsear resposta: %w", uerr)
	}

	result := CGIBSNovaSolicitacaoResp{
		TokenContrib:        parsed.TokenContrib,
		TipoSolicitacao:     parsed.TipoSolicitacao,
		SituacaoSolicitacao: parsed.SituacaoSolicitacao,
		IDSolicitacao:       parsed.IDSolicitacao,
		Resultado:           parsed.Resultado,
	}
	if t, ok := parseCGIBSSolicitacaoDate(parsed.DataTransacaoIni); ok {
		result.DataTransacaoIni = t
	} else if parsed.DataTransacaoIni != "" {
		log.Printf("[CGIBS] AVISO: DataTransacaoIni %q não reconhecida em nenhum formato conhecido — devolvendo zero (chamador decide o fallback)", parsed.DataTransacaoIni)
	}
	if t, ok := parseCGIBSSolicitacaoDate(parsed.DataTransacaoFim); ok {
		result.DataTransacaoFim = t
	} else if parsed.DataTransacaoFim != "" {
		log.Printf("[CGIBS] AVISO: DataTransacaoFim %q não reconhecida em nenhum formato conhecido — devolvendo zero (chamador decide o fallback)", parsed.DataTransacaoFim)
	}

	log.Printf("[CGIBS] Nova Solicitação processada: IDSolicitacao=%d SituacaoSolicitacao=%q TipoSolicitacao=%q", result.IDSolicitacao, result.SituacaoSolicitacao, result.TipoSolicitacao)
	return result, nil
}

// cgibsCancelamentoRequest é o corpo enviado à API de Cancelamento (MOC 5.6, inferido do nome
// dado pela spec — seção não extraiu como texto). Mesma suposição de autenticação documentada
// em cgibsHabilitacaoRequest.
type cgibsCancelamentoRequest struct {
	ClientID      string `json:"ClientID"`
	ClientSecret  string `json:"ClientSecret"`
	IDSolicitacao int64  `json:"IDSolicitacao"`
}

// cgibsCancelamentoResponse é a resposta esperada da API de Cancelamento.
type cgibsCancelamentoResponse struct {
	Resultado string `json:"Resultado"`
}

// CancelarSolicitacao chama a API de Cancelamento da CGIBS para cancelar uma solicitação ainda
// em situação 'solicitada' (ainda não gerada/enviada, conforme MOC — a checagem de qual
// situação permite cancelamento é responsabilidade do chamador, não desta função). Lê
// CGIBS_CANCELAMENTO_URL do ambiente; se vazia, retorna ErrCGIBSCancelamentoNaoConfigurado SEM
// tentar nenhuma chamada de rede — mesmo padrão de Habilitar/ObterArquivo/NovaSolicitacao.
func CancelarSolicitacao(clientID, clientSecret string, idSolicitacao int64) (resultado string, err error) {
	endpoint := strings.TrimSpace(os.Getenv("CGIBS_CANCELAMENTO_URL"))
	if endpoint == "" {
		return "", ErrCGIBSCancelamentoNaoConfigurado
	}

	payload := cgibsCancelamentoRequest{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		IDSolicitacao: idSolicitacao,
	}
	payloadJSON, merr := json.Marshal(payload)
	if merr != nil {
		return "", fmt.Errorf("cgibs cancelar solicitacao: erro ao montar corpo: %w", merr)
	}

	log.Printf("[CGIBS] Cancelando solicitação: POST %s (idSolicitacao=%d)", endpoint, idSolicitacao)

	req, rerr := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(payloadJSON)))
	if rerr != nil {
		return "", fmt.Errorf("cgibs cancelar solicitacao: erro ao criar requisição: %w", rerr)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, derr := cgibsHTTPClient.Do(req)
	if derr != nil {
		return "", fmt.Errorf("cgibs cancelar solicitacao: requisição falhou: %w", derr)
	}
	defer httpResp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, cgibsMaxJSONBodyBytes))
	log.Printf("[CGIBS] Resposta do Cancelamento (HTTP %d): %s", httpResp.StatusCode, TruncateRunes(string(respBody), 500))

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", fmt.Errorf("cgibs cancelar solicitacao: HTTP %d: %s", httpResp.StatusCode, TruncateRunes(string(respBody), 500))
	}

	var parsed cgibsCancelamentoResponse
	if uerr := json.Unmarshal(respBody, &parsed); uerr != nil {
		return "", fmt.Errorf("cgibs cancelar solicitacao: erro ao parsear resposta: %w", uerr)
	}

	log.Printf("[CGIBS] Cancelamento processado (idSolicitacao=%d): Resultado=%q", idSolicitacao, parsed.Resultado)
	return parsed.Resultado, nil
}
