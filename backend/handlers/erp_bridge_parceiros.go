package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// ERPBridgeParceirosSyncHandler — POST /api/erp-bridge/parceiros/sync
//
// Autenticado via X-API-Key (igual /api/erp-bridge/import/batch).
// Recebe lista de {cnpj, nome} e faz upsert na tabela parceiros,
// sempre atualizando o nome (fonte de verdade: Oracle FORN/CLIE).
// ---------------------------------------------------------------------------

type parceiroSyncItem struct {
	CNPJ string `json:"cnpj"`
	Nome string `json:"nome"`
}

type parceirosSyncRequest struct {
	Parceiros []parceiroSyncItem `json:"parceiros"`
}

type parceirosSyncResult struct {
	Upserted int `json:"upserted"`
}

func ERPBridgeParceirosSyncHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		// ── Auth via X-API-Key ────────────────────────────────────────────────
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			http.Error(w, `{"error":"X-API-Key obrigatório"}`, http.StatusUnauthorized)
			return
		}
		hash := sha256.Sum256([]byte(apiKey))
		hashHex := hex.EncodeToString(hash[:])

		var companyID string
		err := db.QueryRow(
			`SELECT company_id FROM erp_bridge_config WHERE api_key_hash = $1`, hashHex,
		).Scan(&companyID)
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"API key inválida"}`, http.StatusUnauthorized)
			return
		}
		if err != nil {
			log.Printf("[ParceirosSync] db error auth: %v", err)
			http.Error(w, `{"error":"erro interno"}`, http.StatusInternalServerError)
			return
		}

		// ── Parse body ────────────────────────────────────────────────────────
		var req parceirosSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"JSON inválido"}`, http.StatusBadRequest)
			return
		}

		upserted := 0
		for _, p := range req.Parceiros {
			cnpj := strings.TrimSpace(p.CNPJ)
			nome := strings.TrimSpace(p.Nome)
			if cnpj == "" {
				continue
			}
			_, err := db.Exec(`
				INSERT INTO parceiros (company_id, cnpj, nome) VALUES ($1, $2, $3)
				ON CONFLICT (company_id, cnpj) DO UPDATE SET nome = EXCLUDED.nome
			`, companyID, cnpj, nome)
			if err != nil {
				log.Printf("[ParceirosSync] upsert error [%s]: %v", cnpj, err)
				continue
			}
			upserted++
		}

		log.Printf("[ParceirosSync] company=%s upserted=%d", companyID, upserted)
		json.NewEncoder(w).Encode(parceirosSyncResult{Upserted: upserted})
	}
}
