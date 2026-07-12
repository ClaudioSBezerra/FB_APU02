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
	ChaveDoc            string  `json:"chave_doc"`
	TipoDoc             string  `json:"tipo_doc"`
	FornCNPJ            string  `json:"forn_cnpj"`
	FornNome            *string `json:"forn_nome"`
	NumParcelas         int     `json:"num_parcelas"`
	TotalPago           float64 `json:"total_pago"`
	PrimeiraParcela     string  `json:"primeira_parcela"`
	UltimaParcela       string  `json:"ultima_parcela"`
	ValorNota           float64 `json:"valor_nota"`
	ValorCBSNota        float64 `json:"valor_cbs_nota"`
	ValorIBSNota        float64 `json:"valor_ibs_nota"`
	ValorCBSNaoExtinto  float64 `json:"valor_cbs_nao_extinto"`
	SituacaoCredito     *string `json:"situacao_credito"`
	StatusConciliacao   string  `json:"status_conciliacao"`
	PossivelDuplicidade bool    `json:"possivel_duplicidade"`
	// Story 3.2: indicador de pagamento como evidência auxiliar — nenhum
	// destes campos jamais influencia StatusConciliacao (FR-4).
	PaymentStatus           *string `json:"payment_status"`
	MatchType               *string `json:"match_type"`
	FallbackAmbiguous       bool    `json:"fallback_ambiguous"`
	Origem                  string  `json:"origem"`
	PagamentoCobreValorNota bool    `json:"pagamento_cobre_valor_nota"`
	UltimoStatusBusca       *string `json:"ultimo_status_busca"`
	UltimaTentativaBusca    *string `json:"ultima_tentativa_busca"`
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
WITH pagamentos_dedup AS (
    -- Story 2.5 (AC #1/#2): quando a MESMA liquidação (chave_doc + num_doc_pagamento)
    -- tem uma linha 'csv' e uma linha 'sap_api', a linha 'csv' é excluída desta
    -- agregação (o registro sap_api prevalece). A linha csv NUNCA é removida da
    -- tabela — continua visível via PagamentosFornecedoresListHandler, para auditoria.
    -- CSV legado sem num_doc_pagamento (NULL) nunca é afetado por esta regra.
    SELECT pf.*
    FROM pagamentos_fornecedores pf
    WHERE pf.company_id = $1
      AND ($2 = '' OR pf.mes_ano = $2)
      AND ($3 = '' OR pf.forn_cnpj = $3)
      AND NOT (
          pf.origem = 'csv'
          AND pf.num_doc_pagamento IS NOT NULL
          AND EXISTS (
              -- O EXISTS aplica os MESMOS filtros de mes_ano/forn_cnpj ($2/$3) da
              -- CTE externa: sem isso, uma linha sap_api de outro mês/fornecedor
              -- suprimiria a csv sem aparecer ela mesma no resultado filtrado,
              -- fazendo a liquidação inteira sumir do relatório (bug encontrado
              -- e confirmado em revisão — ver Review Findings).
              -- tipo_doc/forn_cnpj também precisam bater: pagamentos_agg agrupa
              -- por (chave_doc, tipo_doc, forn_cnpj), então só suprimir a linha
              -- csv quando ela realmente cairia no mesmo grupo da sap_api evita
              -- excluir uma linha sem nenhuma outra absorver seu valor.
              SELECT 1 FROM pagamentos_fornecedores sap
              WHERE sap.company_id = pf.company_id
                AND sap.chave_doc = pf.chave_doc
                AND sap.num_doc_pagamento = pf.num_doc_pagamento
                AND sap.origem = 'sap_api'
                AND sap.tipo_doc = pf.tipo_doc
                AND sap.forn_cnpj = pf.forn_cnpj
                AND ($2 = '' OR sap.mes_ano = $2)
                AND ($3 = '' OR sap.forn_cnpj = $3)
          )
      )
),
pagamentos_agg AS (
    SELECT
        pf.chave_doc,
        pf.tipo_doc,
        pf.forn_cnpj,
        MAX(pf.forn_nome)           AS forn_nome,
        COUNT(*)                    AS num_parcelas,
        SUM(pf.valor_pagamento)     AS total_pago,
        MIN(pf.data_pagamento)      AS primeira_parcela,
        MAX(pf.data_pagamento)      AS ultima_parcela,
        -- Story 3.2: valor representativo do grupo para os campos que variam
        -- por parcela — usa o valor da parcela mais recente. Desempate estável
        -- por pf.id DESC (revisão): sem ele, 2 parcelas na MESMA data poderiam
        -- fazer cada ARRAY_AGG escolher uma linha diferente, gerando uma tripla
        -- incoerente (payment_status de uma parcela, origem de outra). Com o
        -- id no critério, os 3 sempre vêm da mesma linha (a mais recente).
        (ARRAY_AGG(pf.payment_status ORDER BY pf.data_pagamento DESC, pf.id DESC))[1] AS payment_status,
        (ARRAY_AGG(pf.match_type     ORDER BY pf.data_pagamento DESC, pf.id DESC))[1] AS match_type,
        BOOL_OR(COALESCE(pf.fallback_ambiguous, false))                              AS fallback_ambiguous,
        (ARRAY_AGG(pf.origem         ORDER BY pf.data_pagamento DESC, pf.id DESC))[1] AS origem
    FROM pagamentos_dedup pf
    GROUP BY pf.chave_doc, pf.tipo_doc, pf.forn_cnpj
),
creditos_tipados AS (
    -- Story 3.1: rfb_creditos.modelo_dfe não é 'NFE'/'CTE' — é o código de
    -- modelo fiscal ('55'=NFe, '57'=CTe). O fallback por SUBSTRING(chave_dfe)
    -- reaproveita o mesmo mecanismo de rfb_apuracao.go/rfb_debitos_lista.go
    -- (modelo_dfe vazio → deriva da chave), mas o mapeamento para os literais
    -- 'NFE'/'CTE' é lógica NOVA desta story (aqueles arquivos só repassam o
    -- código cru '55'/'57', nunca convertem para label). O fallback dispara
    -- não só quando modelo_dfe está vazio, mas para QUALQUER valor diferente
    -- de '55'/'57' (ex.: '65' de NFC-e, '67' de CT-e OS) — se nenhuma das
    -- checagens bater, tipo_doc_derivado fica NULL (tratado via COALESCE mais
    -- abaixo, nunca propagado cru para o Go).
    SELECT
        rc.*,
        CASE
            WHEN COALESCE(rc.modelo_dfe, '') = '55' THEN 'NFE'
            WHEN COALESCE(rc.modelo_dfe, '') = '57' THEN 'CTE'
            WHEN length(rc.chave_dfe) = 44 AND SUBSTRING(rc.chave_dfe, 21, 2) = '55' THEN 'NFE'
            WHEN length(rc.chave_dfe) = 44 AND SUBSTRING(rc.chave_dfe, 21, 2) = '57' THEN 'CTE'
            ELSE NULL
        END AS tipo_doc_derivado
    FROM rfb_creditos rc
    WHERE rc.company_id = $1
      -- ni_emitente é o equivalente de forn_cnpj para créditos (confirmado em
      -- rfb_creditos.go) — filtro de fornecedor também vale para créditos sem
      -- pagamento, para não escondê-los de uma busca por fornecedor.
      AND ($3 = '' OR rc.ni_emitente = $3)
      -- Nenhum filtro de mes_ano aqui, deliberadamente (ver Dev Notes/Task 4
      -- da Story 3.1): mes_ano é propriedade do PAGAMENTO, não do crédito —
      -- filtrar créditos por mês esconderia todo crédito aguardando_pagamento
      -- sob qualquer filtro de mês, contrariando o propósito da FR-11.
),
conciliacao_creditos AS (
    -- Story 3.1 (AC #1/#2): base invertida — TODO crédito RFB aparece, com ou
    -- sem pagamento localizado (antes, a base era pagamentos_fornecedores, e
    -- um crédito sem nenhum pagamento nunca era visto por esta query).
    SELECT
        rc.chave_dfe                                        AS chave_doc,
        -- COALESCE necessário: tipo_doc_derivado é NULL para modelo_dfe fora
        -- de '55'/'57' (ex.: NFC-e '65', CT-e OS '67') ou chave_dfe malformada
        -- — ConciliacaoItem.TipoDoc é string não-nullable no Go, então um NULL
        -- aqui quebrava o Scan (500 na página inteira, achado em revisão).
        COALESCE(rc.tipo_doc_derivado, '')                  AS tipo_doc,
        COALESCE(pa.forn_cnpj, rc.ni_emitente, '')          AS forn_cnpj,
        pa.forn_nome,
        COALESCE(pa.num_parcelas, 0)::INT                   AS num_parcelas,
        COALESCE(pa.total_pago, 0)                          AS total_pago,
        pa.primeira_parcela,
        pa.ultima_parcela,
        COALESCE(
            CASE WHEN rc.tipo_doc_derivado = 'NFE' THEN ne.v_nf    ELSE NULL END,
            CASE WHEN rc.tipo_doc_derivado = 'CTE' THEN ct.v_prest ELSE NULL END,
            0
        )::NUMERIC(15,2)                                    AS valor_nota,
        COALESCE(
            CASE WHEN rc.tipo_doc_derivado = 'NFE' THEN ne.v_cbs ELSE NULL END,
            CASE WHEN rc.tipo_doc_derivado = 'CTE' THEN ct.v_cbs ELSE NULL END,
            0
        )::NUMERIC(15,2)                                    AS valor_cbs_nota,
        COALESCE(
            CASE WHEN rc.tipo_doc_derivado = 'NFE' THEN ne.v_ibs ELSE NULL END,
            CASE WHEN rc.tipo_doc_derivado = 'CTE' THEN ct.v_ibs ELSE NULL END,
            0
        )::NUMERIC(15,2)                                    AS valor_ibs_nota,
        COALESCE(rc.valor_cbs_nao_extinto, 0)               AS valor_cbs_nao_extinto,
        rc.situacao_credito,
        -- Story 3.1 (AC #1): "sem nenhum pagamento" usa um EXISTS não filtrado
        -- por mes_ano/forn_cnpj (checa a tabela crua, mesma abrangência do
        -- NOT EXISTS de pagamentos_orfaos) — NUNCA pa.chave_doc IS NULL, que
        -- reflete apenas o filtro atual. Sem isso, um crédito JÁ pago (mas em
        -- outro mês/fornecedor que o filtro corrente) apareceria como
        -- 'aguardando_pagamento' em vez de 'pendente' (bug encontrado e
        -- confirmado empiricamente em revisão — ver Review Findings).
        CASE
            WHEN rc.valor_cbs_nao_extinto > 0
                 AND NOT EXISTS (
                     SELECT 1 FROM pagamentos_fornecedores px
                     WHERE px.company_id = $1 AND px.chave_doc = rc.chave_dfe
                 )                                          THEN 'aguardando_pagamento'
            WHEN rc.valor_cbs_nao_extinto > 0               THEN 'pendente'
            ELSE                                                  'extinto'
        END                                                 AS status_conciliacao,
        -- possivel_duplicidade (Story 2.5) só faz sentido quando há pagamento
        -- para comparar — sem pagamento, é sempre false (nada para duplicar).
        pa.chave_doc IS NOT NULL
        AND COALESCE(
            CASE WHEN rc.tipo_doc_derivado = 'NFE' THEN ne.v_nf    ELSE NULL END,
            CASE WHEN rc.tipo_doc_derivado = 'CTE' THEN ct.v_prest ELSE NULL END
        ) IS NOT NULL
        AND pa.total_pago > COALESCE(
            CASE WHEN rc.tipo_doc_derivado = 'NFE' THEN ne.v_nf    ELSE NULL END,
            CASE WHEN rc.tipo_doc_derivado = 'CTE' THEN ct.v_prest ELSE NULL END,
            0
        )                                                   AS possivel_duplicidade,
        pa.payment_status,
        pa.match_type,
        COALESCE(pa.fallback_ambiguous, false)              AS fallback_ambiguous,
        COALESCE(pa.origem, '')                             AS origem,
        -- Story 3.2 (AC #2): soma de pagamentos cobre a nota mas a RFB ainda
        -- não confirmou extinção — exige valor_nota real (mesma checagem IS
        -- NOT NULL já usada em possivel_duplicidade, Story 2.5) e crédito
        -- ainda 'pendente' (valor_cbs_nao_extinto > 0); NUNCA reclassifica.
        -- COALESCE(..., false) obrigatório: valor_cbs_nao_extinto é NULLABLE
        -- (migration 096, DEFAULT 0 mas sem NOT NULL). Com nota casada +
        -- pagamento cobrindo + valor_cbs_nao_extinto NULL, a expressão sem
        -- COALESCE resolve para NULL (TRUE AND NULL AND TRUE AND TRUE), e o Scan
        -- em bool derruba a listagem inteira com 500 — confirmado
        -- empiricamente em revisão (mesma classe do bug de tipo_doc da 3.1).
        COALESCE(
            pa.chave_doc IS NOT NULL
            AND rc.valor_cbs_nao_extinto > 0
            AND COALESCE(
                CASE WHEN rc.tipo_doc_derivado = 'NFE' THEN ne.v_nf    ELSE NULL END,
                CASE WHEN rc.tipo_doc_derivado = 'CTE' THEN ct.v_prest ELSE NULL END
            ) IS NOT NULL
            AND pa.total_pago >= COALESCE(
                CASE WHEN rc.tipo_doc_derivado = 'NFE' THEN ne.v_nf    ELSE NULL END,
                CASE WHEN rc.tipo_doc_derivado = 'CTE' THEN ct.v_prest ELSE NULL END,
                0
            )
        , false)                                            AS pagamento_cobre_valor_nota,
        -- Story 3.2 (AC #3): só é relevante quando não há pagamento — quando
        -- um pagamento real é persistido, persistPayments já apaga a linha
        -- correspondente de sap_resultados_busca (ver services), então o
        -- LEFT JOIN abaixo naturalmente não traz nada nesse caso.
        srb.payment_status                                  AS ultimo_status_busca,
        srb.ultima_tentativa_em                              AS ultima_tentativa_busca
    FROM creditos_tipados rc
    LEFT JOIN pagamentos_agg pa
        ON pa.chave_doc = rc.chave_dfe
    LEFT JOIN nfe_entradas ne
        ON ne.company_id = $1
       AND rc.tipo_doc_derivado = 'NFE'
       AND ne.chave_nfe = rc.chave_dfe
    LEFT JOIN cte_entradas ct
        ON ct.company_id = $1
       AND rc.tipo_doc_derivado = 'CTE'
       AND ct.chave_cte = rc.chave_dfe
    LEFT JOIN sap_resultados_busca srb
        ON srb.company_id = $1
       AND srb.chave_dfe = rc.chave_dfe
),
pagamentos_orfaos AS (
    -- Story 3.1 (AC #3, regressão): pagamento sem NENHUM rfb_creditos
    -- correspondente continua aparecendo como 'sem_dados' — comportamento
    -- idêntico ao da query anterior à inversão (quando rc.id IS NULL).
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
        0::NUMERIC(15,2)                                    AS valor_cbs_nao_extinto,
        NULL::VARCHAR(100)                                  AS situacao_credito,
        'sem_dados'                                         AS status_conciliacao,
        COALESCE(
            CASE WHEN pa.tipo_doc = 'NFE' THEN ne.v_nf    ELSE NULL END,
            CASE WHEN pa.tipo_doc = 'CTE' THEN ct.v_prest ELSE NULL END
        ) IS NOT NULL
        AND pa.total_pago > COALESCE(
            CASE WHEN pa.tipo_doc = 'NFE' THEN ne.v_nf    ELSE NULL END,
            CASE WHEN pa.tipo_doc = 'CTE' THEN ct.v_prest ELSE NULL END,
            0
        )                                                   AS possivel_duplicidade,
        pa.payment_status,
        pa.match_type,
        COALESCE(pa.fallback_ambiguous, false)              AS fallback_ambiguous,
        COALESCE(pa.origem, '')                             AS origem,
        false                                                AS pagamento_cobre_valor_nota,
        NULL::VARCHAR(20)                                   AS ultimo_status_busca,
        NULL::TIMESTAMPTZ                                   AS ultima_tentativa_busca
    FROM pagamentos_agg pa
    LEFT JOIN nfe_entradas ne
        ON ne.company_id = $1
       AND pa.tipo_doc = 'NFE'
       AND ne.chave_nfe = pa.chave_doc
    LEFT JOIN cte_entradas ct
        ON ct.company_id = $1
       AND pa.tipo_doc = 'CTE'
       AND ct.chave_cte = pa.chave_doc
    WHERE NOT EXISTS (
        SELECT 1 FROM rfb_creditos rc2
        WHERE rc2.company_id = $1
          AND rc2.chave_dfe = pa.chave_doc
    )
),
conciliacao AS (
    SELECT * FROM conciliacao_creditos
    UNION ALL
    SELECT * FROM pagamentos_orfaos
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
    status_conciliacao,
    possivel_duplicidade,
    payment_status,
    match_type,
    fallback_ambiguous,
    origem,
    pagamento_cobre_valor_nota,
    ultimo_status_busca,
    ultima_tentativa_busca
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
			var paymentStatus sql.NullString
			var matchType sql.NullString
			var ultimoStatusBusca sql.NullString
			var ultimaTentativaBusca sql.NullTime

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
				&it.PossivelDuplicidade,
				&paymentStatus,
				&matchType,
				&it.FallbackAmbiguous,
				&it.Origem,
				&it.PagamentoCobreValorNota,
				&ultimoStatusBusca,
				&ultimaTentativaBusca,
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
			if paymentStatus.Valid {
				it.PaymentStatus = &paymentStatus.String
			}
			if matchType.Valid {
				it.MatchType = &matchType.String
			}
			if ultimoStatusBusca.Valid {
				it.UltimoStatusBusca = &ultimoStatusBusca.String
			}
			if ultimaTentativaBusca.Valid {
				s := ultimaTentativaBusca.Time.Format("2006-01-02T15:04:05Z07:00")
				it.UltimaTentativaBusca = &s
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
