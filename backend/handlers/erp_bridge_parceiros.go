package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
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

// ---------------------------------------------------------------------------
// ParceirosListHandler — GET /api/parceiros
//
// Listagem paginada de parceiros (fornecedor/cliente), escopada por
// company_id, com filtros opcionais por cnpj/nome. Mesmo padrão de
// paginação de PagamentosFornecedoresListHandler.
// ---------------------------------------------------------------------------

type parceiroItem struct {
	CNPJ                   string  `json:"cnpj"`
	Nome                   string  `json:"nome"`
	AderiuSplitPayment     bool    `json:"aderiu_split_payment"`
	DataAdesaoSplitPayment *string `json:"data_adesao_split_payment"`
}

func ParceirosListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}

		q := r.URL.Query()
		// Normaliza para casar com o formato gravado pelo sync (dígitos puros) mesmo
		// se o usuário buscar com CNPJ formatado — mesmo padrão de pagamentos_fornecedores.go.
		cnpj := reNonDigit.ReplaceAllString(q.Get("cnpj"), "")
		nome := strings.TrimSpace(q.Get("nome"))

		page, _ := strconv.Atoi(q.Get("page"))
		if page < 1 {
			page = 1
		}
		pageSize, _ := strconv.Atoi(q.Get("page_size"))
		if pageSize < 1 {
			pageSize = 50
		}
		if pageSize > 200 {
			pageSize = 200
		}

		args := []interface{}{companyID}
		where := []string{"company_id = $1"}
		idx := 2

		if cnpj != "" {
			where = append(where, fmt.Sprintf("cnpj ILIKE $%d", idx))
			args = append(args, "%"+cnpj+"%")
			idx++
		}
		if nome != "" {
			where = append(where, fmt.Sprintf("nome ILIKE $%d", idx))
			args = append(args, "%"+nome+"%")
			idx++
		}

		whereClause := "WHERE " + strings.Join(where, " AND ")

		var total int
		countQuery := "SELECT COUNT(*) FROM parceiros " + whereClause
		if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
			log.Printf("[ParceirosListHandler] count: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao contar registros")
			return
		}

		offset := (page - 1) * pageSize
		listArgs := append(args, pageSize, offset)
		rows, err := db.Query(fmt.Sprintf(`
			SELECT cnpj, nome, aderiu_split_payment, data_adesao_split_payment
			FROM parceiros
			%s
			ORDER BY nome
			LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1),
			listArgs...,
		)
		if err != nil {
			log.Printf("[ParceirosListHandler] query: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao listar parceiros")
			return
		}
		defer rows.Close()

		items := []parceiroItem{}
		for rows.Next() {
			var item parceiroItem
			var dataAdesao sql.NullTime
			if err := rows.Scan(&item.CNPJ, &item.Nome, &item.AderiuSplitPayment, &dataAdesao); err != nil {
				log.Printf("[ParceirosListHandler] scan: %v", err)
				continue
			}
			if dataAdesao.Valid {
				formatted := dataAdesao.Time.Format("2006-01-02")
				item.DataAdesaoSplitPayment = &formatted
			}
			items = append(items, item)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items":     items,
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		})
	}
}

// ---------------------------------------------------------------------------
// ParceirosUpdateSplitPaymentHandler — PATCH /api/parceiros/split-payment
//
// Atualiza a flag de adesão ao Split Payment de um parceiro já existente
// (nunca cria/exclui parceiro — quem cria é ERPBridgeParceirosSyncHandler).
// Ao desmarcar a adesão, data_adesao_split_payment mantém o valor histórico.
// ---------------------------------------------------------------------------

type parceirosUpdateSplitPaymentRequest struct {
	CNPJ                   string  `json:"cnpj"`
	AderiuSplitPayment     bool    `json:"aderiu_split_payment"`
	DataAdesaoSplitPayment *string `json:"data_adesao_split_payment"`
}

func ParceirosUpdateSplitPaymentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			jsonErr(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		userID := GetUserIDFromContext(r)
		if userID == "" {
			jsonErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro ao obter empresa")
			return
		}

		var req parceirosUpdateSplitPaymentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "JSON inválido")
			return
		}
		// Normaliza como parceiros.cnpj é gravado pelo sync (ERPBridgeParceirosSyncHandler
		// grava dígitos puros) — mesmo padrão de reNonDigit já usado em pagamentos_fornecedores.go.
		cnpj := reNonDigit.ReplaceAllString(req.CNPJ, "")
		if cnpj == "" {
			jsonErr(w, http.StatusBadRequest, "cnpj é obrigatório")
			return
		}

		// Regra (Edge Case Matrix da spec):
		// - Ao aderir (true) sem data informada, grava data_adesao_split_payment=hoje.
		// - Ao aderir (true) com data informada, usa a data informada.
		// - Ao desmarcar (false), NUNCA altera data_adesao_split_payment — mantém o
		//   valor histórico. Por isso a coluna de data só entra no SET quando há
		//   um valor definido para gravar (adesão true).
		var dataAdesao *string
		if req.AderiuSplitPayment {
			if req.DataAdesaoSplitPayment != nil && strings.TrimSpace(*req.DataAdesaoSplitPayment) != "" {
				dataAdesao = req.DataAdesaoSplitPayment
			} else {
				hoje := time.Now().Format("2006-01-02")
				dataAdesao = &hoje
			}
		}

		// COALESCE($2::date, data_adesao_split_payment): quando dataAdesao é nil
		// (desmarcando adesão), a coluna de data não é tocada — mantém o valor
		// histórico já gravado, em vez de ser limpa.
		var persistedData sql.NullTime
		err = db.QueryRow(`
			UPDATE parceiros
			SET aderiu_split_payment = $1,
			    data_adesao_split_payment = COALESCE($2::date, data_adesao_split_payment)
			WHERE company_id = $3 AND cnpj = $4
			RETURNING data_adesao_split_payment`,
			req.AderiuSplitPayment, dataAdesao, companyID, cnpj,
		).Scan(&persistedData)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "Parceiro não encontrado")
			return
		}
		if err != nil {
			log.Printf("[ParceirosUpdateSplitPayment] update [%s]: %v", cnpj, err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao atualizar parceiro")
			return
		}

		var respData *string
		if persistedData.Valid {
			formatted := persistedData.Time.Format("2006-01-02")
			respData = &formatted
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"cnpj":                      cnpj,
			"aderiu_split_payment":      req.AderiuSplitPayment,
			"data_adesao_split_payment": respData,
		})
	}
}
