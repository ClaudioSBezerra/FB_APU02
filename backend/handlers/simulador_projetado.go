package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// SimuladorAliquota é uma linha de alíquota (oficial ou com override da empresa)
// exibida no Simulador Projetado.
type SimuladorAliquota struct {
	Ano                int     `json:"ano"`
	PercIBSUf          float64 `json:"perc_ibs_uf"`
	PercIBSMun         float64 `json:"perc_ibs_mun"`
	PercCBS            float64 `json:"perc_cbs"`
	PercReducICMS      float64 `json:"perc_reduc_icms"`
	PercReducPisCofins float64 `json:"perc_reduc_piscofins"`
	Editado            bool    `json:"editado"` // true = valor vem de simulador_aliquotas (override da empresa)
}

// GetSimuladorAliquotasHandler — GET /api/rfb/simulador/aliquotas
// Retorna as alíquotas 2027-2033: valor oficial (tabela_aliquotas) por padrão,
// substituído pelo override da empresa (simulador_aliquotas) quando existir.
func GetSimuladorAliquotasHandler(db *sql.DB) http.HandlerFunc {
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
		companyID, err := GetEffectiveCompanyID(db, claims["user_id"].(string), r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[SimuladorAliquotas]")
			return
		}

		rows, err := db.Query(`
			SELECT
				t.ano,
				COALESCE(s.perc_ibs_uf, t.perc_ibs_uf),
				COALESCE(s.perc_ibs_mun, t.perc_ibs_mun),
				COALESCE(s.perc_cbs, t.perc_cbs),
				COALESCE(s.perc_reduc_icms, t.perc_reduc_icms),
				COALESCE(s.perc_reduc_piscofins, t.perc_reduc_piscofins),
				(s.id IS NOT NULL) AS editado
			FROM tabela_aliquotas t
			LEFT JOIN simulador_aliquotas s ON s.company_id = $1 AND s.ano = t.ano
			ORDER BY t.ano
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar alíquotas", err, "[SimuladorAliquotas]")
			return
		}
		defer rows.Close()

		aliquotas := []SimuladorAliquota{}
		for rows.Next() {
			var a SimuladorAliquota
			if err := rows.Scan(&a.Ano, &a.PercIBSUf, &a.PercIBSMun, &a.PercCBS, &a.PercReducICMS, &a.PercReducPisCofins, &a.Editado); err != nil {
				log.Printf("[SimuladorAliquotas] scan error: %v", err)
				continue
			}
			aliquotas = append(aliquotas, a)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"aliquotas": aliquotas})
	}
}

// PutSimuladorAliquotaHandler — PUT /api/rfb/simulador/aliquotas
// Grava (upsert) o override de um ano específico pra empresa atual.
func PutSimuladorAliquotaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPut {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, claims["user_id"].(string), r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[SimuladorAliquotas]")
			return
		}

		var req SimuladorAliquota
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "JSON inválido")
			return
		}
		if req.Ano < 2027 || req.Ano > 2033 {
			jsonErr(w, http.StatusBadRequest, "Ano deve estar entre 2027 e 2033")
			return
		}
		for _, v := range []float64{req.PercIBSUf, req.PercIBSMun, req.PercCBS, req.PercReducICMS, req.PercReducPisCofins} {
			if v < 0 || v > 100 {
				jsonErr(w, http.StatusBadRequest, "Percentuais devem estar entre 0 e 100")
				return
			}
		}

		_, err = db.Exec(`
			INSERT INTO simulador_aliquotas (
				company_id, ano, perc_ibs_uf, perc_ibs_mun, perc_cbs, perc_reduc_icms, perc_reduc_piscofins
			) VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (company_id, ano) DO UPDATE SET
				perc_ibs_uf = EXCLUDED.perc_ibs_uf,
				perc_ibs_mun = EXCLUDED.perc_ibs_mun,
				perc_cbs = EXCLUDED.perc_cbs,
				perc_reduc_icms = EXCLUDED.perc_reduc_icms,
				perc_reduc_piscofins = EXCLUDED.perc_reduc_piscofins,
				updated_at = NOW()
		`, companyID, req.Ano, req.PercIBSUf, req.PercIBSMun, req.PercCBS, req.PercReducICMS, req.PercReducPisCofins)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao salvar alíquota", err, "[SimuladorAliquotas]")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeleteSimuladorAliquotaHandler — DELETE /api/rfb/simulador/aliquotas?ano=NNNN
// Remove o override da empresa pra um ano, voltando a usar o valor oficial.
func DeleteSimuladorAliquotaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodDelete {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, claims["user_id"].(string), r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[SimuladorAliquotas]")
			return
		}

		ano := r.URL.Query().Get("ano")
		if ano == "" {
			jsonErr(w, http.StatusBadRequest, "ano obrigatório")
			return
		}
		if _, err := db.Exec(`DELETE FROM simulador_aliquotas WHERE company_id = $1 AND ano = $2`, companyID, ano); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao remover override", err, "[SimuladorAliquotas]")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// SimuladorProjecaoAno é a projeção de débito/crédito IBS+CBS pra um ano da transição.
// Saldo = débito - crédito por imposto (positivo = a pagar, negativo = a recuperar) —
// o "quadro de apuração" da tela é justamente esses dois saldos lado a lado.
type SimuladorProjecaoAno struct {
	Ano        int     `json:"ano"`
	PercIBS    float64 `json:"perc_ibs"` // perc_ibs_uf + perc_ibs_mun
	PercCBS    float64 `json:"perc_cbs"`
	DebitoIBS  float64 `json:"debito_ibs_projetado"`
	DebitoCBS  float64 `json:"debito_cbs_projetado"`
	CreditoIBS float64 `json:"credito_ibs_projetado"`
	CreditoCBS float64 `json:"credito_cbs_projetado"`
	SaldoIBS   float64 `json:"saldo_ibs"`
	SaldoCBS   float64 `json:"saldo_cbs"`
}

// GetSimuladorProjecaoHandler — GET /api/rfb/simulador/projecao?periodo=YYYYMM|ano=YYYY
// Projeta débito (base de nfe_saidas) e crédito (base de nfe_entradas + cte_entradas)
// de IBS/CBS para 2027-2033, usando as alíquotas mescladas (oficial + override).
func GetSimuladorProjecaoHandler(db *sql.DB) http.HandlerFunc {
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
		companyID, err := GetEffectiveCompanyID(db, claims["user_id"].(string), r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[SimuladorProjecao]")
			return
		}

		qp := r.URL.Query()
		periodo := qp.Get("periodo")
		ano := qp.Get("ano")

		periodoWhere := ""
		var periodoArg string
		if periodo != "" {
			periodoWhere = "AND mes_ano = $2"
			periodoArg = periodo
		} else if ano != "" {
			periodoWhere = "AND SPLIT_PART(mes_ano, '/', 2) = $2"
			periodoArg = ano
		}

		var baseDebito, baseCredito float64
		debitoQ := "SELECT COALESCE(SUM(v_bc_ibs_cbs),0) FROM nfe_saidas WHERE company_id = $1 " + periodoWhere
		if periodoArg != "" {
			db.QueryRow(debitoQ, companyID, periodoArg).Scan(&baseDebito)
		} else {
			db.QueryRow(debitoQ, companyID).Scan(&baseDebito)
		}

		creditoQ := `
			SELECT
				COALESCE((SELECT SUM(v_bc_ibs_cbs) FROM nfe_entradas WHERE company_id = $1 ` + periodoWhere + `), 0) +
				COALESCE((SELECT SUM(v_bc_ibs_cbs) FROM cte_entradas WHERE company_id = $1 ` + periodoWhere + `), 0)
		`
		if periodoArg != "" {
			db.QueryRow(creditoQ, companyID, periodoArg).Scan(&baseCredito)
		} else {
			db.QueryRow(creditoQ, companyID).Scan(&baseCredito)
		}

		rows, err := db.Query(`
			SELECT
				t.ano,
				COALESCE(s.perc_ibs_uf, t.perc_ibs_uf) + COALESCE(s.perc_ibs_mun, t.perc_ibs_mun),
				COALESCE(s.perc_cbs, t.perc_cbs)
			FROM tabela_aliquotas t
			LEFT JOIN simulador_aliquotas s ON s.company_id = $1 AND s.ano = t.ano
			ORDER BY t.ano
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar alíquotas", err, "[SimuladorProjecao]")
			return
		}
		defer rows.Close()

		projecao := []SimuladorProjecaoAno{}
		for rows.Next() {
			var p SimuladorProjecaoAno
			if err := rows.Scan(&p.Ano, &p.PercIBS, &p.PercCBS); err != nil {
				log.Printf("[SimuladorProjecao] scan error: %v", err)
				continue
			}
			p.DebitoIBS = baseDebito * p.PercIBS / 100.0
			p.DebitoCBS = baseDebito * p.PercCBS / 100.0
			p.CreditoIBS = baseCredito * p.PercIBS / 100.0
			p.CreditoCBS = baseCredito * p.PercCBS / 100.0
			p.SaldoIBS = p.DebitoIBS - p.CreditoIBS
			p.SaldoCBS = p.DebitoCBS - p.CreditoCBS
			projecao = append(projecao, p)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"base_debito_ibs_cbs":  baseDebito,
			"base_credito_ibs_cbs": baseCredito,
			"projecao":             projecao,
		})
	}
}
