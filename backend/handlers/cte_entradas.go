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
// CteEntradasCompetenciasHandler — GET /api/cte-entradas/competencias
// ---------------------------------------------------------------------------

func CteEntradasCompetenciasHandler(db *sql.DB) http.HandlerFunc {
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
		rows, err := db.Query(`SELECT mes_ano FROM (SELECT DISTINCT mes_ano FROM cte_entradas WHERE company_id = $1 AND mes_ano IS NOT NULL AND mes_ano != '') sub ORDER BY SPLIT_PART(mes_ano,'/',2) DESC, SPLIT_PART(mes_ano,'/',1) DESC`, companyID)
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
// CteEntradasListHandler — GET /api/cte-entradas
// ---------------------------------------------------------------------------

type cteRow struct {
	ID              string  `json:"id"`
	ChaveCTe        string  `json:"chave_cte"`
	Modelo          int     `json:"modelo"`
	Serie           string  `json:"serie"`
	NumeroCTe       string  `json:"numero_cte"`
	DataEmissao     string  `json:"data_emissao"`
	DataAutorizacao string  `json:"data_autorizacao"`
	MesAno          string  `json:"mes_ano"`
	EmitCNPJ        string  `json:"emit_cnpj"`
	EmitNome        string  `json:"emit_nome"`
	EmitUF          string  `json:"emit_uf"`
	RemNome         string  `json:"rem_nome"`
	DestCNPJCPF     string  `json:"dest_cnpj_cpf"`
	DestNome        string  `json:"dest_nome"`
	Modal           string  `json:"modal"`
	VPrest          float64 `json:"v_prest"`
	VBcIbsCbs       float64 `json:"v_bc_ibs_cbs"`
	VIbsUf          float64 `json:"v_ibs_uf"`
	VIbsMun         float64 `json:"v_ibs_mun"`
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

func CteEntradasListHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[CTeEntradas]")
			return
		}

		q := r.URL.Query()
		mesAno   := q.Get("mes_ano")
		emitCNPJ := q.Get("emit_cnpj")
		modelo   := q.Get("modelo")
		destCNPJ := q.Get("dest_cnpj")
		dataDe   := q.Get("data_de")
		dataAte  := q.Get("data_ate")

		safeColsCte := map[string]string{
			"data_emissao": "data_emissao",
			"v_prest":      "v_prest",
			"v_ibs":        "v_ibs",
			"v_cbs":        "v_cbs",
		}
		sortCol := "data_emissao"
		if c, ok := safeColsCte[q.Get("sort_by")]; ok { sortCol = c }
		sortDir := "DESC"
		if q.Get("sort_dir") == "asc" { sortDir = "ASC" }

		page, pageSize := 1, 100
		if p, e := strconv.Atoi(q.Get("page")); e == nil && p > 0 { page = p }
		if ps, e := strconv.Atoi(q.Get("page_size")); e == nil && ps > 0 && ps <= 500 { pageSize = ps }

		args := []interface{}{companyID}
		idx  := 2
		where := "WHERE company_id = $1"

		if mesAno   != "" { where += fmt.Sprintf(" AND mes_ano = $%d", idx);         args = append(args, mesAno);   idx++ }
		if emitCNPJ != "" { where += fmt.Sprintf(" AND emit_cnpj = $%d", idx);       args = append(args, emitCNPJ); idx++ }
		if modelo   != "" { where += fmt.Sprintf(" AND modelo = $%d", idx);          args = append(args, modelo);   idx++ }
		if dataDe   != "" { where += fmt.Sprintf(" AND data_emissao >= $%d", idx);   args = append(args, dataDe);   idx++ }
		if dataAte  != "" { where += fmt.Sprintf(" AND data_emissao <= $%d", idx);   args = append(args, dataAte);  idx++ }
		if destCNPJ != "" { where += fmt.Sprintf(" AND dest_cnpj_cpf = $%d", idx);  args = append(args, destCNPJ); idx++ }
		if q.Get("sem_ibs_cbs") == "true" { where += " AND (v_ibs = 0 AND v_cbs = 0)" }

		var total int
		if err := db.QueryRow("SELECT COUNT(*) FROM cte_entradas "+where, args...).Scan(&total); err != nil {
			log.Printf("CteEntradasList count error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}

		var totVPrest, totIBS, totCBS float64
		whereTotais := where + " AND COALESCE(cancelado,'N') != 'S'"
		db.QueryRow(
			"SELECT COALESCE(SUM(v_prest),0), COALESCE(SUM(v_ibs),0), COALESCE(SUM(v_cbs),0) FROM cte_entradas "+whereTotais,
			args...,
		).Scan(&totVPrest, &totIBS, &totCBS)

		offset := (page - 1) * pageSize
		selectQ := `
			SELECT
				ce.id, ce.chave_cte, ce.modelo, ce.serie, ce.numero_cte,
				TO_CHAR(ce.data_emissao, 'DD/MM/YYYY'),
				COALESCE(TO_CHAR(ce.data_autorizacao, 'DD/MM/YYYY'),''),
				ce.mes_ano,
				ce.emit_cnpj,
				COALESCE(ce.emit_nome, (
					SELECT nome FROM parceiros
					WHERE company_id = ce.company_id AND cnpj = ce.emit_cnpj
					LIMIT 1
				), '') AS emit_nome,
				COALESCE(ce.emit_uf, '') AS emit_uf,
				COALESCE(ce.rem_nome, '') AS rem_nome,
				COALESCE(ce.dest_cnpj_cpf,''),
				COALESCE(ce.dest_nome, '') AS dest_nome,
				COALESCE(ce.modal, '') AS modal,
				ce.v_prest,
				ce.v_bc_ibs_cbs, ce.v_ibs_uf, ce.v_ibs_mun, ce.v_ibs, ce.v_cbs,
				COALESCE(ce.base_icms,0), COALESCE(ce.icms,0), COALESCE(ce.icms_st,0), COALESCE(ce.ipi,0),
				COALESCE(ce.base_pis,0), COALESCE(ce.pis,0), COALESCE(ce.base_cofins,0), COALESCE(ce.cofins,0),
				COALESCE(ce.base_partilha,0), COALESCE(ce.icms_partilha,0),
				COALESCE(ce.cancelado, 'N') AS cancelado
			FROM cte_entradas ce ` + where +
			fmt.Sprintf(" ORDER BY %s %s, numero_cte DESC LIMIT $%d OFFSET $%d", sortCol, sortDir, idx, idx+1)
		pageArgs := append(args, pageSize, offset)

		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			log.Printf("CteEntradasList error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		defer rows.Close()

		list := []cteRow{}
		for rows.Next() {
			var row cteRow
			if err := rows.Scan(
				&row.ID, &row.ChaveCTe, &row.Modelo, &row.Serie, &row.NumeroCTe,
				&row.DataEmissao, &row.DataAutorizacao, &row.MesAno,
				&row.EmitCNPJ, &row.EmitNome, &row.EmitUF, &row.RemNome, &row.DestCNPJCPF, &row.DestNome, &row.Modal,
				&row.VPrest,
				&row.VBcIbsCbs, &row.VIbsUf, &row.VIbsMun, &row.VIBS, &row.VCBS,
				&row.BaseIcms, &row.Icms, &row.IcmsSt, &row.Ipi,
				&row.BasePis, &row.Pis, &row.BaseCofins, &row.Cofins,
				&row.BasePartilha, &row.IcmsPartilha,
				&row.Cancelado,
			); err != nil {
				log.Printf("CteEntradasList scan error: %v", err)
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
				"v_prest": totVPrest,
				"v_ibs":   totIBS,
				"v_cbs":   totCBS,
			},
			"items": list,
		})
	}
}
