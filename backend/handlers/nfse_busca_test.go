package handlers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// ---------------------------------------------------------------------------
// Helpers de certificado de teste — gerados aqui mesmo (autoassinados), NUNCA um certificado
// A1 real nem a API real do Portal Nacional ADN (mesmo princípio de
// services/nfse_adn_test.go; helpers de teste não são compartilhados entre pacotes Go).
// ---------------------------------------------------------------------------

func nfseBuscaGenTestCA(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("falha ao gerar chave da CA de teste: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "CA de Teste NFS-e Busca"},
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

type nfseBuscaLeafOpts struct {
	commonName string
	isServer   bool
	ipAddrs    []net.IP
}

func nfseBuscaGenTestLeaf(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, opts nfseBuscaLeafOpts) (*x509.Certificate, *rsa.PrivateKey) {
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

func nfseBuscaBuildTestPfxBase64(t *testing.T, cert *x509.Certificate, key *rsa.PrivateKey, senha string) string {
	t.Helper()
	pfxBytes, err := pkcs12.Encode(rand.Reader, key, cert, nil, senha)
	if err != nil {
		t.Fatalf("falha ao empacotar .pfx de teste: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pfxBytes)
}

// nfseBuscaWriteCAPEM grava o certificado da CA de teste em um arquivo PEM temporário e
// aponta NFSE_ADN_TLS_CA_FILE para ele — é assim (ver comentário da constante em
// services/nfse_adn.go) que o client mTLS montado DENTRO do handler passa a confiar no
// certificado autoassinado do servidor de teste, sem alterar nenhum código de produção (que
// usa o pool de CAs padrão do sistema quando essa variável não está definida).
func nfseBuscaWriteCAPEM(t *testing.T, ca *x509.Certificate) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca-teste-nfse-busca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("falha ao gravar PEM da CA de teste: %v", err)
	}
	t.Setenv("NFSE_ADN_TLS_CA_FILE", path)
}

// nfseBuscaStartADNTestServer sobe um httptest.Server com mTLS EXIGIDO (ClientAuth:
// tls.RequireAndVerifyClientCert), certificado autoassinado gerado no teste — nunca a API real
// do ADN, conforme pedido pela spec-nfse-cliente-adn.md.
func nfseBuscaStartADNTestServer(t *testing.T, handler http.Handler, serverCert tls.Certificate, clientCAPool *x509.CertPool) *httptest.Server {
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

func postBuscarNFSe(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/nfse/buscar", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	BuscarNFSeHandler(db)(rec, req)
	return rec
}

func countNFSeRows(t *testing.T, db *sql.DB, tabela, companyID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+tabela+" WHERE company_id = $1", companyID).Scan(&count); err != nil {
		t.Fatalf("falha ao contar linhas de %s: %v", tabela, err)
	}
	return count
}

// ---------------------------------------------------------------------------
// Testes — pré-DB (sem necessidade de Postgres)
// ---------------------------------------------------------------------------

func TestBuscarNFSeHandlerUnauthorized(t *testing.T) {
	handler := BuscarNFSeHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/nfse/buscar", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestBuscarNFSeHandlerMethodNotAllowed(t *testing.T) {
	handler := BuscarNFSeHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nfse/buscar", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Testes — integração com Postgres (skip automático se indisponível)
// ---------------------------------------------------------------------------

// TestBuscarNFSeHandler_SemCertificado_Retorna400SemChamadaRede cobre o 1º Acceptance
// Criterion: empresa sem certificado cadastrado -> 400 SEM tentar nenhuma chamada de rede.
// NFSE_ADN_BASE_URL aponta para uma porta fechada: se o handler mudar de comportamento e
// tentar uma chamada de rede antes de checar o certificado, o teste deixaria de ver 400 (veria
// 502, já que a conexão seria recusada) — é essa divergência que detecta a regressão.
func TestBuscarNFSeHandler_SemCertificado_Retorna400SemChamadaRede(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	t.Setenv("NFSE_ADN_BASE_URL", "http://127.0.0.1:1")

	rec := postBuscarNFSe(t, db, authed)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (sem certificado, sem tentar rede), obteve %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "certificado") {
		t.Errorf("mensagem de erro deveria mencionar o certificado ausente, veio: %s", rec.Body.String())
	}
}

// TestBuscarNFSeHandler_CertificadoVencido_Retorna400SemChamadaRede cobre o 2º Acceptance
// Criterion: certificado com status calculado 'vencido' -> 400 SEM tentar a chamada mTLS.
func TestBuscarNFSeHandler_CertificadoVencido_Retorna400SemChamadaRede(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	t.Setenv("NFSE_ADN_BASE_URL", "http://127.0.0.1:1")

	passado := time.Now().AddDate(0, 0, -5)
	insertNFSeCert(t, db, companyID, validPfxBase64Content, "senha-teste", "vencido.pfx", &passado, "ativo")

	rec := postBuscarNFSe(t, db, authed)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (certificado vencido, sem tentar mTLS), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestBuscarNFSeHandler_ServidorAceita_GravaDocumentosSemDuplicarNaSegundaChamada cobre o 3º
// Acceptance Criterion: servidor de teste mTLS aceita o certificado e devolve um payload
// simulado -> os documentos aparecem em nfse_entradas/nfse_saidas, sem duplicar numa 2ª
// chamada.
func TestBuscarNFSeHandler_ServidorAceita_GravaDocumentosSemDuplicarNaSegundaChamada(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	const cnpjEmpresa = "12345678000199"

	ca, caKey := nfseBuscaGenTestCA(t)
	serverCert, serverKey := nfseBuscaGenTestLeaf(t, ca, caKey, nfseBuscaLeafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	clientCert, clientKey := nfseBuscaGenTestLeaf(t, ca, caKey, nfseBuscaLeafOpts{commonName: "EMPRESA TESTE LTDA:" + cnpjEmpresa})

	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(ca)

	chamadas := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/contribuintes/DFe/", func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"loteVazio": true,
			"ultimoNSU": "1",
			"documentos": []map[string]interface{}{
				{
					"nsu": "1", "identificadorNfse": "NFSE-ENTRADA-001",
					"cnpjPrestador": "99999999000199", "cnpjTomador": cnpjEmpresa,
					"dataEmissao": "2026-10-01", "valorServicos": 100.0,
				},
				{
					"nsu": "2", "identificadorNfse": "NFSE-SAIDA-001",
					"cnpjPrestador": cnpjEmpresa, "cnpjTomador": "88888888000188",
					"dataEmissao": "2026-10-02", "valorServicos": 200.0,
				},
			},
		})
	})

	ts := nfseBuscaStartADNTestServer(t, mux, serverTLSCert, clientCAPool)
	defer ts.Close()

	nfseBuscaWriteCAPEM(t, ca)
	t.Setenv("NFSE_ADN_BASE_URL", ts.URL)

	pfxB64 := nfseBuscaBuildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCert(t, db, companyID, pfxB64, "senha-correta", "empresa.pfx", &futuro, "ativo")

	rec1 := postBuscarNFSe(t, db, authed)
	if rec1.Code != http.StatusOK {
		t.Fatalf("1ª busca: esperava 200, obteve %d: %s", rec1.Code, rec1.Body.String())
	}
	var body1 struct {
		DocumentosNovos     int `json:"documentos_novos"`
		DocumentosRecebidos int `json:"documentos_recebidos"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &body1); err != nil {
		t.Fatalf("falha ao decodificar resposta: %v, corpo: %s", err, rec1.Body.String())
	}
	if body1.DocumentosNovos != 2 {
		t.Errorf("esperava 2 documentos novos na 1ª busca, veio %d", body1.DocumentosNovos)
	}

	if n := countNFSeRows(t, db, "nfse_entradas", companyID); n != 1 {
		t.Errorf("esperava 1 linha em nfse_entradas (empresa é tomadora), veio %d", n)
	}
	if n := countNFSeRows(t, db, "nfse_saidas", companyID); n != 1 {
		t.Errorf("esperava 1 linha em nfse_saidas (empresa é prestadora), veio %d", n)
	}

	// Este teste cobre idempotência entre 2 ciclos manuais, não o rate limit (patch
	// separado, ver NFSeBuscaRL) — sem isto, a 2ª chamada abaixo levaria 429 em vez de 200.
	NFSeBuscaRL.Reset(companyID)

	// 2ª chamada: o servidor de teste devolve o MESMO lote de novo (simula um 2º ciclo manual
	// "Buscar Agora") — não deve duplicar nenhuma linha.
	rec2 := postBuscarNFSe(t, db, authed)
	if rec2.Code != http.StatusOK {
		t.Fatalf("2ª busca: esperava 200, obteve %d: %s", rec2.Code, rec2.Body.String())
	}
	var body2 struct {
		DocumentosNovos int `json:"documentos_novos"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("falha ao decodificar resposta da 2ª busca: %v, corpo: %s", err, rec2.Body.String())
	}
	if body2.DocumentosNovos != 0 {
		t.Errorf("esperava 0 documentos novos na 2ª busca (já capturados), veio %d", body2.DocumentosNovos)
	}
	if n := countNFSeRows(t, db, "nfse_entradas", companyID); n != 1 {
		t.Errorf("2ª busca duplicou nfse_entradas — esperava 1 linha, veio %d", n)
	}
	if n := countNFSeRows(t, db, "nfse_saidas", companyID); n != 1 {
		t.Errorf("2ª busca duplicou nfse_saidas — esperava 1 linha, veio %d", n)
	}
	if chamadas != 2 {
		t.Errorf("esperava 2 chamadas ao servidor de teste (1 por busca), veio %d", chamadas)
	}
}

// TestBuscarNFSeHandler_ServidorRejeitaCertificado_Retorna502SemGravarNada cobre o 4º
// Acceptance Criterion: servidor de teste rejeita o certificado (handshake falha) -> 502, nada
// gravado no banco.
func TestBuscarNFSeHandler_ServidorRejeitaCertificado_Retorna502SemGravarNada(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	serverCA, serverCAKey := nfseBuscaGenTestCA(t)
	serverCert, serverKey := nfseBuscaGenTestLeaf(t, serverCA, serverCAKey, nfseBuscaLeafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}

	// CA que o servidor confia para certificados de CLIENTE — DIFERENTE da que assina o
	// certificado da empresa abaixo, então o handshake mTLS tem que falhar.
	trustedClientCA, _ := nfseBuscaGenTestCA(t)
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(trustedClientCA)

	untrustedCA, untrustedCAKey := nfseBuscaGenTestCA(t)
	clientCert, clientKey := nfseBuscaGenTestLeaf(t, untrustedCA, untrustedCAKey, nfseBuscaLeafOpts{commonName: "EMPRESA TESTE LTDA:12345678000199"})

	ts := nfseBuscaStartADNTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handshake deveria ter falhado antes de qualquer requisição chegar ao handler do servidor de teste")
	}), serverTLSCert, clientCAPool)
	defer ts.Close()

	nfseBuscaWriteCAPEM(t, serverCA)
	t.Setenv("NFSE_ADN_BASE_URL", ts.URL)

	pfxB64 := nfseBuscaBuildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCert(t, db, companyID, pfxB64, "senha-correta", "empresa.pfx", &futuro, "ativo")

	rec := postBuscarNFSe(t, db, authed)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("esperava 502 (certificado rejeitado pelo servidor), obteve %d: %s", rec.Code, rec.Body.String())
	}

	if n := countNFSeRows(t, db, "nfse_entradas", companyID); n != 0 {
		t.Errorf("certificado rejeitado não deveria gravar nada em nfse_entradas, veio %d linha(s)", n)
	}
	if n := countNFSeRows(t, db, "nfse_saidas", companyID); n != 0 {
		t.Errorf("certificado rejeitado não deveria gravar nada em nfse_saidas, veio %d linha(s)", n)
	}
}
