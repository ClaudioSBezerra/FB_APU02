package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fb_apu02/crypto"
)

// ---------------------------------------------------------------------------
// Helpers de DB/certificado específicos de ExecutarCicloNFSe. Os helpers de certificado mTLS
// (genTestCA, genTestLeaf, buildTestPfxBase64, startNFSeADNTestServer, leafOpts) já existem em
// nfse_adn_test.go, mesmo pacote `services` — reaproveitados aqui sem duplicação.
// ---------------------------------------------------------------------------

// openNFSeTestDB abre conexão com o Postgres de teste (reaproveita openTestDB, definido em
// sap_sync_trigger_test.go) e pula o teste se a migration 133 ainda não tiver sido aplicada
// nesse banco — mesmo padrão de handlers/nfse_credentials_test.go.
func openNFSeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'nfse_certificados')`).Scan(&exists); err != nil || !exists {
		db.Close()
		t.Skip("tabela nfse_certificados não existe — rode as migrations (133) antes deste teste")
	}
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'nfse_certificados' AND column_name = 'ultimo_nsu')`).Scan(&exists); err != nil || !exists {
		db.Close()
		t.Skip("coluna nfse_certificados.ultimo_nsu não existe — rode a migration 134 antes deste teste")
	}
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'nfse_adn_falhas')`).Scan(&exists); err != nil || !exists {
		db.Close()
		t.Skip("tabela nfse_adn_falhas não existe — rode a versão atual da migration 134 antes deste teste")
	}
	return db
}

// insertNFSeCertificadoTeste insere uma linha nfse_certificados de teste com os campos
// sensíveis já criptografados (mesmo formato gravado por SaveNFSeCertificadoHandler).
func insertNFSeCertificadoTeste(t *testing.T, db *sql.DB, companyID, pfxPlain, senhaPlain, nomeArquivo string, validade *time.Time, status string) {
	t.Helper()
	encPfx, err := crypto.EncryptField(pfxPlain)
	if err != nil {
		t.Fatalf("falha ao criptografar pfx de teste: %v", err)
	}
	encSenha, err := crypto.EncryptField(senhaPlain)
	if err != nil {
		t.Fatalf("falha ao criptografar senha de teste: %v", err)
	}
	var validadeParam interface{}
	if validade != nil {
		validadeParam = *validade
	}
	if _, err := db.Exec(`
		INSERT INTO nfse_certificados (company_id, certificado_pfx, certificado_senha, nome_arquivo, validade, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, companyID, encPfx, encSenha, nomeArquivo, validadeParam, status); err != nil {
		t.Fatalf("falha ao inserir nfse_certificados de teste: %v", err)
	}
}

// writeNFSeCAPEMTeste grava o certificado da CA de teste num arquivo PEM temporário e aponta
// NFSE_ADN_TLS_CA_FILE pra ele — é assim (ver comentário da constante em nfse_adn.go) que o
// client mTLS montado DENTRO de ExecutarCicloNFSe passa a confiar no certificado autoassinado
// do servidor de teste, sem alterar nenhum código de produção.
func writeNFSeCAPEMTeste(t *testing.T, ca *x509.Certificate) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca-teste-nfse-processor.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("falha ao gravar PEM da CA de teste: %v", err)
	}
	t.Setenv("NFSE_ADN_TLS_CA_FILE", path)
}

func countNFSeRowsTeste(t *testing.T, db *sql.DB, tabela, companyID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+tabela+" WHERE company_id = $1", companyID).Scan(&count); err != nil {
		t.Fatalf("falha ao contar linhas de %s: %v", tabela, err)
	}
	return count
}

// executarCicloTeste chama ExecutarCicloNFSe com as opções padrão e devolve as contagens mais
// usadas pelos testes.
func executarCicloTeste(db *sql.DB, companyID string) (recebidos, novos, ignorados int, err error) {
	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	return res.Recebidos, res.Novos, res.Ignorados, err
}

// ---------------------------------------------------------------------------
// ExecutarCicloNFSe — certificado ausente/vencido (sem chamada de rede)
// ---------------------------------------------------------------------------

func TestExecutarCicloNFSe_SemCertificado_RetornaErroCertificadoNaoCadastrado(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	t.Setenv("NFSE_ADN_BASE_URL", "http://127.0.0.1:1")

	_, _, _, err := executarCicloTeste(db, companyID)
	if !errors.Is(err, ErrNFSeCertificadoNaoCadastrado) {
		t.Fatalf("esperava ErrNFSeCertificadoNaoCadastrado, veio: %v", err)
	}
}

func TestExecutarCicloNFSe_CertificadoVencido_RetornaErroCertificadoIndisponivel(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	defer db.Exec(`DELETE FROM nfse_certificados WHERE company_id = $1`, companyID)

	t.Setenv("NFSE_ADN_BASE_URL", "http://127.0.0.1:1")

	passado := time.Now().AddDate(0, 0, -5)
	insertNFSeCertificadoTeste(t, db, companyID, "conteudo-fake-de-certificado", "senha-teste", "vencido.pfx", &passado, "ativo")

	_, _, _, err := executarCicloTeste(db, companyID)
	if !errors.Is(err, ErrNFSeCertificadoIndisponivel) {
		t.Fatalf("esperava ErrNFSeCertificadoIndisponivel, veio: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExecutarCicloNFSe — ciclo completo contra servidor mTLS de teste
// ---------------------------------------------------------------------------

// TestExecutarCicloNFSe_ServidorRejeitaCertificado_RetornaErroAutenticacaoSemGravarNada cobre
// falha de mTLS: o servidor de teste rejeita o certificado da empresa (handshake falha) ->
// ExecutarCicloNFSe devolve um erro que satisfaz errors.Is(err, ErrNFSeADNAutenticacaoFalhou),
// sem gravar nada no banco.
func TestExecutarCicloNFSe_ServidorRejeitaCertificado_RetornaErroAutenticacaoSemGravarNada(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	defer db.Exec(`DELETE FROM nfse_certificados WHERE company_id = $1`, companyID)

	serverCA, serverCAKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, serverCA, serverCAKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}

	// CA que o servidor confia para certificados de CLIENTE — DIFERENTE da que assina o
	// certificado da empresa abaixo, então o handshake mTLS tem que falhar.
	trustedClientCA, _ := genTestCA(t)
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(trustedClientCA)

	untrustedCA, untrustedCAKey := genTestCA(t)
	clientCert, clientKey := genTestLeaf(t, untrustedCA, untrustedCAKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})

	ts := startNFSeADNTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handshake deveria ter falhado antes de qualquer requisição chegar ao handler do servidor de teste")
	}), serverTLSCert, clientCAPool)
	defer ts.Close()

	writeNFSeCAPEMTeste(t, serverCA)
	t.Setenv("NFSE_ADN_BASE_URL", ts.URL)

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCertificadoTeste(t, db, companyID, pfxB64, "senha-correta", "empresa.pfx", &futuro, "ativo")

	recebidos, novos, ignorados, err := executarCicloTeste(db, companyID)
	if !errors.Is(err, ErrNFSeADNAutenticacaoFalhou) {
		t.Fatalf("esperava erro ErrNFSeADNAutenticacaoFalhou, veio: %v", err)
	}
	if recebidos != 0 || novos != 0 || ignorados != 0 {
		t.Errorf("certificado rejeitado não deveria gravar/ignorar/contar nada, veio recebidos=%d novos=%d ignorados=%d", recebidos, novos, ignorados)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_entradas", companyID); n != 0 {
		t.Errorf("certificado rejeitado não deveria gravar nada em nfse_entradas, veio %d linha(s)", n)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_saidas", companyID); n != 0 {
		t.Errorf("certificado rejeitado não deveria gravar nada em nfse_saidas, veio %d linha(s)", n)
	}
}

// cnpjEmpresaTeste é o CNPJ do certificado de teste (raiz 12345678).
const cnpjEmpresaTeste = "12345678000199"

// prepararCicloTeste sobe o servidor mTLS de teste com handler, aponta o processor pra ele e
// cadastra o certificado ativo da empresa. Esperas (pausa entre lotes/backoff) zeradas.
func prepararCicloTeste(t *testing.T, db *sql.DB, companyID string, handler http.Handler) *esperasTeste {
	t.Helper()
	env := novoADNTesteMTLS(t, handler, cnpjEmpresaTeste)
	writeNFSeCAPEMTeste(t, env.ca)
	t.Setenv("NFSE_ADN_BASE_URL", env.ts.URL)
	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCertificadoTeste(t, db, companyID, env.pfxB64, "senha-correta", "empresa.pfx", &futuro, "ativo")
	t.Cleanup(func() { db.Exec(`DELETE FROM nfse_certificados WHERE company_id = $1`, companyID) })
	return semEsperasNoProcessor(t)
}

func ultimoNSUTeste(t *testing.T, db *sql.DB, companyID string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT ultimo_nsu FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&n); err != nil {
		t.Fatalf("ler ultimo_nsu: %v", err)
	}
	return n
}

// nfseTomadaTeste: NFS-e em que a empresa é tomadora (prestador terceiro).
func nfseTomadaTeste(numero string) nfseXMLOpts {
	return nfseXMLOpts{
		NNFSe: numero, EmitCNPJ: "99888777000166", PrestCNPJ: "99888777000166", TomaCNPJ: cnpjEmpresaTeste,
		DhEmi: "2026-10-01T10:00:00-03:00", DCompet: "2026-10-01", VServ: "100.00", VISSQN: "5.00", TpRetISSQN: "1",
	}
}

// AC1 + AC2: 120 NFS-e → 3 lotes, ultimo_nsu = 120; a 2ª execução começa em /DFe/120?lote=true.
func TestExecutarCicloNFSe_120Documentos_CursorPersistidoERetomada(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	fila := &filaADNTeste{t: t, tamanhoLote: 50}
	for i := int64(1); i <= 120; i++ {
		fila.docs = append(fila.docs, itemNFSeTeste(t, i, chaveTeste("7", i), nfseTomadaTeste(fmt.Sprint(i))))
	}
	prepararCicloTeste(t, db, companyID, fila)

	recebidos, novos, ignorados, err := executarCicloTeste(db, companyID)
	if err != nil {
		t.Fatalf("1º ciclo: %v", err)
	}
	if recebidos != 120 || novos != 120 || ignorados != 0 {
		t.Errorf("1º ciclo: recebidos=%d novos=%d ignorados=%d", recebidos, novos, ignorados)
	}
	if n := ultimoNSUTeste(t, db, companyID); n != 120 {
		t.Errorf("ultimo_nsu = %d, esperava 120", n)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_entradas", companyID); n != 120 {
		t.Errorf("nfse_entradas = %d linhas, esperava 120", n)
	}
	if p := fila.Pedidos(); len(p) != 4 {
		t.Errorf("1º ciclo: esperava 4 pedidos (3 lotes + 404), veio %v", p)
	}

	// 2º ciclo: começa do cursor salvo — nada reprocessado.
	antes := len(fila.Pedidos())
	recebidos2, novos2, _, err := executarCicloTeste(db, companyID)
	if err != nil {
		t.Fatalf("2º ciclo: %v", err)
	}
	p := fila.Pedidos()[antes:]
	if len(p) != 1 || p[0] != "/contribuintes/DFe/120?lote=true" {
		t.Errorf("2º ciclo deveria começar (e terminar) em /contribuintes/DFe/120?lote=true, pedidos=%v", p)
	}
	if recebidos2 != 0 || novos2 != 0 {
		t.Errorf("2º ciclo: recebidos=%d novos=%d", recebidos2, novos2)
	}
}

func TestExecutarCicloNFSe_FilaVazia_ZeroRecebidosSucesso(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	prepararCicloTeste(t, db, companyID, &filaADNTeste{t: t, tamanhoLote: 50})

	recebidos, novos, ignorados, err := executarCicloTeste(db, companyID)
	if err != nil || recebidos != 0 || novos != 0 || ignorados != 0 {
		t.Fatalf("fila vazia: %d/%d/%d err=%v", recebidos, novos, ignorados, err)
	}
	if n := ultimoNSUTeste(t, db, companyID); n != 0 {
		t.Errorf("ultimo_nsu = %d, esperava 0", n)
	}
}

type linhaNFSeTeste struct {
	cnpjPrestador, cnpjTomador, dataEmissao, mesAno, numero, xml string
	nsu                                                          int64
	valor, iss, issRetido, pis, cofins, irrf, csll, inss         float64
	cancelada                                                    bool
	dataCancelamento                                             sql.NullTime
}

func lerLinhaNFSeTeste(t *testing.T, db *sql.DB, tabela, companyID, chave string) (linhaNFSeTeste, bool) {
	t.Helper()
	var l linhaNFSeTeste
	err := db.QueryRow(`SELECT COALESCE(cnpj_prestador,''), COALESCE(cnpj_tomador,''), COALESCE(TO_CHAR(data_emissao,'YYYY-MM-DD'),''),
		COALESCE(mes_ano,''), COALESCE(numero_nfse,''), COALESCE(xml_gzip_b64,''), COALESCE(nsu,0),
		valor_servicos, iss, iss_retido, pis, cofins, irrf, csll, inss, cancelada, data_cancelamento
		FROM `+tabela+` WHERE company_id = $1 AND identificador_nfse = $2`, companyID, chave).Scan(
		&l.cnpjPrestador, &l.cnpjTomador, &l.dataEmissao, &l.mesAno, &l.numero, &l.xml, &l.nsu,
		&l.valor, &l.iss, &l.issRetido, &l.pis, &l.cofins, &l.irrf, &l.csll, &l.inss, &l.cancelada, &l.dataCancelamento)
	if err == sql.ErrNoRows {
		return l, false
	}
	if err != nil {
		t.Fatalf("ler %s/%s: %v", tabela, chave, err)
	}
	return l, true
}

// Direção por raiz do CNPJ + extração de cada campo fiscal (AC3) + documentos ignorados (XML
// inválido, zip-bomb, empresa não é parte, tipos não tratados) sem derrubar o lote.
func TestExecutarCicloNFSe_DirecaoCamposFiscaisEIgnorados(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	tomada := nfseXMLOpts{
		NNFSe: "1261", EmitCNPJ: "99888777000166", PrestCNPJ: "99888777000166", TomaCNPJ: cnpjEmpresaTeste,
		DhEmi: "2026-09-30T23:30:00-03:00", DCompet: "2026-09-01", VServ: "10000.00", VISSQN: "500.00", TpRetISSQN: "2",
		VPis: "65.00", VCofins: "300.00", VRetCP: "1100.00", VRetIRRF: "150.00", VRetCSLL: "100.00",
	}
	prestada := nfseXMLOpts{NNFSe: "77", EmitCNPJ: cnpjEmpresaTeste, PrestCNPJ: cnpjEmpresaTeste, TomaCNPJ: "55444333000122",
		DhEmi: "2026-10-02T09:00:00-03:00", DCompet: "2026-10-01", VServ: "200.00", VISSQN: "10.00", TpRetISSQN: "1"}
	// Filial (mesma raiz 12345678, outro CNPJ) presta para a matriz → empresa nos dois lados.
	ambas := nfseXMLOpts{NNFSe: "78", EmitCNPJ: "12345678000280", PrestCNPJ: "12345678000280", TomaCNPJ: cnpjEmpresaTeste,
		DhEmi: "2026-10-03T09:00:00-03:00", DCompet: "2026-10-01", VServ: "300.00"}
	// Tomada de filial (toma = filial) prestada por pessoa física (CPF).
	tomadaFilialPF := nfseXMLOpts{NNFSe: "5", PrestCPF: "98765432100", TomaCNPJ: "12345678000370",
		DhEmi: "2026-10-04T09:00:00-03:00", DCompet: "2026-10-01", VServ: "400.00"}
	nenhuma := nfseXMLOpts{NNFSe: "9", EmitCNPJ: "99888777000166", PrestCNPJ: "99888777000166", TomaCNPJ: "55444333000122", VServ: "1.00"}

	var bomba bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&bomba, gzip.BestCompression)
	zw.Write(bytes.Repeat([]byte{' '}, 8<<20))
	zw.Close()

	itemTomada := itemNFSeTeste(t, 1, chaveTeste("1", 1), tomada)
	fila := &filaADNTeste{t: t, tamanhoLote: 50, docs: []map[string]interface{}{
		itemTomada,
		itemNFSeTeste(t, 2, chaveTeste("1", 2), prestada),
		itemNFSeTeste(t, 3, chaveTeste("1", 3), ambas),
		itemNFSeTeste(t, 4, chaveTeste("1", 4), tomadaFilialPF),
		itemNFSeTeste(t, 5, chaveTeste("1", 5), nenhuma),
		{"NSU": int64(6), "ChaveAcesso": chaveTeste("1", 6), "TipoDocumento": "NFSE", "ArquivoXml": gzipB64Teste(t, []byte("<NFSe><infNFSe>quebrado"))},
		{"NSU": int64(7), "ChaveAcesso": chaveTeste("1", 7), "TipoDocumento": "NFSE", "ArquivoXml": base64.StdEncoding.EncodeToString(bomba.Bytes())},
		{"NSU": int64(8), "ChaveAcesso": chaveTeste("1", 8), "TipoDocumento": "DPS", "ArquivoXml": "eA=="},
		itemEventoTeste(9, chaveTeste("1", 1), "CONFIRMACAO_TOMADOR", "2026-10-05T10:00:00-03:00"),
		itemNFSeTeste(t, 10, chaveTeste("1", 10), nfseTomadaTeste("10")),
	}}
	prepararCicloTeste(t, db, companyID, fila)

	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// novos: tomada(1) + prestada(1) + ambas(2) + tomadaFilialPF(1) + nsu10(1) = 6
	// ignorados: nenhuma, DPS, evento não-cancelamento = 3; falhas (quarentena): XML inválido, zip-bomb = 2
	if res.Recebidos != 10 || res.Novos != 6 || res.Ignorados != 3 || res.Falhas != 2 {
		t.Errorf("recebidos=%d novos=%d ignorados=%d falhas=%d (esperava 10/6/3/2)", res.Recebidos, res.Novos, res.Ignorados, res.Falhas)
	}
	// Quarentena: NSU 6 e 7 com o ArquivoXml exatamente como recebido.
	for _, item := range []map[string]interface{}{fila.docs[5], fila.docs[6]} {
		nsu := item["NSU"].(int64)
		var arquivo, motivo string
		if err := db.QueryRow(`SELECT COALESCE(arquivo_xml,''), motivo FROM nfse_adn_falhas WHERE company_id = $1 AND nsu = $2`, companyID, nsu).Scan(&arquivo, &motivo); err != nil {
			t.Errorf("NSU %d deveria estar em nfse_adn_falhas: %v", nsu, err)
			continue
		}
		if arquivo != item["ArquivoXml"].(string) || motivo == "" {
			t.Errorf("NSU %d em quarentena divergiu: motivo=%q arquivo_igual=%t", nsu, motivo, arquivo == item["ArquivoXml"].(string))
		}
	}
	if n := ultimoNSUTeste(t, db, companyID); n != 10 {
		t.Errorf("ultimo_nsu = %d, esperava 10", n)
	}

	// AC3: todos os campos fiscais da tomada.
	l, ok := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chaveTeste("1", 1))
	if !ok {
		t.Fatal("NFS-e tomada não gravada em nfse_entradas")
	}
	want := linhaNFSeTeste{cnpjPrestador: "99888777000166", cnpjTomador: cnpjEmpresaTeste, dataEmissao: "2026-09-30", mesAno: "09/2026",
		numero: "1261", xml: itemTomada["ArquivoXml"].(string), nsu: 1,
		valor: 10000, iss: 500, issRetido: 500, pis: 65, cofins: 300, irrf: 150, csll: 100, inss: 1100}
	if l != want {
		t.Errorf("colunas da tomada:\n veio %+v\nesperado %+v", l, want)
	}
	if _, ok := lerLinhaNFSeTeste(t, db, "nfse_saidas", companyID, chaveTeste("1", 1)); ok {
		t.Error("tomada não deveria estar em nfse_saidas")
	}

	// Prestada → só saídas.
	if _, ok := lerLinhaNFSeTeste(t, db, "nfse_saidas", companyID, chaveTeste("1", 2)); !ok {
		t.Error("prestada deveria estar em nfse_saidas")
	}
	if _, ok := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chaveTeste("1", 2)); ok {
		t.Error("prestada não deveria estar em nfse_entradas")
	}
	// Ambas (filial ↔ matriz, mesma raiz) → duas tabelas.
	_, okE := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chaveTeste("1", 3))
	_, okS := lerLinhaNFSeTeste(t, db, "nfse_saidas", companyID, chaveTeste("1", 3))
	if !okE || !okS {
		t.Errorf("mesma raiz nos dois lados deveria gravar nas duas tabelas (entradas=%t saídas=%t)", okE, okS)
	}
	// Tomada pela filial, prestador PF → entradas, cnpj_prestador = CPF.
	if l, ok := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chaveTeste("1", 4)); !ok || l.cnpjPrestador != "98765432100" || l.cnpjTomador != "12345678000370" {
		t.Errorf("tomada da filial: ok=%t linha=%+v", ok, l)
	}
	// Ignorados não gravados.
	for _, n := range []int64{5, 6, 7, 8} {
		for _, tab := range []string{"nfse_entradas", "nfse_saidas"} {
			if _, ok := lerLinhaNFSeTeste(t, db, tab, companyID, chaveTeste("1", n)); ok {
				t.Errorf("NSU %d não deveria estar em %s", n, tab)
			}
		}
	}
	// Documento DEPOIS dos inválidos no mesmo lote foi gravado (o lote segue).
	if _, ok := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chaveTeste("1", 10)); !ok {
		t.Error("NSU 10 (após XML inválido/zip-bomb) deveria ter sido gravado")
	}
}

// Cancelamento: nota existente marcada (as duas tabelas, quando nos dois lados); evento de nota
// inexistente só loga; reenvio da nota não desmarca cancelada mas atualiza os dados.
func TestExecutarCicloNFSe_Cancelamento(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	ambas := nfseXMLOpts{NNFSe: "78", EmitCNPJ: "12345678000280", PrestCNPJ: "12345678000280", TomaCNPJ: cnpjEmpresaTeste,
		DhEmi: "2026-10-03T09:00:00-03:00", DCompet: "2026-10-01", VServ: "300.00"}
	reenvio := nfseTomadaTeste("1")
	reenvio.VServ = "999.00"
	fila := &filaADNTeste{t: t, tamanhoLote: 3, docs: []map[string]interface{}{
		itemNFSeTeste(t, 1, chaveTeste("2", 1), nfseTomadaTeste("1")),
		itemNFSeTeste(t, 2, chaveTeste("2", 2), ambas),
		itemNFSeTeste(t, 3, chaveTeste("2", 3), nfseTomadaTeste("3")),
		itemEventoTeste(4, chaveTeste("2", 1), "CANCELAMENTO", "2026-10-10T15:30:00-03:00"),
		itemEventoTeste(5, chaveTeste("2", 2), "CANCELAMENTO_POR_SUBSTITUICAO", "2026-10-11T08:00:00"),
		itemEventoTeste(6, chaveTeste("2", 999), "CANCELAMENTO", "2026-10-11T08:00:00-03:00"),                          // nota inexistente
		itemEventoTeste(7, chaveTeste("2", 3), "SOLICITACAO_CANCELAMENTO_ANALISE_FISCAL", "2026-10-12T08:00:00-03:00"), // não cancela
		itemNFSeTeste(t, 8, chaveTeste("2", 1), reenvio),                                                               // reenvio da nota já cancelada
	}}
	prepararCicloTeste(t, db, companyID, fila)

	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// novos: nsu1(1) + nsu2(2) + nsu3(1) = 4 (nsu8 é atualização); ignorados: nsu7; pendente: nsu6 (nota inexistente)
	if res.Recebidos != 8 || res.Novos != 4 || res.Ignorados != 1 || res.CancelamentosPendentes != 1 || res.Falhas != 0 {
		t.Errorf("res=%+v (esperava recebidos=8 novos=4 ignorados=1 pendentes=1)", res)
	}
	if n := countPendentesTeste(t, db, companyID, chaveTeste("2", 999)); n != 1 {
		t.Errorf("cancelamento da nota inexistente deveria ficar pendente, veio %d linha(s)", n)
	}

	l, _ := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chaveTeste("2", 1))
	if !l.cancelada || !l.dataCancelamento.Valid || !l.dataCancelamento.Time.Equal(time.Date(2026, 10, 10, 18, 30, 0, 0, time.UTC)) {
		t.Errorf("nota 1 deveria estar cancelada em 10/10 15:30 -03: %+v", l)
	}
	if l.valor != 999 || l.nsu != 8 {
		t.Errorf("reenvio deveria atualizar dados (valor=999, nsu=8) mantendo cancelada: %+v", l)
	}
	for _, tab := range []string{"nfse_entradas", "nfse_saidas"} {
		l2, _ := lerLinhaNFSeTeste(t, db, tab, companyID, chaveTeste("2", 2))
		if !l2.cancelada || !l2.dataCancelamento.Valid {
			t.Errorf("nota 2 em %s deveria estar cancelada: %+v", tab, l2)
		}
	}
	if l3, _ := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chaveTeste("2", 3)); l3.cancelada {
		t.Error("solicitação de cancelamento em análise NÃO cancela a nota")
	}
}

// Interrupção no meio: rejeição no 3º lote → erro; lotes 1–2 gravados e ultimo_nsu do lote 2.
func TestExecutarCicloNFSe_ErroNoLote3_PreservaLotesAnteriores(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	fila := &filaADNTeste{t: t, tamanhoLote: 50}
	for i := int64(1); i <= 150; i++ {
		fila.docs = append(fila.docs, itemNFSeTeste(t, i, chaveTeste("3", i), nfseTomadaTeste(fmt.Sprint(i))))
	}
	fila.intercept = func(w http.ResponseWriter, nsu int64, chamada int) bool {
		if chamada == 3 {
			escreverRejeicaoTeste(w)
			return true
		}
		return false
	}
	prepararCicloTeste(t, db, companyID, fila)

	recebidos, novos, _, err := executarCicloTeste(db, companyID)
	if !errors.Is(err, ErrNFSeADNRejeicao) {
		t.Fatalf("esperava ErrNFSeADNRejeicao, veio %v", err)
	}
	if recebidos != 100 || novos != 100 {
		t.Errorf("recebidos=%d novos=%d (esperava 100/100)", recebidos, novos)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_entradas", companyID); n != 100 {
		t.Errorf("nfse_entradas = %d, esperava 100 (lotes 1–2)", n)
	}
	if n := ultimoNSUTeste(t, db, companyID); n != 100 {
		t.Errorf("ultimo_nsu = %d, esperava 100 (lote 2)", n)
	}
}

// 429 com tentativas esgotadas no 2º lote: SUCESSO PARCIAL, progresso salvo; o próximo ciclo
// continua do cursor.
func TestExecutarCicloNFSe_429Esgotado_SucessoParcialComProgresso(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	fila := &filaADNTeste{t: t, tamanhoLote: 50}
	for i := int64(1); i <= 70; i++ {
		fila.docs = append(fila.docs, itemNFSeTeste(t, i, chaveTeste("4", i), nfseTomadaTeste(fmt.Sprint(i))))
	}
	bloquear := true
	fila.intercept = func(w http.ResponseWriter, nsu int64, chamada int) bool {
		if bloquear && nsu >= 50 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		}
		return false
	}
	esperas := prepararCicloTeste(t, db, companyID, fila)

	recebidos, novos, _, err := executarCicloTeste(db, companyID)
	if err != nil {
		t.Fatalf("429 esgotado deveria ser sucesso parcial, veio %v", err)
	}
	if recebidos != 50 || novos != 50 || ultimoNSUTeste(t, db, companyID) != 50 {
		t.Errorf("recebidos=%d novos=%d ultimo_nsu=%d", recebidos, novos, ultimoNSUTeste(t, db, companyID))
	}
	if got := fmt.Sprint(esperas.lista()); got != "[1s 2s 2s]" { // pausa entre lotes + 2 Retry-After
		t.Errorf("esperas: %s", got)
	}

	bloquear = false
	recebidos2, novos2, _, err := executarCicloTeste(db, companyID)
	if err != nil || recebidos2 != 20 || novos2 != 20 || ultimoNSUTeste(t, db, companyID) != 70 {
		t.Errorf("2º ciclo: recebidos=%d novos=%d err=%v", recebidos2, novos2, err)
	}
}

func countPendentesTeste(t *testing.T, db *sql.DB, companyID, chave string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nfse_cancelamentos_pendentes WHERE company_id = $1 AND chave_acesso = $2`, companyID, chave).Scan(&n); err != nil {
		t.Fatalf("contar pendentes: %v", err)
	}
	return n
}

func countFalhasTeste(t *testing.T, db *sql.DB, companyID string) int {
	t.Helper()
	return countNFSeRowsTeste(t, db, "nfse_adn_falhas", companyID)
}

// TipoDocumento/TipoEvento desconhecidos — inclusive enum numérico — vão para a quarentena; o
// cursor avança e o documento válido do mesmo lote é gravado.
func TestExecutarCicloNFSe_EnumDesconhecidoOuNumerico_Quarentena(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	fila := &filaADNTeste{t: t, tamanhoLote: 50, docs: []map[string]interface{}{
		{"NSU": int64(1), "ChaveAcesso": chaveTeste("6", 1), "TipoDocumento": 3, "ArquivoXml": "eA=="},
		{"NSU": int64(2), "ChaveAcesso": chaveTeste("6", 2), "TipoDocumento": "TIPO_NOVO", "ArquivoXml": "eA=="},
		{"NSU": int64(3), "ChaveAcesso": chaveTeste("6", 3), "TipoDocumento": "EVENTO", "TipoEvento": "EVENTO_NOVO", "ArquivoXml": "eA=="},
		{"NSU": int64(4), "ChaveAcesso": chaveTeste("6", 4), "TipoDocumento": "EVENTO", "TipoEvento": 7, "ArquivoXml": "eA=="},
		itemNFSeTeste(t, 5, chaveTeste("6", 5), nfseTomadaTeste("5")),
	}}
	prepararCicloTeste(t, db, companyID, fila)

	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Falhas != 4 || res.Novos != 1 || res.Ignorados != 0 {
		t.Errorf("res=%+v (esperava falhas=4 novos=1 ignorados=0)", res)
	}
	if n := countFalhasTeste(t, db, companyID); n != 4 {
		t.Errorf("nfse_adn_falhas = %d linhas, esperava 4", n)
	}
	var tipoDoc string
	if err := db.QueryRow(`SELECT tipo_documento FROM nfse_adn_falhas WHERE company_id = $1 AND nsu = 1`, companyID).Scan(&tipoDoc); err != nil || tipoDoc != "3" {
		t.Errorf("enum numérico deveria ficar registrado como \"3\": %q err=%v", tipoDoc, err)
	}
	if n := ultimoNSUTeste(t, db, companyID); n != 5 {
		t.Errorf("ultimo_nsu = %d, esperava 5", n)
	}
}

// Erro de banco num documento (valor que estoura NUMERIC(15,2)) desfaz só aquele documento
// (SAVEPOINT) e o põe em quarentena; os demais do lote são gravados.
func TestExecutarCicloNFSe_ErroDeBancoNumDocumento_IsoladoPorSavepoint(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	gigante := nfseTomadaTeste("2")
	gigante.VServ = "100000000000000000000.00"
	fila := &filaADNTeste{t: t, tamanhoLote: 50, docs: []map[string]interface{}{
		itemNFSeTeste(t, 1, chaveTeste("8", 1), nfseTomadaTeste("1")),
		itemNFSeTeste(t, 2, chaveTeste("8", 2), gigante),
		itemNFSeTeste(t, 3, chaveTeste("8", 3), nfseTomadaTeste("3")),
	}}
	prepararCicloTeste(t, db, companyID, fila)

	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	if err != nil {
		t.Fatalf("erro de banco num documento não pode abortar o lote: %v", err)
	}
	if res.Novos != 2 || res.Falhas != 1 {
		t.Errorf("res=%+v (esperava novos=2 falhas=1)", res)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_entradas", companyID); n != 2 {
		t.Errorf("nfse_entradas = %d, esperava 2", n)
	}
	var motivo string
	if err := db.QueryRow(`SELECT motivo FROM nfse_adn_falhas WHERE company_id = $1 AND nsu = 2`, companyID).Scan(&motivo); err != nil || !strings.Contains(motivo, "erro ao gravar") {
		t.Errorf("NSU 2 deveria estar em quarentena por erro de gravação: motivo=%q err=%v", motivo, err)
	}
	if n := ultimoNSUTeste(t, db, companyID); n != 3 {
		t.Errorf("ultimo_nsu = %d, esperava 3", n)
	}
}

// Evento de cancelamento ANTES da nota no mesmo lote (NSU menor): as NFSE são processadas
// primeiro, então o evento encontra a nota — nada fica pendente.
func TestExecutarCicloNFSe_EventoAntesDaNotaNoMesmoLote_Cancela(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := chaveTeste("A", 1)
	fila := &filaADNTeste{t: t, tamanhoLote: 50, docs: []map[string]interface{}{
		itemEventoTeste(1, chave, "CANCELAMENTO", "2026-10-10T15:30:00-03:00"),
		itemNFSeTeste(t, 2, chave, nfseTomadaTeste("1")),
	}}
	prepararCicloTeste(t, db, companyID, fila)

	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Novos != 1 || res.Cancelamentos != 1 || res.CancelamentosPendentes != 0 {
		t.Errorf("res=%+v", res)
	}
	if l, _ := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chave); !l.cancelada {
		t.Errorf("nota deveria estar cancelada: %+v", l)
	}
	if n := countPendentesTeste(t, db, companyID, chave); n != 0 {
		t.Errorf("não deveria sobrar cancelamento pendente, veio %d", n)
	}
}

// Evento de cancelamento num lote e a nota só num lote POSTERIOR: o cancelamento fica pendente e
// é aplicado (com a data do evento) quando a nota chega; a pendência é removida.
func TestExecutarCicloNFSe_CancelamentoPendente_AplicadoQuandoNotaChega(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := chaveTeste("B", 1)
	fila := &filaADNTeste{t: t, tamanhoLote: 1, docs: []map[string]interface{}{
		itemEventoTeste(1, chave, "CANCELAMENTO_POR_OFICIO", "2026-10-10T15:30:00-03:00"),
		itemNFSeTeste(t, 2, chave, nfseTomadaTeste("1")),
	}}
	prepararCicloTeste(t, db, companyID, fila)

	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Lotes != 2 || res.CancelamentosPendentes != 1 || res.Cancelamentos != 1 || res.Novos != 1 {
		t.Errorf("res=%+v", res)
	}
	l, ok := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chave)
	if !ok || !l.cancelada || !l.dataCancelamento.Valid || !l.dataCancelamento.Time.Equal(time.Date(2026, 10, 10, 18, 30, 0, 0, time.UTC)) {
		t.Errorf("nota deveria chegar já cancelada com a data do evento: ok=%t %+v", ok, l)
	}
	if n := countPendentesTeste(t, db, companyID, chave); n != 0 {
		t.Errorf("pendência deveria ter sido removida, veio %d", n)
	}
}

// Nota criada numa tabela depois de a mesma chave já estar cancelada na tabela irmã herda o
// cancelamento.
func TestExecutarCicloNFSe_TabelaIrmaCancelada_HerdaCancelamento(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := chaveTeste("C", 1)
	prestada := nfseXMLOpts{NNFSe: "1", EmitCNPJ: cnpjEmpresaTeste, PrestCNPJ: cnpjEmpresaTeste, TomaCNPJ: "55444333000122",
		DhEmi: "2026-10-02T09:00:00-03:00", DCompet: "2026-10-01", VServ: "200.00"}
	fila := &filaADNTeste{t: t, tamanhoLote: 1, docs: []map[string]interface{}{
		itemNFSeTeste(t, 1, chave, prestada),
		itemEventoTeste(2, chave, "CANCELAMENTO", "2026-10-10T15:30:00-03:00"),
		itemNFSeTeste(t, 3, chave, nfseTomadaTeste("1")),
	}}
	prepararCicloTeste(t, db, companyID, fila)

	if _, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{}); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	for _, tab := range []string{"nfse_saidas", "nfse_entradas"} {
		if l, ok := lerLinhaNFSeTeste(t, db, tab, companyID, chave); !ok || !l.cancelada || !l.dataCancelamento.Valid {
			t.Errorf("%s deveria estar cancelada: ok=%t %+v", tab, ok, l)
		}
	}
}

// Replay com NSU menor (ex.: cursor zerado) não sobrescreve a versão mais nova já gravada.
func TestExecutarCicloNFSe_ReplayComNSUMenor_NaoSobrescreve(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	chave := chaveTeste("D", 1)
	nova := nfseTomadaTeste("1")
	nova.VServ = "500.00"
	fila := &filaADNTeste{t: t, tamanhoLote: 50, docs: []map[string]interface{}{itemNFSeTeste(t, 10, chave, nova)}}
	prepararCicloTeste(t, db, companyID, fila)
	if _, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{}); err != nil {
		t.Fatalf("1º ciclo: %v", err)
	}

	antiga := nfseTomadaTeste("1")
	antiga.VServ = "100.00"
	fila.mu.Lock()
	fila.docs = []map[string]interface{}{itemNFSeTeste(t, 5, chave, antiga)}
	fila.mu.Unlock()
	if _, err := db.Exec(`UPDATE nfse_certificados SET ultimo_nsu = 0 WHERE company_id = $1`, companyID); err != nil {
		t.Fatalf("zerar cursor: %v", err)
	}
	res, err := ExecutarCicloNFSe(context.Background(), db, companyID, NFSeCicloOpcoes{})
	if err != nil {
		t.Fatalf("2º ciclo: %v", err)
	}
	if res.Atualizados != 0 || res.Novos != 0 || res.Ignorados != 1 {
		t.Errorf("replay antigo: res=%+v (esperava atualizados=0 novos=0 ignorados=1)", res)
	}
	if l, _ := lerLinhaNFSeTeste(t, db, "nfse_entradas", companyID, chave); l.valor != 500 || l.nsu != 10 {
		t.Errorf("replay com NSU menor não pode regredir a linha: %+v", l)
	}
}

// Ciclo já em andamento para a empresa → ErrNFSeCicloEmAndamento, sem tocar no banco.
func TestExecutarCicloNFSe_CicloEmAndamento_Recusa(t *testing.T) {
	const companyID = "company-fake-ciclo-em-andamento"
	liberar, ok := TentarReservarCicloNFSe(companyID)
	if !ok {
		t.Fatal("primeira reserva deveria funcionar")
	}
	defer liberar()
	if _, err := ExecutarCicloNFSe(context.Background(), nil, companyID, NFSeCicloOpcoes{}); !errors.Is(err, ErrNFSeCicloEmAndamento) {
		t.Fatalf("esperava ErrNFSeCicloEmAndamento, veio %v", err)
	}
}
