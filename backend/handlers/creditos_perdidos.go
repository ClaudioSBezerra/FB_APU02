package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Tipos de retorno
// ---------------------------------------------------------------------------

type credPerdAliquota struct {
	Ano float64 `json:"ano"`
	IBS float64 `json:"ibs"`
	CBS float64 `json:"cbs"`
}

type credPerdFornecedor struct {
	FornCNPJ   string  `json:"forn_cnpj"`
	FornNome   string  `json:"forn_nome"`
	QtdNotas   int     `json:"qtd_notas"`
	ValorTotal float64 `json:"valor_total"`
	IBSEstimado float64 `json:"ibs_estimado"`
	CBSEstimado float64 `json:"cbs_estimado"`
	TotalEstimado float64 `json:"total_estimado"`
}

type credPerdSimplesForn struct {
	FornCNPJ   string  `json:"forn_cnpj"`
	FornNome   string  `json:"forn_nome"`
	ValorTotal float64 `json:"valor_total"`
	IBSPerdido float64 `json:"ibs_perdido"`
	CBSPerdido float64 `json:"cbs_perdido"`
	TotalPerdido float64 `json:"total_perdido"`
}

type credPerdNFe struct {
	TotalNotas     int                  `json:"total_notas"`
	TotalUniverse  int                  `json:"total_universe"`
	PercSemCredito float64              `json:"perc_sem_credito"`
	ValorTotal     float64              `json:"valor_total"`
	IBSEstimado    float64              `json:"ibs_estimado"`
	CBSEstimado    float64              `json:"cbs_estimado"`
	TotalEstimado  float64              `json:"total_estimado"`
	PorFornecedor  []credPerdFornecedor `json:"por_fornecedor"`
}

type credPerdSimples struct {
	TotalFornecedores int                   `json:"total_fornecedores"`
	ValorTotal        float64               `json:"valor_total"`
	IBSPerdido        float64               `json:"ibs_perdido"`
	CBSPerdido        float64               `json:"cbs_perdido"`
	TotalPerdido      float64               `json:"total_perdido"`
	PorFornecedor     []credPerdSimplesForn `json:"por_fornecedor"`
}

type credPerdTransportadora struct {
	EmitCNPJ      string  `json:"emit_cnpj"`
	EmitNome      string  `json:"emit_nome"`
	QtdCTes       int     `json:"qtd_ctes"`
	ValorTotal    float64 `json:"valor_total"`
	IBSEstimado   float64 `json:"ibs_estimado"`
	CBSEstimado   float64 `json:"cbs_estimado"`
	TotalEstimado float64 `json:"total_estimado"`
}

type credPerdCTe struct {
	TotalCTes         int                      `json:"total_ctes"`
	TotalUniverse     int                      `json:"total_universe"`
	PercSemCredito    float64                  `json:"perc_sem_credito"`
	ValorTotal        float64                  `json:"valor_total"`
	IBSEstimado       float64                  `json:"ibs_estimado"`
	CBSEstimado       float64                  `json:"cbs_estimado"`
	TotalEstimado     float64                  `json:"total_estimado"`
	PorTransportadora []credPerdTransportadora `json:"por_transportadora"`
}

type CreditosPerdidosResponse struct {
	Aliquotas           credPerdAliquota `json:"aliquotas"`
	NFeSemCredito       credPerdNFe      `json:"nfe_sem_credito"`
	SimplesNacional     credPerdSimples  `json:"simples_nacional"`
	CteSemCredito       credPerdCTe      `json:"cte_sem_credito"`
	TotalCreditoEmRisco float64          `json:"total_credito_em_risco"`
}

// ---------------------------------------------------------------------------
// CreditosPerdidosHandler — GET /api/apuracao/creditos-perdidos
// ---------------------------------------------------------------------------
func CreditosPerdidosHandler(db *sql.DB) http.HandlerFunc {
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

		// Alíquotas 2033
		var ibsRate, cbsRate float64
		err = db.QueryRow(`
			SELECT perc_ibs_uf + perc_ibs_mun, perc_cbs
			FROM tabela_aliquotas WHERE ano = 2033 LIMIT 1
		`).Scan(&ibsRate, &cbsRate)
		if err != nil {
			ibsRate = 17.7
			cbsRate = 8.8
		}

		resp := CreditosPerdidosResponse{
			Aliquotas: credPerdAliquota{Ano: 2033, IBS: ibsRate, CBS: cbsRate},
		}

		// ── 1. NF-e sem IBS/CBS ──────────────────────────────────────────────
		// Exclui transferências internas: mesma raiz CNPJ (8 primeiros dígitos)
		// cobre transferências entre filiais, uso e consumo e ativo imobilizado.

		// Total de notas de terceiros (excluindo intra-grupo)
		var totalUniverse int
		db.QueryRow(`
			SELECT COUNT(*)
			FROM nfe_entradas
			WHERE company_id = $1
			  AND LEFT(forn_cnpj, 8) != LEFT(dest_cnpj_cpf, 8)
		`, companyID).Scan(&totalUniverse)

		// Notas sem IBS/CBS de terceiros, agrupadas por fornecedor
		rows, err := db.Query(`
			SELECT
				forn_cnpj,
				COALESCE(forn_nome, ''),
				COUNT(*)          AS qtd_notas,
				SUM(v_nf)         AS valor_total
			FROM nfe_entradas
			WHERE company_id = $1
			  AND v_ibs = 0
			  AND v_cbs = 0
			  AND LEFT(forn_cnpj, 8) != LEFT(dest_cnpj_cpf, 8)
			GROUP BY forn_cnpj, forn_nome
			ORDER BY valor_total DESC
			LIMIT 50
		`, companyID)
		if err != nil {
			log.Printf("CreditosPerdidos nfe query error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar NF-e")
			return
		}
		defer rows.Close()

		var nfeFornList []credPerdFornecedor
		var nfeTotalNotas int
		var nfeValorTotal float64

		for rows.Next() {
			var f credPerdFornecedor
			if err := rows.Scan(&f.FornCNPJ, &f.FornNome, &f.QtdNotas, &f.ValorTotal); err != nil {
				continue
			}
			f.IBSEstimado = f.ValorTotal * (ibsRate / 100.0)
			f.CBSEstimado = f.ValorTotal * (cbsRate / 100.0)
			f.TotalEstimado = f.IBSEstimado + f.CBSEstimado
			nfeFornList = append(nfeFornList, f)
			nfeTotalNotas += f.QtdNotas
			nfeValorTotal += f.ValorTotal
		}

		if nfeFornList == nil {
			nfeFornList = []credPerdFornecedor{}
		}

		percSem := 0.0
		if totalUniverse > 0 {
			percSem = float64(nfeTotalNotas) / float64(totalUniverse) * 100.0
		}
		nfeIBS := nfeValorTotal * (ibsRate / 100.0)
		nfeCBS := nfeValorTotal * (cbsRate / 100.0)

		resp.NFeSemCredito = credPerdNFe{
			TotalNotas:     nfeTotalNotas,
			TotalUniverse:  totalUniverse,
			PercSemCredito: percSem,
			ValorTotal:     nfeValorTotal,
			IBSEstimado:    nfeIBS,
			CBSEstimado:    nfeCBS,
			TotalEstimado:  nfeIBS + nfeCBS,
			PorFornecedor:  nfeFornList,
		}

		// ── 2. Simples Nacional (EFD) ────────────────────────────────────────
		simplesRows, err := db.Query(`
			SELECT
				fornecedor_cnpj,
				fornecedor_nome,
				SUM(total_valor) AS valor_total
			FROM mv_operacoes_simples
			WHERE company_id = $1
			GROUP BY fornecedor_cnpj, fornecedor_nome
			ORDER BY valor_total DESC
			LIMIT 50
		`, companyID)
		if err != nil {
			log.Printf("CreditosPerdidos simples query error: %v", err)
			// Não aborta — retorna sem dados do Simples
		}

		var simplesFornList []credPerdSimplesForn
		var simplesTotalValor, simplesIBS, simplesCBS float64

		if simplesRows != nil {
			defer simplesRows.Close()
			for simplesRows.Next() {
				var f credPerdSimplesForn
				if err := simplesRows.Scan(&f.FornCNPJ, &f.FornNome, &f.ValorTotal); err != nil {
					continue
				}
				f.IBSPerdido = f.ValorTotal * (ibsRate / 100.0)
				f.CBSPerdido = f.ValorTotal * (cbsRate / 100.0)
				f.TotalPerdido = f.IBSPerdido + f.CBSPerdido
				simplesFornList = append(simplesFornList, f)
				simplesTotalValor += f.ValorTotal
				simplesIBS += f.IBSPerdido
				simplesCBS += f.CBSPerdido
			}
		}
		if simplesFornList == nil {
			simplesFornList = []credPerdSimplesForn{}
		}

		resp.SimplesNacional = credPerdSimples{
			TotalFornecedores: len(simplesFornList),
			ValorTotal:        simplesTotalValor,
			IBSPerdido:        simplesIBS,
			CBSPerdido:        simplesCBS,
			TotalPerdido:      simplesIBS + simplesCBS,
			PorFornecedor:     simplesFornList,
		}

		// ── 3. CT-e sem IBS/CBS ──────────────────────────────────────────────
		var cteTotalUniverse int
		db.QueryRow(`SELECT COUNT(*) FROM cte_entradas WHERE company_id = $1`, companyID).Scan(&cteTotalUniverse)

		cteRows, err := db.Query(`
			SELECT
				emit_cnpj,
				COALESCE(emit_nome, ''),
				COUNT(*)        AS qtd_ctes,
				SUM(v_prest)    AS valor_total
			FROM cte_entradas
			WHERE company_id = $1
			  AND (v_ibs IS NULL OR v_ibs = 0)
			  AND (v_cbs IS NULL OR v_cbs = 0)
			GROUP BY emit_cnpj, emit_nome
			ORDER BY valor_total DESC
			LIMIT 50
		`, companyID)
		if err != nil {
			log.Printf("CreditosPerdidos cte query error: %v", err)
		}

		var cteTranspList []credPerdTransportadora
		var cteTotalCTes int
		var cteValorTotal float64

		if cteRows != nil {
			defer cteRows.Close()
			for cteRows.Next() {
				var t credPerdTransportadora
				if err := cteRows.Scan(&t.EmitCNPJ, &t.EmitNome, &t.QtdCTes, &t.ValorTotal); err != nil {
					continue
				}
				t.IBSEstimado = t.ValorTotal * (ibsRate / 100.0)
				t.CBSEstimado = t.ValorTotal * (cbsRate / 100.0)
				t.TotalEstimado = t.IBSEstimado + t.CBSEstimado
				cteTranspList = append(cteTranspList, t)
				cteTotalCTes += t.QtdCTes
				cteValorTotal += t.ValorTotal
			}
		}
		if cteTranspList == nil {
			cteTranspList = []credPerdTransportadora{}
		}

		ctePercSem := 0.0
		if cteTotalUniverse > 0 {
			ctePercSem = float64(cteTotalCTes) / float64(cteTotalUniverse) * 100.0
		}
		cteIBS := cteValorTotal * (ibsRate / 100.0)
		cteCBS := cteValorTotal * (cbsRate / 100.0)

		resp.CteSemCredito = credPerdCTe{
			TotalCTes:         cteTotalCTes,
			TotalUniverse:     cteTotalUniverse,
			PercSemCredito:    ctePercSem,
			ValorTotal:        cteValorTotal,
			IBSEstimado:       cteIBS,
			CBSEstimado:       cteCBS,
			TotalEstimado:     cteIBS + cteCBS,
			PorTransportadora: cteTranspList,
		}

		// ── Total combinado ──────────────────────────────────────────────────
		resp.TotalCreditoEmRisco = resp.NFeSemCredito.TotalEstimado +
			resp.SimplesNacional.TotalPerdido +
			resp.CteSemCredito.TotalEstimado

		json.NewEncoder(w).Encode(resp)
	}
}
