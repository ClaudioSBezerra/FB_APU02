package services

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// resolveNFSeSchedulerHorario — sem DB, sem ticker real (I/O Matrix: "Falha ao resolver
// NFSE_SCHEDULER_HORARIO").
// ---------------------------------------------------------------------------

func TestResolveNFSeSchedulerHorario_Ausente_UsaDefault(t *testing.T) {
	t.Setenv(nfseSchedulerHorarioEnv, "")
	if got := resolveNFSeSchedulerHorario(); got != nfseSchedulerHorarioDefault {
		t.Errorf("esperava default %q, veio %q", nfseSchedulerHorarioDefault, got)
	}
}

func TestResolveNFSeSchedulerHorario_FormatoInvalido_UsaDefault(t *testing.T) {
	t.Setenv(nfseSchedulerHorarioEnv, "25:99")
	if got := resolveNFSeSchedulerHorario(); got != nfseSchedulerHorarioDefault {
		t.Errorf("esperava default %q para valor inválido, veio %q", nfseSchedulerHorarioDefault, got)
	}
}

func TestResolveNFSeSchedulerHorario_Valido_UsaValorConfigurado(t *testing.T) {
	t.Setenv(nfseSchedulerHorarioEnv, "22:30")
	if got := resolveNFSeSchedulerHorario(); got != "22:30" {
		t.Errorf("esperava \"22:30\", veio %q", got)
	}
}

// TestResolveNFSeSchedulerHorario_SemZeroPadding_Canonicaliza cobre o item 4 do Spec Change Log
// (loopback 1): "6:00" é um HH:MM válido para time.Parse, mas sem canonicalização nunca bateria
// contra now.Format("15:04") (sempre zero-paddado "06:00") a cada tick do scheduler.
func TestResolveNFSeSchedulerHorario_SemZeroPadding_Canonicaliza(t *testing.T) {
	t.Setenv(nfseSchedulerHorarioEnv, "6:00")
	if got := resolveNFSeSchedulerHorario(); got != "06:00" {
		t.Errorf("esperava \"06:00\" (canonicalizado com zero à esquerda), veio %q", got)
	}
}

// ---------------------------------------------------------------------------
// deveExecutarCicloNFSe — item 5 do Spec Change Log (loopback 1): catch-up dentro do mesmo dia.
// ---------------------------------------------------------------------------

func TestDeveExecutarCicloNFSe_AindaNaoChegouNoHorario_NaoDispara(t *testing.T) {
	if deveExecutarCicloNFSe("05:59", "06:00", "2026-10-23", "") {
		t.Error("não deveria disparar antes do horário configurado")
	}
}

func TestDeveExecutarCicloNFSe_PassouDoHorarioENaoRodouHoje_DisparaMesmoDepoisDoMinutoExato(t *testing.T) {
	// "Agora" simulado (08:15) é bem depois do horário configurado (06:00) e o ciclo ainda não
	// rodou hoje — catch-up: dispara mesmo sem bater exatamente no minuto configurado.
	if !deveExecutarCicloNFSe("08:15", "06:00", "2026-10-23", "") {
		t.Error("deveria disparar — já passou do horário e nunca rodou (catch-up)")
	}
	if !deveExecutarCicloNFSe("08:15", "06:00", "2026-10-23", "2026-10-22") {
		t.Error("deveria disparar — a última execução registrada foi ontem, não hoje")
	}
}

func TestDeveExecutarCicloNFSe_JaRodouHoje_NaoDisparaDeNovo(t *testing.T) {
	if deveExecutarCicloNFSe("08:15", "06:00", "2026-10-23", "2026-10-23") {
		t.Error("não deveria disparar de novo no mesmo dia depois de já ter rodado")
	}
}

// ---------------------------------------------------------------------------
// nfseSchedulerProcessCompany — item 3 do Spec Change Log (loopback 1): recuperação de panic.
// ---------------------------------------------------------------------------

// TestNfseSchedulerProcessCompany_PanicNaoDerrubaOProcesso força um panic real (nil pointer
// dereference dentro de ExecutarCicloNFSe, chamando db.QueryRow com db=nil) e confirma que
// nfseSchedulerProcessCompany recupera sozinho, sem deixar o panic escapar para o chamador (que,
// numa goroutine de produção sem recover, derrubaria o processo inteiro), e loga a recuperação.
func TestNfseSchedulerProcessCompany_PanicNaoDerrubaOProcesso(t *testing.T) {
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	const companyID = "company-fake-panic-test"
	NFSeBuscaRL.Reset(companyID)

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic vazou de nfseSchedulerProcessCompany (recover() interno não funcionou): %v", r)
			}
		}()
		nfseSchedulerProcessCompany(nil, companyID)
	}()

	if !strings.Contains(logBuf.String(), "PANIC recuperado") {
		t.Errorf("esperava log de panic recuperado mencionando companyID=%s, veio: %s", companyID, logBuf.String())
	}
}

// ---------------------------------------------------------------------------
// runNFSeSchedulerCycle — I/O Matrix: empresa sem certificado/vencida é pulada; erro numa
// empresa não impede o ciclo das demais.
// ---------------------------------------------------------------------------

// TestRunNFSeSchedulerCycle_EmpresaSemCertificadoOuVencida_NuncaDisparada cobre: uma empresa sem
// nenhuma linha em nfse_certificados e outra com certificado vencido nunca aparecem na lista de
// empresas despachadas pelo ciclo agendado — isso não é um erro, nenhuma goroutine chega a ser
// disparada para elas.
func TestRunNFSeSchedulerCycle_EmpresaSemCertificadoOuVencida_NuncaDisparada(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()

	companySemCert, cleanupSemCert := setupTestCompany(t, db)
	defer cleanupSemCert()

	companyVencida, cleanupVencida := setupTestCompany(t, db)
	defer cleanupVencida()
	defer db.Exec(`DELETE FROM nfse_certificados WHERE company_id = $1`, companyVencida)

	passado := time.Now().AddDate(0, 0, -5)
	insertNFSeCertificadoTeste(t, db, companyVencida, "conteudo-fake-de-certificado", "senha-teste", "vencido.pfx", &passado, "ativo")

	companies, wg := runNFSeSchedulerCycle(db)
	wg.Wait()

	for _, c := range companies {
		if c == companySemCert {
			t.Errorf("empresa sem certificado nunca deveria aparecer na lista de empresas despachadas")
		}
		if c == companyVencida {
			t.Errorf("empresa com certificado vencido nunca deveria aparecer na lista de empresas despachadas")
		}
	}
}

// TestRunNFSeSchedulerCycle_ErroEmUmaEmpresaNaoImpedeAsDemais cobre a principal Acceptance
// Criterion da spec: 2 empresas com certificado ativo, uma com mTLS válido (o servidor de teste
// aceita) e outra com mTLS inválido (o servidor rejeita) — ambas são disparadas pelo ciclo, e a
// falha da segunda não impede a primeira de ser processada e persistir seus documentos.
func TestRunNFSeSchedulerCycle_ErroEmUmaEmpresaNaoImpedeAsDemais(t *testing.T) {
	db := openNFSeTestDB(t)
	defer db.Close()

	companyA, cleanupA := setupTestCompany(t, db)
	defer cleanupA()
	companyB, cleanupB := setupTestCompany(t, db)
	defer cleanupB()
	defer db.Exec(`DELETE FROM nfse_certificados WHERE company_id IN ($1, $2)`, companyA, companyB)

	trustedCA, trustedCAKey := genTestCA(t)
	serverCert, serverKey := genTestLeaf(t, trustedCA, trustedCAKey, leafOpts{isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	serverTLSCert := tls.Certificate{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}
	clientCAPool := x509.NewCertPool()
	clientCAPool.AddCert(trustedCA)

	const cnpjA = "11111111000191"
	clientCertA, clientKeyA := genTestLeaf(t, trustedCA, trustedCAKey, leafOpts{commonName: "EMPRESA A:" + cnpjA})

	// Empresa B assina seu certificado com uma CA que o servidor NÃO confia — handshake mTLS
	// falha, simulando o cenário "certificado vencido/revogado no Portal Nacional" da I/O Matrix.
	untrustedCA, untrustedCAKey := genTestCA(t)
	const cnpjB = "22222222000192"
	clientCertB, clientKeyB := genTestLeaf(t, untrustedCA, untrustedCAKey, leafOpts{commonName: "EMPRESA B:" + cnpjB})

	mux := http.NewServeMux()
	mux.HandleFunc("/contribuintes/DFe/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"loteVazio": true,
			"ultimoNSU": "1",
			"documentos": []map[string]interface{}{
				{
					"nsu": "1", "identificadorNfse": "NFSE-SCHED-001",
					"cnpjPrestador": "99999999000199", "cnpjTomador": cnpjA,
					"dataEmissao": "2026-10-10", "valorServicos": 150.0,
				},
			},
		})
	})
	ts := startNFSeADNTestServer(t, mux, serverTLSCert, clientCAPool)
	defer ts.Close()

	writeNFSeCAPEMTeste(t, trustedCA)
	t.Setenv("NFSE_ADN_BASE_URL", ts.URL)

	futuro := time.Now().AddDate(1, 0, 0)
	pfxA := buildTestPfxBase64(t, clientCertA, clientKeyA, "senha-a")
	insertNFSeCertificadoTeste(t, db, companyA, pfxA, "senha-a", "a.pfx", &futuro, "ativo")
	pfxB := buildTestPfxBase64(t, clientCertB, clientKeyB, "senha-b")
	insertNFSeCertificadoTeste(t, db, companyB, pfxB, "senha-b", "b.pfx", &futuro, "ativo")

	// Garante que um teste anterior não deixou as duas empresas bloqueadas pelo NFSeBuscaRL
	// compartilhado (30s/empresa) — companyA/companyB são UUIDs novos a cada execução, então na
	// prática isto é defensivo, não estritamente necessário.
	NFSeBuscaRL.Reset(companyA)
	NFSeBuscaRL.Reset(companyB)

	companies, wg := runNFSeSchedulerCycle(db)
	wg.Wait()

	foundA, foundB := false, false
	for _, c := range companies {
		switch c {
		case companyA:
			foundA = true
		case companyB:
			foundB = true
		}
	}
	if !foundA {
		t.Errorf("empresa A (certificado ativo, mTLS válido) deveria ter sido disparada pelo ciclo")
	}
	if !foundB {
		t.Errorf("empresa B (certificado ativo, mTLS inválido) deveria ter sido disparada pelo ciclo — o erro é dela, não a impede de ser tentada")
	}

	if n := countNFSeRowsTeste(t, db, "nfse_entradas", companyA); n != 1 {
		t.Errorf("empresa A deveria ter 1 documento persistido mesmo com a empresa B falhando, veio %d", n)
	}
	if n := countNFSeRowsTeste(t, db, "nfse_entradas", companyB); n != 0 {
		t.Errorf("empresa B (mTLS rejeitado) não deveria persistir nada, veio %d", n)
	}
}
