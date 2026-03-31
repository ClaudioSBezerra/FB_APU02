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

// MalhaFinaRow — documento presente na RFB (rfb_debitos) mas ausente ou cancelado na empresa.
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
	StatusNota         string  `json:"status_nota"` // "AUSENTE" | "CANCELADA"
}

type MalhaFinaTotals struct {
	ValorCBSTotal      float64 `json:"valor_cbs_total"`
	ValorCBSNaoExtinto float64 `json:"valor_cbs_nao_extinto"`
	CanceladasCount    int     `json:"canceladas_count"`
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

	dataDe     := q.Get("data_de")    // YYYY-MM-DD
	dataAte    := q.Get("data_ate")   // YYYY-MM-DD
	statusFilt := q.Get("status")     // "ausente" | "cancelada" | "" = todas
	filterCNPJ := strings.NewReplacer(".", "", "/", "", "-", "").Replace(q.Get("emit_cnpj"))

	safeColsMalha := map[string]string{
		"data_dfe_emissao":     "rd.data_dfe_emissao",
		"valor_cbs_total":      "rd.valor_cbs_total",
		"valor_cbs_nao_extinto": "rd.valor_cbs_nao_extinto",
	}
	sortCol := "rd.data_dfe_emissao"
	if c, ok := safeColsMalha[q.Get("sort_by")]; ok { sortCol = c }
	sortDir := "DESC"
	if q.Get("sort_dir") == "asc" { sortDir = "ASC" }

	// ── Montar WHERE ──────────────────────────────────────────────────────────
	args := []interface{}{companyID}

	modeloPlaceholders := make([]string, len(modelosDFe))
	for i, m := range modelosDFe {
		args = append(args, m)
		modeloPlaceholders[i] = fmt.Sprintf("$%d", len(args))
	}

	// Mostrar notas que estão na RFB mas não têm registro NORMAL na empresa.
	// Notas importadas como canceladas (cancelado='S') ainda aparecem com status CANCELADA.
	// Dedup por created_at removido: índice único em (company_id, chave_dfe) já garante unicidade.
	where := fmt.Sprintf(
		"rd.company_id = $1 AND rd.modelo_dfe IN (%s) AND rd.chave_dfe != ''"+
			" AND NOT EXISTS (SELECT 1 FROM %s t WHERE t.company_id = $1 AND t.%s = rd.chave_dfe AND COALESCE(t.cancelado,'N') != 'S')",
		strings.Join(modeloPlaceholders, ","), excludeTable, excludeChaveCol,
	)

	if dataDe != "" {
		args = append(args, dataDe)
		where += fmt.Sprintf(" AND rd.data_dfe_emissao >= $%d::date", len(args))
	}
	if dataAte != "" {
		args = append(args, dataAte)
		where += fmt.Sprintf(" AND rd.data_dfe_emissao <= $%d::date", len(args))
	}
	switch statusFilt {
	case "ausente":
		where += fmt.Sprintf(" AND NOT EXISTS (SELECT 1 FROM %s t2 WHERE t2.company_id = $1 AND t2.%s = rd.chave_dfe)", excludeTable, excludeChaveCol)
	case "cancelada":
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM %s t2 WHERE t2.company_id = $1 AND t2.%s = rd.chave_dfe)", excludeTable, excludeChaveCol)
	}
	if filterCNPJ != "" {
		args = append(args, filterCNPJ+"%")
		where += fmt.Sprintf(" AND rd.ni_emitente LIKE $%d", len(args))
	}

	// ── COUNT + TOTAIS em 1 query (CTE com EXISTS pré-computado) ─────────────
	// Substitui 3 queries separadas por uma única passagem sobre os dados.
	statsSQL := fmt.Sprintf(`
		WITH base AS (
			SELECT rd.valor_cbs_total,
			       rd.valor_cbs_nao_extinto,
			       EXISTS (SELECT 1 FROM %s t2 WHERE t2.company_id = $1 AND t2.%s = rd.chave_dfe) AS is_cancelada
			FROM rfb_debitos rd
			WHERE %s
		)
		SELECT COUNT(*),
		       COALESCE(SUM(valor_cbs_total), 0),
		       COALESCE(SUM(valor_cbs_nao_extinto), 0),
		       COUNT(*) FILTER (WHERE is_cancelada)
		FROM base
	`, excludeTable, excludeChaveCol, where)

	var total, canceladasCount int
	var totCBSTotal, totCBSNaoExtinto float64
	if err := db.QueryRow(statsSQL, args...).Scan(&total, &totCBSTotal, &totCBSNaoExtinto, &canceladasCount); err != nil {
		log.Printf("malha_fina stats error: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro ao calcular totais")
		return
	}

	totalPages := (total + malhaFinaPageSize - 1) / malhaFinaPageSize
	if totalPages < 1 {
		totalPages = 1
	}

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
		       COALESCE(rd.tipo_apuracao, ''),
		       CASE WHEN EXISTS (SELECT 1 FROM %s t2 WHERE t2.company_id = $1 AND t2.%s = rd.chave_dfe)
		            THEN 'CANCELADA' ELSE 'AUSENTE' END AS status_nota
		FROM rfb_debitos rd
		WHERE %s
		ORDER BY %s %s NULLS LAST
		LIMIT $%d OFFSET $%d
	`, excludeTable, excludeChaveCol, where, sortCol, sortDir, limitIdx, offsetIdx)

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
			&row.SituacaoDebito, &row.TipoApuracao, &row.StatusNota,
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
		Totals:     MalhaFinaTotals{ValorCBSTotal: totCBSTotal, ValorCBSNaoExtinto: totCBSNaoExtinto, CanceladasCount: canceladasCount},
		Items:      items,
	})
}

// MalhaFinaResumoRow — linha do resumo por emitente + dia (da MV).
type MalhaFinaResumoRow struct {
	NiEmitente          string  `json:"ni_emitente"`
	DataEmissao         string  `json:"data_emissao"` // YYYY-MM-DD
	Quantidade          int     `json:"quantidade"`
	ValorCBSNaoExtinto  float64 `json:"valor_cbs_nao_extinto"`
}

// MalhaFinaResumoGeralRow — linha do resumo geral (inclui tipo).
type MalhaFinaResumoGeralRow struct {
	Tipo                string  `json:"tipo"`
	NiEmitente          string  `json:"ni_emitente"`
	DataEmissao         string  `json:"data_emissao"`
	Quantidade          int     `json:"quantidade"`
	ValorCBSNaoExtinto  float64 `json:"valor_cbs_nao_extinto"`
}

// RefreshMalhaFinaMV dispara REFRESH CONCURRENTLY na mv_malha_fina_resumo.
// Deve ser chamado em goroutine após downloads de rfb_debitos.
func RefreshMalhaFinaMV(db *sql.DB) {
	if _, err := db.Exec(`REFRESH MATERIALIZED VIEW CONCURRENTLY mv_malha_fina_resumo`); err != nil {
		log.Printf("[MalhaFina MV] Erro no REFRESH: %v", err)
	} else {
		log.Printf("[MalhaFina MV] REFRESH concluído")
	}
}

// malhaFinaResumoFromMV consulta a MV pré-computada — O(1) em vez de O(n²).
func malhaFinaResumoFromMV(db *sql.DB, w http.ResponseWriter, r *http.Request, tipo string) {
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

	q       := r.URL.Query()
	dataDe  := q.Get("data_de")
	dataAte := q.Get("data_ate")

	args  := []interface{}{companyID, tipo}
	where := "company_id = $1 AND tipo = $2"

	if dataDe != "" {
		args = append(args, dataDe)
		where += fmt.Sprintf(" AND data_emissao >= $%d::date", len(args))
	}
	if dataAte != "" {
		args = append(args, dataAte)
		where += fmt.Sprintf(" AND data_emissao <= $%d::date", len(args))
	}

	rows, err := db.Query(fmt.Sprintf(`
		SELECT ni_emitente,
		       TO_CHAR(data_emissao, 'YYYY-MM-DD'),
		       quantidade,
		       valor_cbs_nao_extinto
		FROM mv_malha_fina_resumo
		WHERE %s
		ORDER BY ni_emitente, data_emissao DESC
	`, where), args...)
	if err != nil {
		log.Printf("malha_fina resumo MV error: %v", err)
		jsonErr(w, http.StatusInternalServerError, "Erro ao buscar resumo")
		return
	}
	defer rows.Close()

	items := []MalhaFinaResumoRow{}
	for rows.Next() {
		var row MalhaFinaResumoRow
		if err := rows.Scan(&row.NiEmitente, &row.DataEmissao, &row.Quantidade, &row.ValorCBSNaoExtinto); err != nil {
			continue
		}
		items = append(items, row)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": items})
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

// Handlers de resumo por tipo — consultam a MV (instantâneo)

func MalhaFinaNFeEntradasResumoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { malhaFinaResumoFromMV(db, w, r, "nfe-entradas") }
}
func MalhaFinaNFeSaidasResumoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { malhaFinaResumoFromMV(db, w, r, "nfe-saidas") }
}
func MalhaFinaCTeResumoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { malhaFinaResumoFromMV(db, w, r, "cte") }
}

// MalhaFinaResumoGeralHandler — GET /api/malha-fina/resumo-geral
// Retorna todos os tipos unificados da MV para a aba Resumo Geral.
func MalhaFinaResumoGeralHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		q      := r.URL.Query()
		dataDe  := q.Get("data_de")
		dataAte := q.Get("data_ate")

		args  := []interface{}{companyID}
		where := "company_id = $1"
		if dataDe != "" {
			args = append(args, dataDe)
			where += fmt.Sprintf(" AND data_emissao >= $%d::date", len(args))
		}
		if dataAte != "" {
			args = append(args, dataAte)
			where += fmt.Sprintf(" AND data_emissao <= $%d::date", len(args))
		}

		rows, err := db.Query(fmt.Sprintf(`
			SELECT tipo,
			       ni_emitente,
			       TO_CHAR(data_emissao, 'YYYY-MM-DD'),
			       quantidade,
			       valor_cbs_nao_extinto
			FROM mv_malha_fina_resumo
			WHERE %s
			ORDER BY tipo, ni_emitente, data_emissao DESC
		`, where), args...)
		if err != nil {
			log.Printf("malha_fina resumo geral error: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao buscar resumo geral")
			return
		}
		defer rows.Close()

		items := []MalhaFinaResumoGeralRow{}
		for rows.Next() {
			var row MalhaFinaResumoGeralRow
			if err := rows.Scan(&row.Tipo, &row.NiEmitente, &row.DataEmissao, &row.Quantidade, &row.ValorCBSNaoExtinto); err != nil {
				continue
			}
			items = append(items, row)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}
}

// MalhaFinaResumoRefreshHandler — POST /api/malha-fina/resumo-geral/refresh
// Dispara REFRESH CONCURRENTLY manualmente.
func MalhaFinaResumoRefreshHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Verifica auth (qualquer usuário autenticado pode disparar)
		if _, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims); !ok {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		go RefreshMalhaFinaMV(db)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "refresh_iniciado"})
	}
}
