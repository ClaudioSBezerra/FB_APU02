package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// nfseRow — linha de nfse_entradas/nfse_saidas (schema idêntico nas duas tabelas,
// migrations 133/134). Valores tributários vêm do XML da NFS-e (services/nfse_adn_xml.go).
// cancelada/numero_nfse/data_cancelamento (migration 134) foram ACRESCENTADOS ao contrato —
// nenhum campo anterior mudou. Notas canceladas aparecem na listagem (com cancelada=true) mas
// ficam FORA dos totais.
// ---------------------------------------------------------------------------

type nfseRow struct {
	ID                string  `json:"id"`
	IdentificadorNFSe string  `json:"identificador_nfse"`
	CNPJPrestador     string  `json:"cnpj_prestador"`
	CNPJTomador       string  `json:"cnpj_tomador"`
	DataEmissao       string  `json:"data_emissao"`
	MesAno            string  `json:"mes_ano"`
	ValorServicos     float64 `json:"valor_servicos"`
	BaseISS           float64 `json:"base_iss"`
	ISS               float64 `json:"iss"`
	AliquotaISS       float64 `json:"aliquota_iss"`
	ISSRetido         float64 `json:"iss_retido"`
	BasePIS           float64 `json:"base_pis"`
	PIS               float64 `json:"pis"`
	BaseCOFINS        float64 `json:"base_cofins"`
	COFINS            float64 `json:"cofins"`
	INSS              float64 `json:"inss"`
	IRRF              float64 `json:"irrf"`
	CSLL              float64 `json:"csll"`
	NumeroNFSe        string  `json:"numero_nfse"`
	Cancelada         bool    `json:"cancelada"`
	DataCancelamento  string  `json:"data_cancelamento"` // DD/MM/YYYY ou "" (mesmo formato de data_emissao)
}

// nfseSafeSortCols — colunas aceitas em sort_by, mesmo princípio de allowlist já usado em
// NfeEntradasListHandler/NfeSaidasListHandler (nunca interpolar sort_by direto na query).
var nfseSafeSortCols = map[string]string{
	"data_emissao":   "data_emissao",
	"valor_servicos": "valor_servicos",
	"iss":            "iss",
	"pis":            "pis",
	"cofins":         "cofins",
}

// nfseSanitizeFloat substitui NaN/Inf por 0 antes de serializar — colunas NUMERIC do Postgres
// podem conter NaN (valor válido na coluna), e encoding/json não sabe codificar NaN/Inf.
func nfseSanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// nfseCompetencias é o corpo compartilhado de NFSeEntradasCompetenciasHandler/
// NFSeSaidasCompetenciasHandler — mesmo padrão de NfeEntradasCompetenciasHandler, só troca a
// tabela consultada.
func nfseCompetencias(db *sql.DB, table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if table != "nfse_entradas" && table != "nfse_saidas" {
			log.Printf("[nfseCompetencias] tabela inválida: %q", table)
			jsonErr(w, http.StatusInternalServerError, "Erro interno")
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
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}
		query := fmt.Sprintf(`SELECT mes_ano FROM (SELECT DISTINCT mes_ano FROM %s WHERE company_id = $1 AND mes_ano IS NOT NULL AND mes_ano != '') sub ORDER BY SPLIT_PART(mes_ano,'/',2) DESC, SPLIT_PART(mes_ano,'/',1) DESC`, table)
		rows, err := db.Query(query, companyID)
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
// NFSeEntradasCompetenciasHandler — GET /api/nfse-entradas/competencias
// ---------------------------------------------------------------------------

func NFSeEntradasCompetenciasHandler(db *sql.DB) http.HandlerFunc {
	return nfseCompetencias(db, "nfse_entradas")
}

// ---------------------------------------------------------------------------
// NFSeSaidasCompetenciasHandler — GET /api/nfse-saidas/competencias
// ---------------------------------------------------------------------------

func NFSeSaidasCompetenciasHandler(db *sql.DB) http.HandlerFunc {
	return nfseCompetencias(db, "nfse_saidas")
}

// nfseList é o corpo compartilhado de NFSeEntradasListHandler/NFSeSaidasListHandler — mesmo
// padrão de paginação/filtro/ordenação/totalizador de NfeEntradasListHandler/
// NfeSaidasListHandler (nfe_entradas.go/nfe_saidas.go), só troca a tabela consultada (o schema
// de nfse_entradas/nfse_saidas é idêntico nas duas tabelas, migration 133).
func nfseList(db *sql.DB, table, logPrefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if table != "nfse_entradas" && table != "nfse_saidas" {
			log.Printf("%s tabela inválida: %q", logPrefix, table)
			jsonErr(w, http.StatusInternalServerError, "Erro interno")
			return
		}

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
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, logPrefix)
			return
		}

		q := r.URL.Query()
		mesAno := q.Get("mes_ano")
		cnpjPrestador := q.Get("cnpj_prestador")
		cnpjTomador := q.Get("cnpj_tomador")
		dataDe := q.Get("data_de")
		dataAte := q.Get("data_ate")

		sortCol := "data_emissao"
		if c, ok := nfseSafeSortCols[q.Get("sort_by")]; ok {
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
		// page sem limite superior permitiria um offset absurdamente grande (ou overflow de
		// int) — acima de 1_000_000 ou offset negativo, trata como primeira página.
		if page > 1_000_000 {
			page = 1
		}

		args := []interface{}{companyID}
		idx := 2
		where := "WHERE company_id = $1"

		if mesAno != "" {
			where += fmt.Sprintf(" AND mes_ano = $%d", idx)
			args = append(args, mesAno)
			idx++
		}
		if cnpjPrestador != "" {
			where += fmt.Sprintf(" AND cnpj_prestador = $%d", idx)
			args = append(args, cnpjPrestador)
			idx++
		}
		if cnpjTomador != "" {
			where += fmt.Sprintf(" AND cnpj_tomador = $%d", idx)
			args = append(args, cnpjTomador)
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

		var total int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" "+where, args...).Scan(&total); err != nil {
			log.Printf("%s count error: %v", logPrefix, err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}

		// Totais excluem notas canceladas (a contagem/listagem acima continua incluindo-as).
		var totValorServicos, totISS, totPIS, totCOFINS, totINSS, totIRRF, totCSLL float64
		if err := db.QueryRow(
			"SELECT COALESCE(SUM(valor_servicos) FILTER (WHERE NOT cancelada),0), COALESCE(SUM(iss) FILTER (WHERE NOT cancelada),0), "+
				"COALESCE(SUM(pis) FILTER (WHERE NOT cancelada),0), COALESCE(SUM(cofins) FILTER (WHERE NOT cancelada),0), "+
				"COALESCE(SUM(inss) FILTER (WHERE NOT cancelada),0), COALESCE(SUM(irrf) FILTER (WHERE NOT cancelada),0), "+
				"COALESCE(SUM(csll) FILTER (WHERE NOT cancelada),0) FROM "+table+" "+where,
			args...,
		).Scan(&totValorServicos, &totISS, &totPIS, &totCOFINS, &totINSS, &totIRRF, &totCSLL); err != nil {
			log.Printf("%s sum error: %v", logPrefix, err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		totValorServicos = nfseSanitizeFloat(totValorServicos)
		totISS = nfseSanitizeFloat(totISS)
		totPIS = nfseSanitizeFloat(totPIS)
		totCOFINS = nfseSanitizeFloat(totCOFINS)
		totINSS = nfseSanitizeFloat(totINSS)
		totIRRF = nfseSanitizeFloat(totIRRF)
		totCSLL = nfseSanitizeFloat(totCSLL)

		offset := (page - 1) * pageSize
		if offset < 0 {
			page = 1
			offset = 0
		}
		selectQ := `
			SELECT
				id, identificador_nfse,
				COALESCE(cnpj_prestador, ''), COALESCE(cnpj_tomador, ''),
				COALESCE(TO_CHAR(data_emissao, 'DD/MM/YYYY'), ''),
				COALESCE(mes_ano, ''),
				COALESCE(valor_servicos, 0),
				COALESCE(base_iss, 0), COALESCE(iss, 0), COALESCE(aliquota_iss, 0), COALESCE(iss_retido, 0),
				COALESCE(base_pis, 0), COALESCE(pis, 0), COALESCE(base_cofins, 0), COALESCE(cofins, 0),
				COALESCE(inss, 0), COALESCE(irrf, 0), COALESCE(csll, 0),
				COALESCE(numero_nfse, ''), cancelada,
				COALESCE(TO_CHAR(data_cancelamento AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY'), '')
			FROM ` + table + " " + where +
			fmt.Sprintf(" ORDER BY %s %s, identificador_nfse DESC LIMIT $%d OFFSET $%d", sortCol, sortDir, idx, idx+1)
		pageArgs := append(args, pageSize, offset)

		rows, err := db.Query(selectQ, pageArgs...)
		if err != nil {
			log.Printf("%s error: %v", logPrefix, err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}
		defer rows.Close()

		list := []nfseRow{}
		for rows.Next() {
			var row nfseRow
			if err := rows.Scan(
				&row.ID, &row.IdentificadorNFSe,
				&row.CNPJPrestador, &row.CNPJTomador,
				&row.DataEmissao, &row.MesAno,
				&row.ValorServicos,
				&row.BaseISS, &row.ISS, &row.AliquotaISS, &row.ISSRetido,
				&row.BasePIS, &row.PIS, &row.BaseCOFINS, &row.COFINS,
				&row.INSS, &row.IRRF, &row.CSLL,
				&row.NumeroNFSe, &row.Cancelada, &row.DataCancelamento,
			); err != nil {
				log.Printf("%s scan error: %v", logPrefix, err)
				continue
			}
			row.ValorServicos = nfseSanitizeFloat(row.ValorServicos)
			row.BaseISS = nfseSanitizeFloat(row.BaseISS)
			row.ISS = nfseSanitizeFloat(row.ISS)
			row.AliquotaISS = nfseSanitizeFloat(row.AliquotaISS)
			row.ISSRetido = nfseSanitizeFloat(row.ISSRetido)
			row.BasePIS = nfseSanitizeFloat(row.BasePIS)
			row.PIS = nfseSanitizeFloat(row.PIS)
			row.BaseCOFINS = nfseSanitizeFloat(row.BaseCOFINS)
			row.COFINS = nfseSanitizeFloat(row.COFINS)
			row.INSS = nfseSanitizeFloat(row.INSS)
			row.IRRF = nfseSanitizeFloat(row.IRRF)
			row.CSLL = nfseSanitizeFloat(row.CSLL)
			list = append(list, row)
		}
		if err := rows.Err(); err != nil {
			log.Printf("%s rows iteration error: %v", logPrefix, err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao consultar banco")
			return
		}

		totalPages := (total + pageSize - 1) / pageSize
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
			"totals": map[string]float64{
				"valor_servicos": totValorServicos,
				"iss":            totISS,
				"pis":            totPIS,
				"cofins":         totCOFINS,
				"inss":           totINSS,
				"irrf":           totIRRF,
				"csll":           totCSLL,
			},
			"items": list,
		})
	}
}

// ---------------------------------------------------------------------------
// NFSeEntradasListHandler — GET /api/nfse-entradas
// ---------------------------------------------------------------------------

func NFSeEntradasListHandler(db *sql.DB) http.HandlerFunc {
	return nfseList(db, "nfse_entradas", "[NFSeEntradas]")
}

// ---------------------------------------------------------------------------
// NFSeSaidasListHandler — GET /api/nfse-saidas
// ---------------------------------------------------------------------------

func NFSeSaidasListHandler(db *sql.DB) http.HandlerFunc {
	return nfseList(db, "nfse_saidas", "[NFSeSaidas]")
}
