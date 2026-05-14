package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// DashboardResumoHandler retorna visão consolidada de documentos fiscais e apuração IBS/CBS
// filtrada por company_id e mes_ano.
func DashboardResumoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, 401, "Não autenticado")
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, 500, "Erro ao identificar empresa", err, "[DashboardResumo]")
			return
		}

		// Meses disponíveis (apenas anos >= 2025)
		perRows, _ := db.Query(`
			SELECT mes_ano FROM (
				SELECT DISTINCT mes_ano FROM (
					SELECT mes_ano FROM nfe_entradas WHERE company_id = $1
					UNION
					SELECT mes_ano FROM cte_entradas WHERE company_id = $1
				) t
				WHERE mes_ano IS NOT NULL AND mes_ano != ''
				  AND SPLIT_PART(mes_ano, '/', 2) >= '2025'
			) u
			ORDER BY SPLIT_PART(mes_ano, '/', 2) DESC,
			         SPLIT_PART(mes_ano, '/', 1) DESC
		`, companyID)
		var mesesDisp []string
		if perRows != nil {
			defer perRows.Close()
			for perRows.Next() {
				var m string
				if perRows.Scan(&m) == nil {
					mesesDisp = append(mesesDisp, m)
				}
			}
		}
		if mesesDisp == nil {
			mesesDisp = []string{}
		}

		// Mês selecionado: parâmetro ?mes_ano=MM/YYYY; padrão = mais recente
		mesDB := r.URL.Query().Get("mes_ano")
		if mesDB == "" && len(mesesDisp) > 0 {
			mesDB = mesesDisp[0]
		}

		// ── NF-e Entradas ──────────────────────────────────────────────────────
		var entCount int
		var entVNF, entVIBS, entVCBS, entVBcIBS, entVIBSLiq, entVCBSLiq float64
		err = db.QueryRow(`
			SELECT
				COUNT(*),
				COALESCE(SUM(v_nf), 0),
				COALESCE(SUM(v_ibs), 0),
				COALESCE(SUM(v_cbs), 0),
				COALESCE(SUM(v_bc_ibs_cbs), 0),
				COALESCE(SUM(CASE WHEN (ne.v_ibs > 0 OR ne.v_cbs > 0) AND fs.cnpj IS NULL
				                       AND ne.forn_cnpj IS NOT NULL AND ne.forn_cnpj != ''
				              THEN ne.v_ibs ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN (ne.v_ibs > 0 OR ne.v_cbs > 0) AND fs.cnpj IS NULL
				                       AND ne.forn_cnpj IS NOT NULL AND ne.forn_cnpj != ''
				              THEN ne.v_cbs ELSE 0 END), 0)
			FROM nfe_entradas ne
			LEFT JOIN forn_simples fs ON fs.cnpj = ne.forn_cnpj
			WHERE ne.company_id = $1 AND ne.mes_ano = $2
		`, companyID, mesDB).Scan(&entCount, &entVNF, &entVIBS, &entVCBS, &entVBcIBS, &entVIBSLiq, &entVCBSLiq)
		if err != nil {
			sanitizeDBErr(w, 500, "Erro ao consultar NF-e entradas", err, "[DashboardResumo]")
			return
		}

		// ── NF-e Saídas ────────────────────────────────────────────────────────
		var saiCount int
		var saiVNF, saiVIBS, saiVCBS, saiVBcIBS float64
		err = db.QueryRow(`
			SELECT
				COUNT(*),
				COALESCE(SUM(v_nf), 0),
				COALESCE(SUM(v_ibs), 0),
				COALESCE(SUM(v_cbs), 0),
				COALESCE(SUM(v_bc_ibs_cbs), 0)
			FROM nfe_saidas
			WHERE company_id = $1 AND mes_ano = $2
		`, companyID, mesDB).Scan(&saiCount, &saiVNF, &saiVIBS, &saiVCBS, &saiVBcIBS)
		if err != nil {
			sanitizeDBErr(w, 500, "Erro ao consultar NF-e saídas", err, "[DashboardResumo]")
			return
		}

		// ── CT-e Entradas (usa v_prest — v_rec foi removido na migration 083) ────
		var cteCount int
		var cteVPrest, cteVIBS, cteVCBS, cteVBcIBS, cteVIBSLiq, cteVCBSLiq float64
		err = db.QueryRow(`
			SELECT
				COUNT(*),
				COALESCE(SUM(v_prest), 0),
				COALESCE(SUM(v_ibs), 0),
				COALESCE(SUM(v_cbs), 0),
				COALESCE(SUM(v_bc_ibs_cbs), 0),
				COALESCE(SUM(CASE WHEN (ce.v_ibs > 0 OR ce.v_cbs > 0) AND fs.cnpj IS NULL
				                       AND ce.emit_cnpj IS NOT NULL AND ce.emit_cnpj != ''
				              THEN ce.v_ibs ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN (ce.v_ibs > 0 OR ce.v_cbs > 0) AND fs.cnpj IS NULL
				                       AND ce.emit_cnpj IS NOT NULL AND ce.emit_cnpj != ''
				              THEN ce.v_cbs ELSE 0 END), 0)
			FROM cte_entradas ce
			LEFT JOIN forn_simples fs ON fs.cnpj = ce.emit_cnpj
			WHERE ce.company_id = $1 AND ce.mes_ano = $2
		`, companyID, mesDB).Scan(&cteCount, &cteVPrest, &cteVIBS, &cteVCBS, &cteVBcIBS, &cteVIBSLiq, &cteVCBSLiq)
		if err != nil {
			sanitizeDBErr(w, 500, "Erro ao consultar CT-e entradas", err, "[DashboardResumo]")
			return
		}

		// ── Campos derivados ───────────────────────────────────────────────────
		totalCreditosIBS := entVIBS + cteVIBS
		totalCreditosCBS := entVCBS + cteVCBS
		totalDebitosIBS := saiVIBS
		totalDebitosCBS := saiVCBS
		saldoIBS := totalDebitosIBS - totalCreditosIBS
		saldoCBS := totalDebitosCBS - totalCreditosCBS

		// Créditos líquidos: excluem Simples Nacional e entradas sem IBS/CBS
		creditosIBSLiquidos := entVIBSLiq + cteVIBSLiq
		creditosCBSLiquidos := entVCBSLiq + cteVCBSLiq
		creditosIBSEmRisco  := totalCreditosIBS - creditosIBSLiquidos
		creditosCBSEmRisco  := totalCreditosCBS - creditosCBSLiquidos

		var aliquotaEfetivaIBS *float64
		if saiVBcIBS > 0 {
			v := (totalDebitosIBS / saiVBcIBS) * 100
			aliquotaEfetivaIBS = &v
		}

		creditosApropriar := totalCreditosIBS + totalCreditosCBS - (totalDebitosIBS + totalDebitosCBS)
		if creditosApropriar < 0 {
			creditosApropriar = 0
		}

		// ── Resposta ───────────────────────────────────────────────────────────
		type blocoDoc struct {
			Count  int     `json:"count"`
			VNF    float64 `json:"v_nf"`
			VIBS   float64 `json:"v_ibs"`
			VCBS   float64 `json:"v_cbs"`
			VBcIBS float64 `json:"v_bc_ibs_cbs"`
		}

		type blocoDocCTE struct {
			Count   int     `json:"count"`
			VPrest  float64 `json:"v_prest"`
			VIBS    float64 `json:"v_ibs"`
			VCBS    float64 `json:"v_cbs"`
			VBcIBS  float64 `json:"v_bc_ibs_cbs"`
		}

		resp := struct {
			MesesDisponiveis      []string    `json:"meses_disponiveis"`
			MesSelecionado        string      `json:"mes_selecionado"`
			MesAno                string      `json:"mes_ano"`
			NfeEntradas           blocoDoc    `json:"nfe_entradas"`
			NfeSaidas             blocoDoc    `json:"nfe_saidas"`
			CteEntradas           blocoDocCTE `json:"cte_entradas"`
			TotalCreditosIBS      float64     `json:"total_creditos_ibs"`
			TotalCreditosCBS      float64     `json:"total_creditos_cbs"`
			CreditosIBSLiquidos   float64     `json:"creditos_ibs_liquidos"`
			CreditosCBSLiquidos   float64     `json:"creditos_cbs_liquidos"`
			CreditosIBSEmRisco    float64     `json:"creditos_ibs_em_risco"`
			CreditosCBSEmRisco    float64     `json:"creditos_cbs_em_risco"`
			TotalDebitosIBS       float64     `json:"total_debitos_ibs"`
			TotalDebitosCBS       float64     `json:"total_debitos_cbs"`
			SaldoIBS              float64     `json:"saldo_ibs"`
			SaldoCBS              float64     `json:"saldo_cbs"`
			CreditosApropriar     float64     `json:"creditos_apropriar"`
			AliquotaEfetivaIBS    *float64    `json:"aliquota_efetiva_ibs"`
		}{
			MesesDisponiveis:      mesesDisp,
			MesSelecionado:        mesDB,
			MesAno:                mesDB,
			NfeEntradas:           blocoDoc{Count: entCount, VNF: entVNF, VIBS: entVIBS, VCBS: entVCBS, VBcIBS: entVBcIBS},
			NfeSaidas:             blocoDoc{Count: saiCount, VNF: saiVNF, VIBS: saiVIBS, VCBS: saiVCBS, VBcIBS: saiVBcIBS},
			CteEntradas:           blocoDocCTE{Count: cteCount, VPrest: cteVPrest, VIBS: cteVIBS, VCBS: cteVCBS, VBcIBS: cteVBcIBS},
			TotalCreditosIBS:      totalCreditosIBS,
			TotalCreditosCBS:      totalCreditosCBS,
			CreditosIBSLiquidos:   creditosIBSLiquidos,
			CreditosCBSLiquidos:   creditosCBSLiquidos,
			CreditosIBSEmRisco:    creditosIBSEmRisco,
			CreditosCBSEmRisco:    creditosCBSEmRisco,
			TotalDebitosIBS:       totalDebitosIBS,
			TotalDebitosCBS:       totalDebitosCBS,
			SaldoIBS:              saldoIBS,
			SaldoCBS:              saldoCBS,
			CreditosApropriar:     creditosApropriar,
			AliquotaEfetivaIBS:    aliquotaEfetivaIBS,
		}

		json.NewEncoder(w).Encode(resp)
	}
}
