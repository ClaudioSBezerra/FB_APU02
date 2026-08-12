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
// NfeSaidasListHandler — GET /api/nfe-saidas
// ---------------------------------------------------------------------------

type nfeSaidaRow struct {
	ID                       string  `json:"id"`
	ChaveNFe                 string  `json:"chave_nfe"`
	Modelo                   int     `json:"modelo"`
	Serie                    string  `json:"serie"`
	NumeroNFe                string  `json:"numero_nfe"`
	DataEmissao              string  `json:"data_emissao"`
	DataAutorizacao          string  `json:"data_autorizacao"`
	MesAno                   string  `json:"mes_ano"`
	EmitCNPJ                 string  `json:"emit_cnpj"`
	EmitNome                 string  `json:"emit_nome"`
	EmitUF                   string  `json:"emit_uf"`
	DestCNPJCPF              string  `json:"dest_cnpj_cpf"`
	DestNome                 string  `json:"dest_nome"`
	DestUF                   string  `json:"dest_uf"`
	VNF                      float64 `json:"v_nf"`
	VBCIbsCbs                float64 `json:"v_bc_ibs_cbs"`
	VIBSuf                   float64 `json:"v_ibs_uf"`
	VIBSMun                  float64 `json:"v_ibs_mun"`
	VIBS                     float64 `json:"v_ibs"`
	VCBS                     float64 `json:"v_cbs"`
	BaseIcms                 float64 `json:"base_icms"`
	Icms                     float64 `json:"icms"`
	IcmsSt                   float64 `json:"icms_st"`
	Ipi                      float64 `json:"ipi"`
	BasePis                  float64 `json:"base_pis"`
	Pis                      float64 `json:"pis"`
	BaseCofins               float64 `json:"base_cofins"`
	Cofins                   float64 `json:"cofins"`
	BasePartilha             float64 `json:"base_partilha"`
	IcmsPartilha             float64 `json:"icms_partilha"`
	Cancelado                string  `json:"cancelado"`                   // "S" ou "N"
	QtdeParcelasSplitPayment int     `json:"qtde_parcelas_split_payment"` // rfb_debitos_liquidacoes (migration 126) — 0 até a fonte do XML (Grupo Y) ser implementada
}

// ---------------------------------------------------------------------------
// NfeSaidasCompetenciasHandler — GET /api/nfe-saidas/competencias
// ---------------------------------------------------------------------------

func NfeSaidasCompetenciasHandler(db *sql.DB) http.HandlerFunc {
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
		rows, err := db.Query(`SELECT mes_ano FROM (SELECT DISTINCT mes_ano FROM nfe_saidas WHERE company_id = $1 AND mes_ano IS NOT NULL AND mes_ano != '') sub ORDER BY SPLIT_PART(mes_ano,'/',2) DESC, SPLIT_PART(mes_ano,'/',1) DESC`, companyID)
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

func NfeSaidasListHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[NFeSaidas]")
			return
		}

		q := r.URL.Query()
		mesAno := q.Get("mes_ano")
		emitCNPJ := q.Get("emit_cnpj")
		modelo := q.Get("modelo")
		destCNPJ := q.Get("dest_cnpj")
		dataDe := q.Get("data_de")
		dataAte := q.Get("data_ate")

		safeColsSaida := map[string]string{
			"data_emissao": "data_emissao",
			"v_nf":         "v_nf",
			"v_ibs":        "v_ibs",
			"v_cbs":        "v_cbs",
		}
		sortCol := "data_emissao"
		if c, ok := safeColsSaida[q.Get("sort_by")]; ok {
			sortCol = c
		}
		sortDir := "DESC"
		if q.Get("sort_dir") == "asc" {
			sortDir = "ASC"
		}

		page, pageSize := 1, 100
		if p, e := strconv.Atoi(q.Get("page")); e == nil && p > 0 {
			page = p
		}
		if ps, e := strconv.Atoi(q.Get("page_size")); e == nil && ps > 0 && ps <= 500 {
			pageSize = ps
		}

		args := []interface{}{companyID}
		idx := 2
		where := "WHERE company_id = $1"

		if mesAno != "" {
			where += fmt.Sprintf(" AND mes_ano = $%d", idx)
			args = append(args, mesAno)
			idx++
		}
		if emitCNPJ != "" {
			where += fmt.Sprintf(" AND emit_cnpj = $%d", idx)
			args = append(args, emitCNPJ)
			idx++
		}
		if modelo != "" {
			where += fmt.Sprintf(" AND modelo = $%d", idx)
			args = append(args, modelo)
			idx++
		}
		if dataDe != "" {
			where += fmt.Sprintf(" AND data_emissao >= $%d", idx)
			args = append(args, dataDe)
			idx++
		}
		if dataAte != "" {
			where += fmt.Sprintf(" AND data_emissao <= $%d", idx)
			args = append(args, dataAte)
			idx++
		}
		if destCNPJ != "" {
			where += fmt.Sprintf(" AND dest_cnpj_cpf = $%d", idx)
			args = append(args, destCNPJ)
			idx++
		}

		var total int
		if err := db.QueryRow("SELECT COUNT(*) FROM nfe_saidas "+where, args...).Scan(&total); err != nil {
			log.Printf("NfeSaidasList count error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}

		var totVNF, totIBS, totCBS float64
		whereTotais := where + " AND COALESCE(cancelado,'N') != 'S'"
		db.QueryRow(
			"SELECT COALESCE(SUM(v_nf),0), COALESCE(SUM(v_ibs),0), COALESCE(SUM(v_cbs),0) FROM nfe_saidas "+whereTotais,
			args...,
		).Scan(&totVNF, &totIBS, &totCBS)

		offset := (page - 1) * pageSize
		selectQ := `
			SELECT
				ns.id, ns.chave_nfe, ns.modelo, ns.serie, ns.numero_nfe,
				TO_CHAR(ns.data_emissao, 'DD/MM/YYYY'),
				COALESCE(TO_CHAR(ns.data_autorizacao, 'DD/MM/YYYY'),''),
				ns.mes_ano,
				ns.emit_cnpj,
				COALESCE(ns.emit_nome, (
					SELECT nome FROM parceiros
					WHERE company_id = ns.company_id AND cnpj = ns.emit_cnpj
					LIMIT 1
				), '') AS emit_nome,
				COALESCE(ns.emit_uf, '') AS emit_uf,
				COALESCE(ns.dest_cnpj_cpf,''),
				COALESCE(ns.dest_nome, (
					SELECT nome FROM parceiros
					WHERE company_id = ns.company_id AND cnpj = ns.dest_cnpj_cpf
					LIMIT 1
				), '') AS dest_nome,
				COALESCE(ns.dest_uf, '') AS dest_uf,
				ns.v_nf,
				ns.v_bc_ibs_cbs, ns.v_ibs_uf, ns.v_ibs_mun, ns.v_ibs, ns.v_cbs,
				COALESCE(ns.base_icms,0), COALESCE(ns.icms,0), COALESCE(ns.icms_st,0), COALESCE(ns.ipi,0),
				COALESCE(ns.base_pis,0), COALESCE(ns.pis,0), COALESCE(ns.base_cofins,0), COALESCE(ns.cofins,0),
				COALESCE(ns.base_partilha,0), COALESCE(ns.icms_partilha,0),
				COALESCE(ns.cancelado, 'N') AS cancelado,
				(SELECT COUNT(*) FROM rfb_debitos_liquidacoes l
					WHERE l.company_id = ns.company_id AND l.chave_dfe = ns.chave_nfe) AS qtde_parcelas_split_payment
			FROM nfe_saidas ns ` + where +
			fmt.Sprintf(" ORDER BY %s %s, numero_nfe DESC LIMIT $%d OFFSET $%d", sortCol, sortDir, idx, idx+1)
		pageArgs := append(args, pageSize, offset)

		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			log.Printf("NfeSaidasList error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		defer rows.Close()

		list := []nfeSaidaRow{}
		for rows.Next() {
			var row nfeSaidaRow
			if err := rows.Scan(
				&row.ID, &row.ChaveNFe, &row.Modelo, &row.Serie, &row.NumeroNFe,
				&row.DataEmissao, &row.DataAutorizacao, &row.MesAno,
				&row.EmitCNPJ, &row.EmitNome, &row.EmitUF, &row.DestCNPJCPF, &row.DestNome, &row.DestUF,
				&row.VNF,
				&row.VBCIbsCbs, &row.VIBSuf, &row.VIBSMun, &row.VIBS, &row.VCBS,
				&row.BaseIcms, &row.Icms, &row.IcmsSt, &row.Ipi,
				&row.BasePis, &row.Pis, &row.BaseCofins, &row.Cofins,
				&row.BasePartilha, &row.IcmsPartilha,
				&row.Cancelado,
				&row.QtdeParcelasSplitPayment,
			); err != nil {
				log.Printf("NfeSaidasList scan error: %v", err)
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
