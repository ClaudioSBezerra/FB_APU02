package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// tgTenant é um tenant de teste completo: ambiente + grupo + empresa + usuário membro.
type tgTenant struct {
	envID, groupID, companyID, userID string
}

// newTGTenant cria um tenant isolado. ueRole = papel do usuário em user_environments.
func newTGTenant(t *testing.T, db *sql.DB, label, ueRole string) (tgTenant, func()) {
	t.Helper()
	var tn tgTenant
	suffix := fmt.Sprintf("%s-%d", label, time.Now().UnixNano())

	if err := db.QueryRow(`INSERT INTO environments (name) VALUES ($1) RETURNING id`, "tg-env-"+suffix).Scan(&tn.envID); err != nil {
		t.Fatalf("env: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO enterprise_groups (environment_id, name) VALUES ($1, $2) RETURNING id`, tn.envID, "tg-grp-"+suffix).Scan(&tn.groupID); err != nil {
		t.Fatalf("grupo: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO users (email, password_hash, full_name) VALUES ($1, 'x', 'tg') RETURNING id`, "tg-"+suffix+"@teste.local").Scan(&tn.userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO companies (group_id, name, owner_id) VALUES ($1, $2, $3) RETURNING id`, tn.groupID, "tg-emp-"+suffix, tn.userID).Scan(&tn.companyID); err != nil {
		t.Fatalf("empresa: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_environments (user_id, environment_id, role) VALUES ($1, $2, $3)`, tn.userID, tn.envID, ueRole); err != nil {
		t.Fatalf("user_environments: %v", err)
	}
	return tn, func() {
		db.Exec(`DELETE FROM environments WHERE id = $1`, tn.envID) // cascade: grupos/empresas
		db.Exec(`DELETE FROM users WHERE id = $1`, tn.userID)
	}
}

func tgAs(r *http.Request, userID, role string) *http.Request {
	ctx := context.WithValue(r.Context(), ClaimsKey, jwt.MapClaims{"user_id": userID, "role": role})
	return r.WithContext(ctx)
}

func tgDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	// guarda: as tabelas de hierarquia precisam existir (migrations rodadas)
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'user_environments')`).Scan(&ok); err != nil || !ok {
		t.Skip("user_environments não existe — rode as migrations")
	}
	return db
}

func tgCount(t *testing.T, db *sql.DB, query string, args ...interface{}) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// ─── GetEffectiveCompanyID / CompanyScope ────────────────────────────────────

func TestIsolamentoGetEffectiveCompanyIDNegaEmpresaAlheia(t *testing.T) {
	db := tgDB(t)
	vitima, cleanV := newTGTenant(t, db, "vit", "user")
	defer cleanV()
	atacante, cleanA := newTGTenant(t, db, "atk", "admin")
	defer cleanA()

	// empresa alheia pedida explicitamente: negada, sem fallback para a empresa padrão
	got, err := GetEffectiveCompanyID(db, atacante.userID, vitima.companyID)
	if err != ErrCompanyNotAuthorized || got != "" {
		t.Errorf("empresa alheia: esperava ErrCompanyNotAuthorized, obteve (%q, %v)", got, err)
	}
	// UUID inválido também falha fechado
	if _, err := GetEffectiveCompanyID(db, atacante.userID, "nao-e-uuid"); err != ErrCompanyNotAuthorized {
		t.Errorf("UUID inválido: esperava ErrCompanyNotAuthorized, obteve %v", err)
	}
	// empresa própria e sem header continuam funcionando
	if got, err := GetEffectiveCompanyID(db, atacante.userID, atacante.companyID); err != nil || got != atacante.companyID {
		t.Errorf("empresa própria: obteve (%q, %v)", got, err)
	}
	if got, err := GetEffectiveCompanyID(db, atacante.userID, ""); err != nil || got != atacante.companyID {
		t.Errorf("sem header: obteve (%q, %v)", got, err)
	}
}

func TestIsolamentoCompanyScopeBloqueiaEmpresaAlheia(t *testing.T) {
	db := tgDB(t)
	vitima, cleanV := newTGTenant(t, db, "vit", "user")
	defer cleanV()
	atacante, cleanA := newTGTenant(t, db, "atk", "admin")
	defer cleanA()

	chamado := false
	h := CompanyScope(db, func(w http.ResponseWriter, r *http.Request) { chamado = true })

	req := tgAs(httptest.NewRequest(http.MethodGet, "/api/x", nil), atacante.userID, "user")
	req.Header.Set("X-Company-ID", vitima.companyID)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusForbidden || chamado {
		t.Fatalf("esperava 403 sem chamar o handler, obteve %d (chamado=%v)", rec.Code, chamado)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["code"] != "company_not_authorized" {
		t.Errorf("code = %q, esperava company_not_authorized", body["code"])
	}

	// empresa própria passa
	req = tgAs(httptest.NewRequest(http.MethodGet, "/api/x", nil), atacante.userID, "user")
	req.Header.Set("X-Company-ID", atacante.companyID)
	h(httptest.NewRecorder(), req)
	if !chamado {
		t.Error("empresa própria deveria chegar ao handler")
	}
}

// ─── preferred-company ───────────────────────────────────────────────────────

func TestIsolamentoPreferredCompanyNaoConcedeVinculo(t *testing.T) {
	db := tgDB(t)
	vitima, cleanV := newTGTenant(t, db, "vit", "user")
	defer cleanV()
	atacante, cleanA := newTGTenant(t, db, "atk", "admin")
	defer cleanA()

	post := func(companyID string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"company_id": companyID})
		req := tgAs(httptest.NewRequest(http.MethodPost, "/api/user/preferred-company", bytes.NewReader(body)), atacante.userID, "user")
		rec := httptest.NewRecorder()
		UpdatePreferredCompanyHandler(db)(rec, req)
		return rec
	}

	// empresa alheia: 403 e NENHUM vínculo criado no ambiente da vítima
	if rec := post(vitima.companyID); rec.Code != http.StatusForbidden {
		t.Fatalf("esperava 403, obteve %d", rec.Code)
	}
	if n := tgCount(t, db, `SELECT count(*) FROM user_environments WHERE user_id = $1 AND environment_id = $2`, atacante.userID, vitima.envID); n != 0 {
		t.Fatalf("vínculo indevido criado no ambiente da vítima (%d linhas)", n)
	}

	// empresa própria: 204 e preferred_company_id atualizado, papel preservado
	if rec := post(atacante.companyID); rec.Code != http.StatusNoContent {
		t.Fatalf("empresa própria: esperava 204, obteve %d", rec.Code)
	}
	var pref, role string
	db.QueryRow(`SELECT preferred_company_id::text, role FROM user_environments WHERE user_id = $1 AND environment_id = $2`, atacante.userID, atacante.envID).Scan(&pref, &role)
	if pref != atacante.companyID || role != "admin" {
		t.Errorf("preferida = %q role = %q", pref, role)
	}
}

// ─── Hierarquia (environment.go) ─────────────────────────────────────────────

func TestHierarquiaNegaMutacaoEmTenantAlheio(t *testing.T) {
	db := tgDB(t)
	vitima, cleanV := newTGTenant(t, db, "vit", "admin")
	defer cleanV()
	atacante, cleanA := newTGTenant(t, db, "atk", "admin")
	defer cleanA()

	do := func(h http.HandlerFunc, method, path string, body interface{}) int {
		var rd *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		} else {
			rd = bytes.NewReader(nil)
		}
		req := tgAs(httptest.NewRequest(method, path, rd), atacante.userID, "user")
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec.Code
	}

	cases := []struct {
		nome string
		code int
	}{
		{"DeleteCompany", do(DeleteCompanyHandler(db), http.MethodDelete, "/api/config/companies?id="+vitima.companyID, nil)},
		{"UpdateCompany", do(UpdateCompanyHandler(db), http.MethodPut, "/api/config/companies", map[string]interface{}{"id": vitima.companyID, "name": "hackeada"})},
		{"CreateCompany", do(CreateCompanyHandler(db), http.MethodPost, "/api/config/companies", map[string]interface{}{"group_id": vitima.groupID, "name": "injetada"})},
		{"DeleteGroup", do(DeleteGroupHandler(db), http.MethodDelete, "/api/config/groups?id="+vitima.groupID, nil)},
		{"CreateGroup", do(CreateGroupHandler(db), http.MethodPost, "/api/config/groups", map[string]interface{}{"environment_id": vitima.envID, "name": "injetado"})},
		{"UpdateEnvironment", do(UpdateEnvironmentHandler(db), http.MethodPut, "/api/config/environments", map[string]interface{}{"id": vitima.envID, "name": "hackeado"})},
		{"DeleteEnvironment", do(DeleteEnvironmentHandler(db), http.MethodDelete, "/api/config/environments?id="+vitima.envID, nil)},
		{"GetGroups(env alheio)", do(GetGroupsHandler(db), http.MethodGet, "/api/config/groups?environment_id="+vitima.envID, nil)},
		{"GetCompanies(grupo alheio)", do(GetCompaniesHandler(db), http.MethodGet, "/api/config/companies?group_id="+vitima.groupID, nil)},
	}
	for _, c := range cases {
		if c.code != http.StatusForbidden {
			t.Errorf("%s: esperava 403, obteve %d", c.nome, c.code)
		}
	}

	// nada mudou no tenant da vítima
	if n := tgCount(t, db, `SELECT count(*) FROM companies WHERE group_id = $1`, vitima.groupID); n != 1 {
		t.Errorf("empresas do grupo da vítima = %d, esperava 1 (sem exclusão nem injeção)", n)
	}
	var nome string
	db.QueryRow(`SELECT name FROM companies WHERE id = $1`, vitima.companyID).Scan(&nome)
	if nome == "hackeada" {
		t.Error("empresa da vítima foi renomeada")
	}
	if n := tgCount(t, db, `SELECT count(*) FROM enterprise_groups WHERE environment_id = $1`, vitima.envID); n != 1 {
		t.Errorf("grupos do ambiente da vítima = %d, esperava 1", n)
	}
	if n := tgCount(t, db, `SELECT count(*) FROM environments WHERE id = $1 AND name <> 'hackeado'`, vitima.envID); n != 1 {
		t.Error("ambiente da vítima foi apagado ou renomeado")
	}
}

func TestHierarquiaListagemSemFiltroNaoVazaOutrosTenants(t *testing.T) {
	db := tgDB(t)
	vitima, cleanV := newTGTenant(t, db, "vit", "user")
	defer cleanV()
	atacante, cleanA := newTGTenant(t, db, "atk", "admin")
	defer cleanA()

	req := tgAs(httptest.NewRequest(http.MethodGet, "/api/config/companies", nil), atacante.userID, "user")
	rec := httptest.NewRecorder()
	GetCompaniesHandler(db)(rec, req)
	var companies []Company
	json.NewDecoder(rec.Body).Decode(&companies)
	achouPropria := false
	for _, c := range companies {
		if c.ID == vitima.companyID {
			t.Fatal("listagem sem filtro vazou empresa de outro tenant")
		}
		if c.ID == atacante.companyID {
			achouPropria = true
		}
	}
	if !achouPropria {
		t.Error("a empresa do próprio usuário deveria aparecer")
	}

	req = tgAs(httptest.NewRequest(http.MethodGet, "/api/config/groups", nil), atacante.userID, "user")
	rec = httptest.NewRecorder()
	GetGroupsHandler(db)(rec, req)
	var groups []EnterpriseGroup
	json.NewDecoder(rec.Body).Decode(&groups)
	for _, g := range groups {
		if g.ID == vitima.groupID {
			t.Fatal("listagem de grupos vazou grupo de outro tenant")
		}
	}
}

func TestHierarquiaExclusaoExigeAdminDoAmbiente(t *testing.T) {
	db := tgDB(t)
	// membro comum do ambiente (role 'user'): pode usar, não pode excluir
	tn, clean := newTGTenant(t, db, "mem", "user")
	defer clean()

	req := tgAs(httptest.NewRequest(http.MethodDelete, "/api/config/companies?id="+tn.companyID, nil), tn.userID, "user")
	rec := httptest.NewRecorder()
	DeleteCompanyHandler(db)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("membro sem papel admin: esperava 403, obteve %d", rec.Code)
	}

	// admin da plataforma pode
	req = tgAs(httptest.NewRequest(http.MethodDelete, "/api/config/companies?id="+tn.companyID, nil), tn.userID, "admin")
	rec = httptest.NewRecorder()
	DeleteCompanyHandler(db)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin da plataforma: esperava 200, obteve %d", rec.Code)
	}
}

// ─── Papel admin no ERP Bridge e cadastro ────────────────────────────────────

func TestAuthMiddlewareAdminExigidoNoERPBridge(t *testing.T) {
	t.Setenv("JWT_SECRET", "segredo-de-teste-com-32-bytes-ou-mais!!")

	token := func(role string) string {
		tk := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": "u1", "role": role, "exp": time.Now().Add(time.Hour).Unix(),
		})
		s, err := tk.SignedString(getJWTSecret())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	for _, tc := range []struct {
		role string
		code int
	}{{"user", http.StatusForbidden}, {"admin", http.StatusNoContent}} {
		h := AuthMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }, "admin")
		req := httptest.NewRequest(http.MethodPost, "/api/erp-bridge/config/generate-api-key", nil)
		req.Header.Set("Authorization", "Bearer "+token(tc.role))
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != tc.code {
			t.Errorf("role=%s: esperava %d, obteve %d", tc.role, tc.code, rec.Code)
		}
	}
}

func TestRegistroDesabilitadoPorVariavel(t *testing.T) {
	t.Setenv("ALLOW_REGISTRATION", "false")
	rec := httptest.NewRecorder()
	RegisterHandler(nil)(rec, httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusForbidden {
		t.Errorf("com ALLOW_REGISTRATION=false esperava 403, obteve %d", rec.Code)
	}

	// default (variável ausente ou true): segue o fluxo normal (corpo inválido → 400)
	t.Setenv("ALLOW_REGISTRATION", "true")
	rec = httptest.NewRecorder()
	RegisterHandler(nil)(rec, httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader([]byte(`nao-e-json`))))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("com ALLOW_REGISTRATION=true esperava 400 (JSON inválido), obteve %d", rec.Code)
	}
}

// ─── Casos de borda apontados na revisão ─────────────────────────────────────

func TestIsolamentoPreferredCompanyDonoSemVinculoNaoGanhaVinculo(t *testing.T) {
	db := tgDB(t)
	tn, clean := newTGTenant(t, db, "dono", "user")
	defer clean()
	// dono da empresa, mas sem vínculo no ambiente
	db.Exec(`DELETE FROM user_environments WHERE user_id = $1`, tn.userID)

	body, _ := json.Marshal(map[string]string{"company_id": tn.companyID})
	req := tgAs(httptest.NewRequest(http.MethodPost, "/api/user/preferred-company", bytes.NewReader(body)), tn.userID, "user")
	rec := httptest.NewRecorder()
	UpdatePreferredCompanyHandler(db)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("dono: esperava 204, obteve %d", rec.Code)
	}
	if n := tgCount(t, db, `SELECT count(*) FROM user_environments WHERE user_id = $1`, tn.userID); n != 0 {
		t.Fatalf("o dono ganhou vínculo no ambiente (%d linhas) — escalada para o ambiente inteiro", n)
	}
}

func TestHierarquiaMembroComumAtualizaMasNaoExclui(t *testing.T) {
	db := tgDB(t)
	tn, clean := newTGTenant(t, db, "mem", "user")
	defer clean()

	body, _ := json.Marshal(map[string]interface{}{"id": tn.companyID, "name": "renomeada-pelo-membro"})
	req := tgAs(httptest.NewRequest(http.MethodPut, "/api/config/companies", bytes.NewReader(body)), tn.userID, "user")
	rec := httptest.NewRecorder()
	UpdateCompanyHandler(db)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("membro atualizando a própria empresa: esperava 200, obteve %d", rec.Code)
	}

	req = tgAs(httptest.NewRequest(http.MethodDelete, "/api/config/groups?id="+tn.groupID, nil), tn.userID, "user")
	rec = httptest.NewRecorder()
	DeleteGroupHandler(db)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("membro excluindo grupo: esperava 403, obteve %d", rec.Code)
	}
}

func TestHierarquiaAdminDaPlataformaRecebe404EmRecursoInexistente(t *testing.T) {
	db := tgDB(t)
	const inexistente = "00000000-0000-4000-8000-000000000000"

	req := tgAs(httptest.NewRequest(http.MethodDelete, "/api/config/companies?id="+inexistente, nil), inexistente, "admin")
	rec := httptest.NewRecorder()
	DeleteCompanyHandler(db)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("admin da plataforma: esperava 404, obteve %d", rec.Code)
	}

	// usuário comum não descobre a existência: continua 403
	req = tgAs(httptest.NewRequest(http.MethodDelete, "/api/config/companies?id="+inexistente, nil), inexistente, "user")
	rec = httptest.NewRecorder()
	DeleteCompanyHandler(db)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("usuário comum: esperava 403, obteve %d", rec.Code)
	}
}

func TestIsolamentoCompanyScopeUUIDInvalidoNegaSemTocarNoBanco(t *testing.T) {
	// db=nil: UUID inválido tem de ser recusado antes de qualquer consulta (sem panic, sem ruído no Postgres)
	chamado := false
	h := CompanyScope(nil, func(w http.ResponseWriter, r *http.Request) { chamado = true })
	req := tgAs(httptest.NewRequest(http.MethodGet, "/api/x", nil), "u1", "user")
	req.Header.Set("X-Company-ID", "' OR 1=1 --")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusForbidden || chamado {
		t.Fatalf("esperava 403 sem chamar o handler, obteve %d (chamado=%v)", rec.Code, chamado)
	}
}
