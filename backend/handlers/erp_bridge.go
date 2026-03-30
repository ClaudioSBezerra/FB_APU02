package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type ERPBridgeConfig struct {
	CompanyID          string     `json:"company_id"`
	Ativo              bool       `json:"ativo"`
	Horario            string     `json:"horario"` // HH:MM
	DiasRetroativos    int        `json:"dias_retroativos"`
	UltimoRunEm        *time.Time `json:"ultimo_run_em"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ResetTracker       bool       `json:"reset_tracker"`
	ErpType            string     `json:"erp_type"`
	FBTaxEmail         string     `json:"fbtax_email"`
	FBTaxPasswordSet   bool       `json:"fbtax_password_set"`
	OracleDsn          string     `json:"oracle_dsn"`
	OracleUsuario      string     `json:"oracle_usuario"`
	OracleSenhaSet     bool       `json:"oracle_senha_set"`
	APIKey             string     `json:"api_key"`
	DaemonLastSeen     *time.Time `json:"daemon_last_seen"`
	DaemonOnline       bool       `json:"daemon_online"`
}

type ERPBridgeRun struct {
	ID             string           `json:"id"`
	CompanyID      string           `json:"company_id"`
	IniciadoEm     time.Time        `json:"iniciado_em"`
	FinalizadoEm   *time.Time       `json:"finalizado_em"`
	Status         string           `json:"status"`
	DataIni        *string          `json:"data_ini"`
	DataFim        *string          `json:"data_fim"`
	TotalEnviados  int              `json:"total_enviados"`
	TotalIgnorados int              `json:"total_ignorados"`
	TotalErros     int              `json:"total_erros"`
	ErroMsg        *string          `json:"erro_msg"`
	Origem         string           `json:"origem"`
	Items          []ERPBridgeRunItem `json:"items,omitempty"`
}

type ERPBridgeRunItem struct {
	ID        string  `json:"id"`
	RunID     string  `json:"run_id"`
	Servidor  string  `json:"servidor"`
	Tipo      string  `json:"tipo"`
	Enviados  int     `json:"enviados"`
	Ignorados int     `json:"ignorados"`
	Erros     int     `json:"erros"`
	Status    string  `json:"status"`
	ErroMsg   *string `json:"erro_msg"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func erpBridgeGetCompany(db *sql.DB, r *http.Request) (string, error) {
	claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
	if !ok {
		return "", sql.ErrNoRows
	}
	userID := claims["user_id"].(string)
	return GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
}

// ── GET/PATCH /api/erp-bridge/config ─────────────────────────────────────────

func ERPBridgeConfigHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodGet:
			var cfg ERPBridgeConfig
			var horario string
			var erpType, fbtaxEmail, fbtaxPassword, oracleDsn, oracleUsuario, oracleSenha, apiKey sql.NullString
			err := db.QueryRow(`
				SELECT company_id, ativo, TO_CHAR(horario, 'HH24:MI'), dias_retroativos,
				       ultimo_run_em, updated_at, reset_tracker,
				       COALESCE(erp_type, 'oracle_xml'),
				       fbtax_email, fbtax_password, oracle_dsn, oracle_usuario, oracle_senha, api_key,
				       daemon_last_seen
				FROM erp_bridge_config WHERE company_id = $1
			`, companyID).Scan(&cfg.CompanyID, &cfg.Ativo, &horario,
				&cfg.DiasRetroativos, &cfg.UltimoRunEm, &cfg.UpdatedAt, &cfg.ResetTracker,
				&erpType, &fbtaxEmail, &fbtaxPassword, &oracleDsn, &oracleUsuario, &oracleSenha, &apiKey,
				&cfg.DaemonLastSeen)
			if err == sql.ErrNoRows {
				cfg = ERPBridgeConfig{
					CompanyID:       companyID,
					Ativo:           false,
					Horario:         "02:00",
					DiasRetroativos: 1,
					UpdatedAt:       time.Now(),
					ResetTracker:    false,
					ErpType:         "oracle_xml",
				}
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			} else {
				cfg.Horario = horario
				if erpType.Valid {
					cfg.ErpType = erpType.String
				} else {
					cfg.ErpType = "oracle_xml"
				}
				if fbtaxEmail.Valid {
					cfg.FBTaxEmail = fbtaxEmail.String
				}
				cfg.FBTaxPasswordSet = fbtaxPassword.Valid && fbtaxPassword.String != ""
				if oracleDsn.Valid {
					cfg.OracleDsn = oracleDsn.String
				}
				if oracleUsuario.Valid {
					cfg.OracleUsuario = DecryptFieldWithFallback(oracleUsuario.String)
				}
				cfg.OracleSenhaSet = oracleSenha.Valid && oracleSenha.String != ""
				if apiKey.Valid && apiKey.String != "" {
					cfg.APIKey = DecryptFieldWithFallback(apiKey.String)
				}
				// Daemon está online se fez heartbeat nos últimos 3 minutos
				if cfg.DaemonLastSeen != nil {
					cfg.DaemonOnline = time.Since(*cfg.DaemonLastSeen) < 3*time.Minute
				}
			}
			json.NewEncoder(w).Encode(cfg)

		case http.MethodPatch:
			var req struct {
				Ativo           *bool   `json:"ativo"`
				Horario         *string `json:"horario"`
				DiasRetroativos *int    `json:"dias_retroativos"`
				ResetTracker    *bool   `json:"reset_tracker"`
				ErpType         *string `json:"erp_type"`
				FBTaxEmail      *string `json:"fbtax_email"`
				FBTaxPassword   *string `json:"fbtax_password"`
				OracleDsn       *string `json:"oracle_dsn"`
				OracleUsuario   *string `json:"oracle_usuario"`
				OracleSenha     *string `json:"oracle_senha"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "JSON inválido", http.StatusBadRequest)
				return
			}
			_, err := db.Exec(`
				INSERT INTO erp_bridge_config (company_id, ativo, horario, dias_retroativos, updated_at)
				VALUES ($1, COALESCE($2, false), COALESCE($3::TIME, '02:00'), COALESCE($4, 1), NOW())
				ON CONFLICT (company_id) DO UPDATE SET
				    ativo            = COALESCE($2, erp_bridge_config.ativo),
				    horario          = COALESCE($3::TIME, erp_bridge_config.horario),
				    dias_retroativos = COALESCE($4, erp_bridge_config.dias_retroativos),
				    reset_tracker    = COALESCE($5, erp_bridge_config.reset_tracker),
				    updated_at       = NOW()
			`, companyID, req.Ativo, req.Horario, req.DiasRetroativos, req.ResetTracker)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Atualiza credenciais individualmente se fornecidas
			if req.FBTaxEmail != nil {
				db.Exec(`UPDATE erp_bridge_config SET fbtax_email = $2 WHERE company_id = $1`, companyID, *req.FBTaxEmail)
			}
			if req.FBTaxPassword != nil && *req.FBTaxPassword != "" {
				if enc, encErr := EncryptField(*req.FBTaxPassword); encErr == nil {
					db.Exec(`UPDATE erp_bridge_config SET fbtax_password = $2 WHERE company_id = $1`, companyID, enc)
				}
			}
			if req.OracleUsuario != nil {
				if enc, encErr := EncryptField(*req.OracleUsuario); encErr == nil {
					db.Exec(`UPDATE erp_bridge_config SET oracle_usuario = $2 WHERE company_id = $1`, companyID, enc)
				}
			}
			if req.OracleSenha != nil && *req.OracleSenha != "" {
				if enc, encErr := EncryptField(*req.OracleSenha); encErr == nil {
					db.Exec(`UPDATE erp_bridge_config SET oracle_senha = $2 WHERE company_id = $1`, companyID, enc)
				}
			}
			if req.ErpType != nil {
				db.Exec(`UPDATE erp_bridge_config SET erp_type = $2 WHERE company_id = $1`, companyID, *req.ErpType)
			}
			if req.OracleDsn != nil {
				db.Exec(`UPDATE erp_bridge_config SET oracle_dsn = $2 WHERE company_id = $1`, companyID, *req.OracleDsn)
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// ── GET /api/erp-bridge/runs  (lista) ─────────────────────────────────────────
// ── POST /api/erp-bridge/runs (bridge abre um novo run) ──────────────────────

func ERPBridgeRunsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodGet:
			rows, err := db.Query(`
				SELECT id, iniciado_em, finalizado_em, status, data_ini, data_fim,
				       total_enviados, total_ignorados, total_erros, erro_msg, origem
				FROM erp_bridge_runs
				WHERE company_id = $1
				ORDER BY iniciado_em DESC
				LIMIT 200
			`, companyID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			var runs []ERPBridgeRun
			for rows.Next() {
				var run ERPBridgeRun
				run.CompanyID = companyID
				if scanErr := rows.Scan(&run.ID, &run.IniciadoEm, &run.FinalizadoEm,
					&run.Status, &run.DataIni, &run.DataFim,
					&run.TotalEnviados, &run.TotalIgnorados, &run.TotalErros,
					&run.ErroMsg, &run.Origem); scanErr != nil {
					continue
				}
				runs = append(runs, run)
			}
			if runs == nil {
				runs = []ERPBridgeRun{}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"items": runs, "total": len(runs)})

		case http.MethodPost:
			var req struct {
				DataIni string `json:"data_ini"`
				DataFim string `json:"data_fim"`
				Origem  string `json:"origem"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "JSON inválido", http.StatusBadRequest)
				return
			}
			origem := req.Origem
			if origem == "" {
				origem = "manual"
			}
			var id string
			err := db.QueryRow(`
				INSERT INTO erp_bridge_runs (company_id, data_ini, data_fim, origem, status)
				VALUES ($1, $2::DATE, $3::DATE, $4, 'running')
				RETURNING id
			`, companyID, req.DataIni, req.DataFim, origem).Scan(&id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Atualiza ultimo_run_em na config
			db.Exec(`
				INSERT INTO erp_bridge_config (company_id, ativo, horario, dias_retroativos, ultimo_run_em)
				VALUES ($1, false, '02:00', 1, NOW())
				ON CONFLICT (company_id) DO UPDATE SET ultimo_run_em = NOW()
			`, companyID)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": id})

		case http.MethodDelete:
			// Limpa runs finalizados (não remove running/pending)
			db.Exec(`
				DELETE FROM erp_bridge_runs
				WHERE company_id = $1 AND status NOT IN ('running','pending')
			`, companyID)
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// ── GET    /api/erp-bridge/runs/{id}        (detalhe com items) ───────────────
// ── PATCH  /api/erp-bridge/runs/{id}        (bridge finaliza run) ─────────────
// ── POST   /api/erp-bridge/runs/{id}/items  (bridge envia stats) ──────────────

func ERPBridgeRunHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Extrai runID e sub-path de /api/erp-bridge/runs/{id}[/items]
		path := strings.TrimPrefix(r.URL.Path, "/api/erp-bridge/runs/")
		parts := strings.SplitN(path, "/", 2)
		runID := parts[0]
		subPath := ""
		if len(parts) > 1 {
			subPath = parts[1]
		}

		if runID == "" {
			http.Error(w, "Run ID obrigatório", http.StatusBadRequest)
			return
		}

		// Verifica que o run pertence à empresa autenticada
		var exists bool
		db.QueryRow(`SELECT EXISTS(SELECT 1 FROM erp_bridge_runs WHERE id=$1 AND company_id=$2)`,
			runID, companyID).Scan(&exists)
		if !exists {
			http.Error(w, "Run não encontrado", http.StatusNotFound)
			return
		}

		switch {

		// POST /api/erp-bridge/runs/{id}/items — bridge reporta stats por servidor/tipo
		case subPath == "items" && r.Method == http.MethodPost:
			var items []ERPBridgeRunItem
			if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
				http.Error(w, "JSON inválido (esperado array de items)", http.StatusBadRequest)
				return
			}
			for _, item := range items {
				status := item.Status
				if status == "" {
					status = "ok"
				}
				db.Exec(`
					INSERT INTO erp_bridge_run_items
					    (run_id, servidor, tipo, enviados, ignorados, erros, status, erro_msg)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				`, runID, item.Servidor, item.Tipo, item.Enviados,
					item.Ignorados, item.Erros, status, item.ErroMsg)
			}
			w.WriteHeader(http.StatusCreated)

		// PATCH /api/erp-bridge/runs/{id} — bridge atualiza status do run
		case subPath == "" && r.Method == http.MethodPatch:
			var req struct {
				Status         string  `json:"status"`
				TotalEnviados  int     `json:"total_enviados"`
				TotalIgnorados int     `json:"total_ignorados"`
				TotalErros     int     `json:"total_erros"`
				ErroMsg        *string `json:"erro_msg"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "JSON inválido", http.StatusBadRequest)
				return
			}
			var execErr error
			if req.Status == "running" {
				// Início de execução: apenas marca como running, sem finalizado_em
				_, execErr = db.Exec(`
					UPDATE erp_bridge_runs SET status = 'running' WHERE id = $1
				`, runID)
			} else if req.Status == "cancelled" {
				// Cancelamento: finaliza imediatamente sem totais
				_, execErr = db.Exec(`
					UPDATE erp_bridge_runs SET status = 'cancelled', finalizado_em = NOW()
					WHERE id = $1 AND status IN ('pending','running')
				`, runID)
			} else {
				if req.Status == "" {
					req.Status = "success"
				}
				_, execErr = db.Exec(`
					UPDATE erp_bridge_runs SET
					    status          = $2,
					    finalizado_em   = NOW(),
					    total_enviados  = $3,
					    total_ignorados = $4,
					    total_erros     = $5,
					    erro_msg        = $6
					WHERE id = $1
				`, runID, req.Status, req.TotalEnviados, req.TotalIgnorados,
					req.TotalErros, req.ErroMsg)
			}
			if execErr != nil {
				http.Error(w, execErr.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		// GET /api/erp-bridge/runs/{id} — detalhe completo com items
		case subPath == "" && r.Method == http.MethodGet:
			var run ERPBridgeRun
			run.CompanyID = companyID
			err := db.QueryRow(`
				SELECT id, iniciado_em, finalizado_em, status, data_ini, data_fim,
				       total_enviados, total_ignorados, total_erros, erro_msg, origem
				FROM erp_bridge_runs WHERE id = $1
			`, runID).Scan(&run.ID, &run.IniciadoEm, &run.FinalizadoEm, &run.Status,
				&run.DataIni, &run.DataFim, &run.TotalEnviados, &run.TotalIgnorados,
				&run.TotalErros, &run.ErroMsg, &run.Origem)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			irows, _ := db.Query(`
				SELECT id, servidor, tipo, enviados, ignorados, erros, status, erro_msg
				FROM erp_bridge_run_items
				WHERE run_id = $1
				ORDER BY servidor, tipo
			`, runID)
			if irows != nil {
				defer irows.Close()
				for irows.Next() {
					var item ERPBridgeRunItem
					item.RunID = runID
					irows.Scan(&item.ID, &item.Servidor, &item.Tipo,
						&item.Enviados, &item.Ignorados, &item.Erros,
						&item.Status, &item.ErroMsg)
					run.Items = append(run.Items, item)
				}
			}
			if run.Items == nil {
				run.Items = []ERPBridgeRunItem{}
			}
			json.NewEncoder(w).Encode(run)

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// ── GET /api/erp-bridge/servidores ────────────────────────────────────────────
// Retorna servidores configurados (erp_bridge_servidores) UNION histórico de run_items.

func ERPBridgeServidoresHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		rows, err := db.Query(`
			SELECT DISTINCT nome FROM (
			  SELECT nome FROM erp_bridge_servidores WHERE company_id = $1
			  UNION
			  SELECT DISTINCT i.servidor AS nome
			  FROM erp_bridge_run_items i
			  JOIN erp_bridge_runs r ON r.id = i.run_id
			  WHERE r.company_id = $1
			  UNION
			  SELECT 'FCCORP' AS nome
			  FROM erp_bridge_config
			  WHERE company_id = $1 AND COALESCE(erp_type,'oracle_xml') = 'sap_s4hana'
			) t ORDER BY nome
		`, companyID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var servidores []string
		for rows.Next() {
			var s string
			if rows.Scan(&s) == nil {
				servidores = append(servidores, s)
			}
		}
		if servidores == nil {
			servidores = []string{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"items": servidores})
	}
}

// ── POST /api/erp-bridge/servidores ───────────────────────────────────────────
// Chamado pelo daemon Bridge ao iniciar para registrar seus servidores configurados.
// Body: { "nomes": ["FC - Aracaju", "FC - Salvador", ...] }

func ERPBridgeRegistrarServidoresHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			Nomes []string `json:"nomes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Nomes) == 0 {
			http.Error(w, "body inválido", http.StatusBadRequest)
			return
		}
		for _, nome := range body.Nomes {
			db.Exec(`
				INSERT INTO erp_bridge_servidores (company_id, nome, updated_at)
				VALUES ($1, $2, NOW())
				ON CONFLICT (company_id, nome) DO UPDATE SET updated_at = NOW()
			`, companyID, nome)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── POST /api/erp-bridge/config/generate-api-key ─────────────────────────────
// Gera uma nova API key para o daemon Bridge e a armazena criptografada.
// Retorna a chave em plaintext para copiar ao config.yaml — mostrada apenas uma vez.

func ERPBridgeGenerateAPIKeyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Gera 32 bytes aleatórios → chave hex de 64 caracteres
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			http.Error(w, "erro ao gerar chave", http.StatusInternalServerError)
			return
		}
		key := hex.EncodeToString(raw)
		hash := sha256.Sum256([]byte(key))
		hashHex := hex.EncodeToString(hash[:])
		enc, encErr := EncryptField(key)
		if encErr != nil {
			http.Error(w, "erro ao criptografar chave", http.StatusInternalServerError)
			return
		}
		db.Exec(`
			INSERT INTO erp_bridge_config (company_id, api_key, api_key_hash)
			VALUES ($1, $2, $3)
			ON CONFLICT (company_id) DO UPDATE SET api_key = $2, api_key_hash = $3, updated_at = NOW()
		`, companyID, enc, hashHex)
		json.NewEncoder(w).Encode(map[string]string{"api_key": key})
	}
}

// ── GET /api/erp-bridge/credentials ──────────────────────────────────────────
// Endpoint público (sem JWT) — autenticado via X-API-Key.
// Usado pelo daemon Bridge para buscar credenciais criptografadas.

func ERPBridgeCredentialsHandler(db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			http.Error(w, "X-API-Key obrigatório", http.StatusUnauthorized)
			return
		}
		hash := sha256.Sum256([]byte(apiKey))
		hashHex := hex.EncodeToString(hash[:])
		var fbtaxEmail, fbtaxPassword, oracleUsuario, oracleSenha, erpType, oracleDsn sql.NullString
		err := db.QueryRow(`
			SELECT fbtax_email, fbtax_password, oracle_usuario, oracle_senha,
			       COALESCE(erp_type, 'oracle_xml'), COALESCE(oracle_dsn, '')
			FROM erp_bridge_config WHERE api_key_hash = $1
		`, hashHex).Scan(&fbtaxEmail, &fbtaxPassword, &oracleUsuario, &oracleSenha, &erpType, &oracleDsn)
		if err == sql.ErrNoRows {
			http.Error(w, "API key inválida", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		result := map[string]string{
			"fbtax_email":    "",
			"fbtax_password": "",
			"oracle_usuario": "",
			"oracle_senha":   "",
			"erp_type":       "oracle_xml",
			"oracle_dsn":     "",
		}
		if fbtaxEmail.Valid {
			result["fbtax_email"] = fbtaxEmail.String
		}
		if fbtaxPassword.Valid && fbtaxPassword.String != "" {
			result["fbtax_password"] = DecryptFieldWithFallback(fbtaxPassword.String)
		}
		if oracleUsuario.Valid && oracleUsuario.String != "" {
			result["oracle_usuario"] = DecryptFieldWithFallback(oracleUsuario.String)
		}
		if oracleSenha.Valid && oracleSenha.String != "" {
			result["oracle_senha"] = DecryptFieldWithFallback(oracleSenha.String)
		}
		if erpType.Valid {
			result["erp_type"] = erpType.String
		}
		if oracleDsn.Valid && oracleDsn.String != "" {
			result["oracle_dsn"] = DecryptFieldWithFallback(oracleDsn.String)
		}
		json.NewEncoder(w).Encode(result)
	})
}

// ── POST /api/erp-bridge/trigger ──────────────────────────────────────────────
// Cria um run com status='pending' para o daemon Bridge executar na próxima varredura.
// Body: { "data_ini": "YYYY-MM-DD", "data_fim": "YYYY-MM-DD", "filiais_filter": ["nome"] }

func ERPBridgeTriggerHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			DataIni        string   `json:"data_ini"`
			DataFim        string   `json:"data_fim"`
			FiliaisFilter  []string `json:"filiais_filter"`
			OnlyParceiros  bool     `json:"only_parceiros"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DataIni == "" || req.DataFim == "" {
			http.Error(w, "data_ini e data_fim são obrigatórios", http.StatusBadRequest)
			return
		}
		var running bool
		db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM erp_bridge_runs
				WHERE company_id = $1 AND status IN ('running','pending')
			)
		`, companyID).Scan(&running)
		if running {
			http.Error(w, "Já existe uma importação em andamento ou aguardando execução.", http.StatusConflict)
			return
		}
		var filiaisJSON *string
		if len(req.FiliaisFilter) > 0 {
			b, _ := json.Marshal(req.FiliaisFilter)
			s := string(b)
			filiaisJSON = &s
		}
		var id string
		err = db.QueryRow(`
			INSERT INTO erp_bridge_runs
			    (company_id, data_ini, data_fim, origem, status, filiais_filter, only_parceiros)
			VALUES ($1, $2::DATE, $3::DATE, 'manual', 'pending', $4, $5)
			RETURNING id
		`, companyID, req.DataIni, req.DataFim, filiaisJSON, req.OnlyParceiros).Scan(&id)
		if err != nil {
			log.Printf("ERPBridgeTrigger insert error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "pending"})
	}
}

// ── GET /api/erp-bridge/pending ───────────────────────────────────────────────
// Usado pelo daemon Bridge para buscar runs pendentes criados pela UI.

func ERPBridgePendingHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		rows, err := db.Query(`
			SELECT id, data_ini, data_fim, filiais_filter, COALESCE(only_parceiros, FALSE)
			FROM erp_bridge_runs
			WHERE company_id = $1 AND status = 'pending'
			ORDER BY iniciado_em ASC
		`, companyID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		type PendingRun struct {
			ID             string  `json:"id"`
			DataIni        *string `json:"data_ini"`
			DataFim        *string `json:"data_fim"`
			FiliaisFilter  *string `json:"filiais_filter"`
			OnlyParceiros  bool    `json:"only_parceiros"`
		}
		var items []PendingRun
		for rows.Next() {
			var p PendingRun
			if rows.Scan(&p.ID, &p.DataIni, &p.DataFim, &p.FiliaisFilter, &p.OnlyParceiros) == nil {
				items = append(items, p)
			}
		}
		if items == nil {
			items = []PendingRun{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"items": items})
	}
}

// ── POST /api/erp-bridge/heartbeat ───────────────────────────────────────────
// Chamado pelo daemon a cada ciclo para indicar que está ativo.
// Aproveita para limpar runs presos em pending/running por mais de 2 horas.

func ERPBridgeHeartbeatHandler(db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Autenticação via X-API-Key (mesmo padrão de /credentials e /pending)
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			http.Error(w, "X-API-Key obrigatório", http.StatusUnauthorized)
			return
		}
		hash := sha256.Sum256([]byte(apiKey))
		hashHex := hex.EncodeToString(hash[:])

		var companyID string
		err := db.QueryRow(`
			SELECT company_id FROM erp_bridge_config WHERE api_key_hash = $1
		`, hashHex).Scan(&companyID)
		if err == sql.ErrNoRows {
			http.Error(w, "API key inválida", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Atualiza daemon_last_seen
		db.Exec(`
			UPDATE erp_bridge_config SET daemon_last_seen = NOW() WHERE company_id = $1
		`, companyID)

		// Limpa runs presos: pending/running por mais de 2 horas → error
		db.Exec(`
			UPDATE erp_bridge_runs
			SET status = 'error',
			    finalizado_em = NOW(),
			    erro_msg = 'Run abandonado: daemon reiniciado ou timeout de 2h'
			WHERE company_id = $1
			  AND status IN ('pending', 'running')
			  AND iniciado_em < NOW() - INTERVAL '2 hours'
		`, companyID)

		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}
