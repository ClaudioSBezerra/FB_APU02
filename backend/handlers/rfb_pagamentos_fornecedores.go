package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"

	jwt "github.com/golang-jwt/jwt/v5"
)

// ─── Response types ───────────────────────────────────────────────────────────

type ConciliacaoSumario struct {
	TotalPago     float64 `json:"total_pago"`
	TotalNotas    int     `json:"total_notas"`
	CBSExtinto    float64 `json:"cbs_extinto"`
	NotasExtinto  int     `json:"notas_extinto"`
	CBSPendente   float64 `json:"cbs_pendente"`
	NotasPendente int     `json:"notas_pendente"`
	NotasSemDados int     `json:"notas_sem_dados"`
}

type ConciliacaoItem struct {
	ChaveDoc           string  `json:"chave_doc"`
	TipoDoc            string  `json:"tipo_doc"`
	FornCNPJ           string  `json:"forn_cnpj"`
	FornNome           *string `json:"forn_nome"`
	NumParcelas        int     `json:"num_parcelas"`
	TotalPago          float64 `json:"total_pago"`
	PrimeiraParcela    string  `json:"primeira_parcela"`
	UltimaParcela      string  `json:"ultima_parcela"`
	ValorNota          float64 `json:"valor_nota"`
	ValorCBSNota       float64 `json:"valor_cbs_nota"`
	ValorIBSNota       float64 `json:"valor_ibs_nota"`
	ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
	SituacaoCredito    *string `json:"situacao_credito"`
	StatusConciliacao  string  `json:"status_conciliacao"`
}

// ─── SQL base (CTE) ───────────────────────────────────────────────────────────

// cteBase é a expressão de tabela comum que agrega pagamentos_fornecedores
// e cruza com nfe_entradas, cte_entradas e rfb_creditos.
// Parâmetros posicionais:
//
//	$1 = company_id
//	$2 = mes_ano ('' para ignorar)
//	$3 = forn_cnpj dígitos ('' para ignorar)
const cteBase = `
WITH pagamentos_agg AS (
    SELECT
        pf.chave_doc,
        pf.tipo_doc,
        pf.forn_cnpj,
        MAX(pf.forn_nome)           AS forn_nome,
        COUNT(*)                    AS num_parcelas,
        SUM(pf.valor_pagamento)     AS total_pago,
        MIN(pf.data_pagamento)      AS primeira_parcela,
        MAX(pf.data_pagamento)      AS ultima_parcela
    FROM pagamentos_fornecedores pf
    WHERE pf.company_id = $1
      AND ($2 = '' OR pf.mes_ano = $2)
      AND ($3 = '' OR pf.forn_cnpj = $3)
    GROUP BY pf.chave_doc, pf.tipo_doc, pf.forn_cnpj
),
conciliacao AS (
    SELECT
        pa.chave_doc,
        pa.tipo_doc,
        pa.forn_cnpj,
        pa.forn_nome,
        pa.num_parcelas::INT                                AS num_parcelas,
        pa.total_pago,
        pa.primeira_parcela,
        pa.ultima_parcela,
        COALESCE(
            CASE WHEN pa.tipo_doc = 'NFE' THEN ne.v_nf    ELSE NULL END,
            CASE WHEN pa.tipo_doc = 'CTE' THEN ct.v_prest ELSE NULL END,
            0
        )::NUMERIC(15,2)                                    AS valor_nota,
        COALESCE(
            CASE WHEN pa.tipo_doc = 'NFE' THEN ne.v_cbs ELSE NULL END,
            CASE WHEN pa.tipo_doc = 'CTE' THEN ct.v_cbs ELSE NULL END,
            0
        )::NUMERIC(15,2)                                    AS valor_cbs_nota,
        COALESCE(
            CASE WHEN pa.tipo_doc = 'NFE' THEN ne.v_ibs ELSE NULL END,
            CASE WHEN pa.tipo_doc = 'CTE' THEN ct.v_ibs ELSE NULL END,
            0
        )::NUMERIC(15,2)                                    AS valor_ibs_nota,
        COALESCE(rc.valor_cbs_nao_extinto, 0)               AS valor_cbs_nao_extinto,
        rc.situacao_credito,
        CASE
            WHEN rc.id IS NULL                THEN 'sem_dados'
            WHEN rc.valor_cbs_nao_extinto > 0 THEN 'pendente'
            ELSE                                   'extinto'
        END                                                 AS status_conciliacao
    FROM pagamentos_agg pa
    LEFT JOIN nfe_entradas ne
        ON ne.company_id = $1
       AND pa.tipo_doc = 'NFE'
       AND ne.chave_nfe = pa.chave_doc
    LEFT JOIN cte_entradas ct
        ON ct.company_id = $1
       AND pa.tipo_doc = 'CTE'
       AND ct.chave_cte = pa.chave_doc
    LEFT JOIN rfb_creditos rc
        ON rc.company_id = $1
       AND rc.chave_dfe = pa.chave_doc
)
`

// nonDigit remove tudo que não é dígito de uma string.
var nonDigit = regexp.MustCompile(`[^0-9]`)

// RFBPagamentosFornecedoresHandler — GET /api/rfb/pagamentos-fornecedores
// Retorna sumário (4 cards) + lista paginada da conciliação pagamentos vs CBS RFB.
func RFBPagamentosFornecedoresHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBPgtosFornecedores]")
			return
		}

		// ── Parse query params ────────────────────────────────────────────────
		q := r.URL.Query()

		mesAno := q.Get("mes_ano")
		statusParam := q.Get("status")
		if statusParam == "" {
			statusParam = "all"
		}
		fornCNPJ := nonDigit.ReplaceAllString(q.Get("forn_cnpj"), "")

		page := 1
		if p, err2 := strconv.Atoi(q.Get("page")); err2 == nil && p > 1 {
			page = p
		}
		pageSize := 50
		if ps, err2 := strconv.Atoi(q.Get("page_size")); err2 == nil && ps >= 1 && ps <= 200 {
			pageSize = ps
		}
		offset := (page - 1) * pageSize

		// statusFilter: '' significa sem filtro (todos os status)
		statusFilter := ""
		if statusParam != "all" {
			statusFilter = statusParam
		}

		// ── Summary query — sem filtro de status para os cards mostrarem o mês completo ──
		const sumSQL = cteBase + `
SELECT
    COALESCE(SUM(total_pago), 0),
    COUNT(*),
    COALESCE(SUM(CASE WHEN status_conciliacao = 'extinto'  THEN valor_cbs_nao_extinto ELSE 0 END), 0),
    COUNT(*) FILTER (WHERE status_conciliacao = 'extinto'),
    COALESCE(SUM(CASE WHEN status_conciliacao = 'pendente' THEN valor_cbs_nao_extinto ELSE 0 END), 0),
    COUNT(*) FILTER (WHERE status_conciliacao = 'pendente'),
    COUNT(*) FILTER (WHERE status_conciliacao = 'sem_dados')
FROM conciliacao
`
		var sumario ConciliacaoSumario
		err = db.QueryRow(sumSQL, companyID, mesAno, fornCNPJ).Scan(
			&sumario.TotalPago,
			&sumario.TotalNotas,
			&sumario.CBSExtinto,
			&sumario.NotasExtinto,
			&sumario.CBSPendente,
			&sumario.NotasPendente,
			&sumario.NotasSemDados,
		)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao calcular sumário", err, "[RFBPgtosFornecedores]")
			return
		}

		// ── Count query — com filtro de status ───────────────────────────────
		const countSQL = cteBase + `
SELECT COUNT(*) FROM conciliacao
WHERE ($4 = '' OR status_conciliacao = $4)
`
		var total int
		err = db.QueryRow(countSQL, companyID, mesAno, fornCNPJ, statusFilter).Scan(&total)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao contar registros", err, "[RFBPgtosFornecedores]")
			return
		}

		// ── List query ────────────────────────────────────────────────────────
		const listSQL = cteBase + `
SELECT
    chave_doc,
    tipo_doc,
    forn_cnpj,
    forn_nome,
    num_parcelas,
    total_pago,
    primeira_parcela,
    ultima_parcela,
    valor_nota,
    valor_cbs_nota,
    valor_ibs_nota,
    valor_cbs_nao_extinto,
    situacao_credito,
    status_conciliacao
FROM conciliacao
WHERE ($4 = '' OR status_conciliacao = $4)
ORDER BY
    CASE status_conciliacao WHEN 'pendente' THEN 0 WHEN 'sem_dados' THEN 1 ELSE 2 END,
    total_pago DESC
LIMIT $5 OFFSET $6
`
		rows, err := db.Query(listSQL, companyID, mesAno, fornCNPJ, statusFilter, pageSize, offset)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao listar conciliação", err, "[RFBPgtosFornecedores]")
			return
		}
		defer rows.Close()

		items := []ConciliacaoItem{}
		for rows.Next() {
			var it ConciliacaoItem
			var fornNome sql.NullString
			var situacaoCredito sql.NullString
			var primParcela sql.NullTime
			var ultParcela sql.NullTime

			if err := rows.Scan(
				&it.ChaveDoc,
				&it.TipoDoc,
				&it.FornCNPJ,
				&fornNome,
				&it.NumParcelas,
				&it.TotalPago,
				&primParcela,
				&ultParcela,
				&it.ValorNota,
				&it.ValorCBSNota,
				&it.ValorIBSNota,
				&it.ValorCBSNaoExtinto,
				&situacaoCredito,
				&it.StatusConciliacao,
			); err != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao ler item de conciliação", err, "[RFBPgtosFornecedores]")
				return
			}
			if fornNome.Valid {
				it.FornNome = &fornNome.String
			}
			if situacaoCredito.Valid {
				it.SituacaoCredito = &situacaoCredito.String
			}
			if primParcela.Valid {
				it.PrimeiraParcela = primParcela.Time.Format("2006-01-02")
			}
			if ultParcela.Valid {
				it.UltimaParcela = ultParcela.Time.Format("2006-01-02")
			}
			items = append(items, it)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"sumario":   sumario,
			"items":     items,
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		})
	}
}
