package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
)

// ResumoAgregado é o resumo calculado dinamicamente sobre rfb_debitos filtrado.
type ResumoAgregado struct {
	TotalDebitos       int     `json:"total_debitos"`
	ValorCBSTotal      float64 `json:"valor_cbs_total"`
	ValorCBSExtinto    float64 `json:"valor_cbs_extinto"`
	ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
	TotalCorrente      int     `json:"total_corrente"`
	TotalAjuste        int     `json:"total_ajuste"`
	TotalExtemporaneo  int     `json:"total_extemporaneo"`
}

// ListarDebitosHandler serves GET /api/rfb/debitos
// Parâmetros: periodo (YYYYMM), ano (YYYY), modelo, data_de, data_ate, chave,
//
//	ni_adquirente, ni_emitente, page, page_size
//
// Consulta rfb_debitos diretamente — não depende de request_id.
func ListarDebitosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBDebitosLista]")
			return
		}

		qp := r.URL.Query()
		periodo := qp.Get("periodo") // YYYYMM — mês específico
		ano := qp.Get("ano")         // YYYY — todo o ano

		page := 1
		pageSize := 100
		if p, e := strconv.Atoi(qp.Get("page")); e == nil && p > 0 {
			page = p
		}
		if ps, e := strconv.Atoi(qp.Get("page_size")); e == nil && ps > 0 && ps <= 500 {
			pageSize = ps
		}

		filterModelo := qp.Get("modelo")
		filterDataDe := qp.Get("data_de")
		filterDataAte := qp.Get("data_ate")
		filterChave := qp.Get("chave")
		filterAdquir := qp.Get("ni_adquirente")
		filterEmit := qp.Get("ni_emitente")

		args := []interface{}{companyID}
		idx := 2
		where := "WHERE d.company_id = $1"

		// Filtro de período — normaliza tanto YYYYMM quanto YYYY-MM
		if periodo != "" {
			where += fmt.Sprintf(
				" AND REGEXP_REPLACE(d.data_apuracao, '[^0-9]', '', 'g') = $%d", idx)
			args = append(args, periodo)
			idx++
		} else if ano != "" {
			where += fmt.Sprintf(
				" AND REGEXP_REPLACE(d.data_apuracao, '[^0-9]', '', 'g') LIKE $%d", idx)
			args = append(args, ano+"%")
			idx++
		}

		if filterModelo != "" {
			where += fmt.Sprintf(
				" AND (d.modelo_dfe = $%d OR (COALESCE(d.modelo_dfe,'')='' AND length(d.chave_dfe)=44 AND SUBSTRING(d.chave_dfe,21,2)=$%d))",
				idx, idx)
			args = append(args, filterModelo)
			idx++
		}
		if filterDataDe != "" {
			where += fmt.Sprintf(" AND d.data_dfe_emissao >= $%d::date", idx)
			args = append(args, filterDataDe)
			idx++
		}
		if filterDataAte != "" {
			where += fmt.Sprintf(" AND d.data_dfe_emissao <= $%d::date", idx)
			args = append(args, filterDataAte)
			idx++
		}
		if filterChave != "" {
			where += fmt.Sprintf(" AND d.chave_dfe ILIKE $%d", idx)
			args = append(args, "%"+filterChave+"%")
			idx++
		}
		if filterAdquir != "" {
			where += fmt.Sprintf(" AND d.ni_adquirente LIKE $%d", idx)
			args = append(args, "%"+filterAdquir+"%")
			idx++
		}
		if filterEmit != "" {
			where += fmt.Sprintf(" AND d.ni_emitente = $%d", idx)
			args = append(args, filterEmit)
			idx++
		}

		// Resumo agregado
		var resumo ResumoAgregado
		sumQ := fmt.Sprintf(`
			SELECT
				COUNT(*),
				COALESCE(SUM(valor_cbs_total), 0),
				COALESCE(SUM(valor_cbs_extinto), 0),
				COALESCE(SUM(valor_cbs_nao_extinto), 0),
				COUNT(*) FILTER (WHERE tipo_apuracao = 'corrente'),
				COUNT(*) FILTER (WHERE tipo_apuracao = 'ajuste'),
				COUNT(*) FILTER (WHERE tipo_apuracao = 'extemporaneo')
			FROM rfb_debitos d
			%s`, where)
		if err := db.QueryRow(sumQ, args...).Scan(
			&resumo.TotalDebitos,
			&resumo.ValorCBSTotal, &resumo.ValorCBSExtinto, &resumo.ValorCBSNaoExtinto,
			&resumo.TotalCorrente, &resumo.TotalAjuste, &resumo.TotalExtemporaneo,
		); err != nil {
			log.Printf("[RFB Debitos Lista] aggregate error: %v", err)
		}

		// Quebra do resumo por situação do débito (dado já reportado pela RFB por documento).
		type SituacaoBreakdown struct {
			Situacao           string  `json:"situacao"`
			Quantidade         int     `json:"quantidade"`
			ValorCBSTotal      float64 `json:"valor_cbs_total"`
			ValorCBSExtinto    float64 `json:"valor_cbs_extinto"`
			ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
		}
		var porSituacao []SituacaoBreakdown
		situacaoQ := fmt.Sprintf(`
			SELECT COALESCE(NULLIF(d.situacao_debito, ''), '(sem situação)'),
				COUNT(*),
				COALESCE(SUM(d.valor_cbs_total), 0),
				COALESCE(SUM(d.valor_cbs_extinto), 0),
				COALESCE(SUM(d.valor_cbs_nao_extinto), 0)
			FROM rfb_debitos d
			%s
			GROUP BY 1
			ORDER BY 3 DESC`, where)
		if sitRows, sitErr := db.Query(situacaoQ, args...); sitErr == nil {
			defer sitRows.Close()
			for sitRows.Next() {
				var s SituacaoBreakdown
				if sitRows.Scan(&s.Situacao, &s.Quantidade, &s.ValorCBSTotal, &s.ValorCBSExtinto, &s.ValorCBSNaoExtinto) == nil {
					porSituacao = append(porSituacao, s)
				}
			}
		} else {
			log.Printf("[RFB Debitos Lista] breakdown por situação error: %v", sitErr)
		}

		// Quebra por forma de pagamento prevista (cronograma de liquidação, migration 126).
		// Documentos sem linha em rfb_debitos_liquidacoes caem em "(sem cronograma)" — a
		// maioria hoje, até o cronograma ser populado a partir dos dados de compra/venda do ERP.
		type FormaPagamentoBreakdown struct {
			ArranjoPagamento      string  `json:"arranjo_pagamento"`
			QtdeParcelas          int     `json:"qtde_parcelas"`
			ValorCBSPrevisto      float64 `json:"valor_cbs_previsto"`
			ParcelasLiquidadas    int     `json:"parcelas_liquidadas"`
			ParcelasReconciliadas int     `json:"parcelas_reconciliadas"`
		}
		var porFormaPagamento []FormaPagamentoBreakdown
		formaPagamentoQ := fmt.Sprintf(`
			SELECT COALESCE(l.arranjo_pagamento, '(sem cronograma)'),
				COUNT(*),
				COALESCE(SUM(l.valor_cbs_proporcional), 0),
				COUNT(*) FILTER (WHERE l.status = 'liquidado'),
				COUNT(*) FILTER (WHERE l.status = 'reconciliado')
			FROM rfb_debitos d
			LEFT JOIN rfb_debitos_liquidacoes l
				ON l.company_id = d.company_id AND l.chave_dfe = d.chave_dfe
			%s
			GROUP BY 1
			ORDER BY 2 DESC`, where)
		if fpRows, fpErr := db.Query(formaPagamentoQ, args...); fpErr == nil {
			defer fpRows.Close()
			for fpRows.Next() {
				var f FormaPagamentoBreakdown
				if fpRows.Scan(&f.ArranjoPagamento, &f.QtdeParcelas, &f.ValorCBSPrevisto,
					&f.ParcelasLiquidadas, &f.ParcelasReconciliadas) == nil {
					porFormaPagamento = append(porFormaPagamento, f)
				}
			}
		} else {
			log.Printf("[RFB Debitos Lista] breakdown por forma de pagamento error: %v", fpErr)
		}

		totalPages := (resumo.TotalDebitos + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		offset := (page - 1) * pageSize

		selectQ := `
			SELECT d.id,
				d.tipo_apuracao,
				CASE
					WHEN COALESCE(d.modelo_dfe, '') != '' THEN d.modelo_dfe
					WHEN length(d.chave_dfe) = 44 THEN SUBSTRING(d.chave_dfe, 21, 2)
					ELSE ''
				END AS modelo_dfe,
				CASE WHEN length(d.chave_dfe) = 44 THEN SUBSTRING(d.chave_dfe, 23, 3) ELSE '' END AS serie,
				CASE
					WHEN COALESCE(d.numero_dfe, '') != '' THEN d.numero_dfe
					WHEN length(d.chave_dfe) = 44 THEN LTRIM(SUBSTRING(d.chave_dfe, 26, 9), '0')
					ELSE ''
				END AS numero_dfe,
				COALESCE(d.chave_dfe, ''),
				CASE WHEN d.data_dfe_emissao IS NOT NULL
					THEN to_char(d.data_dfe_emissao, 'YYYY-MM-DD"T"HH24:MI:SS"Z"') END,
				COALESCE(d.data_apuracao, ''),
				COALESCE(d.ni_emitente, ''),
				COALESCE(d.ni_adquirente, ''),
				n.v_nf,
				COALESCE(d.valor_cbs_total, 0),
				COALESCE(d.valor_cbs_extinto, 0),
				COALESCE(d.valor_cbs_nao_extinto, 0),
				COALESCE(d.situacao_debito, '')
			FROM rfb_debitos d
			LEFT JOIN nfe_saidas n ON n.chave_nfe = d.chave_dfe AND n.company_id = d.company_id
			` + where + fmt.Sprintf(`
			ORDER BY d.data_apuracao DESC, d.tipo_apuracao, d.data_dfe_emissao DESC NULLS LAST
			LIMIT $%d OFFSET $%d`, idx, idx+1)

		pageArgs := append(args, pageSize, offset)
		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar débitos", err, "[RFBDebitosLista]")
			return
		}
		defer rows.Close()

		var debitos []RFBDebitoRow
		for rows.Next() {
			var d RFBDebitoRow
			if err := rows.Scan(
				&d.ID, &d.TipoApuracao, &d.ModeloDfe,
				&d.Serie, &d.NumeroDfe, &d.ChaveDfe, &d.DataDfeEmissao, &d.DataApuracao,
				&d.NiEmitente, &d.NiAdquirente, &d.ValorDocumento,
				&d.ValorCBSTotal, &d.ValorCBSExtinto, &d.ValorCBSNaoExtinto,
				&d.SituacaoDebito,
			); err != nil {
				log.Printf("[RFB Debitos Lista] scan error: %v", err)
				continue
			}
			debitos = append(debitos, d)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"debitos":              debitos,
			"resumo":               resumo,
			"resumo_por_situacao":  porSituacao,
			"resumo_por_pagamento": porFormaPagamento,
			"pagination": map[string]int{
				"page":        page,
				"page_size":   pageSize,
				"total":       resumo.TotalDebitos,
				"total_pages": totalPages,
			},
		})
	}
}

// PeriodosDebitosHandler serves GET /api/rfb/debitos/periodos
// Retorna os períodos distintos disponíveis em rfb_debitos para a empresa.
func PeriodosDebitosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[RFBDebitosLista]")
			return
		}

		rows, err := db.Query(`
			SELECT DISTINCT REGEXP_REPLACE(data_apuracao, '[^0-9]', '', 'g') AS periodo
			FROM rfb_debitos
			WHERE company_id = $1 AND data_apuracao IS NOT NULL AND data_apuracao != ''
			ORDER BY periodo DESC
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar períodos de débitos", err, "[RFBDebitosLista]")
			return
		}
		defer rows.Close()

		var periodos []string
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil && len(p) >= 4 {
				periodos = append(periodos, p)
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{"periodos": periodos})
	}
}
