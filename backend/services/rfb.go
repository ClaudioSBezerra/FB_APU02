package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Versões da API RFB CBS suportadas. A versão de cada solicitação é escolhida por
// configuração (ResolveRFBAPIVersion) e gravada em rfb_requests.api_versao; o download
// segue a versão da solicitação.
const (
	RFBAPIVersaoV1 = "v1"
	RFBAPIVersaoV2 = "v2"
)

// rfbAPIVersion é o segmento de versão do caminho v1 (apuracao-cbs, creditos-cbs, download).
const rfbAPIVersion = "v1"

// ResolveRFBAPIVersion é o ÚNICO ponto que decide a versão da API para um ambiente.
// RFB_API_VERSION (default v1) vale para todos; RFB_API_VERSION_RESTRITA sobrescreve
// para "producao_restrita". Valor global inválido cai em v1 (default seguro); override
// RESTRITA inválido é ignorado (mantém o valor global válido, em vez de rebaixar para v1).
func ResolveRFBAPIVersion(ambiente string) string {
	return resolveRFBAPIVersion(ambiente, os.Getenv)
}

func resolveRFBAPIVersion(ambiente string, getenv func(string) string) string {
	v := strings.ToLower(strings.TrimSpace(getenv("RFB_API_VERSION")))
	if ambiente == "producao_restrita" {
		if r := strings.ToLower(strings.TrimSpace(getenv("RFB_API_VERSION_RESTRITA"))); r != "" {
			if r == RFBAPIVersaoV1 || r == RFBAPIVersaoV2 {
				v = r
			} else {
				log.Printf("[RFB] AVISO: RFB_API_VERSION_RESTRITA inválida %q — ignorada (mantém %q)", r, v)
			}
		}
	}
	switch v {
	case RFBAPIVersaoV2:
		return RFBAPIVersaoV2
	case "", RFBAPIVersaoV1:
		return RFBAPIVersaoV1
	default:
		log.Printf("[RFB] AVISO: versão de API inválida %q — usando v1", v)
		return RFBAPIVersaoV1
	}
}

// rfbTokenCache caches OAuth2 tokens per client_id across goroutines.
// The RFB API associates tiqueteDownload with the access_token that made
// the assessment request — reusing the same token ensures the download succeeds.
var rfbTokenCache = struct {
	mu     sync.Mutex
	tokens map[string]rfbCachedToken
}{tokens: make(map[string]rfbCachedToken)}

// rfbRateLimitCache stores when the RFB API rate-limit block expires per cnpjBase.
// The block is set from the Retry-After response header on HTTP 429 and prevents
// further requests until the window expires (typically midnight UTC = 21h BRT).
var rfbRateLimitCache = struct {
	mu    sync.Mutex
	until map[string]time.Time
}{until: make(map[string]time.Time)}

// IsRateLimited reports whether the given cnpjBase is currently blocked by the
// RFB API rate limit. Returns (true, expiry) while the block is active.
func IsRateLimited(cnpjBase string) (bool, time.Time) {
	rfbRateLimitCache.mu.Lock()
	defer rfbRateLimitCache.mu.Unlock()
	if t, ok := rfbRateLimitCache.until[cnpjBase]; ok && time.Now().Before(t) {
		return true, t
	}
	return false, time.Time{}
}

// SetRateLimitUntil records an externally-computed rate-limit expiry (e.g. restored
// from the DB on container restart) for the given cnpjBase.
func SetRateLimitUntil(cnpjBase string, until time.Time) {
	rfbRateLimitCache.mu.Lock()
	rfbRateLimitCache.until[cnpjBase] = until
	rfbRateLimitCache.mu.Unlock()
}

type rfbCachedToken struct {
	token     string
	expiresAt time.Time
}

// RFBClient wraps communication with the Receita Federal CBS API.
type RFBClient struct {
	httpClient *http.Client
	baseURL    string // e.g. https://api.receitafederal.gov.br
	tokenURL   string // e.g. https://api.receitafederal.gov.br/token
	webhookURL string // e.g. https://fbtax.cloud/api/rfb/webhook
	pathPrefix string // "rtc" (producao) ou "prr-rtc" (producao_restrita); usado só nos caminhos v1 (a v2 usa apuracao-cbs[-prr])
	ambiente   string // ambiente registrado (define o segmento apuracao-cbs[-prr] na v2)
	apiVersao  string // "v1" (default) ou "v2" — ver SetAPIVersao

	// Credenciais do último GetToken bem-sucedido: permitem renovar o token quando a
	// situacao devolve 401 (token invalidado no gateway antes do fim do cache).
	lastClientID     string
	lastClientSecret string
}

// RFB API response types
type RFBTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// RFBApuracaoResponse é a resposta da solicitação. v1 devolve "tiquete"; v2 devolve
// "tiqueteSolicitacao" (+ "tEASegundos", tempo estimado de atendimento — tolerado como
// RawMessage porque a doc não fixa o tipo: número ou string não pode quebrar o Unmarshal).
type RFBApuracaoResponse struct {
	Tiquete            string          `json:"tiquete"`
	TiqueteSolicitacao string          `json:"tiqueteSolicitacao"`
	TEASegundos        json.RawMessage `json:"tEASegundos"`
	CodigoErro         string          `json:"codigoErro"`
	MensagemErro       string          `json:"mensagemErro"`
}

// TiqueteEfetivo devolve o tíquete da solicitação, seja no formato v1 ou v2.
func (r RFBApuracaoResponse) TiqueteEfetivo() string {
	if r.TiqueteSolicitacao != "" {
		return r.TiqueteSolicitacao
	}
	return r.Tiquete
}

// RFBSituacao é a resposta de GET .../v2/situacao/{tiquete}.
type RFBSituacao struct {
	Estado              string `json:"estado"` // PENDENTE | EM_PROCESSAMENTO | CONCLUIDA | ERRO
	UrlAssinada         string `json:"urlAssinada"`
	UrlAssinadaExpiraEm string `json:"urlAssinadaExpiraEm"`
	CodigoErro          string `json:"codigoErro"`
	MensagemErro        string `json:"mensagemErro"`
}

// NewRFBClient creates a new RFB API client from environment variables.
func NewRFBClient() *RFBClient {
	baseURL := os.Getenv("RFB_API_URL")
	if baseURL == "" {
		baseURL = "https://api.receitafederal.gov.br"
	}

	tokenURL := os.Getenv("RFB_TOKEN_URL")
	if tokenURL == "" {
		tokenURL = baseURL + "/token"
	}

	webhookURL := os.Getenv("RFB_WEBHOOK_URL")
	if webhookURL == "" {
		webhookURL = "https://fbtax.cloud/api/rfb/webhook"
	}

	return &RFBClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		tokenURL:   tokenURL,
		webhookURL: webhookURL,
		pathPrefix: "rtc",
		ambiente:   "producao",
		apiVersao:  RFBAPIVersaoV1,
	}
}

// SetAmbiente configures the API path prefix based on the registered environment.
// "producao_restrita" (beta credentials from credencial-api-beta) uses "prr-rtc".
// All other values default to "rtc" (regular production).
func (c *RFBClient) SetAmbiente(ambiente string) {
	c.ambiente = ambiente
	if ambiente == "producao_restrita" {
		c.pathPrefix = "prr-rtc"
	} else {
		c.pathPrefix = "rtc"
	}
}

// SetAPIVersao define a versão da API usada nas próximas chamadas (RFBAPIVersaoV1/V2).
// Qualquer valor diferente de "v2" é tratado como v1.
func (c *RFBClient) SetAPIVersao(v string) {
	if v == RFBAPIVersaoV2 {
		c.apiVersao = RFBAPIVersaoV2
	} else {
		c.apiVersao = RFBAPIVersaoV1
	}
}

type rfbOp int

const (
	rfbOpSolicitarDebito rfbOp = iota
	rfbOpSolicitarCredito
	rfbOpSituacao
	rfbOpDownload
)

// buildURL é a função única de montagem de URL da API RFB.
//   - v1: {base}/{rtc|prr-rtc}/{apuracao-cbs|creditos-cbs|download}/v1/{id}
//   - v2: {base}/{apuracao-cbs|apuracao-cbs-prr}/v2/{debitos|creditos|situacao}/{id}
//
// O download por tíquete só existe na v1: opDownload é sempre v1, independentemente
// da versão do cliente (é o último degrau do fallback de download).
// id é o CNPJ base (solicitar) ou o tíquete (situacao/download).
func (c *RFBClient) buildURL(op rfbOp, id string) string {
	id = url.PathEscape(id)
	if op == rfbOpDownload {
		return fmt.Sprintf("%s/%s/download/%s/%s", c.baseURL, c.pathPrefix, rfbAPIVersion, id)
	}
	if c.apiVersao == RFBAPIVersaoV2 || op == rfbOpSituacao {
		seg := "apuracao-cbs"
		if c.ambiente == "producao_restrita" {
			seg = "apuracao-cbs-prr"
		}
		switch op {
		case rfbOpSolicitarCredito:
			return fmt.Sprintf("%s/%s/v2/creditos/%s", c.baseURL, seg, id)
		case rfbOpSituacao:
			return fmt.Sprintf("%s/%s/v2/situacao/%s", c.baseURL, seg, id)
		default:
			return fmt.Sprintf("%s/%s/v2/debitos/%s", c.baseURL, seg, id)
		}
	}
	if op == rfbOpSolicitarCredito {
		return fmt.Sprintf("%s/%s/creditos-cbs/%s/%s", c.baseURL, c.pathPrefix, rfbAPIVersion, id)
	}
	return fmt.Sprintf("%s/%s/apuracao-cbs/%s/%s", c.baseURL, c.pathPrefix, rfbAPIVersion, id)
}

// GetToken returns a valid OAuth2 access token for the given client credentials.
// Tokens are cached per client_id (with a 5-minute safety margin before expiry) so
// that the assessment request and subsequent download use the SAME token — required
// by the RFB API which associates tiqueteDownload with the issuing access_token.
func (c *RFBClient) GetToken(clientID, clientSecret string) (string, error) {
	c.lastClientID, c.lastClientSecret = clientID, clientSecret
	rfbTokenCache.mu.Lock()
	if ct, ok := rfbTokenCache.tokens[clientID]; ok && time.Now().Before(ct.expiresAt) {
		rfbTokenCache.mu.Unlock()
		log.Printf("[RFB] Reusing cached token for clientID ...%s (expires in %.0fs)",
			func() string {
				if len(clientID) >= 6 {
					return clientID[len(clientID)-6:]
				}
				return clientID
			}(),
			time.Until(ct.expiresAt).Seconds(),
		)
		return ct.token, nil
	}
	rfbTokenCache.mu.Unlock()

	log.Printf("[RFB] Requesting OAuth2 token from %s", c.tokenURL)

	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", c.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, rfbMaxJSONBodyBytes))

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfterStr := resp.Header.Get("Retry-After")
		if secs, err2 := strconv.Atoi(retryAfterStr); err2 == nil && secs > 0 {
			until := time.Now().Add(time.Duration(secs) * time.Second)
			brtLoc, _ := time.LoadLocation("America/Sao_Paulo")
			log.Printf("[RFB] Token endpoint rate limit — Retry-After: %ds (até %s BRT)",
				secs, until.In(brtLoc).Format("02/01 15:04"))
			return "", fmt.Errorf("RATE_LIMIT_429|retry_until=%s|token endpoint rate limit exceeded (Retry-After: %ds — tente após %s BRT)",
				until.UTC().Format(time.RFC3339), secs, until.In(brtLoc).Format("15:04"))
		}
		log.Printf("[RFB] Token error (HTTP 429): %s", string(body))
		return "", fmt.Errorf("RATE_LIMIT_429|token endpoint rate limit exceeded: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[RFB] Token error (HTTP %d): %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("token request returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp RFBTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access_token in response")
	}

	log.Printf("[RFB] Token obtained — expires_in: %d | token_type: %s | last8: ...%s",
		tokenResp.ExpiresIn, tokenResp.TokenType,
		func() string {
			t := tokenResp.AccessToken
			if len(t) >= 8 {
				return t[len(t)-8:]
			}
			return t
		}(),
	)

	// Cache the token so that assessment and download use the same access_token.
	// Safety margin: expire cache 5 minutes before the real expiry.
	safetyMargin := 300
	if tokenResp.ExpiresIn > safetyMargin {
		rfbTokenCache.mu.Lock()
		rfbTokenCache.tokens[clientID] = rfbCachedToken{
			token:     tokenResp.AccessToken,
			expiresAt: time.Now().Add(time.Duration(tokenResp.ExpiresIn-safetyMargin) * time.Second),
		}
		rfbTokenCache.mu.Unlock()
	}

	return tokenResp.AccessToken, nil
}

// SolicitarApuracao sends a CBS assessment request to the RFB API.
// cnpjBase must be 8 digits (company root CNPJ).
// Returns the tiquete (ticket) for later download.
func (c *RFBClient) SolicitarApuracao(token, cnpjBase string) (string, error) {
	endpoint := c.buildURL(rfbOpSolicitarDebito, cnpjBase)
	log.Printf("[RFB] Requesting CBS assessment: POST %s (webhook: %s, api: %s, ambiente: %s)", endpoint, c.webhookURL, c.apiVersao, c.ambiente)

	payload := map[string]string{
		"urlRetorno": c.webhookURL,
	}
	payloadJSON, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(string(payloadJSON)))
	if err != nil {
		return "", fmt.Errorf("failed to create assessment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("assessment request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, rfbMaxJSONBodyBytes))
	log.Printf("[RFB] Assessment response (HTTP %d): %s", resp.StatusCode, string(body))

	// Log rate-limit and diagnostic headers when request fails
	if resp.StatusCode != http.StatusCreated {
		rateLimitHeaders := []string{
			"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
			"Retry-After", "X-Quota-Limit", "X-Quota-Remaining",
			"X-Request-ID", "X-Correlation-ID",
		}
		for _, h := range rateLimitHeaders {
			if v := resp.Header.Get(h); v != "" {
				log.Printf("[RFB] Header %s: %s", h, v)
			}
		}
	}

	// Handle 429 — parse Retry-After and set in-memory block so subsequent
	// calls (scheduler or manual) are rejected locally without hitting the API again.
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfterStr := resp.Header.Get("Retry-After")
		if secs, err2 := strconv.Atoi(retryAfterStr); err2 == nil && secs > 0 {
			until := time.Now().Add(time.Duration(secs) * time.Second)
			SetRateLimitUntil(cnpjBase, until)
			brtLoc, _ := time.LoadLocation("America/Sao_Paulo")
			log.Printf("[RFB] Rate limit ativo para CNPJ %s — bloqueado até %s BRT (Retry-After: %ds)",
				cnpjBase, until.In(brtLoc).Format("02/01 15:04"), secs)
			return "", fmt.Errorf("RATE_LIMIT_429|retry_until=%s|API rate limit exceeded (Retry-After: %ds — tente após %s BRT)",
				until.UTC().Format(time.RFC3339), secs, until.In(brtLoc).Format("15:04"))
		}
		return "", fmt.Errorf("RATE_LIMIT_429|API rate limit exceeded: %s", string(body))
	}

	var apuracaoResp RFBApuracaoResponse
	if err := json.Unmarshal(body, &apuracaoResp); err != nil {
		return "", fmt.Errorf("failed to parse assessment response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		errMsg := apuracaoResp.MensagemErro
		if errMsg == "" {
			errMsg = string(body)
		}
		return "", fmt.Errorf("assessment returned HTTP %d: [%s] %s", resp.StatusCode, apuracaoResp.CodigoErro, errMsg)
	}

	tiquete := apuracaoResp.TiqueteEfetivo()
	if tiquete == "" {
		return "", fmt.Errorf("empty tiquete in response")
	}

	log.Printf("[RFB] Assessment requested successfully (%s), tiquete: %s", c.apiVersao, tiquete)
	return tiquete, nil
}

// SolicitarCredito sends a CBS credits request to the RFB API (/creditos-cbs/v1/ or /v2/creditos/).
// cnpjBase must be 8 digits. Returns the tiquete for later download.
func (c *RFBClient) SolicitarCredito(token, cnpjBase string) (string, error) {
	endpoint := c.buildURL(rfbOpSolicitarCredito, cnpjBase)
	log.Printf("[RFB] Requesting CBS credits: POST %s (webhook: %s, api: %s, ambiente: %s)", endpoint, c.webhookURL, c.apiVersao, c.ambiente)

	payload := map[string]string{"urlRetorno": c.webhookURL}
	payloadJSON, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(string(payloadJSON)))
	if err != nil {
		return "", fmt.Errorf("failed to create credits request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("credits request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, rfbMaxJSONBodyBytes))
	log.Printf("[RFB] Credits response (HTTP %d): %s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusCreated {
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfterStr := resp.Header.Get("Retry-After")
			if secs, err2 := strconv.Atoi(retryAfterStr); err2 == nil && secs > 0 {
				until := time.Now().Add(time.Duration(secs) * time.Second)
				SetRateLimitUntil(cnpjBase, until)
				brtLoc, _ := time.LoadLocation("America/Sao_Paulo")
				return "", fmt.Errorf("RATE_LIMIT_429|retry_until=%s|API rate limit exceeded (Retry-After: %ds — tente após %s BRT)",
					until.UTC().Format(time.RFC3339), secs, until.In(brtLoc).Format("15:04"))
			}
			return "", fmt.Errorf("RATE_LIMIT_429|API rate limit exceeded: %s", string(body))
		}
	}

	var apuracaoResp RFBApuracaoResponse
	if err := json.Unmarshal(body, &apuracaoResp); err != nil {
		return "", fmt.Errorf("failed to parse credits response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		errMsg := apuracaoResp.MensagemErro
		if errMsg == "" {
			errMsg = string(body)
		}
		return "", fmt.Errorf("credits returned HTTP %d: [%s] %s", resp.StatusCode, apuracaoResp.CodigoErro, errMsg)
	}

	tiquete := apuracaoResp.TiqueteEfetivo()
	if tiquete == "" {
		return "", fmt.Errorf("empty tiquete in credits response")
	}

	log.Printf("[RFB] Credits requested successfully (%s), tiquete: %s", c.apiVersao, tiquete)
	return tiquete, nil
}

// DownloadArquivo downloads the CBS assessment JSON file using the ticket (v1: /download/v1/{tiquete}).
// Returns the raw JSON bytes. Note: each ticket can only be downloaded ONCE.
// O corpo é limitado a rfbMaxDownloadBytes (RFBErrTooLarge se exceder); 200 sem bytes é erro.
func (c *RFBClient) DownloadArquivo(token, tiquete string) ([]byte, error) {
	endpoint := c.buildURL(rfbOpDownload, tiquete)
	log.Printf("[RFB] Downloading assessment file: GET %s (prefix: %s)", endpoint, c.pathPrefix)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	downloadClient := &http.Client{Timeout: 15 * time.Minute}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, rfbMaxErrBodyBytes))
		body := rfbTruncBody(string(errBody))
		// Log the response to diagnose gateway vs application errors.
		// Note: documented responses are 200/400/403/404 — HTTP 401 indicates
		// gateway-level authentication failure, NOT an application error.
		log.Printf("[RFB] Download FAILED — HTTP %d | URL: %s | Body: %s | Token (last 8): ...%s",
			resp.StatusCode, endpoint, body,
			func() string {
				if len(token) >= 8 {
					return token[len(token)-8:]
				}
				return token
			}(),
		)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("HTTP 401 (gateway auth) — token inválido ou expirado: %s", body)
		case http.StatusForbidden:
			return nil, fmt.Errorf("HTTP 403 — CNPJ do consumidor não corresponde ao CNPJ da solicitação: %s", body)
		case http.StatusNotFound:
			return nil, fmt.Errorf("HTTP 404 — arquivo não encontrado ou tíquete inválido: %s", body)
		default:
			return nil, fmt.Errorf("HTTP %d — %s", resp.StatusCode, body)
		}
	}

	body, err := readBodyLimited(resp.Body, rfbMaxDownloadBytes)
	if err != nil {
		return nil, fmt.Errorf("download por tíquete: %w", err)
	}

	log.Printf("[RFB] Download completed successfully (%d bytes)", len(body))
	return body, nil
}

// invalidateCachedToken remove do cache global o token indicado (ex.: 401 no gateway).
func invalidateCachedToken(token string) {
	rfbTokenCache.mu.Lock()
	defer rfbTokenCache.mu.Unlock()
	for id, ct := range rfbTokenCache.tokens {
		if ct.token == token {
			delete(rfbTokenCache.tokens, id)
		}
	}
}

// ConsultarSituacao consulta GET .../v2/situacao/{tiquete} (com Bearer). Serve de rede de
// segurança para webhook perdido e para renovar uma urlAssinada expirada. Em 401 invalida
// o token no cache e repete UMA vez com token novo (se o client conhece as credenciais).
func (c *RFBClient) ConsultarSituacao(token, tiquete string) (*RFBSituacao, error) {
	sit, _, err := c.consultarSituacao(token, tiquete)
	return sit, err
}

// consultarSituacao é ConsultarSituacao devolvendo também o token efetivamente usado
// (diferente do recebido se houve renovação por 401), para o chamador reaproveitá-lo.
func (c *RFBClient) consultarSituacao(token, tiquete string) (*RFBSituacao, string, error) {
	sit, status, err := c.consultarSituacaoOnce(token, tiquete)
	if err == nil || status != http.StatusUnauthorized {
		return sit, token, err
	}
	invalidateCachedToken(token)
	if c.lastClientID == "" {
		return nil, token, err
	}
	log.Printf("[RFB] situacao devolveu 401 — token invalidado, renovando e repetindo 1x")
	newToken, terr := c.GetToken(c.lastClientID, c.lastClientSecret)
	if terr != nil {
		return nil, token, fmt.Errorf("%v (renovação do token falhou: %w)", err, terr)
	}
	sit, _, err = c.consultarSituacaoOnce(newToken, tiquete)
	return sit, newToken, err
}

func (c *RFBClient) consultarSituacaoOnce(token, tiquete string) (*RFBSituacao, int, error) {
	endpoint := c.buildURL(rfbOpSituacao, tiquete)
	log.Printf("[RFB] Consultando situação: GET %s", endpoint)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create situacao request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("situacao request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, rfbMaxJSONBodyBytes))
	if resp.StatusCode != http.StatusOK {
		// Corpo de erro do gateway RFB (não da URL assinada): truncado, sem urlAssinada.
		return nil, resp.StatusCode, fmt.Errorf("situacao HTTP %d — %s", resp.StatusCode, rfbTruncBody(string(body)))
	}
	var sit RFBSituacao
	if err := json.Unmarshal(body, &sit); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to parse situacao response: %w", err)
	}
	// Não loga o corpo (contém urlAssinada) — só o estado.
	log.Printf("[RFB] Situação do tíquete %s: %s", tiquete, sit.Estado)
	return &sit, resp.StatusCode, nil
}

// rfbTruncBody limita um corpo de resposta a 300 runes (UTF-8-safe) para log/erro.
func rfbTruncBody(s string) string {
	t := TruncateRunes(s, 300)
	if len(t) < len(s) {
		return t + "..."
	}
	return t
}

// TruncateRunes é o helper ÚNICO de truncamento de texto que vai para o banco/log:
// corta por RUNES (nunca no meio de um caractere multibyte), troca bytes UTF-8 inválidos
// por U+FFFD e remove NUL (o Postgres rejeita ambos). max <= 0 devolve "".
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}
