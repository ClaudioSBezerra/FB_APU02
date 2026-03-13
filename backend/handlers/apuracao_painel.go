package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Tipos de retorno
// ---------------------------------------------------------------------------

type apuracaoIBSResult struct {
	DebitoUF        float64 `json:"debito_uf"`
	DebitoMun       float64 `json:"debito_mun"`
	DebitoTotal     float64 `json:"debito_total"`
	QtdSaidas       int     `json:"qtd_saidas"`
	CreditoNfeUF    float64 `json:"credito_nfe_uf"`
	CreditoNfeMun   float64 `json:"credito_nfe_mun"`
	CreditoNfeTotal float64 `json:"credito_nfe_total"`
	QtdEntradas     int     `json:"qtd_entradas"`
	CreditoCte      float64 `json:"credito_cte"`
	QtdCtes         int     `json:"qtd_ctes"`
	SaldoUF         float64 `json:"saldo_uf"`
	SaldoMun        float64 `json:"saldo_mun"`
	SaldoTotal      float64 `json:"saldo_total"`
}

type apuracaoCBSResult struct {
	DebitoTotal     float64 `json:"debito_total"`
	QtdSaidas       int     `json:"qtd_saidas"`
	CreditoNfeTotal float64 `json:"credito_nfe_total"`
	QtdEntradas     int     `json:"qtd_entradas"`
	CreditoCte      float64 `json:"credito_cte"`
	QtdCtes         int     `json:"qtd_ctes"`
	SaldoTotal      float64 `json:"saldo_total"`
}

type apuracaoPainelResponse struct {
	MesesDisponiveis []string          `json:"meses_disponiveis"`
	MesSelecionado   string            `json:"mes_selecionado"`
	IBS              apuracaoIBSResult `json:"ibs"`
	CBS              apuracaoCBSResult `json:"cbs"`
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

func ApuracaoPainelHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		userID := claims["user_id"].(string)

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa: "+err.Error())
			return
		}

		// ── Meses disponíveis (union das 3 tabelas) ──────────────────────────
		rows, err := db.Query(`
			SELECT DISTINCT mes_ano FROM (
				SELECT mes_ano FROM nfe_saidas   WHERE company_id = $1
				UNION
				SELECT mes_ano FROM nfe_entradas WHERE company_id = $1
				UNION
				SELECT mes_ano FROM cte_entradas WHERE company_id = $1
			) t ORDER BY mes_ano DESC
		`, companyID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao listar períodos: "+err.Error())
			return
		}
		defer rows.Close()

		var meses []string
		for rows.Next() {
			var m string
			if err := rows.Scan(&m); err == nil {
				meses = append(meses, m)
			}
		}
		if meses == nil {
			meses = []string{}
		}

		// ── Mês selecionado ──────────────────────────────────────────────────
		mesAno := r.URL.Query().Get("mes_ano")
		if mesAno == "" && len(meses) > 0 {
			mesAno = meses[0] // mais recente
		}

		var resp apuracaoPainelResponse
		resp.MesesDisponiveis = meses
		resp.MesSelecionado = mesAno

		if mesAno == "" {
			// Sem dados — retorna zeros
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
			return
		}

		// ── Débitos (nfe_saidas) ─────────────────────────────────────────────
		var debitoIBSUF, debitoIBSMun, debitoIBS, debitoCBS float64
		var qtdSaidas int
		err = db.QueryRow(`
			SELECT
				COALESCE(SUM(v_ibs_uf),  0),
				COALESCE(SUM(v_ibs_mun), 0),
				COALESCE(SUM(v_ibs),     0),
				COALESCE(SUM(v_cbs),     0),
				COUNT(*)
			FROM nfe_saidas
			WHERE company_id = $1 AND mes_ano = $2
		`, companyID, mesAno).Scan(&debitoIBSUF, &debitoIBSMun, &debitoIBS, &debitoCBS, &qtdSaidas)
		if err != nil && err != sql.ErrNoRows {
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar saídas: "+err.Error())
			return
		}

		// ── Créditos NF-e (nfe_entradas) ─────────────────────────────────────
		var creditoNfeIBSUF, creditoNfeIBSMun, creditoNfeIBS, creditoNfeCBS float64
		var qtdEntradas int
		err = db.QueryRow(`
			SELECT
				COALESCE(SUM(v_ibs_uf),  0),
				COALESCE(SUM(v_ibs_mun), 0),
				COALESCE(SUM(v_ibs),     0),
				COALESCE(SUM(v_cbs),     0),
				COUNT(*)
			FROM nfe_entradas
			WHERE company_id = $1 AND mes_ano = $2
		`, companyID, mesAno).Scan(&creditoNfeIBSUF, &creditoNfeIBSMun, &creditoNfeIBS, &creditoNfeCBS, &qtdEntradas)
		if err != nil && err != sql.ErrNoRows {
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar entradas: "+err.Error())
			return
		}

		// ── Créditos CT-e (cte_entradas) ─────────────────────────────────────
		var creditoCteIBS, creditoCteCBS float64
		var qtdCtes int
		err = db.QueryRow(`
			SELECT
				COALESCE(SUM(v_ibs), 0),
				COALESCE(SUM(v_cbs), 0),
				COUNT(*)
			FROM cte_entradas
			WHERE company_id = $1 AND mes_ano = $2
		`, companyID, mesAno).Scan(&creditoCteIBS, &creditoCteCBS, &qtdCtes)
		if err != nil && err != sql.ErrNoRows {
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar CT-e: "+err.Error())
			return
		}

		// ── Cálculo dos saldos ────────────────────────────────────────────────
		resp.IBS = apuracaoIBSResult{
			DebitoUF:        debitoIBSUF,
			DebitoMun:       debitoIBSMun,
			DebitoTotal:     debitoIBS,
			QtdSaidas:       qtdSaidas,
			CreditoNfeUF:    creditoNfeIBSUF,
			CreditoNfeMun:   creditoNfeIBSMun,
			CreditoNfeTotal: creditoNfeIBS,
			QtdEntradas:     qtdEntradas,
			CreditoCte:      creditoCteIBS,
			QtdCtes:         qtdCtes,
			SaldoUF:         debitoIBSUF - creditoNfeIBSUF,
			SaldoMun:        debitoIBSMun - creditoNfeIBSMun,
			SaldoTotal:      debitoIBS - creditoNfeIBS - creditoCteIBS,
		}
		resp.CBS = apuracaoCBSResult{
			DebitoTotal:     debitoCBS,
			QtdSaidas:       qtdSaidas,
			CreditoNfeTotal: creditoNfeCBS,
			QtdEntradas:     qtdEntradas,
			CreditoCte:      creditoCteCBS,
			QtdCtes:         qtdCtes,
			SaldoTotal:      debitoCBS - creditoNfeCBS - creditoCteCBS,
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}
}
