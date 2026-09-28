package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setupCGIBSWebhookTest cria uma company + cgibs_credentials ativa E habilitada (cnpj_matriz
// completo de 14 dígitos, token_contrib conhecido em texto puro para o teste montar o header)
// e devolve companyID/cnpjMatriz/token prontos para montar o payload do webhook. habilitado=true
// é necessário desde o item 9 da revisão adversarial (matchCGIBSCredential exige habilitado=true,
// não só ativo=true).
func setupCGIBSWebhookTest(t *testing.T, db *sql.DB, cnpjMatriz, tokenContrib string) (companyID string, cleanup func()) {
	t.Helper()
	companyID, cleanupCompany := setupTestCompany(t, db)
	insertCGIBSCred(t, db, companyID, cnpjMatriz, tokenContrib, true, true)
	cleanup = func() {
		db.Exec(`DELETE FROM cgibs_credentials WHERE company_id = $1`, companyID)
		cleanupCompany()
	}
	return companyID, cleanup
}

func postCGIBSWebhook(db *sql.DB, token string, payload map[string]interface{}) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/cgibs/webhook", bytes.NewReader(body))
	if token != "" {
		req.Header.Set("X-CGIBS-Token", token)
	}
	rec := httptest.NewRecorder()
	CGIBSWebhookHandler(db)(rec, req)
	return rec
}

func countCGIBSSolicitacoes(t *testing.T, db *sql.DB, companyID string, idSolicitacaoExterno int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM cgibs_solicitacoes WHERE company_id = $1 AND id_solicitacao_externo = $2
	`, companyID, idSolicitacaoExterno).Scan(&n); err != nil {
		t.Fatalf("falha ao contar cgibs_solicitacoes: %v", err)
	}
	return n
}

func countCGIBSArquivos(t *testing.T, db *sql.DB, companyID string, idSolicitacaoExterno int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM cgibs_arquivos a
		JOIN cgibs_solicitacoes s ON s.id = a.solicitacao_id
		WHERE a.company_id = $1 AND s.id_solicitacao_externo = $2
	`, companyID, idSolicitacaoExterno).Scan(&n); err != nil {
		t.Fatalf("falha ao contar cgibs_arquivos: %v", err)
	}
	return n
}

func webhookPayload(cnpj string, idSolicitacao int64, situacao string, arquivos []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"TipoSolicitacao":         "diferencial",
		"SituacaoSolicitacao":     situacao,
		"CNPJ":                    cnpj,
		"IDSolicitacao":           idSolicitacao,
		"DataSolicitacao":         "2026-09-28T08:00:00Z",
		"DataTransacaoIni":        "2026-09-01",
		"DataTransacaoFim":        "2026-09-30",
		"QtdOperacoes":            10,
		"QtdArqVinculados":        len(arquivos),
		"DataValidadeSolicitacao": "2026-10-28",
		"Arquivos":                arquivos,
	}
}

func TestCGIBSWebhook_TokenECNPJCorretos_FazUpsert(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const token = "token-webhook-teste-1"
	companyID, cleanup := setupCGIBSWebhookTest(t, db, "98765432000188", token)
	defer cleanup()

	payload := webhookPayload("98765432000188", 5001, "gerada", []map[string]interface{}{
		{"NumeroSequencial": 1, "NomeArquivo": "extrato_cc_001.json"},
		{"NumeroSequencial": 2, "NomeArquivo": "extrato_cc_002.json"},
	})

	rec := postCGIBSWebhook(db, token, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}

	if got := countCGIBSSolicitacoes(t, db, companyID, 5001); got != 1 {
		t.Fatalf("esperava 1 cgibs_solicitacoes, obteve %d", got)
	}
	if got := countCGIBSArquivos(t, db, companyID, 5001); got != 2 {
		t.Fatalf("esperava 2 cgibs_arquivos, obteve %d", got)
	}

	var situacao string
	if err := db.QueryRow(`SELECT situacao_solicitacao FROM cgibs_solicitacoes WHERE company_id=$1 AND id_solicitacao_externo=5001`, companyID).Scan(&situacao); err != nil {
		t.Fatalf("erro ao ler situacao: %v", err)
	}
	if situacao != "gerada" {
		t.Errorf("esperava situacao_solicitacao=gerada, veio %q", situacao)
	}
	rows, err := db.Query(`
		SELECT status FROM cgibs_arquivos a JOIN cgibs_solicitacoes s ON s.id=a.solicitacao_id
		WHERE s.company_id=$1 AND s.id_solicitacao_externo=5001 ORDER BY numero_sequencial
	`, companyID)
	if err != nil {
		t.Fatalf("erro ao ler status dos arquivos: %v", err)
	}
	defer rows.Close()
	var statuses []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		statuses = append(statuses, s)
	}
	if len(statuses) != 2 || statuses[0] != "pendente" || statuses[1] != "pendente" {
		t.Errorf("esperava 2 arquivos com status=pendente, veio %v", statuses)
	}
}

func TestCGIBSWebhook_ReenviadoMesmoIDSolicitacao_AtualizaNaoDuplica(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const token = "token-webhook-teste-2"
	companyID, cleanup := setupCGIBSWebhookTest(t, db, "98765432000188", token)
	defer cleanup()

	payload1 := webhookPayload("98765432000188", 5002, "solicitada", []map[string]interface{}{
		{"NumeroSequencial": 1, "NomeArquivo": "extrato_cc_001.json"},
	})
	rec1 := postCGIBSWebhook(db, token, payload1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("1º envio: esperava 200, obteve %d: %s", rec1.Code, rec1.Body.String())
	}

	// Reenvio: situação avançou e um 2º arquivo foi vinculado — mesma IDSolicitacao.
	payload2 := webhookPayload("98765432000188", 5002, "enviada", []map[string]interface{}{
		{"NumeroSequencial": 1, "NomeArquivo": "extrato_cc_001.json"},
		{"NumeroSequencial": 2, "NomeArquivo": "extrato_cc_002.json"},
	})
	rec2 := postCGIBSWebhook(db, token, payload2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("2º envio: esperava 200, obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	if got := countCGIBSSolicitacoes(t, db, companyID, 5002); got != 1 {
		t.Fatalf("esperava exatamente 1 cgibs_solicitacoes após reenvio (upsert), obteve %d", got)
	}
	if got := countCGIBSArquivos(t, db, companyID, 5002); got != 2 {
		t.Fatalf("esperava 2 cgibs_arquivos após reenvio (1 novo + 1 upsert), obteve %d", got)
	}

	var situacao string
	if err := db.QueryRow(`SELECT situacao_solicitacao FROM cgibs_solicitacoes WHERE company_id=$1 AND id_solicitacao_externo=5002`, companyID).Scan(&situacao); err != nil {
		t.Fatalf("erro ao ler situacao: %v", err)
	}
	if situacao != "enviada" {
		t.Errorf("esperava situacao_solicitacao atualizada para 'enviada', veio %q", situacao)
	}
}

func TestCGIBSWebhook_TokenErrado_RejeitaNaoGrava(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const token = "token-webhook-correto"
	companyID, cleanup := setupCGIBSWebhookTest(t, db, "98765432000188", token)
	defer cleanup()

	payload := webhookPayload("98765432000188", 5003, "gerada", []map[string]interface{}{
		{"NumeroSequencial": 1, "NomeArquivo": "extrato_cc_001.json"},
	})

	rec := postCGIBSWebhook(db, "token-completamente-errado", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 genérico (não vazar detalhe), obteve %d: %s", rec.Code, rec.Body.String())
	}
	if got := countCGIBSSolicitacoes(t, db, companyID, 5003); got != 0 {
		t.Fatalf("token errado: esperava 0 linhas gravadas, obteve %d", got)
	}
}

func TestCGIBSWebhook_CNPJNaoBate_RejeitaNaoGrava(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const token = "token-webhook-teste-3"
	companyID, cleanup := setupCGIBSWebhookTest(t, db, "98765432000188", token)
	defer cleanup()

	// CNPJ raiz diferente do cadastrado (98765432), mesmo token.
	payload := webhookPayload("11122233000144", 5004, "gerada", nil)

	rec := postCGIBSWebhook(db, token, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 genérico, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if got := countCGIBSSolicitacoes(t, db, companyID, 5004); got != 0 {
		t.Fatalf("CNPJ não bate: esperava 0 linhas gravadas, obteve %d", got)
	}
}

func TestCGIBSWebhook_SemToken_RejeitaNaoGrava(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const token = "token-webhook-teste-4"
	companyID, cleanup := setupCGIBSWebhookTest(t, db, "98765432000188", token)
	defer cleanup()

	payload := webhookPayload("98765432000188", 5005, "gerada", nil)
	rec := postCGIBSWebhook(db, "", payload) // sem header X-CGIBS-Token
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 genérico, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if got := countCGIBSSolicitacoes(t, db, companyID, 5005); got != 0 {
		t.Fatalf("sem token: esperava 0 linhas gravadas, obteve %d", got)
	}
}

// TestCGIBSWebhook_CredencialInativa_RejeitaNaoGrava confirma que uma credencial com
// ativo=false não valida o webhook, mesmo com CNPJ+token corretos e habilitado=true —
// suposição documentada em matchCGIBSCredential (cgibs_webhook.go).
func TestCGIBSWebhook_CredencialInativa_RejeitaNaoGrava(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanupCompany := setupTestCompany(t, db)
	defer func() {
		db.Exec(`DELETE FROM cgibs_credentials WHERE company_id = $1`, companyID)
		cleanupCompany()
	}()
	const token = "token-webhook-inativo"
	insertCGIBSCred(t, db, companyID, "98765432000188", token, false, true)

	payload := webhookPayload("98765432000188", 5006, "gerada", nil)
	rec := postCGIBSWebhook(db, token, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 genérico, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if got := countCGIBSSolicitacoes(t, db, companyID, 5006); got != 0 {
		t.Fatalf("credencial inativa: esperava 0 linhas gravadas, obteve %d", got)
	}
}

// TestCGIBSWebhook_CredencialNaoHabilitada_RejeitaNaoGrava confirma o item 9 da revisão
// adversarial: uma credencial ativa mas NUNCA confirmada pela CGIBS (habilitado=false) não
// autentica o webhook, mesmo com CNPJ+token corretos — só ativo=true não bastava antes desta
// correção.
func TestCGIBSWebhook_CredencialNaoHabilitada_RejeitaNaoGrava(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanupCompany := setupTestCompany(t, db)
	defer func() {
		db.Exec(`DELETE FROM cgibs_credentials WHERE company_id = $1`, companyID)
		cleanupCompany()
	}()
	const token = "token-webhook-nao-habilitado"
	insertCGIBSCred(t, db, companyID, "98765432000188", token, true, false)

	payload := webhookPayload("98765432000188", 5007, "gerada", nil)
	rec := postCGIBSWebhook(db, token, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 genérico, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if got := countCGIBSSolicitacoes(t, db, companyID, 5007); got != 0 {
		t.Fatalf("credencial não habilitada: esperava 0 linhas gravadas, obteve %d", got)
	}
}

// TestCGIBSWebhook_QtdNegativaEDataInvertida_NaoAbortaGravaSanitizado cobre o item 7: um
// payload com QtdOperacoes/QtdArqVinculados negativos e DataTransacaoIni > DataTransacaoFim
// violaria os CHECKs de cgibs_solicitacoes (migration 131) e abortaria a transação inteira —
// os campos devem ser sanitizados (zerados) em vez de derrubar a gravação da solicitação.
func TestCGIBSWebhook_QtdNegativaEDataInvertida_NaoAbortaGravaSanitizado(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const token = "token-webhook-teste-7"
	companyID, cleanup := setupCGIBSWebhookTest(t, db, "98765432000188", token)
	defer cleanup()

	payload := webhookPayload("98765432000188", 5008, "gerada", []map[string]interface{}{
		{"NumeroSequencial": 1, "NomeArquivo": "extrato_cc_001.json"},
	})
	payload["QtdOperacoes"] = -5
	payload["QtdArqVinculados"] = -1
	payload["DataTransacaoIni"] = "2026-09-30"
	payload["DataTransacaoFim"] = "2026-09-01"

	rec := postCGIBSWebhook(db, token, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 (payload sanitizado, não rejeitado), obteve %d: %s", rec.Code, rec.Body.String())
	}
	if got := countCGIBSSolicitacoes(t, db, companyID, 5008); got != 1 {
		t.Fatalf("esperava 1 cgibs_solicitacoes gravada mesmo com payload inválido, obteve %d", got)
	}

	var qtdOp, qtdArq int
	var iniValid, fimValid bool
	if err := db.QueryRow(`
		SELECT qtd_operacoes, qtd_arq_vinculados, data_transacao_ini IS NOT NULL, data_transacao_fim IS NOT NULL
		FROM cgibs_solicitacoes WHERE company_id=$1 AND id_solicitacao_externo=5008
	`, companyID).Scan(&qtdOp, &qtdArq, &iniValid, &fimValid); err != nil {
		t.Fatalf("erro ao ler solicitacao gravada: %v", err)
	}
	if qtdOp != 0 {
		t.Errorf("esperava qtd_operacoes sanitizado para 0, veio %d", qtdOp)
	}
	if qtdArq != 0 {
		t.Errorf("esperava qtd_arq_vinculados sanitizado para 0, veio %d", qtdArq)
	}
	if iniValid || fimValid {
		t.Errorf("esperava data_transacao_ini/fim zeradas (invertidas no payload), veio ini_preenchida=%v fim_preenchida=%v", iniValid, fimValid)
	}
}

// TestCGIBSWebhook_ReenvioComSituacaoNaoReconhecida_NaoRegride cobre o item 8: um reenvio cuja
// SituacaoSolicitacao não é reconhecida não pode regredir o valor já salvo por um envio
// anterior válido para o default da coluna.
func TestCGIBSWebhook_ReenvioComSituacaoNaoReconhecida_NaoRegride(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const token = "token-webhook-teste-8"
	companyID, cleanup := setupCGIBSWebhookTest(t, db, "98765432000188", token)
	defer cleanup()

	payload1 := webhookPayload("98765432000188", 5009, "enviada", nil)
	rec1 := postCGIBSWebhook(db, token, payload1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("1º envio: esperava 200, obteve %d: %s", rec1.Code, rec1.Body.String())
	}

	payload2 := webhookPayload("98765432000188", 5009, "situacao-desconhecida-xyz", nil)
	rec2 := postCGIBSWebhook(db, token, payload2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("2º envio: esperava 200, obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	var situacao string
	if err := db.QueryRow(`SELECT situacao_solicitacao FROM cgibs_solicitacoes WHERE company_id=$1 AND id_solicitacao_externo=5009`, companyID).Scan(&situacao); err != nil {
		t.Fatalf("erro ao ler situacao: %v", err)
	}
	if situacao != "enviada" {
		t.Errorf("reenvio com situação não reconhecida regrediu o valor salvo — esperava manter 'enviada', veio %q", situacao)
	}
}

// TestCGIBSWebhook_EmpresaNaoEncontrada_DescartaSemErro cobre o item 10: company_id vindo de
// uma linha órfã de cgibs_credentials (sem FK garantida para companies — migration 102) não
// pode fazer o INSERT falhar com erro de FK confuso; o webhook é descartado com 200.
func TestCGIBSWebhook_EmpresaNaoEncontrada_DescartaSemErro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const token = "token-webhook-teste-10"
	companyID, cleanupCompany := setupTestCompany(t, db)
	insertCGIBSCred(t, db, companyID, "98765432000188", token, true, true)
	defer db.Exec(`DELETE FROM cgibs_credentials WHERE company_id = $1`, companyID)

	// Remove a empresa SEM remover a credencial — cgibs_credentials.company_id é TEXT sem FK
	// (migration 102), então a linha fica órfã, exatamente o cenário do item 10.
	cleanupCompany()

	payload := webhookPayload("98765432000188", 5010, "gerada", nil)
	rec := postCGIBSWebhook(db, token, payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200 (descarte silencioso), obteve %d: %s", rec.Code, rec.Body.String())
	}
	if got := countCGIBSSolicitacoes(t, db, companyID, 5010); got != 0 {
		t.Fatalf("empresa inexistente: esperava 0 linhas gravadas, obteve %d", got)
	}
}
