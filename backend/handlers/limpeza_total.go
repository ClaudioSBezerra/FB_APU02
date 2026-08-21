package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// LimpezaTotalTabelas lista todas as tabelas de "movimento" apagadas pela
// limpeza global de dados de teste (pedido do gestor Marlos, ago/2026 — o
// servidor só tem dados de teste até a operação real começar em jan/2027).
// Diferente de LimpezaBaseHandler (escopado por empresa, DELETE, 3 tabelas),
// esta ação é global — sem filtro de company_id — e usa TRUNCATE nas ~36
// tabelas de movimento do sistema inteiro. NUNCA adicionar aqui uma tabela
// estrutural/config/autenticação (companies, users, credenciais, etc).
// 'participants' entra porque tem FK ON DELETE CASCADE em import_jobs (sem
// isso o TRUNCATE ... CASCADE a arrastaria de qualquer forma).
var LimpezaTotalTabelas = []string{
	"nfe_entradas", "nfe_saidas", "cte_entradas",
	"rfb_debitos", "rfb_creditos", "rfb_debitos_liquidacoes", "rfb_creditos_liquidacoes",
	"rfb_resumo", "rfb_creditos_resumo", "rfb_requests",
	"cgibs_debitos", "cgibs_requests", "cgibs_resumo",
	"erp_bridge_runs", "erp_bridge_run_items",
	"import_jobs", "participants",
	"sap_sync_runs", "sap_resultados_busca",
	"reg_0140", "reg_c010", "reg_c100", "reg_c190", "reg_c500", "reg_c600", "reg_d100", "reg_d500",
	"ai_reports", "operacoes_comerciais", "pagamentos_fornecedores", "pagamentos_imports", "forn_simples",
	"comunicacoes_agregado", "energia_agregado", "frete_agregado",
	"parceiros",
}

// Frase que o admin precisa digitar exatamente igual na tela pra confirmar —
// mais forte que o confirm de dois cliques da Limpeza de Base, dado o
// escopo (todas as empresas, todas as tabelas de movimento, sem volta).
const limpezaTotalFrase = "LIMPAR TODOS OS DADOS DE TESTE"

// LimpezaTotalHandler expõe GET (preview de contagens globais) e POST
// (executa o TRUNCATE, mediante frase de confirmação exata).
func LimpezaTotalHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			jsonErr(w, 401, "Não autenticado")
			return
		}
		if role, _ := claims["role"].(string); role != "admin" {
			jsonErr(w, 403, "Acesso restrito a administradores")
			return
		}
		userID := GetUserIDFromContext(r)

		switch r.Method {

		case http.MethodGet:
			contagens := map[string]int64{}
			var total int64
			for _, tbl := range LimpezaTotalTabelas {
				var count int64
				if err := db.QueryRow("SELECT COUNT(*) FROM " + tbl).Scan(&count); err == nil {
					contagens[tbl] = count
					total += count
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"contagens": contagens,
				"total":     total,
				"tabelas":   LimpezaTotalTabelas,
				"frase":     limpezaTotalFrase,
			})

		case http.MethodPost:
			var req struct {
				Confirmacao string `json:"confirmacao"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonErr(w, 400, "Requisição inválida")
				return
			}
			if strings.TrimSpace(req.Confirmacao) != limpezaTotalFrase {
				jsonErr(w, 400, "Frase de confirmação incorreta")
				return
			}

			log.Printf("[LimpezaTotal] Admin %s iniciando limpeza GLOBAL de dados de teste (todas as empresas, %d tabelas)",
				userID, len(LimpezaTotalTabelas))

			stmt := "TRUNCATE TABLE " + strings.Join(LimpezaTotalTabelas, ", ") + " RESTART IDENTITY CASCADE"
			if _, err := db.Exec(stmt); err != nil {
				sanitizeDBErr(w, 500, "Erro ao truncar tabelas", err, "[LimpezaTotal]")
				return
			}

			// Sinaliza reset_tracker pra todo daemon ERP Bridge configurado —
			// evita que o tracker.db local ache que chaves (agora inexistentes)
			// já foram enviadas e pule o reimport quando a operação real começar.
			if _, err := db.Exec(`UPDATE erp_bridge_config SET reset_tracker = true, updated_at = NOW()`); err != nil {
				log.Printf("[LimpezaTotal] Aviso: não foi possível sinalizar reset_tracker: %v", err)
			}

			// mv_malha_fina_resumo já é atualizada via CONCURRENTLY em outro
			// lugar (LimpezaBase) — mantém o mesmo método aqui. As outras 3
			// não têm essa garantia validada, então usa REFRESH simples.
			go func() {
				if _, err := db.Exec("REFRESH MATERIALIZED VIEW CONCURRENTLY mv_malha_fina_resumo"); err != nil {
					log.Printf("[LimpezaTotal] Aviso: refresh mv_malha_fina_resumo: %v", err)
				}
			}()
			for _, mv := range []string{"mv_mercadorias_agregada", "mv_operacoes_simples", "mv_compras_fornecedores"} {
				go func(view string) {
					if _, err := db.Exec("REFRESH MATERIALIZED VIEW " + view); err != nil {
						log.Printf("[LimpezaTotal] Aviso: refresh %s: %v", view, err)
					}
				}(mv)
			}

			log.Printf("[LimpezaTotal] Concluído por %s", userID)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": "Limpeza total concluída — todas as tabelas de movimento foram truncadas em todas as empresas.",
			})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
