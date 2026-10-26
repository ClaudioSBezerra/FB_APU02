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
	// NFS-e (nfse_saidas = prestadas/débito, nfse_entradas = tomadas/crédito) — expostas
	// separadas das NF-e para a tela mostrar a origem; entram em DebitoGeral/CreditoGeral e
	// nos saldos.
	DebitoNfseUF     float64 `json:"debito_nfse_uf"`
	DebitoNfseMun    float64 `json:"debito_nfse_mun"`
	DebitoNfseTotal  float64 `json:"debito_nfse_total"`
	QtdNfsePrestadas int     `json:"qtd_nfse_prestadas"`
	CreditoNfseUF    float64 `json:"credito_nfse_uf"`
	CreditoNfseMun   float64 `json:"credito_nfse_mun"`
	CreditoNfseTotal float64 `json:"credito_nfse_total"`
	QtdNfseTomadas   int     `json:"qtd_nfse_tomadas"`
	DebitoGeral      float64 `json:"debito_geral"`  // NF-e + NFS-e
	CreditoGeral     float64 `json:"credito_geral"` // NF-e + CT-e + NFS-e
	SaldoUF          float64 `json:"saldo_uf"`
	SaldoMun         float64 `json:"saldo_mun"`
	SaldoTotal       float64 `json:"saldo_total"`
}

type apuracaoCBSResult struct {
	DebitoTotal      float64 `json:"debito_total"`
	QtdSaidas        int     `json:"qtd_saidas"`
	CreditoNfeTotal  float64 `json:"credito_nfe_total"`
	QtdEntradas      int     `json:"qtd_entradas"`
	CreditoCte       float64 `json:"credito_cte"`
	QtdCtes          int     `json:"qtd_ctes"`
	DebitoNfseTotal  float64 `json:"debito_nfse_total"`
	QtdNfsePrestadas int     `json:"qtd_nfse_prestadas"`
	CreditoNfseTotal float64 `json:"credito_nfse_total"`
	QtdNfseTomadas   int     `json:"qtd_nfse_tomadas"`
	DebitoGeral      float64 `json:"debito_geral"`  // NF-e + NFS-e
	CreditoGeral     float64 `json:"credito_geral"` // NF-e + CT-e + NFS-e
	SaldoTotal       float64 `json:"saldo_total"`
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[ApuracaoPainel]")
			return
		}

		// ── Meses disponíveis (union das 5 tabelas com dados reais) ──────────
		// NFS-e por competência (mes_ano), ignorando canceladas — mesma regra das somas.
		rows, err := db.Query(`
			SELECT mes_ano FROM (
				SELECT DISTINCT mes_ano FROM (
					SELECT mes_ano FROM nfe_saidas   WHERE company_id = $1
					UNION
					SELECT mes_ano FROM nfe_entradas WHERE company_id = $1
					UNION
					SELECT mes_ano FROM cte_entradas WHERE company_id = $1
					UNION
					SELECT mes_ano FROM nfse_saidas   WHERE company_id = $1 AND NOT cancelada
					UNION
					SELECT mes_ano FROM nfse_entradas WHERE company_id = $1 AND NOT cancelada
				) t
				WHERE mes_ano IS NOT NULL AND mes_ano != ''
				  AND SPLIT_PART(mes_ano, '/', 2) >= '2025'
			) u
			ORDER BY SPLIT_PART(mes_ano, '/', 2) DESC,
			         SPLIT_PART(mes_ano, '/', 1) DESC
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao listar períodos", err, "[ApuracaoPainel]")
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar saídas", err, "[ApuracaoPainel]")
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar entradas", err, "[ApuracaoPainel]")
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar CT-e", err, "[ApuracaoPainel]")
			return
		}

		// ── NFS-e (base própria: XMLs do ADN, nunca rfb_*/cgibs_*) ────────────
		// Prestadas = débito, tomadas = crédito. Colunas IBS/CBS da migration 135 (NULL =
		// grupo IBSCBS ausente no XML → soma 0). Canceladas ficam fora.
		var nfseSaida, nfseEntrada nfseApuracaoSoma
		if nfseSaida, err = somaNFSeApuracao(db, "nfse_saidas", companyID, mesAno); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar NFS-e prestadas", err, "[ApuracaoPainel]")
			return
		}
		if nfseEntrada, err = somaNFSeApuracao(db, "nfse_entradas", companyID, mesAno); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar NFS-e tomadas", err, "[ApuracaoPainel]")
			return
		}

		// ── Cálculo dos saldos ────────────────────────────────────────────────
		debitoGeralIBS := debitoIBS + nfseSaida.IBSTotal
		creditoGeralIBS := creditoNfeIBS + creditoCteIBS + nfseEntrada.IBSTotal
		debitoGeralCBS := debitoCBS + nfseSaida.CBS
		creditoGeralCBS := creditoNfeCBS + creditoCteCBS + nfseEntrada.CBS
		resp.IBS = apuracaoIBSResult{
			DebitoUF:         debitoIBSUF,
			DebitoMun:        debitoIBSMun,
			DebitoTotal:      debitoIBS,
			QtdSaidas:        qtdSaidas,
			CreditoNfeUF:     creditoNfeIBSUF,
			CreditoNfeMun:    creditoNfeIBSMun,
			CreditoNfeTotal:  creditoNfeIBS,
			QtdEntradas:      qtdEntradas,
			CreditoCte:       creditoCteIBS,
			QtdCtes:          qtdCtes,
			DebitoNfseUF:     nfseSaida.IBSUF,
			DebitoNfseMun:    nfseSaida.IBSMun,
			DebitoNfseTotal:  nfseSaida.IBSTotal,
			QtdNfsePrestadas: nfseSaida.Qtd,
			CreditoNfseUF:    nfseEntrada.IBSUF,
			CreditoNfseMun:   nfseEntrada.IBSMun,
			CreditoNfseTotal: nfseEntrada.IBSTotal,
			QtdNfseTomadas:   nfseEntrada.Qtd,
			DebitoGeral:      debitoGeralIBS,
			CreditoGeral:     creditoGeralIBS,
			SaldoUF:          (debitoIBSUF + nfseSaida.IBSUF) - (creditoNfeIBSUF + nfseEntrada.IBSUF),
			SaldoMun:         (debitoIBSMun + nfseSaida.IBSMun) - (creditoNfeIBSMun + nfseEntrada.IBSMun),
			SaldoTotal:       debitoGeralIBS - creditoGeralIBS,
		}
		resp.CBS = apuracaoCBSResult{
			DebitoTotal:      debitoCBS,
			QtdSaidas:        qtdSaidas,
			CreditoNfeTotal:  creditoNfeCBS,
			QtdEntradas:      qtdEntradas,
			CreditoCte:       creditoCteCBS,
			QtdCtes:          qtdCtes,
			DebitoNfseTotal:  nfseSaida.CBS,
			QtdNfsePrestadas: nfseSaida.Qtd,
			CreditoNfseTotal: nfseEntrada.CBS,
			QtdNfseTomadas:   nfseEntrada.Qtd,
			DebitoGeral:      debitoGeralCBS,
			CreditoGeral:     creditoGeralCBS,
			SaldoTotal:       debitoGeralCBS - creditoGeralCBS,
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}
}

// ---------------------------------------------------------------------------
// NFS-e — 4ª fonte da apuração própria
// ---------------------------------------------------------------------------

type nfseApuracaoSoma struct {
	IBSUF    float64
	IBSMun   float64
	IBSTotal float64
	CBS      float64
	Qtd      int
}

// somaNFSeApuracao soma IBS/CBS de nfse_saidas ou nfse_entradas na competência (mes_ano
// MM/YYYY), excluindo canceladas. table vem só de literais internos ("nfse_saidas" |
// "nfse_entradas"), nunca da requisição.
func somaNFSeApuracao(db *sql.DB, table, companyID, mesAno string) (nfseApuracaoSoma, error) {
	var s nfseApuracaoSoma
	err := db.QueryRow(`
		SELECT
			COALESCE(SUM(ibs_uf),    0),
			COALESCE(SUM(ibs_mun),   0),
			COALESCE(SUM(ibs_total), 0),
			COALESCE(SUM(cbs),       0),
			COUNT(*)
		FROM `+table+`
		WHERE company_id = $1 AND mes_ano = $2 AND NOT cancelada
	`, companyID, mesAno).Scan(&s.IBSUF, &s.IBSMun, &s.IBSTotal, &s.CBS, &s.Qtd)
	if err == sql.ErrNoRows {
		err = nil
	}
	return s, err
}
