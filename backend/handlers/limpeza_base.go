package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// LimpezaBaseHandler exposes GET (preview counts + available periods) and DELETE
// (execute scoped deletion) for the Limpeza de Base de Dados admin feature.
func LimpezaBaseHandler(db *sql.DB) http.HandlerFunc {
	allowedTables := map[string]bool{
		"nfe_entradas": true,
		"nfe_saidas":   true,
		"cte_entradas": true,
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, 401, "Não autenticado")
			return
		}

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, 401, "Não autenticado")
			return
		}
		if role, _ := claims["role"].(string); role != "admin" {
			jsonErr(w, 403, "Acesso restrito a administradores")
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, 500, "Erro ao identificar empresa", err, "[LimpezaBase]")
			return
		}

		switch r.Method {

		case http.MethodGet:
			mesAno := r.URL.Query().Get("mes_ano")

			// Available periods (union of all 3 tables)
			perRows, _ := db.Query(`
				SELECT mes_ano FROM (
					SELECT DISTINCT mes_ano FROM (
						SELECT mes_ano FROM nfe_entradas WHERE company_id = $1
						UNION
						SELECT mes_ano FROM nfe_saidas   WHERE company_id = $1
						UNION
						SELECT mes_ano FROM cte_entradas WHERE company_id = $1
					) t
					WHERE mes_ano IS NOT NULL AND mes_ano != ''
				) u
				ORDER BY SPLIT_PART(mes_ano, '/', 2) DESC,
				         SPLIT_PART(mes_ano, '/', 1) DESC
			`, companyID)

			var periodos []string
			if perRows != nil {
				defer perRows.Close()
				for perRows.Next() {
					var m string
					if perRows.Scan(&m) == nil {
						periodos = append(periodos, m)
					}
				}
			}
			if periodos == nil {
				periodos = []string{}
			}

			// Counts per table
			contagens := map[string]int64{}
			for _, tbl := range []string{"nfe_entradas", "nfe_saidas", "cte_entradas"} {
				var count int64
				var qErr error
				if mesAno != "" {
					qErr = db.QueryRow("SELECT COUNT(*) FROM "+tbl+" WHERE company_id = $1 AND mes_ano = $2", companyID, mesAno).Scan(&count)
				} else {
					qErr = db.QueryRow("SELECT COUNT(*) FROM "+tbl+" WHERE company_id = $1", companyID).Scan(&count)
				}
				if qErr == nil {
					contagens[tbl] = count
				}
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"periodos":  periodos,
				"contagens": contagens,
			})

		case http.MethodDelete:
			var req struct {
				Tabelas []string `json:"tabelas"`
				MesAno  string   `json:"mes_ano"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonErr(w, 400, "Requisição inválida")
				return
			}
			if len(req.Tabelas) == 0 {
				jsonErr(w, 400, "Selecione ao menos uma tabela")
				return
			}
			for _, t := range req.Tabelas {
				if !allowedTables[t] {
					jsonErr(w, 400, "Tabela inválida: "+t)
					return
				}
			}

			log.Printf("[LimpezaBase] Admin %s removendo tabelas=%v mes_ano=%q empresa=%s",
				userID, req.Tabelas, req.MesAno, companyID)

			totais := map[string]int64{}
			for _, tbl := range req.Tabelas {
				var res sql.Result
				var execErr error
				if req.MesAno != "" {
					res, execErr = db.Exec("DELETE FROM "+tbl+" WHERE company_id = $1 AND mes_ano = $2", companyID, req.MesAno)
				} else {
					res, execErr = db.Exec("DELETE FROM "+tbl+" WHERE company_id = $1", companyID)
				}
				if execErr != nil {
					sanitizeDBErr(w, 500, "Erro ao limpar tabela "+tbl, execErr, "[LimpezaBase]")
					return
				}
				n, _ := res.RowsAffected()
				totais[tbl] = n
				log.Printf("[LimpezaBase] %s: %d registros removidos", tbl, n)
			}

			// Signal bridge reset only when all 3 tables are cleared for all periods
			resetTracker := len(req.Tabelas) == 3 && req.MesAno == ""
			if resetTracker {
				_, resetErr := db.Exec(`
					INSERT INTO erp_bridge_config (company_id, ativo, horario, dias_retroativos, reset_tracker, updated_at)
					VALUES ($1, false, '02:00', 1, true, NOW())
					ON CONFLICT (company_id) DO UPDATE SET reset_tracker = true, updated_at = NOW()
				`, companyID)
				if resetErr != nil {
					log.Printf("[LimpezaBase] Aviso: nao foi possivel sinalizar reset_tracker: %v", resetErr)
				}
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":       "Registros removidos com sucesso",
				"totais":        totais,
				"reset_tracker": resetTracker,
			})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
