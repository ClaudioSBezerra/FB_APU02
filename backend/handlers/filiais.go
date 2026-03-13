package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// FilialInfo represents a filial (branch) for the global selector.
type FilialInfo struct {
	CNPJ    string `json:"cnpj"`
	Nome    string `json:"nome"`
	Apelido string `json:"apelido"`
}

// GetFiliaisHandler returns distinct filiais for the authenticated company.
// GET /api/filiais
func GetFiliaisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			http.Error(w, "Error getting company: "+err.Error(), http.StatusInternalServerError)
			return
		}

		rows, err := db.Query(`
			SELECT DISTINCT m.filial_cnpj, m.filial_nome, COALESCE(a.apelido, '') as apelido
			FROM mv_mercadorias_agregada m
			LEFT JOIN filial_apelidos a ON a.cnpj = m.filial_cnpj AND a.company_id = $1
			WHERE m.company_id = $1
			  AND m.filial_cnpj IS NOT NULL
			  AND m.filial_cnpj != ''
			ORDER BY m.filial_nome ASC, m.filial_cnpj ASC
		`, companyID)
		if err != nil {
			http.Error(w, "Error querying filiais: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		filiais := make([]FilialInfo, 0)
		for rows.Next() {
			var f FilialInfo
			if err := rows.Scan(&f.CNPJ, &f.Nome, &f.Apelido); err != nil {
				continue
			}
			filiais = append(filiais, f)
		}

		json.NewEncoder(w).Encode(filiais)
	}
}
