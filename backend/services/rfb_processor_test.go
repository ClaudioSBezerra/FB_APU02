package services

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testDFe é um DF-e (débito ou crédito) mínimo usado para montar payloads JSON
// de teste no formato da RFB (RFBDebito/RFBCredito).
type testDFe struct {
	Chave string
	Valor float64
}

// debitosComCreditosJSON monta um corpo de apuração RFB com débitos e créditos
// embutidos dentro de "apuracaoCorrente" — espelha o formato real em que a RFB
// devolve a NF-e de entrada do comprador junto da apuração de débitos.
func debitosComCreditosJSON(dataApuracao string, debitos, creditos []testDFe) string {
	var sb strings.Builder
	sb.WriteString(`{"apuracaoCorrente":{"debitos":[`)
	for i, it := range debitos {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`{"chaveDfe":"%s","dataApuracao":"%s","valorCBSTotal":%.2f,"valorCBSExtinto":0,"valorCBSNaoExtinto":%.2f}`,
			it.Chave, dataApuracao, it.Valor, it.Valor))
	}
	sb.WriteString(`],"creditos":[`)
	for i, it := range creditos {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`{"chaveDfe":"%s","dataApuracao":"%s","valorCBSTotal":%.2f,"valorCBSExtinto":0,"valorCBSNaoExtinto":%.2f}`,
			it.Chave, dataApuracao, it.Valor, it.Valor))
	}
	sb.WriteString(`]}}`)
	return sb.String()
}

// debitosJSON monta um corpo de apuração só com débitos (sem créditos embutidos).
func debitosJSON(dataApuracao string, debitos []testDFe) string {
	return debitosComCreditosJSON(dataApuracao, debitos, nil)
}

// debitosExtemporaneosJSON monta um corpo de apuração com débitos SOMENTE no grupo
// "debitosExtemporaneos" — sem "apuracaoCorrente" nem "apuracaoAjuste". Usado para
// cobrir o Patch 2 (spec-rfb-resumo-agregacao-sql.md): antes da correção, dataApuracao
// só era capturado dos blocos corrente/ajuste, então uma resposta só com itens
// extemporâneos deixava dataApuracao="" e a agregação SQL filtrava pelo período errado.
func debitosExtemporaneosJSON(dataApuracao string, debitos []testDFe) string {
	var sb strings.Builder
	sb.WriteString(`{"debitosExtemporaneos":{"debitos":[`)
	for i, it := range debitos {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`{"chaveDfe":"%s","dataApuracao":"%s","valorCBSTotal":%.2f,"valorCBSExtinto":0,"valorCBSNaoExtinto":%.2f}`,
			it.Chave, dataApuracao, it.Valor, it.Valor))
	}
	sb.WriteString(`],"creditos":[]}}`)
	return sb.String()
}

// debitosCorrentesComCreditosExtemporaneosJSON monta um corpo com débitos em
// "apuracaoCorrente" (período periodoDebitos) e créditos embutidos SOMENTE em
// "debitosExtemporaneos" (período periodoCreditos, que pode divergir do período dos
// débitos). Usado para provar que o período usado para agregar os créditos embutidos
// (aggregateRfbCreditosSQL dentro de ProcessarDownloadRFB/ReprocessarRawJSON) vem do
// período real dos próprios itens de crédito, não é só reaproveitado de dataApuracao
// (débitos) — ver dataApuracaoCreditos.
func debitosCorrentesComCreditosExtemporaneosJSON(periodoDebitos, periodoCreditos string, debitos, creditosExtemp []testDFe) string {
	var sb strings.Builder
	sb.WriteString(`{"apuracaoCorrente":{"debitos":[`)
	for i, it := range debitos {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`{"chaveDfe":"%s","dataApuracao":"%s","valorCBSTotal":%.2f,"valorCBSExtinto":0,"valorCBSNaoExtinto":%.2f}`,
			it.Chave, periodoDebitos, it.Valor, it.Valor))
	}
	sb.WriteString(`],"creditos":[]},"debitosExtemporaneos":{"debitos":[],"creditos":[`)
	for i, it := range creditosExtemp {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`{"chaveDfe":"%s","dataApuracao":"%s","valorCBSTotal":%.2f,"valorCBSExtinto":0,"valorCBSNaoExtinto":%.2f}`,
			it.Chave, periodoCreditos, it.Valor, it.Valor))
	}
	sb.WriteString(`]}}`)
	return sb.String()
}

// newFakeRFBDownload sobe um servidor de teste que devolve body como corpo do
// download (rota /download/<rfbAPIVersion>/<tiquete>) e um token OAuth2 fixo —
// usado para exercitar ProcessarDownloadRFB/ProcessarDownloadCreditosRFB com um
// payload controlado, sem chamar a RFB real. Usa a constante rfbAPIVersion (não
// o literal "v1") para não descolar do client se a versão da API mudar.
func newFakeRFBDownload(t *testing.T, body string) *RFBClient {
	t.Helper()
	downloadPath := "/download/" + rfbAPIVersion + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			fmt.Fprint(w, `{"access_token":"tok-teste-agg","token_type":"Bearer","expires_in":3600}`)
		case strings.Contains(r.URL.Path, downloadPath):
			fmt.Fprint(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RFB_API_URL", srv.URL)
	t.Setenv("RFB_TOKEN_URL", srv.URL+"/token")
	return NewRFBClient()
}

// insertRFBRequest cria uma linha rfb_requests pronta para download (status
// 'webhook_received', fora de downloading/completed/reprocessing) simulando uma
// solicitação já respondida pela RFB, aguardando ProcessarDownloadRFB/
// ProcessarDownloadCreditosRFB.
func insertRFBRequest(t *testing.T, db *sql.DB, companyID, tiquete, tipo, ambiente string) string {
	t.Helper()
	var requestID string
	if err := db.QueryRow(`
		INSERT INTO rfb_requests (company_id, cnpj_base, tiquete, status, tipo, ambiente)
		VALUES ($1, '00000000', $2, 'webhook_received', $3, $4) RETURNING id
	`, companyID, tiquete, tipo, ambiente).Scan(&requestID); err != nil {
		t.Fatalf("falha ao criar rfb_requests de teste (tipo=%s): %v", tipo, err)
	}
	return requestID
}

// waitAsyncSAPSync dá tempo para a goroutine TriggerSAPSync disparada ao final de
// ProcessarDownloadRFB/ProcessarDownloadCreditosRFB terminar sua consulta a
// sap_credentials antes do db.Close() do teste. As empresas de teste nunca têm
// sap_credentials (a consulta retorna ErrNoRows quase imediatamente), mas
// evitamos a corrida com o Close mesmo assim.
func waitAsyncSAPSync() {
	time.Sleep(100 * time.Millisecond)
}

// readRfbResumo lê a linha de rfb_resumo para company_id+data_apuracao.
func readRfbResumo(t *testing.T, db *sql.DB, companyID, dataApuracao string) (totalDebitos int, valorTotal, valorExtinto, valorNaoExtinto float64, totalCorrente, totalAjuste, totalExtemporaneo int) {
	t.Helper()
	err := db.QueryRow(`
		SELECT total_debitos, valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
		       total_corrente, total_ajuste, total_extemporaneo
		FROM rfb_resumo WHERE company_id = $1 AND data_apuracao = $2
	`, companyID, dataApuracao).Scan(&totalDebitos, &valorTotal, &valorExtinto, &valorNaoExtinto,
		&totalCorrente, &totalAjuste, &totalExtemporaneo)
	if err != nil {
		t.Fatalf("falha ao ler rfb_resumo (company=%s periodo=%s): %v", companyID, dataApuracao, err)
	}
	return
}

// readRfbCreditosResumo lê a linha de rfb_creditos_resumo para company_id+data_apuracao.
// found=false quando nenhuma linha existe (nenhum crédito encontrado no período).
func readRfbCreditosResumo(t *testing.T, db *sql.DB, companyID, dataApuracao string) (totalCreditos int, valorTotal float64, totalCorrente, totalAjuste int, found bool) {
	t.Helper()
	err := db.QueryRow(`
		SELECT total_creditos, valor_cbs_total, total_corrente, total_ajuste
		FROM rfb_creditos_resumo WHERE company_id = $1 AND data_apuracao = $2
	`, companyID, dataApuracao).Scan(&totalCreditos, &valorTotal, &totalCorrente, &totalAjuste)
	if err == sql.ErrNoRows {
		return 0, 0, 0, 0, false
	}
	if err != nil {
		t.Fatalf("falha ao ler rfb_creditos_resumo (company=%s periodo=%s): %v", companyID, dataApuracao, err)
	}
	return totalCreditos, valorTotal, totalCorrente, totalAjuste, true
}

// TestProcessarDownloadRFB_SomaCumulativaEntreChamadas cobre a AC central da
// spec: duas chamadas sucessivas com chaves diferentes devem produzir um resumo
// com a SOMA de ambas (via agregação SQL sobre rfb_debitos), não só os totais da
// última chamada — o bug que a migração para consulta incremental da RFB
// (out/2026) exporia silenciosamente.
func TestProcessarDownloadRFB_SomaCumulativaEntreChamadas(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodo = "202601"

	client1 := newFakeRFBDownload(t, debitosJSON(periodo, []testDFe{{Chave: "CHAVE-A", Valor: 100}}))
	req1 := insertRFBRequest(t, db, companyID, "tiq-agg-1", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client1, req1); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	waitAsyncSAPSync()

	totalDebitos, valorTotal, _, _, totalCorrente, _, _ := readRfbResumo(t, db, companyID, periodo)
	if totalDebitos != 1 || valorTotal != 100 || totalCorrente != 1 {
		t.Fatalf("após call 1: esperava 1/100.00/1, veio %d/%.2f/%d", totalDebitos, valorTotal, totalCorrente)
	}

	client2 := newFakeRFBDownload(t, debitosJSON(periodo, []testDFe{{Chave: "CHAVE-B", Valor: 200}}))
	req2 := insertRFBRequest(t, db, companyID, "tiq-agg-2", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client2, req2); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	waitAsyncSAPSync()

	totalDebitos, valorTotal, _, _, totalCorrente, _, _ = readRfbResumo(t, db, companyID, periodo)
	if totalDebitos != 2 || valorTotal != 300 || totalCorrente != 2 {
		t.Fatalf("após call 2: esperava soma cumulativa 2/300.00/2 (não só a última chamada, que sozinha seria 1/200.00/1), veio %d/%.2f/%d",
			totalDebitos, valorTotal, totalCorrente)
	}
}

// TestProcessarDownloadRFB_ItemSemChaveNaoDuplica cobre a correção do Loopback 1:
// um item sem chave_dfe não pode ser deduplicado por UPSERT, então a agregação
// SQL o exclui explicitamente (chave_dfe IS NOT NULL and non-empty) e só a soma em
// memória da chamada atual é usada para ele — repetir o mesmo item sem chave em
// duas chamadas não pode duplicar seu valor no resumo final.
func TestProcessarDownloadRFB_ItemSemChaveNaoDuplica(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodo = "202602"
	body := debitosJSON(periodo, []testDFe{{Chave: "", Valor: 50}})

	client1 := newFakeRFBDownload(t, body)
	req1 := insertRFBRequest(t, db, companyID, "tiq-semchave-1", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client1, req1); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	waitAsyncSAPSync()

	client2 := newFakeRFBDownload(t, body)
	req2 := insertRFBRequest(t, db, companyID, "tiq-semchave-2", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client2, req2); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	waitAsyncSAPSync()

	totalDebitos, valorTotal, _, _, totalCorrente, _, _ := readRfbResumo(t, db, companyID, periodo)
	if totalDebitos != 1 || valorTotal != 50 || totalCorrente != 1 {
		t.Fatalf("item sem chave repetido em 2 chamadas não deve duplicar no resumo: esperava 1/50.00/1, veio %d/%.2f/%d",
			totalDebitos, valorTotal, totalCorrente)
	}

	// Confirma que a tabela normalizada, em contraste, TEM as 2 linhas físicas (não há
	// como deduplicar sem chave) — prova que a proteção está na exclusão explícita da
	// agregação SQL, não em algo que aconteceria "de graça".
	var rowsSemChave int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM rfb_debitos
		WHERE company_id = $1 AND data_apuracao = $2 AND (chave_dfe IS NULL OR chave_dfe = '')
	`, companyID, periodo).Scan(&rowsSemChave); err != nil {
		t.Fatalf("falha ao contar rfb_debitos sem chave: %v", err)
	}
	if rowsSemChave != 2 {
		t.Fatalf("esperava 2 linhas físicas sem chave em rfb_debitos (sem dedup possível), achou %d", rowsSemChave)
	}
}

// TestProcessarDownloadRFB_PeriodoVazioZeros cobre o caso de borda da matriz da
// spec: uma resposta sem nenhum grupo de débitos ainda deve gravar um resumo com
// totais 0 (via COALESCE na agregação SQL), sem erro de NULL constraint.
func TestProcessarDownloadRFB_PeriodoVazioZeros(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	client := newFakeRFBDownload(t, `{}`)
	req := insertRFBRequest(t, db, companyID, "tiq-vazio", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client, req); err != nil {
		t.Fatalf("call: %v", err)
	}
	waitAsyncSAPSync()

	totalDebitos, valorTotal, valorExtinto, valorNaoExtinto, totalCorrente, totalAjuste, totalExtemporaneo :=
		readRfbResumo(t, db, companyID, "")
	if totalDebitos != 0 || valorTotal != 0 || valorExtinto != 0 || valorNaoExtinto != 0 ||
		totalCorrente != 0 || totalAjuste != 0 || totalExtemporaneo != 0 {
		t.Fatalf("período sem linhas: esperava tudo zero, veio debitos=%d valorTotal=%.2f corrente=%d ajuste=%d extemp=%d",
			totalDebitos, valorTotal, totalCorrente, totalAjuste, totalExtemporaneo)
	}
}

// TestProcessarDownloadRFB_CreditosEmbutidos_SomaCumulativaViaSQL cobre
// especificamente o ponto 2 do Spec Change Log: o UPSERT de rfb_creditos_resumo
// embutido em ProcessarDownloadRFB (créditos que vieram junto da resposta de
// débitos) também precisa da correção híbrida SQL+memória — uma implementação
// anterior deixou esse ponto de fora, duas chamadas com créditos embutidos de
// chaves diferentes devem somar, não sobrescrever com só a última chamada.
func TestProcessarDownloadRFB_CreditosEmbutidos_SomaCumulativaViaSQL(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodo = "202603"

	body1 := debitosComCreditosJSON(periodo,
		[]testDFe{{Chave: "D1", Valor: 10}},
		[]testDFe{{Chave: "C1", Valor: 5}},
	)
	client1 := newFakeRFBDownload(t, body1)
	req1 := insertRFBRequest(t, db, companyID, "tiq-cred-1", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client1, req1); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	waitAsyncSAPSync()

	totalCreditos, valorCreditos, corrente, _, found := readRfbCreditosResumo(t, db, companyID, periodo)
	if !found || totalCreditos != 1 || valorCreditos != 5 || corrente != 1 {
		t.Fatalf("após call 1: esperava rfb_creditos_resumo 1/5.00/1, veio found=%v %d/%.2f/%d",
			found, totalCreditos, valorCreditos, corrente)
	}

	body2 := debitosComCreditosJSON(periodo,
		[]testDFe{{Chave: "D2", Valor: 20}},
		[]testDFe{{Chave: "C2", Valor: 7}},
	)
	client2 := newFakeRFBDownload(t, body2)
	req2 := insertRFBRequest(t, db, companyID, "tiq-cred-2", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client2, req2); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	waitAsyncSAPSync()

	totalCreditos, valorCreditos, corrente, _, found = readRfbCreditosResumo(t, db, companyID, periodo)
	if !found || totalCreditos != 2 || valorCreditos != 12 || corrente != 2 {
		t.Fatalf("créditos embutidos: 2 chamadas com chaves diferentes devem somar (esperava 2/12.00/2, não só a última chamada isolada 1/7.00/1), veio %d/%.2f/%d",
			totalCreditos, valorCreditos, corrente)
	}
}

// TestProcessarDownloadRFB_ApenasExtemporaneo_PeriodoReal cobre o Patch 2 da 2ª
// rodada de revisão adversarial (spec-rfb-resumo-agregacao-sql.md): antes da
// correção, dataApuracao só era capturado dos blocos ApuracaoCorrente/ApuracaoAjuste,
// nunca de DebitosExtemporaneos — uma resposta só com itens extemporâneos deixava
// dataApuracao="" e a agregação SQL filtrava por data_apuracao=”, enquanto os
// próprios itens eram inseridos em rfb_debitos com o período REAL. O resumo do
// período verdadeiro nunca era atualizado (pior que o comportamento antigo). Este
// teste assevera que, com SOMENTE debitosExtemporaneos no payload (sem corrente/
// ajuste), o resumo é gravado sob o período real e não vazio.
func TestProcessarDownloadRFB_ApenasExtemporaneo_PeriodoReal(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodo = "202607"
	body := debitosExtemporaneosJSON(periodo, []testDFe{{Chave: "EXT-D1", Valor: 42}})

	client := newFakeRFBDownload(t, body)
	req := insertRFBRequest(t, db, companyID, "tiq-extemp-only", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client, req); err != nil {
		t.Fatalf("call: %v", err)
	}
	waitAsyncSAPSync()

	// O período real deve ter o resumo correto — não zerado.
	totalDebitos, valorTotal, _, _, totalCorrente, totalAjuste, totalExtemporaneo :=
		readRfbResumo(t, db, companyID, periodo)
	if totalDebitos != 1 || valorTotal != 42 || totalExtemporaneo != 1 || totalCorrente != 0 || totalAjuste != 0 {
		t.Fatalf("payload só-extemporâneo: esperava resumo do período real %s = 1 débito/42.00/extemp=1, veio debitos=%d valorTotal=%.2f corrente=%d ajuste=%d extemp=%d",
			periodo, totalDebitos, valorTotal, totalCorrente, totalAjuste, totalExtemporaneo)
	}

	// Nada deve ter sido gravado sob o período vazio ("") — esse era o bug: a query de
	// agregação SQL rodava com dataApuracao="" e não encontrava as linhas reais.
	var countPeriodoVazio int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM rfb_resumo WHERE company_id = $1 AND data_apuracao = ''
	`, companyID).Scan(&countPeriodoVazio); err != nil {
		t.Fatalf("falha ao verificar rfb_resumo sob período vazio: %v", err)
	}
	if countPeriodoVazio != 0 {
		t.Fatalf("payload só-extemporâneo não pode gravar rfb_resumo sob período vazio (bug do Patch 2), achou %d linha(s)", countPeriodoVazio)
	}
}

// TestProcessarDownloadRFB_CreditosEmbutidos_PeriodoProprioDivergente cobre a
// decisão tomada para o Patch 2 em relação aos créditos embutidos: o período usado
// para agregar/gravar rfb_creditos_resumo (créditos embutidos na mesma resposta de
// débitos) vem do período REAL dos próprios itens de crédito processados
// (dataApuracaoCreditos), que pode divergir do período dos débitos (dataApuracao) —
// não é só reaproveitado. Aqui os débitos estão no período P1 (corrente) e os
// créditos embutidos SOMENTE no período P2 (extemporâneo), P1 != P2.
func TestProcessarDownloadRFB_CreditosEmbutidos_PeriodoProprioDivergente(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	const periodoDebitos = "202608"
	const periodoCreditos = "202607" // propositalmente diferente do período dos débitos

	body := debitosCorrentesComCreditosExtemporaneosJSON(periodoDebitos, periodoCreditos,
		[]testDFe{{Chave: "D-DIV", Valor: 100}},
		[]testDFe{{Chave: "C-DIV", Valor: 9}},
	)
	client := newFakeRFBDownload(t, body)
	req := insertRFBRequest(t, db, companyID, "tiq-periodo-divergente", "debito", "producao")
	if err := ProcessarDownloadRFB(db, client, req); err != nil {
		t.Fatalf("call: %v", err)
	}
	waitAsyncSAPSync()

	// rfb_resumo (débitos) deve ficar sob periodoDebitos.
	totalDebitos, valorDebitos, _, _, _, _, _ := readRfbResumo(t, db, companyID, periodoDebitos)
	if totalDebitos != 1 || valorDebitos != 100 {
		t.Fatalf("débitos: esperava resumo sob periodoDebitos=%s (1/100.00), veio %d/%.2f", periodoDebitos, totalDebitos, valorDebitos)
	}

	// rfb_creditos_resumo (créditos embutidos) deve ficar sob periodoCreditos — o
	// período REAL dos itens de crédito — não sob periodoDebitos nem vazio.
	totalCreditos, valorCreditos, corrente, _, found := readRfbCreditosResumo(t, db, companyID, periodoCreditos)
	if !found || totalCreditos != 1 || valorCreditos != 9 || corrente != 1 {
		t.Fatalf("créditos embutidos: esperava resumo sob periodoCreditos=%s (1/9.00/corrente=1), veio found=%v %d/%.2f/%d",
			periodoCreditos, found, totalCreditos, valorCreditos, corrente)
	}

	// Não pode ter gravado o resumo de créditos sob o período dos débitos (prova que a
	// variável de período não foi só reaproveitada de dataApuracao).
	_, _, _, _, foundSobPeriodoDebitos := readRfbCreditosResumo(t, db, companyID, periodoDebitos)
	if foundSobPeriodoDebitos {
		t.Fatalf("créditos embutidos não podem ser gravados sob o período dos débitos (%s) quando o período real dos créditos diverge (%s)",
			periodoDebitos, periodoCreditos)
	}
}
