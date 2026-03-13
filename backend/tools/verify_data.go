//go:build scripts

package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	connStr := "postgres://postgres:postgres@localhost:5432/fb_apu01?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Check reg_c100
	rows, err := db.Query("SELECT id, ind_oper, dt_doc FROM reg_c100")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("reg_c100 data:")
	for rows.Next() {
		var id string
		var indOper string
		var dtDoc sql.NullString
		if err := rows.Scan(&id, &indOper, &dtDoc); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("ID: %s, Oper: %s, Date: %v\n", id, indOper, dtDoc)
	}
}