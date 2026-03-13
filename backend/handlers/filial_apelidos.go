package handlers

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
)

type FilialApelido struct {
	CNPJ    string `json:"cnpj"`
	Apelido string `json:"apelido"`
}

// FilialApelidosHandler handles GET (list) and DELETE (clear all) for filial apelidos.
func FilialApelidosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

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

		switch r.Method {
		case http.MethodGet:
			rows, err := db.Query(
				"SELECT cnpj, apelido FROM filial_apelidos WHERE company_id = $1 ORDER BY cnpj",
				companyID,
			)
			if err != nil {
				log.Printf("FilialApelidos GET error: %v", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			list := []FilialApelido{}
			for rows.Next() {
				var fa FilialApelido
				if err := rows.Scan(&fa.CNPJ, &fa.Apelido); err != nil {
					log.Printf("FilialApelidos scan error: %v", err)
					continue
				}
				list = append(list, fa)
			}

			json.NewEncoder(w).Encode(list)

		case http.MethodDelete:
			_, err := db.Exec("DELETE FROM filial_apelidos WHERE company_id = $1", companyID)
			if err != nil {
				log.Printf("FilialApelidos DELETE error: %v", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"message": "Apelidos removidos com sucesso"})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// ImportFilialApelidosHandler handles POST multipart CSV import of filial apelidos.
// CSV format: CNPJ;APELIDO (header optional)
func ImportFilialApelidosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

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

		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Invalid file: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Read all bytes to handle BOM
		raw, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Error reading file: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Remove UTF-8 BOM if present
		content := string(raw)
		content = strings.TrimPrefix(content, "\xef\xbb\xbf")

		lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")

		imported := 0
		skipped := 0
		var errs []string

		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			parts := strings.SplitN(line, ";", 2)
			if len(parts) < 2 {
				skipped++
				errs = append(errs, "Linha "+strconv.Itoa(i+1)+": formato inválido (esperado CNPJ;APELIDO)")
				continue
			}

			rawCNPJ := strings.TrimSpace(parts[0])
			rawApelido := strings.TrimSpace(parts[1])

			// Skip header line
			if strings.EqualFold(rawCNPJ, "cnpj") {
				continue
			}

			// Sanitize CNPJ: keep only digits
			cnpj := strings.Map(func(r rune) rune {
				if unicode.IsDigit(r) {
					return r
				}
				return -1
			}, rawCNPJ)

			if len(cnpj) != 14 {
				skipped++
				errs = append(errs, "Linha "+strconv.Itoa(i+1)+": CNPJ inválido '"+rawCNPJ+"' (deve ter 14 dígitos)")
				continue
			}

			apelido := strings.TrimSpace(rawApelido)
			if len(apelido) < 2 || len(apelido) > 20 {
				skipped++
				errs = append(errs, "Linha "+strconv.Itoa(i+1)+": apelido '"+apelido+"' deve ter entre 2 e 20 caracteres")
				continue
			}

			_, err := db.Exec(
				`INSERT INTO filial_apelidos (company_id, cnpj, apelido)
				 VALUES ($1, $2, $3)
				 ON CONFLICT (company_id, cnpj) DO UPDATE SET apelido = $3, updated_at = NOW()`,
				companyID, cnpj, apelido,
			)
			if err != nil {
				skipped++
				errs = append(errs, "Linha "+strconv.Itoa(i+1)+": erro ao inserir CNPJ "+cnpj+": "+err.Error())
				continue
			}
			imported++
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"imported": imported,
			"skipped":  skipped,
			"errors":   errs,
		})
	}
}
