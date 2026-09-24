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

// readRfbResumo lê a linha de rfb_resumo para company_id+data_apuracao. Aceita o período
// em "AAAAMM" (como os payloads v1 de teste o enviam) ou "mm/aaaa": desde a migration 129
// o valor persistido é sempre "mm/aaaa", então normaliza antes de consultar.
func readRfbResumo(t *testing.T, db *sql.DB, companyID, dataApuracao string) (totalDebitos int, valorTotal, valorExtinto, valorNaoExtinto float64, totalCorrente, totalAjuste, totalExtemporaneo int) {
	t.Helper()
	err := db.QueryRow(`
		SELECT total_debitos, valor_cbs_total, valor_cbs_extinto, valor_cbs_nao_extinto,
		       total_corrente, total_ajuste, total_extemporaneo
		FROM rfb_resumo WHERE company_id = $1 AND data_apuracao = $2
	`, companyID, normalizeDataApuracao(dataApuracao)).Scan(&totalDebitos, &valorTotal, &valorExtinto, &valorNaoExtinto,
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
	`, companyID, normalizeDataApuracao(dataApuracao)).Scan(&totalCreditos, &valorTotal, &totalCorrente, &totalAjuste)
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
	`, companyID, normalizeDataApuracao(periodo)).Scan(&rowsSemChave); err != nil {
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

// ── Shape v2 (apuracao[].pa / debitos[]) + normalização "mm/aaaa" (migration 129) ──

// testDebitoV2 é um débito v2 mínimo para montar payloads de teste.
type testDebitoV2 struct {
	Chave                                                    string
	Registro                                                 string // RFC3339 sem timezone; "" omite
	Apurado, Excedente, Inexigivel, Suspenso, Extinto, Saldo float64
}

// debitosV2JSON monta o corpo v2: {"apuracao":[{"pa":"mm/aaaa","debitos":[...]}]}.
// Cada chave do mapa é um pa; a ordem de iteração é fixada por paOrder.
func debitosV2JSON(paOrder []string, porPA map[string][]testDebitoV2) string {
	var sb strings.Builder
	sb.WriteString(`{"apuracao":[`)
	for i, pa := range paOrder {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`{"pa":"%s","debitos":[`, pa))
		for j, it := range porPA[pa] {
			if j > 0 {
				sb.WriteString(",")
			}
			registro := ""
			if it.Registro != "" {
				registro = fmt.Sprintf(`"registro":"%s",`, it.Registro)
			}
			sb.WriteString(fmt.Sprintf(`{"origem":1,"documento":55,"chave":"%s","emissao":"2026-01-10T10:00:00",%s"atualizacao":"2026-02-01T00:00:00","cbs":{"apurado":%.2f,"excedente":%.2f,"inexigivel":%.2f,"suspenso":%.2f,"extinto":%.2f,"saldoDevedor":%.2f}}`,
				it.Chave, registro, it.Apurado, it.Excedente, it.Inexigivel, it.Suspenso, it.Extinto, it.Saldo))
		}
		sb.WriteString(`]}`)
	}
	sb.WriteString(`]}`)
	return sb.String()
}

func TestDetectRFBShape(t *testing.T) {
	cases := map[string]string{
		`{"apuracaoCorrente":{"debitos":[]}}`:          "v1",
		`{"debitosExtemporaneos":{"debitos":[]}}`:      "v1",
		`{"apuracao":[{"pa":"01/2026","debitos":[]}]}`: "v2",
		`{"apuracao":[]}`:                              "v2",
		`{"apuracao":null}`:                            "v2",
		`{"apuracaoCorrente":{},"apuracao":[]}`:        "v1",
		`{}`:                                           "v1",
		`{"creditos":[]}`:                              "v1",
		`not json`:                                     "v1",
	}
	for raw, want := range cases {
		if got := detectRFBShape([]byte(raw)); got != want {
			t.Errorf("detectRFBShape(%s) = %s, esperava %s", raw, got, want)
		}
	}
}

func TestPeriodoValido(t *testing.T) {
	for in, want := range map[string]bool{"01/2026": true, "12/2026": true, "13/2026": false, "00/2026": false, "202601": false, "1/2026": false, "01/26": false, "": false} {
		if got := periodoValido(in); got != want {
			t.Errorf("periodoValido(%q) = %v, esperava %v", in, got, want)
		}
	}
	if periodoValido(normalizeDataApuracao("202613")) {
		t.Errorf("normalizeDataApuracao(202613) não pode produzir período válido")
	}
	if _, _, ok := parsePeriodoMMYYYY("1/2026"); ok {
		t.Errorf("parsePeriodoMMYYYY deve exigir mês com 2 dígitos")
	}
}

func TestNormalizeDataApuracao(t *testing.T) {
	cases := map[string]string{"202601": "01/2026", "01/2026": "01/2026", "": "", "2026-01": "2026-01", "20260a": "20260a", "202613": "202613", "202600": "202600"}
	for in, want := range cases {
		if got := normalizeDataApuracao(in); got != want {
			t.Errorf("normalizeDataApuracao(%q) = %q, esperava %q", in, got, want)
		}
	}
}

// (a) + (e): v2 simples, 1 período; valor_cbs_nao_extinto = saldoDevedor; colunas novas gravadas.
func TestProcessarDownloadRFB_V2_UmPeriodo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	body := debitosV2JSON([]string{"01/2026"}, map[string][]testDebitoV2{
		"01/2026": {{Chave: "V2-A", Registro: "2026-01-20T08:00:00", Apurado: 100, Excedente: 1, Inexigivel: 2, Suspenso: 3, Extinto: 30, Saldo: 70}},
	})
	req := insertRFBRequest(t, db, companyID, "tiq-v2-1", "debito", "producao")
	if err := ProcessarDownloadRFB(db, newFakeRFBDownload(t, body), req); err != nil {
		t.Fatalf("v2: %v", err)
	}
	waitAsyncSAPSync()

	totalDebitos, valorTotal, valorExtinto, valorNaoExtinto, corrente, _, _ := readRfbResumo(t, db, companyID, "01/2026")
	if totalDebitos != 1 || valorTotal != 100 || valorExtinto != 30 || corrente != 1 {
		t.Fatalf("resumo v2: esperava 1/100/30/corrente=1, veio %d/%.2f/%.2f/%d", totalDebitos, valorTotal, valorExtinto, corrente)
	}
	// (e) valor_cbs_nao_extinto = saldoDevedor (aproximação documentada)
	if valorNaoExtinto != 70 {
		t.Fatalf("valor_cbs_nao_extinto: esperava saldoDevedor=70, veio %.2f", valorNaoExtinto)
	}

	var da, tipo string
	var origem, documento sql.NullInt64
	var exc, inex, susp, saldo, naoExt sql.NullFloat64
	var reg, atu sql.NullTime
	if err := db.QueryRow(`
		SELECT data_apuracao, tipo_apuracao, origem, documento, data_registro, data_atualizacao,
		       valor_cbs_excedente, valor_cbs_inexigivel, valor_cbs_suspenso, valor_cbs_saldo_devedor, valor_cbs_nao_extinto
		FROM rfb_debitos WHERE company_id = $1 AND chave_dfe = 'V2-A'
	`, companyID).Scan(&da, &tipo, &origem, &documento, &reg, &atu, &exc, &inex, &susp, &saldo, &naoExt); err != nil {
		t.Fatalf("ler rfb_debitos: %v", err)
	}
	if da != "01/2026" || tipo != "corrente" {
		t.Fatalf("data_apuracao/tipo: esperava 01/2026/corrente, veio %s/%s", da, tipo)
	}
	if !origem.Valid || origem.Int64 != 1 || !documento.Valid || documento.Int64 != 55 || !reg.Valid || !atu.Valid {
		t.Fatalf("origem/documento/registro/atualizacao não gravados: %+v %+v %+v %+v", origem, documento, reg, atu)
	}
	if exc.Float64 != 1 || inex.Float64 != 2 || susp.Float64 != 3 || saldo.Float64 != 70 || naoExt.Float64 != 70 {
		t.Fatalf("colunas novas: excedente=%v inexigivel=%v suspenso=%v saldo=%v nao_extinto=%v", exc, inex, susp, saldo, naoExt)
	}

	// Reprocessar o raw_json v2 salvo deve dar o mesmo resultado (detector de shape no reprocess).
	if err := ReprocessarRawJSON(db, req); err != nil {
		t.Fatalf("reprocessar v2: %v", err)
	}
	waitAsyncSAPSync()
	totalDebitos, valorTotal, _, valorNaoExtinto, _, _, _ = readRfbResumo(t, db, companyID, "01/2026")
	if totalDebitos != 1 || valorTotal != 100 || valorNaoExtinto != 70 {
		t.Fatalf("após reprocessar v2: esperava 1/100/70, veio %d/%.2f/%.2f", totalDebitos, valorTotal, valorNaoExtinto)
	}
}

// (b): dois pa numa mesma resposta v2 → 2 linhas de resumo, cada uma só com o seu período.
func TestProcessarDownloadRFB_V2_DoisPeriodosGeramDoisResumos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	body := debitosV2JSON([]string{"12/2025", "01/2026"}, map[string][]testDebitoV2{
		"12/2025": {{Chave: "P1-A", Registro: "2025-12-05T00:00:00", Apurado: 10, Extinto: 0, Saldo: 10}},
		"01/2026": {
			{Chave: "P2-A", Registro: "2026-01-05T00:00:00", Apurado: 20, Saldo: 20},
			{Chave: "P2-B", Registro: "2026-01-06T00:00:00", Apurado: 30, Saldo: 30},
		},
	})
	req := insertRFBRequest(t, db, companyID, "tiq-v2-2pa", "debito", "producao")
	if err := ProcessarDownloadRFB(db, newFakeRFBDownload(t, body), req); err != nil {
		t.Fatalf("v2: %v", err)
	}
	waitAsyncSAPSync()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rfb_resumo WHERE company_id = $1`, companyID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("esperava 2 linhas em rfb_resumo (uma por pa), veio %d (err=%v)", n, err)
	}
	if td, vt, _, _, _, _, _ := readRfbResumo(t, db, companyID, "12/2025"); td != 1 || vt != 10 {
		t.Fatalf("12/2025: esperava 1/10, veio %d/%.2f", td, vt)
	}
	if td, vt, _, _, _, _, _ := readRfbResumo(t, db, companyID, "01/2026"); td != 2 || vt != 50 {
		t.Fatalf("01/2026: esperava 2/50, veio %d/%.2f", td, vt)
	}
}

// (c): heurística de bucketing v2 — mês(pa)==mês(registro) → corrente; registro posterior → extemporaneo.
func TestProcessarDownloadRFB_V2_BucketingPorDataRegistro(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	body := debitosV2JSON([]string{"01/2026"}, map[string][]testDebitoV2{
		"01/2026": {
			{Chave: "B-CORR", Registro: "2026-01-31T23:00:00", Apurado: 10, Saldo: 10},
			{Chave: "B-EXT", Registro: "2026-03-05T00:00:00", Apurado: 40, Saldo: 40},
		},
	})
	req := insertRFBRequest(t, db, companyID, "tiq-v2-bucket", "debito", "producao")
	if err := ProcessarDownloadRFB(db, newFakeRFBDownload(t, body), req); err != nil {
		t.Fatalf("v2: %v", err)
	}
	waitAsyncSAPSync()

	tipos := map[string]string{}
	rows, err := db.Query(`SELECT chave_dfe, tipo_apuracao FROM rfb_debitos WHERE company_id = $1`, companyID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c, tp string
		rows.Scan(&c, &tp)
		tipos[c] = tp
	}
	if tipos["B-CORR"] != "corrente" || tipos["B-EXT"] != "extemporaneo" {
		t.Fatalf("bucketing: esperava B-CORR=corrente/B-EXT=extemporaneo, veio %v", tipos)
	}
	td, vt, _, _, corrente, ajuste, ext := readRfbResumo(t, db, companyID, "01/2026")
	if td != 2 || vt != 50 || corrente != 1 || ajuste != 0 || ext != 1 {
		t.Fatalf("resumo: esperava 2/50/corrente=1/ajuste=0/ext=1, veio %d/%.2f/%d/%d/%d", td, vt, corrente, ajuste, ext)
	}
}

func TestBucketTipoApuracaoV2(t *testing.T) {
	d := func(s string) *time.Time { tt, _ := time.Parse("2006-01-02", s); return &tt }
	cases := []struct {
		pa   string
		reg  *time.Time
		want string
	}{
		{"01/2026", d("2026-01-01"), "corrente"},
		{"01/2026", d("2026-01-31"), "corrente"},
		{"01/2026", d("2026-02-01"), "extemporaneo"},
		{"12/2025", d("2026-01-02"), "extemporaneo"}, // virada de ano
		{"01/2026", nil, "corrente"},
		{"lixo", d("2026-05-01"), "corrente"},
	}
	for _, c := range cases {
		if got := bucketTipoApuracaoV2(c.pa, c.reg); got != c.want {
			t.Errorf("bucketTipoApuracaoV2(%s, %v) = %s, esperava %s", c.pa, c.reg, got, c.want)
		}
	}
}

// (d): v1 ("AAAAMM" por item) e v2 ("mm/aaaa") para a mesma empresa/período agregam juntos;
// ambos persistem data_apuracao em "mm/aaaa".
func TestProcessarDownloadRFB_V1eV2AgregamJuntos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	req1 := insertRFBRequest(t, db, companyID, "tiq-mix-v1", "debito", "producao")
	if err := ProcessarDownloadRFB(db, newFakeRFBDownload(t, debitosJSON("202601", []testDFe{{Chave: "MIX-V1", Valor: 100}})), req1); err != nil {
		t.Fatalf("v1: %v", err)
	}
	waitAsyncSAPSync()

	body := debitosV2JSON([]string{"01/2026"}, map[string][]testDebitoV2{
		"01/2026": {{Chave: "MIX-V2", Registro: "2026-01-15T00:00:00", Apurado: 200, Saldo: 200}},
	})
	req2 := insertRFBRequest(t, db, companyID, "tiq-mix-v2", "debito", "producao")
	if err := ProcessarDownloadRFB(db, newFakeRFBDownload(t, body), req2); err != nil {
		t.Fatalf("v2: %v", err)
	}
	waitAsyncSAPSync()

	var distintos int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT data_apuracao) FROM rfb_debitos WHERE company_id = $1 AND data_apuracao = '01/2026'`, companyID).Scan(&distintos); err != nil || distintos != 1 {
		t.Fatalf("esperava só data_apuracao='01/2026' (v1 normalizado), distintos=%d err=%v", distintos, err)
	}
	var fora int
	db.QueryRow(`SELECT COUNT(*) FROM rfb_debitos WHERE company_id = $1 AND data_apuracao <> '01/2026'`, companyID).Scan(&fora)
	if fora != 0 {
		t.Fatalf("%d linhas com data_apuracao fora de mm/aaaa", fora)
	}
	var nResumo int
	db.QueryRow(`SELECT COUNT(*) FROM rfb_resumo WHERE company_id = $1`, companyID).Scan(&nResumo)
	if nResumo != 1 {
		t.Fatalf("esperava 1 linha de rfb_resumo (mesmo período), veio %d", nResumo)
	}
	td, vt, _, _, corrente, _, _ := readRfbResumo(t, db, companyID, "01/2026")
	if td != 2 || vt != 300 || corrente != 2 {
		t.Fatalf("resumo agregado v1+v2: esperava 2/300/2, veio %d/%.2f/%d", td, vt, corrente)
	}
}

// (i) período inválido num payload v2 não grava linhas/resumo; os demais períodos processam normalmente.
func TestProcessarDownloadRFB_V2_PeriodoInvalidoIgnorado(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	body := debitosV2JSON([]string{"13/2026", "01/2026"}, map[string][]testDebitoV2{
		"13/2026": {{Chave: "INV-A", Apurado: 10, Saldo: 10}},
		"01/2026": {{Chave: "OK-A", Registro: "2026-01-20T08:00:00", Apurado: 100, Saldo: 100}},
	})
	req := insertRFBRequest(t, db, companyID, "tiq-v2-inv", "debito", "producao")
	if err := ProcessarDownloadRFB(db, newFakeRFBDownload(t, body), req); err != nil {
		t.Fatalf("v2: %v", err)
	}
	waitAsyncSAPSync()

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM rfb_debitos WHERE company_id = $1 AND chave_dfe = 'INV-A'`, companyID).Scan(&n)
	if n != 0 {
		t.Fatalf("débito de período inválido não pode ser gravado, veio %d", n)
	}
	db.QueryRow(`SELECT COUNT(*) FROM rfb_resumo WHERE company_id = $1 AND data_apuracao = '13/2026'`, companyID).Scan(&n)
	if n != 0 {
		t.Fatalf("resumo de período inválido não pode ser gravado, veio %d", n)
	}
	if td, vt, _, _, _, _, _ := readRfbResumo(t, db, companyID, "01/2026"); td != 1 || vt != 100 {
		t.Fatalf("01/2026: esperava 1/100, veio %d/%.2f", td, vt)
	}
}

// (iii) v1 seguido de v2 sobre a mesma chave preserva situacao/formas_extincao/eventos do v1.
func TestProcessarDownloadRFB_V2NaoApagaCamposRicosDoV1(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	insertRFBCredential(t, db, companyID, "producao")

	v1 := `{"apuracaoCorrente":{"debitos":[{"chaveDfe":"KEEP-1","dataApuracao":"202601","valorCBSTotal":100,"valorCBSExtinto":0,"valorCBSNaoExtinto":100,` +
		`"situacao":"ATIVO","formasExtincao":[{"forma":"PAGAMENTO"}],"eventos":[{"tipo":"X"}]}]}}`
	req1 := insertRFBRequest(t, db, companyID, "tiq-keep-1", "debito", "producao")
	if err := ProcessarDownloadRFB(db, newFakeRFBDownload(t, v1), req1); err != nil {
		t.Fatalf("v1: %v", err)
	}
	waitAsyncSAPSync()

	v2 := debitosV2JSON([]string{"01/2026"}, map[string][]testDebitoV2{
		"01/2026": {{Chave: "KEEP-1", Registro: "2026-01-20T08:00:00", Apurado: 100, Extinto: 40, Saldo: 60}},
	})
	req2 := insertRFBRequest(t, db, companyID, "tiq-keep-2", "debito", "producao")
	if err := ProcessarDownloadRFB(db, newFakeRFBDownload(t, v2), req2); err != nil {
		t.Fatalf("v2: %v", err)
	}
	waitAsyncSAPSync()

	var sit sql.NullString
	var formas, eventos sql.NullString
	var extinto float64
	if err := db.QueryRow(`SELECT situacao_debito, formas_extincao::text, eventos::text, valor_cbs_extinto
		FROM rfb_debitos WHERE company_id = $1 AND chave_dfe = 'KEEP-1'`, companyID).Scan(&sit, &formas, &eventos, &extinto); err != nil {
		t.Fatal(err)
	}
	if sit.String != "ATIVO" || !strings.Contains(formas.String, "PAGAMENTO") || !strings.Contains(eventos.String, `"X"`) {
		t.Fatalf("campos ricos do v1 devem ser preservados: situacao=%q formas=%q eventos=%q", sit.String, formas.String, eventos.String)
	}
	if extinto != 40 {
		t.Fatalf("valores monetários devem vir do v2 (extinto=40), veio %.2f", extinto)
	}
}
