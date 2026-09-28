package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"database/sql"

	"github.com/golang-jwt/jwt/v5"
)

// CGIBSResumoPeriodo é o resumo agregado ao vivo sobre cgibs_lancamentos para um período —
// substitui o antigo "resumo por request" (cgibs_resumo, dropado na migration 132), que não
// tem mais correspondência no modelo de conta corrente fiscal: aqui a apuração é incremental
// por lançamento, não por solicitação. Nunca cacheado — sempre calculado via SQL no momento da
// consulta (mesmo princípio já fixado para a agregação RFB, ver rfb_debitos_lista.go).
type CGIBSResumoPeriodo struct {
	DataIni                      string  `json:"data_ini"`
	DataFim                      string  `json:"data_fim"`
	TotalOperacoes               int     `json:"total_operacoes"`
	RecursoFinanceiroDisponivel  float64 `json:"recurso_financeiro_disponivel"`
	RecursoFinanceiroATransferir float64 `json:"recurso_financeiro_a_transferir"`
	CreditoAApropriar            float64 `json:"credito_a_apropriar"`
	CreditoNaoUtilizado          float64 `json:"credito_nao_utilizado"`
	CreditoUtilizado             float64 `json:"credito_utilizado"`
	DebitoEmAberto               float64 `json:"debito_em_aberto"`
	DebitoExtinto                float64 `json:"debito_extinto"`
}

// ResumoCGIBSHandler — GET /api/cgibs/resumo?data_ini=YYYY-MM-DD&data_fim=YYYY-MM-DD
//
// Endpoint substituto real do antigo "resumo por request" (cgibs_resumo/cgibs_requests,
// dropados na migration 132) — a spec de reescrita dos handlers (spec-cgibs-nova-solicitacao-
// drop-legado.md) removeu esse conceito sem oferecer um substituto; este é ele.
//
// Escolha de campo de data (documentada aqui por não haver um lugar mais natural na spec):
// filtra por cgibs_lancamentos.dth_lancto, NÃO por cgibs_operacoes.dth_emissao. Lançamentos são
// incrementais — um mesmo documento fiscal (operação) pode receber lançamentos de crédito/
// débito bem depois de emitido (ex.: apropriação de crédito meses após a emissão da nota,
// conforme o modelo de conta corrente fiscal do ANEXO I do MOC). Um "resumo do período
// X" deveria refletir os MOVIMENTOS ocorridos dentro do período consultado — como um extrato
// bancário filtra por data do lançamento, não por data de abertura da conta — e não os
// documentos emitidos nesse período (que é uma pergunta diferente, já respondida por
// ListarOperacoesCGIBSHandler). Se o negócio quiser a semântica "documentos emitidos no
// período" no futuro, é um segundo endpoint, não uma troca deste.
func ResumoCGIBSHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[CGIBSResumo]")
			return
		}

		qp := r.URL.Query()
		dataIniStr := strings.TrimSpace(qp.Get("data_ini"))
		dataFimStr := strings.TrimSpace(qp.Get("data_fim"))
		dataIni, ierr := time.Parse("2006-01-02", dataIniStr)
		if ierr != nil {
			jsonErr(w, http.StatusBadRequest, "data_ini inválida — use o formato AAAA-MM-DD")
			return
		}
		dataFim, ferr := time.Parse("2006-01-02", dataFimStr)
		if ferr != nil {
			jsonErr(w, http.StatusBadRequest, "data_fim inválida — use o formato AAAA-MM-DD")
			return
		}
		if dataFim.Before(dataIni) {
			jsonErr(w, http.StatusBadRequest, "data_fim não pode ser anterior a data_ini")
			return
		}

		var resumo CGIBSResumoPeriodo
		resumo.DataIni = dataIniStr
		resumo.DataFim = dataFimStr
		// l.company_id filtra direto (coluna existe, com índice idx_cgibs_lancamentos_company —
		// migration 131), mesmo princípio de defesa em profundidade já usado em
		// LancamentosOperacaoCGIBSHandler (revisão adversarial, item 6) — não depende só do JOIN
		// com cgibs_operacoes para escopar por empresa. O intervalo usa
		// dth_lancto >= $2::date AND dth_lancto < ($3::date + 1 dia) em vez de aplicar ::date na
		// coluna, para manter a comparação sargable (dth_lancto é TIMESTAMP WITH TIME ZONE).
		err = db.QueryRow(`
			SELECT
				COUNT(DISTINCT o.id),
				COALESCE(SUM(l.recurso_financeiro_disponivel), 0),
				COALESCE(SUM(l.recurso_financeiro_a_transferir), 0),
				COALESCE(SUM(l.credito_a_apropriar), 0),
				COALESCE(SUM(l.credito_nao_utilizado), 0),
				COALESCE(SUM(l.credito_utilizado), 0),
				COALESCE(SUM(l.debito_em_aberto), 0),
				COALESCE(SUM(l.debito_extinto), 0)
			FROM cgibs_lancamentos l
			JOIN cgibs_operacoes o ON o.id = l.operacao_id
			WHERE l.company_id = $1
			  AND l.dth_lancto >= $2::date
			  AND l.dth_lancto < ($3::date + INTERVAL '1 day')
		`, companyID, dataIniStr, dataFimStr).Scan(
			&resumo.TotalOperacoes,
			&resumo.RecursoFinanceiroDisponivel,
			&resumo.RecursoFinanceiroATransferir,
			&resumo.CreditoAApropriar,
			&resumo.CreditoNaoUtilizado,
			&resumo.CreditoUtilizado,
			&resumo.DebitoEmAberto,
			&resumo.DebitoExtinto,
		)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar resumo CGIBS", err, "[CGIBSResumo]")
			return
		}

		json.NewEncoder(w).Encode(resumo)
	}
}
