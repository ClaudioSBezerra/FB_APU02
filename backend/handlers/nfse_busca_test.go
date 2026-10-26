package handlers

import (
	"bytes"
	"compress/gzip"
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

	"fb_apu02/services"

	"software.sslmate.com/src/go-pkcs12"
)

// ---------------------------------------------------------------------------
// Helpers de certificado de teste — gerados aqui mesmo (autoassinados), NUNCA um certificado
// A1 real nem a API real do Portal Nacional ADN (mesmo princípio de
// services/nfse_adn_test.go; helpers de teste não são compartilhados entre pacotes Go).
//
// A partir da sub-spec spec-nfse-agendamento-persistencia.md, estes testes cobrem só o
// comportamento HTTP do handler (status code, rate limit, formato da resposta JSON e mapeamento
// de erro). A cobertura de persistência/transação/autoemitido/CNPJ-inválido/dedup em si vive em
// services/nfse_processor_test.go, que testa services.ExecutarCicloNFSe diretamente (a mesma
// função que este handler chama) — rejeição/transitório são cobertos aqui mesmo,
// via TestMapExecutarCicloNFSeErr_RejeicaoETransitorio_Retornam502 abaixo, porque são comportamento do
// MAPEAMENTO HTTP (mapExecutarCicloNFSeErr), não da lógica de services.
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
// services/nfse_adn.go) que o client mTLS montado DENTRO de services.ExecutarCicloNFSe passa a
// confiar no certificado autoassinado do servidor de teste, sem alterar nenhum código de
// produção (que usa o pool de CAs padrão do sistema quando essa variável não está definida).
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

// TestBuscarNFSeHandler_SemCertificado_Retorna400SemChamadaRede cobre o mapeamento HTTP de
// services.ErrNFSeCertificadoNaoCadastrado: empresa sem certificado cadastrado -> 400 SEM
// tentar nenhuma chamada de rede. NFSE_ADN_BASE_URL aponta para uma porta fechada: se o ciclo
// mudar de comportamento e tentar uma chamada de rede antes de checar o certificado, o teste
// deixaria de ver 400 (veria 502, já que a conexão seria recusada) — é essa divergência que
// detecta a regressão.
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

// TestBuscarNFSeHandler_CertificadoVencido_Retorna400SemChamadaRede cobre o mapeamento HTTP de
// services.ErrNFSeCertificadoIndisponivel: certificado com status calculado 'vencido' -> 400 SEM
// tentar a chamada mTLS.
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

// TestBuscarNFSeHandler_SegundaChamadaDentro30s_Retorna429 cobre o rate limit compartilhado
// (NFSeBuscaRL, 1 chamada/30s por empresa): a 2ª chamada pra mesma empresa dentro da janela é
// barrada com 429, sem nem chegar a resolver o certificado.
func TestBuscarNFSeHandler_SegundaChamadaDentro30s_Retorna429(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	t.Setenv("NFSE_ADN_BASE_URL", "http://127.0.0.1:1")
	NFSeBuscaRL.Reset(companyID)

	rec1 := postBuscarNFSe(t, db, authed)
	if rec1.Code == http.StatusTooManyRequests {
		t.Fatalf("1ª chamada não deveria estar limitada ainda, veio 429: %s", rec1.Body.String())
	}

	rec2 := postBuscarNFSe(t, db, authed)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("esperava 429 na 2ª chamada dentro de 30s, obteve %d: %s", rec2.Code, rec2.Body.String())
	}
}

// TestBuscarNFSeHandler_ServidorAceita_Retorna200ComContagens cobre o caminho feliz de ponta a
// ponta via HTTP: o handler delega para services.ExecutarCicloNFSe, que autentica via mTLS,
// busca o documento simulado e persiste — o teste confirma que a resposta HTTP é 200 com os
// campos de contagem corretos. Cobertura de transação/dedup/autoemitido/CNPJ-inválido fica em
// services/nfse_processor_test.go (TestExecutarCicloNFSe_*), que testa a mesma função
// diretamente.
func TestBuscarNFSeHandler_ServidorAceita_Retorna200ComContagens(t *testing.T) {
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

	// Formato real do ADN: 1 lote com 1 NFS-e tomada (XML gzip+base64), depois 404 fim da fila.
	xmlNFSe := `<?xml version="1.0" encoding="UTF-8"?><NFSe xmlns="http://www.sped.fazenda.gov.br/nfse" versao="1.00">` +
		`<infNFSe Id="NFS00000000000000000000000000000000000000000000000001"><nNFSe>1</nNFSe><emit><CNPJ>99999999000199</CNPJ></emit>` +
		`<valores><vISSQN>5.00</vISSQN></valores><DPS><infDPS><dhEmi>2026-10-01T10:00:00-03:00</dhEmi><dCompet>2026-10-01</dCompet>` +
		`<prest><CNPJ>99999999000199</CNPJ></prest><toma><CNPJ>` + cnpjEmpresa + `</CNPJ></toma>` +
		`<valores><vServPrest><vServ>100.00</vServ></vServPrest></valores></infDPS></DPS></infNFSe></NFSe>`
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte(xmlNFSe))
	zw.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/contribuintes/DFe/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.TrimPrefix(r.URL.Path, "/contribuintes/DFe/") != "0" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{"StatusProcessamento": "NENHUM_DOCUMENTO_LOCALIZADO", "TipoAmbiente": "HOMOLOGACAO"})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"StatusProcessamento": "DOCUMENTOS_LOCALIZADOS",
			"TipoAmbiente":        "HOMOLOGACAO",
			"LoteDFe": []map[string]interface{}{{
				"NSU": 1, "ChaveAcesso": "00000000000000000000000000000000000000000000000001", "TipoDocumento": "NFSE",
				"ArquivoXml": base64.StdEncoding.EncodeToString(gz.Bytes()), "DataHoraGeracao": "2026-10-01T10:05:00-03:00",
			}},
		})
	})

	ts := nfseBuscaStartADNTestServer(t, mux, serverTLSCert, clientCAPool)
	defer ts.Close()

	nfseBuscaWriteCAPEM(t, ca)
	t.Setenv("NFSE_ADN_BASE_URL", ts.URL)

	pfxB64 := nfseBuscaBuildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCert(t, db, companyID, pfxB64, "senha-correta", "empresa.pfx", &futuro, "ativo")

	NFSeBuscaRL.Reset(companyID)
	rec := postBuscarNFSe(t, db, authed)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		DocumentosNovos     int `json:"documentos_novos"`
		DocumentosIgnorados int `json:"documentos_ignorados"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar resposta: %v, corpo: %s", err, rec.Body.String())
	}
	if body.DocumentosNovos != 1 {
		t.Errorf("esperava 1 documento novo, veio %d", body.DocumentosNovos)
	}
	if body.DocumentosIgnorados != 0 {
		t.Errorf("esperava 0 documentos ignorados, veio %d", body.DocumentosIgnorados)
	}
	if n := countNFSeRows(t, db, "nfse_entradas", companyID); n != 1 {
		t.Errorf("esperava 1 linha em nfse_entradas (empresa é tomadora), veio %d", n)
	}
}

// TestBuscarNFSeHandler_ServidorRejeitaCertificado_Retorna502SemGravarNada cobre o mapeamento
// HTTP de services.ErrNFSeADNAutenticacaoFalhou: servidor de teste rejeita o certificado
// (handshake falha) -> 502, nada gravado no banco.
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

	NFSeBuscaRL.Reset(companyID)
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

// TestMapExecutarCicloNFSeErr_RejeicaoETransitorio_Retornam502: 400 REJEICAO do ADN e
// transitório (429/5xx) que escape do ciclo mapeiam para 502 (I/O Matrix da
// spec-nfse-adn-parser-real.md: "Rejeição → erro mapeado (502 manual)"). O antigo
// ErrNFSeADNLimiteLotes deixou de existir: atingir o limite de lotes agora é sucesso parcial.
func TestMapExecutarCicloNFSeErr_RejeicaoETransitorio_Retornam502(t *testing.T) {
	for _, err := range []error{services.ErrNFSeADNRejeicao, &services.NFSeADNErroTransitorio{StatusHTTP: 429}} {
		rec := httptest.NewRecorder()
		mapExecutarCicloNFSeErr(rec, "company-id-fake", err, services.NFSeResultadoCiclo{})
		if rec.Code != http.StatusBadGateway {
			t.Errorf("esperava 502 para %v, obteve %d: %s", err, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	mapExecutarCicloNFSeErr(rec, "company-id-fake", services.ErrNFSeADNRejeicao, services.NFSeResultadoCiclo{})
	if !strings.Contains(strings.ToLower(rec.Body.String()), "rejeitou") {
		t.Errorf("mensagem deveria indicar rejeição, veio: %s", rec.Body.String())
	}
}

// Ciclo já em andamento para a empresa (ex.: agendado rodando) → 409, sem tocar no ADN.
func TestBuscarNFSeHandler_CicloEmAndamento_Retorna409(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	t.Setenv("NFSE_ADN_BASE_URL", "http://127.0.0.1:1")
	NFSeBuscaRL.Reset(companyID)
	liberar, ok := services.TentarReservarCicloNFSe(companyID)
	if !ok {
		t.Fatal("reserva do ciclo deveria funcionar")
	}
	defer liberar()

	rec := postBuscarNFSe(t, db, authed)
	if rec.Code != http.StatusConflict {
		t.Fatalf("esperava 409, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "em andamento") {
		t.Errorf("mensagem deveria indicar captura em andamento: %s", rec.Body.String())
	}
}

// Sucesso parcial: 200 com parcial=true e motivo_encerramento; transitório esgotado sem nenhum
// lote: 502 com mensagem e contagens; fila esgotada: parcial=false sem motivo.
func TestBuscarNFSeHandler_RespostaParcialE502(t *testing.T) {
	type corpo struct {
		Error               string  `json:"error"`
		Parcial             *bool   `json:"parcial"`
		MotivoEncerramento  *string `json:"motivo_encerramento"`
		DocumentosRecebidos *int    `json:"documentos_recebidos"`
		DocumentosFalhas    *int    `json:"documentos_falhas"`
	}
	responder := func(res services.NFSeResultadoCiclo) (int, corpo) {
		rec := httptest.NewRecorder()
		status, msg := statusRespostaCicloNFSe(res)
		escreverRespostaBuscaNFSe(rec, status, res, msg)
		var b corpo
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatalf("corpo inválido: %v %s", err, rec.Body.String())
		}
		return rec.Code, b
	}

	code, b := responder(services.NFSeResultadoCiclo{Encerramento: services.NFSeADNEncerramentoLimiteLotes, Lotes: 200, Recebidos: 10000, Falhas: 2})
	if code != http.StatusOK || b.Parcial == nil || !*b.Parcial || b.MotivoEncerramento == nil || *b.MotivoEncerramento != "limite_lotes" || *b.DocumentosFalhas != 2 {
		t.Errorf("parcial por limite: code=%d corpo=%+v", code, b)
	}

	code, b = responder(services.NFSeResultadoCiclo{Encerramento: services.NFSeADNEncerramentoTransitorio, Lotes: 1, Recebidos: 50})
	if code != http.StatusOK || !*b.Parcial || *b.MotivoEncerramento != "transitorio_esgotado" {
		t.Errorf("transitório com progresso deveria ser 200 parcial: code=%d corpo=%+v", code, b)
	}

	code, b = responder(services.NFSeResultadoCiclo{Encerramento: services.NFSeADNEncerramentoTransitorio})
	if code != http.StatusBadGateway || b.Error == "" || b.DocumentosRecebidos == nil || b.Parcial == nil {
		t.Errorf("transitório sem nenhum lote deveria ser 502 com contagens: code=%d corpo=%+v", code, b)
	}

	code, b = responder(services.NFSeResultadoCiclo{Encerramento: services.NFSeADNEncerramentoFimDaFila, Lotes: 1})
	if code != http.StatusOK || *b.Parcial || b.MotivoEncerramento != nil {
		t.Errorf("fila esgotada: code=%d corpo=%+v", code, b)
	}

	// Erro também traz as contagens do que já foi gravado.
	rec := httptest.NewRecorder()
	mapExecutarCicloNFSeErr(rec, "company-id-fake", services.ErrNFSeADNRejeicao, services.NFSeResultadoCiclo{Recebidos: 100, Novos: 100, Lotes: 2})
	var e corpo
	json.Unmarshal(rec.Body.Bytes(), &e)
	if rec.Code != http.StatusBadGateway || e.DocumentosRecebidos == nil || *e.DocumentosRecebidos != 100 {
		t.Errorf("erro deveria trazer contagens: code=%d corpo=%s", rec.Code, rec.Body.String())
	}
}
