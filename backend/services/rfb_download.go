package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// Códigos de erro tipados do passo de download (gravados em rfb_requests.error_code).
const (
	RFBErrNotReady    = "NOT_READY"      // situacao PENDENTE/EM_PROCESSAMENTO — recuperável (rebaixar via webhook/manual)
	RFBErrURLExpirada = "URL_EXPIRADA"   // urlAssinada expirada/recusada e sem como renovar
	RFBErrURLInvalida = "URL_INVALIDA"   // urlAssinada rejeitada pela validação (SSRF) e sem outro meio de download
	RFBErrTooLarge    = "FILE_TOO_LARGE" // arquivo acima de rfbMaxDownloadBytes
	RFBErrDownload    = "DOWNLOAD_ERROR" // qualquer outra falha de download
)

// Limites de leitura de corpo de resposta (proteção de memória).
const (
	rfbMaxJSONBodyBytes int64 = 1 << 20 // 1 MiB: token, solicitação, situacao
	rfbMaxErrBodyBytes  int64 = 4 << 10 // 4 KiB: corpo de erro
)

// rfbMaxDownloadBytes é o teto do arquivo baixado (1 GiB). Variável só para os testes
// poderem baixá-lo; código de produção nunca altera.
var rfbMaxDownloadBytes int64 = 1 << 30

// RFBDownloadError é o erro tipado do download; Code vai para error_code (<= 50 chars).
type RFBDownloadError struct {
	Code    string
	Message string
}

func (e *RFBDownloadError) Error() string { return e.Message }

func newDownloadErr(code, format string, a ...interface{}) *RFBDownloadError {
	return &RFBDownloadError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// readBodyLimited lê até limit bytes; se houver mais, devolve RFBErrTooLarge; corpo vazio
// é erro (200 sem bytes nunca é um arquivo válido).
func readBodyLimited(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, newDownloadErr(RFBErrDownload, "falha ao ler resposta: %v", err)
	}
	if int64(len(body)) > limit {
		return nil, newDownloadErr(RFBErrTooLarge, "arquivo excede o limite de %d bytes", limit)
	}
	if len(body) == 0 {
		return nil, newDownloadErr(RFBErrDownload, "resposta vazia (HTTP 200 sem conteúdo)")
	}
	return body, nil
}

// rfbSSRFTestBypass desliga a validação de https/IP/porta do download por urlAssinada.
// SOMENTE testes do pacote services ligam isto (o servidor httptest é http://127.0.0.1:porta).
// Não é exportado nem lido de env: código de produção nunca o altera, então a validação
// real não é enfraquecida (ver testes de rejeição, que rodam com ele desligado).
var rfbSSRFTestBypass atomic.Bool

// blockedNets: faixas que a urlAssinada nunca pode alcançar (além das classes tratadas por
// métodos de net.IP: loopback, privado, link-local, multicast, unspecified).
var blockedNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "240.0.0.0/4", // "este rede", CGNAT, reservado
		"192.0.0.0/24", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", // IETF + documentação
		"192.88.99.0/24", "198.18.0.0/15", // 6to4 relay (obsoleto), benchmark
		"::/96",          // IPv4-compatível (obsoleto), :: e ::1
		"64:ff9b::/96",   // NAT64
		"64:ff9b:1::/48", // NAT64 local
		"2002::/16",      // 6to4
		"2001::/32",      // Teredo
		"2001:db8::/32",  // documentação
		"100::/64",       // discard
		"fec0::/10",      // site-local (obsoleto)
	} {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

// embeddedIPv4 extrai o IPv4 embutido em IPv4-compatível (::a.b.c.d), NAT64 (64:ff9b::/96)
// e 6to4 (2002:AABB:CCDD::/48); nil se não for nenhum desses.
func embeddedIPv4(ip net.IP) net.IP {
	ip16 := ip.To16()
	if ip16 == nil || ip.To4() != nil {
		return nil
	}
	allZero := func(b []byte) bool {
		for _, x := range b {
			if x != 0 {
				return false
			}
		}
		return true
	}
	switch {
	case allZero(ip16[:12]): // ::a.b.c.d
		return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])
	case ip16[0] == 0x00 && ip16[1] == 0x64 && ip16[2] == 0xff && ip16[3] == 0x9b && allZero(ip16[4:12]): // 64:ff9b::/96
		return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])
	case ip16[0] == 0x20 && ip16[1] == 0x02: // 2002:AABB:CCDD::/16
		return net.IPv4(ip16[2], ip16[3], ip16[4], ip16[5])
	}
	return nil
}

// isBlockedIP indica endereços que a urlAssinada nunca pode alcançar (SSRF). IPv4 embutido
// em IPv6 (mapped/compat/NAT64/6to4) é desembrulhado e revalidado.
func isBlockedIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	if emb := embeddedIPv4(ip); emb != nil && isBlockedIP(emb) {
		return true
	}
	return false
}

// hostAllowed aplica a allow-list opcional RFB_URL_ASSINADA_HOSTS (sufixos de host
// separados por vírgula). Vazia/indefinida = sem restrição. Vale para a URL inicial;
// redirects seguem só a guarda de dial (IP/porta/https).
func hostAllowed(host string) bool {
	raw := strings.TrimSpace(os.Getenv("RFB_URL_ASSINADA_HOSTS"))
	if raw == "" {
		return true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, suf := range strings.Split(raw, ",") {
		suf = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(suf), ".")))
		if suf == "" {
			continue
		}
		if host == suf || strings.HasSuffix(host, "."+suf) {
			return true
		}
	}
	return false
}

// ValidarURLAssinada faz a checagem estática (sem DNS) usada no webhook: só https na porta
// 443 (sem porta explícita = 443), host presente, sem credenciais embutidas, se o host for
// IP literal fora das faixas bloqueadas e, se RFB_URL_ASSINADA_HOSTS estiver definida, host
// dentro da allow-list. A resolução de nomes é validada no dial (safeDialContext), que
// também cobre redirects e DNS rebinding.
func ValidarURLAssinada(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("urlAssinada inválida")
	}
	if u.Scheme != "https" {
		return errors.New("urlAssinada deve usar https")
	}
	if u.Hostname() == "" {
		return errors.New("urlAssinada sem host")
	}
	if u.User != nil {
		return errors.New("urlAssinada com credenciais embutidas")
	}
	if p := u.Port(); p != "" && p != "443" {
		return errors.New("urlAssinada deve usar a porta 443")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedIP(ip) {
		return errors.New("urlAssinada aponta para endereço não permitido")
	}
	if !hostAllowed(u.Hostname()) {
		return errors.New("urlAssinada com host fora da lista permitida")
	}
	return nil
}

// safeDialContext resolve o host e recusa destinos loopback/privados/link-local/etc. e
// portas != 443; disca direto no IP validado (sem segunda resolução → sem rebinding).
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	if rfbSSRFTestBypass.Load() {
		return dialer.DialContext(ctx, network, addr)
	}
	if port != "443" {
		return nil, fmt.Errorf("host %s: porta %s não permitida (só 443)", host, port)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("host %s não resolveu", host)
	}
	for _, ipa := range ips {
		if isBlockedIP(ipa.IP) {
			return nil, fmt.Errorf("host %s resolve para endereço não permitido", host)
		}
	}
	var lastErr error
	for _, ipa := range ips {
		conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ipa.IP.String(), port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	return nil, lastErr
}

// newURLAssinadaClient monta o client do download por urlAssinada: sem Proxy (o dial
// guard precisa ver o destino real), no máximo 5 redirects, cada um só https.
func newURLAssinadaClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           safeDialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
	}
	return &http.Client{
		Timeout:   15 * time.Minute,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("redirects demais")
			}
			if req.URL.Scheme != "https" && !rfbSSRFTestBypass.Load() {
				return errors.New("redirect para destino não-https")
			}
			return nil
		},
	}
}

// urlStatusIndicaExpirada: respostas em que a URL assinada não serve mais (expirada,
// assinatura inválida, removida) e vale renovar via situacao.
func urlStatusIndicaExpirada(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return true
	}
	return false
}

// DownloadURLAssinada baixa o arquivo da URL pré-assinada. SEM Authorization (Bearer pode
// invalidar a assinatura). Nunca loga nem devolve a URL completa em erros — só o host — e
// NÃO inclui o corpo da resposta de erro no erro devolvido (pode trazer XML de
// assinatura/credencial do storage): só o status HTTP.
// 400/401/403/404/410 viram RFBErrURLExpirada (o chamador pode renovar via situacao).
func DownloadURLAssinada(rawURL string) ([]byte, error) {
	u, perr := url.Parse(rawURL)
	if perr != nil {
		return nil, newDownloadErr(RFBErrDownload, "urlAssinada inválida")
	}
	host := u.Hostname()
	if !rfbSSRFTestBypass.Load() {
		if err := ValidarURLAssinada(rawURL); err != nil {
			return nil, newDownloadErr(RFBErrDownload, "%v (host %s)", err, host)
		}
	}
	log.Printf("[RFB] Baixando por urlAssinada (host %s)", host)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, newDownloadErr(RFBErrDownload, "falha ao montar request de urlAssinada (host %s)", host)
	}
	resp, err := newURLAssinadaClient().Do(req)
	if err != nil {
		// *url.Error embute a URL completa — descarta e mantém só a causa.
		var ue *url.Error
		cause := err
		if errors.As(err, &ue) {
			cause = ue.Err
		}
		return nil, newDownloadErr(RFBErrDownload, "download por urlAssinada falhou (host %s): %v", host, cause)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[RFB] Download por urlAssinada FALHOU — HTTP %d (host %s)", resp.StatusCode, host)
		code := RFBErrDownload
		if urlStatusIndicaExpirada(resp.StatusCode) {
			code = RFBErrURLExpirada
		}
		return nil, newDownloadErr(code, "HTTP %d na urlAssinada (host %s)", resp.StatusCode, host)
	}
	body, err := readBodyLimited(resp.Body, rfbMaxDownloadBytes)
	if err != nil {
		var de *RFBDownloadError
		if errors.As(err, &de) {
			return nil, newDownloadErr(de.Code, "urlAssinada (host %s): %s", host, de.Message)
		}
		return nil, err
	}
	log.Printf("[RFB] Download por urlAssinada concluído (%d bytes)", len(body))
	return body, nil
}

// urlExpiraMargem: a URL é tratada como expirada 2 min antes do horário informado
// (relógios desalinhados e download longo não devem estourar no meio).
const urlExpiraMargem = 2 * time.Minute

// ParseURLExpiraEm interpreta urlAssinadaExpiraEm. Vazio → nil (desconhecida). Aceita
// RFC3339/RFC3339Nano e formatos sem offset (assumidos America/Sao_Paulo). Presente mas
// ilegível → TTL conservador de 1h a partir de agora (nunca NULL: uma URL sem expiração
// conhecida seria tratada como eterna).
func ParseURLExpiraEm(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, l := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(l, raw); err == nil {
			return &t
		}
	}
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*3600)
	}
	for _, l := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(l, raw, loc); err == nil {
			return &t
		}
	}
	t := time.Now().Add(time.Hour)
	log.Printf("[RFB] AVISO: urlAssinadaExpiraEm ilegível (%q) — usando TTL conservador de 1h", TruncateRunes(raw, 40))
	return &t
}

// RFBDownloadInput reúne o que o passo de download sabe da solicitação.
type RFBDownloadInput struct {
	Tiquete         string     // tiqueteSolicitacao
	TiqueteDownload string     // v1 (webhook); pode ser vazio
	APIVersao       string     // versão da SOLICITAÇÃO
	URLAssinada     string     // pode ser vazio
	URLExpiraEm     *time.Time // nil = desconhecida (tratada como válida)

	// OnURLRenovada (opcional) é chamada com a URL obtida via situacao ANTES de baixá-la,
	// para persisti-la (uma nova falha no download não perde a URL ainda válida).
	OnURLRenovada func(url string, expiraEm *time.Time)
}

// BaixarArquivoRFB é o helper único (débitos e créditos) do passo de download, com
// fallback em camadas — qualquer falha de um degrau cai para o próximo:
//  1. url_assinada não expirada (com margem de 2 min) → GET sem Authorization;
//  2. versão v2 → situacao (Bearer): CONCLUIDA → nova urlAssinada (persistida antes de
//     baixar); PENDENTE/EM_PROCESSAMENTO/desconhecido → NOT_READY; ERRO → erro da RFB;
//  3. tiquete_download (ou, na v1, o próprio tíquete) → DownloadArquivo v1.
//
// Devolve sempre *RFBDownloadError em caso de falha (Code → error_code).
func BaixarArquivoRFB(c *RFBClient, token string, in RFBDownloadInput) ([]byte, error) {
	v2 := in.APIVersao == RFBAPIVersaoV2

	// 1. urlAssinada persistida
	var urlErr error
	if in.URLAssinada != "" {
		if in.URLExpiraEm != nil && !time.Now().Add(urlExpiraMargem).Before(*in.URLExpiraEm) {
			log.Printf("[RFB] urlAssinada expirada (ou a menos de 2 min de expirar) — tentando renovar")
			urlErr = newDownloadErr(RFBErrURLExpirada, "urlAssinada expirada")
		} else {
			data, err := DownloadURLAssinada(in.URLAssinada)
			if err == nil {
				return data, nil
			}
			urlErr = err
			var de *RFBDownloadError
			if errors.As(err, &de) && de.Code == RFBErrTooLarge {
				return nil, err // renovar não resolve arquivo grande demais
			}
			log.Printf("[RFB] urlAssinada falhou — tentando situacao/tiquete: %v", err)
		}
	}

	// 2. situacao (v2)
	var situacaoErr error
	if v2 {
		sit, tok, err := c.consultarSituacao(token, in.Tiquete)
		if err == nil {
			token = tok // pode ter sido renovado por 401
			estado := strings.ToUpper(strings.TrimSpace(sit.Estado))
			switch estado {
			case "CONCLUIDA":
				if sit.UrlAssinada == "" {
					situacaoErr = newDownloadErr(RFBErrDownload, "situação CONCLUIDA sem urlAssinada")
					break
				}
				if verr := ValidarURLAssinada(sit.UrlAssinada); verr != nil && !rfbSSRFTestBypass.Load() {
					situacaoErr = newDownloadErr(RFBErrURLInvalida, "urlAssinada da situação rejeitada: %v (host %s)", verr, hostOfURL(sit.UrlAssinada))
					break
				}
				exp := ParseURLExpiraEm(sit.UrlAssinadaExpiraEm)
				if in.OnURLRenovada != nil {
					in.OnURLRenovada(sit.UrlAssinada, exp)
				}
				data, derr := DownloadURLAssinada(sit.UrlAssinada)
				if derr == nil {
					return data, nil
				}
				var de *RFBDownloadError
				if errors.As(derr, &de) && de.Code == RFBErrTooLarge {
					return nil, derr
				}
				situacaoErr = derr
			case "ERRO":
				code := TruncateRunes(sit.CodigoErro, 50)
				if code == "" {
					code = "RFB_ERRO"
				}
				return nil, newDownloadErr(code, "%s", TruncateRunes(firstNonEmpty(sit.MensagemErro, "RFB reportou erro no processamento"), 1000))
			default:
				// PENDENTE, EM_PROCESSAMENTO e qualquer estado desconhecido não-terminal.
				return nil, newDownloadErr(RFBErrNotReady, "arquivo ainda não está pronto na RFB (estado %s)", TruncateRunes(estado, 40))
			}
		} else {
			situacaoErr = newDownloadErr(RFBErrDownload, "consulta de situação falhou: %v", err)
		}
	}

	// 3. tíquete de download v1
	tiqueteParaDownload := in.TiqueteDownload
	if tiqueteParaDownload == "" && !v2 {
		// comportamento v1 histórico: cai no tíquete da solicitação
		tiqueteParaDownload = in.Tiquete
		log.Printf("[RFB] AVISO: tiqueteDownload não definido, usando tiqueteSolicitacao '%s'", in.Tiquete)
	}
	if tiqueteParaDownload == "" {
		switch {
		case situacaoErr != nil:
			return nil, situacaoErr
		case urlErr != nil:
			return nil, urlErr
		}
		return nil, newDownloadErr(RFBErrDownload, "sem urlAssinada nem tíquete de download")
	}
	data, err := c.DownloadArquivo(token, tiqueteParaDownload)
	if err != nil {
		var de *RFBDownloadError
		if errors.As(err, &de) {
			return nil, de
		}
		return nil, newDownloadErr(RFBErrDownload, "%v", err)
	}
	return data, nil
}

func hostOfURL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "?"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// downloadErrCode extrai o Code de um erro de BaixarArquivoRFB (default DOWNLOAD_ERROR),
// truncado a 50 (tamanho de error_code).
func downloadErrCode(err error) string {
	var de *RFBDownloadError
	if errors.As(err, &de) && de.Code != "" {
		return TruncateRunes(de.Code, 50)
	}
	return RFBErrDownload
}

// newRFBDownloadInput monta o RFBDownloadInput a partir das colunas lidas de rfb_requests.
func newRFBDownloadInput(tiquete, apiVersao string, tiqueteDownload, urlAssinada sql.NullString, urlExpiraEm sql.NullTime) RFBDownloadInput {
	in := RFBDownloadInput{Tiquete: tiquete, APIVersao: apiVersao}
	if tiqueteDownload.Valid {
		in.TiqueteDownload = tiqueteDownload.String
	}
	if urlAssinada.Valid {
		in.URLAssinada = urlAssinada.String
	}
	if urlExpiraEm.Valid {
		t := urlExpiraEm.Time
		in.URLExpiraEm = &t
	}
	return in
}

// persistURLRenovada devolve o callback que grava a urlAssinada obtida via situacao (e sua
// expiração) na solicitação, antes do download.
func persistURLRenovada(db *sql.DB, requestID string) func(string, *time.Time) {
	return func(u string, exp *time.Time) {
		var expArg sql.NullTime
		if exp != nil {
			expArg = sql.NullTime{Time: *exp, Valid: true}
		}
		if _, err := db.Exec(`
			UPDATE rfb_requests SET url_assinada = $1, url_assinada_expira_em = $2, updated_at = CURRENT_TIMESTAMP
			WHERE id = $3
		`, u, expArg, requestID); err != nil {
			log.Printf("[RFB] AVISO: falha ao persistir urlAssinada renovada (request %s): %v", requestID, err)
		}
	}
}
