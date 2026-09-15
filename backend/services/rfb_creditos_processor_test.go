package services

import (
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
	`, companyID, periodo).Scan(&rowsSemChave); err != nil {
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
