package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"fb_apu02/crypto"
	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
)

// CGIBSSolicitacao reflete as colunas de cgibs_solicitacoes (migration 131 — modelo de conta
// corrente fiscal; substitui o antigo CGIBSRequest/CGIBSResumo, que refletia o modelo
// período/tiquete copiado da RFB e nunca chegou a ser escrito por nenhum handler).
type CGIBSSolicitacao struct {
	ID                   string     `json:"id"`
	CNPJBase             string     `json:"cnpj_base"`
	IDSolicitacaoExterno *int64     `json:"id_solicitacao_externo"`
	TipoSolicitacao      string     `json:"tipo_solicitacao"`
	SituacaoSolicitacao  string     `json:"situacao_solicitacao"`
	DataSolicitacao      *time.Time `json:"data_solicitacao"`
	DataTransacaoIni     *time.Time `json:"data_transacao_ini"`
	DataTransacaoFim     *time.Time `json:"data_transacao_fim"`
	QtdOperacoes         int        `json:"qtd_operacoes"`
	QtdArqVinculados     int        `json:"qtd_arq_vinculados"`
	ErrorMessage         string     `json:"error_message"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// scanCGIBSSolicitacao lê uma linha de cgibs_solicitacoes no shape acima — usado tanto pela
// listagem (StatusCGIBSApuracaoHandler) quanto pelo retorno do INSERT em
// SolicitarCGIBSApuracaoHandler, para não duplicar a lista de colunas/scan em dois lugares.
func scanCGIBSSolicitacao(scan func(dest ...interface{}) error) (CGIBSSolicitacao, error) {
	var s CGIBSSolicitacao
	var idExterno sql.NullInt64
	var dataSolicitacao, dataTransacaoIni, dataTransacaoFim sql.NullTime
	err := scan(
		&s.ID, &s.CNPJBase, &idExterno, &s.TipoSolicitacao, &s.SituacaoSolicitacao,
		&dataSolicitacao, &dataTransacaoIni, &dataTransacaoFim,
		&s.QtdOperacoes, &s.QtdArqVinculados, &s.ErrorMessage,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return CGIBSSolicitacao{}, err
	}
	if idExterno.Valid {
		s.IDSolicitacaoExterno = &idExterno.Int64
	}
	if dataSolicitacao.Valid {
		s.DataSolicitacao = &dataSolicitacao.Time
	}
	if dataTransacaoIni.Valid {
		s.DataTransacaoIni = &dataTransacaoIni.Time
	}
	if dataTransacaoFim.Valid {
		s.DataTransacaoFim = &dataTransacaoFim.Time
	}
	return s, nil
}

// cgibsMaxPeriodoSolicitacao é o maior intervalo aceito entre data_ini e data_fim numa
// SolicitarCGIBSApuracaoHandler (revisão adversarial #2, item 6) — protege contra um período
// absurdamente grande sendo enviado por engano à API externa (ex.: troca de ano por dígito).
const cgibsMaxPeriodoSolicitacaoAnos = 1

// cgibsMaxFuturoSolicitacao é a tolerância aceita para data_fim além de "agora" (revisão
// adversarial #2, item 6): a apuração é sobre transações já ocorridas — um data_fim no futuro
// não faz sentido de negócio, mas 1 dia de folga evita rejeitar por causa de fuso
// horário/relógio do cliente ligeiramente adiantado.
const cgibsMaxFuturoSolicitacao = 24 * time.Hour

// cgibsResultadoIndicaSucesso decide se o campo Resultado da resposta de Nova Solicitação
// (services.NovaSolicitacao) indica sucesso DE NEGÓCIO — distinto do sucesso HTTP, que só
// confirma que a requisição chegou e foi processada pela CGIBS, não que ela aceitou o pedido
// (revisão adversarial #2, item 1).
//
// SUPOSIÇÃO: o MOC não documenta os valores possíveis de Resultado. Por analogia a outros
// campos de texto livre no mesmo estilo devolvidos pela CGIBS (ex. Habilitado, em
// cgibsHabilitacaoResponse, que usa "sucesso" como valor de sucesso confirmado em produção —
// ver cgibs_habilitar_test.go), tratamos como sucesso: string vazia (CGIBS não preencheu o
// campo) OU qualquer texto contendo "sucesso" (case-insensitive). Qualquer outro texto não
// vazio é tratado como mensagem de erro de negócio devolvida pela CGIBS. Se a CGIBS confirmar
// um vocabulário diferente, este é o ponto único de ajuste.
func cgibsResultadoIndicaSucesso(resultado string) bool {
	r := strings.ToLower(strings.TrimSpace(resultado))
	return r == "" || strings.Contains(r, "sucesso")
}

const cgibsSolicitacaoColumns = `
	id, cnpj_base, id_solicitacao_externo, tipo_solicitacao, situacao_solicitacao,
	data_solicitacao, data_transacao_ini, data_transacao_fim,
	qtd_operacoes, qtd_arq_vinculados, COALESCE(error_message,''),
	created_at, updated_at
`

// StatusCGIBSApuracaoHandler — GET /api/cgibs/apuracao/status
func StatusCGIBSApuracaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[StatusCGIBSApuracao]")
			return
		}

		rows, err := db.Query(`
			SELECT `+cgibsSolicitacaoColumns+`
			FROM cgibs_solicitacoes
			WHERE company_id = $1
			ORDER BY created_at DESC
			LIMIT 50
		`, companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar solicitações CGIBS", err, "[StatusCGIBSApuracao]")
			return
		}
		defer rows.Close()

		var solicitacoes []CGIBSSolicitacao
		for rows.Next() {
			s, err := scanCGIBSSolicitacao(rows.Scan)
			if err != nil {
				log.Printf("[StatusCGIBSApuracao] Erro ao ler linha: %v", err)
				continue
			}
			solicitacoes = append(solicitacoes, s)
		}
		if solicitacoes == nil {
			solicitacoes = []CGIBSSolicitacao{}
		}

		// Verificar se há credencial com agendamento (não muda nesta sub-spec).
		var agendamentoAtivo bool
		var horarioAgendamento string
		db.QueryRow(`
			SELECT COALESCE(agendamento_ativo,false), COALESCE(TO_CHAR(horario_agendamento,'HH24:MI'),'06:00')
			FROM cgibs_credentials WHERE company_id=$1
		`, companyID).Scan(&agendamentoAtivo, &horarioAgendamento)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"solicitacoes":        solicitacoes,
			"agendamento_ativo":   agendamentoAtivo,
			"horario_agendamento": horarioAgendamento,
		})
	}
}

// SolicitarCGIBSApuracaoHandler — POST /api/cgibs/apuracao/solicitar
// Aciona a API de Nova Solicitação da CGIBS (MOC 5.5) para o período informado no corpo
// ({data_ini, data_fim}, AAAA-MM-DD) e persiste o resultado em cgibs_solicitacoes. Exige
// credencial ativa E habilitada (services.Habilitar já confirmado pela CGIBS) — mesma checagem
// de estado já usada por matchCGIBSCredential (cgibs_webhook.go): uma credencial nunca
// confirmada não deveria conseguir disparar uma solicitação real.
func SolicitarCGIBSApuracaoHandler(db *sql.DB) http.HandlerFunc {
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
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[SolicitarCGIBSApuracao]")
			return
		}

		var req struct {
			DataIni string `json:"data_ini"`
			DataFim string `json:"data_fim"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "Requisição inválida")
			return
		}
		dataIni, ierr := time.Parse("2006-01-02", strings.TrimSpace(req.DataIni))
		if ierr != nil {
			jsonErr(w, http.StatusBadRequest, "data_ini inválida — use o formato AAAA-MM-DD")
			return
		}
		dataFim, ferr := time.Parse("2006-01-02", strings.TrimSpace(req.DataFim))
		if ferr != nil {
			jsonErr(w, http.StatusBadRequest, "data_fim inválida — use o formato AAAA-MM-DD")
			return
		}
		if dataFim.Before(dataIni) {
			jsonErr(w, http.StatusBadRequest, "data_fim não pode ser anterior a data_ini")
			return
		}
		// Revisão adversarial #2, item 6: período absurdamente grande ou data_fim no futuro
		// distante são rejeitados ANTES de chamar a API externa — mesmo espírito da checagem de
		// habilitado acima (falha rápida, sem gastar uma chamada de rede com um pedido inválido).
		if dataFim.After(dataIni.AddDate(cgibsMaxPeriodoSolicitacaoAnos, 0, 0)) {
			jsonErr(w, http.StatusBadRequest, "Período solicitado não pode ser maior que 1 ano")
			return
		}
		if dataFim.After(time.Now().Add(cgibsMaxFuturoSolicitacao)) {
			jsonErr(w, http.StatusBadRequest, "data_fim não pode estar no futuro")
			return
		}

		// Busca a credencial ativa+habilitada da empresa ANTES de tentar qualquer chamada de
		// rede — se não estiver habilitada, erro claro e imediato (boundary da spec).
		var clientID, clientSecretEnc, cnpjMatriz string
		var ativo, habilitado bool
		err = db.QueryRow(`
			SELECT client_id, client_secret, cnpj_matriz, ativo, habilitado
			FROM cgibs_credentials WHERE company_id = $1
		`, companyID).Scan(&clientID, &clientSecretEnc, &cnpjMatriz, &ativo, &habilitado)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusBadRequest, "Nenhuma credencial CGIBS cadastrada para esta empresa")
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar credencial CGIBS", err, "[SolicitarCGIBSApuracao]")
			return
		}
		if !ativo || !habilitado {
			jsonErr(w, http.StatusBadRequest, "Credencial CGIBS não habilitada — conclua a Habilitação do Contribuinte antes de solicitar a apuração")
			return
		}

		// Recusa duplo clique / solicitação repetida (revisão adversarial, item 3): se já existe
		// uma solicitação em andamento (situacao_solicitacao='solicitada' e sem erro registrado —
		// ou seja, ainda não resolvida pela CGIBS nem marcada como falha), não cria outra.
		var emAndamento bool
		if err := db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM cgibs_solicitacoes
				WHERE company_id = $1 AND situacao_solicitacao = 'solicitada' AND error_message IS NULL
			)
		`, companyID).Scan(&emAndamento); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao verificar solicitações em andamento", err, "[SolicitarCGIBSApuracao]")
			return
		}
		if emAndamento {
			jsonErr(w, http.StatusConflict, "Já existe uma solicitação CGIBS em andamento para esta empresa — aguarde a resolução antes de solicitar novamente")
			return
		}

		clientSecret := crypto.DecryptFieldWithFallback(clientSecretEnc)
		cnpjBase := extractCNPJBase(cnpjMatriz)

		// SUPOSIÇÃO: CNPJ enviado é a raiz de 8 dígitos (cnpjBase), não o CNPJ completo de 14
		// dígitos — mesmo padrão assumido para o restante da integração CGIBS (ver
		// matchCGIBSCredential, cgibs_webhook.go), mas a seção 5.5 do MOC não confirma o formato
		// exato deste campo. Se a CGIBS exigir os 14 dígitos, é aqui que ajustar (usar cnpjMatriz).
		resp, nerr := services.NovaSolicitacao(clientID, clientSecret, cnpjBase, dataIni, dataFim)
		if nerr != nil {
			if errors.Is(nerr, services.ErrCGIBSNovaSolicitacaoNaoConfigurada) {
				jsonErr(w, http.StatusServiceUnavailable, "Integração CGIBS não configurada (CGIBS_NOVA_SOLICITACAO_URL ausente)")
				return
			}
			// Persiste a falha ANTES de responder (revisão adversarial, item 2): sem isso,
			// cgibs_solicitacoes nunca ganhava uma linha com error_message preenchido, e
			// ClearErrorsCGIBSHandler (que filtra por error_message IS NOT NULL) era um no-op
			// morto — a falha ficava invisível para o usuário e não havia o que "limpar".
			// Best-effort: se o INSERT falhar, só loga — a resposta 502 ao cliente não pode
			// depender de um segundo ponto de falha.
			if _, ierr := db.Exec(`
				INSERT INTO cgibs_solicitacoes (
					company_id, cnpj_base, situacao_solicitacao, data_transacao_ini, data_transacao_fim, error_message
				) VALUES ($1, $2, 'solicitada', $3, $4, $5)
			`, companyID, cnpjBase, dataIni, dataFim, nerr.Error()); ierr != nil {
				log.Printf("[SolicitarCGIBSApuracao] Erro ao gravar falha da solicitação: %v", ierr)
			}
			sanitizeDBErr(w, http.StatusBadGateway, "Erro ao solicitar apuração à CGIBS", nerr, "[SolicitarCGIBSApuracao]")
			return
		}

		// Revisão adversarial #2, item 1: sucesso HTTP não significa sucesso de negócio — a
		// CGIBS pode responder 2xx com um Resultado indicando que o pedido não foi aceito (ver
		// cgibsResultadoIndicaSucesso). Sem esta checagem, uma resposta assim seria gravada como
		// uma solicitação "de sucesso" normal, escondendo a falha do usuário.
		if !cgibsResultadoIndicaSucesso(resp.Resultado) {
			log.Printf("[SolicitarCGIBSApuracao] CGIBS respondeu HTTP OK mas Resultado indica falha de negócio: %q", resp.Resultado)
			if _, ierr := db.Exec(`
				INSERT INTO cgibs_solicitacoes (
					company_id, cnpj_base, situacao_solicitacao, data_transacao_ini, data_transacao_fim, error_message
				) VALUES ($1, $2, 'solicitada', $3, $4, $5)
			`, companyID, cnpjBase, dataIni, dataFim, resp.Resultado); ierr != nil {
				log.Printf("[SolicitarCGIBSApuracao] Erro ao gravar falha de negócio da solicitação: %v", ierr)
			}
			sanitizeDBErr(w, http.StatusBadGateway, "CGIBS recusou a solicitação de apuração", fmt.Errorf("Resultado=%q", resp.Resultado), "[SolicitarCGIBSApuracao]")
			return
		}

		// Normaliza os valores recebidos para os CHECKs de cgibs_solicitacoes — mesmas funções
		// já usadas por upsertCGIBSSolicitacao (cgibs_webhook.go), inclusive o mesmo default
		// seguro ('diferencial'/'solicitada') quando a CGIBS devolve algo não reconhecido.
		tipo, _ := normalizeTipoSolicitacao(resp.TipoSolicitacao)
		situacao, _ := normalizeSituacaoSolicitacao(resp.SituacaoSolicitacao)

		var idExternoParam interface{}
		if resp.IDSolicitacao > 0 {
			idExternoParam = resp.IDSolicitacao
		}
		// Se a CGIBS não devolver (ou devolver num formato não reconhecido) as datas de
		// transação, grava o período efetivamente pedido pelo usuário em vez de deixar NULL.
		dataTransacaoIniParam := dataIni
		if !resp.DataTransacaoIni.IsZero() {
			dataTransacaoIniParam = resp.DataTransacaoIni
		}
		dataTransacaoFimParam := dataFim
		if !resp.DataTransacaoFim.IsZero() {
			dataTransacaoFimParam = resp.DataTransacaoFim
		}
		// Revisão adversarial #2, item 4: dataTransacaoIniParam/dataTransacaoFimParam acima podem
		// misturar um valor devolvido pela CGIBS com um valor de fallback local (ex.: a CGIBS
		// devolve DataTransacaoFim num formato não reconhecido — vira zero, current fallback usa
		// dataFim pedido — mas devolve DataTransacaoIni válida e anterior a dataFim original). Essa
		// mistura pode violar o CHECK data_transacao_ini <= data_transacao_fim de
		// cgibs_solicitacoes. Em vez de deixar o INSERT abaixo falhar com um erro de constraint
		// cru, detecta a violação aqui e descarta AMBOS os valores da CGIBS, usando o par original
		// pedido pelo usuário por inteiro — nunca uma combinação parcial que quebre a invariante.
		if dataTransacaoIniParam.After(dataTransacaoFimParam) {
			log.Printf("[SolicitarCGIBSApuracao] AVISO: par de datas resultante (ini=%v fim=%v) violaria data_transacao_ini<=data_transacao_fim — descartando os valores da CGIBS e usando o período original pedido (ini=%v fim=%v)",
				dataTransacaoIniParam, dataTransacaoFimParam, dataIni, dataFim)
			dataTransacaoIniParam = dataIni
			dataTransacaoFimParam = dataFim
		}

		s, err := scanCGIBSSolicitacao(db.QueryRow(`
			INSERT INTO cgibs_solicitacoes (
				company_id, cnpj_base, id_solicitacao_externo, tipo_solicitacao, situacao_solicitacao,
				data_solicitacao, data_transacao_ini, data_transacao_fim, resultado, updated_at
			) VALUES ($1, $2, $3, $4, $5, NOW(), $6, $7, $8, NOW())
			RETURNING `+cgibsSolicitacaoColumns+`
		`, companyID, cnpjBase, idExternoParam, tipo, situacao,
			dataTransacaoIniParam, dataTransacaoFimParam, resp.Resultado,
		).Scan)
		if err != nil {
			// Revisão adversarial #2, item 7: a CGIBS já aceitou o pedido (sucesso HTTP e de
			// negócio, confirmados acima) — se o INSERT falhar por colisão em
			// uq_cgibs_solicitacoes_externo (ex.: IDSolicitacao já persistido por uma chamada
			// anterior/concorrente), isso é uma situação estranha o suficiente para merecer
			// destaque no log (sucesso externo sem conseguirmos persistir local) e uma resposta
			// clara ao cliente, não um 500 genérico com erro cru de constraint.
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				log.Printf("[SolicitarCGIBSApuracao] ATENÇÃO: CGIBS aceitou a solicitação (idSolicitacaoExterno=%v) mas o INSERT local colidiu com uq_cgibs_solicitacoes_externo (constraint=%s) — requer investigação manual: %v",
					idExternoParam, pqErr.Constraint, err)
				jsonErr(w, http.StatusConflict, "Já existe uma solicitação registrada com este identificador externo — a CGIBS pode ter processado o pedido mais de uma vez")
				return
			}
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao gravar solicitação CGIBS", err, "[SolicitarCGIBSApuracao]")
			return
		}

		log.Printf("[SolicitarCGIBSApuracao] companyID=%s solicitação criada id=%s idSolicitacaoExterno=%v situacao=%s",
			companyID, s.ID, s.IDSolicitacaoExterno, s.SituacaoSolicitacao)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"solicitacao": s,
			"message":     "Solicitação enviada à CGIBS com sucesso",
		})
	}
}

// ClearErrorsCGIBSHandler — DELETE /api/cgibs/apuracao/clear-errors
func ClearErrorsCGIBSHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodDelete {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[ClearErrorsCGIBS]")
			return
		}
		if _, err := db.Exec(`DELETE FROM cgibs_solicitacoes WHERE company_id=$1 AND error_message IS NOT NULL`, companyID); err != nil {
			log.Printf("[ClearErrorsCGIBS] Erro ao remover registros de erro: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "Erros removidos"})
	}
}

// DetalheCGIBSHandler — DELETE /api/cgibs/apuracao/{id}
// Se a solicitação ainda está 'solicitada' OU 'enviada' (revisão adversarial #2, item 2 — a
// CHECK de cgibs_solicitacoes.situacao_solicitacao tem os dois estados antes de 'gerada', então
// o cancelamento remoto deveria ser tentado nos dois) E já foi aceita pela CGIBS
// (id_solicitacao_externo preenchido), tenta cancelar do lado deles (services.CancelarSolicitacao)
// antes de apagar local — best-effort: uma falha no cancelamento remoto é apenas logada, o
// delete local acontece de qualquer forma (o usuário pediu remoção). Se a solicitação nunca
// chegou a ser aceita (sem id_solicitacao_externo) ou já está em qualquer situação além dessas
// duas (gerada/cancelada/expirada), o delete é local direto, sem chamar a API.
//
// Revisão adversarial #2, item 3: ANTES de qualquer cancelamento/delete, recusa a exclusão
// (409) se a solicitação já tiver cgibs_arquivos vinculados — um DELETE em cgibs_solicitacoes
// arrastaria via CASCADE os arquivos (e o raw_json de auditoria de cada um) e, por
// consequência, os dados já processados no ledger (cgibs_operacoes/cgibs_lancamentos, que
// referenciam o arquivo via ultimo_arquivo_id/arquivo_id). Isso vale independente da situação
// da solicitação — mesmo uma solicitação 'gerada'/'enviada' com arquivo já baixado não pode ser
// apagada silenciosamente.
func DetalheCGIBSHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims["user_id"].(string)
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao obter empresa", err, "[DetalheCGIBS]")
			return
		}

		id := r.URL.Path[len("/api/cgibs/apuracao/"):]
		if r.Method == http.MethodDelete {
			// Valida o formato do {id} ANTES de usá-lo na query (revisão adversarial, item 5):
			// sem isso, um id não-UUID vira erro cru do Postgres ("invalid input syntax for
			// uuid") e um 500 em vez de um 400 claro. reUUID já existe em
			// pagamentos_fornecedores.go (mesmo pacote handlers) — reaproveitado aqui.
			if !reUUID.MatchString(id) {
				jsonErr(w, http.StatusBadRequest, "ID de solicitação inválido")
				return
			}
			var situacao string
			var idExterno sql.NullInt64
			var clientID, clientSecretEnc sql.NullString
			// cgibs_credentials.company_id é TEXT (schema pré-existente, migration 102, sem FK)
			// enquanto cgibs_solicitacoes.company_id é UUID (migration 131) — precisa de cast
			// explícito para o JOIN (mesma observação já documentada em
			// setupCGIBSHabilitarTest/cgibs_habilitar_test.go).
			err = db.QueryRow(`
				SELECT s.situacao_solicitacao, s.id_solicitacao_externo, c.client_id, c.client_secret
				FROM cgibs_solicitacoes s
				LEFT JOIN cgibs_credentials c ON c.company_id = s.company_id::text
				WHERE s.id = $1 AND s.company_id = $2
			`, id, companyID).Scan(&situacao, &idExterno, &clientID, &clientSecretEnc)
			if err != nil && err != sql.ErrNoRows {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar solicitação CGIBS", err, "[DetalheCGIBS]")
				return
			}

			if err == nil {
				var arquivosVinculados int
				if cerr := db.QueryRow(`SELECT COUNT(*) FROM cgibs_arquivos WHERE solicitacao_id=$1`, id).Scan(&arquivosVinculados); cerr != nil {
					sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao verificar arquivos vinculados", cerr, "[DetalheCGIBS]")
					return
				}
				if arquivosVinculados > 0 {
					jsonErr(w, http.StatusConflict, fmt.Sprintf("Não é possível remover: existem %d arquivo(s) já vinculado(s)/baixado(s) a esta solicitação", arquivosVinculados))
					return
				}
			}

			if err == nil && (situacao == "solicitada" || situacao == "enviada") && idExterno.Valid && clientID.Valid && clientID.String != "" {
				clientSecret := crypto.DecryptFieldWithFallback(clientSecretEnc.String)
				if _, cerr := services.CancelarSolicitacao(clientID.String, clientSecret, idExterno.Int64); cerr != nil {
					log.Printf("[DetalheCGIBS] AVISO: falha ao cancelar solicitação %d na CGIBS (best-effort, seguindo com delete local): %v", idExterno.Int64, cerr)
				}
			}

			if _, err := db.Exec(`DELETE FROM cgibs_solicitacoes WHERE id=$1 AND company_id=$2`, id, companyID); err != nil {
				log.Printf("[DetalheCGIBS] Erro ao deletar solicitação: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
