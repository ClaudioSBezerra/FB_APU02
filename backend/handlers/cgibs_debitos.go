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

// CGIBSDebitoRow representa um débito IBS calculado internamente a partir de nfe_saidas.
type CGIBSDebitoRow struct {
	ID              string  `json:"id"`
	ChaveNFe        string  `json:"chave_nfe"`
	Modelo          int     `json:"modelo"`
	Serie           string  `json:"serie"`
	NumeroNFe       string  `json:"numero_nfe"`
	DataEmissao     *string `json:"data_emissao"`
	MesAno          string  `json:"mes_ano"`
	EmitCNPJ        string  `json:"emit_cnpj"`
	DestCNPJCPF     string  `json:"dest_cnpj_cpf"`
	ValorNF         float64 `json:"valor_nf"`
	ValorIBSUF      float64 `json:"valor_ibs_uf"`
	ValorIBSMun     float64 `json:"valor_ibs_mun"`
	ValorIBSTotal   float64 `json:"valor_ibs_total"`
	Cancelado       string  `json:"cancelado"`
}

type CGIBSResumoAgregado struct {
	TotalDocumentos int     `json:"total_documentos"`
	ValorIBSTotal   float64 `json:"valor_ibs_total"`
	ValorIBSUF      float64 `json:"valor_ibs_uf"`
	ValorIBSMun     float64 `json:"valor_ibs_mun"`
}

// ListarDebitosIBSHandler — GET /api/cgibs/debitos
// Retorna NF-e de saída com valores IBS calculados internamente (fonte: nfe_saidas).
func ListarDebitosIBSHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSDebitos]")
			return
		}

		qp := r.URL.Query()
		mesAno := qp.Get("mes_ano")
		filterChave := qp.Get("chave")
		filterEmit := qp.Get("emit_cnpj")

		page := 1
		pageSize := 100
		if p, e := strconv.Atoi(qp.Get("page")); e == nil && p > 0 {
			page = p
		}
		if ps, e := strconv.Atoi(qp.Get("page_size")); e == nil && ps > 0 && ps <= 500 {
			pageSize = ps
		}

		args := []interface{}{companyID}
		idx := 2
		where := "WHERE company_id = $1 AND COALESCE(v_ibs, 0) > 0 AND COALESCE(cancelado,'N') != 'S'"

		if mesAno != "" {
			where += fmt.Sprintf(" AND mes_ano = $%d", idx)
			args = append(args, mesAno)
			idx++
		}
		if filterChave != "" {
			where += fmt.Sprintf(" AND chave_nfe ILIKE $%d", idx)
			args = append(args, "%"+filterChave+"%")
			idx++
		}
		if filterEmit != "" {
			where += fmt.Sprintf(" AND emit_cnpj = $%d", idx)
			args = append(args, filterEmit)
			idx++
		}

		var resumo CGIBSResumoAgregado
		sumQ := fmt.Sprintf(`
			SELECT COUNT(*),
			       COALESCE(SUM(v_ibs),0),
			       COALESCE(SUM(v_ibs_uf),0),
			       COALESCE(SUM(v_ibs_mun),0)
			FROM nfe_saidas %s`, where)
		if err := db.QueryRow(sumQ, args...).Scan(
			&resumo.TotalDocumentos,
			&resumo.ValorIBSTotal, &resumo.ValorIBSUF, &resumo.ValorIBSMun,
		); err != nil {
			log.Printf("[CGIBS Debitos] aggregate error: %v", err)
		}

		totalPages := (resumo.TotalDocumentos + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		offset := (page - 1) * pageSize

		selectQ := fmt.Sprintf(`
			SELECT id, chave_nfe, COALESCE(modelo,55), COALESCE(serie,''), COALESCE(numero_nfe,''),
			       CASE WHEN data_emissao IS NOT NULL THEN TO_CHAR(data_emissao, 'YYYY-MM-DD') END,
			       COALESCE(mes_ano,''), COALESCE(emit_cnpj,''), COALESCE(dest_cnpj_cpf,''),
			       COALESCE(v_nf,0), COALESCE(v_ibs_uf,0), COALESCE(v_ibs_mun,0), COALESCE(v_ibs,0),
			       COALESCE(cancelado,'N')
			FROM nfe_saidas %s
			ORDER BY data_emissao DESC NULLS LAST, chave_nfe
			LIMIT $%d OFFSET $%d`, where, idx, idx+1)

		pageArgs := append(args, pageSize, offset)
		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar débitos IBS", err, "[CGIBSDebitos]")
			return
		}
		defer rows.Close()

		var debitos []CGIBSDebitoRow
		for rows.Next() {
			var d CGIBSDebitoRow
			if err := rows.Scan(
				&d.ID, &d.ChaveNFe, &d.Modelo, &d.Serie, &d.NumeroNFe,
				&d.DataEmissao, &d.MesAno, &d.EmitCNPJ, &d.DestCNPJCPF,
				&d.ValorNF, &d.ValorIBSUF, &d.ValorIBSMun, &d.ValorIBSTotal,
				&d.Cancelado,
			); err != nil {
				log.Printf("[CGIBS Debitos] scan error: %v", err)
				continue
			}
			debitos = append(debitos, d)
		}
		if debitos == nil {
			debitos = []CGIBSDebitoRow{}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"debitos": debitos,
			"resumo":  resumo,
			"fonte":   "interno",
			"pagination": map[string]int{
				"page":        page,
				"page_size":   pageSize,
				"total":       resumo.TotalDocumentos,
				"total_pages": totalPages,
			},
		})
	}
}

// PeriodosDebitosIBSHandler — GET /api/cgibs/debitos/periodos
func PeriodosDebitosIBSHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSDebitos]")
			return
		}

		rows, err := db.Query(`
			SELECT DISTINCT mes_ano
			FROM nfe_saidas
			WHERE company_id=$1
			  AND mes_ano IS NOT NULL AND mes_ano != ''
			  AND COALESCE(v_ibs,0) > 0
			  AND SPLIT_PART(mes_ano,'/',2) >= '2025'
			ORDER BY mes_ano DESC
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar períodos de débitos IBS", err, "[CGIBSDebitos]")
			return
		}
		defer rows.Close()

		var periodos []string
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil {
				periodos = append(periodos, p)
			}
		}
		if periodos == nil {
			periodos = []string{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"periodos": periodos})
	}
}
