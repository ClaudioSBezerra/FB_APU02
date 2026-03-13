package handlers

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type CFOP struct {
	CFOP          string `json:"cfop"`
	DescricaoCFOP string `json:"descricao_cfop"`
	Tipo          string `json:"tipo"`
}

func ListCFOPsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		rows, err := db.Query("SELECT cfop, descricao_cfop, tipo FROM cfop ORDER BY cfop")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var cfops []CFOP
		for rows.Next() {
			var c CFOP
			if err := rows.Scan(&c.CFOP, &c.DescricaoCFOP, &c.Tipo); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			cfops = append(cfops, c)
		}
		
		if cfops == nil {
			cfops = []CFOP{}
		}

		json.NewEncoder(w).Encode(cfops)
	}
}

func ImportCFOPsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

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

		// Detect delimiter: Try ';' (Standard) then '\t' (Excel copy-paste)
		// We remove ',' because descriptions often contain commas, leading to false positives.
		delimiters := []rune{';', '\t'}
		var reader *csv.Reader
		
		for _, delim := range delimiters {
			if seeker, ok := file.(io.Seeker); ok {
				seeker.Seek(0, 0)
			}
			r := csv.NewReader(file)
			r.Comma = delim
			r.LazyQuotes = true
			
			// Try reading the first line
			line, err := r.Read()
			// Validation: Must have at least 3 columns AND first column (CFOP) must be short (<= 5 chars)
			if err == nil && len(line) >= 3 && len(strings.TrimSpace(line[0])) <= 5 {
				if seeker, ok := file.(io.Seeker); ok {
					seeker.Seek(0, 0)
				}
				reader = csv.NewReader(file)
				reader.Comma = delim
				reader.LazyQuotes = true
				break
			}
		}

		// Default to semicolon if detection fails
		if reader == nil {
			if seeker, ok := file.(io.Seeker); ok {
				seeker.Seek(0, 0)
			}
			reader = csv.NewReader(file)
			reader.Comma = ';'
			reader.LazyQuotes = true
		}

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, "Database connection error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Fallback: Ensure table exists (in case migration failed)
		_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS cfop (
			cfop VARCHAR(4) PRIMARY KEY,
			descricao_cfop VARCHAR(100) NOT NULL,
			tipo VARCHAR(1) NOT NULL
		)`)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Failed to create table: "+err.Error(), http.StatusInternalServerError)
			return
		}

		stmt, err := tx.Prepare("INSERT INTO cfop (cfop, descricao_cfop, tipo) VALUES ($1, $2, $3) ON CONFLICT (cfop) DO UPDATE SET descricao_cfop = $2, tipo = $3")
		if err != nil {
			tx.Rollback()
			http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer stmt.Close()

		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				tx.Rollback()
				http.Error(w, "Error reading CSV (check format): "+err.Error(), http.StatusBadRequest)
				return
			}
			
			if len(record) < 3 {
				continue 
			}
			
			// Skip header
			if strings.EqualFold(record[0], "CFOP") {
				continue
			}

			cfop := strings.TrimSpace(record[0])
			// Remove BOM (Byte Order Mark) if present - common in Windows files
			cfop = strings.TrimPrefix(cfop, "\ufeff")
			
			descricao := strings.TrimSpace(record[1])
			tipo := strings.TrimSpace(record[2])

			// Clean description: remove '~' and truncate to 100 chars
			descricao = strings.ReplaceAll(descricao, "~", "")
			runes := []rune(descricao)
			if len(runes) > 100 {
				descricao = string(runes[:100])
			}

			// Validate CFOP length to prevent DB error "value too long"
			if len(cfop) > 4 {
				tx.Rollback()
				http.Error(w, "CFOP inválido (muito longo): '"+cfop+"'. Verifique se o delimitador do arquivo é ';'", http.StatusBadRequest)
				return
			}

			_, err = stmt.Exec(cfop, descricao, tipo)
			if err != nil {
				tx.Rollback()
				http.Error(w, "Error inserting CFOP "+cfop+": "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		
		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Import successful"})
	}
}