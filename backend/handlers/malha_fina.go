package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	jwt "github.com/golang-jwt/jwt/v5"
)

const malhaFinaPageSize = 100

// MalhaFinaRow — documento presente na RFB (rfb_debitos) mas não importado pela empresa.
type MalhaFinaRow struct {
	ID                 string  `json:"id"`
	ChaveDFe           string  `json:"chave_dfe"`
	ModeloDFe          string  `json:"modelo_dfe"`
	NumeroDFe          string  `json:"numero_dfe"`
	DataDFeEmissao     string  `json:"data_dfe_emissao"`
	DataApuracao       string  `json:"data_apuracao"`
	NiEmitente         string  `json:"ni_emitente"`
	NiAdquirente       string  `json:"ni_adquirente"`
	ValorCBSTotal      float64 `json:"valor_cbs_total"`
	ValorCBSExtinto    float64 `json:"valor_cbs_extinto"`
	ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
	SituacaoDebito     string  `json:"situacao_debito"`
	TipoApuracao       string  `json:"tipo_apuracao"`
}

type MalhaFinaTotals struct {
	ValorCBSTotal      float64 `json:"valor_cbs_total"`
	ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
}

type MalhaFinaResponse struct {
	Total      int             `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalPages int             `json:"total_pages"`
	Totals     MalhaFinaTotals `json:"totals"`
	Items      []MalhaFinaRow  `json:"items"`
}

// malhaFinaList é o handler genérico usado pelos 3 endpoints de Malha Fina.
// modelosDFe:      ex. []string{"55","65"} ou []string{"57"}
// excludeTable:    "nfe_entradas" | "nfe_saidas" | "cte_entradas"
// excludeChaveCol: "chave_nfe" | "chave_cte"
func malhaFinaList(db *sql.DB, w http.ResponseWriter, r *http.Request, modelosDFe []string, excludeTable, excludeChaveCol string) {
	claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
	if !ok {
		jsonErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims["user_id"].(string)
	companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	dataApuracao := q.Get("data_apuracao") // MM/YYYY
	filterCNPJ := strings.NewReplacer(".", "", "/", "", "-", "").Replace(q.Get("cnpj"))

	// ── Montar WHERE ──────────────────────────────────────────────────────────
	args := []interface{}{companyID}

	modeloPlaceholders := make([]string, len(modelosDFe))
	for i, m := range modelosDFe {
		args = append(args, m)
		modeloPlaceholders[i] = fmt.Sprintf("$%d", len(args))
	}

	where := fmt.Sprintf(
		"rd.company_id = $1 AND rd.modelo_dfe IN (%s) AND rd.chave_dfe != '' AND NOT EXISTS (SELECT 1 FROM %s t WHERE t.company_id = $1 AND t.%s = rd.chave_dfe)",
		strings.Join(modeloPlaceholders, ","), excludeTable, excludeChaveCol,
	)

	if dataApuracao != "" {
		args = append(args, dataApuracao)
		where += fmt.Sprintf(" AND rd.data_apuracao = $%d", len(args))
	}
	if filterCNPJ != "" {
		args = append(args, filterCNPJ+"%")
		idx1 := len(args)
		args = append(args, filterCNPJ+"%")
		idx2 := len(args)
		where += fmt.Sprintf(" AND (rd.ni_emitente LIKE $%d OR rd.ni_adquirente LIKE $%d)", idx1, idx2)
	}

	// ── COUNT ─────────────────────────────────────────────────────────────────
	var total int
	if err := db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM rfb_debitos rd WHERE %s", where), args...,
	).Scan(&total); err != nil {
		log.Printf("malha_fina count error: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro ao contar registros")
		return
	}

	totalPages := (total + malhaFinaPageSize - 1) / malhaFinaPageSize
	if totalPages < 1 {
		totalPages = 1
	}

	// ── TOTAIS ────────────────────────────────────────────────────────────────
	var totCBSTotal, totCBSNaoExtinto float64
	_ = db.QueryRow(
		fmt.Sprintf("SELECT COALESCE(SUM(rd.valor_cbs_total),0), COALESCE(SUM(rd.valor_cbs_nao_extinto),0) FROM rfb_debitos rd WHERE %s", where),
		args...,
	).Scan(&totCBSTotal, &totCBSNaoExtinto)

	// ── DADOS ─────────────────────────────────────────────────────────────────
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	dataArgs := append(args, malhaFinaPageSize, (page-1)*malhaFinaPageSize) //nolint

	dataSQL := fmt.Sprintf(`
		SELECT rd.id,
		       rd.chave_dfe,
		       COALESCE(rd.modelo_dfe, ''),
		       COALESCE(rd.numero_dfe, ''),
		       COALESCE(TO_CHAR(rd.data_dfe_emissao, 'DD/MM/YYYY'), ''),
		       COALESCE(rd.data_apuracao, ''),
		       COALESCE(rd.ni_emitente, ''),
		       COALESCE(rd.ni_adquirente, ''),
		       COALESCE(rd.valor_cbs_total, 0),
		       COALESCE(rd.valor_cbs_extinto, 0),
		       COALESCE(rd.valor_cbs_nao_extinto, 0),
		       COALESCE(rd.situacao_debito, ''),
		       COALESCE(rd.tipo_apuracao, '')
		FROM rfb_debitos rd
		WHERE %s
		ORDER BY rd.data_dfe_emissao DESC NULLS LAST
		LIMIT $%d OFFSET $%d
	`, where, limitIdx, offsetIdx)

	rows, err := db.Query(dataSQL, dataArgs...)
	if err != nil {
		log.Printf("malha_fina query error: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro ao buscar dados")
		return
	}
	defer rows.Close()

	items := []MalhaFinaRow{}
	for rows.Next() {
		var row MalhaFinaRow
		if err := rows.Scan(
			&row.ID, &row.ChaveDFe, &row.ModeloDFe, &row.NumeroDFe,
			&row.DataDFeEmissao, &row.DataApuracao,
			&row.NiEmitente, &row.NiAdquirente,
			&row.ValorCBSTotal, &row.ValorCBSExtinto, &row.ValorCBSNaoExtinto,
			&row.SituacaoDebito, &row.TipoApuracao,
		); err != nil {
			log.Printf("malha_fina scan error: %v", err)
			continue
		}
		items = append(items, row)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MalhaFinaResponse{
		Total:      total,
		Page:       page,
		PageSize:   malhaFinaPageSize,
		TotalPages: totalPages,
		Totals:     MalhaFinaTotals{ValorCBSTotal: totCBSTotal, ValorCBSNaoExtinto: totCBSNaoExtinto},
		Items:      items,
	})
}

// MalhaFinaNFeEntradasHandler — GET /api/malha-fina/nfe-entradas
func MalhaFinaNFeEntradasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		malhaFinaList(db, w, r, []string{"55", "65"}, "nfe_entradas", "chave_nfe")
	}
}

// MalhaFinaNFeSaidasHandler — GET /api/malha-fina/nfe-saidas
func MalhaFinaNFeSaidasHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		malhaFinaList(db, w, r, []string{"55", "65"}, "nfe_saidas", "chave_nfe")
	}
}

// MalhaFinaCTeHandler — GET /api/malha-fina/cte
func MalhaFinaCTeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		malhaFinaList(db, w, r, []string{"57"}, "cte_entradas", "chave_cte")
	}
}
