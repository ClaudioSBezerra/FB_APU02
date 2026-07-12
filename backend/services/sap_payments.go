package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxTokenResponseBytes limita a leitura do corpo de resposta do /token — bem
// acima do tamanho esperado de um JSON de token, mas evita que uma resposta
// arbitrariamente grande de um host mal configurado esgote memória.
const maxTokenResponseBytes = 1 << 20 // 1 MiB

// maxLoggedBodyChars limita quantos caracteres do corpo de resposta externo
// entram em mensagens de erro/log — o corpo completo nunca é necessário para
// diagnóstico e um corpo grande/malicioso não deve inflar logs indefinidamente.
const maxLoggedBodyChars = 500

// sapTokenResponse é o formato de resposta do endpoint OAuth2 /token do SAP.
type sapTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// GetToken obtém um access_token OAuth2 (client_credentials) do SAP.
// Diferente de RFBClient.GetToken (services/rfb.go), não há cache de token
// aqui — esta função serve tanto o teste de conexão (Story 1.2, chamada
// única) quanto, futuramente, o motor de sincronização da Epic 2, que pode
// adicionar cache por cima se necessário.
func GetToken(baseURL, clientID, clientSecret string) (string, error) {
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("base URL inválida: %w", err)
	}
	tokenURL := parsedBase.JoinPath("token").String()

	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("falha ao criar requisição de token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("requisição de token falhou: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request retornou HTTP %d: %s", resp.StatusCode, truncateForLog(body))
	}

	var tokenResp sapTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("falha ao parsear resposta de token (corpo: %s): %w", truncateForLog(body), err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("access_token vazio na resposta")
	}

	return tokenResp.AccessToken, nil
}

// truncateForLog corta o corpo de uma resposta externa antes de incluí-lo em
// mensagens de erro/log, evitando logs inflados ou injeção de conteúdo arbitrário
// de um host de terceiros.
func truncateForLog(body []byte) string {
	s := string(body)
	if len(s) > maxLoggedBodyChars {
		return s[:maxLoggedBodyChars] + "...(truncado)"
	}
	return s
}
