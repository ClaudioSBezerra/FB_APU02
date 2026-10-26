package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fixtures no formato REAL do ADN (Swagger "API NFS-e - ADN Contribuinte" v1): JSON PascalCase
// LoteDistribuicaoNSUResponse, ArquivoXml = base64(gzip(XML da NFS-e)) gerado aqui mesmo a
// partir de um XML realista do leiaute nacional (namespace http://www.sped.fazenda.gov.br/nfse).
// Nunca a API real.
// ---------------------------------------------------------------------------

// nfseXMLOpts descreve uma NFS-e de teste. Campos vazios são OMITIDOS do XML (testa "ausente=0").
type nfseXMLOpts struct {
	Chave      string
	NNFSe      string
	EmitCNPJ   string
	PrestCNPJ  string
	PrestCPF   string
	TomaCNPJ   string
	TomaCPF    string
	DhEmi      string
	DCompet    string
	VServ      string
	VISSQN     string
	TpRetISSQN string
	VPis       string
	VCofins    string
	VRetCP     string
	VRetIRRF   string
	VRetCSLL   string
}

func tagOpt(nome, valor string) string {
	if valor == "" {
		return ""
	}
	return "<" + nome + ">" + valor + "</" + nome + ">"
}

// nfseXMLTeste monta um XML de NFS-e no leiaute nacional (estrutura NFSe/infNFSe/.../DPS/infDPS,
// com Signature e demais elementos que o parser deve ignorar).
func nfseXMLTeste(o nfseXMLOpts) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<NFSe xmlns="http://www.sped.fazenda.gov.br/nfse" versao="1.00">`)
	b.WriteString(`<infNFSe Id="NFS` + o.Chave + `">`)
	b.WriteString(`<xLocEmi>Recife</xLocEmi><xLocPrestacao>Recife</xLocPrestacao>`)
	b.WriteString(tagOpt("nNFSe", o.NNFSe))
	b.WriteString(`<cLocIncid>2611606</cLocIncid><xLocIncid>Recife</xLocIncid><xTribNac>Suporte técnico</xTribNac>`)
	b.WriteString(`<verAplic>SefinNac_1.0</verAplic><ambGer>2</ambGer><tpEmis>1</tpEmis><procEmi>1</procEmi><cStat>100</cStat>`)
	b.WriteString(`<dhProc>2026-10-01T10:05:00-03:00</dhProc><nDFSe>987</nDFSe>`)
	b.WriteString(`<emit>` + tagOpt("CNPJ", o.EmitCNPJ) + `<IM>123</IM><xNome>PRESTADOR &amp; CIA LTDA</xNome>`)
	b.WriteString(`<enderNac><xLgr>Rua A</xLgr><nro>1</nro><xBairro>Centro</xBairro><cMun>2611606</cMun><UF>PE</UF><CEP>50000000</CEP></enderNac></emit>`)
	b.WriteString(`<valores><vCalcDR>0.00</vCalcDR><vBC>` + firstNonEmpty(o.VServ, "0.00") + `</vBC><pAliqAplic>5.00</pAliqAplic>`)
	b.WriteString(tagOpt("vISSQN", o.VISSQN))
	b.WriteString(`<vTotalRet>0.00</vTotalRet><vLiq>0.00</vLiq></valores>`)
	b.WriteString(`<DPS versao="1.00"><infDPS Id="DPS261160611234567800019900001000000000000001">`)
	b.WriteString(`<tpAmb>2</tpAmb>` + tagOpt("dhEmi", o.DhEmi) + `<verAplic>ERP 1.0</verAplic><serie>1</serie><nDPS>1</nDPS>`)
	b.WriteString(tagOpt("dCompet", o.DCompet) + `<tpEmit>1</tpEmit><cLocEmi>2611606</cLocEmi>`)
	b.WriteString(`<prest>` + tagOpt("CNPJ", o.PrestCNPJ) + tagOpt("CPF", o.PrestCPF) + `<regTrib><opSimpNac>1</opSimpNac><regEspTrib>0</regEspTrib></regTrib></prest>`)
	b.WriteString(`<toma>` + tagOpt("CNPJ", o.TomaCNPJ) + tagOpt("CPF", o.TomaCPF) + `<xNome>TOMADOR SA</xNome></toma>`)
	b.WriteString(`<serv><locPrest><cLocPrestacao>2611606</cLocPrestacao></locPrest><cServ><cTribNac>010701</cTribNac><xDescServ>Suporte</xDescServ></cServ></serv>`)
	b.WriteString(`<valores><vServPrest>` + tagOpt("vServ", o.VServ) + `</vServPrest>`)
	b.WriteString(`<trib><tribMun><tribISSQN>1</tribISSQN>` + tagOpt("tpRetISSQN", o.TpRetISSQN) + `</tribMun>`)
	b.WriteString(`<tribFed><piscofins><CST>01</CST>` + tagOpt("vPis", o.VPis) + tagOpt("vCofins", o.VCofins) + `<tpRetPisCofins>1</tpRetPisCofins></piscofins>`)
	b.WriteString(tagOpt("vRetCP", o.VRetCP) + tagOpt("vRetIRRF", o.VRetIRRF) + tagOpt("vRetCSLL", o.VRetCSLL) + `</tribFed>`)
	b.WriteString(`<totTrib><indTotTrib>0</indTotTrib></totTrib></trib></valores>`)
	b.WriteString(`</infDPS><Signature xmlns="http://www.w3.org/2000/09/xmldsig#"><SignedInfo/></Signature></DPS>`)
	b.WriteString(`</infNFSe><Signature xmlns="http://www.w3.org/2000/09/xmldsig#"><SignedInfo/></Signature></NFSe>`)
	return b.String()
}

// gzipB64Teste comprime e codifica em base64 (forma do ArquivoXml).
func gzipB64Teste(t *testing.T, conteudo []byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(conteudo); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// chaveTeste gera uma chave de acesso de 50 dígitos a partir de n.
func chaveTeste(prefixo string, n int64) string {
	s := fmt.Sprintf("%s%d", prefixo, n)
	if len(s) < 50 {
		s = strings.Repeat("0", 50-len(s)) + s
	}
	return s
}

func itemNFSeTeste(t *testing.T, nsu int64, chave string, o nfseXMLOpts) map[string]interface{} {
	t.Helper()
	o.Chave = chave
	return map[string]interface{}{
		"NSU": nsu, "ChaveAcesso": chave, "TipoDocumento": "NFSE", "TipoEvento": nil,
		"ArquivoXml": gzipB64Teste(t, []byte(nfseXMLTeste(o))), "DataHoraGeracao": "2026-10-01T10:05:00-03:00",
	}
}

func itemEventoTeste(nsu int64, chave, tipoEvento, dataHora string) map[string]interface{} {
	return map[string]interface{}{
		"NSU": nsu, "ChaveAcesso": chave, "TipoDocumento": "EVENTO", "TipoEvento": tipoEvento,
		"ArquivoXml": base64.StdEncoding.EncodeToString([]byte("evento-nao-interpretado")), "DataHoraGeracao": dataHora,
	}
}

func escreverLoteTeste(w http.ResponseWriter, itens []map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"StatusProcessamento": "DOCUMENTOS_LOCALIZADOS", "LoteDFe": itens,
		"Alertas": []map[string]interface{}{}, "Erros": nil,
		"TipoAmbiente": "HOMOLOGACAO", "VersaoAplicativo": "1.0.0", "DataHoraProcessamento": "2026-10-26T10:00:00-03:00",
	})
}

func escreverFimTeste(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"StatusProcessamento": "NENHUM_DOCUMENTO_LOCALIZADO", "LoteDFe": nil,
		"Alertas": nil, "Erros": []map[string]interface{}{{"Codigo": "E2220", "Descricao": "Nenhum documento localizado"}},
		"TipoAmbiente": "HOMOLOGACAO", "VersaoAplicativo": "1.0.0", "DataHoraProcessamento": "2026-10-26T10:00:00-03:00",
	})
}

func escreverRejeicaoTeste(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"StatusProcessamento": "REJEICAO", "LoteDFe": nil,
		"Erros":        []map[string]interface{}{{"Codigo": "E1235", "Descricao": "CNPJ não habilitado", "Mensagem": map[string]interface{}{}, "Parametros": []string{"x"}}},
		"TipoAmbiente": "PRODUCAO", "VersaoAplicativo": "1.0.0", "DataHoraProcessamento": "2026-10-26T10:00:00-03:00",
	})
}

// nsuDaRequisicaoTeste extrai o NSU do caminho /contribuintes/DFe/{NSU}; falha o teste se
// lote=true não vier na query.
func nsuDaRequisicaoTeste(t *testing.T, r *http.Request) int64 {
	t.Helper()
	if r.URL.Query().Get("lote") != "true" {
		t.Errorf("requisição sem lote=true: %s", r.URL.String())
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/contribuintes/DFe/"), 10, 64)
	if err != nil {
		t.Errorf("NSU inválido no caminho %q", r.URL.Path)
	}
	return n
}

// filaADNTeste simula a fila de distribuição: documentos com NSU 1..len(docs), lotes de até
// tamanhoLote com NSU > o pedido; nada mais → 404 NENHUM_DOCUMENTO_LOCALIZADO. intercept (opcional)
// pode responder no lugar da fila (devolve true se respondeu). Registra os caminhos pedidos.
type filaADNTeste struct {
	t           *testing.T
	mu          sync.Mutex
	docs        []map[string]interface{}
	tamanhoLote int
	pedidos     []string
	intercept   func(w http.ResponseWriter, nsu int64, chamada int) bool
}

func (f *filaADNTeste) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	nsu := nsuDaRequisicaoTeste(f.t, r)
	f.mu.Lock()
	f.pedidos = append(f.pedidos, r.URL.RequestURI())
	chamada := len(f.pedidos)
	f.mu.Unlock()
	if f.intercept != nil && f.intercept(w, nsu, chamada) {
		return
	}
	var lote []map[string]interface{}
	for _, d := range f.docs {
		if n := d["NSU"].(int64); n > nsu && len(lote) < f.tamanhoLote {
			lote = append(lote, d)
		}
	}
	if len(lote) == 0 {
		escreverFimTeste(w)
		return
	}
	escreverLoteTeste(w, lote)
}

func (f *filaADNTeste) Pedidos() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.pedidos...)
}

// adnTesteMTLS sobe um servidor mTLS de teste com handler e devolve um client já confiando nele
// (para testes de nfse_adn.go que não passam pelo banco) e a CA (para os do processor).
type adnTesteMTLS struct {
	ts     *httptest.Server
	ca     *x509.Certificate
	client *http.Client
	pfxB64 string
}

func novoADNTesteMTLS(t *testing.T, handler http.Handler, cnpjCertificado string) *adnTesteMTLS {
	t.Helper()
	ca, caKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, ca, caKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	clientCert, clientKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:" + cnpjCertificado})
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	ts := startNFSeADNTestServer(t, handler, tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}, pool)
	t.Cleanup(ts.Close)

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	client, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-correta"})
	if err != nil {
		t.Fatalf("falha ao montar client mTLS: %v", err)
	}
	trustServerCA(t, client, ca)
	return &adnTesteMTLS{ts: ts, ca: ca, client: client, pfxB64: pfxB64}
}

// esperasTeste registra as esperas pedidas (sem esperar de verdade).
type esperasTeste struct {
	mu      sync.Mutex
	esperas []time.Duration
}

func (e *esperasTeste) sleep(ctx context.Context, d time.Duration) error {
	e.mu.Lock()
	e.esperas = append(e.esperas, d)
	e.mu.Unlock()
	return ctx.Err()
}

func (e *esperasTeste) lista() []time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]time.Duration(nil), e.esperas...)
}

// cfgFilaTeste devolve a configuração padrão com Sleep instrumentado (sem espera real).
func cfgFilaTeste() (nfseADNFilaConfig, *esperasTeste) {
	e := &esperasTeste{}
	cfg := nfseADNFilaConfigPadrao
	cfg.Sleep = e.sleep
	return cfg, e
}

// semEsperasNoProcessor troca nfseADNFilaConfigPadrao (usado por ExecutarCicloNFSe) por uma
// versão sem espera real durante o teste.
func semEsperasNoProcessor(t *testing.T) *esperasTeste {
	t.Helper()
	original := nfseADNFilaConfigPadrao
	cfg, e := cfgFilaTeste()
	nfseADNFilaConfigPadrao = cfg
	t.Cleanup(func() { nfseADNFilaConfigPadrao = original })
	return e
}
