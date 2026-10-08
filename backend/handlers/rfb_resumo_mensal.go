package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
)

// periodoMMAAAARegex valida o formato "mm/aaaa" (ex. "01/2026") — mesmo formato canônico de
// rfb_resumo.data_apuracao/rfb_creditos_resumo.data_apuracao desde a migration 129. Mesmo
// padrão de validação já usado para filtros de período em rfb_debitos_lista.go/rfb_creditos.go
// (lá a entrada externa é "YYYYMM" e é traduzida para "mm/aaaa" via SQL; aqui o parâmetro
// "periodo" já chega direto no formato "mm/aaaa", batendo 1:1 com a coluna).
var periodoMMAAAARegex = regexp.MustCompile(`^(0[1-9]|1[0-2])/[0-9]{4}$`)

// RFBResumoMensal é a resposta de GET /api/rfb/resumo-mensal — camada de apuração mensal
// sobre o schema raso (binário extinto/não-extinto) do CBS v1, ver spec-apuracao-mensal-
// cbs-ibs.md e spec-apuracao-mensal-cbs-ibs-plano.md.
type RFBResumoMensal struct {
	Periodo                string  `json:"periodo"`
	ValorCBSNaoExtinto     float64 `json:"valor_cbs_nao_extinto"`
	ValorCBSExtintoCredito float64 `json:"valor_cbs_extinto_credito"`
	SaldoAPagarAproximado  float64 `json:"saldo_a_pagar_aproximado"`
	// Campos adicionados para os KPIs da tela "Gestão de Créditos e Débitos" (spec
	// apuracao-cbs-dashboard-real.md) — mesmas fontes/COALESCE das 2 queries já existentes
	// abaixo, apenas lendo colunas adicionais das mesmas linhas já buscadas.
	TotalDebitos         int     `json:"total_debitos"`
	TotalCreditos        int     `json:"total_creditos"`
	ValorCBSTotalDebito  float64 `json:"valor_cbs_total_debito"`
	ValorCBSTotalCredito float64 `json:"valor_cbs_total_credito"`
	// Aproximado é sempre true nesta versão — o schema v1 de rfb_resumo/rfb_creditos_resumo só
	// tem o corte binário extinto/não-extinto, sem a granularidade de apropriação
	// (a_apropriar/não_utilizado/utilizado) que o schema v2 da RFB vai trazer pronta (ver
	// docs.receitafederal.gov.br/apuracao-cbs/credito.html, campo "prescrito" nativo). Não
	// confundir com o saldo do IBS (ResumoCGIBSHandler), que já é exato — ali o schema de
	// captura (migration 131) já tem a granularidade completa.
	Aproximado     bool   `json:"aproximado"`
	StatusPeriodo  string `json:"status_periodo"`
	DataFechamento string `json:"data_fechamento"`
}

// RFBResumoMensalHandler — GET /api/rfb/resumo-mensal?periodo=mm/aaaa
//
// Lê rfb_resumo/rfb_creditos_resumo (cada um no máximo 1 linha por (company_id,
// data_apuracao), migrations 054/096) e calcula um saldo a pagar APROXIMADO para o CBS,
// rotulado explicitamente como tal — contraste com o resumo do IBS (ResumoCGIBSHandler),
// que é exato porque o schema de captura (migration 131) já é granular o suficiente.
func RFBResumoMensalHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[RFBResumoMensal]")
			return
		}

		periodo := strings.TrimSpace(r.URL.Query().Get("periodo"))
		if !periodoMMAAAARegex.MatchString(periodo) {
			jsonErr(w, http.StatusBadRequest, "periodo inválido — use o formato mm/aaaa (ex. 01/2026)")
			return
		}

		var resp RFBResumoMensal
		resp.Periodo = periodo

		err = db.QueryRow(`
			SELECT COALESCE(valor_cbs_nao_extinto, 0), COALESCE(total_debitos, 0), COALESCE(valor_cbs_total, 0)
			FROM rfb_resumo
			WHERE company_id = $1 AND data_apuracao = $2
		`, companyID, periodo).Scan(&resp.ValorCBSNaoExtinto, &resp.TotalDebitos, &resp.ValorCBSTotalDebito)
		if err != nil && err != sql.ErrNoRows {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar resumo de débitos CBS", err, "[RFBResumoMensal]")
			return
		}
		// err == sql.ErrNoRows: nenhuma linha para o período — resp.ValorCBSNaoExtinto,
		// resp.TotalDebitos e resp.ValorCBSTotalDebito permanecem no zero-value (0),
		// equivalente ao COALESCE para o caso "linha existe mas coluna é NULL".

		err = db.QueryRow(`
			SELECT COALESCE(valor_cbs_extinto, 0), COALESCE(total_creditos, 0), COALESCE(valor_cbs_total, 0)
			FROM rfb_creditos_resumo
			WHERE company_id = $1 AND data_apuracao = $2
		`, companyID, periodo).Scan(&resp.ValorCBSExtintoCredito, &resp.TotalCreditos, &resp.ValorCBSTotalCredito)
		if err != nil && err != sql.ErrNoRows {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar resumo de créditos CBS", err, "[RFBResumoMensal]")
			return
		}

		resp.SaldoAPagarAproximado = resp.ValorCBSNaoExtinto - resp.ValorCBSExtintoCredito
		resp.Aproximado = true

		// StatusPeriodoApuracao recebe qualquer data dentro do mês de referência — parseia
		// "mm/aaaa" como o primeiro dia desse mês (parse "01/2006" não aceita dia; usar
		// "01/"+periodo coloca o dia 1 explícito, formato "02/01/2006").
		paTime, perr := time.Parse("02/01/2006", "01/"+periodo)
		if perr != nil {
			// Não deveria acontecer — já validado pelo regex acima — mas evita 500 silencioso.
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao interpretar período", perr, "[RFBResumoMensal]")
			return
		}
		modo := services.ResolveModoCalendario()
		status, dataFechamento := services.StatusPeriodoApuracao(paTime, modo)
		resp.StatusPeriodo = status
		resp.DataFechamento = dataFechamento.Format(time.RFC3339)

		json.NewEncoder(w).Encode(resp)
	}
}
