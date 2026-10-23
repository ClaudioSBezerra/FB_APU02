package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

// ---------------------------------------------------------------------------
// ExecutarCicloNFSe — certificado ausente/vencido (sem chamada de rede)
// ---------------------------------------------------------------------------

func TestExecutarCicloNFSe_SemCertificado_RetornaErroCertificadoNaoCadastrado(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()

	t.Setenv("NFSE_ADN_BASE_URL", "http://127.0.0.1:1")

	_, _, _, err := ExecutarCicloNFSe(context.Background(), db, companyID)
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

	_, _, _, err := ExecutarCicloNFSe(context.Background(), db, companyID)
	if !errors.Is(err, ErrNFSeCertificadoIndisponivel) {
		t.Fatalf("esperava ErrNFSeCertificadoIndisponivel, veio: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExecutarCicloNFSe — ciclo completo contra servidor mTLS de teste
// ---------------------------------------------------------------------------

// TestExecutarCicloNFSe_GravaDocumentosSemDuplicarNoSegundoCiclo cobre: transação (lote inteiro
// de 4 documentos gravado numa única chamada), autoemitido (documento com a empresa como
// prestadora E tomadora grava nos dois lados), CNPJ inválido (documento ignorado, não gravado,
// não quebra o lote) e dedup entre dois ciclos consecutivos (ON CONFLICT DO NOTHING).
func TestExecutarCicloNFSe_GravaDocumentosSemDuplicarNoSegundoCiclo(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	defer db.Exec(`DELETE FROM nfse_certificados WHERE company_id = $1`, companyID)

	const cnpjEmpresa = "12345678000199"

	ca, caKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, ca, caKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	clientCert, clientKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:" + cnpjEmpresa})

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
				{
					"nsu": "3", "identificadorNfse": "NFSE-AUTOEMITIDO-001",
					"cnpjPrestador": cnpjEmpresa, "cnpjTomador": cnpjEmpresa,
					"dataEmissao": "2026-10-03", "valorServicos": 300.0,
				},
				{
					"nsu": "4", "identificadorNfse": "NFSE-CNPJINVALIDO-001",
					"cnpjPrestador": "123", "cnpjTomador": cnpjEmpresa,
					"dataEmissao": "2026-10-04", "valorServicos": 400.0,
				},
			},
		})
	})

	ts := startNFSeADNTestServer(t, mux, serverTLSCert, clientCAPool)
	defer ts.Close()

	writeNFSeCAPEMTeste(t, ca)
	t.Setenv("NFSE_ADN_BASE_URL", ts.URL)

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCertificadoTeste(t, db, companyID, pfxB64, "senha-correta", "empresa.pfx", &futuro, "ativo")

	recebidos1, novos1, ignorados1, err := ExecutarCicloNFSe(context.Background(), db, companyID)
	if err != nil {
		t.Fatalf("1º ciclo: erro inesperado: %v", err)
	}
	// 4 documentos no lote devolvido pelo ADN, independentemente de quantos foram persistidos.
	if recebidos1 != 4 {
		t.Errorf("esperava 4 documentos recebidos no 1º ciclo, veio %d", recebidos1)
	}
	// NFSE-ENTRADA-001 (1) + NFSE-SAIDA-001 (1) + NFSE-AUTOEMITIDO-001 (grava nos dois lados = 2) = 4
	if novos1 != 4 {
		t.Errorf("esperava 4 documentos novos no 1º ciclo, veio %d", novos1)
	}
	if ignorados1 != 1 {
		t.Errorf("esperava 1 documento ignorado (CNPJ prestador inválido) no 1º ciclo, veio %d", ignorados1)
	}

	if n := countNFSeRowsTeste(t, db, "nfse_entradas", companyID); n != 2 { // tomador normal + autoemitido
		t.Errorf("esperava 2 linhas em nfse_entradas (tomador normal + autoemitido), veio %d", n)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_saidas", companyID); n != 2 { // prestador normal + autoemitido
		t.Errorf("esperava 2 linhas em nfse_saidas (prestador normal + autoemitido), veio %d", n)
	}

	// 2º ciclo: o servidor de teste devolve o MESMO lote de novo (simula um 2º ciclo manual ou
	// agendado) — não deve duplicar nenhuma linha.
	recebidos2, novos2, _, err := ExecutarCicloNFSe(context.Background(), db, companyID)
	if err != nil {
		t.Fatalf("2º ciclo: erro inesperado: %v", err)
	}
	if recebidos2 != 4 {
		t.Errorf("esperava 4 documentos recebidos no 2º ciclo (mesmo lote, mesmo que já capturados), veio %d", recebidos2)
	}
	if novos2 != 0 {
		t.Errorf("esperava 0 documentos novos no 2º ciclo (já capturados), veio %d", novos2)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_entradas", companyID); n != 2 {
		t.Errorf("2º ciclo duplicou nfse_entradas — esperava 2 linhas, veio %d", n)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_saidas", companyID); n != 2 {
		t.Errorf("2º ciclo duplicou nfse_saidas — esperava 2 linhas, veio %d", n)
	}
	if chamadas != 2 {
		t.Errorf("esperava 2 chamadas ao servidor de teste (1 por ciclo), veio %d", chamadas)
	}
}

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

	recebidos, novos, ignorados, err := ExecutarCicloNFSe(context.Background(), db, companyID)
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

// TestExecutarCicloNFSe_RecebidosContaDuplicadosMesmoSemNovos cobre especificamente o item 1 do
// Spec Change Log (loopback 1): recebidos é SEMPRE len(documentos) devolvidos pelo ADN, mesmo
// quando TODOS já foram capturados num ciclo anterior (ON CONFLICT DO NOTHING não incrementa
// novos). 2 documentos no 1º ciclo, os mesmos 2 de novo no 2º ciclo — recebidos deve ser 2 nos
// dois ciclos, mesmo que novos seja 0 no segundo.
func TestExecutarCicloNFSe_RecebidosContaDuplicadosMesmoSemNovos(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, cleanup := setupTestCompany(t, db)
	defer cleanup()
	defer db.Exec(`DELETE FROM nfse_certificados WHERE company_id = $1`, companyID)

	const cnpjEmpresa = "12345678000199"

	ca, caKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, ca, caKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	clientCert, clientKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "EMPRESA TESTE LTDA:" + cnpjEmpresa})

	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(ca)

	mux := http.NewServeMux()
	mux.HandleFunc("/contribuintes/DFe/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"loteVazio": true,
			"ultimoNSU": "1",
			"documentos": []map[string]interface{}{
				{
					"nsu": "1", "identificadorNfse": "NFSE-DUP-001",
					"cnpjPrestador": "99999999000199", "cnpjTomador": cnpjEmpresa,
					"dataEmissao": "2026-10-01", "valorServicos": 100.0,
				},
				{
					"nsu": "2", "identificadorNfse": "NFSE-DUP-002",
					"cnpjPrestador": cnpjEmpresa, "cnpjTomador": "88888888000188",
					"dataEmissao": "2026-10-02", "valorServicos": 200.0,
				},
			},
		})
	})

	ts := startNFSeADNTestServer(t, mux, serverTLSCert, clientCAPool)
	defer ts.Close()

	writeNFSeCAPEMTeste(t, ca)
	t.Setenv("NFSE_ADN_BASE_URL", ts.URL)

	pfxB64 := buildTestPfxBase64(t, clientCert, clientKey, "senha-correta")
	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCertificadoTeste(t, db, companyID, pfxB64, "senha-correta", "empresa.pfx", &futuro, "ativo")

	recebidos1, novos1, _, err := ExecutarCicloNFSe(context.Background(), db, companyID)
	if err != nil {
		t.Fatalf("1º ciclo: erro inesperado: %v", err)
	}
	if recebidos1 != 2 {
		t.Errorf("esperava 2 documentos recebidos no 1º ciclo, veio %d", recebidos1)
	}
	if novos1 != 2 {
		t.Errorf("esperava 2 documentos novos no 1º ciclo, veio %d", novos1)
	}

	// 2º ciclo: o ADN devolve o MESMO lote (os mesmos 2 documentos já capturados) — recebidos
	// continua 2 (é o que o governo mandou), mas novos cai para 0 (ON CONFLICT DO NOTHING).
	recebidos2, novos2, _, err := ExecutarCicloNFSe(context.Background(), db, companyID)
	if err != nil {
		t.Fatalf("2º ciclo: erro inesperado: %v", err)
	}
	if recebidos2 != 2 {
		t.Errorf("esperava 2 documentos recebidos no 2º ciclo (duplicados, mas ainda recebidos), veio %d", recebidos2)
	}
	if novos2 != 0 {
		t.Errorf("esperava 0 documentos novos no 2º ciclo (já capturados), veio %d", novos2)
	}
}
