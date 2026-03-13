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
	// Configuração da conexão
	connStr := "postgres://postgres:postgres@localhost:5432/fiscal_db?sslmode=disable"
	if envUrl := os.Getenv("DATABASE_URL"); envUrl != "" {
		connStr = envUrl
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Erro ao conectar no DB: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Erro ao pingar DB: %v", err)
	}

	fmt.Println("--- Diagnóstico do Banco de Dados ---")

	// 1. Verificar Jobs
	var jobCount int
	db.QueryRow("SELECT count(*) FROM import_jobs").Scan(&jobCount)
	fmt.Printf("Total de Jobs: %d\n", jobCount)

	rows, err := db.Query("SELECT id, filename, status, created_at, message FROM import_jobs ORDER BY created_at DESC LIMIT 3")
	if err != nil {
		log.Printf("Erro ao listar jobs: %v", err)
	} else {
		defer rows.Close()
		fmt.Println("\nÚltimos 3 Jobs:")
		for rows.Next() {
			var id, filename, status, createdAt string
			var message sql.NullString
			rows.Scan(&id, &filename, &status, &createdAt, &message)
			msg := "N/A"
			if message.Valid { msg = message.String }
			fmt.Printf("- [%s] %s (%s)\n  Msg: %s\n", status, filename, createdAt, msg)
		}
	}

	// 2. Contar Registros das Tabelas SPED
	tables := map[string]string{
		"Participantes (0150)": "participants",
		"Estabelecimentos (0140)": "reg_0140",
		"NFe (C100)": "reg_c100",
		"Energia (C500)": "reg_c500",
		"Consumo (C600)": "reg_c600",
		"Transporte (D100)": "reg_d100",
	}

	fmt.Println("\nContagem de Registros SPED:")
	// Iterar em ordem fixa para consistência visual se possível, mas map é aleatório.
	// Para debug rápido, a ordem aleatória não é crítica, mas vou fazer manual para ficar bonito.
	orderedLabels := []string{"Participantes (0150)", "Estabelecimentos (0140)", "NFe (C100)", "Energia (C500)", "Consumo (C600)", "Transporte (D100)"}
	
	for _, label := range orderedLabels {
		table := tables[label]
		var count int
		err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count)
		if err != nil {
			fmt.Printf("- %s: Erro (%v)\n", label, err)
		} else {
			fmt.Printf("- %s: %d\n", label, count)
		}
	}
    fmt.Println("-------------------------------------")
}