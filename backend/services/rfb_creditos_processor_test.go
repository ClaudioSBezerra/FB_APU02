package services

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// creditosJSON monta um corpo de apuração de créditos (endpoint /creditos-cbs/)
// com um grupo corrente e, opcionalmente, um grupo de créditos extemporâneos —
// usado para testar o bucketing "tipo_apuracao <> 'ajuste' → total_corrente".
func creditosJSON(dataApuracao string, correnteCreditos, extemporaneoCreditos []testDFe) string {
	item := func(it testDFe) string {
		return fmt.Sprintf(`{"chaveDfe":"%s","dataApuracao":"%s","valorCBSTotal":%.2f,"valorCBSExtinto":0,"valorCBSNaoExtinto":%.2f}`,
			it.Chave, dataApuracao, it.Valor, it.Valor)
	}

	var sb strings.Builder
	sb.WriteString(`{"apuracaoCorrente":{"creditos":[`)
	for i, it := range correnteCreditos {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(item(it))
	}
	sb.WriteString(`],"debitos":[]}`)
	if len(extemporaneoCreditos) > 0 {
		sb.WriteString(`,"debitosExtemporaneos":{"creditos":[`)
		for i, it := range extemporaneoCreditos {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(item(it))
		}
		sb.WriteString(`],"debitos":[]}`)
	}
	sb.WriteString(`}`)
	return sb.String()
}

// TestProcessarDownloadCreditosRFB_SomaCumulativaEntreChamadas espelha a AC
// central da spec para o endpoint dedicado de créditos: duas chamadas com chaves
// diferentes somam via agregação SQL sobre rfb_creditos, não sobrescrevem com só
// a última chamada.
func TestProcessarDownloadCreditosRFB_SomaCumulativaEntreChamadas(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodo = "202604"

	client1 := newFakeRFBDownload(t, creditosJSON(periodo, []testDFe{{Chave: "CRED-A", Valor: 30}}, nil))
	req1 := insertRFBRequest(t, db, companyID, "tiq-credagg-1", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, client1, req1); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	waitAsyncSAPSync()

	totalCreditos, valorTotal, corrente, _, found := readRfbCreditosResumo(t, db, companyID, periodo)
	if !found || totalCreditos != 1 || valorTotal != 30 || corrente != 1 {
		t.Fatalf("após call 1: esperava 1/30.00/1, veio found=%v %d/%.2f/%d", found, totalCreditos, valorTotal, corrente)
	}

	client2 := newFakeRFBDownload(t, creditosJSON(periodo, []testDFe{{Chave: "CRED-B", Valor: 40}}, nil))
	req2 := insertRFBRequest(t, db, companyID, "tiq-credagg-2", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, client2, req2); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	waitAsyncSAPSync()

	totalCreditos, valorTotal, corrente, _, found = readRfbCreditosResumo(t, db, companyID, periodo)
	if !found || totalCreditos != 2 || valorTotal != 70 || corrente != 2 {
		t.Fatalf("após call 2: esperava soma cumulativa 2/70.00/2 (não só a última chamada isolada 1/40.00/1), veio %d/%.2f/%d",
			totalCreditos, valorTotal, corrente)
	}
}

// TestProcessarDownloadCreditosRFB_ItemSemChaveNaoDuplica cobre a mesma correção
// do Loopback 1 para o endpoint de créditos: item sem chave_dfe repetido em duas
// chamadas não pode duplicar no resumo final.
func TestProcessarDownloadCreditosRFB_ItemSemChaveNaoDuplica(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodo = "202605"
	body := creditosJSON(periodo, []testDFe{{Chave: "", Valor: 15}}, nil)

	client1 := newFakeRFBDownload(t, body)
	req1 := insertRFBRequest(t, db, companyID, "tiq-credsemchave-1", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, client1, req1); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	waitAsyncSAPSync()

	client2 := newFakeRFBDownload(t, body)
	req2 := insertRFBRequest(t, db, companyID, "tiq-credsemchave-2", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, client2, req2); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	waitAsyncSAPSync()

	totalCreditos, valorTotal, corrente, _, found := readRfbCreditosResumo(t, db, companyID, periodo)
	if !found || totalCreditos != 1 || valorTotal != 15 || corrente != 1 {
		t.Fatalf("item sem chave repetido em 2 chamadas não deve duplicar no resumo: esperava 1/15.00/1, veio found=%v %d/%.2f/%d",
			found, totalCreditos, valorTotal, corrente)
	}

	var rowsSemChave int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM rfb_creditos
		WHERE company_id = $1 AND data_apuracao = $2 AND (chave_dfe IS NULL OR chave_dfe = '')
	`, companyID, normalizeDataApuracao(periodo)).Scan(&rowsSemChave); err != nil {
		t.Fatalf("falha ao contar rfb_creditos sem chave: %v", err)
	}
	if rowsSemChave != 2 {
		t.Fatalf("esperava 2 linhas físicas sem chave em rfb_creditos (sem dedup possível), achou %d", rowsSemChave)
	}
}

// TestProcessarDownloadCreditosRFB_PeriodoVazioSemResumo cobre o caso de borda de
// período sem nenhum crédito: rfb_creditos_resumo não é gravado (gating
// pré-existente `if totalCreditos > 0`, preservado pela spec), sem erro.
func TestProcessarDownloadCreditosRFB_PeriodoVazioSemResumo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	client := newFakeRFBDownload(t, `{}`)
	req := insertRFBRequest(t, db, companyID, "tiq-credvazio", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, client, req); err != nil {
		t.Fatalf("call: %v", err)
	}
	waitAsyncSAPSync()

	_, _, _, _, found := readRfbCreditosResumo(t, db, companyID, "")
	if found {
		t.Fatalf("período sem créditos: esperava nenhuma linha em rfb_creditos_resumo (gating total_creditos > 0)")
	}
}

// TestProcessarDownloadCreditosRFB_ExtemporaneoContaComoCorrente confirma a
// regra de bucketing decidida no Loopback 1 (Design Notes / linha 25 da spec):
// tipo_apuracao <> 'ajuste' (incluindo "extemporaneo") conta em total_corrente —
// não há coluna total_extemporaneo em rfb_creditos_resumo.
func TestProcessarDownloadCreditosRFB_ExtemporaneoContaComoCorrente(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodo = "202606"
	// Inclui um item corrente junto do extemporâneo para exercitar o bucketing dos dois
	// tipos numa mesma chamada (dataApuracao é capturado de ambos os blocos — ver
	// TestProcessarDownloadCreditosRFB_ApenasExtemporaneo_PeriodoReal para o caso
	// só-extemporâneo).
	client := newFakeRFBDownload(t, creditosJSON(periodo,
		[]testDFe{{Chave: "COR-1", Valor: 5}},
		[]testDFe{{Chave: "EXT-1", Valor: 15}},
	))
	req := insertRFBRequest(t, db, companyID, "tiq-credextemp", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, client, req); err != nil {
		t.Fatalf("call: %v", err)
	}
	waitAsyncSAPSync()

	totalCreditos, valorTotal, corrente, ajuste, found := readRfbCreditosResumo(t, db, companyID, periodo)
	if !found || totalCreditos != 2 || valorTotal != 20 || corrente != 2 || ajuste != 0 {
		t.Fatalf("crédito extemporâneo deve contar em total_corrente junto do corrente: esperava 2/20.00 corrente=2 ajuste=0, veio found=%v %d/%.2f corrente=%d ajuste=%d",
			found, totalCreditos, valorTotal, corrente, ajuste)
	}
}

// TestProcessarDownloadCreditosRFB_ApenasExtemporaneo_PeriodoReal cobre o Patch 2
// da 2ª rodada de revisão adversarial (spec-rfb-resumo-agregacao-sql.md) no endpoint
// dedicado de créditos: antes da correção, dataApuracao só era capturado nos blocos
// ApuracaoCorrente e no fallback flat — o bloco ApuracaoAjuste e o bloco
// DebitosExtemporaneos nunca setavam a variável. Uma resposta só com créditos
// extemporâneos (sem nenhum item corrente/ajuste) deixava dataApuracao="" e a
// agregação SQL filtrava por data_apuracao=”, enquanto os próprios itens eram
// inseridos em rfb_creditos com o período REAL — o resumo do período verdadeiro
// nunca era atualizado. Este teste assevera que o resumo é gravado sob o período
// real (não vazio) com os valores corretos.
func TestProcessarDownloadCreditosRFB_ApenasExtemporaneo_PeriodoReal(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodo = "202609"
	// correnteCreditos vazio (nil): SOMENTE o grupo debitosExtemporaneos tem itens.
	body := creditosJSON(periodo, nil, []testDFe{{Chave: "EXT-C1", Valor: 33}})

	client := newFakeRFBDownload(t, body)
	req := insertRFBRequest(t, db, companyID, "tiq-credextemp-only", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, client, req); err != nil {
		t.Fatalf("call: %v", err)
	}
	waitAsyncSAPSync()

	// O período real deve ter o resumo correto — não zerado, não ausente.
	totalCreditos, valorTotal, corrente, ajuste, found := readRfbCreditosResumo(t, db, companyID, periodo)
	if !found || totalCreditos != 1 || valorTotal != 33 || corrente != 1 || ajuste != 0 {
		t.Fatalf("payload só-extemporâneo: esperava resumo do período real %s = 1 crédito/33.00/corrente=1, veio found=%v %d/%.2f corrente=%d ajuste=%d",
			periodo, found, totalCreditos, valorTotal, corrente, ajuste)
	}

	// Nada deve ter sido gravado sob o período vazio ("") — esse era o bug.
	_, _, _, _, foundVazio := readRfbCreditosResumo(t, db, companyID, "")
	if foundVazio {
		t.Fatalf("payload só-extemporâneo não pode gravar rfb_creditos_resumo sob período vazio (bug do Patch 2)")
	}
}

// creditoV2JSON monta um crédito v2 completo (árvore cbs.apropriacao.utilizacao.naoUtilizado).
func creditoV2JSON(chave, registro string, apurado float64) string {
	return fmt.Sprintf(`{"origem":2,"documento":57,"chave":"%s","emissao":"2026-01-10T10:00:00","registro":"%s","atualizacao":"2026-02-01T00:00:00",
		"cbs":{"apurado":%.2f,"excedentes":1.5,"apropriacao":{"inapropriavel":2.5,"suspenso":3.5,"prescrito":4.5,"aApropriar":5.5,"apropriado":6.5,
		"utilizacao":{"inutilizavel":7.5,"utilizado":8.5,"restabelecido":9.5,"naoUtilizado":{"saldoCredor":10.5,"pedidoRessarcimento":11.5}}}}}`,
		chave, registro, apurado)
}

// (f) + multi-período: créditos v2 gravam a árvore nas colunas novas e geram um resumo por pa.
func TestProcessarDownloadCreditosRFB_V2_ArvoreEMultiPeriodo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	body := `{"apuracao":[` +
		`{"pa":"12/2025","creditos":[` + creditoV2JSON("CV2-A", "2025-12-10T00:00:00", 100) + `]},` +
		`{"pa":"01/2026","creditos":[` + creditoV2JSON("CV2-B", "2026-01-10T00:00:00", 200) + `,` + creditoV2JSON("CV2-C", "2026-04-01T00:00:00", 50) + `]}]}`
	req := insertRFBRequest(t, db, companyID, "tiq-credv2", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, newFakeRFBDownload(t, body), req); err != nil {
		t.Fatalf("créditos v2: %v", err)
	}
	waitAsyncSAPSync()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rfb_creditos_resumo WHERE company_id = $1`, companyID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("esperava 2 linhas em rfb_creditos_resumo (uma por pa), veio %d (err=%v)", n, err)
	}
	if tc, vt, _, _, found := readRfbCreditosResumo(t, db, companyID, "12/2025"); !found || tc != 1 || vt != 100 {
		t.Fatalf("12/2025: esperava 1/100, veio found=%v %d/%.2f", found, tc, vt)
	}
	// bucketing v2 em créditos: CV2-C (registro em abril) é extemporaneo, que conta em total_corrente
	// (aggregateRfbCreditosSQL: tipo <> 'ajuste').
	if tc, vt, corrente, _, found := readRfbCreditosResumo(t, db, companyID, "01/2026"); !found || tc != 2 || vt != 250 || corrente != 2 {
		t.Fatalf("01/2026: esperava 2/250/corrente=2, veio found=%v %d/%.2f/%d", found, tc, vt, corrente)
	}

	// (v) extinto = utilizacao.utilizado (8.5), nao_extinto = naoUtilizado.saldoCredor (10.5)
	var ext, naoExt float64
	if err := db.QueryRow(`SELECT valor_cbs_extinto, valor_cbs_nao_extinto FROM rfb_creditos WHERE company_id = $1 AND chave_dfe = 'CV2-C'`, companyID).Scan(&ext, &naoExt); err != nil {
		t.Fatalf("ler extinto/nao_extinto: %v", err)
	}
	if ext != 8.5 || naoExt != 10.5 {
		t.Fatalf("créditos v2: esperava extinto=8.5 nao_extinto=10.5, veio %.2f/%.2f", ext, naoExt)
	}

	var da, tipo string
	var v [11]sql.NullFloat64
	var origem, documento sql.NullInt64
	var reg, atu sql.NullTime
	if err := db.QueryRow(`
		SELECT data_apuracao, tipo_apuracao, origem, documento, data_registro, data_atualizacao,
		       valor_cbs_excedentes, valor_cbs_apropriacao_inapropriavel, valor_cbs_apropriacao_suspenso,
		       valor_cbs_apropriacao_prescrito, valor_cbs_apropriacao_a_apropriar, valor_cbs_apropriacao_apropriado,
		       valor_cbs_utilizacao_inutilizavel, valor_cbs_utilizacao_utilizado, valor_cbs_utilizacao_restabelecido,
		       valor_cbs_nao_utilizado_saldo_credor, valor_cbs_nao_utilizado_pedido_ressarcimento
		FROM rfb_creditos WHERE company_id = $1 AND chave_dfe = 'CV2-C'
	`, companyID).Scan(&da, &tipo, &origem, &documento, &reg, &atu,
		&v[0], &v[1], &v[2], &v[3], &v[4], &v[5], &v[6], &v[7], &v[8], &v[9], &v[10]); err != nil {
		t.Fatalf("ler rfb_creditos: %v", err)
	}
	if da != "01/2026" || tipo != "extemporaneo" {
		t.Fatalf("data_apuracao/tipo: esperava 01/2026/extemporaneo, veio %s/%s", da, tipo)
	}
	if origem.Int64 != 2 || documento.Int64 != 57 || !reg.Valid || !atu.Valid {
		t.Fatalf("origem/documento/registro/atualizacao: %+v %+v %+v %+v", origem, documento, reg, atu)
	}
	for i, want := range []float64{1.5, 2.5, 3.5, 4.5, 5.5, 6.5, 7.5, 8.5, 9.5, 10.5, 11.5} {
		if !v[i].Valid || v[i].Float64 != want {
			t.Fatalf("coluna da árvore #%d: esperava %.1f, veio %+v", i, want, v[i])
		}
	}

	// Reprocessar o raw_json v2 mantém o resultado.
	if err := ReprocessarRawJSONCreditosRFB(db, req); err != nil {
		t.Fatalf("reprocessar créditos v2: %v", err)
	}
	waitAsyncSAPSync()
	if tc, vt, _, _, found := readRfbCreditosResumo(t, db, companyID, "01/2026"); !found || tc != 2 || vt != 250 {
		t.Fatalf("após reprocessar: esperava 2/250, veio found=%v %d/%.2f", found, tc, vt)
	}
}

// v1 de créditos normaliza "AAAAMM" → "mm/aaaa" na persistência e colunas v2 ficam NULL.
func TestProcessarDownloadCreditosRFB_V1GravaMMAAAAEColunasV2Nulas(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	req := insertRFBRequest(t, db, companyID, "tiq-credv1-norm", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, newFakeRFBDownload(t, creditosJSON("202602", []testDFe{{Chave: "CV1-A", Valor: 12}}, nil)), req); err != nil {
		t.Fatalf("v1: %v", err)
	}
	waitAsyncSAPSync()

	var da string
	var origem sql.NullInt64
	var apropriado sql.NullFloat64
	if err := db.QueryRow(`SELECT data_apuracao, origem, valor_cbs_apropriacao_apropriado FROM rfb_creditos WHERE company_id = $1 AND chave_dfe = 'CV1-A'`, companyID).Scan(&da, &origem, &apropriado); err != nil {
		t.Fatal(err)
	}
	if da != "02/2026" || origem.Valid || apropriado.Valid {
		t.Fatalf("esperava 02/2026 com colunas v2 NULL, veio %s origem=%+v apropriado=%+v", da, origem, apropriado)
	}
}

// período inválido em créditos v2 é descartado sem gravar linhas/resumo; os demais processam.
func TestProcessarDownloadCreditosRFB_V2_PeriodoInvalidoIgnorado(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	body := `{"apuracao":[` +
		`{"pa":"13/2026","creditos":[` + creditoV2JSON("CINV-A", "2026-01-10T00:00:00", 10) + `]},` +
		`{"pa":"01/2026","creditos":[` + creditoV2JSON("COK-A", "2026-01-10T00:00:00", 200) + `]}]}`
	req := insertRFBRequest(t, db, companyID, "tiq-credv2-inv", "credito", "producao")
	if err := ProcessarDownloadCreditosRFB(db, newFakeRFBDownload(t, body), req); err != nil {
		t.Fatalf("créditos v2: %v", err)
	}
	waitAsyncSAPSync()

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM rfb_creditos WHERE company_id = $1 AND chave_dfe = 'CINV-A'`, companyID).Scan(&n)
	if n != 0 {
		t.Fatalf("crédito de período inválido não pode ser gravado, veio %d", n)
	}
	if _, _, _, _, found := readRfbCreditosResumo(t, db, companyID, "13/2026"); found {
		t.Fatalf("resumo de período inválido não pode existir")
	}
	if tc, vt, _, _, found := readRfbCreditosResumo(t, db, companyID, "01/2026"); !found || tc != 1 || vt != 200 {
		t.Fatalf("01/2026: esperava 1/200, veio found=%v %d/%.2f", found, tc, vt)
	}
}
