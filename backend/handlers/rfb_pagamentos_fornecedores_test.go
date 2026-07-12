package handlers

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// openTestDB abre conexão com o Postgres de teste — pula o teste (não falha)
// se o banco não estiver disponível localmente. Réplica do padrão já usado em
// backend/services/sap_sync_trigger_test.go (helpers de teste não são
// compartilhados entre pacotes Go).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/fiscal_db?sslmode=disable"
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skipf("DB indisponível para teste de integração: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("DB indisponível para teste de integração: %v", err)
	}
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'pagamentos_fornecedores')`).Scan(&exists); err != nil || !exists {
		t.Skip("tabela pagamentos_fornecedores não existe — rode as migrations antes deste teste")
	}
	return db
}

// setupTestCompany cria environment/enterprise_group/company de teste e retorna
// uma função de cleanup. Diferente do equivalente em services/sap_sync_trigger_test.go,
// este cleanup também apaga pagamentos_fornecedores/nfe_entradas/cte_entradas
// da empresa de teste ANTES de apagar o environment — essas tabelas não têm
// ON DELETE CASCADE em company_id (migration 110/059/¿?), então o DELETE do
// environment falharia silenciosamente (erro descartado) se essas linhas
// continuassem existindo.
func setupTestCompany(t *testing.T, db *sql.DB) (companyID string, cleanup func()) {
	t.Helper()

	var envID, groupID string
	if err := db.QueryRow(`INSERT INTO environments (name) VALUES ('teste-rfb-pgtos-fornecedores-2.5') RETURNING id`).Scan(&envID); err != nil {
		t.Fatalf("falha ao criar environment de teste: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO enterprise_groups (environment_id, name) VALUES ($1, 'grupo-teste-2.5') RETURNING id`, envID).Scan(&groupID); err != nil {
		t.Fatalf("falha ao criar enterprise_group de teste: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO companies (group_id, name) VALUES ($1, 'empresa-teste-2.5') RETURNING id`, groupID).Scan(&companyID); err != nil {
		t.Fatalf("falha ao criar company de teste: %v", err)
	}

	cleanup = func() {
		db.Exec(`DELETE FROM pagamentos_fornecedores WHERE company_id = $1`, companyID)
		db.Exec(`DELETE FROM nfe_entradas WHERE company_id = $1`, companyID)
		db.Exec(`DELETE FROM cte_entradas WHERE company_id = $1`, companyID)
		db.Exec(`DELETE FROM rfb_creditos WHERE company_id = $1`, companyID)
		db.Exec(`DELETE FROM environments WHERE id = $1`, envID) // cascade limpa enterprise_groups/companies
	}
	return companyID, cleanup
}

// insertPagamento insere uma linha de pagamentos_fornecedores de teste.
// numDocPagamento == "" grava NULL. origem 'csv' recebe um import_id gerado;
// origem 'sap_api' grava import_id NULL, espelhando persistPayments (Story 2.3).
func insertPagamento(t *testing.T, db *sql.DB, companyID, chaveDoc, tipoDoc, fornCNPJ string, valor float64, numDocPagamento, origem, bukrs string) {
	t.Helper()

	var numDoc interface{}
	if numDocPagamento != "" {
		numDoc = numDocPagamento
	}
	var bukrsVal interface{}
	if bukrs != "" {
		bukrsVal = bukrs
	}

	if origem == "csv" {
		if _, err := db.Exec(`
			INSERT INTO pagamentos_fornecedores
				(company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento,
				 num_doc_pagamento, mes_ano, origem, import_id)
			VALUES ($1, $2, $3, $4, '2026-06-15', $5, $6, '2026-06', 'csv', gen_random_uuid())
		`, companyID, chaveDoc, tipoDoc, fornCNPJ, valor, numDoc); err != nil {
			t.Fatalf("falha ao inserir pagamento csv de teste: %v", err)
		}
		return
	}

	if _, err := db.Exec(`
		INSERT INTO pagamentos_fornecedores
			(company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento,
			 num_doc_pagamento, mes_ano, origem, bukrs)
		VALUES ($1, $2, $3, $4, '2026-06-16', $5, $6, '2026-06', 'sap_api', $7)
	`, companyID, chaveDoc, tipoDoc, fornCNPJ, valor, numDoc, bukrsVal); err != nil {
		t.Fatalf("falha ao inserir pagamento sap_api de teste: %v", err)
	}
}

// insertPagamentoSAPComStatus insere um pagamento origem='sap_api' com
// payment_status/match_type/fallback_ambiguous explícitos — usado pelos
// testes da Story 3.2 (indicador de pagamento). insertPagamento (acima) não
// controla esses campos (ficam NULL/false), suficiente para as Stories
// 2.5/3.1 mas não para esta.
func insertPagamentoSAPComStatus(t *testing.T, db *sql.DB, companyID, chaveDoc, tipoDoc, fornCNPJ string, valor float64, numDocPagamento, bukrs, paymentStatus, matchType string, fallbackAmbiguous bool) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO pagamentos_fornecedores
			(company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento,
			 num_doc_pagamento, mes_ano, origem, bukrs, payment_status, match_type, fallback_ambiguous)
		VALUES ($1, $2, $3, $4, '2026-06-16', $5, $6, '2026-06', 'sap_api', $7, $8, $9, $10)
	`, companyID, chaveDoc, tipoDoc, fornCNPJ, valor, numDocPagamento, bukrs, paymentStatus, matchType, fallbackAmbiguous); err != nil {
		t.Fatalf("falha ao inserir pagamento sap_api com status de teste: %v", err)
	}
}

// insertNFeEntrada insere um cabeçalho mínimo de NF-e de teste, com v_nf
// preenchido — necessário para valor_nota/possivel_duplicidade na conciliação.
func insertNFeEntrada(t *testing.T, db *sql.DB, companyID, chaveNFe string, vNF float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO nfe_entradas
			(company_id, chave_nfe, modelo, data_emissao, mes_ano, forn_cnpj, v_nf)
		VALUES ($1, $2, 55, '2026-06-10', '2026-06', '12345678000190', $3)
	`, companyID, chaveNFe, vNF); err != nil {
		t.Fatalf("falha ao inserir nfe_entradas de teste: %v", err)
	}
}

// insertRfbCredito insere um crédito RFB de teste, com um rfb_requests
// mínimo associado (FK obrigatória). modeloDfe: '55' (NFE) ou '57' (CTE).
func insertRfbCredito(t *testing.T, db *sql.DB, companyID, chaveDfe, modeloDfe string, valorCbsNaoExtinto float64) {
	t.Helper()
	var requestID string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, status) VALUES ($1, '12345678', 'completed') RETURNING id
	`, companyID).Scan(&requestID); err != nil {
		t.Fatalf("falha ao criar rfb_requests de teste: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO rfb_creditos (request_id, company_id, modelo_dfe, chave_dfe, valor_cbs_nao_extinto, situacao_credito)
		VALUES ($1, $2, $3, $4, $5, 'Em Análise')
	`, requestID, companyID, modeloDfe, chaveDfe, valorCbsNaoExtinto); err != nil {
		t.Fatalf("falha ao inserir rfb_creditos de teste: %v", err)
	}
}

// queryConciliacaoStatus consulta apenas status_conciliacao para uma chave_doc
// — usado pelos testes da Story 3.1 (inversão rfb_creditos), que testam o
// status resultante em vez das contagens/valores agregados.
func queryConciliacaoStatus(t *testing.T, db *sql.DB, companyID, chaveDoc string) string {
	t.Helper()
	const q = cteBase + `
SELECT status_conciliacao
FROM conciliacao
WHERE chave_doc = $4
`
	var status string
	if err := db.QueryRow(q, companyID, "", "", chaveDoc).Scan(&status); err != nil {
		t.Fatalf("erro ao consultar status_conciliacao para chave_doc=%s: %v", chaveDoc, err)
	}
	return status
}

// indicadoresPagamento agrupa os campos novos da Story 3.2 (indicador de
// pagamento como evidência auxiliar) para os testes.
type indicadoresPagamento struct {
	paymentStatus        sql.NullString
	matchType            sql.NullString
	fallbackAmbiguous    bool
	origem               string
	pagamentoCobreNota   bool
	ultimoStatusBusca    sql.NullString
	ultimaTentativaBusca sql.NullString
}

func queryIndicadoresPagamento(t *testing.T, db *sql.DB, companyID, chaveDoc string) indicadoresPagamento {
	t.Helper()
	const q = cteBase + `
SELECT payment_status, match_type, fallback_ambiguous, origem, pagamento_cobre_valor_nota,
       ultimo_status_busca, ultima_tentativa_busca
FROM conciliacao
WHERE chave_doc = $4
`
	var ind indicadoresPagamento
	if err := db.QueryRow(q, companyID, "", "", chaveDoc).Scan(
		&ind.paymentStatus, &ind.matchType, &ind.fallbackAmbiguous, &ind.origem, &ind.pagamentoCobreNota,
		&ind.ultimoStatusBusca, &ind.ultimaTentativaBusca,
	); err != nil {
		t.Fatalf("erro ao consultar indicadores de pagamento para chave_doc=%s: %v", chaveDoc, err)
	}
	return ind
}

// insertSapResultadoBusca insere uma linha de teste em sap_resultados_busca
// (Story 3.2) diretamente, sem passar por persistPayments (services).
func insertSapResultadoBusca(t *testing.T, db *sql.DB, companyID, chaveDfe, bukrs, paymentStatus, matchType string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO sap_resultados_busca (company_id, chave_dfe, bukrs, payment_status, match_type)
		VALUES ($1, $2, $3, $4, $5)
	`, companyID, chaveDfe, bukrs, paymentStatus, matchType); err != nil {
		t.Fatalf("falha ao inserir sap_resultados_busca de teste: %v", err)
	}
}

// querySumarioExtintos consulta a contagem/soma de créditos com
// status_conciliacao='extinto' — mesma lógica agregada de sumSQL (handler),
// reescrita aqui porque sumSQL é uma const local à closure do handler.
func querySumarioExtintos(t *testing.T, db *sql.DB, companyID string) (notasExtinto int, cbsExtinto float64) {
	t.Helper()
	const q = cteBase + `
SELECT
    COUNT(*) FILTER (WHERE status_conciliacao = 'extinto'),
    COALESCE(SUM(CASE WHEN status_conciliacao = 'extinto' THEN valor_cbs_nao_extinto ELSE 0 END), 0)
FROM conciliacao
`
	if err := db.QueryRow(q, companyID, "", "").Scan(&notasExtinto, &cbsExtinto); err != nil {
		t.Fatalf("erro ao consultar sumário de extintos: %v", err)
	}
	return notasExtinto, cbsExtinto
}

// queryConciliacaoItem executa a CTE real (cteBase) filtrando por chave_doc,
// exatamente como recomendado nos Dev Notes da Story 2.5 — testa a lógica de
// dedup/agregação diretamente via SQL, sem passar pela camada HTTP/JWT.
type conciliacaoRow struct {
	numParcelas         int
	totalPago           float64
	valorNota           float64
	possivelDuplicidade bool
}

func queryConciliacaoItem(t *testing.T, db *sql.DB, companyID, chaveDoc string) conciliacaoRow {
	t.Helper()
	const q = cteBase + `
SELECT num_parcelas, total_pago, valor_nota, possivel_duplicidade
FROM conciliacao
WHERE chave_doc = $4
`
	var row conciliacaoRow
	if err := db.QueryRow(q, companyID, "", "", chaveDoc).Scan(
		&row.numParcelas, &row.totalPago, &row.valorNota, &row.possivelDuplicidade,
	); err != nil {
		t.Fatalf("erro ao consultar conciliação para chave_doc=%s: %v", chaveDoc, err)
	}
	return row
}

// queryTotalPagoComFiltroMesAno consulta total_pago/num_parcelas agregados
// para uma chave_doc com um filtro de mes_ano específico ($2), retornando
// zero se a chave não aparecer no resultado filtrado (em vez de falhar) —
// necessário para o teste de regressão do "buraco temporal" abaixo, onde a
// ausência de linhas é justamente o que está sendo verificado.
func queryTotalPagoComFiltroMesAno(t *testing.T, db *sql.DB, companyID, chaveDoc, mesAno string) (numParcelas int, totalPago float64) {
	t.Helper()
	const q = cteBase + `
SELECT COALESCE(SUM(num_parcelas), 0), COALESCE(SUM(total_pago), 0)
FROM pagamentos_agg
WHERE chave_doc = $4
`
	if err := db.QueryRow(q, companyID, mesAno, "", chaveDoc).Scan(&numParcelas, &totalPago); err != nil {
		t.Fatalf("erro ao consultar total_pago filtrado por mes_ano=%s para chave_doc=%s: %v", mesAno, chaveDoc, err)
	}
	return numParcelas, totalPago
}

func TestConciliacao_FiltroMesAno_NaoEsconderLiquidacaoEmMesDiferente(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Regressão: a linha csv (mes_ano=2026-06) e a linha sap_api correspondente
	// (mesmo num_doc_pagamento, mas mes_ano=2026-05 — divergência de data real
	// mas rara) não podem se anular mutuamente a ponto da liquidação inteira
	// desaparecer ao filtrar por um dos dois meses. O EXISTS da CTE de dedup
	// deve respeitar o mesmo filtro de mes_ano da consulta externa.
	chaveDoc := "35240612345678000190550010000000019000000001"
	if _, err := db.Exec(`
		INSERT INTO pagamentos_fornecedores
			(company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento,
			 num_doc_pagamento, mes_ano, origem, import_id)
		VALUES ($1, $2, 'NFE', '12345678000190', '2026-06-15', 100.00, 'DOC-MES', '2026-06', 'csv', gen_random_uuid())
	`, companyID, chaveDoc); err != nil {
		t.Fatalf("falha ao inserir pagamento csv de teste: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO pagamentos_fornecedores
			(company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento,
			 num_doc_pagamento, mes_ano, origem, bukrs)
		VALUES ($1, $2, 'NFE', '12345678000190', '2026-05-30', 100.00, 'DOC-MES', '2026-05', 'sap_api', '1000')
	`, companyID, chaveDoc); err != nil {
		t.Fatalf("falha ao inserir pagamento sap_api de teste: %v", err)
	}

	// Filtrando pelo mês da linha csv: a linha csv NÃO pode ser suprimida aqui,
	// pois a sap_api que a substituiria não está neste mês — o pagamento não
	// pode simplesmente sumir do relatório de junho.
	numParcelas, totalPago := queryTotalPagoComFiltroMesAno(t, db, companyID, chaveDoc, "2026-06")
	if numParcelas == 0 || totalPago == 0 {
		t.Errorf("BUG: liquidação desapareceu ao filtrar mes_ano=2026-06 (num_parcelas=%d, total_pago=%.2f) — o pagamento de R$100 ficou invisível", numParcelas, totalPago)
	}

	// Sem filtro de mês, o dedup normal deve se aplicar (conta uma vez, valor sap_api).
	numParcelasSemFiltro, totalPagoSemFiltro := queryTotalPagoComFiltroMesAno(t, db, companyID, chaveDoc, "")
	if numParcelasSemFiltro != 1 || totalPagoSemFiltro != 100.00 {
		t.Errorf("esperava num_parcelas=1 total_pago=100.00 sem filtro de mês, obteve num_parcelas=%d total_pago=%.2f", numParcelasSemFiltro, totalPagoSemFiltro)
	}
}

func TestConciliacao_CSVeSAP_MesmaLiquidacao_ContaUmaVez(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chaveDoc := "35240612345678000190550010000000011000000001"
	insertNFeEntrada(t, db, companyID, chaveDoc, 150.00)
	// Mesma liquidação (mesmo num_doc_pagamento) registrada duas vezes: uma via
	// CSV (valor desatualizado) e depois via SAP (valor correto) — cenário
	// central da Story 2.5, AC #1/#2.
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 100.00, "DOC-1", "csv", "")
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 150.00, "DOC-1", "sap_api", "1000")

	row := queryConciliacaoItem(t, db, companyID, chaveDoc)

	if row.numParcelas != 1 {
		t.Errorf("esperava num_parcelas=1 (dedup por liquidação), obteve %d", row.numParcelas)
	}
	if row.totalPago != 150.00 {
		t.Errorf("esperava total_pago=150.00 (valor do registro sap_api prevalecendo), obteve %.2f", row.totalPago)
	}
}

func TestConciliacao_LinhaCSVContinuaVisivelParaAuditoria(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chaveDoc := "35240612345678000190550010000000011000000002"
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 100.00, "DOC-2", "csv", "")
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 150.00, "DOC-2", "sap_api", "1000")

	// A linha csv "perdedora" da agregação nunca é apagada da tabela — segue
	// consultável (ex.: via PagamentosFornecedoresListHandler), para auditoria.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pagamentos_fornecedores WHERE company_id = $1 AND chave_doc = $2`, companyID, chaveDoc).Scan(&count); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if count != 2 {
		t.Errorf("esperava 2 linhas na tabela (csv + sap_api, nenhuma apagada), obteve %d", count)
	}

	var csvCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pagamentos_fornecedores WHERE company_id = $1 AND chave_doc = $2 AND origem = 'csv'`, companyID, chaveDoc).Scan(&csvCount); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if csvCount != 1 {
		t.Errorf("esperava a linha csv original ainda presente, obteve %d linhas csv", csvCount)
	}
}

func TestConciliacao_NumDocPagamentoDiferente_NaoColapsa(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chaveDoc := "35240612345678000190550010000000011000000003"
	insertNFeEntrada(t, db, companyID, chaveDoc, 500.00)
	// Duas parcelas GENUINAMENTE distintas (num_doc_pagamento diferente) — não
	// deve colapsar, mesmo com origens diferentes (não é regressão do
	// comportamento aceito no PRD para parcelas realmente distintas).
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 100.00, "DOC-CSV", "csv", "")
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 150.00, "DOC-SAP", "sap_api", "1000")

	row := queryConciliacaoItem(t, db, companyID, chaveDoc)

	if row.numParcelas != 2 {
		t.Errorf("esperava num_parcelas=2 (liquidações distintas, sem colapsar), obteve %d", row.numParcelas)
	}
	if row.totalPago != 250.00 {
		t.Errorf("esperava total_pago=250.00 (100+150, ambos contados), obteve %.2f", row.totalPago)
	}
}

func TestConciliacao_PossivelDuplicidade(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chaveComExcesso := "35240612345678000190550010000000011000000004"
	insertNFeEntrada(t, db, companyID, chaveComExcesso, 100.00)
	insertPagamento(t, db, companyID, chaveComExcesso, "NFE", "12345678000190", 100.00, "DOC-A", "csv", "")
	insertPagamento(t, db, companyID, chaveComExcesso, "NFE", "12345678000190", 100.00, "DOC-B", "sap_api", "1000")

	row := queryConciliacaoItem(t, db, companyID, chaveComExcesso)
	if !row.possivelDuplicidade {
		t.Errorf("esperava possivel_duplicidade=true (total_pago %.2f > valor_nota %.2f)", row.totalPago, row.valorNota)
	}

	chaveNormal := "35240612345678000190550010000000011000000005"
	insertNFeEntrada(t, db, companyID, chaveNormal, 200.00)
	insertPagamento(t, db, companyID, chaveNormal, "NFE", "12345678000190", 100.00, "DOC-C", "csv", "")

	rowNormal := queryConciliacaoItem(t, db, companyID, chaveNormal)
	if rowNormal.possivelDuplicidade {
		t.Errorf("esperava possivel_duplicidade=false (total_pago %.2f <= valor_nota %.2f)", rowNormal.totalPago, rowNormal.valorNota)
	}
}

func TestConciliacao_NFSESemNotaCasada_NaoFalsoPositivoDeDuplicidade(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// NFSE é um tipo_doc válido (pagamentos_fornecedores.go, validTipos) mas
	// nunca tem cabeçalho em nfe_entradas/cte_entradas — valor_nota cai no
	// fallback 0. Sem a checagem "valor_nota IS NOT NULL", QUALQUER pagamento
	// NFSE marcaria possivel_duplicidade=true (bug encontrado em revisão).
	chaveNFSE := "NFSE-2024-000001"
	insertPagamento(t, db, companyID, chaveNFSE, "NFSE", "98765432000110", 500.00, "", "csv", "")

	row := queryConciliacaoItem(t, db, companyID, chaveNFSE)
	if row.possivelDuplicidade {
		t.Errorf("esperava possivel_duplicidade=false para NFSE sem nota casada (valor_nota sempre 0 para este tipo_doc), obteve true")
	}
}

func TestConciliacao_CreditoSemPagamento_AguardandoPagamento(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Story 3.1, AC #1: crédito RFB com valor_cbs_nao_extinto > 0 e NENHUM
	// pagamento (nem csv, nem sap_api) — antes da inversão, este crédito
	// simplesmente não aparecia na CTE (base era pagamentos_fornecedores).
	chaveDfe := "35240612345678000190550010000000031000000001"
	insertRfbCredito(t, db, companyID, chaveDfe, "55", 80.00)

	status := queryConciliacaoStatus(t, db, companyID, chaveDfe)
	if status != "aguardando_pagamento" {
		t.Errorf("esperava status_conciliacao='aguardando_pagamento', obteve %q", status)
	}
}

func TestConciliacao_CreditoComPagamento_Pendente(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Crédito com valor_cbs_nao_extinto > 0 E pagamento localizado — deve
	// continuar 'pendente' (não 'aguardando_pagamento', que é só para ausência
	// total de pagamento).
	chaveDfe := "35240612345678000190550010000000031000000002"
	insertRfbCredito(t, db, companyID, chaveDfe, "55", 80.00)
	insertPagamento(t, db, companyID, chaveDfe, "NFE", "12345678000190", 50.00, "DOC-PEND", "csv", "")

	status := queryConciliacaoStatus(t, db, companyID, chaveDfe)
	if status != "pendente" {
		t.Errorf("esperava status_conciliacao='pendente' (tem pagamento, mas RFB ainda não extinguiu), obteve %q", status)
	}
}

func TestConciliacao_CreditoJaPago_NaoViraAguardandoPagamentoSobFiltroDeMes(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Bug encontrado em revisão: a determinação de status_conciliacao usava
	// pa.chave_doc IS NULL (que reflete o filtro de mes_ano ATUAL) em vez de
	// checar se existe pagamento em QUALQUER mês. Um crédito pago em maio,
	// filtrado por junho, aparecia incorretamente como 'aguardando_pagamento'
	// (como se nunca tivesse sido pago) em vez de 'pendente'.
	chaveDfe := "35240612345678000190550010000000031000000007"
	insertRfbCredito(t, db, companyID, chaveDfe, "55", 80.00)
	if _, err := db.Exec(`
		INSERT INTO pagamentos_fornecedores
			(company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento, mes_ano, origem, import_id)
		VALUES ($1, $2, 'NFE', '12345678000190', '2026-05-15', 80.00, '2026-05', 'csv', gen_random_uuid())
	`, companyID, chaveDfe); err != nil {
		t.Fatalf("falha ao inserir pagamento de teste: %v", err)
	}

	const q = cteBase + `SELECT status_conciliacao FROM conciliacao WHERE chave_doc = $4`
	var statusJunho string
	if err := db.QueryRow(q, companyID, "2026-06", "", chaveDfe).Scan(&statusJunho); err != nil {
		t.Fatalf("erro ao consultar status filtrado por 2026-06: %v", err)
	}
	if statusJunho != "pendente" {
		t.Errorf("BUG: esperava status_conciliacao='pendente' sob filtro mes_ano=2026-06 (crédito já foi pago em maio), obteve %q", statusJunho)
	}
}

func TestConciliacao_ModeloDfeDesconhecido_NaoQuebraScan(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Bug encontrado em revisão: tipo_doc_derivado é NULL para modelo_dfe fora
	// de '55'/'57' (ex.: '65' de NFC-e) — ConciliacaoItem.TipoDoc é string não
	// nullable no Go, então escanear NULL aqui quebrava a página inteira com
	// 500 ("converting NULL to string"), não apenas o item problemático.
	// Dígitos 21-22 desta chave são '65' (não '55'/'57'), então nem modelo_dfe
	// nem o fallback por SUBSTRING conseguem derivar NFE/CTE.
	chaveDfe := "35240612345678000190650010000000031000000080"
	insertRfbCredito(t, db, companyID, chaveDfe, "65", 50.00)

	const q = cteBase + `SELECT tipo_doc, status_conciliacao FROM conciliacao WHERE chave_doc = $4`
	var tipoDoc, status string
	if err := db.QueryRow(q, companyID, "", "", chaveDfe).Scan(&tipoDoc, &status); err != nil {
		t.Fatalf("BUG: Scan falhou para modelo_dfe desconhecido (deveria cair em tipo_doc='' via COALESCE): %v", err)
	}
	if tipoDoc != "" {
		t.Errorf("esperava tipo_doc='' (modelo_dfe desconhecido), obteve %q", tipoDoc)
	}
	if status != "aguardando_pagamento" {
		t.Errorf("esperava status_conciliacao='aguardando_pagamento' mesmo com tipo_doc indeterminado, obteve %q", status)
	}
}

func TestConciliacao_SumarioContaExtintosSemPagamento(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Story 3.1, AC #2: "CBS Extinto" deve refletir TODOS os créditos extintos
	// pela RFB, com ou sem pagamento registrado — antes da inversão, um
	// crédito extinto sem pagamento nunca entrava nessa contagem.
	chaveComPagamento := "35240612345678000190550010000000031000000003"
	insertRfbCredito(t, db, companyID, chaveComPagamento, "55", 0)
	insertPagamento(t, db, companyID, chaveComPagamento, "NFE", "12345678000190", 100.00, "DOC-EXT1", "csv", "")

	chaveSemPagamento := "35240612345678000190550010000000031000000004"
	insertRfbCredito(t, db, companyID, chaveSemPagamento, "55", 0)

	notasExtinto, cbsExtinto := querySumarioExtintos(t, db, companyID)
	if notasExtinto != 2 {
		t.Errorf("esperava 2 notas extintas (com e sem pagamento), obteve %d", notasExtinto)
	}
	// cbs_extinto soma valor_cbs_nao_extinto (sempre 0 para extinto) — o que
	// importa aqui é a CONTAGEM de notas, não o valor somado.
	_ = cbsExtinto
}

func TestConciliacao_PagamentoSemCredito_ContinuaSemDados(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Story 3.1, AC #3 (regressão explícita): pagamento sem NENHUM rfb_creditos
	// correspondente continua aparecendo como 'sem_dados' — a inversão não pode
	// quebrar esse caso, que já existia antes desta story.
	chaveDoc := "35240612345678000190550010000000031000000005"
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 100.00, "DOC-ORFAO", "csv", "")

	status := queryConciliacaoStatus(t, db, companyID, chaveDoc)
	if status != "sem_dados" {
		t.Errorf("esperava status_conciliacao='sem_dados' (pagamento sem crédito RFB), obteve %q", status)
	}
}

func TestConciliacao_FiltroMesAno_NaoEsconderCreditoAguardandoPagamento(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Task 4: um crédito aguardando_pagamento não tem data_pagamento nenhuma —
	// o filtro de mes_ano não pode escondê-lo, senão FR-11 fica letra morta
	// sob qualquer filtro de mês aplicado.
	chaveDfe := "35240612345678000190550010000000031000000006"
	insertRfbCredito(t, db, companyID, chaveDfe, "55", 80.00)

	const q = cteBase + `SELECT status_conciliacao FROM conciliacao WHERE chave_doc = $4`
	var status string
	if err := db.QueryRow(q, companyID, "2026-06", "", chaveDfe).Scan(&status); err != nil {
		t.Fatalf("BUG: crédito aguardando_pagamento desapareceu sob filtro mes_ano=2026-06: %v", err)
	}
	if status != "aguardando_pagamento" {
		t.Errorf("esperava status_conciliacao='aguardando_pagamento' mesmo com filtro de mês, obteve %q", status)
	}
}

func TestConciliacao_RegressaoSoCSV_ComportamentoInalterado(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chaveDoc := "35240612345678000190550010000000011000000006"
	insertNFeEntrada(t, db, companyID, chaveDoc, 300.00)
	// Duas parcelas CSV distintas (valores diferentes, para não colidir com
	// uq_pag_forn_csv), sem nenhum pagamento SAP — comportamento anterior a
	// esta story preservado (soma tudo, nada é excluído).
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 100.00, "DOC-X", "csv", "")
	insertPagamento(t, db, companyID, chaveDoc, "NFE", "12345678000190", 120.00, "", "csv", "")

	row := queryConciliacaoItem(t, db, companyID, chaveDoc)
	if row.numParcelas != 2 {
		t.Errorf("esperava num_parcelas=2 (regressão: só csv, sem dedup aplicável), obteve %d", row.numParcelas)
	}
	if row.totalPago != 220.00 {
		t.Errorf("esperava total_pago=220.00, obteve %.2f", row.totalPago)
	}
}

// ─── Story 3.2: Indicador de Pagamento como Evidência Auxiliar ────────────────

func TestConciliacao_PagoTotal_IndicadorLocalizado_NaoAlteraStatus(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// AC #1: PAGO_TOTAL mostra indicador de pagamento localizado, mas
	// status_conciliacao continua vindo só de rfb_creditos.valor_cbs_nao_extinto.
	chaveDfe := "35240612345678000190550010000000032000000001"
	insertRfbCredito(t, db, companyID, chaveDfe, "55", 80.00) // RFB ainda não extinguiu
	insertPagamentoSAPComStatus(t, db, companyID, chaveDfe, "NFE", "12345678000190", 100.00, "DOC-1", "1000", "PAGO_TOTAL", "CHAVE", false)

	ind := queryIndicadoresPagamento(t, db, companyID, chaveDfe)
	if !ind.paymentStatus.Valid || ind.paymentStatus.String != "PAGO_TOTAL" {
		t.Errorf("esperava payment_status='PAGO_TOTAL', obteve %+v", ind.paymentStatus)
	}
	status := queryConciliacaoStatus(t, db, companyID, chaveDfe)
	if status != "pendente" {
		t.Errorf("esperava status_conciliacao='pendente' (RFB não extinguiu, SAP não decide) mesmo com PAGO_TOTAL, obteve %q", status)
	}
}

func TestConciliacao_PagamentoCobreNota_AguardandoConfirmacaoRFB(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// AC #2: total_pago >= valor_nota, mas crédito ainda pendente (RFB não
	// confirmou) → pagamento_cobre_valor_nota=true, sem reclassificar o crédito.
	chaveCobre := "35240612345678000190550010000000032000000002"
	insertRfbCredito(t, db, companyID, chaveCobre, "55", 50.00)
	insertNFeEntrada(t, db, companyID, chaveCobre, 100.00)
	insertPagamentoSAPComStatus(t, db, companyID, chaveCobre, "NFE", "12345678000190", 100.00, "DOC-2", "1000", "PAGO_TOTAL", "CHAVE", false)

	ind := queryIndicadoresPagamento(t, db, companyID, chaveCobre)
	if !ind.pagamentoCobreNota {
		t.Errorf("esperava pagamento_cobre_valor_nota=true (total_pago=100 >= valor_nota=100)")
	}
	status := queryConciliacaoStatus(t, db, companyID, chaveCobre)
	if status != "pendente" {
		t.Errorf("esperava status_conciliacao inalterado ('pendente'), obteve %q", status)
	}

	// Caso normal: pagamento não cobre a nota inteira.
	chaveNormal := "35240612345678000190550010000000032000000003"
	insertRfbCredito(t, db, companyID, chaveNormal, "55", 50.00)
	insertNFeEntrada(t, db, companyID, chaveNormal, 200.00)
	insertPagamentoSAPComStatus(t, db, companyID, chaveNormal, "NFE", "12345678000190", 50.00, "DOC-3", "1000", "PAGO_PARCIAL", "CHAVE", false)

	indNormal := queryIndicadoresPagamento(t, db, companyID, chaveNormal)
	if indNormal.pagamentoCobreNota {
		t.Errorf("esperava pagamento_cobre_valor_nota=false (total_pago=50 < valor_nota=200)")
	}
}

func TestConciliacao_UltimoStatusBusca_NaoLocalizadoVsEmAberto(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// AC #3: crédito aguardando_pagamento com última tentativa NAO_LOCALIZADO
	// vs EM_ABERTO — mensagens/dados distintos.
	chaveNaoLocalizado := "35240612345678000190550010000000032000000004"
	insertRfbCredito(t, db, companyID, chaveNaoLocalizado, "55", 30.00)
	insertSapResultadoBusca(t, db, companyID, chaveNaoLocalizado, "1000", "NAO_LOCALIZADO", "NAO_LOCALIZADO")

	ind := queryIndicadoresPagamento(t, db, companyID, chaveNaoLocalizado)
	if !ind.ultimoStatusBusca.Valid || ind.ultimoStatusBusca.String != "NAO_LOCALIZADO" {
		t.Errorf("esperava ultimo_status_busca='NAO_LOCALIZADO', obteve %+v", ind.ultimoStatusBusca)
	}
	if !ind.ultimaTentativaBusca.Valid {
		t.Errorf("esperava ultima_tentativa_busca preenchida")
	}

	chaveEmAberto := "35240612345678000190550010000000032000000005"
	insertRfbCredito(t, db, companyID, chaveEmAberto, "55", 30.00)
	insertSapResultadoBusca(t, db, companyID, chaveEmAberto, "1000", "EM_ABERTO", "CHAVE")

	indAberto := queryIndicadoresPagamento(t, db, companyID, chaveEmAberto)
	if !indAberto.ultimoStatusBusca.Valid || indAberto.ultimoStatusBusca.String != "EM_ABERTO" {
		t.Errorf("esperava ultimo_status_busca='EM_ABERTO', obteve %+v", indAberto.ultimoStatusBusca)
	}

	// Crédito aguardando_pagamento sem NENHUMA tentativa registrada — ambos NULL.
	chaveNuncaTentado := "35240612345678000190550010000000032000000006"
	insertRfbCredito(t, db, companyID, chaveNuncaTentado, "55", 30.00)

	indNunca := queryIndicadoresPagamento(t, db, companyID, chaveNuncaTentado)
	if indNunca.ultimoStatusBusca.Valid {
		t.Errorf("esperava ultimo_status_busca=NULL (nunca tentado), obteve %+v", indNunca.ultimoStatusBusca)
	}
}

func TestConciliacao_Origem_VisivelNoItemAgregado(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// AC #4: origem visível no item agregado (não só no drill-down de parcelas).
	chaveCSV := "35240612345678000190550010000000032000000007"
	insertPagamento(t, db, companyID, chaveCSV, "NFE", "12345678000190", 100.00, "DOC-CSV", "csv", "")
	indCSV := queryIndicadoresPagamento(t, db, companyID, chaveCSV)
	if indCSV.origem != "csv" {
		t.Errorf("esperava origem='csv', obteve %q", indCSV.origem)
	}

	chaveSAP := "35240612345678000190550010000000032000000008"
	insertPagamentoSAPComStatus(t, db, companyID, chaveSAP, "NFE", "12345678000190", 100.00, "DOC-SAP", "1000", "PAGO_TOTAL", "CHAVE", false)
	indSAP := queryIndicadoresPagamento(t, db, companyID, chaveSAP)
	if indSAP.origem != "sap_api" {
		t.Errorf("esperava origem='sap_api', obteve %q", indSAP.origem)
	}
}

func TestConciliacao_ValorCbsNaoExtintoNull_NaoQuebraScanDeCobreValorNota(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Bug crítico encontrado em revisão (mesma classe do tipo_doc NULL da 3.1):
	// valor_cbs_nao_extinto é NULLABLE (migration 096, DEFAULT 0 mas sem NOT
	// NULL). Com nota casada + pagamento cobrindo + valor_cbs_nao_extinto NULL,
	// a expressão de pagamento_cobre_valor_nota resolvia para NULL (TRUE AND
	// NULL AND TRUE AND TRUE) e o Scan em `bool` derrubava a página inteira com
	// 500. O COALESCE(..., false) corrige. insertRfbCredito só aceita float,
	// então grava-se o crédito NULL direto aqui.
	chaveDfe := "35240612345678000190550010000000032000000009"
	var requestID string
	if err := db.QueryRow(`INSERT INTO rfb_requests (company_id, cnpj_base, status) VALUES ($1, '12345678', 'completed') RETURNING id`, companyID).Scan(&requestID); err != nil {
		t.Fatalf("falha ao criar rfb_requests: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO rfb_creditos (request_id, company_id, modelo_dfe, chave_dfe, valor_cbs_nao_extinto)
		VALUES ($1, $2, '55', $3, NULL)
	`, requestID, companyID, chaveDfe); err != nil {
		t.Fatalf("falha ao inserir rfb_creditos com valor_cbs_nao_extinto NULL: %v", err)
	}
	insertNFeEntrada(t, db, companyID, chaveDfe, 100.00)
	insertPagamentoSAPComStatus(t, db, companyID, chaveDfe, "NFE", "12345678000190", 100.00, "DOC-NULL", "1000", "PAGO_TOTAL", "CHAVE", false)

	// O que importa é o Scan NÃO falhar (antes do fix, isto retornava
	// "converting NULL to bool" e derrubava a listagem inteira).
	ind := queryIndicadoresPagamento(t, db, companyID, chaveDfe)
	if ind.pagamentoCobreNota {
		t.Errorf("esperava pagamento_cobre_valor_nota=false quando valor_cbs_nao_extinto é NULL (crédito não é 'pendente'), obteve true")
	}
}
