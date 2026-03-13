package handlers

import (
	"database/sql"
	"encoding/json"
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

func GetTaxRatesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		query := `
			SELECT ano, perc_ibs_uf, perc_ibs_mun, perc_cbs, perc_reduc_icms, perc_reduc_piscofins 
			FROM tabela_aliquotas 
			ORDER BY ano ASC
		`

		rows, err := db.Query(query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var rates []TaxRate
		for rows.Next() {
			var r TaxRate
			if err := rows.Scan(&r.Ano, &r.PercIBS_UF, &r.PercIBS_Mun, &r.PercCBS, &r.PercReducICMS, &r.PercReducPisCofins); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rates = append(rates, r)
		}

		json.NewEncoder(w).Encode(rates)
	}
}