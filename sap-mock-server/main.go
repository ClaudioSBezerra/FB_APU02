// Command sap-mock-server simula a API SAP "Consulta de Pagamentos por Chave
// de DF-e" (spec técnica v0.1, 09/07/2026, time FI/Basis) enquanto a API real
// não existe. NÃO é código de produção — ferramenta de desenvolvimento para
// destravar a Story 1.2 e a Epic 2 (sincronização automática de pagamentos).
//
// Uso:
//
//	go run ./sap-mock-server
//	SAP_MOCK_PORT=9090 go run ./sap-mock-server
//
// Depois, aponte o base_url de uma credencial SAP de teste (Story 1.1,
// tela /config/sap-credenciais) para http://localhost:8090 (ou a porta
// configurada). Client ID/Secret podem ser qualquer valor não vazio —
// veja "Credenciais e casos de erro forçados" abaixo.
//
// Endpoints simulados (contrato idêntico ao da spec):
//   - POST /token                                    — OAuth2 client_credentials
//   - GET  /sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/PaymentByDFe(dfeKey='{chave}')
//   - POST /sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/SearchPayments
//   - GET  /health
//
// Cenários determinísticos por chave: o dígito de checksum da própria chave
// decide o cenário (PAGO_TOTAL, PAGO_PARCIAL, EM_ABERTO, NAO_LOCALIZADO,
// FALLBACK ambíguo), então a mesma chave sempre produz a mesma resposta —
// útil para testes repetíveis.
//
// Credenciais e casos de erro forçados:
//   - client_id = "erro_401" no /token           → 401 (credencial inválida)
//   - chave iniciando com "ERR429"               → 429 com Retry-After
//   - chave iniciando com "ERR500"                → 500
//   - chave malformada (nem 44 nem 50 dígitos)    → 400 (consulta individual)
//     em lote, uma chave malformada não derruba o lote — volta como
//     dfeType=DESCONHECIDO/matchType=NAO_LOCALIZADO, igual ao comportamento
//     esperado do lado FB_APU02 (ver Story 2.2).
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const maxBatchSize = 500

// ─── Contrato de resposta (idêntico à spec) ────────────────────────────────

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type paymentItem struct {
	ClearingDocument   string  `json:"clearingDocument"`
	ClearingDate       string  `json:"clearingDate"`
	PaymentPostingDate string  `json:"paymentPostingDate"`
	PaidAmount         float64 `json:"paidAmount"`
	PaymentMethod      string  `json:"paymentMethod"`
	PaymentMethodDesc  string  `json:"paymentMethodDesc"`
	SettlementMode     string  `json:"settlementMode"`
	IsPartial          bool    `json:"isPartial"`
}

type dfeResponse struct {
	DFeKey            string        `json:"dfeKey"`
	DFeType           string        `json:"dfeType"`
	MatchType         string        `json:"matchType"`
	CompanyCode       string        `json:"companyCode"`
	SupplierCNPJ      string        `json:"supplierCNPJ"`
	SupplierID        string        `json:"supplierId"`
	InvoiceDocument   string        `json:"invoiceDocument"`
	InvoiceFiscalYear string        `json:"invoiceFiscalYear"`
	InvoiceDate       string        `json:"invoiceDate"`
	InvoiceAmount     float64       `json:"invoiceAmount"`
	Currency          string        `json:"currency"`
	PaymentStatus     string        `json:"paymentStatus"`
	TotalPaidAmount   float64       `json:"totalPaidAmount"`
	Payments          []paymentItem `json:"payments"`
	FallbackNote      string        `json:"fallbackNote,omitempty"`
}

type apiError struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	Target        string `json:"target,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
}

// ─── Estado do processo (rate-limit simulado, opcional) ───────────────────

var requestCounter int64

func rateLimitSimulationEnabled() bool {
	return os.Getenv("SAP_MOCK_SIMULATE_RATE_LIMIT") == "true"
}

// ─── main ───────────────────────────────────────────────────────────────

func main() {
	port := os.Getenv("SAP_MOCK_PORT")
	if port == "" {
		port = "8090"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/token", handleToken)
	// A sintaxe OData "PaymentByDFe(dfeKey='...')" não bate com um path exato
	// do ServeMux (o "(" faz parte do path) — roteamos pelo prefixo do
	// diretório e despachamos manualmente pelo restante do path.
	mux.HandleFunc("/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/", handleDFePaymentSubtree)

	addr := ":" + port
	log.Printf("[sap-mock-server] simulando API SAP em http://localhost%s — NÃO é ambiente de produção", addr)
	log.Printf("[sap-mock-server] aponte base_url de uma credencial de teste (tela /config/sap-credenciais) para http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "sap-mock-server"})
}

// ─── /token — OAuth2 client_credentials ───────────────────────────────────

func handleToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID, _, ok := r.BasicAuth()
	if !ok || clientID == "" {
		writeAPIError(w, http.StatusUnauthorized, "invalid_client", "client_id/client_secret ausentes ou mal formatados")
		return
	}

	// client_id mágico para forçar falha de autenticação (Story 1.2, 2.2)
	if clientID == "erro_401" {
		writeAPIError(w, http.StatusUnauthorized, "invalid_client", "credencial inválida (simulado)")
		return
	}

	if err := r.ParseForm(); err != nil || r.FormValue("grant_type") != "client_credentials" {
		writeAPIError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type deve ser client_credentials")
		return
	}

	json.NewEncoder(w).Encode(tokenResponse{
		AccessToken: "sap-mock-token-" + randomHex(16),
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})
}

// ─── Roteador do subtree .../0001/ — despacha entre PaymentByDFe e SearchPayments ──

func handleDFePaymentSubtree(w http.ResponseWriter, r *http.Request) {
	remainder := strings.TrimPrefix(r.URL.Path, "/sap/opu/odata4/sap/zapi_dfe_payment/srvd/sap/dfe_payment/0001/")
	switch {
	case strings.HasPrefix(remainder, "PaymentByDFe("):
		handlePaymentByDFe(w, r, remainder)
	case remainder == "SearchPayments":
		handleSearchPayments(w, r)
	default:
		http.NotFound(w, r)
	}
}

// ─── GET PaymentByDFe(dfeKey='{chave}') — consulta individual ─────────────

func handlePaymentByDFe(w http.ResponseWriter, r *http.Request, pathRemainder string) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireBearerToken(w, r) {
		return
	}
	if limited, retryAfter := checkSimulatedRateLimit(); limited {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "limite de requisições excedido (simulado)")
		return
	}

	dfeKey := extractDFeKeyParam(pathRemainder)
	if dfeKey == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_key", "parâmetro dfeKey ausente ou mal formatado")
		return
	}

	if strings.HasPrefix(dfeKey, "ERR429") {
		w.Header().Set("Retry-After", "5")
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "limite de requisições excedido (forçado por chave de teste)")
		return
	}
	if strings.HasPrefix(dfeKey, "ERR500") {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "erro interno simulado")
		return
	}
	if !isValidKeyLength(dfeKey) {
		writeAPIError(w, http.StatusBadRequest, "invalid_key", fmt.Sprintf("chave %q não tem 44 (NF-e/CT-e) nem 50 (NFS-e) dígitos", dfeKey))
		return
	}

	json.NewEncoder(w).Encode(simulatePayment(dfeKey))
}

// ─── POST SearchPayments — consulta em lote ───────────────────────────────

func handleSearchPayments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireBearerToken(w, r) {
		return
	}
	if limited, retryAfter := checkSimulatedRateLimit(); limited {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "limite de requisições excedido (simulado)")
		return
	}

	var req struct {
		DFeKeys []string `json:"dfeKeys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_body", "corpo da requisição inválido")
		return
	}

	// FB_APU02 decidiu operar com lotes de até 500 (ver PRD FR-2) — o mock
	// aplica o mesmo limite documentado na seção 7 da spec.
	if len(req.DFeKeys) > maxBatchSize {
		writeAPIError(w, http.StatusBadRequest, "batch_too_large", fmt.Sprintf("lote com %d chaves excede o limite de %d", len(req.DFeKeys), maxBatchSize))
		return
	}

	// Deduplica preservando a primeira ocorrência, igual ao comportamento
	// documentado na spec ("chaves duplicadas são deduplicadas").
	seen := make(map[string]bool, len(req.DFeKeys))
	results := make([]dfeResponse, 0, len(req.DFeKeys))
	for _, key := range req.DFeKeys {
		if seen[key] {
			continue
		}
		seen[key] = true

		if strings.HasPrefix(key, "ERR429") {
			w.Header().Set("Retry-After", "5")
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "limite de requisições excedido (forçado por chave de teste)")
			return
		}
		if strings.HasPrefix(key, "ERR500") {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "erro interno simulado")
			return
		}
		if !isValidKeyLength(key) {
			// Chave malformada dentro de um lote não derruba o lote inteiro —
			// volta como não localizada, para exercitar o filtro do lado
			// FB_APU02 (Story 2.2, [ASSUMPTION] de chave malformada em lote).
			results = append(results, dfeResponse{
				DFeKey:        key,
				DFeType:       "DESCONHECIDO",
				MatchType:     "NAO_LOCALIZADO",
				PaymentStatus: "NAO_LOCALIZADO",
			})
			continue
		}
		results = append(results, simulatePayment(key))
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"value": results})
}

// ─── Simulação determinística por chave ───────────────────────────────────

// simulatePayment deriva um cenário determinístico a partir da própria chave,
// de forma que a mesma chave sempre produza a mesma resposta (testes repetíveis).
func simulatePayment(dfeKey string) dfeResponse {
	dfeType := "DESCONHECIDO"
	supplierCNPJ := ""
	if len(dfeKey) == 44 {
		modelo := dfeKey[20:22]
		switch modelo {
		case "55":
			dfeType = "NFE"
		case "57":
			dfeType = "CTE"
		}
		supplierCNPJ = dfeKey[6:20]
	} else if len(dfeKey) == 50 {
		dfeType = "NFSE"
	}

	scenario := checksumScenario(dfeKey)
	invoiceAmount := 1000 + float64(digitSum(dfeKey)%50)*137.50
	invoiceDate := "2026-05-12"
	companyCode := "1000"
	if supplierCNPJ == "" {
		supplierCNPJ = fmt.Sprintf("%014d", digitSum(dfeKey)*987654321%1e13)
	}

	base := dfeResponse{
		DFeKey:            dfeKey,
		DFeType:           dfeType,
		CompanyCode:       companyCode,
		SupplierCNPJ:      supplierCNPJ,
		SupplierID:        fmt.Sprintf("%010d", digitSum(dfeKey)*31),
		InvoiceDocument:   fmt.Sprintf("51%08d", digitSum(dfeKey)*7919%1e8),
		InvoiceFiscalYear: "2026",
		InvoiceDate:       invoiceDate,
		InvoiceAmount:     round2(invoiceAmount),
		Currency:          "BRL",
	}

	switch scenario {
	case 0: // PAGO_TOTAL — match direto, um único pagamento cobrindo o valor total
		base.MatchType = "CHAVE"
		base.PaymentStatus = "PAGO_TOTAL"
		base.TotalPaidAmount = base.InvoiceAmount
		base.Payments = []paymentItem{{
			ClearingDocument:   fmt.Sprintf("20%08d", digitSum(dfeKey)*13%1e8),
			ClearingDate:       "2026-06-10",
			PaymentPostingDate: "2026-06-10",
			PaidAmount:         base.InvoiceAmount,
			PaymentMethod:      "T",
			PaymentMethodDesc:  "Transferência bancária",
			SettlementMode:     "OUTROS_MEIOS_PAGAMENTO",
			IsPartial:          false,
		}}
	case 1: // PAGO_PARCIAL — match direto, duas parcelas (pagamento por conta)
		base.MatchType = "CHAVE"
		base.PaymentStatus = "PAGO_PARCIAL"
		parcela1 := round2(base.InvoiceAmount * 0.4)
		parcela2 := round2(base.InvoiceAmount * 0.3)
		base.TotalPaidAmount = round2(parcela1 + parcela2)
		base.Payments = []paymentItem{
			{
				ClearingDocument:   fmt.Sprintf("20%08d", digitSum(dfeKey)*17%1e8),
				ClearingDate:       "2026-06-05",
				PaymentPostingDate: "2026-06-05",
				PaidAmount:         parcela1,
				PaymentMethod:      "T",
				PaymentMethodDesc:  "Transferência bancária",
				SettlementMode:     "OUTROS_MEIOS_PAGAMENTO",
				IsPartial:          true,
			},
			{
				ClearingDocument:   fmt.Sprintf("20%08d", digitSum(dfeKey)*19%1e8),
				ClearingDate:       "2026-06-20",
				PaymentPostingDate: "2026-06-20",
				PaidAmount:         parcela2,
				PaymentMethod:      "T",
				PaymentMethodDesc:  "Transferência bancária",
				SettlementMode:     "OUTROS_MEIOS_PAGAMENTO",
				IsPartial:          true,
			},
		}
	case 2: // EM_ABERTO — fatura localizada, sem nenhuma liquidação
		base.MatchType = "CHAVE"
		base.PaymentStatus = "EM_ABERTO"
		base.TotalPaidAmount = 0
		base.Payments = []paymentItem{}
	case 3: // NAO_LOCALIZADO — não foi possível resolver a chave
		base.MatchType = "NAO_LOCALIZADO"
		base.PaymentStatus = "NAO_LOCALIZADO"
		base.TotalPaidAmount = 0
		base.Payments = []paymentItem{}
	case 4: // FALLBACK ambíguo — múltiplos candidatos (ex.: NFS-e/lançamento direto)
		base.MatchType = "FALLBACK"
		base.PaymentStatus = "PAGO_TOTAL"
		base.TotalPaidAmount = base.InvoiceAmount
		base.FallbackNote = "Múltiplos candidatos encontrados por CNPJ+número de documento na janela de ±90 dias — retornado o de valor mais próximo (simulado)"
		base.Payments = []paymentItem{{
			ClearingDocument:   fmt.Sprintf("20%08d", digitSum(dfeKey)*23%1e8),
			ClearingDate:       "2026-06-15",
			PaymentPostingDate: "2026-06-15",
			PaidAmount:         base.InvoiceAmount,
			PaymentMethod:      "B",
			PaymentMethodDesc:  "Boleto",
			SettlementMode:     "OUTROS_MEIOS_PAGAMENTO",
			IsPartial:          false,
		}}
	}

	return base
}

func checksumScenario(dfeKey string) int {
	return digitSum(dfeKey) % 5
}

func digitSum(s string) int {
	sum := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			sum += int(c - '0')
		} else {
			sum += int(c)
		}
	}
	return sum
}

func isValidKeyLength(key string) bool {
	return len(key) == 44 || len(key) == 50
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// ─── Helpers de request/response ──────────────────────────────────────────

// extractDFeKeyParam faz o parsing manual da sintaxe OData PaymentByDFe(dfeKey='{chave}')
// embutida no path — não é um query param nem um path param convencional.
func extractDFeKeyParam(pathRemainder string) string {
	idx := strings.Index(pathRemainder, "dfeKey='")
	if idx == -1 {
		return ""
	}
	rest := pathRemainder[idx+len("dfeKey='"):]
	end := strings.Index(rest, "'")
	if end == -1 {
		return rest
	}
	return rest[:end]
}

func requireBearerToken(w http.ResponseWriter, r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") == "" {
		w.Header().Set("Content-Type", "application/json")
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "token Bearer ausente ou inválido")
		return false
	}
	return true
}

func checkSimulatedRateLimit() (limited bool, retryAfterSeconds int) {
	if !rateLimitSimulationEnabled() {
		return false, 0
	}
	n := atomic.AddInt64(&requestCounter, 1)
	if n%10 == 0 {
		return true, 5
	}
	return false, 0
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiError{
		Code:          code,
		Message:       message,
		CorrelationID: randomHex(8),
	})
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
