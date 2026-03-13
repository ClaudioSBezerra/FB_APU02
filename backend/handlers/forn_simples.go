package handlers

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type FornSimples struct {
	CNPJ string `json:"cnpj"`
}

func ListFornSimplesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		rows, err := db.Query("SELECT cnpj FROM forn_simples ORDER BY cnpj")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var list []FornSimples
		for rows.Next() {
			var f FornSimples
			if err := rows.Scan(&f.CNPJ); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			list = append(list, f)
		}

		if list == nil {
			list = []FornSimples{}
		}

		json.NewEncoder(w).Encode(list)
	}
}

func CreateFornSimplesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var f FornSimples
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Clean CNPJ (keep only numbers)
		cnpj := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, f.CNPJ)

		if len(cnpj) != 14 {
			http.Error(w, "CNPJ must have 14 digits", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("INSERT INTO forn_simples (cnpj) VALUES ($1) ON CONFLICT (cnpj) DO NOTHING", cnpj)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"message": "CNPJ added successfully", "cnpj": cnpj})
	}
}

func DeleteFornSimplesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if r.Method != "DELETE" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cnpj := r.URL.Query().Get("cnpj")
		if cnpj == "" {
			http.Error(w, "CNPJ parameter is required", http.StatusBadRequest)
			return
		}

		// Clean CNPJ
		cnpj = strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, cnpj)

		result, err := db.Exec("DELETE FROM forn_simples WHERE cnpj = $1", cnpj)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			http.Error(w, "CNPJ not found", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "CNPJ deleted successfully"})
	}
}

func ImportFornSimplesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Invalid file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		reader := csv.NewReader(file)
		reader.Comma = ';'
		reader.LazyQuotes = true

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, "Database connection error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		stmt, err := tx.Prepare("INSERT INTO forn_simples (cnpj) VALUES ($1) ON CONFLICT (cnpj) DO NOTHING")
		if err != nil {
			tx.Rollback()
			http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer stmt.Close()

		count := 0
		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				tx.Rollback()
				http.Error(w, "Error reading CSV: "+err.Error(), http.StatusBadRequest)
				return
			}

			if len(record) < 1 {
				continue
			}

			rawCNPJ := strings.TrimSpace(record[0])
			// Skip header if present
			if strings.EqualFold(rawCNPJ, "CNPJ") {
				continue
			}

			// Clean CNPJ
			cnpj := strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, rawCNPJ)

			if len(cnpj) != 14 {
				// Optionally log warning or skip
				continue
			}

			_, err = stmt.Exec(cnpj)
			if err != nil {
				tx.Rollback()
				http.Error(w, "Error inserting CNPJ "+cnpj+": "+err.Error(), http.StatusInternalServerError)
				return
			}
			count++
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Import successful",
			"count":   count,
		})
	}
}
