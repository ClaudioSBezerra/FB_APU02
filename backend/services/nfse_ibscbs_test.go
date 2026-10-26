package services

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// spec-nfse-ibscbs-xml.md — fixture: XML REAL do leiaute 1.01 (nota 30 da Fortes Bezerra,
// 01/10/2026, sem assinatura, e-mails/telefone anonimizados) em testdata/.
// ---------------------------------------------------------------------------

const nfseNota30Chave = "52014051238149716000128000000000003026101790888035"

func lerNota30Teste(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "nfse_nota30_layout101.xml"))
	if err != nil {
		t.Fatalf("ler fixture: %v", err)
	}
	return b
}

// ptrF/ptrS: valor de um ponteiro ou nil (para comparar sem depender de endereço).
func ptrF(p *float64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func ptrS(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// camposIBSCBSTeste lista os campos IBS/CBS em ordem fixa (nome → valor desreferenciado).
func camposIBSCBSTeste(g NFSeIBSCBSDados) [][2]interface{} {
	return [][2]interface{}{
		{"cod_localidade_incid", ptrS(g.CodLocalidadeIncid)}, {"base", ptrF(g.Base)},
		{"ibs_uf_aliquota", ptrF(g.IBSUFAliquota)}, {"ibs_uf_aliquota_efetiva", ptrF(g.IBSUFAliquotaEfetiva)},
		{"ibs_mun_aliquota", ptrF(g.IBSMunAliquota)}, {"ibs_mun_aliquota_efetiva", ptrF(g.IBSMunAliquotaEfetiva)},
		{"cbs_aliquota", ptrF(g.CBSAliquota)}, {"cbs_aliquota_efetiva", ptrF(g.CBSAliquotaEfetiva)},
		{"valor_total_nf", ptrF(g.ValorTotalNF)}, {"ibs_total", ptrF(g.IBSTotal)},
		{"ibs_uf", ptrF(g.IBSUF)}, {"ibs_mun", ptrF(g.IBSMun)}, {"cbs", ptrF(g.CBS)},
		{"fin_nfse", ptrS(g.FinNFSe)}, {"ind_op", ptrS(g.IndOp)}, {"ind_dest", ptrS(g.IndDest)},
		{"cst", ptrS(g.CST)}, {"cclass_trib", ptrS(g.CClassTrib)},
	}
}

// nota30IBSCBSEsperado: valores conferidos manualmente no XML real.
var nota30IBSCBSEsperado = []interface{}{
	"2611606", 23280.0,
	0.10, 0.10,
	0.0, 0.0,
	0.90, 0.90,
	24000.0, 23.28,
	23.28, 0.0, 209.52,
	"0", "100301", "0",
	"000", "000001",
}

func conferirIBSCBSTeste(t *testing.T, contexto string, g NFSeIBSCBSDados) {
	t.Helper()
	for i, c := range camposIBSCBSTeste(g) {
		if c[1] != nota30IBSCBSEsperado[i] {
			t.Errorf("%s: %s = %v, esperado %v", contexto, c[0], c[1], nota30IBSCBSEsperado[i])
		}
	}
}

func TestNFSeExtrairDadosNFSeXML_Nota30Layout101_IBSCBSEISS(t *testing.T) {
	d, invalidos, err := extrairDadosNFSeXMLDetalhado(lerNota30Teste(t))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(invalidos) != 0 {
		t.Errorf("nenhum campo deveria ser inválido, veio %q", invalidos)
	}
	if d.BaseISS != 24000 || d.AliquotaISS != 3 || d.ISS != 720 || d.ValorServicos != 24000 {
		t.Errorf("ISS: base=%v aliquota=%v iss=%v vServ=%v (esperava 24000/3/720/24000)", d.BaseISS, d.AliquotaISS, d.ISS, d.ValorServicos)
	}
	if d.NumeroNFSe != "30" || d.CNPJPrestador != "38149716000128" || d.CNPJTomador != "10230480001960" || d.MesAno != "10/2026" {
		t.Errorf("identificação: %+v", d)
	}
	if strings.TrimPrefix(d.IDInfNFSe, "NFS") != nfseNota30Chave {
		t.Errorf("Id = %q", d.IDInfNFSe)
	}
	conferirIBSCBSTeste(t, "nota 30", d.IBSCBS)
}

func TestNFSeExtrairDadosNFSeXML_SemIBSCBS_Nil(t *testing.T) {
	d, err := extrairDadosNFSeXML([]byte(nfseXMLTeste(nfseXMLCompletoTeste())))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	for _, c := range camposIBSCBSTeste(d.IBSCBS) {
		if c[1] != nil {
			t.Errorf("sem grupo IBSCBS, %s deveria ser nil (NULL), veio %v", c[0], c[1])
		}
	}
	if d.BaseISS != 10000 || d.AliquotaISS != 5 {
		t.Errorf("ISS preenchido mesmo sem IBSCBS: base=%v aliquota=%v", d.BaseISS, d.AliquotaISS)
	}
}

func TestNFSeExtrairDadosNFSeXML_IBSCBSValorInvalido_NilEContado(t *testing.T) {
	xml := strings.Replace(string(lerNota30Teste(t)), "<vCBS>209.52</vCBS>", "<vCBS>abc</vCBS>", 1)
	xml = strings.Replace(xml, "<pIBSUF>0.10</pIBSUF>", "<pIBSUF>0,10</pIBSUF>", 1) // vírgula aceita
	xml = strings.Replace(xml, "<pCBS>0.90</pCBS>", "<pCBS>123456.00</pCBS>", 1)    // fora de NUMERIC(7,4)
	xml = strings.Replace(xml, "<vIBSMun>0.00</vIBSMun>", "<vIBSMun></vIBSMun>", 1) // vazio = NULL, não inválido
	d, invalidos, err := extrairDadosNFSeXMLDetalhado([]byte(xml))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if d.IBSCBS.CBS != nil || d.IBSCBS.CBSAliquota != nil || d.IBSCBS.IBSMun != nil {
		t.Errorf("vCBS/pCBS inválidos e vIBSMun vazio deveriam ser nil: cbs=%v pcbs=%v vibsmun=%v",
			ptrF(d.IBSCBS.CBS), ptrF(d.IBSCBS.CBSAliquota), ptrF(d.IBSCBS.IBSMun))
	}
	if ptrF(d.IBSCBS.IBSUFAliquota) != 0.10 || ptrF(d.IBSCBS.IBSTotal) != 23.28 {
		t.Errorf("demais campos deveriam seguir: pIBSUF=%v vIBSTot=%v", ptrF(d.IBSCBS.IBSUFAliquota), ptrF(d.IBSCBS.IBSTotal))
	}
	if strings.Join(invalidos, ",") != "pCBS,vCBS" {
		t.Errorf("invalidos = %q, esperava [pCBS vCBS]", invalidos)
	}
}

// ---------------------------------------------------------------------------
// Banco: captura e reprocessamento
// ---------------------------------------------------------------------------

type linhaIBSCBSTeste struct {
	baseISS, aliquotaISS float64
	g                    NFSeIBSCBSDados
	versao               int
	cancelada            bool
	dataCancelamento     sql.NullTime
	nsu                  sql.NullInt64
}

func lerLinhaIBSCBSTeste(t *testing.T, db *sql.DB, tabela, companyID, chave string) linhaIBSCBSTeste {
	t.Helper()
	var l linhaIBSCBSTeste
	var (
		loc, fin, indOp, indDest, cst, cClass                                     sql.NullString
		base, pUF, pUFEf, pMun, pMunEf, pCBS, pCBSEf, vTot, vIBS, vUF, vMun, vCBS sql.NullFloat64
	)
	err := db.QueryRow(`SELECT base_iss, aliquota_iss, ibscbs_cod_localidade_incid, ibscbs_base,
		ibs_uf_aliquota, ibs_uf_aliquota_efetiva, ibs_mun_aliquota, ibs_mun_aliquota_efetiva, cbs_aliquota, cbs_aliquota_efetiva,
		ibscbs_valor_total_nf, ibs_total, ibs_uf, ibs_mun, cbs,
		ibscbs_fin_nfse, ibscbs_ind_op, ibscbs_ind_dest, ibscbs_cst, ibscbs_cclass_trib,
		xml_parser_versao, cancelada, data_cancelamento, nsu
		FROM `+tabela+` WHERE company_id = $1 AND identificador_nfse = $2`, companyID, chave).Scan(
		&l.baseISS, &l.aliquotaISS, &loc, &base, &pUF, &pUFEf, &pMun, &pMunEf, &pCBS, &pCBSEf,
		&vTot, &vIBS, &vUF, &vMun, &vCBS, &fin, &indOp, &indDest, &cst, &cClass,
		&l.versao, &l.cancelada, &l.dataCancelamento, &l.nsu)
	if err != nil {
		t.Fatalf("ler %s/%s: %v", tabela, chave, err)
	}
	f := func(v sql.NullFloat64) *float64 {
		if !v.Valid {
			return nil
		}
		x := v.Float64
		return &x
	}
	s := func(v sql.NullString) *string {
		if !v.Valid {
			return nil
		}
		x := v.String
		return &x
	}
	l.g = NFSeIBSCBSDados{
		CodLocalidadeIncid: s(loc), Base: f(base),
		IBSUFAliquota: f(pUF), IBSUFAliquotaEfetiva: f(pUFEf), IBSMunAliquota: f(pMun), IBSMunAliquotaEfetiva: f(pMunEf),
		CBSAliquota: f(pCBS), CBSAliquotaEfetiva: f(pCBSEf),
		ValorTotalNF: f(vTot), IBSTotal: f(vIBS), IBSUF: f(vUF), IBSMun: f(vMun), CBS: f(vCBS),
		FinNFSe: s(fin), IndOp: s(indOp), IndDest: s(indDest), CST: s(cst), CClassTrib: s(cClass),
	}
	return l
}

// Nota nova capturada já sai com IBS/CBS, base/alíquota ISS e xml_parser_versao = 2.
func TestExecutarCicloNFSe_Nota30_GravaIBSCBSEVersao(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	// Certificado da prestadora (raiz 38149716) → a nota 30 é prestada (nfse_saidas).
	fila := &filaADNTeste{t: t, tamanhoLote: 50, docs: []map[string]interface{}{{
		"NSU": int64(30), "ChaveAcesso": nfseNota30Chave, "TipoDocumento": "NFSE", "TipoEvento": nil,
		"ArquivoXml": gzipB64Teste(t, lerNota30Teste(t)), "DataHoraGeracao": "2026-10-01T17:53:54-03:00",
	}}}
	env := novoADNTesteMTLS(t, fila, "38149716000128")
	writeNFSeCAPEMTeste(t, env.ca)
	t.Setenv("NFSE_ADN_BASE_URL", env.ts.URL)
	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCertificadoTeste(t, db, companyID, env.pfxB64, "senha-correta", "empresa.pfx", &futuro, "ativo")
	t.Cleanup(func() { db.Exec(`DELETE FROM nfse_certificados WHERE company_id = $1`, companyID) })
	semEsperasNoProcessor(t)

	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	if err != nil {
		t.Fatalf("ciclo: %v", err)
	}
	if res.Novos != 1 || res.CamposInvalidos != 0 {
		t.Fatalf("novos=%d campos_invalidos=%d (esperava 1/0)", res.Novos, res.CamposInvalidos)
	}
	l := lerLinhaIBSCBSTeste(t, db, "nfse_saidas", companyID, nfseNota30Chave)
	if l.versao != NFSeXMLParserVersao || l.baseISS != 24000 || l.aliquotaISS != 3 {
		t.Errorf("versao=%d base_iss=%v aliquota_iss=%v", l.versao, l.baseISS, l.aliquotaISS)
	}
	conferirIBSCBSTeste(t, "nfse_saidas", l.g)
}

// Linha gravada por parser antigo (versão 0, campos IBS/CBS NULL, cancelada) é reextraída do
// XML: ganha IBS/CBS e versão 2, mantém cancelada/data_cancelamento/nsu. XML ilegível: logada e
// marcada versão 2 (não repete). Sem XML: intocada.
func TestReprocessarXMLNFSe_PreservaCanceladaEMarcaVersao(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	xmlB64 := gzipB64Teste(t, lerNota30Teste(t))
	if _, err := db.Exec(`INSERT INTO nfse_saidas (company_id, identificador_nfse, cnpj_prestador, cnpj_tomador, valor_servicos, iss,
			xml_gzip_b64, nsu, numero_nfse, cancelada, data_cancelamento, xml_parser_versao)
		VALUES ($1, $2, '38149716000128', '10230480001960', 24000, 720, $3, 30, '30', true, '2026-10-05T10:00:00-03:00', 0),
		       ($1, 'reproc-xml-quebrado', NULL, NULL, 1, 0, $4, 31, NULL, false, NULL, 0),
		       ($1, 'reproc-sem-xml', NULL, NULL, 2, 0, NULL, NULL, NULL, false, NULL, 0)`,
		companyID, nfseNota30Chave, xmlB64, gzipB64Teste(t, []byte("<NFSe><infNFSe>quebrado"))); err != nil {
		t.Fatalf("inserir linhas antigas: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO nfse_entradas (company_id, identificador_nfse, xml_gzip_b64, nsu, xml_parser_versao)
		VALUES ($1, $2, $3, 30, 1)`, companyID, nfseNota30Chave, xmlB64); err != nil {
		t.Fatalf("inserir entrada antiga: %v", err)
	}

	res, err := ReprocessarXMLNFSe(db)
	if err != nil {
		t.Fatalf("reprocessar: %v", err)
	}
	if res.Atualizadas < 2 || res.Falhas < 1 {
		t.Errorf("atualizadas=%d falhas=%d (esperava ≥2/≥1)", res.Atualizadas, res.Falhas)
	}

	l := lerLinhaIBSCBSTeste(t, db, "nfse_saidas", companyID, nfseNota30Chave)
	if l.versao != NFSeXMLParserVersao || !l.cancelada || !l.dataCancelamento.Valid || l.nsu.Int64 != 30 {
		t.Errorf("saída reprocessada: versao=%d cancelada=%t data_cancelamento=%v nsu=%v", l.versao, l.cancelada, l.dataCancelamento, l.nsu)
	}
	if l.baseISS != 24000 || l.aliquotaISS != 3 {
		t.Errorf("ISS: base=%v aliquota=%v", l.baseISS, l.aliquotaISS)
	}
	conferirIBSCBSTeste(t, "saída reprocessada", l.g)
	conferirIBSCBSTeste(t, "entrada reprocessada", lerLinhaIBSCBSTeste(t, db, "nfse_entradas", companyID, nfseNota30Chave).g)

	q := lerLinhaIBSCBSTeste(t, db, "nfse_saidas", companyID, "reproc-xml-quebrado")
	if q.versao != NFSeXMLParserVersao || q.nsu.Int64 != 31 {
		t.Errorf("XML quebrado deveria ser marcado v%d sem perder o NSU: versao=%d nsu=%v", NFSeXMLParserVersao, q.versao, q.nsu)
	}
	if v := lerLinhaIBSCBSTeste(t, db, "nfse_saidas", companyID, "reproc-sem-xml").versao; v != 0 {
		t.Errorf("linha sem XML não deveria ser tocada: versao=%d", v)
	}

	// Segunda execução: nada pendente desta empresa (não repete em loop).
	var pendentes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nfse_saidas WHERE company_id = $1 AND xml_parser_versao < $2 AND xml_gzip_b64 IS NOT NULL`,
		companyID, NFSeXMLParserVersao).Scan(&pendentes); err != nil || pendentes != 0 {
		t.Errorf("pendentes após reprocessar = %d (err %v)", pendentes, err)
	}
}
