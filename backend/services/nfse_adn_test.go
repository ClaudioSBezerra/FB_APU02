package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"fb_apu02/crypto"

	"software.sslmate.com/src/go-pkcs12"
)

// ---------------------------------------------------------------------------
// Helpers de certificado de teste — gerados aqui mesmo (autoassinados), NUNCA um certificado
// A1 real nem a API real do Portal Nacional ADN, conforme exigido pela spec-nfse-cliente-adn.md.
// ---------------------------------------------------------------------------

func genTestCA(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("falha ao gerar chave da CA de teste: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "CA de Teste NFS-e"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("falha ao criar certificado da CA de teste: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("falha ao parsear certificado da CA de teste: %v", err)
	}
	return cert, key
}

type leafOpts struct {
	commonName string
	isServer   bool
	ipAddrs    []net.IP
}

func genTestLeaf(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, opts leafOpts) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("falha ao gerar chave de teste: %v", err)
	}
	eku := x509.ExtKeyUsageClientAuth
	if opts.isServer {
		eku = x509.ExtKeyUsageServerAuth
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: opts.commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{eku},
		IPAddresses:  opts.ipAddrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("falha ao criar certificado de teste: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("falha ao parsear certificado de teste: %v", err)
	}
	return cert, key
}

// buildTestPfxBase64 empacota um cert+chave de teste num .pfx (PKCS#12) e devolve em base64 —
// mesmo formato que SaveNFSeCertificadoHandler recebe/criptografa (nfse_credentials.go).
func buildTestPfxBase64(t *testing.T, cert *x509.Certificate, key *rsa.PrivateKey, senha string) string {
	t.Helper()
	pfxBytes, err := pkcs12.Encode(rand.Reader, key, cert, nil, senha)
	if err != nil {
		t.Fatalf("falha ao empacotar .pfx de teste: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pfxBytes)
}

// buildTestPfxBase64ComCadeia é igual a buildTestPfxBase64, mas inclui a CA que assinou o leaf
// no próprio .pfx (caCerts) — reproduz o formato comum em certificados ICP-Brasil A1 reais que
// trazem a cadeia de CA intermediária embutida, caso que pkcs12.Decode rejeita com "expected
// exactly two safe bags in the PFX PDU" (ver nfse_pkcs12_test.go).
func buildTestPfxBase64ComCadeia(t *testing.T, cert, ca *x509.Certificate, key *rsa.PrivateKey, senha string) string {
	t.Helper()
	pfxBytes, err := pkcs12.Encode(rand.Reader, key, cert, []*x509.Certificate{ca}, senha)
	if err != nil {
		t.Fatalf("falha ao empacotar .pfx de teste com cadeia de CA: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pfxBytes)
}

// startNFSeADNTestServer sobe um httptest.Server com mTLS EXIGIDO (ClientAuth:
// tls.RequireAndVerifyClientCert), certificado autoassinado gerado no teste — nunca a API real
// do ADN. clientCAPool define qual(is) CA(s) de certificado de CLIENTE o servidor aceita.
func startNFSeADNTestServer(t *testing.T, handler http.Handler, serverCert tls.Certificate, clientCAPool *x509.CertPool) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAPool,
	}
	ts.StartTLS()
	return ts
}

// trustServerCA injeta a CA de teste do SERVIDOR no client mTLS construído por
// BuildNFSeMTLSClient — necessário só porque o servidor de teste usa um certificado
// autoassinado (em produção o certificado do ADN já é assinado por uma CA pública confiada
// pelo sistema operacional). Não altera a lógica de produção, só o client usado neste teste.
func trustServerCA(t *testing.T, client *http.Client, serverCA *x509.Certificate) {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("esperava *http.Transport, veio %T", client.Transport)
	}
	pool := x509.NewCertPool()
	pool.AddCert(serverCA)
	transport.TLSClientConfig.RootCAs = pool
}

// ---------------------------------------------------------------------------
// ExtractCNPJFromCertificateSubject
// ---------------------------------------------------------------------------

func TestNFSeExtractCNPJFromCertificateSubject_FormatoICPBrasil_Valido(t *testing.T) {
	ca, caKey := genTestCA(t)
	cert, _ := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})

	cnpj, err := ExtractCNPJFromCertificateSubject(cert)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cnpj != "12345678000199" {
		t.Errorf("esperava 12345678000199, veio %q", cnpj)
	}
}

func TestNFSeExtractCNPJFromCertificateSubject_ComPontuacao_ExtraiSoDigitos(t *testing.T) {
	ca, caKey := genTestCA(t)
	cert, _ := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12.345.678/0001-99"})

	cnpj, err := ExtractCNPJFromCertificateSubject(cert)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cnpj != "12345678000199" {
		t.Errorf("esperava 12345678000199, veio %q", cnpj)
	}
}

func TestNFSeExtractCNPJFromCertificateSubject_SemCNPJNoCN_RetornaErro(t *testing.T) {
	ca, caKey := genTestCA(t)
	cert, _ := genTestLeaf(t, ca, caKey, leafOpts{commonName: "CERTIFICADO SEM CNPJ"})

	if _, err := ExtractCNPJFromCertificateSubject(cert); err == nil {
		t.Fatal("esperava erro para CN sem CNPJ (14 dígitos), não houve")
	}
}

// ---------------------------------------------------------------------------
// DecryptNFSeCertificado / BuildNFSeMTLSClient
// ---------------------------------------------------------------------------

func TestNFSeDecryptNFSeCertificado_RoundTrip(t *testing.T) {
	pfxEnc, err := crypto.EncryptField("conteudo-pfx-base64")
	if err != nil {
		t.Fatalf("falha ao criptografar pfx de teste: %v", err)
	}
	senhaEnc, err := crypto.EncryptField("senha-teste")
	if err != nil {
		t.Fatalf("falha ao criptografar senha de teste: %v", err)
	}

	cert, err := DecryptNFSeCertificado(pfxEnc, senhaEnc)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cert.PfxBase64 != "conteudo-pfx-base64" || cert.Senha != "senha-teste" {
		t.Errorf("round-trip divergiu: %+v", cert)
	}
}

func TestNFSeBuildNFSeMTLSClient_PfxNaoEBase64_RetornaErro(t *testing.T) {
	_, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: "!!!nao-e-base64!!!", Senha: "qualquer"})
	if err == nil {
		t.Fatal("esperava erro para certificado_pfx que não é base64 válido")
	}
}

func TestNFSeBuildNFSeMTLSClient_SenhaIncorreta_RetornaErro(t *testing.T) {
	ca, caKey := genTestCA(t)
	cert, key := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})
	pfxB64 := buildTestPfxBase64(t, cert, key, "senha-correta")

	_, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-ERRADA"})
	if err == nil {
		t.Fatal("esperava erro ao decodificar .pfx com senha incorreta")
	}
}

func TestNFSeBuildNFSeMTLSClient_Valido_DecodificaCertEChave(t *testing.T) {
	ca, caKey := genTestCA(t)
	cert, key := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})
	pfxB64 := buildTestPfxBase64(t, cert, key, "senha-correta")

	httpClient, x509Cert, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-correta"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if httpClient == nil || x509Cert == nil {
		t.Fatal("esperava client e certificado não-nulos")
	}
	if x509Cert.Subject.CommonName != "EMPRESA TESTE LTDA:12345678000199" {
		t.Errorf("CN inesperado: %q", x509Cert.Subject.CommonName)
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig.GetClientCertificate == nil {
		t.Fatal("esperava GetClientCertificate configurado no Transport TLS")
	}
	enviado, gerr := transport.TLSClientConfig.GetClientCertificate(&tls.CertificateRequestInfo{})
	if gerr != nil || enviado == nil || len(enviado.Certificate) != 1 {
		t.Fatalf("esperava GetClientCertificate devolvendo o certificado (1 na cadeia), veio %v / %v", enviado, gerr)
	}
}

// TestNFSeBuildNFSeMTLSClient_ComCadeiaIntermediaria_AceitaEMontaCadeiaCompleta cobre o Patch 1
// (CRÍTICO, revisão adversarial): certificados ICP-Brasil reais frequentemente trazem a CA
// intermediária embutida no mesmo .pfx — pkcs12.Decode rejeita esse formato ("expected exactly
// two safe bags in the PFX PDU"), por isso BuildNFSeMTLSClient usa pkcs12.DecodeChain. O
// tls.Certificate.Certificate montado deve conter a cadeia completa (leaf primeiro, depois a
// CA intermediária) — necessário para o handshake mTLS funcionar com certificados reais com
// cadeia — e Leaf precisa continuar sendo só o certificado do titular (não a CA), já que é ele
// que ExtractCNPJFromCertificateSubject usa.
func TestNFSeBuildNFSeMTLSClient_ComCadeiaIntermediaria_AceitaEMontaCadeiaCompleta(t *testing.T) {
	ca, caKey := genTestCA(t)
	cert, key := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})
	pfxB64 := buildTestPfxBase64ComCadeia(t, cert, ca, key, "senha-correta")

	// Pré-condição: confirma que é justamente o cenário que pkcs12.Decode (sem Chain) rejeita.
	pfxBytes, err := base64.StdEncoding.DecodeString(pfxB64)
	if err != nil {
		t.Fatalf("falha ao decodificar base64 de teste: %v", err)
	}
	if _, _, decErr := pkcs12.Decode(pfxBytes, "senha-correta"); decErr == nil {
		t.Fatal("pré-condição falhou: pkcs12.Decode deveria rejeitar .pfx com cadeia de CA intermediária")
	}

	httpClient, x509Cert, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-correta"})
	if err != nil {
		t.Fatalf("esperava sucesso para .pfx com cadeia de CA intermediária, veio erro: %v", err)
	}
	if httpClient == nil || x509Cert == nil {
		t.Fatal("esperava client e certificado não-nulos")
	}
	// Leaf continua sendo o certificado do titular, não a CA.
	if x509Cert.Subject.CommonName != "EMPRESA TESTE LTDA:12345678000199" {
		t.Errorf("CN inesperado (deveria ser o leaf, não a CA): %q", x509Cert.Subject.CommonName)
	}

	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig.GetClientCertificate == nil {
		t.Fatal("esperava GetClientCertificate configurado no Transport TLS")
	}
	enviado, gerr := transport.TLSClientConfig.GetClientCertificate(&tls.CertificateRequestInfo{})
	if gerr != nil || enviado == nil {
		t.Fatalf("GetClientCertificate deveria devolver o certificado: %v", gerr)
	}
	tlsCert := *enviado
	if len(tlsCert.Certificate) != 2 {
		t.Fatalf("esperava cadeia completa com 2 certificados (leaf + CA intermediária) em tlsCert.Certificate, veio %d", len(tlsCert.Certificate))
	}
	if !bytes.Equal(tlsCert.Certificate[0], x509Cert.Raw) {
		t.Error("esperava que o PRIMEIRO certificado da cadeia fosse o leaf (titular), não a CA")
	}
	if !bytes.Equal(tlsCert.Certificate[1], ca.Raw) {
		t.Error("esperava que o SEGUNDO certificado da cadeia fosse a CA intermediária")
	}
	if tlsCert.Leaf.Subject.CommonName != "EMPRESA TESTE LTDA:12345678000199" {
		t.Error("tlsCert.Leaf deveria continuar sendo o certificado do titular, não a CA")
	}
}

// ---------------------------------------------------------------------------
// parseNFSeADNLote — formato real (LoteDistribuicaoNSUResponse, JSON PascalCase)
// ---------------------------------------------------------------------------

func TestNFSeParseNFSeADNLote_FormatoReal(t *testing.T) {
	body := []byte(`{"StatusProcessamento":"DOCUMENTOS_LOCALIZADOS","LoteDFe":[
		{"NSU":11,"ChaveAcesso":"26116062212345678000199000000000000126100000000001","TipoDocumento":"NFSE","TipoEvento":null,"ArquivoXml":"H4sI","DataHoraGeracao":"2026-10-01T10:05:00-03:00"},
		{"NSU":12,"ChaveAcesso":"X","TipoDocumento":"EVENTO","TipoEvento":"CANCELAMENTO","ArquivoXml":null,"DataHoraGeracao":null}],
		"Alertas":[{"Mensagem":{},"Parametros":["a"],"Codigo":"A001","Descricao":"alerta","Complemento":null}],
		"Erros":null,"TipoAmbiente":"PRODUCAO","VersaoAplicativo":"1.2.3","DataHoraProcessamento":"2026-10-26T10:00:00-03:00"}`)
	lote, err := parseNFSeADNLote(body)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if lote.FimDaFila() || len(lote.LoteDFe) != 2 || lote.TipoAmbiente != "PRODUCAO" || lote.VersaoAplicativo != "1.2.3" {
		t.Fatalf("parse divergiu: %+v", lote)
	}
	if d := lote.LoteDFe[0]; d.NSUValor() != 11 || d.TipoDocumento != "NFSE" || d.TipoEvento != "" || d.ArquivoXml != "H4sI" {
		t.Errorf("item 0 divergiu: %+v", d)
	}
	if d := lote.LoteDFe[1]; d.TipoDocumento != "EVENTO" || !EhEventoCancelamentoNFSe(string(d.TipoEvento)) {
		t.Errorf("item 1 divergiu: %+v", d)
	}
	if menor, maior := lote.FaixaNSU(); menor != 11 || maior != 12 {
		t.Errorf("faixa NSU: %d..%d", menor, maior)
	}
	if got := codigosMensagensADN(lote.Alertas); len(got) != 1 || got[0] != "A001: alerta" {
		t.Errorf("códigos de alertas: %q", got)
	}
}

func TestNFSeParseNFSeADNLote_EnumNumerico_NaoQuebra(t *testing.T) {
	lote, err := parseNFSeADNLote([]byte(`{"StatusProcessamento":2,"LoteDFe":[{"NSU":1,"TipoDocumento":3}],"TipoAmbiente":1}`))
	if err != nil {
		t.Fatalf("enum numérico não deveria quebrar o parse: %v", err)
	}
	if lote.StatusProcessamento != "2" || lote.LoteDFe[0].TipoDocumento != "3" {
		t.Errorf("enum numérico deveria virar dígitos: %+v", lote)
	}
}

func TestNFSeParseNFSeADNLote_FormatoSupostoAntigo_RetornaErro(t *testing.T) {
	// O formato SUPOSTO anterior (camelCase) decodificava pra struct vazia sem erro — a causa do
	// "0 recebidos". Agora é formato inesperado.
	if _, err := parseNFSeADNLote([]byte(`{"loteVazio":true,"ultimoNSU":"10","documentos":[]}`)); err == nil {
		t.Fatal("esperava erro para corpo sem StatusProcessamento")
	}
	if _, err := parseNFSeADNLote([]byte("isto nao e json")); err == nil {
		t.Fatal("esperava erro para corpo que não é JSON")
	}
}

func TestNFSeEhEventoCancelamento(t *testing.T) {
	for _, v := range []string{"CANCELAMENTO", "CANCELAMENTO_POR_SUBSTITUICAO", "CANCELAMENTO_POR_OFICIO", "CANCELAMENTO_DEFERIDO_ANALISE_FISCAL", "cancelamento"} {
		if !EhEventoCancelamentoNFSe(v) {
			t.Errorf("%q deveria ser cancelamento", v)
		}
	}
	for _, v := range []string{"SOLICITACAO_CANCELAMENTO_ANALISE_FISCAL", "CANCELAMENTO_INDEFERIDO_ANALISE_FISCAL", "CONFIRMACAO_TOMADOR", "BLOQUEIO_POR_OFICIO", ""} {
		if EhEventoCancelamentoNFSe(v) {
			t.Errorf("%q NÃO deveria ser cancelamento", v)
		}
	}
}

func TestNFSeParseRetryAfter(t *testing.T) {
	agora := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	casos := map[string]time.Duration{
		"":                              0,
		"7":                             7 * time.Second,
		"0":                             0,
		"-3":                            0,
		"abc":                           0,
		"Mon, 26 Oct 2026 12:00:30 GMT": 30 * time.Second,
		"Mon, 26 Oct 2026 11:00:00 GMT": 0, // no passado
	}
	for in, want := range casos {
		if got := parseRetryAfter(in, agora); got != want {
			t.Errorf("parseRetryAfter(%q) = %s, esperava %s", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// ResolveNFSeADNBaseURL
// ---------------------------------------------------------------------------

func TestNFSeResolveNFSeADNBaseURL_Default_Producao(t *testing.T) {
	t.Setenv("NFSE_ADN_BASE_URL", "")
	t.Setenv("NFSE_ADN_AMBIENTE", "")
	if got := ResolveNFSeADNBaseURL(); got != NFSeADNBaseURLProducao {
		t.Errorf("esperava %q, veio %q", NFSeADNBaseURLProducao, got)
	}
}

func TestNFSeResolveNFSeADNBaseURL_ProducaoRestrita(t *testing.T) {
	t.Setenv("NFSE_ADN_BASE_URL", "")
	t.Setenv("NFSE_ADN_AMBIENTE", "producao_restrita")
	if got := ResolveNFSeADNBaseURL(); got != NFSeADNBaseURLProducaoRestrita {
		t.Errorf("esperava %q, veio %q", NFSeADNBaseURLProducaoRestrita, got)
	}
}

func TestNFSeResolveNFSeADNBaseURL_OverrideExplicito(t *testing.T) {
	t.Setenv("NFSE_ADN_BASE_URL", "https://exemplo.teste/")
	t.Setenv("NFSE_ADN_AMBIENTE", "producao_restrita") // override explícito vence mesmo com ambiente setado
	if got := ResolveNFSeADNBaseURL(); got != "https://exemplo.teste" {
		t.Errorf("override explícito deveria vencer (sem a barra final), veio %q", got)
	}
}

// ---------------------------------------------------------------------------
// BuscarLoteDFeADN — servidor de teste mTLS (sem acesso à API real)
// ---------------------------------------------------------------------------

func TestNFSeBuscarLoteDFeADN_200_ChamaComLoteTrue(t *testing.T) {
	var uri string
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		escreverLoteTeste(w, []map[string]interface{}{itemNFSeTeste(t, 8, chaveTeste("1", 8), nfseXMLOpts{NNFSe: "8"})})
	}), "12345678000199")

	lote, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 7)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if uri != "/contribuintes/DFe/7?lote=true" {
		t.Errorf("URI chamada: %q", uri)
	}
	if lote.FimDaFila() || len(lote.LoteDFe) != 1 || lote.LoteDFe[0].NSUValor() != 8 {
		t.Errorf("lote divergiu: %+v", lote)
	}
}

func TestNFSeBuscarLoteDFeADN_404NenhumDocumento_FimDaFilaSemErro(t *testing.T) {
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escreverFimTeste(w) }), "12345678000199")
	lote, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0)
	if err != nil {
		t.Fatalf("404 NENHUM_DOCUMENTO_LOCALIZADO é fim da fila, não erro: %v", err)
	}
	if !lote.FimDaFila() {
		t.Errorf("esperava FimDaFila: %+v", lote)
	}
}

func TestNFSeBuscarLoteDFeADN_404SemCorpoDocumentado_ErroComunicacao(t *testing.T) {
	env := novoADNTesteMTLS(t, http.NotFoundHandler(), "12345678000199")
	_, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0)
	if !errors.Is(err, ErrNFSeADNComunicacaoFalhou) {
		t.Fatalf("404 sem corpo do ADN (rota errada) não pode virar 'fila vazia' silenciosa — veio %v", err)
	}
}

func TestNFSeBuscarLoteDFeADN_400Rejeicao_ErroComCodigos(t *testing.T) {
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escreverRejeicaoTeste(w) }), "12345678000199")
	_, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0)
	if !errors.Is(err, ErrNFSeADNRejeicao) {
		t.Fatalf("esperava ErrNFSeADNRejeicao, veio %v", err)
	}
	if !strings.Contains(err.Error(), "E1235") {
		t.Errorf("erro deveria carregar o código de Erros: %v", err)
	}
}

func TestNFSeBuscarLoteDFeADN_429E503_TransitorioComRetryAfter(t *testing.T) {
	status := http.StatusTooManyRequests
	retryAfter := "7"
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
	}), "12345678000199")

	_, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0)
	var trans *NFSeADNErroTransitorio
	if !errors.As(err, &trans) || !errors.Is(err, ErrNFSeADNTransitorio) {
		t.Fatalf("esperava *NFSeADNErroTransitorio, veio %v", err)
	}
	if trans.StatusHTTP != 429 || trans.RetryAfter != 7*time.Second {
		t.Errorf("transitório divergiu: %+v", trans)
	}

	status, retryAfter = http.StatusServiceUnavailable, ""
	_, err = BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0)
	if !errors.As(err, &trans) || trans.StatusHTTP != 503 || trans.RetryAfter != 0 {
		t.Fatalf("503 sem Retry-After deveria ser transitório com RetryAfter=0, veio %v", err)
	}
}

// TestNFSeBuscarDFeADN_CertificadoRejeitado_RetornaErroAutenticacao: o servidor de teste REJEITA
// o certificado (assinado por uma CA que ele não confia) — handshake mTLS falha.
func TestNFSeBuscarDFeADN_CertificadoRejeitado_RetornaErroAutenticacao(t *testing.T) {
	serverCA, serverCAKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, serverCA, serverCAKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}

	trustedClientCA, _ := genTestCA(t)
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(trustedClientCA)

	untrustedCA, untrustedCAKey := genTestCA(t)
	clientCert, clientKey := genTestLeaf(t, untrustedCA, untrustedCAKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})

	ts := startNFSeADNTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handshake deveria ter falhado antes de qualquer requisição chegar ao handler")
	}), serverTLSCert, clientCAPool)
	defer ts.Close()

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	httpClient, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-correta"})
	if err != nil {
		t.Fatalf("falha ao montar client mTLS: %v", err)
	}
	trustServerCA(t, httpClient, serverCA)

	_, err = BuscarLoteDFeADN(context.Background(), httpClient, ts.URL, 0)
	if !errors.Is(err, ErrNFSeADNAutenticacaoFalhou) {
		t.Errorf("esperava erro envolvendo ErrNFSeADNAutenticacaoFalhou, veio: %v", err)
	}
}

func TestNFSeBuscarDFeADN_HTTP401_RetornaErroAutenticacao(t *testing.T) {
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"erro":"nao autorizado"}`))
	}), "12345678000199")
	_, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0)
	if !errors.Is(err, ErrNFSeADNAutenticacaoFalhou) {
		t.Errorf("esperava erro envolvendo ErrNFSeADNAutenticacaoFalhou para HTTP 401, veio: %v", err)
	}
}

func TestNFSeBuscarDFeADN_FormatoInesperado_RetornaErroFormato(t *testing.T) {
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("isto nao e o JSON esperado"))
	}), "12345678000199")
	_, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0)
	if !errors.Is(err, ErrNFSeADNFormatoInesperado) {
		t.Errorf("esperava erro envolvendo ErrNFSeADNFormatoInesperado, veio: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PercorrerFilaADN — paginação por NSU, transitórios, limites, interrupção
// ---------------------------------------------------------------------------

func filaComDocsTeste(t *testing.T, n int) *filaADNTeste {
	f := &filaADNTeste{t: t, tamanhoLote: 50}
	for i := int64(1); i <= int64(n); i++ {
		f.docs = append(f.docs, itemNFSeTeste(t, i, chaveTeste("9", i), nfseXMLOpts{NNFSe: fmt.Sprint(i)}))
	}
	return f
}

// AC1 (nível de serviço): 120 documentos → 3 lotes (50/50/20) e para no 404; cursor = 120.
func TestNFSePercorrerFilaADN_120Documentos_3LotesAteFim(t *testing.T) {
	fila := filaComDocsTeste(t, 120)
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, esperas := cfgFilaTeste()

	var maiores []int64
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(l NFSeADNLote, maior int64) error {
		maiores = append(maiores, maior)
		return nil
	})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.Lotes != 3 || res.Documentos != 120 || res.UltimoNSU != 120 || res.Encerramento != NFSeADNEncerramentoFimDaFila || res.Parcial() {
		t.Errorf("resultado divergiu: %+v", res)
	}
	if fmt.Sprint(maiores) != "[50 100 120]" {
		t.Errorf("maiores NSU por lote: %v", maiores)
	}
	want := []string{"/contribuintes/DFe/0?lote=true", "/contribuintes/DFe/50?lote=true", "/contribuintes/DFe/100?lote=true", "/contribuintes/DFe/120?lote=true"}
	if got := fila.Pedidos(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("pedidos: %v, esperava %v", got, want)
	}
	// Pausa de ~1s entre lotes (3 pausas: antes dos pedidos 2, 3 e 4).
	if got := esperas.lista(); len(got) != 3 || got[0] != time.Second {
		t.Errorf("esperava 3 pausas de 1s entre lotes, veio %v", got)
	}
}

func TestNFSePercorrerFilaADN_FilaVazia_SucessoSemProcessar(t *testing.T) {
	fila := filaComDocsTeste(t, 0)
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, _ := cfgFilaTeste()
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error {
		t.Error("fila vazia não deveria chamar processar")
		return nil
	})
	if err != nil || res.Documentos != 0 || res.Encerramento != NFSeADNEncerramentoFimDaFila {
		t.Fatalf("fila vazia: res=%+v err=%v", res, err)
	}
}

// 429 com Retry-After na 1ª tentativa do 2º lote: espera o Retry-After e segue até o fim.
func TestNFSePercorrerFilaADN_429ComRetryAfter_RespeitaEsperaESegue(t *testing.T) {
	fila := filaComDocsTeste(t, 60)
	fila.intercept = func(w http.ResponseWriter, nsu int64, chamada int) bool {
		if chamada == 2 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		}
		return false
	}
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, esperas := cfgFilaTeste()

	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error { return nil })
	if err != nil || res.Encerramento != NFSeADNEncerramentoFimDaFila || res.UltimoNSU != 60 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// pausa entre lotes (1s), Retry-After (3s), pausa entre lotes (1s)
	if got := fmt.Sprint(esperas.lista()); got != "[1s 3s 1s]" {
		t.Errorf("esperas: %s", got)
	}
}

// Retry-After absurdo é limitado a EsperaMax; sem Retry-After, backoff exponencial.
func TestNFSePercorrerFilaADN_Backoff_TetoEExponencial(t *testing.T) {
	fila := filaComDocsTeste(t, 10)
	fila.intercept = func(w http.ResponseWriter, nsu int64, chamada int) bool {
		switch chamada {
		case 1:
			w.Header().Set("Retry-After", "86400")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		case 2:
			w.WriteHeader(http.StatusBadGateway)
			return true
		}
		return false
	}
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, esperas := cfgFilaTeste()
	cfg.MaxTentativas = 3
	if _, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error { return nil }); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	got := esperas.lista()
	if len(got) < 2 || got[0] != cfg.EsperaMax || got[1] != cfg.BackoffBase*2 {
		t.Errorf("esperava [EsperaMax, 2×BackoffBase, ...], veio %v", got)
	}
}

// 429 em todas as tentativas do 2º lote: encerra com SUCESSO PARCIAL, 1º lote preservado.
func TestNFSePercorrerFilaADN_429Esgotado_SucessoParcial(t *testing.T) {
	fila := filaComDocsTeste(t, 80)
	fila.intercept = func(w http.ResponseWriter, nsu int64, chamada int) bool {
		if nsu >= 50 {
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		}
		return false
	}
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, _ := cfgFilaTeste()
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error { return nil })
	if err != nil {
		t.Fatalf("429 esgotado não é erro (sucesso parcial), veio %v", err)
	}
	if res.Encerramento != NFSeADNEncerramentoTransitorio || !res.Parcial() || res.UltimoNSU != 50 || res.Lotes != 1 {
		t.Errorf("res=%+v", res)
	}
	if n := len(fila.Pedidos()); n != 1+cfg.MaxTentativas {
		t.Errorf("esperava 1 + %d pedidos, veio %d", cfg.MaxTentativas, n)
	}
}

// Interrupção no meio: rejeição no 3º lote → erro, lotes 1–2 processados, cursor 100.
func TestNFSePercorrerFilaADN_ErroNoLote3_MantemLotesAnteriores(t *testing.T) {
	fila := filaComDocsTeste(t, 150)
	fila.intercept = func(w http.ResponseWriter, nsu int64, chamada int) bool {
		if chamada == 3 {
			escreverRejeicaoTeste(w)
			return true
		}
		return false
	}
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, _ := cfgFilaTeste()
	processados := 0
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error {
		processados++
		return nil
	})
	if !errors.Is(err, ErrNFSeADNRejeicao) {
		t.Fatalf("esperava ErrNFSeADNRejeicao, veio %v", err)
	}
	if processados != 2 || res.UltimoNSU != 100 || res.Documentos != 100 {
		t.Errorf("processados=%d res=%+v", processados, res)
	}
}

func TestNFSePercorrerFilaADN_ErroDoProcessar_Para(t *testing.T) {
	fila := filaComDocsTeste(t, 120)
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, _ := cfgFilaTeste()
	falha := errors.New("falha de banco simulada")
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(_ NFSeADNLote, maior int64) error {
		if maior == 100 {
			return falha
		}
		return nil
	})
	if !errors.Is(err, falha) || res.UltimoNSU != 50 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestNFSePercorrerFilaADN_LimiteDeLotes_SucessoParcial(t *testing.T) {
	fila := filaComDocsTeste(t, 200)
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, _ := cfgFilaTeste()
	cfg.MaxLotes = 2
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error { return nil })
	if err != nil || res.Encerramento != NFSeADNEncerramentoLimiteLotes || res.UltimoNSU != 100 || len(fila.Pedidos()) != 2 {
		t.Fatalf("res=%+v err=%v pedidos=%v", res, err, fila.Pedidos())
	}
}

// Servidor que devolve sempre o mesmo NSU (sem avançar) não pode prender o ciclo em loop.
func TestNFSePercorrerFilaADN_CursorSemAvanco_Encerra(t *testing.T) {
	item := itemNFSeTeste(t, 5, chaveTeste("1", 5), nfseXMLOpts{})
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escreverLoteTeste(w, []map[string]interface{}{item})
	}), "12345678000199")
	cfg, _ := cfgFilaTeste()
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error { return nil })
	if err != nil || res.Lotes != 1 || res.Encerramento != NFSeADNEncerramentoCursorSemAvanco {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestNFSePercorrerFilaADN_CtxCancelado_EncerraSemErro(t *testing.T) {
	fila := filaComDocsTeste(t, 120)
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	ctx, cancel := context.WithCancel(context.Background())
	cfg, _ := cfgFilaTeste()
	res, err := PercorrerFilaADN(ctx, env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error {
		cancel() // cancela durante o 1º lote
		return nil
	})
	if err != nil || res.Encerramento != NFSeADNEncerramentoCancelado || res.UltimoNSU != 50 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestNFSePercorrerFilaADN_LimiteDeTempo_SucessoParcial(t *testing.T) {
	fila := filaComDocsTeste(t, 120)
	env := novoADNTesteMTLS(t, fila, "12345678000199")
	cfg, _ := cfgFilaTeste()
	cfg.MaxDuracao = time.Nanosecond
	time.Sleep(time.Millisecond)
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error { return nil })
	if err != nil || res.Encerramento != NFSeADNEncerramentoLimiteTempo {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestNFSeNfseSleepCtx_RespeitaCancelamento(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inicio := time.Now()
	if err := nfseSleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("esperava context.Canceled, veio %v", err)
	}
	if time.Since(inicio) > time.Second {
		t.Error("espera não respeitou o cancelamento")
	}
}

// Corpo acima do limite: detectado e rejeitado como formato inesperado (nunca truncado).
func TestNFSeBuscarLoteDFeADN_CorpoAcimaDoLimite_ErroFormato(t *testing.T) {
	original := nfseADNMaxBodyBytes
	nfseADNMaxBodyBytes = 1024
	t.Cleanup(func() { nfseADNMaxBodyBytes = original })

	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		itens := make([]map[string]interface{}, 0, 5)
		for i := int64(1); i <= 5; i++ {
			itens = append(itens, itemNFSeTeste(t, i, chaveTeste("1", i), nfseXMLOpts{NNFSe: "1"}))
		}
		escreverLoteTeste(w, itens)
	}), "12345678000199")
	_, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0)
	if !errors.Is(err, ErrNFSeADNFormatoInesperado) || !strings.Contains(err.Error(), "excede o limite") {
		t.Fatalf("esperava ErrNFSeADNFormatoInesperado por corpo acima do limite, veio %v", err)
	}
}

// Corpo exatamente no limite é aceito.
func TestNFSeBuscarLoteDFeADN_CorpoNoLimite_Aceito(t *testing.T) {
	corpo := []byte(`{"StatusProcessamento":"DOCUMENTOS_LOCALIZADOS","LoteDFe":[]}`)
	original := nfseADNMaxBodyBytes
	nfseADNMaxBodyBytes = int64(len(corpo))
	t.Cleanup(func() { nfseADNMaxBodyBytes = original })
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(corpo) }), "12345678000199")
	if _, err := BuscarLoteDFeADN(context.Background(), env.client, env.ts.URL, 0); err != nil {
		t.Fatalf("corpo no limite deveria passar: %v", err)
	}
}

func TestNFSeEhErroRedeTransitorioADN_Classificacao(t *testing.T) {
	timeoutErr := &url.Error{Op: "Get", URL: "https://adn", Err: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}}
	transitorios := map[string]error{
		"timeout":       timeoutErr,
		"conn reset":    &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}}},
		"conn refused":  &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}},
		"dns":           &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "adn.nfse.gov.br"}}},
		"eof":           &url.Error{Op: "Get", URL: "x", Err: io.EOF},
		"unexpectedEOF": fmt.Errorf("lendo corpo: %w", io.ErrUnexpectedEOF),
	}
	for nome, err := range transitorios {
		if !ehErroRedeTransitorioADN(err) {
			t.Errorf("%s deveria ser transitório: %v", nome, err)
		}
	}
	naoTransitorios := map[string]error{
		"nil":              nil,
		"alerta tls":       &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")}},
		"bad record MAC":   &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "local error", Err: errors.New("tls: bad record MAC")}},
		"cert do servidor": &url.Error{Op: "Get", URL: "x", Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}},
		"genérico":         errors.New("algo deu errado"),
	}
	for nome, err := range naoTransitorios {
		if ehErroRedeTransitorioADN(err) {
			t.Errorf("%s NÃO deveria ser transitório: %v", nome, err)
		}
	}
}

// Conexão recusada (nada escutando) → *NFSeADNErroTransitorio (retentativa), não autenticação.
func TestNFSeBuscarLoteDFeADN_ConexaoRecusada_Transitorio(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, err = BuscarLoteDFeADN(context.Background(), &http.Client{Timeout: 5 * time.Second}, "https://"+addr, 0)
	var trans *NFSeADNErroTransitorio
	if !errors.As(err, &trans) || trans.StatusHTTP != 0 || errors.Is(err, ErrNFSeADNAutenticacaoFalhou) {
		t.Fatalf("conexão recusada deveria ser transitória, veio %v", err)
	}
}

// Servidor que fecha a conexão sem responder (EOF) → transitório; esgotadas as tentativas, o
// percurso encerra com sucesso parcial transitorio_esgotado.
func TestNFSePercorrerFilaADN_EOFDoServidor_TransitorioEsgotado(t *testing.T) {
	env := novoADNTesteMTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("sem hijacker")
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}), "12345678000199")
	cfg, _ := cfgFilaTeste()
	res, err := PercorrerFilaADN(context.Background(), env.client, env.ts.URL, 0, cfg, func(NFSeADNLote, int64) error { return nil })
	if err != nil || res.Encerramento != NFSeADNEncerramentoTransitorio {
		t.Fatalf("EOF do servidor deveria virar transitório esgotado: res=%+v err=%v", res, err)
	}
}
