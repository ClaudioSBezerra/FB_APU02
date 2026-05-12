package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
)

func jsonErr(w http.ResponseWriter, status int, msg string, extra ...map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	out := map[string]string{"error": msg}
	for _, m := range extra {
		for k, v := range m {
			out[k] = v
		}
	}
	json.NewEncoder(w).Encode(out)
}

type TaxRate struct {
	Ano                int     `json:"ano"`
	PercIBS_UF         float64 `json:"perc_ibs_uf"`
	PercIBS_Mun        float64 `json:"perc_ibs_mun"`
	PercCBS            float64 `json:"perc_cbs"`
	PercReducICMS      float64 `json:"perc_reduc_icms"`
	PercReducPisCofins float64 `json:"perc_reduc_piscofins"`
}

// sanitizeDBErr loga o erro interno do banco e retorna uma mensagem genérica ao cliente.
// NUNCA expõe err.Error() ao cliente — previne vazamento de schema/query PostgreSQL (SEC-04).
// Uso: sanitizeDBErr(w, http.StatusInternalServerError, "contexto da operação", err, "[HandlerName]")
func sanitizeDBErr(w http.ResponseWriter, status int, userMsg string, err error, prefix string) {
	log.Printf("%s %s: %v", prefix, userMsg, err)
	jsonErr(w, status, userMsg)
}

func GetTaxRatesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		query := `
			SELECT ano, perc_ibs_uf, perc_ibs_mun, perc_cbs, perc_reduc_icms, perc_reduc_piscofins 
			FROM tabela_aliquotas 
			ORDER BY ano ASC
		`

		rows, err := db.Query(query)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar alíquotas", err, "[TaxRates]")
			return
		}
		defer rows.Close()

		var rates []TaxRate
		for rows.Next() {
			var r TaxRate
			if err := rows.Scan(&r.Ano, &r.PercIBS_UF, &r.PercIBS_Mun, &r.PercCBS, &r.PercReducICMS, &r.PercReducPisCofins); err != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao ler alíquota", err, "[TaxRates]")
				return
			}
			rates = append(rates, r)
		}

		json.NewEncoder(w).Encode(rates)
	}
}