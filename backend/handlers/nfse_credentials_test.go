package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"fb_apu02/crypto"
	"fb_apu02/services"

	"github.com/golang-jwt/jwt/v5"
	"software.sslmate.com/src/go-pkcs12"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// validPfxBase64 é um valor base64 válido (não precisa ser um .pfx/PKCS#12 real —
// a validação do handler só confirma que é base64 decodificável, nunca faz parsing
// de PKCS#12, que está fora de escopo desta sub-spec).
//
// NOTA (spec-nfse-pkcs12-normalizacao.md): desde que SaveNFSeCertificadoHandler passou a
// chamar services.NormalizarPKCS12ParaDER — que de fato decodifica o PKCS#12 — este conteúdo
// "fake" não é mais suficiente para os testes que esperam um upload aceito (201/200): nem
// pkcs12.Decode nem o fallback via openssl conseguem processar texto arbitrário, então o
// handler passaria a responder 400. Os testes abaixo que esperam sucesso usam
// genValidPfxBase64(t, senha) (certificado de teste real, DER-compatível) em vez desta
// constante. validPfxBase64Content/validPfxBase64() continuam existindo só para os poucos
// testes que nunca chegam a invocar a normalização (ex.: validação de base64 malformado,
// checagem de role via AuthMiddleware) ou que testam leitura de linhas já inseridas
// diretamente no banco (insertNFSeCert), nunca passando pelo handler de upload.
const validPfxBase64Content = "conteudo-fake-de-certificado-pfx-para-teste"

func validPfxBase64() string {
	return base64.StdEncoding.EncodeToString([]byte(validPfxBase64Content))
}

// genValidPfxBase64 gera um certificado .pfx (PKCS#12) de teste VÁLIDO e DER-compatível —
// decodifica de primeira com pkcs12.Decode, sem precisar do fallback via openssl — empacotado
// com a senha dada, em base64 (mesmo formato que SaveNFSeCertificadoHandler recebe no corpo
// JSON). Usado pelos testes de SaveNFSeCertificadoHandler que precisam que o upload seja
// ACEITO (201/200): desde a normalização automática (spec-nfse-pkcs12-normalizacao.md), o
// handler decodifica o PKCS#12 de verdade, então certificado_senha precisa ser a senha que
// realmente destranca o .pfx gerado aqui.
func genValidPfxBase64(t *testing.T, senha string) string {
	t.Helper()
	return genValidPfxBase64CN(t, senha, "certificado-teste-nfse")
}

// genValidPfxBase64CN é genValidPfxBase64 com o Common Name dado (ex.: "EMPRESA:CNPJ", formato
// ICP-Brasil de onde o upload extrai cnpj_titular).
func genValidPfxBase64CN(t *testing.T, senha, cn string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("falha ao gerar chave de teste: %v", err)
	}
	pki := pkiTesteNFSe(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	// Assinado pela AC intermediária de teste, que vai junto no .pfx (cadeia completa, sem
	// download) — desde spec-nfse-cadeia-icp-brasil.md o upload exige cadeia válida até a raiz.
	der, err := x509.CreateCertificate(rand.Reader, tmpl, pki.inter, &key.PublicKey, pki.interKey)
	if err != nil {
		t.Fatalf("falha ao criar certificado de teste: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("falha ao parsear certificado de teste: %v", err)
	}
	pfxBytes, err := pkcs12.Modern.Encode(key, cert, []*x509.Certificate{pki.inter}, senha)
	if err != nil {
		t.Fatalf("falha ao empacotar .pfx de teste: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pfxBytes)
}

// ---------------------------------------------------------------------------
// PKI de teste para a validação de cadeia ICP-Brasil (spec-nfse-cadeia-icp-brasil.md)
// ---------------------------------------------------------------------------

// pkiTeste: raiz falsa -> AC intermediária falsa. Ninguém tem a chave de uma raiz ICP-Brasil
// real, então os testes deste pacote trocam completarCadeiaNFSe (ver usarCadeiaTesteNFSe) por
// uma validação real (x509.Verify) contra esta raiz. A lógica de AIA/anti-SSRF/raízes
// embutidas é coberta em services/nfse_cadeia_test.go.
type pkiTeste struct {
	raiz, inter       *x509.Certificate
	raizKey, interKey *rsa.PrivateKey
}

var (
	pkiTesteOnce sync.Once
	pkiTesteVal  pkiTeste
	pkiTesteErr  error
)

func gerarCATeste(cn string, emissor *x509.Certificate, emissorKey *rsa.PrivateKey) (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(48 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	parent, signer := tmpl, key
	if emissor != nil {
		parent, signer = emissor, emissorKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		return nil, nil, err
	}
	c, err := x509.ParseCertificate(der)
	return c, key, err
}

func pkiTesteNFSe(t *testing.T) pkiTeste {
	t.Helper()
	pkiTesteOnce.Do(func() {
		var p pkiTeste
		if p.raiz, p.raizKey, pkiTesteErr = gerarCATeste("Raiz Falsa de Teste", nil, nil); pkiTesteErr != nil {
			return
		}
		if p.inter, p.interKey, pkiTesteErr = gerarCATeste("AC Intermediaria Falsa de Teste", p.raiz, p.raizKey); pkiTesteErr != nil {
			return
		}
		pkiTesteVal = p
	})
	if pkiTesteErr != nil {
		t.Fatalf("falha ao gerar PKI de teste: %v", pkiTesteErr)
	}
	return pkiTesteVal
}

// assinaturaCadeiaNFSe — assinatura de completarCadeiaNFSe (= services.CompletarCadeiaICPBrasil).
type assinaturaCadeiaNFSe = func(context.Context, *x509.Certificate, []*x509.Certificate) ([]*x509.Certificate, error)

// stubCadeiaNFSe valida de verdade (x509.Verify, KeyUsages clientAuth como a produção) contra a
// raiz de teste, com as intermediárias do .pfx; se não fechar, "baixa" das publicadas SÓ o que
// for emissor real de algum certificado já conhecido (Subject == Issuer e assinatura conferida,
// como baixarEmissorAIA na produção — nunca qualquer coisa publicada), contando em downloads, e
// tenta de novo (até 5 rodadas). Devolve as intermediárias da cadeia sem o final e sem a raiz,
// como a função real.
func stubCadeiaNFSe(raiz *x509.Certificate, downloads *int, publicadas ...*x509.Certificate) assinaturaCadeiaNFSe {
	return func(_ context.Context, cert *x509.Certificate, inter []*x509.Certificate) ([]*x509.Certificate, error) {
		roots := x509.NewCertPool()
		roots.AddCert(raiz)
		pool := x509.NewCertPool()
		conhecidas := append([]*x509.Certificate(nil), inter...)
		for _, c := range conhecidas {
			pool.AddCert(c)
		}
		opts := x509.VerifyOptions{Roots: roots, Intermediates: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		chains, err := cert.Verify(opts)
		for rodada := 0; err != nil && rodada < 5; rodada++ {
			novos := 0
			for _, c := range append([]*x509.Certificate{cert}, conhecidas...) {
				for _, pub := range publicadas {
					jaConhecida := false
					for _, k := range conhecidas {
						if bytes.Equal(k.Raw, pub.Raw) {
							jaConhecida = true
							break
						}
					}
					if jaConhecida || !bytes.Equal(pub.RawSubject, c.RawIssuer) || c.CheckSignatureFrom(pub) != nil {
						continue
					}
					conhecidas = append(conhecidas, pub)
					pool.AddCert(pub)
					novos++
				}
			}
			if novos == 0 {
				break
			}
			if downloads != nil {
				*downloads += novos
			}
			chains, err = cert.Verify(opts)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", services.ErrNFSeCadeiaNaoICPBrasil, err)
		}
		c := chains[0]
		return append([]*x509.Certificate(nil), c[1:len(c)-1]...), nil
	}
}

// usarCadeiaTesteNFSe troca completarCadeiaNFSe por f (restaurado no fim do teste).
func usarCadeiaTesteNFSe(t *testing.T, f assinaturaCadeiaNFSe) {
	t.Helper()
	prev := completarCadeiaNFSe
	completarCadeiaNFSe = f
	t.Cleanup(func() { completarCadeiaNFSe = prev })
}

// openNFSeTestDB abre conexão com o Postgres de teste (reaproveita openTestDB,
// definido em rfb_pagamentos_fornecedores_test.go) e pula o teste se a migration 133
// ainda não tiver sido aplicada nesse banco.
func openNFSeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'nfse_certificados')`).Scan(&exists); err != nil || !exists {
		db.Close()
		t.Skip("tabela nfse_certificados não existe — rode as migrations (133) antes deste teste")
	}
	// Migration 134 (cursor NSU/XML/cancelamento): lida por services.ExecutarCicloNFSe e pela
	// listagem (nfse_list.go).
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'nfse_certificados' AND column_name = 'ultimo_nsu')`).Scan(&exists); err != nil || !exists {
		db.Close()
		t.Skip("coluna nfse_certificados.ultimo_nsu não existe — rode a migration 134 antes deste teste")
	}
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'nfse_adn_falhas')`).Scan(&exists); err != nil || !exists {
		db.Close()
		t.Skip("tabela nfse_adn_falhas não existe — rode a versão atual da migration 134 antes deste teste")
	}
	// Migration 135 (IBS/CBS + xml_parser_versao): gravada pelo processor e lida pela listagem.
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'nfse_saidas' AND column_name = 'xml_parser_versao')`).Scan(&exists); err != nil || !exists {
		db.Close()
		t.Skip("coluna nfse_saidas.xml_parser_versao não existe — rode a migration 135 antes deste teste")
	}
	return db
}

// setupNFSeCertTest cria empresa + usuário dono (necessário para GetEffectiveCompanyID) e
// devolve um wrapper que injeta ClaimsKey + X-Company-ID na requisição — mesmo padrão de
// setupCGIBSHabilitarTest (cgibs_habilitar_test.go).
func setupNFSeCertTest(t *testing.T, db *sql.DB) (companyID string, authed func(*http.Request) *http.Request, cleanup func()) {
	t.Helper()
	// Upload valida a cadeia até a raiz — por padrão, contra a raiz de teste (ver pkiTeste).
	usarCadeiaTesteNFSe(t, stubCadeiaNFSe(pkiTesteNFSe(t).raiz, nil))
	companyID, cleanupCompany := setupTestCompany(t, db)

	var userID string
	email := fmt.Sprintf("nfse-cert-%d@teste.local", time.Now().UnixNano())
	if err := db.QueryRow(`INSERT INTO users (email, password_hash, full_name) VALUES ($1, 'x', 'teste') RETURNING id`, email).Scan(&userID); err != nil {
		cleanupCompany()
		t.Fatalf("falha ao criar user de teste: %v", err)
	}
	if _, err := db.Exec(`UPDATE companies SET owner_id = $1 WHERE id = $2`, userID, companyID); err != nil {
		t.Fatalf("falha ao definir owner da company de teste: %v", err)
	}

	authed = func(r *http.Request) *http.Request {
		ctx := context.WithValue(r.Context(), ClaimsKey, jwt.MapClaims{"user_id": userID})
		r = r.WithContext(ctx)
		r.Header.Set("X-Company-ID", companyID)
		return r
	}
	cleanup = func() {
		db.Exec(`DELETE FROM nfse_certificados WHERE company_id = $1`, companyID)
		cleanupCompany()
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}
	return
}

// insertNFSeCert insere uma linha nfse_certificados de teste com os campos sensíveis já
// criptografados (mesmo formato gravado por SaveNFSeCertificadoHandler).
func insertNFSeCert(t *testing.T, db *sql.DB, companyID, pfxPlain, senhaPlain, nomeArquivo string, validade *time.Time, status string) {
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

func postSaveNFSeCert(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("falha ao serializar corpo: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/nfse/certificado", bytes.NewReader(b))
	req = authed(req)
	rec := httptest.NewRecorder()
	SaveNFSeCertificadoHandler(db)(rec, req)
	return rec
}

func getNFSeCert(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/nfse/certificado", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	GetNFSeCertificadoHandler(db)(rec, req)
	return rec
}

func deleteNFSeCert(t *testing.T, db *sql.DB, authed func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/nfse/certificado", nil)
	req = authed(req)
	rec := httptest.NewRecorder()
	DeleteNFSeCertificadoHandler(db)(rec, req)
	return rec
}

func decodeNFSeCertBody(t *testing.T, rec *httptest.ResponseRecorder) NFSeCertificado {
	t.Helper()
	var body struct {
		Certificado NFSeCertificado `json:"certificado"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v, corpo: %s", err, rec.Body.String())
	}
	return body.Certificado
}

// ---------------------------------------------------------------------------
// Testes — pré-DB (sem necessidade de Postgres)
// ---------------------------------------------------------------------------

func TestGetNFSeCertificadoHandlerUnauthorized(t *testing.T) {
	handler := GetNFSeCertificadoHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/nfse/certificado", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestSaveNFSeCertificadoHandlerUnauthorized(t *testing.T) {
	handler := SaveNFSeCertificadoHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/nfse/certificado", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

func TestSaveNFSeCertificadoHandlerMethodNotAllowed(t *testing.T) {
	handler := SaveNFSeCertificadoHandler(nil)
	req := httptest.NewRequest(http.MethodPut, "/api/nfse/certificado", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("esperava HTTP 405, obteve %d", rr.Code)
	}
}

func TestDeleteNFSeCertificadoHandlerUnauthorized(t *testing.T) {
	handler := DeleteNFSeCertificadoHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/nfse/certificado", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava HTTP 401, obteve %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Testes — integração com Postgres (skip automático se indisponível)
// ---------------------------------------------------------------------------

// TestNFSeSchemaMigration_TabelasNovasExistem cobre o critério de aceite "migration 133
// cria as tabelas e elas existem vazias, sem quebrar nenhum handler existente".
func TestNFSeSchemaMigration_TabelasNovasExistem(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()

	for _, tbl := range []string{"nfse_certificados", "nfse_entradas", "nfse_saidas"} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, tbl).Scan(&exists); err != nil {
			t.Fatalf("erro ao verificar tabela %s: %v", tbl, err)
		}
		if !exists {
			t.Fatalf("tabela %s não foi criada pela migration 133", tbl)
		}
	}
}

// TestSaveNFSeCertificadoHandler_PrimeiroCadastroSemPfxOuSenha_Retorna400 cobre a linha
// "Primeiro cadastro sem senha ou sem arquivo" da I/O Matrix: sem registro anterior para
// preservar, certificado_pfx/certificado_senha continuam obrigatórios.
func TestSaveNFSeCertificadoHandler_PrimeiroCadastroSemPfxOuSenha_Retorna400(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   "",
		"certificado_senha": "",
		"validade":          "2027-01-01",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (certificado/senha obrigatórios no 1º cadastro), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSaveNFSeCertificadoHandler_PrimeiroCadastroValido_Retorna201ENaoExpoeSegredos cobre o
// 1º Acceptance Criterion: upload válido -> leitura seguinte nunca expõe certificado_pfx nem
// certificado_senha, só tem_certificado + metadados.
func TestSaveNFSeCertificadoHandler_PrimeiroCadastroValido_Retorna201ENaoExpoeSegredos(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	pfx := genValidPfxBase64(t, "senha-secreta-123")
	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   pfx,
		"certificado_senha": "senha-secreta-123",
		"nome_arquivo":      "certificado.pfx",
		"validade":          "2027-06-15",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("1º cadastro: esperava 201, obteve %d: %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if bytes.Contains([]byte(raw), []byte(pfx)) || bytes.Contains([]byte(raw), []byte("senha-secreta-123")) {
		t.Fatal("resposta do save contém certificado/senha em texto puro — nunca deveria vazar")
	}
	saved := decodeNFSeCertBody(t, rec)
	if !saved.TemCertificado {
		t.Error("esperava tem_certificado=true na resposta do save")
	}

	getRec := getNFSeCert(t, db, authed)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET após save: esperava 200, obteve %d: %s", getRec.Code, getRec.Body.String())
	}
	rawGet := getRec.Body.String()
	if bytes.Contains([]byte(rawGet), []byte(pfx)) || bytes.Contains([]byte(rawGet), []byte("senha-secreta-123")) {
		t.Fatal("resposta do GET contém certificado/senha em texto puro — nunca deveria vazar")
	}
	cert := decodeNFSeCertBody(t, getRec)
	if !cert.TemCertificado {
		t.Error("esperava tem_certificado=true no GET")
	}
	if cert.NomeArquivo != "certificado.pfx" {
		t.Errorf("esperava nome_arquivo=certificado.pfx, veio %q", cert.NomeArquivo)
	}
	if cert.Validade == nil || *cert.Validade != "2027-06-15" {
		t.Errorf("esperava validade=2027-06-15, veio %+v", cert.Validade)
	}
	if cert.Status != "ativo" {
		t.Errorf("esperava status=ativo, veio %q", cert.Status)
	}
}

// TestGetNFSeCertificadoHandler_SemCertificado_Retorna200TemCertificadoFalse cobre o 2º
// Acceptance Criterion: empresa sem certificado cadastrado -> HTTP 200 com
// tem_certificado:false (nunca 404/500).
func TestGetNFSeCertificadoHandler_SemCertificado_Retorna200TemCertificadoFalse(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec := getNFSeCert(t, db, authed)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}
	cert := decodeNFSeCertBody(t, rec)
	if cert.TemCertificado {
		t.Error("esperava tem_certificado=false para empresa sem certificado cadastrado")
	}
}

// TestSaveNFSeCertificadoHandler_ReUpload_SubstituiNaoDuplica cobre o 3º Acceptance
// Criterion: um novo upload para a mesma empresa substitui o registro (UPSERT), não duplica,
// e um re-upload completo retorna 200 (não 201, pois já existe registro).
func TestSaveNFSeCertificadoHandler_ReUpload_SubstituiNaoDuplica(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec1 := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   genValidPfxBase64(t, "senha-original"),
		"certificado_senha": "senha-original",
		"validade":          "2027-01-01",
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("1º save: esperava 201, obteve %d: %s", rec1.Code, rec1.Body.String())
	}

	rec2 := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   genValidPfxBase64(t, "senha-NOVA"),
		"certificado_senha": "senha-NOVA",
		"validade":          "2028-01-01",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("re-upload: esperava 200 (update, não criação), obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&count); err != nil {
		t.Fatalf("falha ao contar registros: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava exatamente 1 registro após re-upload (UPSERT), encontrou %d", count)
	}

	var senhaEnc string
	if err := db.QueryRow(`SELECT certificado_senha FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&senhaEnc); err != nil {
		t.Fatalf("falha ao ler certificado_senha: %v", err)
	}
	if crypto.DecryptFieldWithFallback(senhaEnc) != "senha-NOVA" {
		t.Error("re-upload deveria ter substituído a senha pela nova")
	}
}

// TestSaveNFSeCertificadoHandler_ReUploadSemValidade_PreservaValidadeAnterior cobre
// diretamente o gap #2 do loopback 1 (Spec Change Log) e o 6º Acceptance Criterion: re-upload
// sem informar validade preserva o valor já persistido em vez de apagá-lo com NULL.
func TestSaveNFSeCertificadoHandler_ReUploadSemValidade_PreservaValidadeAnterior(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec1 := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   genValidPfxBase64(t, "senha-original"),
		"certificado_senha": "senha-original",
		"validade":          "2027-03-10",
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("1º save: esperava 201, obteve %d: %s", rec1.Code, rec1.Body.String())
	}

	// Re-upload trocando só a senha, sem informar validade.
	rec2 := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   genValidPfxBase64(t, "senha-rotacionada"),
		"certificado_senha": "senha-rotacionada",
		"validade":          "",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("re-upload sem validade: esperava 200, obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	var validade sql.NullTime
	if err := db.QueryRow(`SELECT validade FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&validade); err != nil {
		t.Fatalf("falha ao ler validade persistida: %v", err)
	}
	if !validade.Valid {
		t.Fatal("validade foi apagada (NULL) por um re-upload que não a informou — deveria preservar o valor anterior")
	}
	if validade.Time.Format("2006-01-02") != "2027-03-10" {
		t.Errorf("esperava validade preservada=2027-03-10, veio %s", validade.Time.Format("2006-01-02"))
	}

	cert := decodeNFSeCertBody(t, getNFSeCert(t, db, authed))
	if cert.Validade == nil || *cert.Validade != "2027-03-10" {
		t.Errorf("GET após re-upload sem validade: esperava validade=2027-03-10, veio %+v", cert.Validade)
	}
}

// TestSaveNFSeCertificadoHandler_ReUploadSemPfxSenha_PreservaValoresAnteriores cobre a linha
// "Re-upload parcial" da I/O Matrix para certificado_pfx/certificado_senha: reenviar esses
// campos vazios (ex.: só corrigindo nome_arquivo) preserva os valores já criptografados.
func TestSaveNFSeCertificadoHandler_ReUploadSemPfxSenha_PreservaValoresAnteriores(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	pfxOriginal := genValidPfxBase64(t, "senha-que-deve-sobreviver")
	rec1 := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   pfxOriginal,
		"certificado_senha": "senha-que-deve-sobreviver",
		"nome_arquivo":      "original.pfx",
		"validade":          "2027-05-05",
	})
	if rec1.Code != http.StatusCreated {
		t.Fatalf("1º save: esperava 201, obteve %d: %s", rec1.Code, rec1.Body.String())
	}
	// Desde spec-nfse-cadeia-icp-brasil.md o upload persiste o .pfx RE-ENCODADO (com a cadeia
	// validada), não os bytes enviados — o que precisa sobreviver ao save parcial é o valor
	// persistido pelo 1º save.
	var pfxEnc1 string
	if err := db.QueryRow(`SELECT certificado_pfx FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&pfxEnc1); err != nil {
		t.Fatalf("falha ao ler certificado_pfx do 1º save: %v", err)
	}
	pfxPersistido1 := crypto.DecryptFieldWithFallback(pfxEnc1)
	if pfxPersistido1 == "" {
		t.Fatal("1º save deveria ter persistido certificado_pfx")
	}

	rec2 := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   "",
		"certificado_senha": "",
		"nome_arquivo":      "renomeado.pfx",
		"validade":          "2027-05-05",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("re-upload parcial: esperava 200, obteve %d: %s", rec2.Code, rec2.Body.String())
	}

	var pfxEnc, senhaEnc string
	if err := db.QueryRow(`SELECT certificado_pfx, certificado_senha FROM nfse_certificados WHERE company_id = $1`, companyID).
		Scan(&pfxEnc, &senhaEnc); err != nil {
		t.Fatalf("falha ao ler campos persistidos: %v", err)
	}
	if crypto.DecryptFieldWithFallback(pfxEnc) != pfxPersistido1 {
		t.Error("certificado_pfx foi sobrescrito por um save com o campo vazio — deveria preservar")
	}
	if crypto.DecryptFieldWithFallback(senhaEnc) != "senha-que-deve-sobreviver" {
		t.Error("certificado_senha foi sobrescrito por um save com o campo vazio — deveria preservar")
	}

	cert := decodeNFSeCertBody(t, getNFSeCert(t, db, authed))
	if cert.NomeArquivo != "renomeado.pfx" {
		t.Errorf("esperava nome_arquivo atualizado para renomeado.pfx, veio %q", cert.NomeArquivo)
	}
}

// TestSaveNFSeCertificadoHandler_CertificadoPfxBase64Invalido_Retorna400 cobre a validação de
// que certificado_pfx precisa ser base64 decodificável (achado "patch" do loopback 1).
func TestSaveNFSeCertificadoHandler_CertificadoPfxBase64Invalido_Retorna400(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   "!!!isto-nao-e-base64-valido!!!",
		"certificado_senha": "senha-qualquer",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (certificado_pfx não é base64 válido), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSaveNFSeCertificadoHandler_ValidadeInvalidaRoundTrip_Retorna400 cobre a validação de
// round-trip: time.Parse aceita silenciosamente datas de calendário inexistentes (ex.
// 2026-02-30, normalizada para 2026-03-02) — o handler precisa rejeitar isso com 400 em vez de
// aceitar uma data diferente da digitada.
func TestSaveNFSeCertificadoHandler_ValidadeInvalidaRoundTrip_Retorna400(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   validPfxBase64(),
		"certificado_senha": "senha-qualquer",
		"validade":          "2026-02-30",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400 (30/02 não existe, round-trip deveria detectar), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetNFSeCertificadoHandler_CertificadoVencido_StatusCalculadoVencido cobre a linha
// "Certificado vencido" da I/O Matrix: validade < NOW() é reportado como status='vencido' na
// leitura mesmo que a coluna ainda diga 'ativo'.
func TestGetNFSeCertificadoHandler_CertificadoVencido_StatusCalculadoVencido(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	passado := time.Now().AddDate(0, 0, -10)
	insertNFSeCert(t, db, companyID, validPfxBase64Content, "senha-teste", "vencido.pfx", &passado, "ativo")

	rec := getNFSeCert(t, db, authed)
	if rec.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", rec.Code, rec.Body.String())
	}
	cert := decodeNFSeCertBody(t, rec)
	if cert.Status != "vencido" {
		t.Errorf("esperava status calculado=vencido para validade passada, veio %q", cert.Status)
	}
}

// TestDeleteNFSeCertificadoHandler_RemoveCertificado cobre a exclusão simples do certificado
// da própria empresa.
func TestDeleteNFSeCertificadoHandler_RemoveCertificado(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	futuro := time.Now().AddDate(1, 0, 0)
	insertNFSeCert(t, db, companyID, validPfxBase64Content, "senha-teste", "a-excluir.pfx", &futuro, "ativo")

	rec := deleteNFSeCert(t, db, authed)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperava 204, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&count); err != nil {
		t.Fatalf("falha ao contar registros: %v", err)
	}
	if count != 0 {
		t.Error("certificado não foi removido pelo DELETE")
	}
}

// TestDeleteNFSeCertificadoHandler_SemCertificado_Retorna404 confirma que excluir quando não
// há certificado cadastrado não é tratado como sucesso silencioso.
func TestDeleteNFSeCertificadoHandler_SemCertificado_Retorna404(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec := deleteNFSeCert(t, db, authed)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperava 404 (nenhum certificado para excluir), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestNFSeCertificado_IsolamentoEntreTenants_EmpresaBNaoAcessaCertificadoDaEmpresaA cobre o 5º
// Acceptance Criterion: a empresa B (outro company_id, outro usuário, outro
// environment/enterprise_group — tenants genuinamente não relacionados) não consegue ler nem
// excluir o certificado cadastrado pela empresa A, mesmo forçando X-Company-ID para o id da
// empresa A.
func TestNFSeCertificado_IsolamentoEntreTenants_EmpresaBNaoAcessaCertificadoDaEmpresaA(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()

	companyA, authedA, cleanupA := setupNFSeCertTest(t, db)
	defer cleanupA()
	companyB, authedB, cleanupB := setupNFSeCertTest(t, db)
	defer cleanupB()

	recSave := postSaveNFSeCert(t, db, authedA, map[string]interface{}{
		"certificado_pfx":   genValidPfxBase64(t, "segredo-da-empresa-a"),
		"certificado_senha": "segredo-da-empresa-a",
		"nome_arquivo":      "empresa-a.pfx",
		"validade":          "2027-07-07",
	})
	if recSave.Code != http.StatusCreated {
		t.Fatalf("save empresa A: esperava 201, obteve %d: %s", recSave.Code, recSave.Body.String())
	}

	// Usuário B autenticado como ele mesmo, mas forjando o header X-Company-ID para o id da
	// empresa A — GetEffectiveCompanyID deve recusar (usuário B não é dono nem membro do
	// grupo/ambiente da empresa A) e cair de volta para a própria empresa B.
	forjado := func(r *http.Request) *http.Request {
		r = authedB(r)
		r.Header.Set("X-Company-ID", companyA)
		return r
	}

	getRec := getNFSeCert(t, db, forjado)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET forjado: esperava 200 (cai para a própria empresa B), obteve %d: %s", getRec.Code, getRec.Body.String())
	}
	certB := decodeNFSeCertBody(t, getRec)
	if certB.TemCertificado {
		t.Fatal("GET com X-Company-ID forjado para empresa A devolveu um certificado — isolamento de tenant violado")
	}
	if certB.NomeArquivo == "empresa-a.pfx" {
		t.Fatal("GET forjado devolveu metadado do certificado da empresa A")
	}

	deleteRec := deleteNFSeCert(t, db, forjado)
	if deleteRec.Code == http.StatusNoContent {
		// Só seria aceitável se essa exclusão tivesse atingido a própria empresa B (que não tem
		// certificado) — o que já deveria ter retornado 404, não 204. Qualquer 204 aqui é
		// suspeito o bastante para falhar o teste.
		t.Fatal("DELETE forjado retornou 204 — verificar se não excluiu o certificado da empresa A")
	}

	// O certificado da empresa A precisa continuar intacto, acessível pela própria empresa A.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nfse_certificados WHERE company_id = $1`, companyA).Scan(&count); err != nil {
		t.Fatalf("falha ao contar registros da empresa A: %v", err)
	}
	if count != 1 {
		t.Fatal("certificado da empresa A não está mais presente após operações forjadas pela empresa B — isolamento de tenant violado")
	}

	certA := decodeNFSeCertBody(t, getNFSeCert(t, db, authedA))
	if !certA.TemCertificado || certA.NomeArquivo != "empresa-a.pfx" {
		t.Errorf("certificado da empresa A deveria continuar intacto, veio %+v", certA)
	}
	_ = companyB
}

// TestSaveNFSeCertificadoHandler_IsolamentoEntreTenants_SaveForjadoNaoAfetaCertificadoDaEmpresaA
// cobre o caminho de SAVE (POST) do mesmo cenário de isolamento de tenant já exercitado para
// GET/DELETE acima: a empresa B (outro company_id, outro usuário, outro
// environment/enterprise_group) não consegue afetar o certificado da empresa A mesmo forçando
// X-Company-ID para o id da empresa A em um POST. GetEffectiveCompanyID recusa o header forjado
// (usuário B não é dono/membro da empresa A) e cai de volta para a própria empresa B — então o
// POST forjado deve criar/atualizar apenas o certificado da empresa B, nunca o da empresa A.
func TestSaveNFSeCertificadoHandler_IsolamentoEntreTenants_SaveForjadoNaoAfetaCertificadoDaEmpresaA(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()

	companyA, authedA, cleanupA := setupNFSeCertTest(t, db)
	defer cleanupA()
	companyB, authedB, cleanupB := setupNFSeCertTest(t, db)
	defer cleanupB()

	recSave := postSaveNFSeCert(t, db, authedA, map[string]interface{}{
		"certificado_pfx":   genValidPfxBase64(t, "segredo-da-empresa-a"),
		"certificado_senha": "segredo-da-empresa-a",
		"nome_arquivo":      "empresa-a.pfx",
		"validade":          "2027-07-07",
	})
	if recSave.Code != http.StatusCreated {
		t.Fatalf("save empresa A: esperava 201, obteve %d: %s", recSave.Code, recSave.Body.String())
	}

	forjado := func(r *http.Request) *http.Request {
		r = authedB(r)
		r.Header.Set("X-Company-ID", companyA)
		return r
	}

	recForjado := postSaveNFSeCert(t, db, forjado, map[string]interface{}{
		"certificado_pfx":   genValidPfxBase64(t, "senha-forjada-pela-empresa-b"),
		"certificado_senha": "senha-forjada-pela-empresa-b",
		"nome_arquivo":      "forjado.pfx",
		"validade":          "2030-01-01",
	})
	// Empresa B ainda não tinha certificado próprio, então este é um 1º cadastro (201) PARA A
	// PRÓPRIA EMPRESA B — nunca uma atualização (200) do certificado da empresa A.
	if recForjado.Code != http.StatusCreated {
		t.Fatalf("save forjado: esperava 201 (cria para a própria empresa B, não para a A), obteve %d: %s", recForjado.Code, recForjado.Body.String())
	}

	certA := decodeNFSeCertBody(t, getNFSeCert(t, db, authedA))
	if !certA.TemCertificado || certA.NomeArquivo != "empresa-a.pfx" {
		t.Fatalf("certificado da empresa A foi alterado por um POST forjado pela empresa B: %+v", certA)
	}

	var senhaEncA string
	if err := db.QueryRow(`SELECT certificado_senha FROM nfse_certificados WHERE company_id = $1`, companyA).Scan(&senhaEncA); err != nil {
		t.Fatalf("falha ao ler certificado_senha da empresa A: %v", err)
	}
	if crypto.DecryptFieldWithFallback(senhaEncA) != "segredo-da-empresa-a" {
		t.Fatal("senha da empresa A foi sobrescrita por um POST forjado pela empresa B — isolamento de tenant violado")
	}

	var countA int
	if err := db.QueryRow(`SELECT COUNT(*) FROM nfse_certificados WHERE company_id = $1`, companyA).Scan(&countA); err != nil {
		t.Fatalf("falha ao contar registros da empresa A: %v", err)
	}
	if countA != 1 {
		t.Fatal("deveria existir exatamente 1 registro para a empresa A após o POST forjado pela empresa B")
	}

	certB := decodeNFSeCertBody(t, getNFSeCert(t, db, authedB))
	if !certB.TemCertificado || certB.NomeArquivo != "forjado.pfx" {
		t.Fatalf("esperava que o POST forjado tivesse criado o certificado da própria empresa B, veio %+v", certB)
	}
	_ = companyB
}

// ---------------------------------------------------------------------------
// Testes — AuthMiddleware (composição real usada em main.go)
// ---------------------------------------------------------------------------

// TestSaveNFSeCertificadoHandler_AuthMiddlewareNaoAdmin_Retorna403 exercita a composição exata
// usada em main.go — handlers.AuthMiddleware(handlers.SaveNFSeCertificadoHandler(db), "admin") —
// com um JWT de usuário não-admin. Até este teste, todos os testes deste arquivo chamavam o
// handler diretamente injetando ClaimsKey no contexto, nunca passando pelo AuthMiddleware de
// verdade, então a exigência de role admin (causa raiz do loopback 1) não tinha nenhuma
// cobertura automatizada.
func TestSaveNFSeCertificadoHandler_AuthMiddlewareNaoAdmin_Retorna403(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, _, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	token, err := GenerateToken("usuario-nao-admin-de-teste", "user")
	if err != nil {
		t.Fatalf("falha ao gerar token de teste: %v", err)
	}

	handler := AuthMiddleware(SaveNFSeCertificadoHandler(db), "admin")

	body, err := json.Marshal(map[string]interface{}{
		"certificado_pfx":   validPfxBase64(),
		"certificado_senha": "senha-qualquer",
		"validade":          "2027-01-01",
	})
	if err != nil {
		t.Fatalf("falha ao serializar corpo: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/nfse/certificado", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperava 403 (role não-admin via AuthMiddleware), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Testes — normalização PKCS#12 BER->DER no upload (spec-nfse-pkcs12-normalizacao.md)
// ---------------------------------------------------------------------------

// mangleToIndefiniteLengthBER reescreve o byte de comprimento da SEQUENCE externa de um .pfx
// DER válido para "indefinite length" (0x80 BER) e adiciona o marcador de fim-de-conteúdo
// (0x00 0x00) — reproduz o erro real de produção ("asn1: syntax error: indefinite length found
// (not DER)") sem depender de uma fixture de certificado real. Mesma técnica usada e validada
// em services/nfse_pkcs12_test.go (duplicada aqui porque é um helper não-exportado do outro
// pacote — ver convenção do projeto de não criar barris cross-package).
func mangleToIndefiniteLengthBER(t *testing.T, pfxBytes []byte) []byte {
	t.Helper()
	if len(pfxBytes) < 2 || pfxBytes[0] != 0x30 {
		t.Fatalf("esperava .pfx começando com SEQUENCE (0x30), veio %x", pfxBytes[:2])
	}
	var headerLen int
	if pfxBytes[1]&0x80 == 0 {
		headerLen = 2
	} else {
		numLenBytes := int(pfxBytes[1] & 0x7F)
		headerLen = 2 + numLenBytes
	}
	content := pfxBytes[headerLen:]
	mangled := append([]byte{0x30, 0x80}, content...)
	mangled = append(mangled, 0x00, 0x00)
	return mangled
}

// genBERPfxBase64 gera um .pfx de teste válido e o transforma em BER sintético (indefinite
// length), devolvendo em base64 — mesmo formato aceito no corpo JSON do upload. Usado para
// testar, através do handler de ponta a ponta, a mesma normalização BER->DER que corrigiu o
// certificado real da Ferreira Costa (ver Intent da spec).
func genBERPfxBase64(t *testing.T, senha string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("falha ao gerar chave de teste: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "certificado-teste-ber"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	// Assinado pela RAIZ de teste diretamente (cadeia fecha sem intermediária) — mantém o .pfx
	// com exatamente 2 safe bags, pré-condição do pkcs12.Decode abaixo.
	pki := pkiTesteNFSe(t)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, pki.raiz, &key.PublicKey, pki.raizKey)
	if err != nil {
		t.Fatalf("falha ao criar certificado de teste: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("falha ao parsear certificado de teste: %v", err)
	}
	pfxBytes, err := pkcs12.Modern.Encode(key, cert, nil, senha)
	if err != nil {
		t.Fatalf("falha ao empacotar .pfx de teste: %v", err)
	}
	if _, _, err := pkcs12.Decode(pfxBytes, senha); err != nil {
		t.Fatalf("pré-condição falhou: .pfx original deveria decodificar direto: %v", err)
	}
	ber := mangleToIndefiniteLengthBER(t, pfxBytes)
	if _, _, err := pkcs12.Decode(ber, senha); err == nil {
		t.Fatal("pré-condição falhou: .pfx BER sintético deveria falhar no pkcs12.Decode direto")
	}
	return base64.StdEncoding.EncodeToString(ber)
}

// TestSaveNFSeCertificadoHandler_CertificadoBER_NormalizaEPersisteDER cobre o 1º Acceptance
// Criterion de ponta a ponta através do handler real: um certificado .pfx BER-encoded válido
// (senha correta) é aceito (201) e o valor persistido em certificado_pfx já decodifica direto
// com pkcs12.Decode — ou seja, já normalizado para DER — exatamente o cenário do certificado
// real da Ferreira Costa que motivou esta spec.
func TestSaveNFSeCertificadoHandler_CertificadoBER_NormalizaEPersisteDER(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	senha := "senha-do-certificado-ber"
	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   genBERPfxBase64(t, senha),
		"certificado_senha": senha,
		"nome_arquivo":      "certificado-ber.pfx",
		"validade":          "2027-09-09",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload de certificado BER: esperava 201, obteve %d: %s", rec.Code, rec.Body.String())
	}

	var pfxEnc string
	if err := db.QueryRow(`SELECT certificado_pfx FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&pfxEnc); err != nil {
		t.Fatalf("falha ao ler certificado_pfx persistido: %v", err)
	}
	pfxPersistidoB64 := crypto.DecryptFieldWithFallback(pfxEnc)
	pfxPersistido, err := base64.StdEncoding.DecodeString(pfxPersistidoB64)
	if err != nil {
		t.Fatalf("certificado_pfx persistido não é base64 válido: %v", err)
	}
	if _, _, err := pkcs12.Decode(pfxPersistido, senha); err != nil {
		t.Fatalf("certificado_pfx persistido deveria decodificar DIRETO com pkcs12.Decode (já normalizado para DER), veio erro: %v", err)
	}
}

// TestSaveNFSeCertificadoHandler_CertificadoJaDER_NaoRegride cobre o 4º Acceptance Criterion:
// um certificado já DER-compatível continua funcionando exatamente como antes (sem regressão),
// sem precisar do fallback via openssl — a normalização aqui é só o caminho rápido
// (pkcs12.Decode direto).
func TestSaveNFSeCertificadoHandler_CertificadoJaDER_NaoRegride(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   genValidPfxBase64(t, "senha-der-compativel"),
		"certificado_senha": "senha-der-compativel",
		"nome_arquivo":      "ja-der.pfx",
		"validade":          "2027-10-10",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("certificado já DER-compatível: esperava 201 (sem regressão), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSaveNFSeCertificadoHandler_SenhaIncorreta_Retorna400 cobre o 2º Acceptance Criterion:
// senha incorreta (pkcs12.Decode falha E o fallback via openssl também falha, por MAC/senha)
// deve retornar 400 com mensagem clara, nunca 500 nem confundir com "openssl ausente" — exercita
// diretamente a chamada real a services.NormalizarPKCS12ParaDER feita por
// SaveNFSeCertificadoHandler (não um mock), usando um .pfx de teste de verdade.
func TestSaveNFSeCertificadoHandler_SenhaIncorreta_Retorna400(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	pfx := genValidPfxBase64(t, "senha-verdadeira")
	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   pfx,
		"certificado_senha": "senha-ERRADA-de-proposito",
		"nome_arquivo":      "senha-errada.pfx",
		"validade":          "2027-01-01",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("senha incorreta: esperava 400, obteve %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("falha ao decodificar corpo do erro: %v", err)
	}
	if strings.Contains(strings.ToLower(body["error"]), "openssl") {
		t.Errorf("mensagem de senha incorreta nunca deveria mencionar openssl (confundiria o usuário com um problema de ambiente): %q", body["error"])
	}
}

// TestSaveNFSeCertificadoHandler_OpensslAusente_Retorna500 cobre o 3º Acceptance Criterion: com
// o binário openssl ausente do ambiente (simulado via PATH vazio, sem desinstalar o openssl do
// ambiente de CI/dev de verdade) e um certificado BER (que precisaria do fallback), o handler
// responde 500 — nunca confundido com senha incorreta (400).
func TestSaveNFSeCertificadoHandler_OpensslAusente_Retorna500(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	_, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	senha := "senha-sem-openssl"
	pfxBase64 := genBERPfxBase64(t, senha) // BER de propósito: precisaria do fallback via openssl

	t.Setenv("PATH", "")

	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx":   pfxBase64,
		"certificado_senha": senha,
		"nome_arquivo":      "sem-openssl.pfx",
		"validade":          "2027-01-01",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("openssl ausente: esperava 500 (problema de ambiente, não do usuário), obteve %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Cadeia ICP-Brasil no upload (spec-nfse-cadeia-icp-brasil.md)
// ---------------------------------------------------------------------------

// genPfxComExtras gera um certificado final assinado pela AC intermediária de teste e devolve
// o .pfx com o final + extras (extras=nil reproduz o caso real do .pfx de 4 KB sem ACs).
func genPfxComExtras(t *testing.T, senha string, extras []*x509.Certificate) (pfxB64 string, final *x509.Certificate) {
	t.Helper()
	pki := pkiTesteNFSe(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "EMPRESA TESTE LTDA:12345678000199"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, pki.inter, &key.PublicKey, pki.interKey)
	if err != nil {
		t.Fatal(err)
	}
	final, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pfx, err := pkcs12.Modern.Encode(key, final, extras, senha)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pfx), final
}

func lerPfxPersistido(t *testing.T, db *sql.DB, companyID, senha string) (*x509.Certificate, []*x509.Certificate) {
	t.Helper()
	var pfxEnc string
	if err := db.QueryRow(`SELECT certificado_pfx FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&pfxEnc); err != nil {
		t.Fatalf("falha ao ler certificado_pfx persistido: %v", err)
	}
	pfx, err := base64.StdEncoding.DecodeString(crypto.DecryptFieldWithFallback(pfxEnc))
	if err != nil {
		t.Fatalf("certificado_pfx persistido não é base64: %v", err)
	}
	_, leaf, cas, err := pkcs12.DecodeChain(pfx, senha)
	if err != nil {
		t.Fatalf("certificado_pfx persistido não decodifica com DecodeChain: %v", err)
	}
	return leaf, cas
}

// AC 1: certificado final sem intermediárias + AIA válida -> o .pfx persistido decodifica com
// DecodeChain trazendo a cadeia completa até antes da raiz.
func TestSaveNFSeCertificadoHandler_SoCertificadoFinal_PersistePfxComCadeiaCompleta(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()
	pki := pkiTesteNFSe(t)
	downloads := 0
	usarCadeiaTesteNFSe(t, stubCadeiaNFSe(pki.raiz, &downloads, pki.inter))

	pfxB64, final := genPfxComExtras(t, "senha-cadeia", nil)
	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx": pfxB64, "certificado_senha": "senha-cadeia", "nome_arquivo": "so-final.pfx", "validade": "2027-01-01",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperava 201, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if downloads == 0 {
		t.Fatal("pré-condição: a cadeia deveria ter sido completada via AIA (simulada)")
	}
	leaf, cas := lerPfxPersistido(t, db, companyID, "senha-cadeia")
	if !bytes.Equal(leaf.Raw, final.Raw) {
		t.Fatal("leaf persistido deveria ser o certificado final")
	}
	if len(cas) != 1 || !bytes.Equal(cas[0].Raw, pki.inter.Raw) {
		t.Fatalf("esperava a AC intermediária (e só ela, sem a raiz) no .pfx persistido, veio %d certificado(s)", len(cas))
	}
}

// AC 4 + "raiz nunca vai pro .pfx": cadeia já completa -> nenhum download, e a raiz enviada
// junto no .pfx é descartada.
func TestSaveNFSeCertificadoHandler_CadeiaJaCompleta_NaoBaixaERemoveRaiz(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()
	pki := pkiTesteNFSe(t)
	downloads := 0
	usarCadeiaTesteNFSe(t, stubCadeiaNFSe(pki.raiz, &downloads, pki.inter))

	pfxB64, _ := genPfxComExtras(t, "senha-completa", []*x509.Certificate{pki.inter, pki.raiz})
	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx": pfxB64, "certificado_senha": "senha-completa", "nome_arquivo": "completo.pfx", "validade": "2027-01-01",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperava 201, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if downloads != 0 {
		t.Fatalf("cadeia já completa: nenhum download esperado, houve %d", downloads)
	}
	_, cas := lerPfxPersistido(t, db, companyID, "senha-completa")
	if len(cas) != 1 || !bytes.Equal(cas[0].Raw, pki.inter.Raw) {
		t.Fatalf("esperava só a AC intermediária (raiz removida) no .pfx persistido, veio %d certificado(s)", len(cas))
	}
}

// Cadeia que não fecha com a raiz de teste (sem AIA simulada) -> 400 e nada persistido.
func TestSaveNFSeCertificadoHandler_CadeiaNaoFecha_Retorna400ENaoSalva(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	pfxB64, _ := genPfxComExtras(t, "senha-x", nil) // sem intermediária, stub padrão não "baixa"
	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx": pfxB64, "certificado_senha": "senha-x", "nome_arquivo": "x.pfx", "validade": "2027-01-01",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d: %s", rec.Code, rec.Body.String())
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&n)
	if n != 0 {
		t.Fatal("certificado com cadeia inválida nunca deveria ser salvo")
	}
}

// Mapeamento das sentinelas de services.CompletarCadeiaICPBrasil para HTTP (I/O Matrix), sem
// vazar detalhe técnico (URL, IP, openssl) na mensagem, e sem persistir nada.
func TestSaveNFSeCertificadoHandler_ErrosDeCadeia_MapeamentoHTTP(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()

	detalhe := " (http://127.0.0.1:8080/ac.crt 169.254.169.254 openssl stderr)"
	casos := []struct {
		nome      string
		err       error
		status    int
		contido   string
		proibidos []string
	}{
		{"AIA bloqueada", fmt.Errorf("%w%s", services.ErrNFSeCadeiaAIABloqueada, detalhe), http.StatusBadRequest, "não pôde ter a cadeia", nil},
		{"download falhou", fmt.Errorf("%w%s", services.ErrNFSeCadeiaDownloadFalhou, detalhe), http.StatusBadGateway, "tente novamente", nil},
		{"AIA invalida (conteudo/formato)", fmt.Errorf("%w%s", services.ErrNFSeCadeiaAIAInvalida, detalhe), http.StatusBadRequest, "verifique com a sua certificadora", []string{"tente novamente", "não é permitido"}},
		{"AIA esquema nao suportado", fmt.Errorf("%w%s", services.ErrNFSeCadeiaAIAEsquemaNaoSuportado, detalhe), http.StatusBadRequest, "esquema não suportado", []string{"não é permitido", "segurança", "bloquead", "tente novamente"}},
		{"nao ICP-Brasil", fmt.Errorf("%w%s", services.ErrNFSeCadeiaNaoICPBrasil, detalhe), http.StatusBadRequest, "não é ICP-Brasil", nil},
		{"vencido", fmt.Errorf("%w%s", services.ErrNFSeCadeiaVencida, detalhe), http.StatusBadRequest, "vencido", nil},
		{"invalida generica", fmt.Errorf("%w%s", services.ErrNFSeCadeiaInvalida, detalhe), http.StatusBadRequest, "raiz ICP-Brasil", nil},
		{"ambiente", fmt.Errorf("%w: %w%s", services.ErrNFSeCadeiaAmbienteIndisponivel, services.ErrNFSeOpensslIndisponivel, detalhe), http.StatusInternalServerError, "contate o suporte", nil},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			companyID, authed, cleanup := setupNFSeCertTest(t, db)
			defer cleanup()
			usarCadeiaTesteNFSe(t, func(context.Context, *x509.Certificate, []*x509.Certificate) ([]*x509.Certificate, error) {
				return nil, c.err
			})

			rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
				"certificado_pfx": genValidPfxBase64(t, "s"), "certificado_senha": "s", "nome_arquivo": "a.pfx", "validade": "2027-01-01",
			})
			if rec.Code != c.status {
				t.Fatalf("esperava %d, obteve %d: %s", c.status, rec.Code, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("corpo não é JSON: %v", err)
			}
			msg := body["error"]
			if !strings.Contains(msg, c.contido) {
				t.Errorf("mensagem %q deveria conter %q", msg, c.contido)
			}
			for _, p := range c.proibidos {
				if strings.Contains(msg, p) {
					t.Errorf("mensagem %q não deveria conter %q", msg, p)
				}
			}
			for _, vazado := range []string{"http://", "127.0.0.1", "169.254", "openssl", "stderr"} {
				if strings.Contains(strings.ToLower(msg), vazado) {
					t.Errorf("mensagem ao usuário vazou detalhe técnico %q: %q", vazado, msg)
				}
			}
			var n int
			db.QueryRow(`SELECT COUNT(*) FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&n)
			if n != 0 {
				t.Fatal("nada deveria ser persistido quando a cadeia não é validada")
			}
		})
	}
}

// O stub só "baixa" o que é emissor real (como a produção): uma AC publicada que não assina o
// certificado não fecha a cadeia -> 400, sem nenhum "download".
func TestSaveNFSeCertificadoHandler_StubNaoBaixaACNaoRelacionada(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()
	pki := pkiTesteNFSe(t)
	naoRelacionada, _, err := gerarCATeste("AC Nao Relacionada", pki.raiz, pki.raizKey)
	if err != nil {
		t.Fatal(err)
	}
	downloads := 0
	usarCadeiaTesteNFSe(t, stubCadeiaNFSe(pki.raiz, &downloads, naoRelacionada))

	pfxB64, _ := genPfxComExtras(t, "senha-nr", nil)
	rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
		"certificado_pfx": pfxB64, "certificado_senha": "senha-nr", "nome_arquivo": "nr.pfx", "validade": "2027-01-01",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d: %s", rec.Code, rec.Body.String())
	}
	if downloads != 0 {
		t.Fatalf("AC que não assina o certificado nunca deveria ser \"baixada\", houve %d", downloads)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&n)
	if n != 0 {
		t.Fatal("certificado com cadeia inválida nunca deveria ser salvo")
	}
}

// genPfxFinalComAIA gera um .pfx só com o certificado final (autoassinado se emissor == nil)
// e AIA opcional, sem intermediária — cadeia que nunca fecha nas raízes ICP-Brasil reais.
func genPfxFinalComAIA(t *testing.T, senha string, aia []string, emissor *x509.Certificate, emissorKey *rsa.PrivateKey) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "EMPRESA FORA DA ICP:12345678000199"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		IssuingCertificateURL: aia,
	}
	parent, signer := tmpl, key
	if emissor != nil {
		parent, signer = emissor, emissorKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pfx, err := pkcs12.Modern.Encode(key, cert, nil, senha)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pfx)
}

// Upload passando pela função REAL services.CompletarCadeiaICPBrasil (sem stub), com as raízes
// ICP-Brasil reais embutidas: cadeias que não fecham falham rápido, sem rede, e mapeiam para 400
// sem persistir nada.
func TestSaveNFSeCertificadoHandler_CadeiaReal_SemStub_Retorna400(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	pki := pkiTesteNFSe(t)

	casos := []struct {
		nome    string
		pfx     func(t *testing.T) string
		contido string
	}{
		{"autoassinado sem AIA", func(t *testing.T) string {
			return genPfxFinalComAIA(t, "s", nil, nil, nil)
		}, "não é ICP-Brasil"},
		{"emitido por AC de outra PKI sem AIA", func(t *testing.T) string {
			pfx, _ := genPfxComExtras(t, "s", nil)
			return pfx
		}, "não é ICP-Brasil"},
		{"AIA para metadata de nuvem", func(t *testing.T) string {
			return genPfxFinalComAIA(t, "s", []string{"http://169.254.169.254/latest/meta-data/"}, pki.inter, pki.interKey)
		}, "não é permitido"},
		{"AIA ldap", func(t *testing.T) string {
			return genPfxFinalComAIA(t, "s", []string{"ldap://ac.exemplo.br/cn=ac"}, pki.inter, pki.interKey)
		}, "esquema não suportado"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			companyID, authed, cleanup := setupNFSeCertTest(t, db)
			defer cleanup()
			usarCadeiaTesteNFSe(t, services.CompletarCadeiaICPBrasil) // função real, raízes reais

			inicio := time.Now()
			rec := postSaveNFSeCert(t, db, authed, map[string]interface{}{
				"certificado_pfx": c.pfx(t), "certificado_senha": "s", "nome_arquivo": "real.pfx", "validade": "2027-01-01",
			})
			if d := time.Since(inicio); d > 5*time.Second {
				t.Fatalf("deveria falhar rápido, sem rede: levou %s", d)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperava 400, obteve %d: %s", rec.Code, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("corpo não é JSON: %v", err)
			}
			if !strings.Contains(body["error"], c.contido) {
				t.Errorf("mensagem %q deveria conter %q", body["error"], c.contido)
			}
			var n int
			db.QueryRow(`SELECT COUNT(*) FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&n)
			if n != 0 {
				t.Fatal("certificado fora da ICP-Brasil nunca deveria ser salvo")
			}
		})
	}
}

// Troca do certificado por outro TITULAR (CNPJ diferente) zera o cursor ultimo_nsu; renovação do
// mesmo titular e atualização sem novo .pfx preservam o cursor.
func TestSaveNFSeCertificadoHandler_TrocaDeCNPJ_ZeraCursor(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()
	companyID, authed, cleanup := setupNFSeCertTest(t, db)
	defer cleanup()

	salvar := func(body map[string]interface{}, wantStatus int) {
		t.Helper()
		if rec := postSaveNFSeCert(t, db, authed, body); rec.Code != wantStatus {
			t.Fatalf("save: esperava %d, obteve %d: %s", wantStatus, rec.Code, rec.Body.String())
		}
	}
	setCursor := func(n int64) {
		t.Helper()
		if _, err := db.Exec(`UPDATE nfse_certificados SET ultimo_nsu = $2 WHERE company_id = $1`, companyID, n); err != nil {
			t.Fatal(err)
		}
	}
	lerEstado := func() (int64, string) {
		t.Helper()
		var n int64
		var cnpj sql.NullString
		if err := db.QueryRow(`SELECT ultimo_nsu, cnpj_titular FROM nfse_certificados WHERE company_id = $1`, companyID).Scan(&n, &cnpj); err != nil {
			t.Fatal(err)
		}
		return n, cnpj.String
	}

	salvar(map[string]interface{}{"certificado_pfx": genValidPfxBase64CN(t, "s1", "EMPRESA A:11111111000191"), "certificado_senha": "s1", "validade": "2027-01-01"}, http.StatusCreated)
	if n, cnpj := lerEstado(); n != 0 || cnpj != "11111111000191" {
		t.Fatalf("1º upload: ultimo_nsu=%d cnpj_titular=%q", n, cnpj)
	}

	// Renovação do MESMO titular: cursor preservado.
	setCursor(500)
	salvar(map[string]interface{}{"certificado_pfx": genValidPfxBase64CN(t, "s2", "EMPRESA A:11111111000191"), "certificado_senha": "s2", "validade": "2028-01-01"}, http.StatusOK)
	if n, _ := lerEstado(); n != 500 {
		t.Errorf("renovação do mesmo CNPJ não deveria zerar o cursor, veio %d", n)
	}

	// Só validade (sem novo .pfx): cursor e CNPJ preservados.
	salvar(map[string]interface{}{"validade": "2029-01-01"}, http.StatusOK)
	if n, cnpj := lerEstado(); n != 500 || cnpj != "11111111000191" {
		t.Errorf("atualização sem .pfx: ultimo_nsu=%d cnpj_titular=%q", n, cnpj)
	}

	// Outro titular: cursor volta a 0.
	salvar(map[string]interface{}{"certificado_pfx": genValidPfxBase64CN(t, "s3", "EMPRESA B:22222222000192"), "certificado_senha": "s3", "validade": "2028-01-01"}, http.StatusOK)
	if n, cnpj := lerEstado(); n != 0 || cnpj != "22222222000192" {
		t.Errorf("troca de CNPJ deveria zerar o cursor: ultimo_nsu=%d cnpj_titular=%q", n, cnpj)
	}
}
