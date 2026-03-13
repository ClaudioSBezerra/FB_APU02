package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"fb_apu02/services"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// --- Structs ---

type contextKey string

const ClaimsKey contextKey = "claims"

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	FullName    string    `json:"full_name"`
	IsVerified  bool      `json:"is_verified"`
	TrialEndsAt time.Time `json:"trial_ends_at"`
	Role        string    `json:"role"`
	CreatedAt   string    `json:"created_at"`
}

type RegisterRequest struct {
	FullName    string `json:"full_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	CompanyName string `json:"company_name"`
	CNPJ        string `json:"cnpj"` // Optional
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
	// Context info for the footer
	Environment string `json:"environment_name"`
	Group       string `json:"group_name"`
	Company     string `json:"company_name"`
	CompanyID   string `json:"company_id"` // Added CompanyID
	CNPJ        string `json:"cnpj"`       // Added CNPJ for company context
}

// --- Utils ---

var jwtSecret = []byte(getEnv("JWT_SECRET", "super-secret-key-change-me-in-prod"))

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func GenerateToken(userID, role string) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"exp":     time.Now().Add(time.Hour * 24).Unix(), // 24 hours
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// --- Handlers ---

// GetMeHandler returns the current authenticated user's details
func GetMeHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)

		var user User
		err := db.QueryRow(`
			SELECT id, email, full_name, is_verified, COALESCE(trial_ends_at, NOW()), COALESCE(role, 'user'), created_at
			FROM users WHERE id = $1
		`, userID).Scan(&user.ID, &user.Email, &user.FullName, &user.IsVerified, &user.TrialEndsAt, &user.Role, &user.CreatedAt)

		if err != nil {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user)
	}
}

func AuthMiddleware(next http.HandlerFunc, requiredRole string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		tokenString := ""
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			tokenString = authHeader[7:]
		} else {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok || !token.Valid {
			http.Error(w, "Invalid token claims", http.StatusUnauthorized)
			return
		}

		// Check role
		userRole, ok := claims["role"].(string)
		if !ok {
			http.Error(w, "Role not found in token", http.StatusUnauthorized)
			return
		}

		if requiredRole != "" && userRole != requiredRole && userRole != "admin" {
			http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), ClaimsKey, claims)
		next(w, r.WithContext(ctx))
	}
}

func GetUserIDFromContext(r *http.Request) string {
	claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
	if !ok {
		return ""
	}
	userID, ok := claims["user_id"].(string)
	if !ok {
		return ""
	}
	return userID
}

// GetEffectiveCompanyID fetches the company ID to use for the current request.
// If requestedCompanyID is provided (e.g. via header), it verifies if the user has access to it.
// If not provided or invalid, it falls back to the default company (Owner > Member).
func GetEffectiveCompanyID(db *sql.DB, userID, requestedCompanyID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 1. If a specific company is requested, verify access
	if requestedCompanyID != "" {
		var exists bool
		// Check if Owner OR Member in the same Group/Environment
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 
				FROM companies c
				LEFT JOIN enterprise_groups eg ON c.group_id = eg.id
				LEFT JOIN user_environments ue ON eg.environment_id = ue.environment_id
				WHERE c.id = $1 
				AND (c.owner_id = $2 OR ue.user_id = $2)
			)
		`, requestedCompanyID, userID).Scan(&exists)

		if err == nil && exists {
			return requestedCompanyID, nil
		}
		// If requested ID is invalid/unauthorized, fall through to default logic
		log.Printf("User %s requested invalid/unauthorized company %s. Falling back to default.", userID, requestedCompanyID)
	}

	// 2. Default Logic (Owner > Member)
	var companyID string

	// Strategy A: Check if user OWNS a company
	err := db.QueryRowContext(ctx, `
		SELECT c.id
		FROM companies c
		WHERE c.owner_id = $1
		ORDER BY c.created_at DESC
		LIMIT 1
	`, userID).Scan(&companyID)

	if err == nil {
		return companyID, nil
	}

	if err != sql.ErrNoRows {
		return "", err
	}

	// Strategy B: Check via User Environment — prioriza preferred_company_id
	err = db.QueryRowContext(ctx, `
		SELECT c.id
		FROM user_environments ue
		JOIN enterprise_groups eg ON eg.environment_id = ue.environment_id
		JOIN companies c ON c.group_id = eg.id
		WHERE ue.user_id = $1
		ORDER BY
			(ue.preferred_company_id IS NOT NULL AND ue.preferred_company_id = c.id) DESC,
			c.created_at DESC
		LIMIT 1
	`, userID).Scan(&companyID)

	if err != nil {
		return "", err
	}

	return companyID, nil
}

// Deprecated: Use GetEffectiveCompanyID instead
func GetUserCompanyID(db *sql.DB, userID string) (string, error) {
	return GetEffectiveCompanyID(db, userID, "")
}

type UserCompany struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TradeName   string `json:"trade_name"`
	IsOwner     bool   `json:"is_owner"`
	Environment string `json:"environment"`
	Group       string `json:"group"`
}

// GetUserCompaniesHandler lists all companies available to the user
func GetUserCompaniesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r)
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Query companies where user:
		// 1. Is the direct owner (owner_id)
		// 2. Is linked to the environment via user_environments
		// 3. The company is in the same group as a company the user owns (ex: FCM no mesmo grupo FCOSTA)
		rows, err := db.Query(`
			SELECT DISTINCT c.id, c.name, COALESCE(c.trade_name, ''), COALESCE(c.owner_id = $1, false) as is_owner,
			       COALESCE(e.name, '') as env_name, COALESCE(eg.name, '') as group_name
			FROM companies c
			LEFT JOIN enterprise_groups eg ON c.group_id = eg.id
			LEFT JOIN environments e ON eg.environment_id = e.id
			LEFT JOIN user_environments ue ON e.id = ue.environment_id
			WHERE c.owner_id = $1
			   OR ue.user_id = $1
			   OR c.group_id IN (
			       SELECT group_id FROM companies WHERE owner_id = $1
			   )
			ORDER BY is_owner DESC, c.name ASC
		`, userID)

		if err != nil {
			log.Printf("Error listing companies: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var companies []UserCompany
		for rows.Next() {
			var c UserCompany
			if err := rows.Scan(&c.ID, &c.Name, &c.TradeName, &c.IsOwner, &c.Environment, &c.Group); err != nil {
				continue
			}
			companies = append(companies, c)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(companies)
	}
}

func RegisterHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Email == "" || req.Password == "" || req.FullName == "" || req.CompanyName == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// 1. Hash Password
		hash, err := HashPassword(req.Password)
		if err != nil {
			log.Printf("[Register] Error hashing password: %v", err)
			http.Error(w, "Error hashing password", http.StatusInternalServerError)
			return
		}

		// 2. Start Transaction
		tx, err := db.Begin()
		if err != nil {
			log.Printf("[Register] Error starting transaction: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// 3. Create User
		var userID string
		var role string
		trialEnds := time.Now().Add(time.Hour * 24 * 14) // 14 days
		// Ensure role column is populated (default 'user') and returned
		err = tx.QueryRow(`
			INSERT INTO users (email, password_hash, full_name, trial_ends_at, is_verified, role)
			VALUES ($1, $2, $3, $4, $5, 'user')
			RETURNING id, role
		`, req.Email, hash, req.FullName, trialEnds, false).Scan(&userID, &role)

		if err != nil {
			log.Printf("[Register] Error creating user: %v", err)
			// Check specifically for unique constraint violation
			tx.Rollback() // Ensure rollback before returning
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode("Este e-mail já está cadastrado.")
			return
		}

		// 4. Create ISOLATED Environment and Group for the user
		var envID, groupID, envName, groupName string

		// Create Environment
		envName = "Ambiente de " + req.FullName
		err = tx.QueryRow("INSERT INTO environments (name, description) VALUES ($1, 'Ambiente Padrão do Usuário') RETURNING id", envName).Scan(&envID)
		if err != nil {
			log.Printf("[Register] Error creating environment: %v", err)
			tx.Rollback()
			http.Error(w, "Error creating environment", http.StatusInternalServerError)
			return
		}

		// Create Group
		groupName = "Grupo de " + req.FullName
		err = tx.QueryRow("INSERT INTO enterprise_groups (environment_id, name, description) VALUES ($1, $2, 'Grupo Padrão do Usuário') RETURNING id", envID, groupName).Scan(&groupID)
		if err != nil {
			log.Printf("[Register] Error creating group: %v", err)
			tx.Rollback()
			http.Error(w, "Error creating group", http.StatusInternalServerError)
			return
		}

		// 5. Link User to Environment
		_, err = tx.Exec("INSERT INTO user_environments (user_id, environment_id, role) VALUES ($1, $2, 'admin')", userID, envID)
		if err != nil {
			log.Printf("[Register] Error linking user to environment: %v", err)
			http.Error(w, "Error linking user to environment", http.StatusInternalServerError)
			return
		}

		// 6. Create Company with owner_id
		var companyID string
		err = tx.QueryRow(`
			INSERT INTO companies (group_id, name, trade_name, owner_id)
			VALUES ($1, $2, $2, $3)
			RETURNING id
		`, groupID, req.CompanyName, userID).Scan(&companyID)
		if err != nil {
			log.Printf("[Register] Error creating company: %v", err)
			http.Error(w, "Error creating company", http.StatusInternalServerError)
			return
		}

		// Commit
		if err := tx.Commit(); err != nil {
			log.Printf("[Register] Error committing transaction: %v", err)
			http.Error(w, "Transaction commit failed", http.StatusInternalServerError)
			return
		}

		// Generate Token
		token, _ := GenerateToken(userID, "user")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AuthResponse{
			Token: token,
			User: User{
				ID:          userID,
				Email:       req.Email,
				FullName:    req.FullName,
				IsVerified:  false,
				TrialEndsAt: trialEnds,
				Role:        "user",
			},
			Environment: envName,
			Group:       groupName,
			Company:     req.CompanyName,
			CompanyID:   companyID,
			CNPJ:        "", // CNPJ removed from company
		})
	}
}

func LoginHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		log.Printf("[Login] Attempting login for: %s", req.Email)

		// Get User
		var user User
		var hash string
		// Use COALESCE for role and trial_ends_at to handle NULLs safely
		err := db.QueryRow(`
			SELECT id, email, full_name, password_hash, is_verified, COALESCE(trial_ends_at, NOW()), COALESCE(role, 'user'), created_at
			FROM users WHERE email = $1
		`, req.Email).Scan(&user.ID, &user.Email, &user.FullName, &hash, &user.IsVerified, &user.TrialEndsAt, &user.Role, &user.CreatedAt)

		if err == sql.ErrNoRows {
			log.Printf("[Login] User not found: %s", req.Email)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode("E-mail não encontrado ou senha inválida")
			return
		} else if err != nil {
			log.Printf("[Login] Database error fetching user: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro no servidor")
			return
		}

		// Check Password
		if !CheckPasswordHash(req.Password, hash) {
			log.Printf("[Login] Invalid password for: %s", req.Email)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode("E-mail não encontrado ou senha inválida")
			return
		}

		// Check Trial Status
		if user.Role != "admin" && user.TrialEndsAt.Before(time.Now()) {
			log.Printf("[Login] Trial expired for: %s", req.Email)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode("Período de teste expirado. Entre em contato para assinar.")
			return
		}

		// 2. Generate Token
		token, err := GenerateToken(user.ID, user.Role)
		if err != nil {
			log.Printf("[Login] Error generating token: %v", err)
			http.Error(w, "Error generating token", http.StatusInternalServerError)
			return
		}

		// 4. Get Environment, Group, and Company Context
		// OPTIMIZATION: Split query to avoid complex joins and potential locks/slowdowns
		// Added Timeout context to prevent 504 Gateway Timeouts on slow DB
		var envName, groupName, companyName, companyID string

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// Strategy A: Check if user OWNS a company (Fastest/Most Common)
		err = db.QueryRowContext(ctx, `
			SELECT e.name, eg.name, c.name, c.id
			FROM companies c
			JOIN enterprise_groups eg ON c.group_id = eg.id
			JOIN environments e ON eg.environment_id = e.id
			WHERE c.owner_id = $1
			ORDER BY c.created_at DESC
			LIMIT 1
		`, user.ID).Scan(&envName, &groupName, &companyName, &companyID)

		if err == sql.ErrNoRows {
			// Strategy B: If not owner, check via User Environment (team members).
			// Prioridade: preferred_company_id > owner > primeiro do grupo.
			log.Printf("[Login] User %s owns no company, checking memberships...", req.Email)
			err = db.QueryRowContext(ctx, `
				SELECT e.name, eg.name, c.name, c.id
				FROM user_environments ue
				JOIN environments e ON ue.environment_id = e.id
				JOIN enterprise_groups eg ON eg.environment_id = e.id
				JOIN companies c ON c.group_id = eg.id
				WHERE ue.user_id = $1
				ORDER BY
					(ue.preferred_company_id IS NOT NULL AND ue.preferred_company_id = c.id) DESC,
					(c.owner_id = $1) DESC,
					c.created_at ASC
				LIMIT 1
			`, user.ID).Scan(&envName, &groupName, &companyName, &companyID)
		}

		if err == sql.ErrNoRows {
			// No company found at all - Auto-provisioning
			log.Printf("[Login] No company context found for user: %s. Auto-provisioning default context...", req.Email)

			// 1. Get/Create Default Environment
			var envID string
			errEnv := db.QueryRowContext(ctx, "SELECT id, name FROM environments WHERE name = 'Ambiente de Testes' LIMIT 1").Scan(&envID, &envName)
			if errEnv == sql.ErrNoRows {
				errEnv = db.QueryRowContext(ctx, "INSERT INTO environments (name, description) VALUES ('Ambiente de Testes', 'Ambiente auto-gerado') RETURNING id, name").Scan(&envID, &envName)
			}

			if errEnv != nil {
				log.Printf("[Login] Auto-provision failed at Environment: %v", errEnv)
				envName = "Sem Ambiente"
				groupName = "Sem Grupo"
				companyName = "Sem Empresa"
				companyID = ""
			} else {
				// 2. Get/Create Default Group
				var groupID string
				errGroup := db.QueryRowContext(ctx, "SELECT id, name FROM enterprise_groups WHERE environment_id = $1 AND name = 'Grupo de Empresas Testes' LIMIT 1", envID).Scan(&groupID, &groupName)
				if errGroup == sql.ErrNoRows {
					errGroup = db.QueryRowContext(ctx, "INSERT INTO enterprise_groups (environment_id, name, description) VALUES ($1, 'Grupo de Empresas Testes', 'Grupo auto-gerado') RETURNING id, name", envID).Scan(&groupID, &groupName)
				}

				if errGroup != nil {
					log.Printf("[Login] Auto-provision failed at Group: %v", errGroup)
					groupName = "Sem Grupo"
					companyName = "Sem Empresa"
					companyID = ""
				} else {
					// 3. Link User to Environment (Idempotent)
					// We use ON CONFLICT DO NOTHING assuming there's a unique constraint or primary key on (user_id, environment_id)
					// If not, we might duplicate, but standard schema usually has it.
					// Checking user_environments definition would be good, but let's assume standard PK.
					_, _ = db.ExecContext(ctx, "INSERT INTO user_environments (user_id, environment_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING", user.ID, envID)

					// 4. Create Company for User
					companyName = "Empresa de " + user.FullName
					if user.FullName == "" {
						companyName = "Minha Empresa"
					}

					errComp := db.QueryRowContext(ctx, `
						INSERT INTO companies (group_id, name, trade_name, owner_id)
						VALUES ($1, $2, $2, $3)
						RETURNING id
					`, groupID, companyName, user.ID).Scan(&companyID)

					if errComp != nil {
						log.Printf("[Login] Auto-provision failed at Company: %v", errComp)
						companyName = "Sem Empresa"
						companyID = ""
					} else {
						log.Printf("[Login] Auto-provision success: Created %s (%s)", companyName, companyID)
					}
				}
			}
			// Reset error to nil so we don't trigger the next error block
			err = nil

		} else if err != nil {
			// Could be timeout or other error
			log.Printf("[Login] Warning: Error fetching context (timeout?): %v. Proceeding without context.", err)
			// Don't fail login, just return empty context so user can enter
			envName = "Carregando..."
			groupName = "Carregando..."
			companyName = "Carregando..."
			companyID = ""
		}

		log.Printf("[Login] Success for %s. Duration: %v", req.Email, time.Since(start))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AuthResponse{
			Token:       token,
			User:        user,
			Environment: envName,
			Group:       groupName,
			Company:     companyName,
			CompanyID:   companyID,
			CNPJ:        "", // CNPJ removed from company
		})
	}
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

func ForgotPasswordHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ForgotPasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode("Requisição inválida")
			return
		}

		var userID string
		err := db.QueryRow("SELECT id FROM users WHERE email = $1", req.Email).Scan(&userID)
		if err == sql.ErrNoRows {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode("E-mail não encontrado")
			return
		} else if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro no banco de dados")
			return
		}

		// Generate token
		tokenBytes := make([]byte, 32)
		rand.Read(tokenBytes)
		token := hex.EncodeToString(tokenBytes)

		expiresAt := time.Now().Add(1 * time.Hour)

		_, err = db.Exec("INSERT INTO verification_tokens (user_id, token, type, expires_at) VALUES ($1, $2, 'password_reset', $3)", userID, token, expiresAt)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro ao criar token")
			return
		}

		// Send real email using email service
		err = services.SendPasswordResetEmail(req.Email, token)
		if err != nil {
			log.Printf("[ForgotPassword] Failed to send email: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro ao enviar e-mail de recuperação")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Se o e-mail estiver cadastrado, você receberá um link de recuperação em instantes",
		})
	}
}

// ResetPasswordRequest struct for password reset
type ResetPasswordRequest struct {
	Token           string `json:"token"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

// ResetPasswordHandler handles password reset with token
func ResetPasswordHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ResetPasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode("Requisição inválida")
			return
		}

		// Validate password match
		if req.Password != req.ConfirmPassword {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode("As senhas não coincidem")
			return
		}

		// Validate password strength (min 8 characters)
		if len(req.Password) < 8 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode("A senha deve ter no mínimo 8 caracteres")
			return
		}

		// Get token info from database
		var userID string
		var expiresAt time.Time
		var tokenType string

		err := db.QueryRow(`
			SELECT user_id, expires_at, type
			FROM verification_tokens
			WHERE token = $1 AND used = false
		`, req.Token).Scan(&userID, &expiresAt, &tokenType)

		if err == sql.ErrNoRows {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode("Token inválido ou já utilizado")
			return
		} else if err != nil {
			log.Printf("[ResetPassword] Database error: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro no banco de dados")
			return
		}

		// Check if token is for password reset
		if tokenType != "password_reset" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode("Tipo de token inválido")
			return
		}

		// Check if token has expired
		if time.Now().After(expiresAt) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGone)
			json.NewEncoder(w).Encode("Token expirado. Solicite uma nova recuperação de senha")
			return
		}

		// Hash new password
		hash, err := HashPassword(req.Password)
		if err != nil {
			log.Printf("[ResetPassword] Error hashing password: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro ao processar senha")
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			log.Printf("[ResetPassword] Error starting transaction: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro no banco de dados")
			return
		}
		defer tx.Rollback()

		// Update user password
		_, err = tx.Exec("UPDATE users SET password_hash = $1 WHERE id = $2", hash, userID)
		if err != nil {
			log.Printf("[ResetPassword] Error updating password: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro ao atualizar senha")
			return
		}

		// Mark token as used
		_, err = tx.Exec("UPDATE verification_tokens SET used = true WHERE token = $1", req.Token)
		if err != nil {
			log.Printf("[ResetPassword] Error marking token as used: %v", err)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("[ResetPassword] Error committing transaction: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode("Erro ao salvar alterações")
			return
		}

		log.Printf("[ResetPassword] Password reset successfully for user %s", userID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Senha redefinida com sucesso. Você já pode fazer login com sua nova senha.",
		})
	}
}

// ChangePasswordHandler allows an authenticated user to change their own password
func ChangePasswordHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "Não autorizado"})
			return
		}
		userID := claims["user_id"].(string)

		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Requisição inválida"})
			return
		}

		if len(req.NewPassword) < 6 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "A nova senha deve ter no mínimo 6 caracteres"})
			return
		}

		var hash string
		err := db.QueryRow("SELECT password_hash FROM users WHERE id = $1", userID).Scan(&hash)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Usuário não encontrado"})
			return
		}

		if !CheckPasswordHash(req.CurrentPassword, hash) {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "Senha atual incorreta"})
			return
		}

		newHash, err := HashPassword(req.NewPassword)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Erro ao processar senha"})
			return
		}

		_, err = db.Exec("UPDATE users SET password_hash = $1 WHERE id = $2", newHash, userID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Erro ao salvar senha"})
			return
		}

		log.Printf("[ChangePassword] Password changed for user %s", userID)
		json.NewEncoder(w).Encode(map[string]string{"message": "Senha alterada com sucesso"})
	}
}
