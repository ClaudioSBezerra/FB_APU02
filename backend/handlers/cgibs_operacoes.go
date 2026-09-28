package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CGIBSOperacao reflete as colunas de cgibs_operacoes (migration 131) — a conta corrente
// fiscal por documento fiscal (ANEXO I do MOC, nível 2). É a "apuração de verdade" do CGIBS
// agora: sempre lida ao vivo via SQL sobre o ledger, nunca cacheada (mesmo princípio já fixado
// para o módulo RFB no fix 2ada5df).
type CGIBSOperacao struct {
	ID                string     `json:"id"`
	OperacaoIDExterno *int64     `json:"operacao_id_externo"`
	ChaveAcesso       string     `json:"chave_acesso"`
	DthEmissao        *time.Time `json:"dth_emissao"`
	DthAutorizacao    *time.Time `json:"dth_autorizacao"`
	CNPJFornecedor    string     `json:"cnpj_fornecedor"`
	CNPJAdquirente    string     `json:"cnpj_adquirente"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// CGIBSLancamento reflete as colunas de cgibs_lancamentos (migration 131) — o extrato_cc
// incremental (ANEXO I, nível 3-4): cada lançamento carrega os 7 valores simultâneos do modelo
// de conta corrente fiscal (recurso financeiro disponível/a transferir, crédito a
// apropriar/não utilizado/utilizado, débito em aberto/extinto).
type CGIBSLancamento struct {
	ID                           string    `json:"id"`
	LancamentoIDExterno          int64     `json:"lancamento_id_externo"`
	DthLancto                    time.Time `json:"dth_lancto"`
	MovCodigo                    *int      `json:"mov_codigo"`
	MovDescricao                 string    `json:"mov_descricao"`
	RecursoFinanceiroDisponivel  float64   `json:"recurso_financeiro_disponivel"`
	RecursoFinanceiroATransferir float64   `json:"recurso_financeiro_a_transferir"`
	CreditoAApropriar            float64   `json:"credito_a_apropriar"`
	CreditoNaoUtilizado          float64   `json:"credito_nao_utilizado"`
	CreditoUtilizado             float64   `json:"credito_utilizado"`
	DebitoEmAberto               float64   `json:"debito_em_aberto"`
	DebitoExtinto                float64   `json:"debito_extinto"`
	CreatedAt                    time.Time `json:"created_at"`
}

// ListarOperacoesCGIBSHandler — GET /api/cgibs/operacoes
// Lista o ledger de conta corrente fiscal (cgibs_operacoes) da empresa efetiva, paginado por
// page/page_size (mesmo padrão de ListarDebitosHandler, rfb_debitos_lista.go), ordenado por
// updated_at DESC (operação com movimento mais recente primeiro).
func ListarOperacoesCGIBSHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[CGIBSOperacoes]")
			return
		}

		qp := r.URL.Query()
		page := 1
		pageSize := 100
		if p, e := strconv.Atoi(qp.Get("page")); e == nil && p > 0 {
			page = p
		}
		if ps, e := strconv.Atoi(qp.Get("page_size")); e == nil && ps > 0 && ps <= 500 {
			pageSize = ps
		}

		var total int
		if err := db.QueryRow(`SELECT COUNT(*) FROM cgibs_operacoes WHERE company_id=$1`, companyID).Scan(&total); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao contar operações CGIBS", err, "[CGIBSOperacoes]")
			return
		}
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		offset := (page - 1) * pageSize

		rows, err := db.Query(`
			SELECT id, operacao_id_externo, chave_acesso, dth_emissao, dth_autorizacao,
			       COALESCE(cnpj_fornecedor,''), COALESCE(cnpj_adquirente,''),
			       created_at, updated_at
			FROM cgibs_operacoes
			WHERE company_id = $1
			ORDER BY updated_at DESC
			LIMIT $2 OFFSET $3
		`, companyID, pageSize, offset)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar operações CGIBS", err, "[CGIBSOperacoes]")
			return
		}
		defer rows.Close()

		var operacoes []CGIBSOperacao
		for rows.Next() {
			var o CGIBSOperacao
			var idExterno sql.NullInt64
			var dthEmissao, dthAutorizacao sql.NullTime
			if err := rows.Scan(
				&o.ID, &idExterno, &o.ChaveAcesso, &dthEmissao, &dthAutorizacao,
				&o.CNPJFornecedor, &o.CNPJAdquirente,
				&o.CreatedAt, &o.UpdatedAt,
			); err != nil {
				log.Printf("[CGIBSOperacoes] Erro ao ler linha: %v", err)
				continue
			}
			if idExterno.Valid {
				o.OperacaoIDExterno = &idExterno.Int64
			}
			if dthEmissao.Valid {
				o.DthEmissao = &dthEmissao.Time
			}
			if dthAutorizacao.Valid {
				o.DthAutorizacao = &dthAutorizacao.Time
			}
			operacoes = append(operacoes, o)
		}
		if operacoes == nil {
			operacoes = []CGIBSOperacao{}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"operacoes": operacoes,
			"pagination": map[string]int{
				"page":        page,
				"page_size":   pageSize,
				"total":       total,
				"total_pages": totalPages,
			},
		})
	}
}

// LancamentosOperacaoCGIBSHandler — GET /api/cgibs/operacoes/{id}/lancamentos
// Extrato completo (todos os lançamentos) de uma operação. O escopo de tenant é confirmado
// pelo próprio SQL (AND l.company_id=$2, direto na coluna indexada da própria
// cgibs_lancamentos — idx_cgibs_lancamentos_company, migration 131; company_id é sempre gravado
// igual ao da cgibs_operacoes dona, ver insertCGIBSLancamento/cgibs_parser.go) — nunca confia
// apenas no {id} vindo da URL. Uma operação inexistente OU de outra empresa devolve a mesma
// lista vazia (200), de propósito: distinguir os dois casos (404 vs vazio) vazaria pra um
// usuário se um dado ID de operação existe em outro tenant.
func LancamentosOperacaoCGIBSHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[CGIBSLancamentos]")
			return
		}

		rest := strings.TrimPrefix(r.URL.Path, "/api/cgibs/operacoes/")
		operacaoID := strings.TrimSuffix(rest, "/lancamentos")
		if operacaoID == "" || operacaoID == rest || strings.Contains(operacaoID, "/") {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		// Valida o formato do {id} ANTES de usá-lo na query (revisão adversarial, item 5): sem
		// isso, um id não-UUID vira erro cru do Postgres ("invalid input syntax for uuid") e um
		// 500 em vez de um 400 claro. reUUID já existe em pagamentos_fornecedores.go (mesmo
		// pacote handlers) — reaproveitado aqui.
		if !reUUID.MatchString(operacaoID) {
			jsonErr(w, http.StatusBadRequest, "ID de operação inválido")
			return
		}

		// Filtra direto por cgibs_lancamentos.company_id (coluna existe, com índice
		// idx_cgibs_lancamentos_company — migration 131) em vez de só pela subquery via
		// cgibs_operacoes (revisão adversarial, item 6): mesma proteção de tenant, mais simples e
		// mais barato — evita o IN (SELECT ...) para cada linha.
		rows, err := db.Query(`
			SELECT l.id, l.lancamento_id_externo, l.dth_lancto, l.mov_codigo, COALESCE(l.mov_descricao,''),
			       l.recurso_financeiro_disponivel, l.recurso_financeiro_a_transferir,
			       l.credito_a_apropriar, l.credito_nao_utilizado, l.credito_utilizado,
			       l.debito_em_aberto, l.debito_extinto, l.created_at
			FROM cgibs_lancamentos l
			WHERE l.operacao_id = $1 AND l.company_id = $2
			ORDER BY l.dth_lancto DESC
		`, operacaoID, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar lançamentos CGIBS", err, "[CGIBSLancamentos]")
			return
		}
		defer rows.Close()

		var lancamentos []CGIBSLancamento
		for rows.Next() {
			var l CGIBSLancamento
			var movCodigo sql.NullInt64
			if err := rows.Scan(
				&l.ID, &l.LancamentoIDExterno, &l.DthLancto, &movCodigo, &l.MovDescricao,
				&l.RecursoFinanceiroDisponivel, &l.RecursoFinanceiroATransferir,
				&l.CreditoAApropriar, &l.CreditoNaoUtilizado, &l.CreditoUtilizado,
				&l.DebitoEmAberto, &l.DebitoExtinto, &l.CreatedAt,
			); err != nil {
				log.Printf("[CGIBSLancamentos] Erro ao ler linha: %v", err)
				continue
			}
			if movCodigo.Valid {
				v := int(movCodigo.Int64)
				l.MovCodigo = &v
			}
			lancamentos = append(lancamentos, l)
		}
		if lancamentos == nil {
			lancamentos = []CGIBSLancamento{}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"lancamentos": lancamentos})
	}
}
