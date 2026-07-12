package services

import (
	"database/sql"
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

// maxBatchSize é o limite de chaves por requisição SearchPayments (AC #1).
// O mock (sap-mock-server) e o AC desta story já resolveram a 500 a inconsistência
// 500 vs 5000 mencionada no addendum do PRD (Open Question #1) — não há decisão
// pendente aqui.
const maxBatchSize = 500

// maxRetries é o número de tentativas para falhas transitórias (429/500) antes
// de marcar o lote como falho (AC #7).
const maxRetries = 3

// maxSearchPaymentsResponseBytes limita a leitura do corpo de resposta do
// SearchPayments — maior que o limite de /token (sap_payments.go) porque um
// lote de até 500 chaves com payments[] aninhado gera um payload bem maior.
const maxSearchPaymentsResponseBytes = 10 << 20 // 10 MiB

// backoffSleep é substituível em testes para evitar esperas reais de segundos
// (retry/backoff transitório, AC #3/#7).
var backoffSleep = time.Sleep

// pacerSleepFunc é usado exclusivamente pelo pacing de saída (AC #2) — mantido
// separado de backoffSleep para que os testes possam desativar apenas o
// backoff de retry sem neutralizar o teste dedicado do pacer.
var pacerSleepFunc = time.Sleep

// paymentItem espelha um item de payments[] no contrato SearchPayments (ver
// addendum do PRD, seção A, e sap-mock-server/main.go).
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

// dfeResponse espelha um item do array "value" retornado por SearchPayments.
// Nesta story (2.2) o resultado é apenas parseado/contado — persistir em
// pagamentos_fornecedores é escopo da Story 2.3.
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

type searchPaymentsResponse struct {
	Value []dfeResponse `json:"value"`
}

// isValidDFeKey replica a validação de tamanho de chave NF-e/CT-e usada pelo
// sap-mock-server (44 ou 50 caracteres) — filtrar aqui evita depender do SAP
// para rejeitar uma chave malformada (AC #5).
func isValidDFeKey(key string) bool {
	return len(key) == 44 || len(key) == 50
}

// filterValidKeys separa chaves válidas de malformadas sem invalidar o restante
// do lote (AC #5).
func filterValidKeys(chaves []string) (valid []string, invalidCount int) {
	for _, k := range chaves {
		if isValidDFeKey(k) {
			valid = append(valid, k)
		} else {
			invalidCount++
		}
	}
	return valid, invalidCount
}

// splitIntoBatches divide chaves em grupos de no máximo size elementos (AC #1).
func splitIntoBatches(chaves []string, size int) [][]string {
	if size <= 0 {
		size = maxBatchSize
	}
	var batches [][]string
	for i := 0; i < len(chaves); i += size {
		end := i + size
		if end > len(chaves) {
			end = len(chaves)
		}
		batches = append(batches, chaves[i:end])
	}
	return batches
}

// ─── Rate limiting (pacing) das chamadas de saída ao SAP (AC #2) ───────────

// callPacer garante um intervalo mínimo entre chamadas sucessivas, pareando
// (não apenas reagindo a 429) a taxa de requisições enviadas ao SAP. Nenhum
// limiter existente no projeto (rfbRateLimitCache, rateLimiter de
// middleware.go) serve para esse propósito — ver Dev Notes da story 2.2.
type callPacer struct {
	mu       sync.Mutex
	interval time.Duration
	lastCall time.Time
}

func newCallPacer(ratePerMinute int) *callPacer {
	if ratePerMinute <= 0 {
		ratePerMinute = 60
	}
	return &callPacer{interval: time.Minute / time.Duration(ratePerMinute)}
}

func (p *callPacer) Wait() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if !p.lastCall.IsZero() {
		if elapsed := now.Sub(p.lastCall); elapsed < p.interval {
			pacerSleepFunc(p.interval - elapsed)
		}
	}
	p.lastCall = time.Now()
}

// sapRatePerMinute lê SAP_SYNC_RATE_LIMIT_PER_MIN (default 60, sugestão do AC #2).
func sapRatePerMinute() int {
	if v := os.Getenv("SAP_SYNC_RATE_LIMIT_PER_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 60
}

// pacerRegistry mantém um callPacer por credencial (chave: clientID) — o rate
// limit do SAP é por consumidor/credencial (addendum do PRD, "60 req/min por
// consumidor"), não por empresa: duas empresas cadastradas com o mesmo
// client_id (ex.: mesmo tenant SAP compartilhado) devem dividir o mesmo
// orçamento, não receber orçamentos somados.
var pacerRegistry = struct {
	mu    sync.Mutex
	byKey map[string]*callPacer
}{byKey: make(map[string]*callPacer)}

func pacerForClient(clientID string) *callPacer {
	pacerRegistry.mu.Lock()
	defer pacerRegistry.mu.Unlock()
	if p, ok := pacerRegistry.byKey[clientID]; ok {
		return p
	}
	p := newCallPacer(sapRatePerMinute())
	pacerRegistry.byKey[clientID] = p
	return p
}

// ─── Chamada HTTP a SearchPayments ──────────────────────────────────────────

// validateBaseURL rejeita rapidamente uma base_url estruturalmente inválida
// (sem scheme/host) antes de gastar chamadas HTTP e tentativas de retry com
// algo que nunca vai funcionar — erro de configuração, não falha transitória.
func validateBaseURL(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("URL sem scheme/host válido: %q", baseURL)
	}
	return nil
}

// searchPaymentsURL monta a URL do endpoint OData de consulta em lote.
func searchPaymentsURL(baseURL string) (string, error) {
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("base URL inválida: %w", err)
	}
	return parsedBase.JoinPath(
		"sap", "opu", "odata4", "sap", "zapi_dfe_payment",
		"srvd", "sap", "dfe_payment", "0001", "SearchPayments",
	).String(), nil
}

// maxRetryAfterSecs limita o quanto o backoff obedece ao header Retry-After —
// um valor mal configurado ou anômalo no header não pode travar o processamento
// do lote por tempo arbitrário.
const maxRetryAfterSecs = 60

// searchPaymentsResult é o retorno de uma única chamada HTTP ao SearchPayments.
type searchPaymentsResult struct {
	statusCode     int
	retryAfterSecs int
	response       *searchPaymentsResponse
	errorBody      []byte // corpo bruto quando statusCode != 200, para log/diagnóstico
}

// callSearchPayments faz uma única chamada POST SearchPayments — sem retry,
// sem paginação de lote (isso é responsabilidade do chamador, processBatch).
func callSearchPayments(token, baseURL string, dfeKeys []string) (searchPaymentsResult, error) {
	endpoint, err := searchPaymentsURL(baseURL)
	if err != nil {
		return searchPaymentsResult{}, err
	}

	body, err := json.Marshal(struct {
		DFeKeys []string `json:"dfeKeys"`
	}{DFeKeys: dfeKeys})
	if err != nil {
		return searchPaymentsResult{}, fmt.Errorf("falha ao serializar corpo da requisição: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return searchPaymentsResult{}, fmt.Errorf("falha ao criar requisição SearchPayments: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return searchPaymentsResult{}, fmt.Errorf("requisição SearchPayments falhou: %w", err)
	}
	defer resp.Body.Close()

	retryAfter := 0
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if n, convErr := strconv.Atoi(ra); convErr == nil {
			retryAfter = n
			if retryAfter > maxRetryAfterSecs {
				retryAfter = maxRetryAfterSecs
			}
		}
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxSearchPaymentsResponseBytes))

	if resp.StatusCode != http.StatusOK {
		return searchPaymentsResult{statusCode: resp.StatusCode, retryAfterSecs: retryAfter, errorBody: respBody}, nil
	}

	var parsed searchPaymentsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return searchPaymentsResult{}, fmt.Errorf("falha ao parsear resposta SearchPayments (corpo: %s): %w", truncateForLog(respBody), err)
	}

	return searchPaymentsResult{statusCode: resp.StatusCode, response: &parsed}, nil
}

// ─── Retry/backoff + tratamento de erros por lote ──────────────────────────

// batchOutcome é o resultado do processamento de um lote (possivelmente após
// divisão recursiva em sub-lotes menores, ver o caso 400 abaixo).
type batchOutcome struct {
	success     bool
	credFail    bool // 401/403 — sem retry (AC #6)
	syncedCount int
	items       []dfeResponse       // itens efetivamente retornados com 200 — persistência é da Story 2.3
	counts      paymentStatusCounts // contagens por paymentStatus — persistência é da Story 2.4
}

// processBatch envia um lote ao SAP com pacing, retry/backoff exponencial
// (429/500, AC #3/#7) e redução automática de lote em caso de 400 (AC #4).
// Uma falha de credencial (401/403) interrompe imediatamente, sem retry (AC #6).
func processBatch(token, baseURL string, batch []string, pacer *callPacer) batchOutcome {
	attempts := 0
	for {
		pacer.Wait()
		result, err := callSearchPayments(token, baseURL, batch)
		if err != nil {
			// Erro de rede/transporte — tratado como falha transitória.
			attempts++
			if attempts >= maxRetries {
				return batchOutcome{success: false}
			}
			backoffSleep(exponentialBackoff(attempts))
			continue
		}

		switch {
		case result.statusCode == http.StatusOK:
			counts := logBatchSummary(result.response, len(batch))
			var items []dfeResponse
			if result.response != nil {
				items = result.response.Value
			}
			return batchOutcome{success: true, syncedCount: len(batch), items: items, counts: counts}

		case result.statusCode == http.StatusUnauthorized || result.statusCode == http.StatusForbidden:
			log.Printf("[SAP Sync] SearchPayments retornou HTTP %d (sem retry): %s", result.statusCode, truncateForLog(result.errorBody))
			return batchOutcome{credFail: true}

		case result.statusCode == http.StatusBadRequest:
			// Lote grande demais para o SAP mesmo dentro do limite de 500 desta
			// story — reduz automaticamente e tenta novamente (AC #4).
			log.Printf("[SAP Sync] SearchPayments retornou HTTP 400 para lote de %d chaves, reduzindo: %s", len(batch), truncateForLog(result.errorBody))
			if len(batch) <= 1 {
				return batchOutcome{success: false}
			}
			mid := len(batch) / 2
			left := processBatch(token, baseURL, batch[:mid], pacer)
			if left.credFail {
				// Não faz sentido gastar mais uma chamada com um token já
				// sabidamente rejeitado pelo SAP (AC #6).
				return batchOutcome{credFail: true}
			}
			right := processBatch(token, baseURL, batch[mid:], pacer)
			if right.credFail {
				return batchOutcome{credFail: true}
			}
			return batchOutcome{
				success:     left.success && right.success,
				syncedCount: left.syncedCount + right.syncedCount,
				items:       append(left.items, right.items...),
				counts:      left.counts.add(right.counts),
			}

		case result.statusCode == http.StatusTooManyRequests || result.statusCode == http.StatusInternalServerError:
			log.Printf("[SAP Sync] SearchPayments retornou HTTP %d (tentativa %d/%d): %s", result.statusCode, attempts+1, maxRetries, truncateForLog(result.errorBody))
			attempts++
			if attempts >= maxRetries {
				return batchOutcome{success: false}
			}
			if result.retryAfterSecs > 0 {
				backoffSleep(time.Duration(result.retryAfterSecs) * time.Second)
			} else {
				backoffSleep(exponentialBackoff(attempts))
			}
			continue

		default:
			log.Printf("[SAP Sync] SearchPayments retornou HTTP %d inesperado (tentativa %d/%d): %s", result.statusCode, attempts+1, maxRetries, truncateForLog(result.errorBody))
			attempts++
			if attempts >= maxRetries {
				return batchOutcome{success: false}
			}
			backoffSleep(exponentialBackoff(attempts))
			continue
		}
	}
}

// paymentStatusCounts acumula quantas chaves retornaram cada paymentStatus ao
// longo de uma sincronização — persistido em sap_sync_runs (Story 2.4).
type paymentStatusCounts struct {
	pagoTotal     int
	pagoParcial   int
	emAberto      int
	naoLocalizado int
}

func (c paymentStatusCounts) add(other paymentStatusCounts) paymentStatusCounts {
	return paymentStatusCounts{
		pagoTotal:     c.pagoTotal + other.pagoTotal,
		pagoParcial:   c.pagoParcial + other.pagoParcial,
		emAberto:      c.emAberto + other.emAberto,
		naoLocalizado: c.naoLocalizado + other.naoLocalizado,
	}
}

// logBatchSummary loga uma contagem simples por paymentStatus e retorna essas
// mesmas contagens estruturadas para o chamador acumular ao longo do sync e
// persistir em sap_sync_runs (Story 2.4).
func logBatchSummary(resp *searchPaymentsResponse, batchSize int) paymentStatusCounts {
	if resp == nil {
		return paymentStatusCounts{}
	}
	var c paymentStatusCounts
	for _, item := range resp.Value {
		switch item.PaymentStatus {
		case "PAGO_TOTAL":
			c.pagoTotal++
		case "PAGO_PARCIAL":
			c.pagoParcial++
		case "EM_ABERTO":
			c.emAberto++
		case "NAO_LOCALIZADO":
			c.naoLocalizado++
		}
	}
	log.Printf("[SAP Sync] Lote de %d chaves respondido: %d itens (PAGO_TOTAL=%d, PAGO_PARCIAL=%d, EM_ABERTO=%d, NAO_LOCALIZADO=%d)",
		batchSize, len(resp.Value), c.pagoTotal, c.pagoParcial, c.emAberto, c.naoLocalizado)
	return c
}

// exponentialBackoff retorna 1s, 2s, 4s... para as tentativas 1, 2, 3...
// Com maxRetries = 3, apenas os backoffs de 1s e 2s chegam a ser usados de
// fato: a 3ª falha já atinge o limite de tentativas e retorna sem dormir.
func exponentialBackoff(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

// ─── Persistência (Story 2.3) ───────────────────────────────────────────────

// persistPayments grava cada item de payments[] retornado pelo SAP como uma
// linha em pagamentos_fornecedores (origem='sap_api'), com upsert idempotente
// via o índice único parcial uq_pag_forn_sap_api (company_id, chave_doc,
// num_doc_pagamento, bukrs) — ver migration 118. dfeResponse sem payments[]
// (EM_ABERTO/NAO_LOCALIZADO) não gera nenhuma linha, propositalmente.
//
// company_id e bukrs usados na gravação são SEMPRE os parâmetros já resolvidos
// pelo contexto da sincronização — nunca um valor vindo da resposta do SAP
// (dfeResponse.CompanyCode é ignorado de propósito), garantindo o isolamento
// multi-tenant (AC #4). Falhas de gravação são isoladas por item: uma linha
// que falhe não impede as demais, e nunca interrompe a sincronização como um
// todo (mesmo princípio de isolamento já usado em toda a Epic 2).
//
// Retorna o número de pagamentos que NÃO puderam ser persistidos/ignorados —
// o chamador (ProcessSAPSync) usa isso para refletir falhas de gravação no
// status final da sincronização, que antes só considerava a chamada ao SAP.
func persistPayments(db *sql.DB, companyID, bukrs string, items []dfeResponse) (failedCount int) {
	for _, item := range items {
		if len(item.Payments) == 0 {
			continue
		}
		fallbackAmbiguous := item.MatchType == "FALLBACK" && item.FallbackNote != ""
		for _, p := range item.Payments {
			if p.ClearingDocument == "" {
				log.Printf("[SAP Sync] clearingDocument vazio para chave=%s — pagamento ignorado (evita colisão silenciosa no índice de dedup)", item.DFeKey)
				failedCount++
				continue
			}
			if p.PaidAmount <= 0 {
				log.Printf("[SAP Sync] paidAmount <= 0 (%.2f) para chave=%s num_doc_pagamento=%s — pagamento ignorado", p.PaidAmount, item.DFeKey, p.ClearingDocument)
				failedCount++
				continue
			}
			dataPagamento, err := time.Parse("2006-01-02", p.ClearingDate)
			if err != nil {
				log.Printf("[SAP Sync] Erro ao parsear clearingDate %q para chave=%s: %v — pagamento ignorado", p.ClearingDate, item.DFeKey, err)
				failedCount++
				continue
			}
			mesAno := dataPagamento.Format("2006-01")

			_, err = db.Exec(`
				INSERT INTO pagamentos_fornecedores
					(company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento,
					 num_doc_pagamento, mes_ano, origem, payment_status, match_type, bukrs,
					 settlement_mode, fallback_ambiguous)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'sap_api', $9, $10, $11, $12, $13)
				ON CONFLICT (company_id, chave_doc, num_doc_pagamento, bukrs) WHERE origem = 'sap_api'
				DO UPDATE SET
					tipo_doc           = EXCLUDED.tipo_doc,
					forn_cnpj          = EXCLUDED.forn_cnpj,
					data_pagamento     = EXCLUDED.data_pagamento,
					valor_pagamento    = EXCLUDED.valor_pagamento,
					mes_ano            = EXCLUDED.mes_ano,
					payment_status     = EXCLUDED.payment_status,
					match_type         = EXCLUDED.match_type,
					settlement_mode    = EXCLUDED.settlement_mode,
					fallback_ambiguous = EXCLUDED.fallback_ambiguous
			`, companyID, item.DFeKey, item.DFeType, item.SupplierCNPJ, dataPagamento, p.PaidAmount,
				p.ClearingDocument, mesAno, item.PaymentStatus, item.MatchType, bukrs,
				p.SettlementMode, fallbackAmbiguous)
			if err != nil {
				failedCount++
				log.Printf("[SAP Sync] Erro ao persistir pagamento chave=%s num_doc_pagamento=%s company_id=%s bukrs=%s: %v",
					item.DFeKey, p.ClearingDocument, companyID, bukrs, err)
			}
		}
	}
	return
}

// ─── Orquestração de alto nível ─────────────────────────────────────────────

// SyncResult é o resultado final de uma sincronização SAP para um BUKRS —
// consumido por syncBukrs (sap_sync_trigger.go) para decidir o status a
// gravar em sap_sync_runs.
type SyncResult struct {
	Status string // concluido | falha | falha_credencial
	Detail string

	// Contagens por paymentStatus acumuladas ao longo de todos os lotes desta
	// sincronização — persistidas em sap_sync_runs (Story 2.4).
	ChavesPagoTotal     int
	ChavesPagoParcial   int
	ChavesEmAberto      int
	ChavesNaoLocalizado int
}

// ProcessSAPSync consulta o SAP (SearchPayments) para o conjunto de chaves de
// um BUKRS, respeitando o limite de lote, o rate limit e a lógica de retry, e
// persiste cada pagamento retornado em pagamentos_fornecedores (Story 2.3).
func ProcessSAPSync(db *sql.DB, companyID, bukrs, clientID, clientSecret, baseURL string, chaves []string) SyncResult {
	if err := validateBaseURL(baseURL); err != nil {
		return SyncResult{Status: "falha", Detail: "base_url inválida, sincronização não tentada: " + err.Error()}
	}

	validKeys, invalidCount := filterValidKeys(chaves)
	if len(validKeys) == 0 {
		return SyncResult{Status: "concluido", Detail: fmt.Sprintf("nenhuma chave válida para sincronizar (%d ignoradas)", invalidCount)}
	}

	token, err := GetToken(baseURL, clientID, clientSecret)
	if err != nil {
		// Sem token não há como distinguir com certeza credencial inválida de
		// falha transitória de rede — tratada como falha de credencial (AC #6),
		// já que retentar sem corrigir a credencial não resolveria o problema
		// na grande maioria dos casos reais. Ver Dev Notes/Completion Notes.
		return SyncResult{Status: "falha_credencial", Detail: "falha ao obter token OAuth2: " + truncateForLog([]byte(err.Error()))}
	}

	pacer := pacerForClient(clientID)
	batches := splitIntoBatches(validKeys, maxBatchSize)

	syncedTotal := 0
	anyFailed := false
	persistFailures := 0
	var totalCounts paymentStatusCounts
	for _, batch := range batches {
		outcome := processBatch(token, baseURL, batch, pacer)
		totalCounts = totalCounts.add(outcome.counts)
		if len(outcome.items) > 0 {
			persistFailures += persistPayments(db, companyID, bukrs, outcome.items)
		}
		if outcome.credFail {
			return SyncResult{
				Status:          "falha_credencial",
				Detail:          fmt.Sprintf("credencial rejeitada pelo SAP (401/403) após sincronizar %d chaves", syncedTotal),
				ChavesPagoTotal: totalCounts.pagoTotal, ChavesPagoParcial: totalCounts.pagoParcial,
				ChavesEmAberto: totalCounts.emAberto, ChavesNaoLocalizado: totalCounts.naoLocalizado,
			}
		}
		syncedTotal += outcome.syncedCount
		if !outcome.success {
			anyFailed = true
		}
	}

	result := SyncResult{
		ChavesPagoTotal: totalCounts.pagoTotal, ChavesPagoParcial: totalCounts.pagoParcial,
		ChavesEmAberto: totalCounts.emAberto, ChavesNaoLocalizado: totalCounts.naoLocalizado,
	}
	if anyFailed {
		result.Status = "falha"
		result.Detail = fmt.Sprintf("%d/%d chaves sincronizadas, %d inválidas ignoradas — 1 ou mais lotes falharam após %d tentativas", syncedTotal, len(validKeys), invalidCount, maxRetries)
		return result
	}
	if persistFailures > 0 {
		// A chamada ao SAP foi bem-sucedida, mas nem todo pagamento retornado
		// pôde ser gravado — refletir isso no status evita reportar 'concluido'
		// quando, na prática, dados retornados pelo SAP não chegaram a persistir.
		result.Status = "falha"
		result.Detail = fmt.Sprintf("%d chaves sincronizadas, %d inválidas ignoradas — %d pagamento(s) retornado(s) pelo SAP não puderam ser persistidos", syncedTotal, invalidCount, persistFailures)
		return result
	}
	result.Status = "concluido"
	result.Detail = fmt.Sprintf("%d chaves sincronizadas, %d inválidas ignoradas", syncedTotal, invalidCount)
	return result
}
