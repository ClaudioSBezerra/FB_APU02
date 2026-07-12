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
