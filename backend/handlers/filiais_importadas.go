package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

type filialImportadaRow struct {
	CNPJ    string `json:"cnpj"`
	Nome    string `json:"nome"`
	Apelido string `json:"apelido"`
}

func queryFiliaisImportadas(db *sql.DB, w http.ResponseWriter, r *http.Request, query string) {
	claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID := claims["user_id"].(string)
	companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
	if err != nil || companyID == "" {
		http.Error(w, "company_id inválido", http.StatusBadRequest)
		return
	}

	rows, err := db.Query(query, companyID)
	if err != nil {
		log.Printf("filiaisImportadas query error: %v", err)
		http.Error(w, "Erro ao consultar filiais", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := []filialImportadaRow{}
	for rows.Next() {
		var f filialImportadaRow
		if err := rows.Scan(&f.CNPJ, &f.Nome, &f.Apelido); err != nil {
			continue
		}
		result = append(result, f)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// NfeSaidasFiliaisHandler — GET /api/nfe-saidas/filiais
// Retorna filiais distintas (emit_cnpj) com apelido, a partir de dados reais importados.
func NfeSaidasFiliaisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const q = `
			SELECT cnpj, nome, apelido FROM (
			  SELECT DISTINCT n.emit_cnpj AS cnpj,
			    COALESCE(fa.apelido, n.emit_cnpj) AS nome,
			    COALESCE(fa.apelido,'') AS apelido,
			    COALESCE(NULLIF(fa.apelido,''), n.emit_cnpj) AS sort_key
			  FROM nfe_saidas n
			  LEFT JOIN filial_apelidos fa
			    ON fa.company_id = n.company_id AND fa.cnpj = n.emit_cnpj
			  WHERE n.company_id = $1
			) t ORDER BY sort_key`
		queryFiliaisImportadas(db, w, r, q)
	}
}

// NfeEntradasFiliaisHandler — GET /api/nfe-entradas/filiais
// Retorna filiais distintas (dest_cnpj_cpf) com apelido, a partir de dados reais importados.
func NfeEntradasFiliaisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const q = `
			SELECT cnpj, nome, apelido FROM (
			  SELECT DISTINCT n.dest_cnpj_cpf AS cnpj,
			    COALESCE(fa.apelido, n.dest_cnpj_cpf) AS nome,
			    COALESCE(fa.apelido,'') AS apelido,
			    COALESCE(NULLIF(fa.apelido,''), n.dest_cnpj_cpf) AS sort_key
			  FROM nfe_entradas n
			  LEFT JOIN filial_apelidos fa
			    ON fa.company_id = n.company_id AND fa.cnpj = n.dest_cnpj_cpf
			  WHERE n.company_id = $1
			) t ORDER BY sort_key`
		queryFiliaisImportadas(db, w, r, q)
	}
}

// CteEntradasFiliaisHandler — GET /api/cte-entradas/filiais
// Retorna filiais distintas (dest_cnpj_cpf) com apelido, a partir de dados reais importados.
func CteEntradasFiliaisHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const q = `
			SELECT cnpj, nome, apelido FROM (
			  SELECT DISTINCT c.dest_cnpj_cpf AS cnpj,
			    COALESCE(fa.apelido, c.dest_cnpj_cpf) AS nome,
			    COALESCE(fa.apelido,'') AS apelido,
			    COALESCE(NULLIF(fa.apelido,''), c.dest_cnpj_cpf) AS sort_key
			  FROM cte_entradas c
			  LEFT JOIN filial_apelidos fa
			    ON fa.company_id = c.company_id AND fa.cnpj = c.dest_cnpj_cpf
			  WHERE c.company_id = $1
			) t ORDER BY sort_key`
		queryFiliaisImportadas(db, w, r, q)
	}
}
