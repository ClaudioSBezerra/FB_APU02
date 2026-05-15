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

// ---------------------------------------------------------------------------
// NfeEntradasCompetenciasHandler — GET /api/nfe-entradas/competencias
// ---------------------------------------------------------------------------

func NfeEntradasCompetenciasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, claims["user_id"].(string), r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}
		rows, err := db.Query(`SELECT mes_ano FROM (SELECT DISTINCT mes_ano FROM nfe_entradas WHERE company_id = $1 AND mes_ano IS NOT NULL AND mes_ano != '') sub ORDER BY SPLIT_PART(mes_ano,'/',2) DESC, SPLIT_PART(mes_ano,'/',1) DESC`, companyID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		defer rows.Close()
		meses := []string{}
		for rows.Next() {
			var m string
			if rows.Scan(&m) == nil {
				meses = append(meses, m)
			}
		}
		json.NewEncoder(w).Encode(meses)
	}
}

// ---------------------------------------------------------------------------
// NfeEntradasListHandler — GET /api/nfe-entradas
// ---------------------------------------------------------------------------

type nfeEntradaRow struct {
	ID              string  `json:"id"`
	ChaveNFe        string  `json:"chave_nfe"`
	Modelo          int     `json:"modelo"`
	Serie           string  `json:"serie"`
	NumeroNFe       string  `json:"numero_nfe"`
	DataEmissao     string  `json:"data_emissao"`
	DataAutorizacao string  `json:"data_autorizacao"`
	MesAno          string  `json:"mes_ano"`
	FornCNPJ        string  `json:"forn_cnpj"`
	FornNome        string  `json:"forn_nome"`
	FornUF          string  `json:"forn_uf"`
	DestCNPJCPF     string  `json:"dest_cnpj_cpf"`
	DestNome        string  `json:"dest_nome"`
	DestUF          string  `json:"dest_uf"`
	VNF             float64 `json:"v_nf"`
	VBCIbsCbs       float64 `json:"v_bc_ibs_cbs"`
	VIBSuf          float64 `json:"v_ibs_uf"`
	VIBSMun         float64 `json:"v_ibs_mun"`
	VIBS            float64 `json:"v_ibs"`
	VCBS            float64 `json:"v_cbs"`
	BaseIcms        float64 `json:"base_icms"`
	Icms            float64 `json:"icms"`
	IcmsSt          float64 `json:"icms_st"`
	Ipi             float64 `json:"ipi"`
	BasePis         float64 `json:"base_pis"`
	Pis             float64 `json:"pis"`
	BaseCofins      float64 `json:"base_cofins"`
	Cofins          float64 `json:"cofins"`
	BasePartilha    float64 `json:"base_partilha"`
	IcmsPartilha    float64 `json:"icms_partilha"`
	Cancelado       string  `json:"cancelado"` // "S" ou "N"
}

func NfeEntradasListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[NFeEntradas]")
			return
		}

		q := r.URL.Query()
		mesAno   := q.Get("mes_ano")
		fornCNPJ := q.Get("forn_cnpj")
		modelo   := q.Get("modelo")
		destCNPJ := q.Get("dest_cnpj")
		dataDe   := q.Get("data_de")
		dataAte  := q.Get("data_ate")

		safeColsEntrada := map[string]string{
			"data_emissao": "data_emissao",
			"v_nf":         "v_nf",
			"v_ibs":        "v_ibs",
			"v_cbs":        "v_cbs",
		}
		sortCol := "data_emissao"
		if c, ok := safeColsEntrada[q.Get("sort_by")]; ok { sortCol = c }
		sortDir := "DESC"
		if q.Get("sort_dir") == "asc" { sortDir = "ASC" }

		page, pageSize := 1, 100
		if p, e := strconv.Atoi(q.Get("page")); e == nil && p > 0 { page = p }
		if ps, e := strconv.Atoi(q.Get("page_size")); e == nil && ps > 0 && ps <= 500 { pageSize = ps }

		args := []interface{}{companyID}
		idx  := 2
		where := "WHERE company_id = $1"

		if mesAno   != "" { where += fmt.Sprintf(" AND mes_ano = $%d", idx);         args = append(args, mesAno);   idx++ }
		if fornCNPJ != "" { where += fmt.Sprintf(" AND forn_cnpj = $%d", idx);       args = append(args, fornCNPJ); idx++ }
		if modelo   != "" { where += fmt.Sprintf(" AND modelo = $%d", idx);          args = append(args, modelo);   idx++ }
		if dataDe   != "" { where += fmt.Sprintf(" AND data_emissao >= $%d", idx);   args = append(args, dataDe);   idx++ }
		if dataAte  != "" { where += fmt.Sprintf(" AND data_emissao <= $%d", idx);   args = append(args, dataAte);  idx++ }
		if destCNPJ != "" { where += fmt.Sprintf(" AND dest_cnpj_cpf = $%d", idx);  args = append(args, destCNPJ); idx++ }
		if q.Get("sem_ibs_cbs") == "true" { where += " AND (v_ibs = 0 AND v_cbs = 0)" }

		var total int
		if err := db.QueryRow("SELECT COUNT(*) FROM nfe_entradas "+where, args...).Scan(&total); err != nil {
			log.Printf("NfeEntradasList count error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}

		var totVNF, totIBS, totCBS float64
		whereTotais := where + " AND COALESCE(cancelado,'N') != 'S'"
		db.QueryRow(
			"SELECT COALESCE(SUM(v_nf),0), COALESCE(SUM(v_ibs),0), COALESCE(SUM(v_cbs),0) FROM nfe_entradas "+whereTotais,
			args...,
		).Scan(&totVNF, &totIBS, &totCBS)

		offset := (page - 1) * pageSize
		selectQ := `
			SELECT
				ne.id, ne.chave_nfe, ne.modelo, ne.serie, ne.numero_nfe,
				TO_CHAR(ne.data_emissao, 'DD/MM/YYYY'),
				COALESCE(TO_CHAR(ne.data_autorizacao, 'DD/MM/YYYY'),''),
				ne.mes_ano,
				ne.forn_cnpj,
				COALESCE(ne.forn_nome, (
					SELECT nome FROM parceiros
					WHERE company_id = ne.company_id AND cnpj = ne.forn_cnpj
					LIMIT 1
				), '') AS forn_nome,
				COALESCE(ne.forn_uf, '') AS forn_uf,
				COALESCE(ne.dest_cnpj_cpf,''),
				COALESCE(ne.dest_nome, '') AS dest_nome,
				COALESCE(ne.dest_uf, '') AS dest_uf,
				ne.v_nf,
				ne.v_bc_ibs_cbs, ne.v_ibs_uf, ne.v_ibs_mun, ne.v_ibs, ne.v_cbs,
				COALESCE(ne.base_icms,0), COALESCE(ne.icms,0), COALESCE(ne.icms_st,0), COALESCE(ne.ipi,0),
				COALESCE(ne.base_pis,0), COALESCE(ne.pis,0), COALESCE(ne.base_cofins,0), COALESCE(ne.cofins,0),
				COALESCE(ne.base_partilha,0), COALESCE(ne.icms_partilha,0),
				COALESCE(ne.cancelado, 'N') AS cancelado
			FROM nfe_entradas ne ` + where +
			fmt.Sprintf(" ORDER BY %s %s, numero_nfe DESC LIMIT $%d OFFSET $%d", sortCol, sortDir, idx, idx+1)
		pageArgs := append(args, pageSize, offset)

		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			log.Printf("NfeEntradasList error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		defer rows.Close()

		list := []nfeEntradaRow{}
		for rows.Next() {
			var row nfeEntradaRow
			if err := rows.Scan(
				&row.ID, &row.ChaveNFe, &row.Modelo, &row.Serie, &row.NumeroNFe,
				&row.DataEmissao, &row.DataAutorizacao, &row.MesAno,
				&row.FornCNPJ, &row.FornNome, &row.FornUF, &row.DestCNPJCPF, &row.DestNome, &row.DestUF,
				&row.VNF,
				&row.VBCIbsCbs, &row.VIBSuf, &row.VIBSMun, &row.VIBS, &row.VCBS,
				&row.BaseIcms, &row.Icms, &row.IcmsSt, &row.Ipi,
				&row.BasePis, &row.Pis, &row.BaseCofins, &row.Cofins,
				&row.BasePartilha, &row.IcmsPartilha,
				&row.Cancelado,
			); err != nil {
				log.Printf("NfeEntradasList scan error: %v", err)
				continue
			}
			list = append(list, row)
		}

		totalPages := (total + pageSize - 1) / pageSize
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
			"totals": map[string]float64{
				"v_nf":  totVNF,
				"v_ibs": totIBS,
				"v_cbs": totCBS,
			},
			"items": list,
		})
	}
}
