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
	"testing"
	"time"

	"fb_apu02/crypto"

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
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("falha ao gerar chave de teste: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "certificado-teste-nfse"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
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
	return base64.StdEncoding.EncodeToString(pfxBytes)
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
	return db
}

// setupNFSeCertTest cria empresa + usuário dono (necessário para GetEffectiveCompanyID) e
// devolve um wrapper que injeta ClaimsKey + X-Company-ID na requisição — mesmo padrão de
// setupCGIBSHabilitarTest (cgibs_habilitar_test.go).
func setupNFSeCertTest(t *testing.T, db *sql.DB) (companyID string, authed func(*http.Request) *http.Request, cleanup func()) {
	t.Helper()
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
	if crypto.DecryptFieldWithFallback(pfxEnc) != pfxOriginal {
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
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
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
