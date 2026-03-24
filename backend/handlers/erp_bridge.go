package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type ERPBridgeConfig struct {
	CompanyID       string     `json:"company_id"`
	Ativo           bool       `json:"ativo"`
	Horario         string     `json:"horario"` // HH:MM
	DiasRetroativos int        `json:"dias_retroativos"`
	UltimoRunEm     *time.Time `json:"ultimo_run_em"`
	UpdatedAt       time.Time  `json:"updated_at"`
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
			err := db.QueryRow(`
				SELECT company_id, ativo, TO_CHAR(horario, 'HH24:MI'), dias_retroativos,
				       ultimo_run_em, updated_at
				FROM erp_bridge_config WHERE company_id = $1
			`, companyID).Scan(&cfg.CompanyID, &cfg.Ativo, &horario,
				&cfg.DiasRetroativos, &cfg.UltimoRunEm, &cfg.UpdatedAt)
			if err == sql.ErrNoRows {
				cfg = ERPBridgeConfig{
					CompanyID:       companyID,
					Ativo:           false,
					Horario:         "02:00",
					DiasRetroativos: 1,
					UpdatedAt:       time.Now(),
				}
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			} else {
				cfg.Horario = horario
			}
			json.NewEncoder(w).Encode(cfg)

		case http.MethodPatch:
			var req struct {
				Ativo           *bool   `json:"ativo"`
				Horario         *string `json:"horario"`
				DiasRetroativos *int    `json:"dias_retroativos"`
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
				    updated_at       = NOW()
			`, companyID, req.Ativo, req.Horario, req.DiasRetroativos)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
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
// Retorna os nomes distintos de servidor vistos no histórico de run_items.

func ERPBridgeServidoresHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		companyID, err := erpBridgeGetCompany(db, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		rows, err := db.Query(`
			SELECT DISTINCT i.servidor
			FROM erp_bridge_run_items i
			JOIN erp_bridge_runs r ON r.id = i.run_id
			WHERE r.company_id = $1
			ORDER BY i.servidor
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
			DataIni       string   `json:"data_ini"`
			DataFim       string   `json:"data_fim"`
			FiliaisFilter []string `json:"filiais_filter"`
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
			    (company_id, data_ini, data_fim, origem, status, filiais_filter)
			VALUES ($1, $2::DATE, $3::DATE, 'manual', 'pending', $4)
			RETURNING id
		`, companyID, req.DataIni, req.DataFim, filiaisJSON).Scan(&id)
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
			SELECT id, data_ini, data_fim, filiais_filter
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
			ID            string  `json:"id"`
			DataIni       *string `json:"data_ini"`
			DataFim       *string `json:"data_fim"`
			FiliaisFilter *string `json:"filiais_filter"`
		}
		var items []PendingRun
		for rows.Next() {
			var p PendingRun
			if rows.Scan(&p.ID, &p.DataIni, &p.DataFim, &p.FiliaisFilter) == nil {
				items = append(items, p)
			}
		}
		if items == nil {
			items = []PendingRun{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"items": items})
	}
}
