package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"database/sql"

	"fb_apu02/services"

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

	// ─── Apuração Mensal (spec-apuracao-mensal-cbs-ibs.md) ───────────────────
	StatusPeriodo     string                  `json:"status_periodo"`
	DataFechamento    string                  `json:"data_fechamento"`
	SaldoAPagar       float64                 `json:"saldo_a_pagar"`
	AlertasDecadencia []CGIBSAlertaDecadencia `json:"alertas_decadencia"`
}

// CGIBSAlertaDecadencia é um alerta de decadência de crédito IBS (prazo de 5 anos, com
// antecedência de 1 ano — dispara a partir de 4 anos desde a apropriação) para uma operação
// (documento fiscal) cujo saldo de crédito não utilizado ainda é positivo e tem pelo menos um
// lançamento antigo contribuindo para esse saldo. Ver query em alertasDecadenciaCGIBS: é uma
// aproximação, não um cálculo fiscal definitivo (documentado no comentário da query).
type CGIBSAlertaDecadencia struct {
	ChaveAcesso   string    `json:"chave_acesso"`
	OperacaoID    string    `json:"operacao_id"`
	Valor         float64   `json:"valor"`
	DthLancto     time.Time `json:"dth_lancto"`
	DiasRestantes int       `json:"dias_restantes"`
	// JaDecaido é true quando dias_restantes já é negativo (lançamento mais antigo passou
	// dos 5 anos) — o crédito já expirou de fato, não é mais "alerta preventivo". Sem limite
	// superior na query (não filtramos lançamentos com mais de 5 anos), então esse caso
	// aparece na mesma lista; o frontend usa este campo pra rotular como perda consumada em
	// vez de mostrar só um número negativo de dias.
	JaDecaido bool `json:"ja_decaido"`
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

		// saldo_a_pagar é literalmente debito_em_aberto já somado acima — só expõe com o
		// rótulo de negócio, sem nova query nem mudança na agregação existente.
		resumo.SaldoAPagar = resumo.DebitoEmAberto

		// status_periodo/data_fechamento usam o mês de referência de data_fim do range
		// pedido (spec: "normalizado pro mês de data_fim").
		modo := services.ResolveModoCalendario()
		status, dataFechamento := services.StatusPeriodoApuracao(dataFim, modo)
		resumo.StatusPeriodo = status
		resumo.DataFechamento = dataFechamento.Format(time.RFC3339)

		alertas, alertaErr := alertasDecadenciaCGIBS(db, companyID)
		if alertaErr != nil {
			// Erro na query de alertas não deve derrubar o resumo inteiro — é uma
			// informação auxiliar, não o dado principal do endpoint.
			log.Printf("[CGIBSResumo] erro ao consultar alertas de decadência: %v", alertaErr)
			alertas = []CGIBSAlertaDecadencia{}
		}
		resumo.AlertasDecadencia = alertas

		json.NewEncoder(w).Encode(resumo)
	}
}

// alertasDecadenciaCGIBS lista operações (documentos fiscais IBS) com saldo de crédito não
// utilizado atualmente positivo e que têm pelo menos um lançamento com mais de 4 anos
// (dth_lancto < NOW() - 4 anos) contribuindo para esse saldo — alerta de decadência com 1 ano
// de antecedência do prazo real de 5 anos (Lei Complementar 214/2025, decadência do crédito
// de IBS já apropriado e não utilizado).
//
// Consulta SEPARADA da agregação de resumo acima (não interfere nela) — agrupa por operação
// SEM filtro de período: o saldo de credito_nao_utilizado é cumulativo (lançamentos
// incrementais, ver migration 131), então "saldo real atual" só existe somando TODOS os
// lançamentos da operação, independente do range data_ini/data_fim pedido ao endpoint.
//
// SUPOSIÇÃO: quando o saldo positivo de uma operação é formado por uma mistura de lançamentos
// antigos (>4 anos) e recentes, não há como saber com precisão fiscal qual fração do saldo
// "pertence" a cada um — o 7º campo do extrato_cc (credito_nao_utilizado) é um valor
// incremental acumulado, não uma fila FIFO/LIFO rastreável por lançamento. Esta query
// simplifica isso assumindo o pior caso: se EXISTE qualquer lançamento antigo que tenha
// contribuído para o saldo atualmente positivo, todo o saldo entra no alerta, e a idade usada
// para contar os "dias restantes" até a decadência de 5 anos é a do lançamento antigo MAIS
// ANTIGO (MIN) — ou seja, o cenário mais conservador/alarmante possível. Isso pode
// superestimar o valor em risco e antecipar o alerta além do que seria estritamente correto
// (parte do saldo pode na verdade vir de apropriação recente) — aceitável para um ALERTA
// informativo, não para o cálculo fiscal definitivo da decadência.
func alertasDecadenciaCGIBS(db *sql.DB, companyID string) ([]CGIBSAlertaDecadencia, error) {
	rows, err := db.Query(`
		SELECT
			o.chave_acesso,
			o.id,
			SUM(l.credito_nao_utilizado) AS saldo,
			MIN(l.dth_lancto) FILTER (WHERE l.dth_lancto < NOW() - INTERVAL '4 years') AS dth_antigo
		FROM cgibs_lancamentos l
		JOIN cgibs_operacoes o ON o.id = l.operacao_id
		WHERE l.company_id = $1
		GROUP BY o.id, o.chave_acesso
		HAVING SUM(l.credito_nao_utilizado) > 0
		   AND MIN(l.dth_lancto) FILTER (WHERE l.dth_lancto < NOW() - INTERVAL '4 years') IS NOT NULL
		ORDER BY dth_antigo ASC
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	agora := time.Now()
	var alertas []CGIBSAlertaDecadencia
	for rows.Next() {
		var a CGIBSAlertaDecadencia
		if err := rows.Scan(&a.ChaveAcesso, &a.OperacaoID, &a.Valor, &a.DthLancto); err != nil {
			return nil, err
		}
		prazoDecadencia := a.DthLancto.AddDate(5, 0, 0)
		a.DiasRestantes = int(prazoDecadencia.Sub(agora).Hours() / 24)
		a.JaDecaido = a.DiasRestantes < 0
		alertas = append(alertas, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if alertas == nil {
		alertas = []CGIBSAlertaDecadencia{}
	}
	return alertas, nil
}
