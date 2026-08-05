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
)

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
	pathPrefix string // "rtc" (producao) or "prr-rtc" (producao_restrita / beta)
}

// RFB API response types
type RFBTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type RFBApuracaoResponse struct {
	Tiquete       string `json:"tiquete"`
	CodigoErro    string `json:"codigoErro"`
	MensagemErro  string `json:"mensagemErro"`
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
	}
}

// SetAmbiente configures the API path prefix based on the registered environment.
// "producao_restrita" (beta credentials from credencial-api-beta) uses "prr-rtc".
// All other values default to "rtc" (regular production).
func (c *RFBClient) SetAmbiente(ambiente string) {
	if ambiente == "producao_restrita" {
		c.pathPrefix = "prr-rtc"
	} else {
		c.pathPrefix = "rtc"
	}
}

// GetToken returns a valid OAuth2 access token for the given client credentials.
// Tokens are cached per client_id (with a 5-minute safety margin before expiry) so
// that the assessment request and subsequent download use the SAME token — required
// by the RFB API which associates tiqueteDownload with the issuing access_token.
func (c *RFBClient) GetToken(clientID, clientSecret string) (string, error) {
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

	body, _ := io.ReadAll(resp.Body)

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
	endpoint := fmt.Sprintf("%s/%s/apuracao-cbs/v1/%s", c.baseURL, c.pathPrefix, cnpjBase)
	log.Printf("[RFB] Requesting CBS assessment: POST %s (webhook: %s, prefix: %s)", endpoint, c.webhookURL, c.pathPrefix)

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

	body, _ := io.ReadAll(resp.Body)
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

	if apuracaoResp.Tiquete == "" {
		return "", fmt.Errorf("empty tiquete in response")
	}

	log.Printf("[RFB] Assessment requested successfully, tiquete: %s", apuracaoResp.Tiquete)
	return apuracaoResp.Tiquete, nil
}

// SolicitarCredito sends a CBS credits request to the RFB API using /creditos-cbs/v1/.
// cnpjBase must be 8 digits. Returns the tiquete for later download.
func (c *RFBClient) SolicitarCredito(token, cnpjBase string) (string, error) {
	endpoint := fmt.Sprintf("%s/%s/creditos-cbs/v1/%s", c.baseURL, c.pathPrefix, cnpjBase)
	log.Printf("[RFB] Requesting CBS credits: POST %s (webhook: %s, prefix: %s)", endpoint, c.webhookURL, c.pathPrefix)

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

	body, _ := io.ReadAll(resp.Body)
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

	if apuracaoResp.Tiquete == "" {
		return "", fmt.Errorf("empty tiquete in credits response")
	}

	log.Printf("[RFB] Credits requested successfully, tiquete: %s", apuracaoResp.Tiquete)
	return apuracaoResp.Tiquete, nil
}

// DownloadArquivo downloads the CBS assessment JSON file using the ticket.
// Returns the raw JSON bytes. Note: each ticket can only be downloaded ONCE.
func (c *RFBClient) DownloadArquivo(token, tiquete string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/%s/download/v1/%s", c.baseURL, c.pathPrefix, tiquete)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read download response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Log the full response to diagnose gateway vs application errors.
		// Note: documented responses are 200/400/403/404 — HTTP 401 indicates
		// gateway-level authentication failure, NOT an application error.
		log.Printf("[RFB] Download FAILED — HTTP %d | URL: %s | Body: %s | Token (last 8): ...%s",
			resp.StatusCode, endpoint, string(body),
			func() string {
				if len(token) >= 8 {
					return token[len(token)-8:]
				}
				return token
			}(),
		)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("HTTP 401 (gateway auth) — token inválido ou expirado: %s", string(body))
		case http.StatusForbidden:
			return nil, fmt.Errorf("HTTP 403 — CNPJ do consumidor não corresponde ao CNPJ da solicitação: %s", string(body))
		case http.StatusNotFound:
			return nil, fmt.Errorf("HTTP 404 — arquivo não encontrado ou tíquete inválido: %s", string(body))
		default:
			return nil, fmt.Errorf("HTTP %d — %s", resp.StatusCode, string(body))
		}
	}

	log.Printf("[RFB] Download completed successfully (%d bytes)", len(body))
	return body, nil
}
