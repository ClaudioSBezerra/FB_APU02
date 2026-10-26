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
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
// parseNFSeADNPayload (suposição de formato isolada — ver comentários em nfse_adn.go)
// ---------------------------------------------------------------------------

func TestNFSeParseNFSeADNPayload_JSONValido(t *testing.T) {
	body := []byte(`{"loteVazio":true,"ultimoNSU":"10","documentos":[{"nsu":"10","identificadorNfse":"NFSE-1","cnpjPrestador":"11111111000111","cnpjTomador":"22222222000122","dataEmissao":"2026-10-01","valorServicos":150.5}]}`)
	resp, err := parseNFSeADNPayload(body)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !resp.LoteVazio || len(resp.Documentos) != 1 || resp.Documentos[0].IdentificadorNFSe != "NFSE-1" {
		t.Errorf("parse divergiu do esperado: %+v", resp)
	}
}

func TestNFSeParseNFSeADNPayload_JSONInvalido_RetornaErro(t *testing.T) {
	if _, err := parseNFSeADNPayload([]byte("isto nao e json")); err == nil {
		t.Fatal("esperava erro para corpo que não é JSON")
	}
}

func TestNFSeParseNFSeADNPayload_DocumentoSemIdentificador_RetornaErro(t *testing.T) {
	body := []byte(`{"loteVazio":true,"documentos":[{"cnpjPrestador":"11111111000111"}]}`)
	if _, err := parseNFSeADNPayload(body); err == nil {
		t.Fatal("esperava erro para documento sem identificadorNfse")
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
// BuscarDFeADN / BuscarTodosDocumentosADN — servidor de teste mTLS (sem acesso à API real)
// ---------------------------------------------------------------------------

// TestBuscarTodosDocumentosADN_PaginaAteLoteVazio cobre o 3º Acceptance Criterion a nível de
// serviço: um servidor de teste mTLS que aceita o certificado e devolve 2 lotes paginados
// (loteVazio=false depois loteVazio=true) — BuscarTodosDocumentosADN agrega os documentos dos
// 2 lotes.
func TestNFSeBuscarTodosDocumentosADN_PaginaAteLoteVazio(t *testing.T) {
	ca, caKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, ca, caKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	clientCert, clientKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})

	serverTLSCert := tls.Certificate{
		Certificate: [][]byte{serverCert.Raw},
		PrivateKey:  serverKey,
	}
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(ca)

	mux := http.NewServeMux()
	mux.HandleFunc("/contribuintes/DFe/", func(w http.ResponseWriter, r *http.Request) {
		nsu := strings.TrimPrefix(r.URL.Path, "/contribuintes/DFe/")
		w.Header().Set("Content-Type", "application/json")
		switch nsu {
		case "0":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"loteVazio": false,
				"ultimoNSU": "5",
				"documentos": []map[string]interface{}{
					{"nsu": "1", "identificadorNfse": "NFSE-001", "cnpjPrestador": "99999999000199", "cnpjTomador": "12345678000199", "dataEmissao": "2026-10-01", "valorServicos": 100.0},
				},
			})
		case "5":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"loteVazio": true,
				"ultimoNSU": "5",
				"documentos": []map[string]interface{}{
					{"nsu": "6", "identificadorNfse": "NFSE-002", "cnpjPrestador": "12345678000199", "cnpjTomador": "88888888000188", "dataEmissao": "2026-10-02", "valorServicos": 200.0},
				},
			})
		default:
			t.Errorf("NSU inesperado na chamada de teste: %q", nsu)
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	ts := startNFSeADNTestServer(t, mux, serverTLSCert, clientCAPool)
	defer ts.Close()

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	httpClient, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-correta"})
	if err != nil {
		t.Fatalf("falha ao montar client mTLS: %v", err)
	}
	trustServerCA(t, httpClient, ca)

	docs, err := BuscarTodosDocumentosADN(context.Background(), httpClient, ts.URL)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("esperava 2 documentos agregados dos 2 lotes, veio %d", len(docs))
	}
	if docs[0].IdentificadorNFSe != "NFSE-001" || docs[1].IdentificadorNFSe != "NFSE-002" {
		t.Errorf("documentos inesperados: %+v", docs)
	}
}

// TestBuscarDFeADN_CertificadoRejeitado_RetornaErroAutenticacao cobre o 4º Acceptance
// Criterion a nível de serviço: um servidor de teste que REJEITA o certificado (assinado por
// uma CA diferente da que o servidor confia) — o handshake mTLS falha e BuscarDFeADN retorna
// um erro que envolve ErrNFSeADNAutenticacaoFalhou.
func TestNFSeBuscarDFeADN_CertificadoRejeitado_RetornaErroAutenticacao(t *testing.T) {
	serverCA, serverCAKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, serverCA, serverCAKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}

	// CA DIFERENTE da que assina o certificado do cliente abaixo — servidor nunca vai confiar
	// no certificado apresentado.
	trustedClientCA, _ := genTestCA(t)
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(trustedClientCA)

	// Certificado do cliente assinado por uma CA diferente (não está no pool acima).
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

	_, err = BuscarDFeADN(context.Background(), httpClient, ts.URL, "0")
	if err == nil {
		t.Fatal("esperava erro — certificado do cliente não é confiado pelo servidor")
	}
	if !errors.Is(err, ErrNFSeADNAutenticacaoFalhou) {
		t.Errorf("esperava erro envolvendo ErrNFSeADNAutenticacaoFalhou, veio: %v", err)
	}
}

// TestBuscarDFeADN_HTTP401_RetornaErroAutenticacao cobre a outra metade da mesma linha da I/O
// Matrix: handshake OK mas a API responde 401/403.
func TestNFSeBuscarDFeADN_HTTP401_RetornaErroAutenticacao(t *testing.T) {
	ca, caKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, ca, caKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}
	clientCert, clientKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(ca)

	ts := startNFSeADNTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"erro":"nao autorizado"}`))
	}), serverTLSCert, clientCAPool)
	defer ts.Close()

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	httpClient, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-correta"})
	if err != nil {
		t.Fatalf("falha ao montar client mTLS: %v", err)
	}
	trustServerCA(t, httpClient, ca)

	_, err = BuscarDFeADN(context.Background(), httpClient, ts.URL, "0")
	if !errors.Is(err, ErrNFSeADNAutenticacaoFalhou) {
		t.Errorf("esperava erro envolvendo ErrNFSeADNAutenticacaoFalhou para HTTP 401, veio: %v", err)
	}
}

// TestBuscarDFeADN_FormatoInesperado_RetornaErroFormato cobre a linha "Resposta em formato
// inesperado" da I/O Matrix: servidor aceita o certificado (handshake OK) mas devolve um corpo
// que o parser não reconhece.
func TestNFSeBuscarDFeADN_FormatoInesperado_RetornaErroFormato(t *testing.T) {
	ca, caKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, ca, caKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}
	clientCert, clientKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(ca)

	ts := startNFSeADNTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("isto nao e o JSON esperado"))
	}), serverTLSCert, clientCAPool)
	defer ts.Close()

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	httpClient, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-correta"})
	if err != nil {
		t.Fatalf("falha ao montar client mTLS: %v", err)
	}
	trustServerCA(t, httpClient, ca)

	_, err = BuscarDFeADN(context.Background(), httpClient, ts.URL, "0")
	if !errors.Is(err, ErrNFSeADNFormatoInesperado) {
		t.Errorf("esperava erro envolvendo ErrNFSeADNFormatoInesperado, veio: %v", err)
	}
}

// TestNFSeBuscarTodosDocumentosADN_NuncaLoteVazio_RetornaErroAoAtingirLimite cobre o caso em
// que o ADN nunca sinaliza loteVazio=true (ex.: divergência da suposição de paginação
// documentada em NFSeADNResponse) — o servidor de teste sempre avança ultimoNSU sem nunca
// parar. BuscarTodosDocumentosADN precisa parar ao atingir nfseADNMaxLotes e retornar um erro
// explícito, em vez de rodar indefinidamente ou devolver sucesso com uma captura incompleta.
func TestNFSeBuscarTodosDocumentosADN_NuncaLoteVazio_RetornaErroAoAtingirLimite(t *testing.T) {
	ca, caKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, ca, caKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	clientCert, clientKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})

	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(ca)

	chamadas := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/contribuintes/DFe/", func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		w.Header().Set("Content-Type", "application/json")
		// loteVazio nunca é true e ultimoNSU sempre avança — simula um ADN que diverge da
		// suposição de paginação e nunca sinaliza o fim do ciclo.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"loteVazio":  false,
			"ultimoNSU":  strings.TrimPrefix(r.URL.Path, "/contribuintes/DFe/") + "x",
			"documentos": []map[string]interface{}{},
		})
	})

	ts := startNFSeADNTestServer(t, mux, serverTLSCert, clientCAPool)
	defer ts.Close()

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	httpClient, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: pfxB64, Senha: "senha-correta"})
	if err != nil {
		t.Fatalf("falha ao montar client mTLS: %v", err)
	}
	trustServerCA(t, httpClient, ca)

	_, err = BuscarTodosDocumentosADN(context.Background(), httpClient, ts.URL)
	if err == nil {
		t.Fatal("esperava erro ao atingir nfseADNMaxLotes sem loteVazio=true, não houve")
	}
	if !strings.Contains(err.Error(), "limite de") {
		t.Errorf("esperava mensagem de erro mencionando o limite de lotes atingido, veio: %v", err)
	}
	if chamadas != nfseADNMaxLotes {
		t.Errorf("esperava exatamente %d chamadas (nfseADNMaxLotes), veio %d", nfseADNMaxLotes, chamadas)
	}
}
