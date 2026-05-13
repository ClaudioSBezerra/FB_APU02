package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"time"
)

var mesAnoRegexp = regexp.MustCompile(`^\d{4}-\d{2}$`)

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

		mes := r.URL.Query().Get("mes")
		if !mesAnoRegexp.MatchString(mes) {
			mes = time.Now().Format("2006-01")
		}

		// ── NF-e Entradas ──────────────────────────────────────────────────────
		var entCount int
		var entVNF, entVIBS, entVCBS, entVBcIBS float64
		err = db.QueryRow(`
			SELECT
				COUNT(*),
				COALESCE(SUM(v_nf), 0),
				COALESCE(SUM(v_ibs), 0),
				COALESCE(SUM(v_cbs), 0),
				COALESCE(SUM(v_bc_ibs_cbs), 0)
			FROM nfe_entradas
			WHERE company_id = $1 AND mes_ano = $2
		`, companyID, mes).Scan(&entCount, &entVNF, &entVIBS, &entVCBS, &entVBcIBS)
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
		`, companyID, mes).Scan(&saiCount, &saiVNF, &saiVIBS, &saiVCBS, &saiVBcIBS)
		if err != nil {
			sanitizeDBErr(w, 500, "Erro ao consultar NF-e saídas", err, "[DashboardResumo]")
			return
		}

		// ── CT-e Entradas (usa v_rec em vez de v_nf) ───────────────────────────
		var cteCount int
		var cteVRec, cteVIBS, cteVCBS, cteVBcIBS float64
		err = db.QueryRow(`
			SELECT
				COUNT(*),
				COALESCE(SUM(v_rec), 0),
				COALESCE(SUM(v_ibs), 0),
				COALESCE(SUM(v_cbs), 0),
				COALESCE(SUM(v_bc_ibs_cbs), 0)
			FROM cte_entradas
			WHERE company_id = $1 AND mes_ano = $2
		`, companyID, mes).Scan(&cteCount, &cteVRec, &cteVIBS, &cteVCBS, &cteVBcIBS)
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
			Count  int     `json:"count"`
			VRec   float64 `json:"v_rec"`
			VIBS   float64 `json:"v_ibs"`
			VCBS   float64 `json:"v_cbs"`
			VBcIBS float64 `json:"v_bc_ibs_cbs"`
		}

		resp := struct {
			MesAno             string      `json:"mes_ano"`
			NfeEntradas        blocoDoc    `json:"nfe_entradas"`
			NfeSaidas          blocoDoc    `json:"nfe_saidas"`
			CteEntradas        blocoDocCTE `json:"cte_entradas"`
			TotalCreditosIBS   float64     `json:"total_creditos_ibs"`
			TotalCreditosCBS   float64     `json:"total_creditos_cbs"`
			TotalDebitosIBS    float64     `json:"total_debitos_ibs"`
			TotalDebitosCBS    float64     `json:"total_debitos_cbs"`
			SaldoIBS           float64     `json:"saldo_ibs"`
			SaldoCBS           float64     `json:"saldo_cbs"`
			CreditosApropriar  float64     `json:"creditos_apropriar"`
			AliquotaEfetivaIBS *float64    `json:"aliquota_efetiva_ibs"`
		}{
			MesAno:             mes,
			NfeEntradas:        blocoDoc{Count: entCount, VNF: entVNF, VIBS: entVIBS, VCBS: entVCBS, VBcIBS: entVBcIBS},
			NfeSaidas:          blocoDoc{Count: saiCount, VNF: saiVNF, VIBS: saiVIBS, VCBS: saiVCBS, VBcIBS: saiVBcIBS},
			CteEntradas:        blocoDocCTE{Count: cteCount, VRec: cteVRec, VIBS: cteVIBS, VCBS: cteVCBS, VBcIBS: cteVBcIBS},
			TotalCreditosIBS:   totalCreditosIBS,
			TotalCreditosCBS:   totalCreditosCBS,
			TotalDebitosIBS:    totalDebitosIBS,
			TotalDebitosCBS:    totalDebitosCBS,
			SaldoIBS:           saldoIBS,
			SaldoCBS:           saldoCBS,
			CreditosApropriar:  creditosApropriar,
			AliquotaEfetivaIBS: aliquotaEfetivaIBS,
		}

		json.NewEncoder(w).Encode(resp)
	}
}
