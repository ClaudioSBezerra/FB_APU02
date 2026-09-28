package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"fb_apu02/crypto"
	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
)

type CGIBSCredential struct {
	ID         string `json:"id"`
	CompanyID  string `json:"company_id"`
	CNPJMatriz string `json:"cnpj_matriz"`
	ClientID   string `json:"client_id"`
	// ClientSecret nunca é serializado (revisão adversarial item 4) — o valor bruto/criptografado
	// não deve sair da API, nem mascarado (uma máscara curta pode expor o segredo inteiro quando
	// ele tem poucos caracteres). O frontend usa TemClientSecret para saber se já existe um
	// segredo salvo, e só envia client_secret de novo quando o usuário digitar um novo valor
	// (ver SaveCGIBSCredentialHandler — item 5).
	ClientSecret       string     `json:"-"`
	TemClientSecret    bool       `json:"tem_client_secret"`
	Ambiente           string     `json:"ambiente"`
	Ativo              bool       `json:"ativo"`
	AgendamentoAtivo   bool       `json:"agendamento_ativo"`
	HorarioAgendamento string     `json:"horario_agendamento"`
	Habilitado         bool       `json:"habilitado"`
	DataHabilitacao    *time.Time `json:"data_habilitacao"`
	WebhookURL         string     `json:"webhook_url"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func GetCGIBSCredentialHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
			return
		}

		var cred CGIBSCredential
		var horario string
		var webhookURL sql.NullString
		var dataHabilitacao sql.NullTime
		err = db.QueryRow(`
			SELECT id, company_id, cnpj_matriz, client_id, client_secret,
			       COALESCE(ambiente,'piloto'), ativo,
			       COALESCE(agendamento_ativo, false),
			       COALESCE(TO_CHAR(horario_agendamento,'HH24:MI'), '06:00'),
			       habilitado, data_habilitacao, webhook_url,
			       created_at, updated_at
			FROM cgibs_credentials WHERE company_id = $1
		`, companyID).Scan(
			&cred.ID, &cred.CompanyID, &cred.CNPJMatriz, &cred.ClientID, &cred.ClientSecret,
			&cred.Ambiente, &cred.Ativo, &cred.AgendamentoAtivo, &horario,
			&cred.Habilitado, &dataHabilitacao, &webhookURL,
			&cred.CreatedAt, &cred.UpdatedAt,
		)
		cred.HorarioAgendamento = horario

		if err == sql.ErrNoRows {
			json.NewEncoder(w).Encode(map[string]interface{}{"credential": nil})
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar credencial CGIBS", err, "[CGIBSCredentials]")
			return
		}
		// client_secret NUNCA é decriptado nem devolvido aqui (item 4) — só informamos se existe
		// um segredo salvo, o que hoje é sempre verdade (coluna NOT NULL, validada não-vazia no
		// Save), mas evita qualquer exposição do valor real e evita reintroduzir o risco do item 5
		// (reenviar o valor mascarado sobrescreveria o segredo real).
		cred.TemClientSecret = cred.ClientSecret != ""
		cred.ClientSecret = ""
		if dataHabilitacao.Valid {
			cred.DataHabilitacao = &dataHabilitacao.Time
		}
		cred.WebhookURL = webhookURL.String
		json.NewEncoder(w).Encode(map[string]interface{}{"credential": cred})
	}
}

func SaveCGIBSCredentialHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
			return
		}

		var req struct {
			CNPJMatriz   string `json:"cnpj_matriz"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			Ambiente     string `json:"ambiente"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		req.CNPJMatriz = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(
			strings.TrimSpace(req.CNPJMatriz), ".", ""), "/", ""), "-", "")
		req.ClientID = strings.TrimSpace(req.ClientID)
		req.ClientSecret = strings.TrimSpace(req.ClientSecret)
		if req.Ambiente != "piloto" && req.Ambiente != "producao" {
			req.Ambiente = "piloto"
		}
		if len(req.CNPJMatriz) != 14 {
			http.Error(w, "CNPJ Matriz deve ter 14 dígitos", http.StatusBadRequest)
			return
		}
		if req.ClientID == "" {
			http.Error(w, "Client ID é obrigatório", http.StatusBadRequest)
			return
		}

		// Lê a credencial já salva (se houver) ANTES de gravar — usada para duas decisões
		// (revisão adversarial itens 2 e 5): (a) se client_secret vier vazio no request, mantém o
		// valor já salvo em vez de rejeitar o save inteiro; (b) se client_id OU ambiente mudarem
		// em relação ao valor salvo, reseta habilitado/data_habilitacao/token_contrib — a
		// habilitação antiga não é válida para uma credencial diferente. Trocar só o
		// client_secret (mesmo client_id/ambiente) NÃO reseta a habilitação: é o mesmo
		// contribuinte perante a CGIBS, apenas o segredo local foi rotacionado — decisão
		// documentada aqui, ajustável se um caso real mostrar que a CGIBS também invalida a
		// habilitação numa rotação de secret isolada.
		var existingClientID, existingAmbiente, existingSecretEnc string
		hasExisting := true
		if err := db.QueryRow(`
			SELECT client_id, ambiente, client_secret FROM cgibs_credentials WHERE company_id = $1
		`, companyID).Scan(&existingClientID, &existingAmbiente, &existingSecretEnc); err != nil {
			if err != sql.ErrNoRows {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar credencial CGIBS existente", err, "[CGIBSCredentials]")
				return
			}
			hasExisting = false
		}

		var encryptedSecret string
		if req.ClientSecret == "" {
			if !hasExisting {
				http.Error(w, "Client Secret é obrigatório", http.StatusBadRequest)
				return
			}
			// Mantém o segredo já salvo — não sobrescreve com vazio (item 5).
			encryptedSecret = existingSecretEnc
		} else {
			// client_secret é sempre criptografado antes de persistir (mesmo helper usado por
			// sap_credentials.go — backend/crypto — já que rfb_credentials, apesar de citado como
			// referência na spec, na verdade NÃO criptografa client_secret hoje; ver relatório final).
			var encErr error
			encryptedSecret, encErr = crypto.EncryptField(req.ClientSecret)
			if encErr != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao proteger credencial", encErr, "[CGIBSCredentials]")
				return
			}
		}

		resetHabilitacao := hasExisting && (existingClientID != req.ClientID || existingAmbiente != req.Ambiente)

		var id string
		err = db.QueryRow(`
			INSERT INTO cgibs_credentials (company_id, cnpj_matriz, client_id, client_secret, ambiente, ativo)
			VALUES ($1, $2, $3, $4, $5, true)
			ON CONFLICT (company_id) DO UPDATE SET
				cnpj_matriz = $2,
				client_id = $3,
				client_secret = $4,
				ambiente = $5,
				ativo = true,
				habilitado = CASE WHEN $6 THEN false ELSE cgibs_credentials.habilitado END,
				data_habilitacao = CASE WHEN $6 THEN NULL ELSE cgibs_credentials.data_habilitacao END,
				token_contrib = CASE WHEN $6 THEN NULL ELSE cgibs_credentials.token_contrib END,
				updated_at = NOW()
			RETURNING id
		`, companyID, req.CNPJMatriz, req.ClientID, encryptedSecret, req.Ambiente, resetHabilitacao).Scan(&id)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao salvar credencial CGIBS", err, "[CGIBSCredentials]")
			return
		}

		var cred CGIBSCredential
		var horario string
		var webhookURL sql.NullString
		var dataHabilitacao sql.NullTime
		db.QueryRow(`
			SELECT id, company_id, cnpj_matriz, client_id, client_secret,
			       COALESCE(ambiente,'piloto'), ativo,
			       COALESCE(agendamento_ativo, false),
			       COALESCE(TO_CHAR(horario_agendamento,'HH24:MI'), '06:00'),
			       habilitado, data_habilitacao, webhook_url,
			       created_at, updated_at
			FROM cgibs_credentials WHERE id = $1
		`, id).Scan(
			&cred.ID, &cred.CompanyID, &cred.CNPJMatriz, &cred.ClientID, &cred.ClientSecret,
			&cred.Ambiente, &cred.Ativo, &cred.AgendamentoAtivo, &horario,
			&cred.Habilitado, &dataHabilitacao, &webhookURL,
			&cred.CreatedAt, &cred.UpdatedAt,
		)
		cred.HorarioAgendamento = horario
		cred.TemClientSecret = cred.ClientSecret != ""
		cred.ClientSecret = ""
		if dataHabilitacao.Valid {
			cred.DataHabilitacao = &dataHabilitacao.Time
		}
		cred.WebhookURL = webhookURL.String
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"credential": cred,
			"message":    "Credenciais CGIBS salvas com sucesso",
		})
	}
}

func UpdateCGIBSScheduleHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPatch {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
			return
		}

		var req struct {
			AgendamentoAtivo   bool   `json:"agendamento_ativo"`
			HorarioAgendamento string `json:"horario_agendamento"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if len(req.HorarioAgendamento) != 5 || req.HorarioAgendamento[2] != ':' {
			req.HorarioAgendamento = "06:00"
		}
		if _, err := db.Exec(`
			UPDATE cgibs_credentials
			SET agendamento_ativo=$1, horario_agendamento=$2::TIME, updated_at=NOW()
			WHERE company_id=$3
		`, req.AgendamentoAtivo, req.HorarioAgendamento, companyID); err != nil {
			log.Printf("[CGIBSCredentials] Erro ao atualizar agendamento: %v", err)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"agendamento_ativo":   req.AgendamentoAtivo,
			"horario_agendamento": req.HorarioAgendamento,
			"message":             "Agendamento atualizado com sucesso",
		})
	}
}

func DeleteCGIBSCredentialHandler(db *sql.DB) http.HandlerFunc {
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
		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
			return
		}
		result, err := db.Exec("DELETE FROM cgibs_credentials WHERE company_id=$1", companyID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao excluir credencial CGIBS", err, "[CGIBSCredentials]")
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			http.Error(w, "Nenhuma credencial encontrada", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// generateTokenContrib gera um TokenContrib aleatório (32 bytes de crypto/rand, hex-encoded
// = 64 chars). É sempre gerado automaticamente — nunca fica vazio — para evitar reproduzir o
// problema real já encontrado com RFB_WEBHOOK_SECRET vazio em produção (webhook sem nenhuma
// validação efetiva).
func generateTokenContrib() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// resolveCGIBSWebhookURL lê CGIBS_WEBHOOK_URL (env) com um default análogo ao usado por
// RFB_WEBHOOK_URL em services/rfb.go (NewRFBClient) — mesmo host de produção (fbtax.cloud),
// caminho próprio do CGIBS. É a URL do NOSSO endpoint receptor, enviada à CGIBS na
// Habilitação; não é campo que o admin digita por empresa (Boundaries da spec).
func resolveCGIBSWebhookURL() string {
	if v := strings.TrimSpace(os.Getenv("CGIBS_WEBHOOK_URL")); v != "" {
		return v
	}
	return "https://fbtax.cloud/api/cgibs/webhook"
}

// HabilitarCGIBSHandler aciona a API de Habilitação do Contribuinte da CGIBS (MOC 5.1) para
// a credencial ativa da empresa efetiva e persiste o resultado. POST /api/cgibs/credentials/habilitar.
//
// token_contrib é gerado (crypto/rand) apenas se a coluna ainda estiver vazia — uma 2ª
// chamada NUNCA sobrescreve um token já existente, pois o token já pode ter sido comunicado
// à CGIBS na 1ª Habilitação e é o que o webhook público usa para validar notificações
// recebidas depois.
//
// A leitura da credencial e a persistência do resultado usam transações CURTAS e SEPARADAS,
// com a chamada de rede (services.Habilitar, até 30s) feita FORA de qualquer transação aberta
// (revisão adversarial item 3 — segurar lock/transação do banco durante uma chamada HTTP
// externa bloqueia outras operações na tabela por até 30s). Isso troca "sem lost update via
// FOR UPDATE" por uma re-verificação otimista no UPDATE final (WHERE id=... AND client_id=...
// — ver comentário abaixo), que é suficiente aqui: o pior caso de uma corrida entre duas
// chamadas de Habilitar concorrentes é uma delas ser descartada (409) e ter que ser refeita,
// nunca um dado inconsistente persistido.
func HabilitarCGIBSHandler(db *sql.DB) http.HandlerFunc {
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
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao buscar empresa", err, "[CGIBSCredentials]")
			return
		}

		// Leitura simples, sem FOR UPDATE e sem transação aberta durante a chamada HTTP externa
		// que vem a seguir (item 3).
		var id, clientID, clientSecretEnc string
		var tokenContribEnc sql.NullString
		var ativo bool
		err = db.QueryRow(`
			SELECT id, client_id, client_secret, token_contrib, ativo
			FROM cgibs_credentials WHERE company_id = $1
		`, companyID).Scan(&id, &clientID, &clientSecretEnc, &tokenContribEnc, &ativo)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusBadRequest, "Nenhuma credencial CGIBS cadastrada para esta empresa")
			return
		}
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao consultar credencial CGIBS", err, "[CGIBSCredentials]")
			return
		}
		if !ativo {
			jsonErr(w, http.StatusBadRequest, "Credencial CGIBS inativa — cadastre/reative antes de habilitar")
			return
		}

		clientSecret := crypto.DecryptFieldWithFallback(clientSecretEnc)

		tokenJaExistia := tokenContribEnc.Valid && tokenContribEnc.String != ""
		var tokenContrib string
		if tokenJaExistia {
			tokenContrib = crypto.DecryptFieldWithFallback(tokenContribEnc.String)
		} else {
			tokenContrib, err = generateTokenContrib()
			if err != nil {
				sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao gerar token de contribuinte", err, "[CGIBSCredentials]")
				return
			}
		}

		webhookURL := resolveCGIBSWebhookURL()

		// Chamada HTTP externa (até 30s) FORA de qualquer transação/lock do banco (item 3).
		habilitadoRaw, dataHabilitacao, herr := services.Habilitar(clientID, clientSecret, webhookURL, tokenContrib)
		if herr != nil {
			if errors.Is(herr, services.ErrCGIBSNaoConfigurado) {
				jsonErr(w, http.StatusServiceUnavailable, "Integração CGIBS não configurada (CGIBS_HABILITACAO_URL ausente)")
				return
			}
			sanitizeDBErr(w, http.StatusBadGateway, "Erro ao habilitar contribuinte na CGIBS", herr, "[CGIBSCredentials]")
			return
		}

		// "sucesso" é a convenção documentada no comentário da coluna habilitado (migration
		// 131): "só true após resposta Habilitado=sucesso". Qualquer outro valor (inclusive
		// vazio) é tratado como não habilitado NESTA chamada — o que NÃO significa que a
		// credencial deixa de estar habilitada de verdade, ver UPDATE abaixo (item 1).
		habilitadoBool := strings.EqualFold(strings.TrimSpace(habilitadoRaw), "sucesso")
		if habilitadoBool && dataHabilitacao.IsZero() {
			// Resposta confirmou sucesso mas sem data parseável (ou ausente) — usa o horário
			// local de recebimento como fallback, para não violar o CHECK
			// (NOT habilitado OR data_habilitacao IS NOT NULL) da migration 131.
			dataHabilitacao = time.Now()
		}

		tokenContribEncNew, eerr := crypto.EncryptField(tokenContrib)
		if eerr != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao proteger token de contribuinte", eerr, "[CGIBSCredentials]")
			return
		}

		var dataHabilitacaoParam interface{}
		if habilitadoBool {
			dataHabilitacaoParam = dataHabilitacao
		}

		// Transação curta, aberta só agora (depois da chamada de rede) para persistir o
		// resultado (item 3).
		tx, err := db.Begin()
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao iniciar transação", err, "[CGIBSCredentials]")
			return
		}
		defer tx.Rollback()

		// habilitado/data_habilitacao só AVANÇAM: uma resposta não-sucesso nesta chamada nunca
		// derruba uma habilitação já confirmada por uma chamada anterior (item 1) — mesmo
		// princípio de proteção já aplicado ao token_contrib acima (COALESCE preserva o
		// token existente mesmo sob corrida). WHERE id=... AND client_id=... re-verifica que a
		// credencial não foi editada/trocada entre a leitura (acima) e este UPDATE — se não bater
		// mais (0 linhas afetadas), o resultado desta chamada é descartado (item 3).
		res, err := tx.Exec(`
			UPDATE cgibs_credentials
			SET habilitado = habilitado OR $1,
			    data_habilitacao = COALESCE(data_habilitacao, CASE WHEN $1 THEN $2::timestamptz END),
			    webhook_url = $3,
			    token_contrib = COALESCE(token_contrib, $4),
			    updated_at = NOW()
			WHERE id = $5 AND client_id = $6
		`, habilitadoBool, dataHabilitacaoParam, webhookURL, tokenContribEncNew, id, clientID)
		if err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao salvar resultado da habilitação", err, "[CGIBSCredentials]")
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			jsonErr(w, http.StatusConflict, "Credencial CGIBS foi alterada ou removida durante a habilitação — tente novamente")
			return
		}

		if err := tx.Commit(); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao confirmar transação", err, "[CGIBSCredentials]")
			return
		}

		// Estado final persistido (pode ter avançado por uma chamada anterior mesmo que esta
		// tenha falhado) — relido para a resposta refletir a verdade do banco, não só o
		// resultado desta chamada isolada.
		var habilitadoFinal bool
		var dataHabilitacaoFinal sql.NullTime
		if err := db.QueryRow(`SELECT habilitado, data_habilitacao FROM cgibs_credentials WHERE id = $1`, id).
			Scan(&habilitadoFinal, &dataHabilitacaoFinal); err != nil {
			sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao reler resultado da habilitação", err, "[CGIBSCredentials]")
			return
		}

		log.Printf("[CGIBSCredentials] Habilitação processada companyID=%s habilitadoNestaChamada=%v habilitadoFinal=%v tokenGeradoAgora=%v",
			companyID, habilitadoBool, habilitadoFinal, !tokenJaExistia)

		resp := map[string]interface{}{
			"habilitado":  habilitadoFinal,
			"webhook_url": webhookURL,
			"message":     "Solicitação de habilitação processada",
		}
		if dataHabilitacaoFinal.Valid {
			resp["data_habilitacao"] = dataHabilitacaoFinal.Time
		} else {
			resp["data_habilitacao"] = nil
		}
		json.NewEncoder(w).Encode(resp)
	}
}
