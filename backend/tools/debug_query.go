//go:build scripts

package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/fiscal_db?sslmode=disable"
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("Cannot connect to DB:", err)
	}
	fmt.Println("Connected to DB successfully.")

	// Query simplificada (só C100)
	query := `
			SELECT 
				COALESCE(j.company_name, 'Desconhecida') as filial_nome,
				COALESCE(TO_CHAR(c.dt_doc, 'MM/YYYY'), 'ND') as mes_ano,
				CASE WHEN c.ind_oper = '0' THEN 'ENTRADA' ELSE 'SAIDA' END as tipo,
				COALESCE(SUM(c.vl_doc), 0) as valor,
				COALESCE(SUM(c.vl_pis), 0) as pis,
				COALESCE(SUM(c.vl_cofins), 0) as cofins,
				COALESCE(SUM(c.vl_icms), 0) as icms
			FROM reg_c100 c
			JOIN import_jobs j ON j.id = c.job_id
			GROUP BY 1, 2, 3
	`

	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf("QUERY ERROR: %v", err)
	}
	defer rows.Close()

	fmt.Println("Query executed successfully. Rows found:")
	count := 0
	for rows.Next() {
		count++
		var nome, mes, tipo string
		var v1, v2, v3, v4 float64
		if err := rows.Scan(&nome, &mes, &tipo, &v1, &v2, &v3, &v4); err != nil {
			log.Fatalf("SCAN ERROR: %v", err)
		}
		fmt.Printf("Row %d: %s | %s | %s\n", count, nome, mes, tipo)
	}
	fmt.Printf("Total rows: %d\n", count)
}