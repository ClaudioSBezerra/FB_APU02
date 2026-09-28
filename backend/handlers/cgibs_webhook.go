package handlers

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"fb_apu02/crypto"
)

// errCGIBSCompanyNotFound sinaliza que company_id (resolvido a partir de uma linha de
// cgibs_credentials — que não tem FK garantida para companies, schema pré-existente, ver
// migration 102) não existe mais em companies. O chamador trata isso como descarte silencioso
// do webhook (200, nada gravado), não como erro de persistência (item 10 da revisão
// adversarial).
var errCGIBSCompanyNotFound = errors.New("empresa não encontrada")

// cgibsWebhookMaxBodyBytes limita o corpo do webhook (endpoint público) — mesmo padrão de
// webhookMaxBodyBytes em rfb_apuracao.go.
const cgibsWebhookMaxBodyBytes = 1 << 20

// CGIBSWebhookArquivo representa um item da lista Arquivos[] do payload de notificação
// (MOC 5.2). Os nomes exatos dos campos JSON (NumeroSequencial/NomeArquivo) SÃO UMA
// SUPOSIÇÃO — a seção correspondente do MOC não extraiu como texto (páginas de
// diagrama/imagem, ver spec-cgibs-conta-corrente-plano.md); ajustar quando tivermos acesso
// real ao payload da CGIBS ou uma cópia legível do PDF.
type CGIBSWebhookArquivo struct {
	NumeroSequencial int64  `json:"NumeroSequencial"`
	NomeArquivo      string `json:"NomeArquivo"`
}

// CGIBSWebhookPayload é o payload de notificação de arquivo disponível (MOC 5.2), conforme
// listado no Code Map da spec (nomes de campo tomados literalmente da spec, exceto Arquivos[]
// — ver CGIBSWebhookArquivo).
type CGIBSWebhookPayload struct {
	TipoSolicitacao         string                `json:"TipoSolicitacao"`
	SituacaoSolicitacao     string                `json:"SituacaoSolicitacao"`
	CNPJ                    string                `json:"CNPJ"`
	IDSolicitacao           int64                 `json:"IDSolicitacao"`
	DataSolicitacao         string                `json:"DataSolicitacao"`
	DataTransacaoIni        string                `json:"DataTransacaoIni"`
	DataTransacaoFim        string                `json:"DataTransacaoFim"`
	QtdOperacoes            int                   `json:"QtdOperacoes"`
	QtdArqVinculados        int                   `json:"QtdArqVinculados"`
	DataValidadeSolicitacao string                `json:"DataValidadeSolicitacao"`
	Arquivos                []CGIBSWebhookArquivo `json:"Arquivos"`
}

// cgibsTimestampLayouts/cgibsDateLayouts: mesma limitação de formato documentada em
// services/cgibs.go (parseCGIBSDataHabilitacao) — a CGIBS não confirmou o formato exato de
// data/hora dos campos do webhook. Melhor esforço: campo não reconhecido vira NULL na
// gravação em vez de rejeitar o webhook inteiro.
var cgibsTimestampLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"}
var cgibsDateLayouts = []string{"2006-01-02", "02/01/2006"}

func parseCGIBSTimestamp(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range cgibsTimestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseCGIBSDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range cgibsDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// extractCNPJBase extrai os 8 dígitos (raiz) de um CNPJ recebido no webhook, tolerando
// pontuação e um CNPJ já enviado só com 8 dígitos. Devolve "" se não houver dígitos
// suficientes.
func extractCNPJBase(cnpj string) string {
	var digits strings.Builder
	for _, r := range cnpj {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	s := digits.String()
	if len(s) < 8 {
		return ""
	}
	return s[:8]
}

// normalizeTipoSolicitacao normaliza para o CHECK de cgibs_solicitacoes.tipo_solicitacao
// ('manual','diferencial'). Devolve também `recognized`: false quando o valor recebido não é
// reconhecido — nesse caso o chamador usa o valor devolvido (default 'diferencial') apenas
// para uma 1ª gravação (INSERT), mas NÃO o usa para sobrescrever um valor já salvo num reenvio
// (item 8 da revisão adversarial — evita regredir situacao/tipo já corretos por causa de um
// payload que não conseguimos interpretar). Aviso logado, webhook nunca é rejeitado por isso.
func normalizeTipoSolicitacao(s string) (value string, recognized bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "manual", "diferencial":
		return v, true
	default:
		if v != "" {
			log.Printf("[CGIBS Webhook] AVISO: TipoSolicitacao %q não reconhecida — default 'diferencial' só numa 1ª gravação, valor já salvo (se houver) é preservado", s)
		}
		return "diferencial", false
	}
}

// normalizeSituacaoSolicitacao normaliza para o CHECK de
// cgibs_solicitacoes.situacao_solicitacao ('solicitada','gerada','enviada','cancelada',
// 'expirada'). Mesma semântica de `recognized` documentada em normalizeTipoSolicitacao.
func normalizeSituacaoSolicitacao(s string) (value string, recognized bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "solicitada", "gerada", "enviada", "cancelada", "expirada":
		return v, true
	default:
		if v != "" {
			log.Printf("[CGIBS Webhook] AVISO: SituacaoSolicitacao %q não reconhecida — default 'solicitada' só numa 1ª gravação, valor já salvo (se houver) é preservado", s)
		}
		return "solicitada", false
	}
}

// matchCGIBSCredential identifica a empresa dona do webhook: cnpj_matriz (8 primeiros
// dígitos) batendo com cnpjBase E token_contrib (decriptado) batendo em tempo constante com
// o token recebido. Só considera credenciais ativas E JÁ HABILITADAS (habilitado=true —
// item 9 da revisão adversarial: uma credencial nunca confirmada pela CGIBS, ou com um token
// de uma tentativa que falhou, não deveria conseguir autenticar webhook) com token_contrib
// configurado. Sem match, devolve erro (o chamador rejeita com 200 genérico, sem gravar nada e
// sem vazar detalhe — mesmo padrão de segurança do webhook RFB).
func matchCGIBSCredential(db *sql.DB, cnpjBase, token string) (companyID string, err error) {
	rows, err := db.Query(`
		SELECT company_id, token_contrib
		FROM cgibs_credentials
		WHERE ativo = true AND habilitado = true AND token_contrib IS NOT NULL AND token_contrib <> ''
		  AND left(cnpj_matriz, 8) = $1
	`, cnpjBase)
	if err != nil {
		return "", fmt.Errorf("consultar cgibs_credentials: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid, tokenEnc string
		if scanErr := rows.Scan(&cid, &tokenEnc); scanErr != nil {
			// Item 13: não descartar silenciosamente — registra antes de seguir para a próxima
			// linha (uma falha de Scan aqui não deveria acontecer em operação normal).
			log.Printf("[CGIBS Webhook] AVISO: erro ao ler linha de cgibs_credentials durante match (cnpjBase=%s): %v", cnpjBase, scanErr)
			continue
		}
		storedToken := crypto.DecryptFieldWithFallback(tokenEnc)
		if subtle.ConstantTimeCompare([]byte(storedToken), []byte(token)) == 1 {
			return cid, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterar cgibs_credentials: %w", err)
	}
	return "", fmt.Errorf("nenhuma credencial casou CNPJ+token")
}

// upsertCGIBSSolicitacao grava a solicitação (upsert por company_id+id_solicitacao_externo)
// e cada item de Arquivos[] (upsert por solicitacao_id+numero_sequencial), status 'pendente'
// para arquivos novos. NÃO dispara download — isso é a sub-spec seguinte
// (spec-cgibs-obter-arquivo-parser.md).
func upsertCGIBSSolicitacao(db *sql.DB, companyID, cnpjBase string, payload CGIBSWebhookPayload) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Item 10: company_id vem de uma linha de cgibs_credentials sem FK garantida para
	// companies (schema pré-existente, migration 102 — fora de escopo corrigir aqui). Se a
	// empresa não existir mais, o INSERT abaixo falharia com um erro de FK confuso — verificamos
	// antes e descartamos o webhook silenciosamente (o chamador loga e responde 200).
	var companyExists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM companies WHERE id = $1)`, companyID).Scan(&companyExists); err != nil {
		return fmt.Errorf("verificar existência da empresa: %w", err)
	}
	if !companyExists {
		return errCGIBSCompanyNotFound
	}

	// tipoSolicitacao/situacao: valor a gravar numa 1ª INSERT (default seguro se não
	// reconhecido) — tipoRecognized/situacaoRecognized controlam se o UPDATE (reenvio) pode
	// sobrescrever um valor já salvo (item 8: nunca regride pra um default por causa de um
	// payload não reconhecido).
	tipoSolicitacao, tipoRecognized := normalizeTipoSolicitacao(payload.TipoSolicitacao)
	situacao, situacaoRecognized := normalizeSituacaoSolicitacao(payload.SituacaoSolicitacao)
	var tipoUpdateParam, situacaoUpdateParam interface{}
	if tipoRecognized {
		tipoUpdateParam = tipoSolicitacao
	}
	if situacaoRecognized {
		situacaoUpdateParam = situacao
	}

	var dataSolicitacao, dataTransacaoIni, dataTransacaoFim, dataValidade interface{}
	if t, ok := parseCGIBSTimestamp(payload.DataSolicitacao); ok {
		dataSolicitacao = t
	}
	iniT, iniOK := parseCGIBSDate(payload.DataTransacaoIni)
	fimT, fimOK := parseCGIBSDate(payload.DataTransacaoFim)
	if iniOK && fimOK && iniT.After(fimT) {
		// Item 7: DataTransacaoIni > DataTransacaoFim violaria o CHECK de cgibs_solicitacoes
		// (data_transacao_ini <= data_transacao_fim) e abortaria a transação inteira — zera os
		// dois em vez de rejeitar o webhook.
		log.Printf("[CGIBS Webhook] AVISO: DataTransacaoIni (%s) > DataTransacaoFim (%s) — ambos zerados (IDSolicitacao=%d)",
			payload.DataTransacaoIni, payload.DataTransacaoFim, payload.IDSolicitacao)
		iniOK, fimOK = false, false
	}
	if iniOK {
		dataTransacaoIni = iniT
	}
	if fimOK {
		dataTransacaoFim = fimT
	}
	if d, ok := parseCGIBSDate(payload.DataValidadeSolicitacao); ok {
		dataValidade = d
	}

	// Item 7: QtdOperacoes/QtdArqVinculados negativos violariam o CHECK (>= 0) — zera em vez de
	// rejeitar o webhook inteiro.
	qtdOperacoes := payload.QtdOperacoes
	if qtdOperacoes < 0 {
		log.Printf("[CGIBS Webhook] AVISO: QtdOperacoes negativo (%d) — zerado (IDSolicitacao=%d)", qtdOperacoes, payload.IDSolicitacao)
		qtdOperacoes = 0
	}
	qtdArqVinculados := payload.QtdArqVinculados
	if qtdArqVinculados < 0 {
		log.Printf("[CGIBS Webhook] AVISO: QtdArqVinculados negativo (%d) — zerado (IDSolicitacao=%d)", qtdArqVinculados, payload.IDSolicitacao)
		qtdArqVinculados = 0
	}

	var solicitacaoID string
	err = tx.QueryRow(`
		INSERT INTO cgibs_solicitacoes (
			company_id, cnpj_base, id_solicitacao_externo, tipo_solicitacao, situacao_solicitacao,
			data_solicitacao, data_transacao_ini, data_transacao_fim,
			qtd_operacoes, qtd_arq_vinculados, data_validade_solicitacao, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (company_id, id_solicitacao_externo) WHERE id_solicitacao_externo IS NOT NULL DO UPDATE SET
			tipo_solicitacao = COALESCE($12, cgibs_solicitacoes.tipo_solicitacao),
			situacao_solicitacao = COALESCE($13, cgibs_solicitacoes.situacao_solicitacao),
			data_solicitacao = EXCLUDED.data_solicitacao,
			data_transacao_ini = EXCLUDED.data_transacao_ini,
			data_transacao_fim = EXCLUDED.data_transacao_fim,
			qtd_operacoes = EXCLUDED.qtd_operacoes,
			qtd_arq_vinculados = EXCLUDED.qtd_arq_vinculados,
			data_validade_solicitacao = EXCLUDED.data_validade_solicitacao,
			updated_at = NOW()
		RETURNING id
	`, companyID, cnpjBase, payload.IDSolicitacao, tipoSolicitacao, situacao,
		dataSolicitacao, dataTransacaoIni, dataTransacaoFim,
		qtdOperacoes, qtdArqVinculados, dataValidade,
		tipoUpdateParam, situacaoUpdateParam,
	).Scan(&solicitacaoID)
	if err != nil {
		return fmt.Errorf("upsert cgibs_solicitacoes: %w", err)
	}

	for _, arq := range payload.Arquivos {
		if arq.NumeroSequencial <= 0 {
			log.Printf("[CGIBS Webhook] AVISO: item de Arquivos[] com NumeroSequencial inválido (%d) ignorado (IDSolicitacao=%d)",
				arq.NumeroSequencial, payload.IDSolicitacao)
			continue
		}
		// status sempre 'pendente' num INSERT novo; num conflito (reenvio do mesmo webhook)
		// só atualiza o nome — nunca sobrescreve um status já avançado (ex.: 'baixado') que a
		// próxima sub-spec (obter-arquivo/parser) venha a gravar.
		_, err = tx.Exec(`
			INSERT INTO cgibs_arquivos (company_id, solicitacao_id, numero_sequencial, nome_arquivo, status)
			VALUES ($1, $2, $3, $4, 'pendente')
			ON CONFLICT (solicitacao_id, numero_sequencial) DO UPDATE SET
				nome_arquivo = EXCLUDED.nome_arquivo
		`, companyID, solicitacaoID, arq.NumeroSequencial, arq.NomeArquivo)
		if err != nil {
			return fmt.Errorf("upsert cgibs_arquivos (seq %d): %w", arq.NumeroSequencial, err)
		}
	}

	return tx.Commit()
}

// CGIBSWebhookHandler recebe notificações de arquivo disponível da CGIBS (PUBLIC — sem JWT,
// mesmo padrão de RFBWebhookHandler em rfb_apuracao.go).
//
// Autenticação: header X-CGIBS-Token com o TokenContrib da credencial. SUPOSIÇÃO — a doc
// extraída do MOC não deixa claro se o token vem em header ou em campo do próprio payload;
// escolhido por analogia ao X-RFB-Signature do webhook RFB (verifyWebhookSignature). Ajustar
// aqui é um ponto único de mudança se a CGIBS confirmar outro mecanismo.
//
// Identificação da empresa: CNPJ (8 dígitos/raiz) batendo com cgibs_credentials.cnpj_matriz
// E o token batendo com cgibs_credentials.token_contrib daquela linha — sem os dois,
// rejeita com 200 genérico (evita retry indefinido da CGIBS) + log de aviso, sem vazar
// detalhe e SEM gravar nada.
//
// Esta sub-spec só persiste (upsert cgibs_solicitacoes + cgibs_arquivos, status 'pendente')
// — não dispara nenhum download (fica para spec-cgibs-obter-arquivo-parser.md).
func CGIBSWebhookHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, cgibsWebhookMaxBodyBytes))
		if err != nil {
			log.Printf("[CGIBS Webhook] Erro ao ler corpo: %v", err)
			http.Error(w, "Error reading body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var payload CGIBSWebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("[CGIBS Webhook] Erro ao parsear JSON: %v (body: %d bytes)", err, len(body))
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "invalid JSON"})
			return
		}

		token := r.Header.Get("X-CGIBS-Token")
		cnpjBase := extractCNPJBase(payload.CNPJ)

		if token == "" || cnpjBase == "" {
			log.Printf("[CGIBS Webhook] Requisição rejeitada (sem token ou CNPJ válido) de %s", GetClientIP(r))
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received"})
			return
		}

		if payload.IDSolicitacao <= 0 {
			log.Printf("[CGIBS Webhook] Requisição rejeitada (IDSolicitacao ausente/inválido) de %s cnpjBase=%s", GetClientIP(r), cnpjBase)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "missing IDSolicitacao"})
			return
		}

		companyID, err := matchCGIBSCredential(db, cnpjBase, token)
		if err != nil {
			log.Printf("[CGIBS Webhook] AVISO: nenhuma credencial casou CNPJ+token (de %s, cnpjBase=%s, IDSolicitacao=%d) — rejeitado, nada gravado",
				GetClientIP(r), cnpjBase, payload.IDSolicitacao)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received"})
			return
		}

		if err := upsertCGIBSSolicitacao(db, companyID, cnpjBase, payload); err != nil {
			if errors.Is(err, errCGIBSCompanyNotFound) {
				// Item 10: empresa não encontrada — descarte claro e silencioso, sem tentar o
				// INSERT (que falharia com erro de FK confuso).
				log.Printf("[CGIBS Webhook] AVISO: empresa não encontrada (companyID=%s, IDSolicitacao=%d) — webhook descartado", companyID, payload.IDSolicitacao)
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "received"})
				return
			}
			log.Printf("[CGIBS Webhook] Erro ao gravar solicitação (companyID=%s, IDSolicitacao=%d): %v", companyID, payload.IDSolicitacao, err)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "received", "warning": "erro ao persistir"})
			return
		}

		log.Printf("[CGIBS Webhook] Solicitação %d (companyID=%s) processada: situacao=%s tipo=%s qtdArquivos=%d",
			payload.IDSolicitacao, companyID, payload.SituacaoSolicitacao, payload.TipoSolicitacao, len(payload.Arquivos))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "received"})
	}
}
