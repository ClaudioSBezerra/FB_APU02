package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

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

// GetToken obtains an OAuth2 access token using client_credentials grant.
func (c *RFBClient) GetToken(clientID, clientSecret string) (string, error) {
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

	resp, err := c.httpClient.Do(req)
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
