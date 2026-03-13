package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	FullName string `json:"full_name"`
}

type Company struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	CNPJ    string `json:"cnpj"`
	OwnerID string `json:"owner_id"`
}

func main() {
	connStr := "postgres://postgres:postgres@localhost:5432/fiscal_db?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 1. Check User Gilson
	rows, err := db.Query("SELECT id, email, role, full_name FROM users WHERE email LIKE 'gilson%'")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var gilsonID string
	fmt.Println("--- USERS (Gilson) ---")
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.FullName); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("User: %+v\n", u)
		gilsonID = u.ID
	}

	// 2. Check Companies
	fmt.Println("\n--- COMPANIES ---")
	cRows, err := db.Query("SELECT id, name, cnpj, COALESCE(owner_id::text, 'NULL') FROM companies")
	if err != nil {
		log.Fatal(err)
	}
	defer cRows.Close()

	for cRows.Next() {
		var c Company
		if err := cRows.Scan(&c.ID, &c.Name, &c.CNPJ, &c.OwnerID); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Company: %+v\n", c)
	}

	// 3. Check Environment Links
	if gilsonID != "" {
		fmt.Println("\n--- ENV LINKS ---")
		eRows, err := db.Query(`
			SELECT ue.role, e.name 
			FROM user_environments ue 
			JOIN environments e ON ue.environment_id = e.id 
			WHERE ue.user_id = $1
		`, gilsonID)
		if err != nil {
			log.Fatal(err)
		}
		defer eRows.Close()
		for eRows.Next() {
			var role, envName string
			eRows.Scan(&role, &envName)
			fmt.Printf("Env: %s | Role: %s\n", envName, role)
		}
	}
}
