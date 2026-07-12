package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"fb_apu02/crypto"
	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
)

// SAPCredential representa a credencial OAuth2 de acesso à API SAP de uma empresa.
// ClientSecret nunca é exposto — apenas ClientSecretSet indica se já foi cadastrado.
type SAPCredential struct {
	ID              string    `json:"id"`
	CompanyID       string    `json:"company_id"`
	ClientID        string    `json:"client_id"`
	ClientSecretSet bool      `json:"client_secret_set"`
	BaseURL         string    `json:"base_url"`
	BukrsList       []string  `json:"bukrs_list"`
	Ativo           bool      `json:"ativo"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// GetSAPCredentialHandler retorna a credencial SAP da empresa efetiva, sem o client_secret.
func GetSAPCredentialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[SAPCredentials]")
			return
		}

		var cred SAPCredential
		var clientSecret string
		var baseURL sql.NullString
		var bukrsList []string
		err = db.QueryRow(`
			SELECT id, company_id, client_id, client_secret,
			       COALESCE(base_url, ''), bukrs_list, ativo, created_at, updated_at
			FROM sap_credentials WHERE company_id = $1
		`, companyID).Scan(
			&cred.ID, &cred.CompanyID, &cred.ClientID, &clientSecret,
			&baseURL, pq.Array(&bukrsList), &cred.Ativo, &cred.CreatedAt, &cred.UpdatedAt,
		)
		if err == sql.ErrNoRows {
			json.NewEncoder(w).Encode(map[string]interface{}{"credential": nil})
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar credencial SAP", err, "[SAPCredentials]")
			return
		}

		cred.BaseURL = baseURL.String
		cred.BukrsList = bukrsList
		cred.ClientSecretSet = clientSecret != ""

		json.NewEncoder(w).Encode(map[string]interface{}{"credential": cred})
	}
}

// SaveSAPCredentialHandler cadastra ou atualiza (UPSERT) a credencial SAP da empresa efetiva.
// client_secret só é atualizado se enviado (permite editar base_url/bukrs_list sem reenviá-lo)
// e é sempre criptografado antes de persistir.
func SaveSAPCredentialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[SAPCredentials]")
			return
		}

		var req struct {
			ClientID     string   `json:"client_id"`
			ClientSecret string   `json:"client_secret"`
			BaseURL      string   `json:"base_url"`
			BukrsList    []string `json:"bukrs_list"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "Dados inválidos")
			return
		}

		req.ClientID = strings.TrimSpace(req.ClientID)
		req.ClientSecret = strings.TrimSpace(req.ClientSecret)
		req.BaseURL = strings.TrimSpace(req.BaseURL)

		seenBukrs := make(map[string]bool, len(req.BukrsList))
		cleanBukrs := make([]string, 0, len(req.BukrsList))
		for _, b := range req.BukrsList {
			b = strings.TrimSpace(b)
			if b != "" && !seenBukrs[b] {
				seenBukrs[b] = true
				cleanBukrs = append(cleanBukrs, b)
			}
		}

		if req.ClientID == "" {
			jsonErr(w, http.StatusBadRequest, "Client ID é obrigatório")
			return
		}
		if len(cleanBukrs) == 0 {
			jsonErr(w, http.StatusBadRequest, "Ao menos um código de empresa SAP (BUKRS) é obrigatório")
			return
		}
		if req.BaseURL != "" && !strings.HasPrefix(req.BaseURL, "http://") && !strings.HasPrefix(req.BaseURL, "https://") {
			jsonErr(w, http.StatusBadRequest, "Base URL deve começar com http:// ou https://")
			return
		}

		// Lê o secret existente e grava dentro da mesma transação (com bloqueio de linha)
		// para evitar lost update entre requisições concorrentes na mesma empresa.
		tx, err := db.Begin()
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao iniciar transação", err, "[SAPCredentials]")
			return
		}
		defer tx.Rollback()

		var existingSecret sql.NullString
		err = tx.QueryRow(`SELECT client_secret FROM sap_credentials WHERE company_id = $1 FOR UPDATE`, companyID).Scan(&existingSecret)
		hasExisting := err == nil
		if err != nil && err != sql.ErrNoRows {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar credencial SAP", err, "[SAPCredentials]")
			return
		}
		if req.ClientSecret == "" && !hasExisting {
			jsonErr(w, http.StatusBadRequest, "Client Secret é obrigatório")
			return
		}

		encryptedSecret := existingSecret.String
		if req.ClientSecret != "" {
			enc, encErr := crypto.EncryptField(req.ClientSecret)
			if encErr != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao proteger credencial", encErr, "[SAPCredentials]")
				return
			}
			encryptedSecret = enc
		}

		_, err = tx.Exec(`
			INSERT INTO sap_credentials (company_id, client_id, client_secret, base_url, bukrs_list, ativo)
			VALUES ($1, $2, $3, $4, $5, true)
			ON CONFLICT (company_id)
			DO UPDATE SET client_id = $2, client_secret = $3, base_url = $4, bukrs_list = $5, ativo = true, updated_at = NOW()
		`, companyID, req.ClientID, encryptedSecret, req.BaseURL, pq.Array(cleanBukrs))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao salvar credencial SAP", err, "[SAPCredentials]")
			return
		}

		if err := tx.Commit(); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao confirmar transação", err, "[SAPCredentials]")
			return
		}

		log.Printf("[SAPCredentials] Credencial SAP salva por userID=%s companyID=%s (nova=%v)", userID, companyID, !hasExisting)

		if hasExisting {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Credenciais SAP salvas com sucesso",
		})
	}
}

// TestSAPConnectionHandler testa a conexão OAuth2 com o SAP usando a credencial
// salva da empresa efetiva. Nunca persiste dado de negócio (somente leitura de
// sap_credentials) e nunca expõe o client_secret decriptado nem detalhes
// internos de erro na resposta HTTP.
func TestSAPConnectionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[SAPCredentials]")
			return
		}

		var clientID, clientSecretEnc string
		var baseURL sql.NullString
		err = db.QueryRow(`
			SELECT client_id, client_secret, base_url FROM sap_credentials WHERE company_id = $1
		`, companyID).Scan(&clientID, &clientSecretEnc, &baseURL)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusBadRequest, "Nenhuma credencial SAP cadastrada para esta empresa")
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar credencial SAP", err, "[SAPCredentials]")
			return
		}
		if !baseURL.Valid || baseURL.String == "" {
			jsonErr(w, http.StatusBadRequest, "Base URL não configurada para esta empresa")
			return
		}

		clientSecret, err := crypto.DecryptField(clientSecretEnc)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao processar credencial", err, "[SAPCredentials]")
			return
		}

		if _, err := services.GetToken(baseURL.String, clientID, clientSecret); err != nil {
			log.Printf("[SAPCredentials] Teste de conexão falhou para companyID=%s: %v", companyID, err)
			jsonErr(w, http.StatusBadGateway, "Falha ao conectar com o SAP — verifique as credenciais e a Base URL")
			return
		}

		log.Printf("[SAPCredentials] Teste de conexão bem-sucedido para companyID=%s", companyID)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Conexão bem-sucedida",
		})
	}
}
