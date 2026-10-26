// Package services — completar a cadeia ICP-Brasil do certificado A1 no upload (AIA).
//
// Contexto (ver spec-nfse-cadeia-icp-brasil.md): o Portal Nacional ADN só lista as raízes
// ICP-Brasil v5/v10/v11/v12 como CAs aceitas no handshake mTLS. Um .pfx que traz só o
// certificado final (sem as ACs intermediárias) faz o Go não enviar certificado nenhum (ou
// enviar uma cadeia que o servidor não consegue montar até a raiz), e o ADN rejeita o
// handshake ("bad record MAC" no TLS 1.3). Windows/Java completam a cadeia sozinhos seguindo a
// extensão AIA "CA Issuers"; o Go não. Aqui a cadeia é completada UMA vez, no upload
// (SaveNFSeCertificadoHandler) — nunca no caminho de busca —, validada com x509.Verify contra
// SÓ as raízes ICP-Brasil embutidas (icpbrasil.go) e persistida já dentro do .pfx.
//
// A integridade das intermediárias baixadas vem do Verify (assinatura até uma raiz embutida),
// não da origem do download — por isso a AIA pode ser http. O download em si é protegido contra
// SSRF (a URL AIA está dentro do certificado, controlado por quem faz o upload): esquema, host e
// porta são checados na URL inicial e em cada redirect, e o IP:porta efetivo é checado no momento
// da conexão (net.Dialer.Control), cobrindo DNS rebinding e redirects.
package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// nfseAIATimeout — timeout de cada download AIA (conexão + resposta completa).
	nfseAIATimeout = 10 * time.Second
	// nfseAIAMaxBytes — tamanho máximo aceito para a resposta de um download AIA.
	nfseAIAMaxBytes = 1 << 20 // 1 MiB
	// nfseAIAMaxSaltos — máximo de downloads AIA (um por certificado "ponta" seguido) ao
	// completar uma cadeia, no total. Cadeias ICP-Brasil reais têm 1-2 ACs intermediárias.
	nfseAIAMaxSaltos = 5
	// nfseAIAMaxRedirects — máximo de redirects seguidos num download AIA (cada destino passa
	// pelo mesmo filtro anti-SSRF na URL e no dial).
	nfseAIAMaxRedirects = 3
	// nfseAIAMaxURLs — máximo de URLs "CA Issuers" tentadas por certificado.
	nfseAIAMaxURLs = 4
	// nfseAIAMaxCertsPorResposta — máximo de certificados aceitos de uma única resposta AIA.
	nfseAIAMaxCertsPorResposta = 10
	// nfseCadeiaTimeoutTotal — orçamento total de CompletarCadeiaICPBrasil (derivado do ctx
	// recebido — se a requisição HTTP for cancelada antes, o orçamento acaba antes).
	nfseCadeiaTimeoutTotal = 60 * time.Second
)

// Sentinelas de CompletarCadeiaICPBrasil. Categorias (mapeadas pelo handler):
//   - ErrNFSeCadeiaInvalida (e suas especializações abaixo, que a embrulham) -> HTTP 400;
//   - ErrNFSeCadeiaDownloadFalhou -> HTTP 502 ("tente novamente"): SÓ rede/timeout/HTTP 5xx/
//     prazo total esgotado — falhas que podem sumir numa nova tentativa;
//   - ErrNFSeCadeiaAmbienteIndisponivel -> HTTP 500 (openssl ausente, disco, raízes embutidas).
var (
	ErrNFSeCadeiaInvalida = errors.New("cadeia do certificado não pôde ser validada até uma raiz ICP-Brasil")
	// ErrNFSeCadeiaNaoICPBrasil — a cadeia não termina numa raiz ICP-Brasil embutida (outra
	// PKI, autoassinado, AIA ausente ou que não leva ao emissor).
	ErrNFSeCadeiaNaoICPBrasil = fmt.Errorf("%w: certificado não é ICP-Brasil", ErrNFSeCadeiaInvalida)
	// ErrNFSeCadeiaVencida — o certificado final, alguma AC da cadeia ou a raiz alcançada está
	// fora do período de validade.
	ErrNFSeCadeiaVencida = fmt.Errorf("%w: certificado ou AC da cadeia vencido", ErrNFSeCadeiaInvalida)
	// ErrNFSeCadeiaAIABloqueada — as URLs AIA apontam para destinos não permitidos pelo filtro
	// anti-SSRF (IP privado/loopback/link-local/reservado, porta diferente de 80/443).
	ErrNFSeCadeiaAIABloqueada = fmt.Errorf("%w: endereço AIA não permitido", ErrNFSeCadeiaInvalida)
	// ErrNFSeCadeiaAIAInvalida — falha PERMANENTE de conteúdo/formato da AIA: a resposta não é
	// certificado, formato inválido, HTTP 4xx, mais de nfseAIAMaxCertsPorResposta certificados,
	// resposta > nfseAIAMaxBytes, excesso de redirects, URL malformada. Tentar de novo não
	// resolve — o usuário precisa falar com a certificadora.
	ErrNFSeCadeiaAIAInvalida = fmt.Errorf("%w: AC intermediária não pôde ser obtida pelo endereço AIA do certificado", ErrNFSeCadeiaInvalida)
	// ErrNFSeCadeiaAIAEsquemaNaoSuportado — a URL AIA usa esquema diferente de http/https (ex.:
	// ldap://). Especialização de ErrNFSeCadeiaAIAInvalida: não é bloqueio de segurança, é
	// protocolo não suportado.
	ErrNFSeCadeiaAIAEsquemaNaoSuportado = fmt.Errorf("%w: esquema não suportado (só http/https)", ErrNFSeCadeiaAIAInvalida)

	// ErrNFSeCadeiaDownloadFalhou — falha de rede/timeout/HTTP 5xx/prazo total esgotado ao
	// baixar a AC intermediária (AC fora do ar, normalmente transitório).
	ErrNFSeCadeiaDownloadFalhou = errors.New("falha ao baixar AC intermediária via AIA")
	// ErrNFSeCadeiaAmbienteIndisponivel — problema de ambiente do servidor (openssl ausente,
	// diretório temporário, raízes embutidas inválidas). Nunca culpa do usuário.
	ErrNFSeCadeiaAmbienteIndisponivel = errors.New("ambiente do servidor indisponível para completar a cadeia do certificado")
)

// Erros internos de UM download AIA — agregados por baixarEmissorAIA nas sentinelas acima.
// Qualquer erro que não embrulhe nenhum destes (e não seja de ambiente) é tratado como falha
// transitória de rede (502).
var (
	// errAIADestinoBloqueado — o filtro anti-SSRF recusou o destino (IP/porta, na URL, no
	// redirect ou no dial).
	errAIADestinoBloqueado = errors.New("destino AIA bloqueado pelo filtro anti-SSRF")
	// errAIAEsquema — esquema diferente de http/https (na URL inicial ou num redirect).
	errAIAEsquema = errors.New("esquema de URL AIA não suportado")
	// errAIAConteudo — falha permanente de conteúdo/formato (ver ErrNFSeCadeiaAIAInvalida).
	errAIAConteudo = errors.New("resposta AIA inválida")
	// errAIANaoEmissor — a resposta foi lida, mas nenhum certificado nela assina o certificado
	// cuja AIA foi seguida.
	errAIANaoEmissor = errors.New("resposta AIA não contém o certificado emissor")
)

// nfseAIAPermitirLoopback libera SÓ endereços de loopback (127.0.0.0/8, ::1) no filtro
// anti-SSRF — e, só para destinos loopback, qualquer porta (a porta aleatória do
// httptest.Server). Necessário para os testes servirem a AIA via httptest.Server. NÃO exportada e
// sem nenhum caminho de configuração (env var/config): só testes deste mesmo pacote alteram.
// Em produção é sempre false.
var nfseAIAPermitirLoopback = false

// faixas bloqueadas que não têm método próprio em netip.Addr.
var nfseAIAFaixasBloqueadas = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT (RFC 6598)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentação (TEST-NET-1)
	netip.MustParsePrefix("192.88.99.0/24"),  // 6to4 relay anycast (obsoleto)
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentação (TEST-NET-2)
	netip.MustParsePrefix("203.0.113.0/24"),  // documentação (TEST-NET-3)
	netip.MustParsePrefix("240.0.0.0/4"),     // reservado + broadcast 255.255.255.255
	netip.MustParsePrefix("::/96"),           // IPv4-compatible (obsoleto, ex. ::7f00:1); ::1 e :: já são loopback/unspecified
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 (pode embutir IPv4 interno)
	netip.MustParsePrefix("64:ff9b:1::/48"),  // NAT64 local-use (RFC 8215)
	netip.MustParsePrefix("100::/64"),        // discard-only (RFC 6666)
	netip.MustParsePrefix("2001::/32"),       // Teredo (embute IPv4)
	netip.MustParsePrefix("2001:db8::/32"),   // documentação
	netip.MustParsePrefix("2002::/16"),       // 6to4 (pode embutir IPv4 interno)
	netip.MustParsePrefix("fec0::/10"),       // site-local (obsoleto)
}

// nfseAIAIPBloqueado diz se um IP não pode ser destino de download AIA: loopback (salvo o
// desvio de teste), privado (10/8, 172.16/12, 192.168/16, fc00::/7), link-local (169.254/16 —
// inclui o endpoint de metadata de nuvem 169.254.169.254 —, fe80::/10), CGNAT, unspecified,
// multicast, broadcast e faixas reservadas/documentação/transição (nfseAIAFaixasBloqueadas).
func nfseAIAIPBloqueado(a netip.Addr) bool {
	if !a.IsValid() {
		return true
	}
	a = a.Unmap()
	if a.IsLoopback() {
		return !nfseAIAPermitirLoopback
	}
	if a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range nfseAIAFaixasBloqueadas {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// nfseAIAPortaPermitida — só 80 e 443. A única exceção é o desvio de teste: com
// nfseAIAPermitirLoopback ligado, destino loopback pode usar qualquer porta (httptest.Server).
func nfseAIAPortaPermitida(porta uint16, destinoLoopback bool) bool {
	if porta == 80 || porta == 443 {
		return true
	}
	return nfseAIAPermitirLoopback && destinoLoopback
}

// nfseAIADialControl roda imediatamente antes de cada connect(), com o IP:porta EFETIVO (já
// resolvido) — por isso cobre DNS rebinding, múltiplos IPs por host e redirects.
func nfseAIADialControl(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: endereço %q não reconhecido", errAIADestinoBloqueado, address)
	}
	if nfseAIAIPBloqueado(ap.Addr()) {
		return fmt.Errorf("%w: %q (%s)", errAIADestinoBloqueado, ap.Addr().String(), network)
	}
	if !nfseAIAPortaPermitida(ap.Port(), ap.Addr().Unmap().IsLoopback()) {
		return fmt.Errorf("%w: porta %d não permitida (só 80/443)", errAIADestinoBloqueado, ap.Port())
	}
	return nil
}

// nfseAIAValidarURL aceita só http/https com host e porta 80/443 (ou a implícita do esquema).
// Usada na URL inicial e em cada redirect (CheckRedirect).
func nfseAIAValidarURL(u *url.URL) error {
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: %q", errAIAEsquema, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: URL AIA sem host", errAIAConteudo)
	}
	if p := u.Port(); p != "" {
		loopback := false
		if a, err := netip.ParseAddr(host); err == nil {
			loopback = a.Unmap().IsLoopback()
		}
		n, perr := strconv.ParseUint(p, 10, 16)
		if perr != nil || !nfseAIAPortaPermitida(uint16(n), loopback) {
			return fmt.Errorf("%w: porta %q não permitida (só 80/443)", errAIADestinoBloqueado, p)
		}
	}
	return nil
}

// nfseAIARootCAs — âncoras para AIA https: o pool do sistema MAIS as raízes ICP-Brasil
// embutidas (sites de ACs ICP-Brasil usam certificado TLS ICP-Brasil, que o pool do sistema
// normalmente não conhece). Só afeta o TLS do download; a cadeia em si continua sendo validada
// SÓ contra as raízes embutidas (Verify em completarCadeia).
func nfseAIARootCAs() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if r, rerr := raizesICPBrasil(); rerr == nil && r != nil {
		for _, c := range r.certs {
			pool.AddCert(c)
		}
	}
	return pool
}

// novoClienteAIA monta o client HTTP usado SÓ para downloads AIA: sem proxy (o filtro precisa
// ver o IP real de destino), filtro anti-SSRF no dial, redirects limitados e revalidados,
// timeout total por download.
func novoClienteAIA() *http.Client {
	dialer := &net.Dialer{Timeout: nfseAIATimeout, Control: nfseAIADialControl}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		TLSClientConfig:        &tls.Config{RootCAs: nfseAIARootCAs(), MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    nfseAIATimeout,
		ResponseHeaderTimeout:  nfseAIATimeout,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   nfseAIATimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > nfseAIAMaxRedirects {
				return fmt.Errorf("%w: mais de %d redirects", errAIAConteudo, nfseAIAMaxRedirects)
			}
			return nfseAIAValidarURL(req.URL)
		},
	}
}

// CompletarCadeiaICPBrasil valida cert até uma raiz ICP-Brasil embutida, baixando via AIA
// ("CA Issuers") as ACs intermediárias que faltarem em intermediarias. Devolve as
// intermediárias da cadeia validada — sem o próprio certificado final e sem a raiz —, do mais
// próximo do certificado final ao mais próximo da raiz (a ordem esperada em
// tls.Certificate.Certificate e no .pfx). Se a cadeia já fecha com o que veio, nada é baixado.
// O orçamento total (nfseCadeiaTimeoutTotal) é derivado de ctx. Não checa revogação (CRL/OCSP),
// por decisão da spec.
func CompletarCadeiaICPBrasil(ctx context.Context, cert *x509.Certificate, intermediarias []*x509.Certificate) ([]*x509.Certificate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, nfseCadeiaTimeoutTotal)
	defer cancel()
	return completarCadeia(ctx, novoClienteAIA(), cert, intermediarias)
}

// erroPrazoCadeia — o orçamento total (ou a requisição de origem) acabou: transitório (502).
func erroPrazoCadeia(ctx context.Context) error {
	return fmt.Errorf("%w: prazo para completar a cadeia esgotado (limite total de %s): %v", ErrNFSeCadeiaDownloadFalhou, nfseCadeiaTimeoutTotal, ctx.Err())
}

// montadorCadeia guarda o estado da montagem: raízes confiáveis e ACs conhecidas (do .pfx +
// baixadas), com o pool correspondente para x509.VerifyOptions.Intermediates.
type montadorCadeia struct {
	cert       *x509.Certificate
	raizes     *raizesConfiaveis
	pool       *x509.CertPool
	conhecidas []*x509.Certificate
}

func novoMontadorCadeia(cert *x509.Certificate, raizes *raizesConfiaveis) *montadorCadeia {
	return &montadorCadeia{cert: cert, raizes: raizes, pool: x509.NewCertPool()}
}

// ehRaiz — c é uma raiz embutida OU uma variante dela (mesmo Subject e mesma chave pública:
// re-emissão, cross-signed). Variante de raiz nunca entra como intermediária, para nunca ser
// persistida no .pfx.
func (m *montadorCadeia) ehRaiz(c *x509.Certificate) bool {
	for _, r := range m.raizes.certs {
		if bytes.Equal(r.Raw, c.Raw) ||
			(bytes.Equal(r.RawSubject, c.RawSubject) && bytes.Equal(r.RawSubjectPublicKeyInfo, c.RawSubjectPublicKeyInfo)) {
			return true
		}
	}
	return false
}

// adicionar inclui c entre as intermediárias conhecidas; false se já conhecida, se é o próprio
// certificado final ou se é (variante de) raiz embutida.
func (m *montadorCadeia) adicionar(c *x509.Certificate) bool {
	if c == nil || bytes.Equal(c.Raw, m.cert.Raw) {
		return false
	}
	for _, k := range m.conhecidas {
		if bytes.Equal(k.Raw, c.Raw) {
			return false
		}
	}
	if m.ehRaiz(c) {
		return false
	}
	m.pool.AddCert(c)
	m.conhecidas = append(m.conhecidas, c)
	return true
}

func emitidoPor(c, emissor *x509.Certificate) bool {
	return !bytes.Equal(emissor.Raw, c.Raw) && bytes.Equal(emissor.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(emissor) == nil
}

func dentroDaValidade(c *x509.Certificate, agora time.Time) bool {
	return !agora.Before(c.NotBefore) && !agora.After(c.NotAfter)
}

func autoassinado(c *x509.Certificate) bool {
	return bytes.Equal(c.RawIssuer, c.RawSubject) && c.CheckSignatureFrom(c) == nil
}

// pontas percorre, a partir do certificado final, TODOS os emissores candidatos válidos (no
// prazo) entre as ACs conhecidas (cross-signed: vários no mesmo nível) e devolve, em ordem de
// descoberta, os certificados cujo emissor falta — de onde seguir a AIA. Emissor só vencido não
// conta como emissor: o certificado que ele assinou vira ponta (a AIA pode trazer uma cópia
// renovada com a mesma chave). Ramo que chega a uma raiz embutida não gera ponta (baixar a raiz
// não adianta). O próprio certificado final entra no fim como último recurso (a AIA dele pode
// levar a outro emissor). venceu=true se algum ramo só tinha emissor (AC ou raiz) vencido.
func (m *montadorCadeia) pontas(agora time.Time) (pontas []*x509.Certificate, venceu bool) {
	visitados := map[string]bool{}
	var visitar func(c *x509.Certificate, prof int)
	visitar = func(c *x509.Certificate, prof int) {
		if visitados[string(c.Raw)] {
			return
		}
		visitados[string(c.Raw)] = true

		temRaiz, raizValida := false, false
		for _, r := range m.raizes.certs {
			if emitidoPor(c, r) {
				temRaiz = true
				if dentroDaValidade(r, agora) {
					raizValida = true
				}
			}
		}
		if temRaiz {
			if !raizValida {
				venceu = true
			}
			return
		}

		var validos []*x509.Certificate
		soVencido := false
		for _, k := range m.conhecidas {
			if !emitidoPor(c, k) {
				continue
			}
			if dentroDaValidade(k, agora) {
				validos = append(validos, k)
			} else {
				soVencido = true
			}
		}
		if len(validos) == 0 {
			if soVencido {
				venceu = true
			}
			pontas = append(pontas, c)
			return
		}
		if prof > len(m.conhecidas) {
			return
		}
		for _, v := range validos {
			visitar(v, prof+1)
		}
	}
	visitar(m.cert, 0)

	temFinal := false
	for _, p := range pontas {
		if p == m.cert {
			temFinal = true
			break
		}
	}
	if !temFinal {
		pontas = append(pontas, m.cert)
	}
	return pontas, venceu
}

// algumVencidoNoCaminho — depois de um Verify falho: o certificado final, ou algum certificado
// alcançável subindo por emissores conhecidos/baixados (inclusive a raiz embutida alcançada),
// está fora do período de validade em agora. Detecção própria (datas), sem depender do texto do
// erro do crypto/x509.
func (m *montadorCadeia) algumVencidoNoCaminho(agora time.Time) bool {
	candidatos := make([]*x509.Certificate, 0, len(m.conhecidas)+len(m.raizes.certs))
	candidatos = append(candidatos, m.conhecidas...)
	candidatos = append(candidatos, m.raizes.certs...)
	visitados := map[string]bool{}
	var visitar func(c *x509.Certificate, prof int) bool
	visitar = func(c *x509.Certificate, prof int) bool {
		if visitados[string(c.Raw)] {
			return false
		}
		visitados[string(c.Raw)] = true
		if !dentroDaValidade(c, agora) {
			return true
		}
		if prof > len(candidatos) {
			return false
		}
		for _, k := range candidatos {
			if emitidoPor(c, k) && visitar(k, prof+1) {
				return true
			}
		}
		return false
	}
	return visitar(m.cert, 0)
}

// classificarFalha traduz um erro de x509.Verify para as sentinelas desta spec.
func (m *montadorCadeia) classificarFalha(verr error, agora time.Time) error {
	if m.algumVencidoNoCaminho(agora) {
		return fmt.Errorf("%w: %v", ErrNFSeCadeiaVencida, verr)
	}
	var inv x509.CertificateInvalidError
	if errors.As(verr, &inv) {
		if inv.Reason == x509.Expired {
			return fmt.Errorf("%w: %v", ErrNFSeCadeiaVencida, verr)
		}
		return fmt.Errorf("%w: %v", ErrNFSeCadeiaInvalida, verr)
	}
	var ua x509.UnknownAuthorityError
	if errors.As(verr, &ua) {
		return fmt.Errorf("%w: %v", ErrNFSeCadeiaNaoICPBrasil, verr)
	}
	return fmt.Errorf("%w: %v", ErrNFSeCadeiaInvalida, verr)
}

func completarCadeia(ctx context.Context, client *http.Client, cert *x509.Certificate, intermediarias []*x509.Certificate) ([]*x509.Certificate, error) {
	if cert == nil {
		return nil, fmt.Errorf("%w: certificado ausente", ErrNFSeCadeiaInvalida)
	}
	if cert.IsCA {
		return nil, fmt.Errorf("%w: o certificado principal do .pfx é uma AC (%q), não um certificado final", ErrNFSeCadeiaInvalida, cert.Subject.String())
	}
	raizes, err := raizesICPBrasil()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNFSeCadeiaAmbienteIndisponivel, err)
	}

	m := novoMontadorCadeia(cert, raizes)
	for _, c := range intermediarias {
		m.adicionar(c)
	}

	tentadas := map[string]bool{} // pontas cuja AIA já foi seguida (ou que não têm como seguir)
	saltos := 0
	for {
		chains, verr := cert.Verify(x509.VerifyOptions{
			Roots:         raizes.pool,
			Intermediates: m.pool,
			// clientAuth explícito (não ExtKeyUsageAny): o certificado vai ser usado como
			// certificado de CLIENTE no mTLS do ADN. Certificado final sem EKU continua aceito
			// (o crypto/x509 trata ausência de EKU como "qualquer uso").
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		})
		if verr == nil {
			if saltos > 0 {
				log.Printf("[NFSeCadeia] cadeia ICP-Brasil completada via AIA (%d download(s))", saltos)
			}
			return intermediariasDaCadeia(escolherCadeia(chains)), nil
		}
		agora := time.Now()

		if !falhaCompletavel(verr, cert) {
			return nil, m.classificarFalha(verr, agora)
		}

		pontas, venceu := m.pontas(agora)
		var primeiroErro, erroTransitorio error
		registrar := func(e error) {
			if primeiroErro == nil {
				primeiroErro = e
			}
			if erroTransitorio == nil && errors.Is(e, ErrNFSeCadeiaDownloadFalhou) {
				erroTransitorio = e
			}
		}
		avancou := false
		for _, p := range pontas {
			chave := string(p.Raw)
			if tentadas[chave] {
				continue
			}
			if autoassinado(p) {
				tentadas[chave] = true
				registrar(fmt.Errorf("%w: cadeia termina em certificado autoassinado que não é raiz ICP-Brasil (%q)", ErrNFSeCadeiaNaoICPBrasil, p.Subject.String()))
				continue
			}
			if len(p.IssuingCertificateURL) == 0 {
				tentadas[chave] = true
				registrar(fmt.Errorf("%w: emissor de %q ausente e o certificado não tem AIA (CA Issuers) para completar a cadeia", ErrNFSeCadeiaNaoICPBrasil, p.Subject.String()))
				continue
			}
			if saltos >= nfseAIAMaxSaltos {
				return nil, fmt.Errorf("%w: limite de %d downloads AIA atingido sem fechar a cadeia", ErrNFSeCadeiaInvalida, nfseAIAMaxSaltos)
			}
			if ctx.Err() != nil {
				return nil, erroPrazoCadeia(ctx)
			}
			tentadas[chave] = true
			saltos++

			baixados, derr := baixarEmissorAIA(ctx, client, p)
			if derr != nil {
				if errors.Is(derr, ErrNFSeCadeiaAmbienteIndisponivel) {
					return nil, derr
				}
				registrar(derr)
				continue
			}
			// Toda a resposta entra no pool (bundle .p7b/PEM pode trazer vários níveis de uma
			// vez) — a integridade continua vindo do Verify.
			novos := 0
			for _, b := range baixados {
				if m.adicionar(b) {
					novos++
				}
			}
			if novos > 0 {
				avancou = true
				break
			}
			registrar(fmt.Errorf("%w: AIA de %q só trouxe certificados já conhecidos", ErrNFSeCadeiaNaoICPBrasil, p.Subject.String()))
		}
		if avancou {
			continue
		}

		// Nada mais a tentar.
		if venceu || m.algumVencidoNoCaminho(agora) {
			return nil, fmt.Errorf("%w: %v", ErrNFSeCadeiaVencida, verr)
		}
		if erroTransitorio != nil {
			return nil, erroTransitorio
		}
		if primeiroErro != nil {
			return nil, primeiroErro
		}
		return nil, m.classificarFalha(verr, agora)
	}
}

// falhaCompletavel — a falha do Verify pode sumir com mais ACs (via AIA): emissor desconhecido,
// ou uma AC candidata (não o próprio certificado final) fora do prazo — o crypto/x509 às vezes
// devolve o CertificateInvalidError da última candidata em vez de UnknownAuthorityError, e uma
// cópia renovada da AC pode existir na AIA.
func falhaCompletavel(verr error, cert *x509.Certificate) bool {
	var ua x509.UnknownAuthorityError
	if errors.As(verr, &ua) {
		return true
	}
	var inv x509.CertificateInvalidError
	if errors.As(verr, &inv) && inv.Reason == x509.Expired && inv.Cert != nil && !bytes.Equal(inv.Cert.Raw, cert.Raw) {
		return true
	}
	return false
}

// escolherCadeia — entre os caminhos devolvidos por Verify, o cuja raiz vence mais tarde (em
// empate, o mais curto).
func escolherCadeia(chains [][]*x509.Certificate) []*x509.Certificate {
	var melhor []*x509.Certificate
	for _, c := range chains {
		if len(c) == 0 {
			continue
		}
		if melhor == nil {
			melhor = c
			continue
		}
		rc, rm := c[len(c)-1].NotAfter, melhor[len(melhor)-1].NotAfter
		if rc.After(rm) || (rc.Equal(rm) && len(c) < len(melhor)) {
			melhor = c
		}
	}
	return melhor
}

// intermediariasDaCadeia devolve a cadeia sem o certificado final (índice 0) e sem a raiz
// (último índice).
func intermediariasDaCadeia(c []*x509.Certificate) []*x509.Certificate {
	if len(c) < 2 {
		return nil
	}
	out := make([]*x509.Certificate, 0, len(c)-2)
	out = append(out, c[1:len(c)-1]...)
	return out
}

// baixarEmissorAIA tenta as URLs "CA Issuers" de ponta em ordem e devolve TODOS os
// certificados da primeira resposta que contiver um emissor real de ponta (Subject == Issuer e
// assinatura conferida); resposta parseável sem o emissor faz tentar a próxima URL. Agregação
// das falhas (todas as URLs falharam), por prioridade:
//   - alguma falha transitória (rede/timeout/HTTP 5xx/prazo) -> ErrNFSeCadeiaDownloadFalhou (502);
//   - alguma falha de conteúdo/formato -> ErrNFSeCadeiaAIAInvalida (400);
//   - resposta sem o emissor -> ErrNFSeCadeiaNaoICPBrasil (400);
//   - destino bloqueado pelo anti-SSRF -> ErrNFSeCadeiaAIABloqueada (400);
//   - só esquemas não suportados -> ErrNFSeCadeiaAIAEsquemaNaoSuportado (400).
//
// Falha de ambiente (openssl/disco) é devolvida imediatamente.
func baixarEmissorAIA(ctx context.Context, client *http.Client, ponta *x509.Certificate) ([]*x509.Certificate, error) {
	urls := ponta.IssuingCertificateURL
	if len(urls) > nfseAIAMaxURLs {
		urls = urls[:nfseAIAMaxURLs]
	}
	var transitorio, conteudo, naoEmissor, bloqueado bool
	var detalhes []string
	for _, raw := range urls {
		if ctx.Err() != nil {
			return nil, erroPrazoCadeia(ctx)
		}
		certs, err := baixarAIA(ctx, client, raw)
		if err == nil {
			for _, c := range certs {
				if emitidoPor(ponta, c) {
					return certs, nil
				}
			}
			err = fmt.Errorf("%w: nenhum dos %d certificado(s) assina %q", errAIANaoEmissor, len(certs), ponta.Subject.String())
		}
		if errors.Is(err, ErrNFSeCadeiaAmbienteIndisponivel) {
			return nil, err
		}
		switch {
		case errors.Is(err, errAIAEsquema):
		case errors.Is(err, errAIADestinoBloqueado):
			bloqueado = true
		case errors.Is(err, errAIAConteudo):
			conteudo = true
		case errors.Is(err, errAIANaoEmissor):
			naoEmissor = true
		default:
			transitorio = true
		}
		log.Printf("[NFSeCadeia] download AIA falhou (%q): %q", raw, err.Error())
		detalhes = append(detalhes, fmt.Sprintf("%q: %q", raw, err.Error()))
	}
	resumo := strings.Join(detalhes, "; ")
	switch {
	case transitorio:
		return nil, fmt.Errorf("%w: %s", ErrNFSeCadeiaDownloadFalhou, resumo)
	case conteudo:
		return nil, fmt.Errorf("%w: %s", ErrNFSeCadeiaAIAInvalida, resumo)
	case naoEmissor:
		return nil, fmt.Errorf("%w: AIA de %q não trouxe o certificado emissor: %s", ErrNFSeCadeiaNaoICPBrasil, ponta.Subject.String(), resumo)
	case bloqueado:
		return nil, fmt.Errorf("%w: %s", ErrNFSeCadeiaAIABloqueada, resumo)
	default:
		return nil, fmt.Errorf("%w: %s", ErrNFSeCadeiaAIAEsquemaNaoSuportado, resumo)
	}
}

// baixarAIA faz UM download AIA. Erros: errAIAEsquema/errAIADestinoBloqueado/errAIAConteudo
// (embrulhados) conforme a causa; ErrNFSeCadeiaAmbienteIndisponivel se faltou openssl/disco;
// qualquer outro é falha transitória de download.
func baixarAIA(ctx context.Context, client *http.Client, raw string) ([]*x509.Certificate, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: URL AIA malformada", errAIAConteudo)
	}
	if verr := nfseAIAValidarURL(u); verr != nil {
		return nil, verr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: URL AIA inválida: %v", errAIAConteudo, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		// url.Error embrulha o erro do CheckRedirect/filtro de dial quando foram eles que
		// recusaram (errors.Is enxerga); senão é rede/timeout.
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	default:
		return nil, fmt.Errorf("%w: HTTP %d", errAIAConteudo, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, nfseAIAMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("falha ao ler resposta: %v", err)
	}
	if len(body) > nfseAIAMaxBytes {
		return nil, fmt.Errorf("%w: resposta excede %d bytes", errAIAConteudo, nfseAIAMaxBytes)
	}
	return parseCertificadosAIA(ctx, body)
}

// parseCertificadosAIA aceita: DER (um certificado, inclusive com espaço/quebra de linha no
// fim); PEM com blocos CERTIFICATE / X509 CERTIFICATE / TRUSTED CERTIFICATE e/ou PKCS7 (os
// certificados de todos os blocos são juntados); base64 puro sem cabeçalho PEM (decodificado e
// tentado como DER/PKCS#7); PKCS#7 certs-only (.p7b) em DER — este último via openssl, só
// depois do pré-filtro barato (pareceSignedDataDER). Erros de formato embrulham errAIAConteudo.
func parseCertificadosAIA(ctx context.Context, body []byte) ([]*x509.Certificate, error) {
	aparado := bytes.TrimSpace(body)
	if len(aparado) == 0 {
		return nil, fmt.Errorf("%w: resposta AIA vazia", errAIAConteudo)
	}
	// DER cru primeiro: o último byte de um DER legítimo pode ser 0x0a/0x20, então o trim só
	// é tentado depois.
	if c, err := x509.ParseCertificate(body); err == nil {
		return []*x509.Certificate{c}, nil
	}
	if c, err := x509.ParseCertificate(aparado); err == nil {
		return []*x509.Certificate{c}, nil
	}
	if bytes.Contains(aparado, []byte("-----BEGIN")) {
		return certificadosDePEMAIA(ctx, aparado)
	}
	if der, ok := decodificarBase64Puro(aparado); ok {
		if c, err := x509.ParseCertificate(der); err == nil {
			return []*x509.Certificate{c}, nil
		}
		return certificadosPKCS7DER(ctx, der)
	}
	return certificadosPKCS7DER(ctx, body)
}

// decodificarBase64Puro — corpo inteiro em base64 (com ou sem quebras de linha), sem cabeçalho
// PEM, cujo conteúdo parece DER (SEQUENCE).
func decodificarBase64Puro(data []byte) ([]byte, bool) {
	limpo := bytes.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n', '\v', '\f':
			return -1
		}
		return r
	}, data)
	if len(limpo) == 0 {
		return nil, false
	}
	der, err := base64.StdEncoding.DecodeString(string(limpo))
	if err != nil {
		der, err = base64.RawStdEncoding.DecodeString(string(limpo))
		if err != nil {
			return nil, false
		}
	}
	if len(der) == 0 || der[0] != 0x30 {
		return nil, false
	}
	return der, true
}

// certificadosDePEMAIA junta os certificados de todos os blocos CERTIFICATE, X509 CERTIFICATE,
// TRUSTED CERTIFICATE e PKCS7 (via openssl) da resposta.
func certificadosDePEMAIA(ctx context.Context, data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	juntar := func(cs ...*x509.Certificate) error {
		certs = append(certs, cs...)
		if len(certs) > nfseAIAMaxCertsPorResposta {
			return fmt.Errorf("%w: resposta AIA com mais de %d certificados", errAIAConteudo, nfseAIAMaxCertsPorResposta)
		}
		return nil
	}
	rest := data
	for {
		var bloco *pem.Block
		bloco, rest = pem.Decode(rest)
		if bloco == nil {
			break
		}
		switch bloco.Type {
		case "CERTIFICATE", "X509 CERTIFICATE":
			c, err := x509.ParseCertificate(bloco.Bytes)
			if err != nil {
				return nil, fmt.Errorf("%w: certificado PEM inválido na resposta AIA: %v", errAIAConteudo, err)
			}
			if err := juntar(c); err != nil {
				return nil, err
			}
		case "TRUSTED CERTIFICATE":
			// Formato OpenSSL: o DER do certificado seguido de dados auxiliares de confiança —
			// só o primeiro elemento ASN.1 é o certificado.
			var primeiro asn1.RawValue
			if _, err := asn1.Unmarshal(bloco.Bytes, &primeiro); err != nil {
				return nil, fmt.Errorf("%w: TRUSTED CERTIFICATE inválido na resposta AIA: %v", errAIAConteudo, err)
			}
			c, err := x509.ParseCertificate(primeiro.FullBytes)
			if err != nil {
				return nil, fmt.Errorf("%w: TRUSTED CERTIFICATE inválido na resposta AIA: %v", errAIAConteudo, err)
			}
			if err := juntar(c); err != nil {
				return nil, err
			}
		case "PKCS7":
			cs, err := certificadosPKCS7DER(ctx, bloco.Bytes)
			if err != nil {
				return nil, err
			}
			if err := juntar(cs...); err != nil {
				return nil, err
			}
		}
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("%w: resposta AIA em PEM sem certificado", errAIAConteudo)
	}
	return certs, nil
}

// oidPKCS7SignedDataDER — OID 1.2.840.113549.1.7.2 (pkcs7-signedData) já codificado em DER
// (tag 06, tamanho 09).
var oidPKCS7SignedDataDER = []byte{0x06, 0x09, 0x2A, 0x86, 0x48, 0x86, 0xF7, 0x0D, 0x01, 0x07, 0x02}

// pareceSignedDataDER — pré-filtro barato antes de gastar um fork do openssl: SEQUENCE que
// contém o OID pkcs7-signedData.
func pareceSignedDataDER(der []byte) bool {
	return len(der) > 0 && der[0] == 0x30 && bytes.Contains(der, oidPKCS7SignedDataDER)
}

// certificadosPKCS7DER extrai os certificados de um PKCS#7 certs-only em DER via
// `openssl pkcs7 -print_certs` — mesmo padrão seguro de subprocesso de nfse_pkcs12.go:
// diretório temporário próprio, arquivo 0600, limpeza garantida, timeout próprio derivado de
// ctx, ambiente mínimo (só PATH), exec.ErrNotFound tratado como problema de ambiente.
func certificadosPKCS7DER(ctx context.Context, der []byte) ([]*x509.Certificate, error) {
	if !pareceSignedDataDER(der) {
		return nil, fmt.Errorf("%w: resposta AIA não é DER, PEM, base64 nem PKCS#7 reconhecível", errAIAConteudo)
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNFSeCadeiaAmbienteIndisponivel, ErrNFSeOpensslIndisponivel)
	}
	tmpDir, err := os.MkdirTemp("", "nfse-p7b-*")
	if err != nil {
		return nil, fmt.Errorf("%w: %w: %v", ErrNFSeCadeiaAmbienteIndisponivel, ErrNFSeAmbienteTemporarioIndisponivel, err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			log.Printf("[NFSeCadeia] falha ao remover diretório temporário %q: %v", tmpDir, rmErr)
		}
	}()
	inPath := filepath.Join(tmpDir, "aia.p7b")
	if err := os.WriteFile(inPath, der, 0600); err != nil {
		return nil, fmt.Errorf("%w: %w: %v", ErrNFSeCadeiaAmbienteIndisponivel, ErrNFSeAmbienteTemporarioIndisponivel, err)
	}

	cctx, cancel := context.WithTimeout(ctx, nfseOpensslTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "openssl", "pkcs7", "-inform", "DER", "-in", inPath, "-print_certs")
	cmd.Env = nfseOpensslEnv()
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: %w", ErrNFSeCadeiaAmbienteIndisponivel, ErrNFSeOpensslIndisponivel)
		}
		if ctx.Err() != nil {
			return nil, erroPrazoCadeia(ctx)
		}
		return nil, fmt.Errorf("%w: resposta AIA não é PKCS#7 reconhecível (openssl pkcs7: %v: %q)",
			errAIAConteudo, err, TruncateRunes(strings.TrimSpace(stderr.String()), 300))
	}
	var certs []*x509.Certificate
	rest := stdout.Bytes()
	for {
		var bloco *pem.Block
		bloco, rest = pem.Decode(rest)
		if bloco == nil {
			break
		}
		if bloco.Type != "CERTIFICATE" {
			continue
		}
		if len(certs) >= nfseAIAMaxCertsPorResposta {
			return nil, fmt.Errorf("%w: PKCS#7 da resposta AIA com mais de %d certificados", errAIAConteudo, nfseAIAMaxCertsPorResposta)
		}
		c, perr := x509.ParseCertificate(bloco.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("%w: certificado inválido no PKCS#7 da resposta AIA: %v", errAIAConteudo, perr)
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("%w: PKCS#7 da resposta AIA não contém certificados", errAIAConteudo)
	}
	return certs, nil
}
