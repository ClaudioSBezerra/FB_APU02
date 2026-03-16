package main

// FB_APU02 — Apuração Assistida + Receita Federal
// Version: 1.0.0
import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"fb_apu02/handlers"
	"fb_apu02/services"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

const (
	BackendVersion = "1.0.2"
	FeatureSet     = "Apuração Assistida NF-e/CT-e, Receita Federal CBS/IBS, Créditos em Risco, Apelidos de Filiais"
)

func GetVersionInfo() string {
	return fmt.Sprintf("Backend Version: %s | Features: %s", BackendVersion, FeatureSet)
}

func PrintVersion() {
	fmt.Println(GetVersionInfo())
}

type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Service   string `json:"service"`
	Version   string `json:"version"`
	Features  string `json:"features"`
	Database  string `json:"database"`
	DBError   string `json:"db_error,omitempty"`
}

var (
	db      *sql.DB
	dbMutex sync.RWMutex
	dbErr   error
)

func getDB() *sql.DB {
	dbMutex.RLock()
	defer dbMutex.RUnlock()
	return db
}

func initDBAsync() {
	go func() {
		var conn *sql.DB
		var err error
		connStr := os.Getenv("DATABASE_URL")
		if connStr == "" {
			connStr = "postgres://postgres:postgres@localhost:5432/fiscal_db?sslmode=disable"
			fmt.Println("DATABASE_URL not set, using default local connection:", connStr)
		}

		attempt := 0
		for {
			attempt++
			conn, err = sql.Open("postgres", connStr)
			if err == nil {
				err = conn.Ping()
				if err == nil {
					conn.SetMaxOpenConns(50)
					conn.SetMaxIdleConns(15)
					conn.SetConnMaxLifetime(15 * time.Minute)

					dbMutex.Lock()
					db = conn
					dbErr = nil
					dbMutex.Unlock()

					fmt.Println("Successfully connected to the database!")
					onDBConnected()
					return
				}
			}

			dbMutex.Lock()
			dbErr = fmt.Errorf("attempt %d: %v", attempt, err)
			dbMutex.Unlock()

			fmt.Printf("Failed to connect to database (attempt %d): %v. Retrying in 5s...\n", attempt, err)
			time.Sleep(5 * time.Second)
		}
	}()
}

func onDBConnected() {
	database := getDB()

	migrationDir := "migrations"
	if _, err := os.Stat(migrationDir); os.IsNotExist(err) {
		if _, err := os.Stat("backend/migrations"); err == nil {
			migrationDir = "backend/migrations"
		}
	}

	fmt.Printf("Looking for migrations in: %s\n", migrationDir)
	files, err := filepath.Glob(filepath.Join(migrationDir, "*.sql"))
	if err != nil {
		log.Printf("Error finding migration files: %v", err)
	} else {
		var tableExists bool
		_ = database.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name='schema_migrations')`).Scan(&tableExists)

		if !tableExists {
			_, err = database.Exec(`CREATE TABLE schema_migrations (
				filename VARCHAR(255) PRIMARY KEY,
				executed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
			)`)
			if err != nil {
				log.Printf("Warning: Failed to create schema_migrations table: %v", err)
			}
		} else {
			var hasFilename bool
			_ = database.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='schema_migrations' AND column_name='filename')`).Scan(&hasFilename)
			if !hasFilename {
				var oldCol string
				_ = database.QueryRow(`SELECT column_name FROM information_schema.columns WHERE table_name='schema_migrations' ORDER BY ordinal_position LIMIT 1`).Scan(&oldCol)
				if oldCol != "" {
					log.Printf("Renaming schema_migrations column '%s' → 'filename'", oldCol)
					_, renameErr := database.Exec(fmt.Sprintf(`ALTER TABLE schema_migrations RENAME COLUMN %s TO filename`, oldCol))
					if renameErr != nil {
						log.Printf("ERROR: Failed to rename column: %v.", renameErr)
					}
				}
			}

			var colType string
			_ = database.QueryRow(`SELECT data_type FROM information_schema.columns WHERE table_name='schema_migrations' AND column_name='filename'`).Scan(&colType)
			if colType != "" && colType != "character varying" && colType != "text" {
				_, _ = database.Exec(`DROP TABLE schema_migrations`)
				_, _ = database.Exec(`CREATE TABLE schema_migrations (
					filename VARCHAR(255) PRIMARY KEY,
					executed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
				)`)
				log.Println("schema_migrations table recreated with correct schema")
			} else {
				var hasExec bool
				_ = database.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='schema_migrations' AND column_name='executed_at')`).Scan(&hasExec)
				if !hasExec {
					_, _ = database.Exec(`ALTER TABLE schema_migrations ADD COLUMN executed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP`)
				}
			}
		}

		if len(files) == 0 {
			log.Println("Warning: No migration files found!")
		}
		for _, file := range files {
			baseName := filepath.Base(file)
			var alreadyExecuted bool
			errCheck := database.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename=$1)", baseName).Scan(&alreadyExecuted)
			if errCheck != nil {
				log.Printf("Warning: Could not check migration status for %s: %v", baseName, errCheck)
				continue
			}
			if alreadyExecuted {
				continue
			}

			fmt.Printf("Executing migration: %s\n", file)
			migration, err := os.ReadFile(file)
			if err != nil {
				log.Printf("Could not read migration file %s: %v", file, err)
				continue
			}
			_, err = database.Exec(string(migration))
			if err != nil {
				log.Printf("Migration %s warning: %v", file, err)
				if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate") {
					_, _ = database.Exec("INSERT INTO schema_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING", baseName)
				}
			} else {
				fmt.Printf("Migration %s executed successfully.\n", file)
			}
			_, insertErr := database.Exec("INSERT INTO schema_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING", baseName)
			if insertErr != nil {
				log.Printf("Warning: Could not record migration %s: %v", baseName, insertErr)
			}
		}
	}

	// Retenção automática de dfe_xml: apaga XMLs com mais de 5 anos (obrigação fiscal = 5 anos)
	go func() {
		ticker := time.NewTicker(7 * 24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().AddDate(-5, 0, 0)
			res, err := database.Exec(`DELETE FROM dfe_xml WHERE created_at < $1`, cutoff)
			if err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					log.Printf("[Retention] Deleted %d dfe_xml records older than 5 years", n)
				}
			}
		}
	}()
}

func DBMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		database := getDB()
		if database == nil {
			dbMutex.RLock()
			err := dbErr
			dbMutex.RUnlock()
			errMsg := "Database not initialized yet"
			if err != nil {
				errMsg += ": " + err.Error()
			}
			http.Error(w, errMsg, http.StatusServiceUnavailable)
			return
		}
		next(w, r)
	}
}

func main() {
	_ = godotenv.Load()
	PrintVersion()

	initDBAsync()
	go services.StartRFBScheduler(getDB)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		dbStatus := "connecting..."
		database := getDB()
		var dbStats string
		var lastErr string

		if database != nil {
			stats := database.Stats()
			if err := database.Ping(); err != nil {
				dbStatus = "error: " + err.Error()
			} else {
				dbStatus = "connected"
			}
			dbStats = fmt.Sprintf("Open: %d, InUse: %d, Idle: %d, Wait: %v", stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitDuration)
		} else {
			dbMutex.RLock()
			if dbErr != nil {
				dbStatus = "error"
				lastErr = dbErr.Error()
			}
			dbMutex.RUnlock()
		}

		response := HealthResponse{
			Status:    "running",
			Timestamp: time.Now().Format(time.RFC3339),
			Service:   "FB_APU02 Apuração Assistida",
			Version:   BackendVersion,
			Features:  FeatureSet,
			Database:  fmt.Sprintf("%s (%s)", dbStatus, dbStats),
			DBError:   lastErr,
		}
		json.NewEncoder(w).Encode(response)
	})

	withDB := func(handlerFactory func(*sql.DB) http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			database := getDB()
			if database == nil {
				http.Error(w, "Database initializing, please wait...", http.StatusServiceUnavailable)
				return
			}
			handlerFactory(database)(w, r)
		}
	}

	withAuth := func(handlerFactory func(*sql.DB) http.HandlerFunc, role string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			database := getDB()
			if database == nil {
				http.Error(w, "Database initializing...", http.StatusServiceUnavailable)
				return
			}
			h := handlerFactory(database)
			handlers.AuthMiddleware(h, role)(w, r)
		}
	}

	// Filiais Endpoint (global branch selector)
	http.HandleFunc("/api/filiais", withAuth(handlers.GetFiliaisHandler, ""))

	// Auth Routes
	http.HandleFunc("/api/auth/register", withDB(handlers.RegisterHandler))
	http.HandleFunc("/api/auth/login", withDB(handlers.LoginHandler))
	http.HandleFunc("/api/auth/me", withAuth(handlers.GetMeHandler, ""))
	http.HandleFunc("/api/auth/forgot-password", withDB(handlers.ForgotPasswordHandler))
	http.HandleFunc("/api/auth/reset-password", withDB(handlers.ResetPasswordHandler))
	http.HandleFunc("/api/auth/change-password", withAuth(handlers.ChangePasswordHandler, ""))
	http.HandleFunc("/api/user/hierarchy", withAuth(handlers.GetUserHierarchyHandler, ""))
	http.HandleFunc("/api/user/companies", withAuth(handlers.GetUserCompaniesHandler, ""))

	// Admin Endpoints
	http.HandleFunc("/api/admin/reset-db", withAuth(handlers.ResetDatabaseHandler, "admin"))
	http.HandleFunc("/api/admin/limpar-apuracao", withAuth(handlers.LimparDadosApuracaoHandler, "admin"))
	http.HandleFunc("/api/company/reset-data", withAuth(handlers.ResetCompanyDataHandler, ""))
	http.HandleFunc("/api/admin/refresh-views", withAuth(handlers.RefreshViewsHandler, ""))
	http.HandleFunc("/api/admin/users", withAuth(handlers.ListUsersHandler, "admin"))
	http.HandleFunc("/api/admin/users/create", withAuth(handlers.CreateUserHandler, "admin"))
	http.HandleFunc("/api/admin/users/promote", withAuth(handlers.PromoteUserHandler, "admin"))
	http.HandleFunc("/api/admin/users/delete", withAuth(handlers.DeleteUserHandler, "admin"))
	http.HandleFunc("/api/admin/users/reassign", withAuth(handlers.ReassignUserHandler, "admin"))

	// Configuration Endpoints
	http.HandleFunc("/api/config/aliquotas", withDB(handlers.GetTaxRatesHandler))
	http.HandleFunc("/api/config/cfop", withDB(handlers.ListCFOPsHandler))
	http.HandleFunc("/api/config/cfop/import", withDB(handlers.ImportCFOPsHandler))

	http.HandleFunc("/api/config/forn-simples", func(w http.ResponseWriter, r *http.Request) {
		database := getDB()
		if database == nil {
			http.Error(w, "Database initializing...", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			handlers.ListFornSimplesHandler(database)(w, r)
		case http.MethodPost:
			handlers.CreateFornSimplesHandler(database)(w, r)
		case http.MethodDelete:
			handlers.DeleteFornSimplesHandler(database)(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	http.HandleFunc("/api/config/forn-simples/import", withDB(handlers.ImportFornSimplesHandler))

	http.HandleFunc("/api/config/filial-apelidos", withAuth(handlers.FilialApelidosHandler, ""))
	http.HandleFunc("/api/config/filial-apelidos/import", withAuth(handlers.ImportFilialApelidosHandler, ""))

	// Environment & Groups Endpoints
	http.HandleFunc("/api/config/environments", withAuth(func(db *sql.DB) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				handlers.GetEnvironmentsHandler(db)(w, r)
			case http.MethodPost:
				handlers.CreateEnvironmentHandler(db)(w, r)
			case http.MethodPut:
				handlers.UpdateEnvironmentHandler(db)(w, r)
			case http.MethodDelete:
				handlers.DeleteEnvironmentHandler(db)(w, r)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		}
	}, ""))

	http.HandleFunc("/api/config/groups", withAuth(func(db *sql.DB) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				handlers.GetGroupsHandler(db)(w, r)
			case http.MethodPost:
				handlers.CreateGroupHandler(db)(w, r)
			case http.MethodDelete:
				handlers.DeleteGroupHandler(db)(w, r)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		}
	}, ""))

	http.HandleFunc("/api/config/companies", withAuth(func(db *sql.DB) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				handlers.GetCompaniesHandler(db)(w, r)
			case http.MethodPost:
				handlers.CreateCompanyHandler(db)(w, r)
			case http.MethodDelete:
				handlers.DeleteCompanyHandler(db)(w, r)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		}
	}, ""))

	// ── Apuração Assistida + Receita Federal ──

	// RFB Credentials
	http.HandleFunc("/api/rfb/credentials", func(w http.ResponseWriter, r *http.Request) {
		database := getDB()
		if database == nil {
			http.Error(w, "Database initializing...", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodGet:
			handlers.AuthMiddleware(handlers.GetRFBCredentialHandler(database), "")(w, r)
		case http.MethodPost:
			handlers.AuthMiddleware(handlers.SaveRFBCredentialHandler(database), "")(w, r)
		case http.MethodDelete:
			handlers.AuthMiddleware(handlers.DeleteRFBCredentialHandler(database), "")(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	http.HandleFunc("/api/rfb/credentials/agendamento", func(w http.ResponseWriter, r *http.Request) {
		database := getDB()
		if database == nil {
			http.Error(w, "Database initializing...", http.StatusServiceUnavailable)
			return
		}
		handlers.AuthMiddleware(handlers.UpdateRFBScheduleHandler(database), "")(w, r)
	})

	// RFB Apuração
	http.HandleFunc("/api/rfb/apuracao/solicitar", withAuth(handlers.SolicitarApuracaoHandler, ""))
	http.HandleFunc("/api/rfb/apuracao/download", withAuth(handlers.DownloadManualHandler, ""))
	http.HandleFunc("/api/rfb/apuracao/reprocess", withAuth(handlers.ReprocessHandler, ""))
	http.HandleFunc("/api/rfb/apuracao/clear-errors", withAuth(handlers.ClearErrorsHandler, ""))
	http.HandleFunc("/api/rfb/apuracao/status", withAuth(handlers.StatusApuracaoHandler, ""))
	http.HandleFunc("/api/rfb/apuracao/", withAuth(handlers.DetalheApuracaoHandler, ""))

	// RFB Webhook (PUBLIC - no JWT auth)
	http.HandleFunc("/api/rfb/webhook", withDB(handlers.RFBWebhookHandler))

	// NF-e Saídas
	http.HandleFunc("/api/nfe-saidas/upload", withAuth(handlers.NfeSaidasUploadHandler, ""))
	http.HandleFunc("/api/nfe-saidas", withAuth(handlers.NfeSaidasListHandler, ""))

	// NF-e Entradas
	http.HandleFunc("/api/nfe-entradas/upload", withAuth(handlers.NfeEntradasUploadHandler, ""))
	http.HandleFunc("/api/nfe-entradas", withAuth(handlers.NfeEntradasListHandler, ""))

	// CT-e Entradas
	http.HandleFunc("/api/cte-entradas/upload", withAuth(handlers.CteEntradasUploadHandler, ""))
	http.HandleFunc("/api/cte-entradas", withAuth(handlers.CteEntradasListHandler, ""))

	// Créditos em Risco
	http.HandleFunc("/api/apuracao/creditos-perdidos", withAuth(handlers.CreditosPerdidosHandler, ""))

	// Painel Apuração IBS/CBS
	http.HandleFunc("/api/apuracao/painel", withAuth(handlers.ApuracaoPainelHandler, ""))

	// Managers (Gestores para relatórios)
	http.HandleFunc("/api/managers", withAuth(handlers.ListManagersHandler, ""))
	http.HandleFunc("/api/managers/create", withAuth(handlers.CreateManagerHandler, ""))
	http.HandleFunc("/api/managers/", func(w http.ResponseWriter, r *http.Request) {
		database := getDB()
		if database == nil {
			http.Error(w, "Database initializing...", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodPut, http.MethodPatch:
			handlers.AuthMiddleware(handlers.UpdateManagerHandler(database), "")(w, r)
		case http.MethodDelete:
			handlers.AuthMiddleware(handlers.DeleteManagerHandler(database), "")(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// DANFE generation
	http.HandleFunc("/api/danfe/", withAuth(handlers.DanfeHandler, ""))

	// Serve frontend static files (SPA — React Router)
	// index.html: no-cache para que o browser sempre busque a versão atual após deploy.
	// Assets com hash (JS/CSS): cache longo — o hash muda a cada build automaticamente.
	staticDir := "./static"
	if _, err := os.Stat(staticDir); err == nil {
		fs := http.FileServer(http.Dir(staticDir))
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			filePath := filepath.Join(staticDir, filepath.Clean(r.URL.Path))
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				// SPA fallback → index.html nunca cacheado
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
				w.Header().Set("Expires", "0")
				http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
				return
			}
			// index.html direto também não deve ser cacheado
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
				w.Header().Set("Expires", "0")
			}
			fs.ServeHTTP(w, r)
		})
		fmt.Println("Serving frontend from ./static")
	}

	fmt.Printf("FB_APU02 Apuração Assistida (Go) starting on port %s...\n", port)
	fmt.Println("==================================================")
	fmt.Printf("   FB_APU02 BACKEND - %s\n", BackendVersion)
	fmt.Println("==================================================")

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      nil,
		ReadTimeout:  300 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down gracefully...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}

		database := getDB()
		if database != nil {
			log.Println("Closing database connections...")
			database.Close()
		}

		log.Println("Shutdown complete.")
		os.Exit(0)
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
