package services

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// ---------------------------------------------------------------------------
// Helpers — PKI falsa gerada no teste (raiz -> intermediárias -> final) e AIA servida por
// httptest.Server. Nunca rede real nem certificado ICP-Brasil real.
// ---------------------------------------------------------------------------

type certOpts struct {
	cn        string
	isCA      bool
	aia       []string
	notBefore time.Time
	notAfter  time.Time
	ipAddrs   []net.IP
	server    bool
	// chave reaproveita uma chave existente (re-emissão / cross-sign com a mesma chave).
	chave *ecdsa.PrivateKey
	// eku substitui o EKU padrão do certificado final; semEKU omite a extensão.
	eku    []x509.ExtKeyUsage
	semEKU bool
}

var serialTeste atomic.Int64

// emitirCert emite um certificado assinado por emissor/emissorKey (ou autoassinado se emissor
// == nil). Chaves ECDSA P-256 (rápidas de gerar).
func emitirCert(t *testing.T, o certOpts, emissor *x509.Certificate, emissorKey crypto.Signer) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key := o.chave
	if key == nil {
		var err error
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("falha ao gerar chave: %v", err)
		}
	}
	if o.notBefore.IsZero() {
		o.notBefore = time.Now().Add(-time.Hour)
	}
	if o.notAfter.IsZero() {
		o.notAfter = time.Now().Add(24 * time.Hour)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano() + serialTeste.Add(1)),
		Subject:               pkix.Name{CommonName: o.cn, Organization: []string{"PKI de Teste"}},
		NotBefore:             o.notBefore,
		NotAfter:              o.notAfter,
		IssuingCertificateURL: o.aia,
		IPAddresses:           o.ipAddrs,
		BasicConstraintsValid: true,
	}
	if o.isCA {
		tmpl.IsCA = true
		tmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature
	} else {
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		switch {
		case o.semEKU:
		case o.eku != nil:
			tmpl.ExtKeyUsage = o.eku
		case o.server:
			tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		default:
			tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
	}
	parent, signer := tmpl, crypto.Signer(key)
	if emissor != nil {
		parent, signer = emissor, emissorKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatalf("falha ao criar certificado %q: %v", o.cn, err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("falha ao parsear certificado %q: %v", o.cn, err)
	}
	return c, key
}

// usarRaizesTeste substitui as raízes ICP-Brasil embutidas pelas raízes falsas (restaurado no
// fim do teste).
func usarRaizesTeste(t *testing.T, raizes ...*x509.Certificate) {
	t.Helper()
	r := &raizesConfiaveis{pool: x509.NewCertPool()}
	for _, c := range raizes {
		r.pool.AddCert(c)
		r.certs = append(r.certs, c)
	}
	prev := nfseRaizesTeste
	nfseRaizesTeste = r
	t.Cleanup(func() { nfseRaizesTeste = prev })
}

// permitirLoopbackAIA habilita o desvio de loopback do filtro anti-SSRF (restaurado no fim).
func permitirLoopbackAIA(t *testing.T) {
	t.Helper()
	prev := nfseAIAPermitirLoopback
	nfseAIAPermitirLoopback = true
	t.Cleanup(func() { nfseAIAPermitirLoopback = prev })
}

type servidorAIA struct {
	ts    *httptest.Server
	mu    sync.Mutex
	rotas map[string]http.HandlerFunc
	hits  atomic.Int64
	conns atomic.Int64
}

func novoServidorAIA(t *testing.T) *servidorAIA {
	t.Helper()
	s := &servidorAIA{rotas: map[string]http.HandlerFunc{}}
	s.ts = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		h := s.rotas[r.URL.Path]
		s.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	s.ts.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			s.conns.Add(1)
		}
	}
	s.ts.Start()
	t.Cleanup(s.ts.Close)
	return s
}

func (s *servidorAIA) url(path string) string { return s.ts.URL + path }

func (s *servidorAIA) servir(path string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rotas[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pkix-cert")
		w.Write(body)
	}
}

func (s *servidorAIA) rota(path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rotas[path] = h
}

func pemDe(c *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

// p7bDe gera um PKCS#7 certs-only via openssl crl2pkcs7 (pula o teste se não houver openssl).
func p7bDe(t *testing.T, outform string, certs ...*x509.Certificate) []byte {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl não disponível — teste de .p7b pulado")
	}
	dir := t.TempDir()
	var buf bytes.Buffer
	for _, c := range certs {
		buf.Write(pemDe(c))
	}
	in := filepath.Join(dir, "certs.pem")
	if err := os.WriteFile(in, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("openssl", "crl2pkcs7", "-nocrl", "-certfile", in, "-outform", outform).Output()
	if err != nil {
		t.Fatalf("openssl crl2pkcs7 falhou: %v", err)
	}
	return out
}

// pkiSimples: raiz -> intermediária -> final, com a AIA do final apontando para /inter no
// servidor (o servidor ainda não serve nada — cada teste decide).
type pkiSimples struct {
	raiz, inter, final       *x509.Certificate
	raizKey, interKey, chave *ecdsa.PrivateKey
}

func novaPKISimples(t *testing.T, s *servidorAIA) pkiSimples {
	t.Helper()
	var p pkiSimples
	p.raiz, p.raizKey = emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	p.inter, p.interKey = emitirCert(t, certOpts{cn: "AC Intermediaria Falsa", isCA: true}, p.raiz, p.raizKey)
	var aia []string
	if s != nil {
		aia = []string{s.url("/inter")}
	}
	p.final, p.chave = emitirCert(t, certOpts{cn: "EMPRESA TESTE LTDA:12345678000199", aia: aia}, p.inter, p.interKey)
	return p
}

func exigirCadeia(t *testing.T, got []*x509.Certificate, esperado ...*x509.Certificate) {
	t.Helper()
	if len(got) != len(esperado) {
		t.Fatalf("esperava %d intermediária(s), veio %d", len(esperado), len(got))
	}
	for i := range esperado {
		if !bytes.Equal(got[i].Raw, esperado[i].Raw) {
			t.Fatalf("intermediária #%d: esperava %q, veio %q", i, esperado[i].Subject.CommonName, got[i].Subject.CommonName)
		}
	}
}

// ---------------------------------------------------------------------------
// Raízes embutidas
// ---------------------------------------------------------------------------

func TestRaizesICPBrasil_EmbutidasCarregamComSHA256Conferido(t *testing.T) {
	r, err := carregarRaizes(icpBrasilRaizesFS, "icpbrasil_raizes", icpBrasilRaizesSHA256)
	if err != nil {
		t.Fatalf("raízes embutidas deveriam carregar: %v", err)
	}
	if len(r.certs) != 4 {
		t.Fatalf("esperava 4 raízes ICP-Brasil, veio %d", len(r.certs))
	}
	vistos := map[string]bool{}
	for _, c := range r.certs {
		if !strings.HasPrefix(c.Subject.CommonName, "Autoridade Certificadora Raiz Brasileira v") {
			t.Errorf("CN inesperado: %q", c.Subject.CommonName)
		}
		if c.CheckSignatureFrom(c) != nil {
			t.Errorf("raiz %q deveria ser autoassinada", c.Subject.CommonName)
		}
		vistos[c.Subject.CommonName] = true
	}
	for _, v := range []string{"v5", "v10", "v11", "v12"} {
		if !vistos["Autoridade Certificadora Raiz Brasileira "+v] {
			t.Errorf("raiz %s ausente", v)
		}
	}
	// O carregamento de produção (sem override) também precisa funcionar.
	if nfseRaizesTeste != nil {
		t.Fatal("pré-condição: nenhum override de raízes deveria estar ativo")
	}
	if _, err := raizesICPBrasil(); err != nil {
		t.Fatalf("raizesICPBrasil() falhou: %v", err)
	}
}

func TestRaizesICPBrasil_ArquivoAdulterado_FalhaCarregamento(t *testing.T) {
	orig, err := icpBrasilRaizesFS.ReadFile("icpbrasil_raizes/ICP-Brasilv5.crt")
	if err != nil {
		t.Fatal(err)
	}
	adulterado := append([]byte(nil), orig...)
	adulterado[len(adulterado)-1] ^= 0xFF
	fsys := fstest.MapFS{"r/ICP-Brasilv5.crt": {Data: adulterado}}
	if _, err := carregarRaizes(fsys, "r", map[string]string{"ICP-Brasilv5.crt": icpBrasilRaizesSHA256["ICP-Brasilv5.crt"]}); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("esperava falha de SHA-256 para raiz adulterada, veio %v", err)
	}

	// Arquivo sem hash documentado.
	fsys = fstest.MapFS{"r/ICP-Brasilv5.crt": {Data: orig}, "r/intrusa.crt": {Data: orig}}
	if _, err := carregarRaizes(fsys, "r", map[string]string{"ICP-Brasilv5.crt": icpBrasilRaizesSHA256["ICP-Brasilv5.crt"]}); err == nil {
		t.Fatal("esperava falha para .crt embutido sem SHA-256 documentado")
	}

	// Hash documentado sem arquivo.
	fsys = fstest.MapFS{"r/ICP-Brasilv5.crt": {Data: orig}}
	if _, err := carregarRaizes(fsys, "r", icpBrasilRaizesSHA256); err == nil {
		t.Fatal("esperava falha para raiz esperada ausente")
	}
}

// Um certificado autoassinado qualquer não pode passar pelas raízes REAIS embutidas.
func TestCompletarCadeia_RaizesReais_CertificadoForaDaICPBrasil_Rejeitado(t *testing.T) {
	p := novaPKISimples(t, nil)
	_, err := CompletarCadeiaICPBrasil(context.Background(), p.final, []*x509.Certificate{p.inter, p.raiz})
	if !errors.Is(err, ErrNFSeCadeiaNaoICPBrasil) {
		t.Fatalf("esperava ErrNFSeCadeiaNaoICPBrasil com as raízes reais, veio %v", err)
	}
}

// ---------------------------------------------------------------------------
// I/O Matrix
// ---------------------------------------------------------------------------

// Cadeia já completa no .pfx: não baixa nada.
func TestCompletarCadeia_JaCompleta_NenhumaRequisicaoAIA(t *testing.T) {
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	s.servir("/inter", p.inter.Raw)
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)

	got, err := CompletarCadeiaICPBrasil(context.Background(), p.final, []*x509.Certificate{p.inter})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	exigirCadeia(t, got, p.inter)
	if n := s.hits.Load(); n != 0 {
		t.Fatalf("cadeia já completa: esperava 0 requisições AIA, houve %d", n)
	}
	if n := s.conns.Load(); n != 0 {
		t.Fatalf("cadeia já completa: esperava 0 conexões, houve %d", n)
	}
}

// Raiz embutida dentro do .pfx nunca volta como intermediária.
func TestCompletarCadeia_RaizNoPfx_NaoVoltaComoIntermediaria(t *testing.T) {
	p := novaPKISimples(t, nil)
	usarRaizesTeste(t, p.raiz)
	got, err := CompletarCadeiaICPBrasil(context.Background(), p.final, []*x509.Certificate{p.raiz, p.inter})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	exigirCadeia(t, got, p.inter)
}

func TestCompletarCadeia_SoFinal_BaixaViaAIA_FormatosDERPEMP7B(t *testing.T) {
	casos := []struct {
		nome string
		body func(t *testing.T, p pkiSimples) []byte
	}{
		{"DER", func(_ *testing.T, p pkiSimples) []byte { return p.inter.Raw }},
		{"PEM", func(_ *testing.T, p pkiSimples) []byte { return pemDe(p.inter) }},
		{"PEM com texto e varios blocos", func(_ *testing.T, p pkiSimples) []byte {
			return append(append([]byte("Bag Attributes\nsubject=lixo\n"), pemDe(p.raiz)...), pemDe(p.inter)...)
		}},
		{"P7B DER", func(t *testing.T, p pkiSimples) []byte { return p7bDe(t, "DER", p.inter) }},
		{"P7B PEM", func(t *testing.T, p pkiSimples) []byte { return p7bDe(t, "PEM", p.inter) }},
		{"P7B DER com raiz junto", func(t *testing.T, p pkiSimples) []byte { return p7bDe(t, "DER", p.inter, p.raiz) }},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s := novoServidorAIA(t)
			p := novaPKISimples(t, s)
			s.servir("/inter", c.body(t, p))
			usarRaizesTeste(t, p.raiz)
			permitirLoopbackAIA(t)

			got, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			exigirCadeia(t, got, p.inter)
			if n := s.hits.Load(); n != 1 {
				t.Fatalf("esperava 1 requisição AIA, houve %d", n)
			}
		})
	}
}

// Dois níveis de AC: a ordem devolvida é do mais próximo do final ao mais próximo da raiz, e o
// resultado re-encodado no .pfx decodifica com DecodeChain trazendo a cadeia completa.
func TestCompletarCadeia_DoisNiveis_OrdemCorretaEPfxComCadeia(t *testing.T) {
	s := novoServidorAIA(t)
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	ac1, ac1Key := emitirCert(t, certOpts{cn: "AC Nivel 1", isCA: true}, raiz, raizKey)
	ac2, ac2Key := emitirCert(t, certOpts{cn: "AC Nivel 2", isCA: true, aia: []string{s.url("/ac1")}}, ac1, ac1Key)
	final, chave := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{s.url("/ac2")}}, ac2, ac2Key)
	s.servir("/ac1", ac1.Raw)
	s.servir("/ac2", pemDe(ac2))
	usarRaizesTeste(t, raiz)
	permitirLoopbackAIA(t)

	got, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	exigirCadeia(t, got, ac2, ac1)

	pfx, err := pkcs12.Modern.Encode(chave, final, got, "senha")
	if err != nil {
		t.Fatal(err)
	}
	_, leaf, cas, err := pkcs12.DecodeChain(pfx, "senha")
	if err != nil {
		t.Fatalf("DecodeChain do .pfx re-encodado falhou: %v", err)
	}
	if !bytes.Equal(leaf.Raw, final.Raw) {
		t.Fatal("leaf do .pfx deveria ser o certificado final")
	}
	exigirCadeia(t, cas, ac2, ac1)
}

func gerarCadeiaLonga(t *testing.T, s *servidorAIA, niveis int) (raiz, final *x509.Certificate) {
	t.Helper()
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	emissor, emissorKey := raiz, raizKey
	var ultimoPath string
	for i := 1; i <= niveis; i++ {
		var aia []string
		if ultimoPath != "" {
			aia = []string{s.url(ultimoPath)}
		}
		ac, acKey := emitirCert(t, certOpts{cn: "AC Nivel " + string(rune('0'+i)), isCA: true, aia: aia}, emissor, emissorKey)
		path := "/ac" + string(rune('0'+i))
		s.servir(path, ac.Raw)
		ultimoPath = path
		emissor, emissorKey = ac, acKey
	}
	final, _ = emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{s.url(ultimoPath)}}, emissor, emissorKey)
	return raiz, final
}

func TestCompletarCadeia_CincoSaltos_Aceita(t *testing.T) {
	s := novoServidorAIA(t)
	raiz, final := gerarCadeiaLonga(t, s, 5)
	usarRaizesTeste(t, raiz)
	permitirLoopbackAIA(t)
	got, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
	if err != nil {
		t.Fatalf("5 saltos deveriam ser aceitos: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("esperava 5 intermediárias, veio %d", len(got))
	}
}

func TestCompletarCadeia_MaisDeCincoSaltos_Rejeita(t *testing.T) {
	s := novoServidorAIA(t)
	raiz, final := gerarCadeiaLonga(t, s, 6)
	usarRaizesTeste(t, raiz)
	permitirLoopbackAIA(t)
	_, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
	if !errors.Is(err, ErrNFSeCadeiaInvalida) || errors.Is(err, ErrNFSeCadeiaDownloadFalhou) {
		t.Fatalf("esperava ErrNFSeCadeiaInvalida (limite de saltos), veio %v", err)
	}
	if n := s.hits.Load(); n != nfseAIAMaxSaltos {
		t.Fatalf("esperava exatamente %d downloads antes de desistir, houve %d", nfseAIAMaxSaltos, n)
	}
}

func TestCompletarCadeia_AIAAusente_NaoICPBrasil(t *testing.T) {
	p := novaPKISimples(t, nil) // final sem AIA
	usarRaizesTeste(t, p.raiz)
	_, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
	if !errors.Is(err, ErrNFSeCadeiaNaoICPBrasil) {
		t.Fatalf("esperava ErrNFSeCadeiaNaoICPBrasil, veio %v", err)
	}
}

// Resposta > 1 MiB é falha permanente de conteúdo (400), não transitória (502).
func TestCompletarCadeia_RespostaMaiorQue1MiB_AIAInvalida(t *testing.T) {
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	grande := append(pemDe(p.inter), bytes.Repeat([]byte("A"), nfseAIAMaxBytes)...)
	s.servir("/inter", grande)
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)
	_, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
	if !errors.Is(err, ErrNFSeCadeiaAIAInvalida) || errors.Is(err, ErrNFSeCadeiaDownloadFalhou) || !strings.Contains(err.Error(), "excede") {
		t.Fatalf("esperava ErrNFSeCadeiaAIAInvalida por tamanho, veio %v", err)
	}
}

func TestCompletarCadeia_DownloadFalha_HTTP500EConexaoRecusada(t *testing.T) {
	t.Run("HTTP 500", func(t *testing.T) {
		s := novoServidorAIA(t)
		p := novaPKISimples(t, s)
		s.rota("/inter", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "fora do ar", 500) })
		usarRaizesTeste(t, p.raiz)
		permitirLoopbackAIA(t)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil); !errors.Is(err, ErrNFSeCadeiaDownloadFalhou) {
			t.Fatalf("esperava ErrNFSeCadeiaDownloadFalhou, veio %v", err)
		}
	})
	t.Run("conexao recusada", func(t *testing.T) {
		s := novoServidorAIA(t)
		p := novaPKISimples(t, s)
		s.ts.Close()
		usarRaizesTeste(t, p.raiz)
		permitirLoopbackAIA(t)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil); !errors.Is(err, ErrNFSeCadeiaDownloadFalhou) {
			t.Fatalf("esperava ErrNFSeCadeiaDownloadFalhou, veio %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		s := novoServidorAIA(t)
		p := novaPKISimples(t, s)
		liberar := make(chan struct{})
		t.Cleanup(func() { close(liberar) })
		s.rota("/inter", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-liberar:
			case <-r.Context().Done():
			}
		})
		usarRaizesTeste(t, p.raiz)
		permitirLoopbackAIA(t)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if _, err := completarCadeia(ctx, novoClienteAIA(), p.final, nil); !errors.Is(err, ErrNFSeCadeiaDownloadFalhou) {
			t.Fatalf("esperava ErrNFSeCadeiaDownloadFalhou por timeout, veio %v", err)
		}
	})
}

func TestCompletarCadeia_PrimeiraURLFalha_SegundaFunciona(t *testing.T) {
	s := novoServidorAIA(t)
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	inter, interKey := emitirCert(t, certOpts{cn: "AC Falsa", isCA: true}, raiz, raizKey)
	final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{"ldap://ac.exemplo/x", s.url("/404"), s.url("/inter")}}, inter, interKey)
	s.servir("/inter", inter.Raw)
	usarRaizesTeste(t, raiz)
	permitirLoopbackAIA(t)
	got, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	exigirCadeia(t, got, inter)
}

// Resposta que não é certificado: falha permanente de conteúdo (400) — e decidida pelo
// pré-filtro, sem precisar de openssl (por isso não pula sem openssl).
func TestCompletarCadeia_RespostaLixo_AIAInvalida(t *testing.T) {
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	s.servir("/inter", []byte("<html>erro</html>"))
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)
	t.Setenv("PATH", "") // sem openssl: o pré-filtro decide antes de qualquer fork
	_, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
	if !errors.Is(err, ErrNFSeCadeiaAIAInvalida) || errors.Is(err, ErrNFSeCadeiaDownloadFalhou) || errors.Is(err, ErrNFSeCadeiaAmbienteIndisponivel) {
		t.Fatalf("esperava ErrNFSeCadeiaAIAInvalida, veio %v", err)
	}
}

// Classificação 400 x 502 de cada falha de download AIA.
func TestCompletarCadeia_ClassificacaoFalhasAIA(t *testing.T) {
	casos := []struct {
		nome     string
		handler  func(p pkiSimples) http.HandlerFunc
		esperado error
	}{
		{"HTTP 404", func(pkiSimples) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }
		}, ErrNFSeCadeiaAIAInvalida},
		{"HTTP 403", func(pkiSimples) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "x", http.StatusForbidden) }
		}, ErrNFSeCadeiaAIAInvalida},
		{"HTTP 503", func(pkiSimples) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "x", http.StatusServiceUnavailable) }
		}, ErrNFSeCadeiaDownloadFalhou},
		{"mais de 10 certificados", func(p pkiSimples) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				w.Write(bytes.Repeat(pemDe(p.inter), nfseAIAMaxCertsPorResposta+1))
			}
		}, ErrNFSeCadeiaAIAInvalida},
		{"PEM sem certificado", func(pkiSimples) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				w.Write(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1, 2, 3}}))
			}
		}, ErrNFSeCadeiaAIAInvalida},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s := novoServidorAIA(t)
			p := novaPKISimples(t, s)
			s.rota("/inter", c.handler(p))
			usarRaizesTeste(t, p.raiz)
			permitirLoopbackAIA(t)
			_, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
			if !errors.Is(err, c.esperado) {
				t.Fatalf("esperava %v, veio %v", c.esperado, err)
			}
			if c.esperado != ErrNFSeCadeiaDownloadFalhou && errors.Is(err, ErrNFSeCadeiaDownloadFalhou) {
				t.Fatalf("falha permanente não pode virar 502: %v", err)
			}
		})
	}
}

func TestCompletarCadeia_P7BSemOpenssl_AmbienteIndisponivel(t *testing.T) {
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	s.servir("/inter", p7bDe(t, "DER", p.inter)) // gerado antes de esvaziar o PATH
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)
	t.Setenv("PATH", "")
	_, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
	if !errors.Is(err, ErrNFSeCadeiaAmbienteIndisponivel) || !errors.Is(err, ErrNFSeOpensslIndisponivel) {
		t.Fatalf("esperava ErrNFSeCadeiaAmbienteIndisponivel+ErrNFSeOpensslIndisponivel, veio %v", err)
	}
}

func TestCompletarCadeia_OutraPKI_NaoICPBrasil(t *testing.T) {
	t.Run("autoassinado", func(t *testing.T) {
		raizConfiavel, _ := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
		auto, _ := emitirCert(t, certOpts{cn: "Autoassinado:12345678000199"}, nil, nil)
		usarRaizesTeste(t, raizConfiavel)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), auto, nil); !errors.Is(err, ErrNFSeCadeiaNaoICPBrasil) {
			t.Fatalf("esperava ErrNFSeCadeiaNaoICPBrasil, veio %v", err)
		}
	})
	t.Run("raiz estranha no pfx", func(t *testing.T) {
		raizConfiavel, _ := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
		p := novaPKISimples(t, nil) // outra PKI
		usarRaizesTeste(t, raizConfiavel)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), p.final, []*x509.Certificate{p.inter, p.raiz}); !errors.Is(err, ErrNFSeCadeiaNaoICPBrasil) {
			t.Fatalf("esperava ErrNFSeCadeiaNaoICPBrasil, veio %v", err)
		}
	})
	t.Run("AIA leva a AC de outra PKI", func(t *testing.T) {
		s := novoServidorAIA(t)
		raizConfiavel, _ := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
		p := novaPKISimples(t, s)
		s.servir("/inter", p.inter.Raw)
		usarRaizesTeste(t, raizConfiavel)
		permitirLoopbackAIA(t)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil); !errors.Is(err, ErrNFSeCadeiaNaoICPBrasil) {
			t.Fatalf("esperava ErrNFSeCadeiaNaoICPBrasil, veio %v", err)
		}
	})
	t.Run("AIA devolve certificado que nao assina", func(t *testing.T) {
		s := novoServidorAIA(t)
		p := novaPKISimples(t, s)
		outra := novaPKISimples(t, nil)
		s.servir("/inter", outra.inter.Raw)
		usarRaizesTeste(t, p.raiz)
		permitirLoopbackAIA(t)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil); !errors.Is(err, ErrNFSeCadeiaNaoICPBrasil) {
			t.Fatalf("esperava ErrNFSeCadeiaNaoICPBrasil, veio %v", err)
		}
	})
}

func TestCompletarCadeia_Vencido(t *testing.T) {
	passado := certOpts{notBefore: time.Now().Add(-48 * time.Hour), notAfter: time.Now().Add(-24 * time.Hour)}

	t.Run("certificado final vencido", func(t *testing.T) {
		raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
		inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
		o := passado
		o.cn = "EMPRESA:12345678000199"
		final, _ := emitirCert(t, o, inter, interKey)
		usarRaizesTeste(t, raiz)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{inter}); !errors.Is(err, ErrNFSeCadeiaVencida) {
			t.Fatalf("esperava ErrNFSeCadeiaVencida, veio %v", err)
		}
	})
	t.Run("intermediaria vencida no pfx", func(t *testing.T) {
		raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
		o := passado
		o.cn, o.isCA = "AC Vencida", true
		inter, interKey := emitirCert(t, o, raiz, raizKey)
		final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", notBefore: time.Now().Add(-47 * time.Hour)}, inter, interKey)
		usarRaizesTeste(t, raiz)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{inter}); !errors.Is(err, ErrNFSeCadeiaVencida) {
			t.Fatalf("esperava ErrNFSeCadeiaVencida, veio %v", err)
		}
	})
	t.Run("intermediaria vencida baixada via AIA", func(t *testing.T) {
		s := novoServidorAIA(t)
		permitirLoopbackAIA(t)
		raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
		o := passado
		o.cn, o.isCA = "AC Vencida", true
		inter, interKey := emitirCert(t, o, raiz, raizKey)
		final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{s.url("/inter")}}, inter, interKey)
		s.servir("/inter", inter.Raw)
		usarRaizesTeste(t, raiz)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), final, nil); !errors.Is(err, ErrNFSeCadeiaVencida) {
			t.Fatalf("esperava ErrNFSeCadeiaVencida, veio %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Anti-SSRF
// ---------------------------------------------------------------------------

func TestNFSeAIAIPBloqueado_Faixas(t *testing.T) {
	bloqueados := []string{
		"127.0.0.1", "127.1.2.3", "::1", "10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"fc00::1", "fd12:3456::1", "169.254.169.254", "169.254.0.1", "fe80::1", "100.64.0.1",
		"100.127.255.254", "0.0.0.0", "::", "224.0.0.1", "239.255.255.250", "ff02::1",
		"255.255.255.255", "240.0.0.1", "0.1.2.3", "::ffff:127.0.0.1", "::ffff:10.0.0.1",
		"::ffff:169.254.169.254",
		// Faixas adicionais: IPv4-compatible, site-local, Teredo, NAT64 local, discard, 6to4
		// relay e documentação.
		"::7f00:1", "::a00:1", "::a9fe:a9fe", "fec0::1", "feff::1", "2001::1", "2001:0:4136:e378::1",
		"64:ff9b:1::a00:1", "100::1", "192.88.99.1", "2001:db8::1", "2001:db8:ffff::1",
		"192.0.2.1", "198.51.100.7", "203.0.113.200",
	}
	for _, ip := range bloqueados {
		if !nfseAIAIPBloqueado(netip.MustParseAddr(ip)) {
			t.Errorf("%s deveria ser bloqueado", ip)
		}
	}
	permitidos := []string{"8.8.8.8", "200.152.32.1", "100.128.0.1", "172.32.0.1", "2001:4860:4860::8888",
		"192.88.100.1", "198.51.101.1", "203.0.114.1", "2001:1::1", "2001:db9::1"}
	for _, ip := range permitidos {
		if nfseAIAIPBloqueado(netip.MustParseAddr(ip)) {
			t.Errorf("%s deveria ser permitido", ip)
		}
	}
	if nfseAIAIPBloqueado(netip.Addr{}) != true {
		t.Error("endereço inválido deveria ser bloqueado")
	}

	// O desvio de teste libera SÓ loopback, nunca rede privada/link-local.
	permitirLoopbackAIA(t)
	if nfseAIAIPBloqueado(netip.MustParseAddr("127.0.0.1")) {
		t.Error("com o desvio de teste, loopback deveria ser permitido")
	}
	for _, ip := range []string{"10.0.0.1", "169.254.169.254", "192.168.0.1", "100.64.0.1", "::7f00:1"} {
		if !nfseAIAIPBloqueado(netip.MustParseAddr(ip)) {
			t.Errorf("mesmo com o desvio de loopback, %s deveria continuar bloqueado", ip)
		}
	}
}

// Configuração de produção (desvio de loopback DESLIGADO): AIA para 127.0.0.1 é bloqueada sem
// nenhuma conexão chegar ao servidor.
func TestCompletarCadeia_AIALoopbackEmProducao_BloqueadaSemConexao(t *testing.T) {
	if nfseAIAPermitirLoopback {
		t.Fatal("pré-condição: o desvio de loopback deveria estar desligado por padrão")
	}
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	s.servir("/inter", p.inter.Raw)
	usarRaizesTeste(t, p.raiz)

	_, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
	if !errors.Is(err, ErrNFSeCadeiaAIABloqueada) {
		t.Fatalf("esperava ErrNFSeCadeiaAIABloqueada, veio %v", err)
	}
	if n := s.conns.Load(); n != 0 {
		t.Fatalf("nenhuma conexão deveria ter sido feita ao destino bloqueado, houve %d", n)
	}
	if n := s.hits.Load(); n != 0 {
		t.Fatalf("nenhuma requisição deveria ter chegado ao destino bloqueado, houve %d", n)
	}
}

// Destinos internos (metadata de nuvem, rede privada, CGNAT, IPv6 loopback, localhost) são
// recusados no dial, sem tentar conectar (por isso não há espera de timeout).
func TestCompletarCadeia_AIADestinosInternos_Bloqueados(t *testing.T) {
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/ac.crt",
		"http://192.168.0.10/ac.crt",
		"http://172.16.5.4/ac.crt",
		"http://100.64.0.1/ac.crt",
		"http://[::1]/ac.crt",
		"http://[fe80::1]/ac.crt",
		"http://0.0.0.0/ac.crt",
		"http://localhost/ac.crt",
		"http://[::7f00:1]/ac.crt",
		"http://192.0.2.10/ac.crt",
		"http://ac.exemplo.br:8080/ac.crt",
		"https://ac.exemplo.br:22/ac.crt",
		"http://8.8.8.8:25/ac.crt",
	} {
		t.Run(u, func(t *testing.T) {
			raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
			inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
			final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{u}}, inter, interKey)
			usarRaizesTeste(t, raiz)
			inicio := time.Now()
			_, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
			if !errors.Is(err, ErrNFSeCadeiaAIABloqueada) {
				t.Fatalf("esperava ErrNFSeCadeiaAIABloqueada, veio %v", err)
			}
			if d := time.Since(inicio); d > 3*time.Second {
				t.Fatalf("bloqueio deveria ser imediato (sem tentar conectar), levou %s", d)
			}
		})
	}
}

// Esquema diferente de http/https: 400 de "esquema não suportado" — NÃO de endereço bloqueado
// por segurança.
func TestCompletarCadeia_AIAEsquemaNaoSuportado(t *testing.T) {
	for _, u := range []string{"ldap://ac.exemplo.br/cn=ac", "file:///etc/passwd", "gopher://127.0.0.1:25/x", "ftp://ac.exemplo.br/ac.crt"} {
		t.Run(u, func(t *testing.T) {
			raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
			inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
			final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{u}}, inter, interKey)
			usarRaizesTeste(t, raiz)
			_, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
			if !errors.Is(err, ErrNFSeCadeiaAIAEsquemaNaoSuportado) || !errors.Is(err, ErrNFSeCadeiaAIAInvalida) {
				t.Fatalf("esperava ErrNFSeCadeiaAIAEsquemaNaoSuportado, veio %v", err)
			}
			if errors.Is(err, ErrNFSeCadeiaAIABloqueada) {
				t.Fatalf("esquema não suportado não é bloqueio anti-SSRF: %v", err)
			}
		})
	}
}

// Redirect para destino bloqueado: o filtro (na URL do redirect e no dial) também barra o
// destino do redirect — inclusive porta fora de 80/443, sem nenhum dial.
func TestCompletarCadeia_AIARedirectParaDestinoBloqueado(t *testing.T) {
	for _, c := range []struct {
		destino  string
		esperado error
	}{
		{"http://169.254.169.254/latest/meta-data/", ErrNFSeCadeiaAIABloqueada},
		{"http://8.8.8.8:8080/ac.crt", ErrNFSeCadeiaAIABloqueada},
		{"http://[::7f00:1]/ac.crt", ErrNFSeCadeiaAIABloqueada},
		{"file:///etc/passwd", ErrNFSeCadeiaAIAEsquemaNaoSuportado},
	} {
		t.Run(c.destino, func(t *testing.T) {
			s := novoServidorAIA(t)
			p := novaPKISimples(t, s)
			s.rota("/inter", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, c.destino, http.StatusFound)
			})
			usarRaizesTeste(t, p.raiz)
			permitirLoopbackAIA(t)
			inicio := time.Now()
			_, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
			if !errors.Is(err, c.esperado) {
				t.Fatalf("esperava %v, veio %v", c.esperado, err)
			}
			if d := time.Since(inicio); d > 3*time.Second {
				t.Fatalf("bloqueio do redirect deveria ser imediato (sem tentar conectar), levou %s", d)
			}
		})
	}
}

// Portas: só 80/443 (ou a implícita), na URL e no dial. O desvio de teste libera qualquer porta
// SÓ para destino loopback.
func TestNFSeAIAPortas(t *testing.T) {
	validar := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return nfseAIAValidarURL(u)
	}
	for _, ok := range []string{"http://ac.exemplo.br/x", "https://ac.exemplo.br/x", "http://ac.exemplo.br:80/x", "https://ac.exemplo.br:443/x", "http://ac.exemplo.br:443/x"} {
		if err := validar(ok); err != nil {
			t.Errorf("%s deveria ser aceita: %v", ok, err)
		}
	}
	for _, ruim := range []string{"http://ac.exemplo.br:8080/x", "https://ac.exemplo.br:22/x", "http://127.0.0.1:54321/x", "http://ac.exemplo.br:0/x", "http://ac.exemplo.br:99999/x"} {
		if err := validar(ruim); !errors.Is(err, errAIADestinoBloqueado) {
			t.Errorf("%s deveria ser bloqueada por porta, veio %v", ruim, err)
		}
	}
	for _, ok := range []string{"8.8.8.8:80", "8.8.8.8:443", "[2001:4860:4860::8888]:443"} {
		if err := nfseAIADialControl("tcp", ok, nil); err != nil {
			t.Errorf("dial para %s deveria ser aceito: %v", ok, err)
		}
	}
	for _, ruim := range []string{"8.8.8.8:8080", "8.8.8.8:22", "[2001:4860:4860::8888]:25", "127.0.0.1:54321"} {
		if err := nfseAIADialControl("tcp", ruim, nil); !errors.Is(err, errAIADestinoBloqueado) {
			t.Errorf("dial para %s deveria ser bloqueado, veio %v", ruim, err)
		}
	}

	permitirLoopbackAIA(t)
	if err := validar("http://127.0.0.1:54321/x"); err != nil {
		t.Errorf("com o desvio, loopback em porta aleatória deveria ser aceito na URL: %v", err)
	}
	if err := nfseAIADialControl("tcp", "127.0.0.1:54321", nil); err != nil {
		t.Errorf("com o desvio, loopback em porta aleatória deveria ser aceito no dial: %v", err)
	}
	if err := nfseAIADialControl("tcp", "[::1]:54321", nil); err != nil {
		t.Errorf("com o desvio, ::1 em porta aleatória deveria ser aceito no dial: %v", err)
	}
	for _, ruim := range []string{"http://8.8.8.8:8080/x", "http://ac.exemplo.br:8080/x"} {
		if err := validar(ruim); !errors.Is(err, errAIADestinoBloqueado) {
			t.Errorf("mesmo com o desvio, %s (não loopback) deveria ser bloqueada, veio %v", ruim, err)
		}
	}
	if err := nfseAIADialControl("tcp", "8.8.8.8:8080", nil); !errors.Is(err, errAIADestinoBloqueado) {
		t.Errorf("mesmo com o desvio, dial para não-loopback em porta 8080 deveria ser bloqueado, veio %v", err)
	}
}

func TestCompletarCadeia_AIARedirectPermitido_Segue(t *testing.T) {
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	s.rota("/inter", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/inter-real", http.StatusMovedPermanently)
	})
	s.servir("/inter-real", p.inter.Raw)
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)
	got, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	exigirCadeia(t, got, p.inter)
}

// Excesso de redirects é falha permanente de conteúdo (400), não 502.
func TestCompletarCadeia_AIARedirectEmLoop_AIAInvalida(t *testing.T) {
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	s.rota("/inter", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/inter", http.StatusFound) })
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)
	if _, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil); !errors.Is(err, ErrNFSeCadeiaAIAInvalida) || errors.Is(err, ErrNFSeCadeiaDownloadFalhou) {
		t.Fatalf("esperava ErrNFSeCadeiaAIAInvalida, veio %v", err)
	}
	if n := s.hits.Load(); n > int64(nfseAIAMaxRedirects+1) {
		t.Fatalf("redirects deveriam ser limitados a %d, houve %d requisições", nfseAIAMaxRedirects, n)
	}
}

// ---------------------------------------------------------------------------
// Cliente mTLS: GetClientCertificate sempre envia o certificado
// ---------------------------------------------------------------------------

// Servidor exige e VERIFICA certificado de cliente e só lista a raiz falsa como CA aceita
// (que não é o emissor direto do certificado final). O .pfx vem do fluxo de upload
// (CompletarCadeiaICPBrasil + pkcs12.Modern.Encode) — a conexão deve funcionar.
func TestNFSeBuildNFSeMTLSClient_CadeiaCompletada_ServidorListandoSoARaiz_Conecta(t *testing.T) {
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	s.servir("/inter", p.inter.Raw)
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)
	inter, err := CompletarCadeiaICPBrasil(context.Background(), p.final, nil)
	if err != nil {
		t.Fatalf("CompletarCadeiaICPBrasil: %v", err)
	}
	pfx, err := pkcs12.Modern.Encode(p.chave, p.final, inter, "senha")
	if err != nil {
		t.Fatal(err)
	}

	serverCA, serverCAKey := genTestCA(t)
	serverLeaf, serverKey := genTestLeaf(t, serverCA, serverCAKey, leafOpts{commonName: "adn-teste", isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(p.raiz)
	var recebido atomic.Value
	ts := startNFSeADNTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) > 0 {
			recebido.Store(r.TLS.PeerCertificates[0].Subject.CommonName)
		}
		escreverFimTeste(w)
	}), tls.Certificate{Certificate: [][]byte{serverLeaf.Raw}, PrivateKey: serverKey}, clientCAs)
	defer ts.Close()

	client, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: base64.StdEncoding.EncodeToString(pfx), Senha: "senha"})
	if err != nil {
		t.Fatalf("BuildNFSeMTLSClient: %v", err)
	}
	trustServerCA(t, client, serverCA)
	if _, err := BuscarLoteDFeADN(context.Background(), client, ts.URL, 0); err != nil {
		t.Fatalf("handshake mTLS deveria funcionar com a cadeia completada: %v", err)
	}
	if recebido.Load() != "EMPRESA TESTE LTDA:12345678000199" {
		t.Fatalf("servidor deveria ter recebido o certificado final, recebeu %v", recebido.Load())
	}
}

// Servidor lista como CA aceita uma raiz que NÃO é o emissor do certificado (e o .pfx não traz
// intermediária): com tls.Config.Certificates o Go não enviaria nada; com GetClientCertificate
// o certificado é enviado mesmo assim (quem decide é o servidor).
func TestNFSeBuildNFSeMTLSClient_ListaDeCAsNaoCasa_EnviaCertificadoMesmoAssim(t *testing.T) {
	p := novaPKISimples(t, nil)
	pfx, err := pkcs12.Modern.Encode(p.chave, p.final, nil, "senha")
	if err != nil {
		t.Fatal(err)
	}
	serverCA, serverCAKey := genTestCA(t)
	serverLeaf, serverKey := genTestLeaf(t, serverCA, serverCAKey, leafOpts{commonName: "adn-teste", isServer: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}})
	listaCAs := x509.NewCertPool()
	listaCAs.AddCert(p.raiz)

	var recebido atomic.Value
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) > 0 {
			recebido.Store(r.TLS.PeerCertificates[0].Subject.CommonName)
		}
		escreverFimTeste(w)
	}))
	ts.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{serverLeaf.Raw}, PrivateKey: serverKey}},
		ClientAuth:   tls.RequireAnyClientCert,
		ClientCAs:    listaCAs,
	}
	ts.StartTLS()
	defer ts.Close()

	client, _, err := BuildNFSeMTLSClient(NFSeADNCertificado{PfxBase64: base64.StdEncoding.EncodeToString(pfx), Senha: "senha"})
	if err != nil {
		t.Fatalf("BuildNFSeMTLSClient: %v", err)
	}
	trustServerCA(t, client, serverCA)
	if _, err := BuscarLoteDFeADN(context.Background(), client, ts.URL, 0); err != nil {
		t.Fatalf("certificado deveria ter sido enviado mesmo com lista de CAs que não casa: %v", err)
	}
	if recebido.Load() != "EMPRESA TESTE LTDA:12345678000199" {
		t.Fatalf("servidor deveria ter recebido o certificado final, recebeu %v", recebido.Load())
	}
}

func TestNFSeExplicarErroHandshakeADN_BadRecordMAC(t *testing.T) {
	err := errors.New(`Get "https://adn.nfse.gov.br/contribuintes/DFe/0": local error: tls: bad record MAC`)
	exp := explicarErroHandshakeADN(err)
	if !strings.Contains(exp, "TLS 1.3") || !strings.Contains(exp, "causa provável") ||
		!strings.Contains(exp, "rejeição do certificado") || !strings.Contains(exp, "interferência de rede") {
		t.Fatalf("explicação inesperada: %q", exp)
	}
	// Causa PROVÁVEL, não afirmação categórica.
	if strings.Contains(exp, "não falha de rede") || strings.Contains(exp, "REJEITOU") {
		t.Fatalf("explicação não deveria afirmar categoricamente a causa: %q", exp)
	}
	if explicarErroHandshakeADN(errors.New("dial tcp: connection refused")) != "" {
		t.Fatal("erro comum de rede não deveria receber a explicação de bad record MAC")
	}
	if explicarErroHandshakeADN(nil) != "" {
		t.Fatal("nil não deveria receber explicação")
	}
}

// ---------------------------------------------------------------------------
// Revisão adversarial (rodadas 1 e 2) — patches 1-18
// ---------------------------------------------------------------------------

// instalarOpensslFalso põe no início do PATH (isolado por teste) um "openssl" falso cujo corpo
// de shell é gerado a partir do diretório dele (para gravar arquivos de prova ali).
func instalarOpensslFalso(t *testing.T, corpo func(dir string) string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + corpo(dir) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "openssl"), []byte(script), 0700); err != nil {
		t.Fatalf("falha ao escrever openssl falso: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// derFalsoSignedData passa no pré-filtro (SEQUENCE + OID pkcs7-signedData), mas não é PKCS#7.
func derFalsoSignedData() []byte {
	return append([]byte{0x30, byte(len(oidPKCS7SignedDataDER))}, oidPKCS7SignedDataDER...)
}

// Patch 3: o orçamento vem do ctx do chamador — ctx já cancelado não faz nenhum download e
// devolve erro claro de prazo esgotado (502).
func TestCompletarCadeia_ContextoCancelado_PrazoEsgotadoSemDownload(t *testing.T) {
	s := novoServidorAIA(t)
	p := novaPKISimples(t, s)
	s.servir("/inter", p.inter.Raw)
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CompletarCadeiaICPBrasil(ctx, p.final, nil)
	if !errors.Is(err, ErrNFSeCadeiaDownloadFalhou) || !strings.Contains(err.Error(), "prazo") {
		t.Fatalf("esperava ErrNFSeCadeiaDownloadFalhou de prazo esgotado, veio %v", err)
	}
	if n := s.hits.Load(); n != 0 {
		t.Fatalf("com ctx cancelado nenhuma requisição AIA deveria ser feita, houve %d", n)
	}

	// Cadeia já completa não depende de rede: fecha mesmo com ctx cancelado.
	got, err := CompletarCadeiaICPBrasil(ctx, p.final, []*x509.Certificate{p.inter})
	if err != nil {
		t.Fatalf("cadeia já completa deveria fechar sem rede mesmo com ctx cancelado: %v", err)
	}
	exigirCadeia(t, got, p.inter)
}

// Patch 3: o subprocesso openssl do .p7b recebe o ctx do chamador — prazo do chamador esgotado
// mata o openssl na hora, sem esperar o timeout próprio de 10s.
func TestCertificadosPKCS7_OpensslRespeitaCtxDoChamador(t *testing.T) {
	instalarOpensslFalso(t, func(string) string { return "exec sleep 8" })
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	inicio := time.Now()
	_, err := parseCertificadosAIA(ctx, derFalsoSignedData())
	if d := time.Since(inicio); d > 3*time.Second {
		t.Fatalf("openssl deveria ter sido interrompido pelo ctx do chamador, levou %s", d)
	}
	if !errors.Is(err, ErrNFSeCadeiaDownloadFalhou) {
		t.Fatalf("esperava erro de prazo esgotado (502), veio %v", err)
	}
}

// Patch 4: pré-filtro barato antes do fork — conteúdo que não pode ser PKCS#7 nunca chama o
// openssl; DER com o OID pkcs7-signedData chama.
func TestParseCertificadosAIA_PreFiltroAntesDoOpenssl(t *testing.T) {
	dir := instalarOpensslFalso(t, func(dir string) string {
		return "touch '" + filepath.Join(dir, "chamado") + "'\nexit 1"
	})
	marcador := filepath.Join(dir, "chamado")
	for nome, body := range map[string][]byte{
		"html":                []byte("<html>erro</html>"),
		"SEQUENCE sem OID":    {0x30, 0x03, 0x02, 0x01, 0x05},
		"OID sem SEQUENCE":    append([]byte{0x31, 0x0b}, oidPKCS7SignedDataDER...),
		"PEM de outro tipo":   pem.EncodeToMemory(&pem.Block{Type: "CMS", Bytes: derFalsoSignedData()}),
		"PEM PKCS7 sem OID":   pem.EncodeToMemory(&pem.Block{Type: "PKCS7", Bytes: []byte{0x30, 0x00}}),
		"base64 de lixo":      []byte(base64.StdEncoding.EncodeToString([]byte("isto nao e der"))),
		"base64 de SEQUENCE":  []byte(base64.StdEncoding.EncodeToString([]byte{0x30, 0x03, 0x02, 0x01, 0x05})),
		"bytes aleatorios ff": bytes.Repeat([]byte{0xff}, 64),
	} {
		_, err := parseCertificadosAIA(context.Background(), body)
		if !errors.Is(err, errAIAConteudo) {
			t.Errorf("%s: esperava erro de conteúdo, veio %v", nome, err)
		}
		if _, statErr := os.Stat(marcador); statErr == nil {
			t.Fatalf("%s: openssl não deveria ter sido chamado (pré-filtro)", nome)
		}
	}
	// Controle positivo: passa no pré-filtro -> openssl é chamado.
	if _, err := parseCertificadosAIA(context.Background(), derFalsoSignedData()); !errors.Is(err, errAIAConteudo) {
		t.Fatalf("openssl falso falha: esperava erro de conteúdo, veio %v", err)
	}
	if _, statErr := os.Stat(marcador); statErr != nil {
		t.Fatal("DER com OID pkcs7-signedData deveria chegar ao openssl")
	}
}

// openssl falso que grava o próprio ambiente e falha.
func opensslQueGravaAmbiente(t *testing.T) (arquivoEnv string) {
	t.Helper()
	dir := instalarOpensslFalso(t, func(dir string) string {
		return "env > '" + filepath.Join(dir, "env.txt") + "'\nexit 1"
	})
	t.Setenv("OPENSSL_CONF", "/caminho/malicioso/openssl.cnf")
	t.Setenv("OPENSSL_ENGINES", "/caminho/malicioso/engines")
	t.Setenv("NFSE_TESTE_VAZAMENTO", "segredo-do-processo")
	return filepath.Join(dir, "env.txt")
}

func exigirAmbienteMinimo(t *testing.T, arquivoEnv string) {
	t.Helper()
	env, err := os.ReadFile(arquivoEnv)
	if err != nil {
		t.Fatalf("openssl falso não foi chamado: %v", err)
	}
	for _, proibido := range []string{"OPENSSL_CONF", "OPENSSL_ENGINES", "NFSE_TESTE_VAZAMENTO", "segredo-do-processo"} {
		if strings.Contains(string(env), proibido) {
			t.Errorf("subprocesso openssl herdou %q do ambiente do servidor:\n%s", proibido, env)
		}
	}
	if !strings.Contains(string(env), "PATH=") {
		t.Errorf("subprocesso openssl deveria receber PATH:\n%s", env)
	}
}

// Patch 5: subprocesso openssl do .p7b roda com ambiente mínimo (só PATH).
func TestCertificadosPKCS7_OpensslAmbienteMinimo(t *testing.T) {
	arquivoEnv := opensslQueGravaAmbiente(t)
	if _, err := parseCertificadosAIA(context.Background(), derFalsoSignedData()); err == nil {
		t.Fatal("openssl falso falha — esperava erro")
	}
	exigirAmbienteMinimo(t, arquivoEnv)
}

// Patch 6: o certificado final precisa servir para clientAuth (sem EKU = qualquer uso) e não
// pode ser uma AC.
func TestCompletarCadeia_EKUEFinalNaoCA(t *testing.T) {
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
	usarRaizesTeste(t, raiz)
	for _, c := range []struct {
		nome string
		o    certOpts
		ok   bool
	}{
		{"clientAuth", certOpts{eku: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}, true},
		{"sem EKU", certOpts{semEKU: true}, true},
		{"clientAuth+emailProtection", certOpts{eku: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection, x509.ExtKeyUsageClientAuth}}, true},
		{"so codeSigning", certOpts{eku: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}}, false},
		{"so serverAuth", certOpts{eku: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, false},
		{"final e AC", certOpts{isCA: true}, false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			o := c.o
			o.cn = "EMPRESA:12345678000199"
			final, _ := emitirCert(t, o, inter, interKey)
			got, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{inter})
			if c.ok {
				if err != nil {
					t.Fatalf("deveria ser aceito: %v", err)
				}
				exigirCadeia(t, got, inter)
				return
			}
			if !errors.Is(err, ErrNFSeCadeiaInvalida) || errors.Is(err, ErrNFSeCadeiaVencida) || errors.Is(err, ErrNFSeCadeiaNaoICPBrasil) {
				t.Fatalf("esperava ErrNFSeCadeiaInvalida (uso/tipo incompatível), veio %v", err)
			}
		})
	}
}

// Patch 7: vencimento detectado pelas datas dos certificados do caminho — inclusive a raiz
// embutida alcançada.
func TestCompletarCadeia_RaizEmbutidaVencida(t *testing.T) {
	passado := certOpts{cn: "Raiz Falsa", isCA: true, notBefore: time.Now().Add(-48 * time.Hour), notAfter: time.Now().Add(-24 * time.Hour)}
	t.Run("intermediaria no pfx", func(t *testing.T) {
		raiz, raizKey := emitirCert(t, passado, nil, nil)
		inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
		final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199"}, inter, interKey)
		usarRaizesTeste(t, raiz)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{inter}); !errors.Is(err, ErrNFSeCadeiaVencida) {
			t.Fatalf("esperava ErrNFSeCadeiaVencida, veio %v", err)
		}
	})
	t.Run("intermediaria via AIA", func(t *testing.T) {
		s := novoServidorAIA(t)
		permitirLoopbackAIA(t)
		raiz, raizKey := emitirCert(t, passado, nil, nil)
		inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
		final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{s.url("/inter")}}, inter, interKey)
		s.servir("/inter", inter.Raw)
		usarRaizesTeste(t, raiz)
		if _, err := CompletarCadeiaICPBrasil(context.Background(), final, nil); !errors.Is(err, ErrNFSeCadeiaVencida) {
			t.Fatalf("esperava ErrNFSeCadeiaVencida, veio %v", err)
		}
	})
	t.Run("deteccao propria sem texto do erro", func(t *testing.T) {
		raiz, raizKey := emitirCert(t, passado, nil, nil)
		inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
		final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199"}, inter, interKey)
		m := novoMontadorCadeia(final, &raizesConfiaveis{pool: x509.NewCertPool(), certs: []*x509.Certificate{raiz}})
		m.adicionar(inter)
		if !m.algumVencidoNoCaminho(time.Now()) {
			t.Fatal("raiz vencida no caminho deveria ser detectada pelas datas")
		}
		if err := m.classificarFalha(errors.New("erro opaco qualquer"), time.Now()); !errors.Is(err, ErrNFSeCadeiaVencida) {
			t.Fatalf("classificação deveria usar as datas, não o texto do erro: %v", err)
		}
	})
}

// Patch 8: variante re-emitida / cross-signed de uma raiz embutida (mesmo Subject + chave)
// nunca entra como intermediária (nem vai para o .pfx).
func TestMontadorCadeia_VarianteDeRaizNuncaEntra(t *testing.T) {
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	outraRaiz, outraKey := emitirCert(t, certOpts{cn: "Outra Raiz", isCA: true}, nil, nil)
	reemitida, _ := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true, chave: raizKey, notAfter: time.Now().Add(240 * time.Hour)}, nil, nil)
	cruzada, _ := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true, chave: raizKey}, outraRaiz, outraKey)
	inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
	final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199"}, inter, interKey)
	if bytes.Equal(reemitida.Raw, raiz.Raw) || bytes.Equal(cruzada.Raw, raiz.Raw) {
		t.Fatal("pré-condição: variantes deveriam ter DER diferente da raiz")
	}

	m := novoMontadorCadeia(final, &raizesConfiaveis{pool: x509.NewCertPool(), certs: []*x509.Certificate{raiz}})
	if m.adicionar(reemitida) || m.adicionar(cruzada) || m.adicionar(raiz) {
		t.Fatal("variante de raiz embutida (mesmo Subject + chave) não pode entrar como intermediária")
	}
	if !m.adicionar(inter) {
		t.Fatal("AC intermediária comum deveria entrar")
	}

	usarRaizesTeste(t, raiz)
	got, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{cruzada, reemitida, inter})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	exigirCadeia(t, got, inter)
}

// Patch 9: várias URLs AIA — a 1ª responde um certificado que não é o emissor, a 2ª o emissor
// certo: sucesso pela 2ª.
func TestCompletarCadeia_PrimeiraURLComCertificadoErrado_SegundaComEmissor(t *testing.T) {
	s := novoServidorAIA(t)
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	inter, interKey := emitirCert(t, certOpts{cn: "AC Falsa", isCA: true}, raiz, raizKey)
	naoRelacionado, _ := emitirCert(t, certOpts{cn: "AC Nao Relacionada", isCA: true}, raiz, raizKey)
	final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{s.url("/errado"), s.url("/certo")}}, inter, interKey)
	s.servir("/errado", naoRelacionado.Raw)
	s.servir("/certo", inter.Raw)
	usarRaizesTeste(t, raiz)
	permitirLoopbackAIA(t)
	got, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	exigirCadeia(t, got, inter)
	if n := s.hits.Load(); n != 2 {
		t.Fatalf("esperava 2 requisições (1ª URL sem o emissor, 2ª com), houve %d", n)
	}
}

// Patch 10: bundle com vários níveis numa única resposta — tudo entra no pool, sem precisar de
// um download por nível (a AC de nível 2 nem tem AIA).
func TestCompletarCadeia_BundleComVariosNiveis_UmDownload(t *testing.T) {
	s := novoServidorAIA(t)
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	ac1, ac1Key := emitirCert(t, certOpts{cn: "AC Nivel 1", isCA: true}, raiz, raizKey)
	ac2, ac2Key := emitirCert(t, certOpts{cn: "AC Nivel 2", isCA: true}, ac1, ac1Key) // sem AIA
	final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{s.url("/bundle")}}, ac2, ac2Key)
	s.servir("/bundle", append(append(pemDe(ac2), pemDe(ac1)...), pemDe(raiz)...))
	usarRaizesTeste(t, raiz)
	permitirLoopbackAIA(t)
	got, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	exigirCadeia(t, got, ac2, ac1)
	if n := s.hits.Load(); n != 1 {
		t.Fatalf("esperava 1 download (bundle), houve %d", n)
	}
}

// Patch 11: intermediária vencida no .pfx não falha de imediato — a AIA pode trazer a cópia
// renovada (mesma chave); só sem cópia válida o resultado é vencido.
func TestCompletarCadeia_IntermediariaVencidaNoPfx_RenovadaViaAIA(t *testing.T) {
	s := novoServidorAIA(t)
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	vencida, interKey := emitirCert(t, certOpts{cn: "AC Renovada", isCA: true, notBefore: time.Now().Add(-48 * time.Hour), notAfter: time.Now().Add(-24 * time.Hour)}, raiz, raizKey)
	renovada, _ := emitirCert(t, certOpts{cn: "AC Renovada", isCA: true, chave: interKey}, raiz, raizKey)
	final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{s.url("/inter")}}, vencida, interKey)
	s.servir("/inter", renovada.Raw)
	usarRaizesTeste(t, raiz)
	permitirLoopbackAIA(t)
	got, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{vencida})
	if err != nil {
		t.Fatalf("intermediária vencida com cópia renovada na AIA deveria fechar: %v", err)
	}
	exigirCadeia(t, got, renovada)

	// AIA só com a mesma cópia vencida: vencido.
	s.servir("/inter", vencida.Raw)
	if _, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{vencida}); !errors.Is(err, ErrNFSeCadeiaVencida) {
		t.Fatalf("sem cópia válida esperava ErrNFSeCadeiaVencida, veio %v", err)
	}
}

// Patch 12: vários emissores candidatos no mesmo nível (cross-signed) — se o caminho por um não
// fecha, tenta os outros e, por último, a AIA do certificado final.
func TestCompletarCadeia_EmissoresCruzados_TentaOutrosCaminhos(t *testing.T) {
	t.Run("outro candidato do pfx", func(t *testing.T) {
		s := novoServidorAIA(t)
		raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
		estranha, estranhaKey := emitirCert(t, certOpts{cn: "Raiz Estranha", isCA: true}, nil, nil)
		ac2, ac2Key := emitirCert(t, certOpts{cn: "AC Nivel 2", isCA: true}, raiz, raizKey)
		interA, interKey := emitirCert(t, certOpts{cn: "AC Cruzada", isCA: true}, estranha, estranhaKey) // sem AIA
		interB, _ := emitirCert(t, certOpts{cn: "AC Cruzada", isCA: true, chave: interKey, aia: []string{s.url("/ac2")}}, ac2, ac2Key)
		final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199"}, interA, interKey)
		s.servir("/ac2", ac2.Raw)
		usarRaizesTeste(t, raiz)
		permitirLoopbackAIA(t)
		got, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{interA, interB})
		if err != nil {
			t.Fatalf("caminho pelo outro candidato deveria fechar: %v", err)
		}
		exigirCadeia(t, got, interB, ac2)
	})
	t.Run("AIA do certificado final", func(t *testing.T) {
		s := novoServidorAIA(t)
		raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
		estranha, estranhaKey := emitirCert(t, certOpts{cn: "Raiz Estranha", isCA: true}, nil, nil)
		interA, interKey := emitirCert(t, certOpts{cn: "AC Cruzada", isCA: true}, estranha, estranhaKey) // sem AIA
		interB, _ := emitirCert(t, certOpts{cn: "AC Cruzada", isCA: true, chave: interKey}, raiz, raizKey)
		final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{s.url("/interB")}}, interA, interKey)
		s.servir("/interB", interB.Raw)
		usarRaizesTeste(t, raiz)
		permitirLoopbackAIA(t)
		got, err := CompletarCadeiaICPBrasil(context.Background(), final, []*x509.Certificate{interA})
		if err != nil {
			t.Fatalf("AIA do certificado final deveria fechar a cadeia: %v", err)
		}
		exigirCadeia(t, got, interB)
	})
}

// Patch 13: vários caminhos válidos — escolhe o da raiz que vence mais tarde.
func TestCompletarCadeia_VariosCaminhos_EscolheRaizQueVenceMaisTarde(t *testing.T) {
	raizVelha, velhaKey := emitirCert(t, certOpts{cn: "Raiz Velha", isCA: true, notAfter: time.Now().Add(24 * time.Hour)}, nil, nil)
	raizNova, novaKey := emitirCert(t, certOpts{cn: "Raiz Nova", isCA: true, notAfter: time.Now().Add(240 * time.Hour)}, nil, nil)
	interVelha, interKey := emitirCert(t, certOpts{cn: "AC Cruzada", isCA: true}, raizVelha, velhaKey)
	interNova, _ := emitirCert(t, certOpts{cn: "AC Cruzada", isCA: true, chave: interKey}, raizNova, novaKey)
	final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199"}, interVelha, interKey)
	usarRaizesTeste(t, raizVelha, raizNova)
	for _, ordem := range [][]*x509.Certificate{{interVelha, interNova}, {interNova, interVelha}} {
		got, err := CompletarCadeiaICPBrasil(context.Background(), final, ordem)
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		exigirCadeia(t, got, interNova)
	}

	// Empate de NotAfter: o mais curto.
	a := []*x509.Certificate{final, interVelha, raizVelha}
	b := []*x509.Certificate{final, raizVelha}
	if got := escolherCadeia([][]*x509.Certificate{a, b}); len(got) != 2 {
		t.Fatalf("em empate de NotAfter da raiz deveria escolher a cadeia mais curta, veio %d", len(got))
	}
}

// Patch 14: formatos tolerados na resposta AIA.
func TestParseCertificadosAIA_FormatosTolerados(t *testing.T) {
	p := novaPKISimples(t, nil)
	b64 := base64.StdEncoding.EncodeToString(p.inter.Raw)
	var b64Quebrado strings.Builder
	for i := 0; i < len(b64); i += 64 {
		fim := i + 64
		if fim > len(b64) {
			fim = len(b64)
		}
		b64Quebrado.WriteString(b64[i:fim] + "\r\n")
	}
	trusted := append(append([]byte(nil), p.inter.Raw...), 0x30, 0x00) // DER + aux de confiança
	casos := map[string][]byte{
		"DER com CRLF no fim":     append(append([]byte(nil), p.inter.Raw...), '\r', '\n'),
		"DER com espacos no fim":  append(append([]byte(nil), p.inter.Raw...), ' ', '\n', '\t'),
		"base64 puro":             []byte(b64),
		"base64 puro com quebras": []byte(b64Quebrado.String()),
		"X509 CERTIFICATE":        pem.EncodeToMemory(&pem.Block{Type: "X509 CERTIFICATE", Bytes: p.inter.Raw}),
		"TRUSTED CERTIFICATE":     pem.EncodeToMemory(&pem.Block{Type: "TRUSTED CERTIFICATE", Bytes: trusted}),
	}
	for nome, body := range casos {
		certs, err := parseCertificadosAIA(context.Background(), body)
		if err != nil {
			t.Errorf("%s: erro inesperado: %v", nome, err)
			continue
		}
		if len(certs) != 1 || !bytes.Equal(certs[0].Raw, p.inter.Raw) {
			t.Errorf("%s: esperava a AC intermediária, veio %d certificado(s)", nome, len(certs))
		}
	}

	t.Run("PEM misturando CERTIFICATE e PKCS7", func(t *testing.T) {
		body := append(pemDe(p.raiz), p7bDe(t, "PEM", p.inter)...)
		certs, err := parseCertificadosAIA(context.Background(), body)
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if len(certs) != 2 || !bytes.Equal(certs[0].Raw, p.raiz.Raw) || !bytes.Equal(certs[1].Raw, p.inter.Raw) {
			t.Fatalf("esperava os certificados dos dois blocos (CERTIFICATE + PKCS7), veio %d", len(certs))
		}
	})
	t.Run("base64 puro de .p7b", func(t *testing.T) {
		body := []byte(base64.StdEncoding.EncodeToString(p7bDe(t, "DER", p.inter)))
		certs, err := parseCertificadosAIA(context.Background(), body)
		if err != nil {
			t.Fatalf("erro inesperado: %v", err)
		}
		if len(certs) != 1 || !bytes.Equal(certs[0].Raw, p.inter.Raw) {
			t.Fatalf("esperava a AC intermediária, veio %d", len(certs))
		}
	})
}

// Patch 15: AIA https de site com certificado TLS ICP-Brasil — o cliente AIA confia nas raízes
// ICP-Brasil (aqui, a raiz de teste no papel delas) além do pool do sistema.
func TestCompletarCadeia_AIAHTTPSComCertificadoTLSICPBrasil(t *testing.T) {
	p := novaPKISimples(t, nil)
	tlsLeaf, tlsKey := emitirCert(t, certOpts{cn: "ac.exemplo.br", server: true, ipAddrs: []net.IP{net.ParseIP("127.0.0.1")}}, p.raiz, p.raizKey)
	var hits atomic.Int64
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(p.inter.Raw)
	}))
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{tlsLeaf.Raw, p.raiz.Raw}, PrivateKey: tlsKey}}}
	ts.StartTLS()
	t.Cleanup(ts.Close)

	final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{ts.URL + "/inter"}}, p.inter, p.interKey)
	usarRaizesTeste(t, p.raiz)
	permitirLoopbackAIA(t)
	got, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
	if err != nil {
		t.Fatalf("AIA https com TLS ICP-Brasil deveria funcionar: %v", err)
	}
	exigirCadeia(t, got, p.inter)
	if hits.Load() != 1 {
		t.Fatalf("esperava 1 requisição https, houve %d", hits.Load())
	}
}

// Patch 17: URL AIA com quebra de linha não injeta linha falsa no log nem no erro encadeado.
func TestCompletarCadeia_URLAIAComQuebraDeLinha_SemLogInjection(t *testing.T) {
	raiz, raizKey := emitirCert(t, certOpts{cn: "Raiz Falsa", isCA: true}, nil, nil)
	inter, interKey := emitirCert(t, certOpts{cn: "AC", isCA: true}, raiz, raizKey)
	injetada := "http://ac.exemplo.br/x\n2026/10/26 12:00:00 [Auth] LINHA FORJADA"
	final, _ := emitirCert(t, certOpts{cn: "EMPRESA:12345678000199", aia: []string{injetada}}, inter, interKey)
	if len(final.IssuingCertificateURL) != 1 || final.IssuingCertificateURL[0] != injetada {
		t.Fatal("pré-condição: AIA com quebra de linha deveria estar no certificado")
	}
	usarRaizesTeste(t, raiz)

	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	_, err := CompletarCadeiaICPBrasil(context.Background(), final, nil)
	if !errors.Is(err, ErrNFSeCadeiaAIAInvalida) {
		t.Fatalf("URL malformada deveria ser ErrNFSeCadeiaAIAInvalida, veio %v", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("erro encadeado não pode conter quebra de linha crua: %q", err.Error())
	}
	if strings.Contains(buf.String(), "\n2026/10/26 12:00:00 [Auth] LINHA FORJADA") {
		t.Fatalf("log injection: linha forjada apareceu no log:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `\n2026/10/26`) {
		t.Fatalf("a URL deveria ter sido logada com %%q (quebra escapada), log:\n%s", buf.String())
	}
}
