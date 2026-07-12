package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ─── SAPSyncRunsListHandler ───────────────────────────────────────────────
// GET /api/sap/sync-runs
// Query params: bukrs, status, page, page_size
//
// Histórico de execuções de sincronização SAP (Story 2.4, AC #2/#5) — cada
// linha é uma execução por (company_id, bukrs), nunca agregada por empresa,
// para que uma falha em 1 de N BUKRS configurados seja visível isoladamente.

type sapSyncRunItem struct {
	ID                  string  `json:"id"`
	Bukrs               string  `json:"bukrs"`
	Status              string  `json:"status"`
	IniciadoEm          string  `json:"iniciado_em"`
	ConcluidoEm         *string `json:"concluido_em"`
	DuracaoSegundos     *int64  `json:"duracao_segundos"`
	ChavesEnviadas      int     `json:"chaves_enviadas"`
	ChavesPagoTotal     int     `json:"chaves_pago_total"`
	ChavesPagoParcial   int     `json:"chaves_pago_parcial"`
	ChavesEmAberto      int     `json:"chaves_em_aberto"`
	ChavesNaoLocalizado int     `json:"chaves_nao_localizado"`
	ErroDetalhe         string  `json:"erro_detalhe"`
}

func SAPSyncRunsListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}

		q := r.URL.Query()
		bukrs := strings.TrimSpace(q.Get("bukrs"))
		status := strings.TrimSpace(q.Get("status"))

		page, err := strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 {
			page = 1
		}
		pageSize, err := strconv.Atoi(q.Get("page_size"))
		if err != nil || pageSize < 1 {
			pageSize = 50
		}
		if pageSize > 200 {
			pageSize = 200
		}

		args := []interface{}{companyID}
		where := []string{"company_id = $1"}
		idx := 2

		if bukrs != "" {
			where = append(where, fmt.Sprintf("bukrs = $%d", idx))
			args = append(args, bukrs)
			idx++
		}
		if status != "" {
			where = append(where, fmt.Sprintf("status = $%d", idx))
			args = append(args, status)
			idx++
		}
		whereClause := "WHERE " + strings.Join(where, " AND ")

		var total int
		countQuery := "SELECT COUNT(*) FROM sap_sync_runs " + whereClause
		if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
			log.Printf("[SAPSyncRunsList] count: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao contar execuções")
			return
		}

		offset := (page - 1) * pageSize
		listArgs := append(args, pageSize, offset)
		rows, err := db.Query(fmt.Sprintf(`
			SELECT id, bukrs, status, iniciado_em, concluido_em,
			       COALESCE(array_length(chaves_enviadas, 1), 0),
			       chaves_pago_total, chaves_pago_parcial, chaves_em_aberto, chaves_nao_localizado,
			       COALESCE(erro_detalhe, '')
			FROM sap_sync_runs %s
			ORDER BY iniciado_em DESC
			LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1),
			listArgs...,
		)
		if err != nil {
			log.Printf("[SAPSyncRunsList] query: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao listar execuções")
			return
		}
		defer rows.Close()

		items := []sapSyncRunItem{}
		for rows.Next() {
			var item sapSyncRunItem
			var iniciadoEm time.Time
			var concluidoEm sql.NullTime
			if err := rows.Scan(
				&item.ID, &item.Bukrs, &item.Status, &iniciadoEm, &concluidoEm,
				&item.ChavesEnviadas, &item.ChavesPagoTotal, &item.ChavesPagoParcial,
				&item.ChavesEmAberto, &item.ChavesNaoLocalizado, &item.ErroDetalhe,
			); err != nil {
				log.Printf("[SAPSyncRunsList] scan: %v", err)
				continue
			}
			item.IniciadoEm = iniciadoEm.Format(time.RFC3339)
			if concluidoEm.Valid {
				s := concluidoEm.Time.Format(time.RFC3339)
				item.ConcluidoEm = &s
				dur := int64(concluidoEm.Time.Sub(iniciadoEm).Seconds())
				item.DuracaoSegundos = &dur
			}
			items = append(items, item)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"items":     items,
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		})
	}
}
