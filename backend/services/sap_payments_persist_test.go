package services

import (
	"database/sql"
	"testing"
)

func countPagamentosSAP(t *testing.T, db *sql.DB, companyID, chaveDoc string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM pagamentos_fornecedores WHERE company_id = $1 AND chave_doc = $2 AND origem = 'sap_api'
	`, companyID, chaveDoc).Scan(&count); err != nil {
		t.Fatalf("falha ao contar pagamentos_fornecedores: %v", err)
	}
	return count
}

func TestPersistPayments_ItemComPagamento_CriaLinha(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(1)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		MatchType:     "CHAVE",
		SupplierCNPJ:  "12345678000199",
		PaymentStatus: "PAGO_TOTAL",
		Payments: []paymentItem{{
			ClearingDocument: "1400000001",
			ClearingDate:     "2026-06-10",
			PaidAmount:       1000.50,
			SettlementMode:   "art27",
		}},
	}}

	persistPayments(db, companyID, "1000", items)

	if got := countPagamentosSAP(t, db, companyID, chave); got != 1 {
		t.Fatalf("esperava 1 linha criada, obteve %d", got)
	}

	var origem, paymentStatus, matchType, bukrs, numDoc string
	var valor float64
	var fallbackAmbiguous bool
	if err := db.QueryRow(`
		SELECT origem, payment_status, match_type, bukrs, num_doc_pagamento, valor_pagamento, fallback_ambiguous
		FROM pagamentos_fornecedores WHERE company_id = $1 AND chave_doc = $2 AND origem = 'sap_api'
	`, companyID, chave).Scan(&origem, &paymentStatus, &matchType, &bukrs, &numDoc, &valor, &fallbackAmbiguous); err != nil {
		t.Fatalf("falha ao ler linha persistida: %v", err)
	}
	if origem != "sap_api" || paymentStatus != "PAGO_TOTAL" || matchType != "CHAVE" || bukrs != "1000" || numDoc != "1400000001" {
		t.Errorf("campos inesperados: origem=%s payment_status=%s match_type=%s bukrs=%s num_doc=%s", origem, paymentStatus, matchType, bukrs, numDoc)
	}
	if valor != 1000.50 {
		t.Errorf("esperava valor_pagamento=1000.50, obteve %v", valor)
	}
	if fallbackAmbiguous {
		t.Error("esperava fallback_ambiguous=false para matchType=CHAVE")
	}
}

func TestPersistPayments_SemPayments_NaoCriaLinha(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(2)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		MatchType:     "NAO_LOCALIZADO",
		PaymentStatus: "NAO_LOCALIZADO",
		Payments:      nil, // EM_ABERTO/NAO_LOCALIZADO não têm payments[]
	}}

	persistPayments(db, companyID, "1000", items)

	if got := countPagamentosSAP(t, db, companyID, chave); got != 0 {
		t.Errorf("esperava 0 linhas para dfeResponse sem payments[], obteve %d", got)
	}
}

func TestPersistPayments_Reprocessamento_AtualizaEmVezDeDuplicar(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(3)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		MatchType:     "CHAVE",
		SupplierCNPJ:  "11111111000111",
		PaymentStatus: "PAGO_PARCIAL",
		Payments: []paymentItem{{
			ClearingDocument: "1400000002",
			ClearingDate:     "2026-06-10",
			PaidAmount:       500.00,
		}},
	}}

	persistPayments(db, companyID, "1000", items)
	if got := countPagamentosSAP(t, db, companyID, chave); got != 1 {
		t.Fatalf("esperava 1 linha após primeira persistência, obteve %d", got)
	}

	// Reprocessa a mesma chave/BUKRS/num_doc_pagamento com valor/status/data/tipo
	// atualizados — confirma que o upsert refresca TODOS os campos mutáveis,
	// não só payment_status/valor_pagamento (achado de code review).
	items[0].PaymentStatus = "PAGO_TOTAL"
	items[0].DFeType = "CTE"
	items[0].SupplierCNPJ = "22222222000122"
	items[0].Payments[0].PaidAmount = 1500.00
	items[0].Payments[0].ClearingDate = "2026-07-15" // muda de mês (2026-06 -> 2026-07)
	persistPayments(db, companyID, "1000", items)

	if got := countPagamentosSAP(t, db, companyID, chave); got != 1 {
		t.Fatalf("AC #2: esperava exatamente 1 linha após reprocessamento (upsert), obteve %d", got)
	}

	var paymentStatus, tipoDoc, mesAno, fornCnpj string
	var valor float64
	if err := db.QueryRow(`
		SELECT payment_status, valor_pagamento, tipo_doc, mes_ano, forn_cnpj FROM pagamentos_fornecedores
		WHERE company_id = $1 AND chave_doc = $2 AND origem = 'sap_api'
	`, companyID, chave).Scan(&paymentStatus, &valor, &tipoDoc, &mesAno, &fornCnpj); err != nil {
		t.Fatalf("falha ao ler linha atualizada: %v", err)
	}
	if paymentStatus != "PAGO_TOTAL" || valor != 1500.00 {
		t.Errorf("esperava dados atualizados (PAGO_TOTAL, 1500.00), obteve (%s, %v)", paymentStatus, valor)
	}
	if tipoDoc != "CTE" {
		t.Errorf("esperava tipo_doc atualizado para CTE, obteve %q", tipoDoc)
	}
	if mesAno != "2026-07" {
		t.Errorf("esperava mes_ano atualizado para 2026-07 (data mudou de mês), obteve %q", mesAno)
	}
	if fornCnpj != "22222222000122" {
		t.Errorf("esperava forn_cnpj atualizado, obteve %q", fornCnpj)
	}
}

// TestPersistPayments_PagamentosPorConta_MesmoValorMesmaData_NaoColidem cobre
// o cenário de negócio "pagamentos por conta" (parcelas distintas de mesmo
// valor no mesmo dia, para a mesma chave/BUKRS) — a razão de uq_pag_forn ter
// sido trocada por um índice parcial nesta story (ver Dev Notes/migration 118).
// Antes da correção, este teste falhava com "duplicate key value violates
// unique constraint uq_pag_forn".
func TestPersistPayments_PagamentosPorConta_MesmoValorMesmaData_NaoColidem(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(8)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		PaymentStatus: "PAGO_PARCIAL",
		Payments: []paymentItem{
			{ClearingDocument: "1400000010", ClearingDate: "2026-06-10", PaidAmount: 250.00},
			{ClearingDocument: "1400000011", ClearingDate: "2026-06-10", PaidAmount: 250.00}, // "por conta": mesmo valor/data, doc diferente
		},
	}}

	persistPayments(db, companyID, "1000", items)

	if got := countPagamentosSAP(t, db, companyID, chave); got != 2 {
		t.Errorf("esperava 2 linhas (pagamentos por conta são distintos por num_doc_pagamento), obteve %d", got)
	}
}

func TestPersistPayments_BukrsDiferentes_NaoColidem(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(4)
	makeItem := func() []dfeResponse {
		return []dfeResponse{{
			DFeKey:        chave,
			DFeType:       "NFE",
			PaymentStatus: "PAGO_TOTAL",
			Payments: []paymentItem{{
				ClearingDocument: "1400000003", // mesmo num_doc_pagamento
				ClearingDate:     "2026-06-10",
				PaidAmount:       200.00,
			}},
		}}
	}

	persistPayments(db, companyID, "1000", makeItem())
	persistPayments(db, companyID, "2000", makeItem())

	if got := countPagamentosSAP(t, db, companyID, chave); got != 2 {
		t.Errorf("esperava 2 linhas (uma por BUKRS, mesma chave/num_doc_pagamento), obteve %d", got)
	}
}

func TestPersistPayments_CompanyCodeDaRespostaEIgnorado_UsaSempreOParametro(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(5)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		CompanyCode:   "9999", // valor da resposta do SAP — NUNCA deve ser usado (AC #4)
		PaymentStatus: "PAGO_TOTAL",
		Payments: []paymentItem{{
			ClearingDocument: "1400000004",
			ClearingDate:     "2026-06-10",
			PaidAmount:       300.00,
		}},
	}}

	persistPayments(db, companyID, "1000", items) // bukrs do contexto = "1000", não "9999"

	var bukrs, gotCompanyID string
	if err := db.QueryRow(`
		SELECT bukrs, company_id::text FROM pagamentos_fornecedores WHERE company_id = $1 AND chave_doc = $2 AND origem = 'sap_api'
	`, companyID, chave).Scan(&bukrs, &gotCompanyID); err != nil {
		t.Fatalf("falha ao ler linha persistida: %v", err)
	}
	if bukrs != "1000" {
		t.Errorf("AC #4: esperava bukrs='1000' (parâmetro do contexto), obteve %q (CompanyCode da resposta deveria ter sido ignorado)", bukrs)
	}
	if gotCompanyID != companyID {
		t.Errorf("esperava company_id=%s, obteve %s", companyID, gotCompanyID)
	}
}

func TestPersistPayments_ClearingDateInvalida_IgnoraItemSemAbortar(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(6)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		PaymentStatus: "PAGO_TOTAL",
		Payments: []paymentItem{
			{ClearingDocument: "data-invalida", ClearingDate: "not-a-date", PaidAmount: 100},
			{ClearingDocument: "1400000005", ClearingDate: "2026-06-10", PaidAmount: 200},
		},
	}}

	failed := persistPayments(db, companyID, "1000", items)

	if got := countPagamentosSAP(t, db, companyID, chave); got != 1 {
		t.Errorf("esperava 1 linha (item com data inválida ignorado, item válido persistido), obteve %d", got)
	}
	if failed != 1 {
		t.Errorf("esperava failedCount=1 (data inválida contabilizada como falha), obteve %d", failed)
	}
}

// TestPersistPayments_ClearingDocumentVazio_NaoSobrescreveOutroPagamento cobre
// o achado de code review: dois pagamentos do mesmo item com ClearingDocument
// vazio colidiriam no índice de dedup e um sobrescreveria o outro
// silenciosamente via DO UPDATE. Corrigido: pagamento com ClearingDocument
// vazio é ignorado e contado como falha, nunca persistido.
func TestPersistPayments_ClearingDocumentVazio_NaoSobrescreveOutroPagamento(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(10)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		PaymentStatus: "PAGO_PARCIAL",
		Payments: []paymentItem{
			{ClearingDocument: "", ClearingDate: "2026-06-10", PaidAmount: 100},
			{ClearingDocument: "", ClearingDate: "2026-06-10", PaidAmount: 200},
			{ClearingDocument: "1400000030", ClearingDate: "2026-06-10", PaidAmount: 300},
		},
	}}

	failed := persistPayments(db, companyID, "1000", items)

	if got := countPagamentosSAP(t, db, companyID, chave); got != 1 {
		t.Errorf("esperava 1 linha (só o pagamento com clearingDocument preenchido), obteve %d", got)
	}
	if failed != 2 {
		t.Errorf("esperava failedCount=2 (2 pagamentos com clearingDocument vazio ignorados), obteve %d", failed)
	}
}

// TestPersistPayments_PaidAmountZeroOuNegativo_Ignorado cobre o achado de
// code review: paidAmount <= 0 violaria o CHECK (valor_pagamento > 0) do banco
// — corrigido para ser filtrado explicitamente, com log determinístico em vez
// de depender só do erro genérico do banco.
func TestPersistPayments_PaidAmountZeroOuNegativo_Ignorado(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(11)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		PaymentStatus: "PAGO_PARCIAL",
		Payments: []paymentItem{
			{ClearingDocument: "1400000040", ClearingDate: "2026-06-10", PaidAmount: 0},
			{ClearingDocument: "1400000041", ClearingDate: "2026-06-10", PaidAmount: -50},
			{ClearingDocument: "1400000042", ClearingDate: "2026-06-10", PaidAmount: 75},
		},
	}}

	failed := persistPayments(db, companyID, "1000", items)

	if got := countPagamentosSAP(t, db, companyID, chave); got != 1 {
		t.Errorf("esperava 1 linha (só o pagamento com valor > 0), obteve %d", got)
	}
	if failed != 2 {
		t.Errorf("esperava failedCount=2 (paidAmount 0 e negativo ignorados), obteve %d", failed)
	}
}

// TestPersistPayments_CompanyIdDiferentes_NaoColidem cobre isolamento
// multi-tenant real no fluxo de persistência (achado de code review — só
// havia teste de BUKRS diferente, não de company_id diferente).
func TestPersistPayments_CompanyIdDiferentes_NaoColidem(t *testing.T) {
	db := openTestDB(t)
	companyA, cleanupA := setupTestCompany(t, db)
	defer cleanupA()
	companyB, cleanupB := setupTestCompany(t, db)
	defer cleanupB()

	chave := validKey(12)
	makeItem := func() []dfeResponse {
		return []dfeResponse{{
			DFeKey:        chave,
			DFeType:       "NFE",
			PaymentStatus: "PAGO_TOTAL",
			Payments: []paymentItem{{
				ClearingDocument: "1400000050", // mesmo num_doc_pagamento e bukrs nas duas empresas
				ClearingDate:     "2026-06-10",
				PaidAmount:       400.00,
			}},
		}}
	}

	persistPayments(db, companyA, "1000", makeItem())
	persistPayments(db, companyB, "1000", makeItem())

	if got := countPagamentosSAP(t, db, companyA, chave); got != 1 {
		t.Errorf("esperava 1 linha para company_id A, obteve %d", got)
	}
	if got := countPagamentosSAP(t, db, companyB, chave); got != 1 {
		t.Errorf("esperava 1 linha para company_id B (isolamento multi-tenant, mesma chave/bukrs/num_doc_pagamento), obteve %d", got)
	}
}

// TestPersistPayments_FallbackAmbiguo_MarcaTrue cobre o caminho positivo da
// heurística de fallback_ambiguous — os testes existentes só cobriam
// MatchType=CHAVE (false); faltava o caminho FALLBACK com FallbackNote
// preenchido (true).
func TestPersistPayments_FallbackAmbiguo_MarcaTrue(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(13)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "NFE",
		MatchType:     "FALLBACK",
		FallbackNote:  "Múltiplos candidatos encontrados por CNPJ+número de documento",
		PaymentStatus: "PAGO_TOTAL",
		Payments: []paymentItem{{
			ClearingDocument: "1400000060",
			ClearingDate:     "2026-06-10",
			PaidAmount:       600.00,
		}},
	}}

	persistPayments(db, companyID, "1000", items)

	var fallbackAmbiguous bool
	if err := db.QueryRow(`
		SELECT fallback_ambiguous FROM pagamentos_fornecedores WHERE company_id = $1 AND chave_doc = $2 AND origem = 'sap_api'
	`, companyID, chave).Scan(&fallbackAmbiguous); err != nil {
		t.Fatalf("falha ao ler linha persistida: %v", err)
	}
	if !fallbackAmbiguous {
		t.Error("esperava fallback_ambiguous=true para MatchType=FALLBACK com FallbackNote preenchido")
	}
}

// TestPersistPayments_DFeTypeDesconhecido_NaoEstouraTamanhoDaColuna cobre o
// achado real de verificação E2E: o SAP pode retornar dfeType="DESCONHECIDO"
// (12 caracteres) para documentos fora de NF-e/CT-e — tipo_doc precisou ser
// alargado de VARCHAR(10) para VARCHAR(20) na migration 118 para não estourar.
func TestPersistPayments_DFeTypeDesconhecido_NaoEstouraTamanhoDaColuna(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := validKey(9)
	items := []dfeResponse{{
		DFeKey:        chave,
		DFeType:       "DESCONHECIDO",
		PaymentStatus: "PAGO_TOTAL",
		Payments: []paymentItem{{
			ClearingDocument: "1400000020",
			ClearingDate:     "2026-06-10",
			PaidAmount:       50.00,
		}},
	}}

	persistPayments(db, companyID, "1000", items)

	if got := countPagamentosSAP(t, db, companyID, chave); got != 1 {
		t.Errorf("esperava 1 linha persistida mesmo com dfeType longo (DESCONHECIDO), obteve %d", got)
	}
}

// TestUqPagForn_CSVContinuaFuncionando confirma que a dedup do fluxo de import
// CSV continua funcionando após a migration 118 desta story — a constraint
// original uq_pag_forn (tabela inteira, migration 110) foi substituída por um
// índice único parcial (uq_pag_forn_csv, WHERE origem='csv') para não colidir
// com pagamentos SAP legítimos de mesmo valor/data ("pagamentos por conta") —
// ver Dev Notes/Completion Notes. O ON CONFLICT usado aqui espelha exatamente
// o que backend/handlers/pagamentos_fornecedores.go usa após esta story.
func TestUqPagForn_CSVContinuaFuncionando(t *testing.T) {
	db := openTestDB(t)
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	insertCSV := func() (sql.Result, error) {
		return db.Exec(`
			INSERT INTO pagamentos_fornecedores
				(company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento, mes_ano, import_id)
			VALUES ($1, $2, 'NFE', '12345678000199', '2026-06-01', 100.00, '2026-06', gen_random_uuid())
			ON CONFLICT (company_id, chave_doc, data_pagamento, valor_pagamento) WHERE origem = 'csv' DO NOTHING
		`, companyID, validKey(7))
	}

	res1, err := insertCSV()
	if err != nil {
		t.Fatalf("falha no primeiro INSERT CSV: %v", err)
	}
	affected1, _ := res1.RowsAffected()
	if affected1 != 1 {
		t.Fatalf("esperava 1 linha afetada no primeiro insert, obteve %d", affected1)
	}

	res2, err := insertCSV()
	if err != nil {
		t.Fatalf("falha no segundo INSERT CSV (deveria ser DO NOTHING, não erro): %v", err)
	}
	affected2, _ := res2.RowsAffected()
	if affected2 != 0 {
		t.Errorf("esperava 0 linhas afetadas no segundo insert (dedup via uq_pag_forn), obteve %d — regressão na dedup do CSV", affected2)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pagamentos_fornecedores WHERE company_id = $1 AND chave_doc = $2 AND origem = 'csv'`, companyID, validKey(7)).Scan(&count); err != nil {
		t.Fatalf("falha ao contar: %v", err)
	}
	if count != 1 {
		t.Errorf("esperava exatamente 1 linha de CSV após dedup, obteve %d", count)
	}
}
