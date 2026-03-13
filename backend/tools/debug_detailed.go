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

	// --- Query C500 (Energia) ---
	fmt.Println("\n=== C500 (Energia) ===")
	queryC500 := `
		SELECT 
			COALESCE(j.company_name, 'Desconhecida') as filial_nome,
			COALESCE(TO_CHAR(c.dt_doc, 'MM/YYYY'), 'ND') as mes_ano,
			COUNT(*) as qtd_registros,
			COALESCE(SUM(c.vl_doc), 0) as valor_total,
			COALESCE(SUM(c.vl_pis), 0) as pis,
			COALESCE(SUM(c.vl_cofins), 0) as cofins,
			COALESCE(SUM(c.vl_icms), 0) as icms
		FROM reg_c500 c
		JOIN import_jobs j ON j.id = c.job_id
		GROUP BY 1, 2
		ORDER BY 1, 2
	`
	rowsC500, err := db.Query(queryC500)
	if err != nil {
		log.Printf("QUERY C500 ERROR: %v", err)
	} else {
		count := 0
		for rowsC500.Next() {
			count++
			var nome, mes string
			var qtd int
			var v1, v2, v3, v4 float64
			if err := rowsC500.Scan(&nome, &mes, &qtd, &v1, &v2, &v3, &v4); err != nil {
				log.Fatalf("SCAN ERROR: %v", err)
			}
			fmt.Printf("Filial: %s | Mês: %s | Qtd: %d | Valor: %.2f | PIS: %.2f | COFINS: %.2f\n", 
				nome, mes, qtd, v1, v2, v3)
		}
		if count == 0 {
			fmt.Println("Nenhum registro C500 encontrado.")
		}
		rowsC500.Close()
	}

	// --- Query D100 (Transporte) ---
	fmt.Println("\n=== D100 (Transporte) ===")
	queryD100 := `
		SELECT 
			COALESCE(j.company_name, 'Desconhecida') as filial_nome,
			COALESCE(TO_CHAR(d.dt_doc, 'MM/YYYY'), 'ND') as mes_ano,
			d.ind_oper,
			COUNT(*) as qtd_registros,
			COALESCE(SUM(d.vl_doc), 0) as valor_total,
			COALESCE(SUM(d.vl_pis), 0) as pis,
			COALESCE(SUM(d.vl_cofins), 0) as cofins,
			COALESCE(SUM(d.vl_icms), 0) as icms
		FROM reg_d100 d
		JOIN import_jobs j ON j.id = d.job_id
		GROUP BY 1, 2, 3
		ORDER BY 1, 2, 3
	`
	rowsD100, err := db.Query(queryD100)
	if err != nil {
		log.Printf("QUERY D100 ERROR: %v", err)
	} else {
		count := 0
		for rowsD100.Next() {
			count++
			var nome, mes, indOper string
			var qtd int
			var v1, v2, v3, v4 float64
			if err := rowsD100.Scan(&nome, &mes, &indOper, &qtd, &v1, &v2, &v3, &v4); err != nil {
				log.Fatalf("SCAN ERROR: %v", err)
			}
			operDesc := "ENTRADA"
			if indOper == "1" {
				operDesc = "SAIDA"
			}
			fmt.Printf("Filial: %s | Mês: %s | Tipo: %s | Qtd: %d | Valor: %.2f | PIS: %.2f | COFINS: %.2f\n", 
				nome, mes, operDesc, qtd, v1, v2, v3)
		}
		if count == 0 {
			fmt.Println("Nenhum registro D100 encontrado.")
		}
		rowsD100.Close()
	}
}