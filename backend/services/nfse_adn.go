// Package services — cliente mTLS do Portal Nacional ADN (NFS-e).
//
// Contrato da API (confirmado pelo Swagger oficial "API NFS-e - ADN Contribuinte" v1 —
// spec-nfse-adn-parser-real.md): GET {base}/contribuintes/DFe/{NSU}?lote=true, autenticação por
// mTLS, resposta JSON PascalCase LoteDistribuicaoNSUResponse. HTTP 200 DOCUMENTOS_LOCALIZADOS =
// lote; HTTP 404 NENHUM_DOCUMENTO_LOCALIZADO = fim da fila; HTTP 400 REJEICAO = erro. Cada item
// (DistribuicaoNSU) traz o XML (gzip + base64) em ArquivoXml — decodificação e extração dos
// campos fiscais em nfse_adn_xml.go. Este arquivo (+ nfse_adn_xml.go) é o PONTO ÚNICO que
// conhece o formato do ADN — não espalhar detalhes de formato para handlers/persistência.
package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fb_apu02/crypto"

	"software.sslmate.com/src/go-pkcs12"
)

// nfseADNExtraCAFileEnv aponta (opcionalmente) para um arquivo PEM de CA adicional usado para
// verificar o certificado TLS do PRÓPRIO Portal Nacional ADN (não o certificado mTLS do
// cliente, que vem de nfse_certificados). Sem esta variável, BuildNFSeMTLSClient usa o pool de
// CAs padrão do sistema operacional (comportamento de produção, igual a qualquer outro client
// Go). Ela existe para permitir: (1) pinning operacional de uma CA específica, se necessário no
// futuro; e (2) testar este client contra um servidor de teste com certificado autoassinado
// (httptest.Server + TLS) sem precisar modificar o pool de certificados do sistema operacional
// nem depender do cache interno (sync.Once) do pool padrão do Go — lida do ambiente a cada
// chamada, nunca cacheada.
const nfseADNExtraCAFileEnv = "NFSE_ADN_TLS_CA_FILE"

// Ambientes do Portal Nacional ADN — confirmado por fonte oficial (manual "APIs ADN Sistema
// Nacional NFS-e" v1.0, 12/02/2026, gov.br/nfse). "contribuintes" é plural — o fórum ACBr
// confirma 404 no singular.
const (
	NFSeADNBaseURLProducao         = "https://adn.nfse.gov.br"
	NFSeADNBaseURLProducaoRestrita = "https://adn.producaorestrita.nfse.gov.br"
)

// nfseADNHTTPTimeout é o timeout por chamada do client mTLS — mesma ordem de grandeza das
// chamadas síncronas em rfb.go (o download, que pode demorar mais, usa timeout próprio lá).
const nfseADNHTTPTimeout = 30 * time.Second

// nfseADNMaxBodyBytes limita a leitura do corpo da resposta do ADN (defesa contra resposta
// anormalmente grande — mesmo princípio de readBodyLimited/rfbMaxJSONBodyBytes em rfb.go).
// Passou do limite, a resposta é REJEITADA como formato inesperado (nunca truncada em silêncio —
// um JSON cortado poderia até decodificar parcialmente). Variável só para os testes reduzirem.
var nfseADNMaxBodyBytes int64 = 20 << 20 // 20 MiB

// Status de processamento (enum StatusProcessamentoDistribuicao do Swagger).
const (
	NFSeADNStatusDocumentosLocalizados = "DOCUMENTOS_LOCALIZADOS"
	NFSeADNStatusNenhumDocumento       = "NENHUM_DOCUMENTO_LOCALIZADO"
	NFSeADNStatusRejeicao              = "REJEICAO"
)

// Tipos de documento (enum TipoDocumentoRequisicao do Swagger: NENHUM, DPS,
// PEDIDO_REGISTRO_EVENTO, NFSE, EVENTO, CNC). Só NFSE e EVENTO (de cancelamento) são
// processados; os demais são ignorados (contados).
const (
	NFSeADNTipoDocumentoNFSe   = "NFSE"
	NFSeADNTipoDocumentoEvento = "EVENTO"
)

// nfseADNEventosCancelamento são os valores do enum TipoEvento do Swagger que EFETIVAM o
// cancelamento da nota. Ficam de fora, de propósito: SOLICITACAO_CANCELAMENTO_ANALISE_FISCAL
// (só um pedido, ainda em análise) e CANCELAMENTO_INDEFERIDO_ANALISE_FISCAL (pedido negado — a
// nota continua válida).
var nfseADNEventosCancelamento = map[string]bool{
	"CANCELAMENTO":                         true,
	"CANCELAMENTO_POR_SUBSTITUICAO":        true,
	"CANCELAMENTO_DEFERIDO_ANALISE_FISCAL": true,
	"CANCELAMENTO_POR_OFICIO":              true,
}

// EhEventoCancelamentoNFSe informa se o TipoEvento (enum do Swagger) efetiva o cancelamento.
func EhEventoCancelamentoNFSe(tipoEvento string) bool {
	return nfseADNEventosCancelamento[strings.ToUpper(strings.TrimSpace(tipoEvento))]
}

// nfseADNFilaConfig reúne os limites do percurso da fila de distribuição. Variável de pacote
// (nfseADNFilaConfigPadrao) para que os testes zerem pausas/backoff sem esperar de verdade.
type nfseADNFilaConfig struct {
	// MaxLotes: lotes por ciclo; ao atingir, o ciclo encerra com SUCESSO PARCIAL (o cursor
	// já está salvo — o próximo ciclo continua dali).
	MaxLotes int
	// MaxDuracao: tempo máximo do ciclo; idem, sucesso parcial.
	MaxDuracao time.Duration
	// PausaEntreLotes: espera entre um lote e o próximo, para não provocar 429.
	PausaEntreLotes time.Duration
	// MaxTentativas: tentativas (total) de um mesmo lote diante de 429/5xx; esgotadas, o ciclo
	// encerra com sucesso parcial.
	MaxTentativas int
	// BackoffBase: espera da 1ª retentativa sem Retry-After (dobra a cada tentativa).
	BackoffBase time.Duration
	// EsperaMax: teto de qualquer espera (Retry-After ou backoff).
	EsperaMax time.Duration
	// Sleep espera d respeitando ctx (injetável nos testes).
	Sleep func(ctx context.Context, d time.Duration) error
}

var nfseADNFilaConfigPadrao = nfseADNFilaConfig{
	MaxLotes:        200,
	MaxDuracao:      5 * time.Minute,
	PausaEntreLotes: time.Second,
	MaxTentativas:   3,
	BackoffBase:     2 * time.Second,
	EsperaMax:       60 * time.Second,
	Sleep:           nfseSleepCtx,
}

// nfseSleepCtx espera d ou até ctx terminar (devolve ctx.Err() nesse caso).
func nfseSleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Erros sentinela — permitem ao handler escolher a mensagem HTTP certa sem acoplar o handler
// aos detalhes de formato/transporte tratados aqui.
var (
	// ErrNFSeADNAutenticacaoFalhou cobre tanto falha de handshake mTLS (certificado
	// rejeitado antes de qualquer resposta HTTP) quanto HTTP 401/403 do ADN.
	ErrNFSeADNAutenticacaoFalhou = errors.New("autenticação mTLS recusada pelo Portal Nacional ADN")
	// ErrNFSeADNFormatoInesperado cobre corpo de resposta que não é o
	// LoteDistribuicaoNSUResponse esperado (ver parseNFSeADNLote).
	ErrNFSeADNFormatoInesperado = errors.New("formato de resposta inesperado do Portal Nacional ADN")
	// ErrNFSeADNComunicacaoFalhou cobre qualquer outra falha de comunicação que não seja
	// autenticação, formato, rejeição ou transitório.
	ErrNFSeADNComunicacaoFalhou = errors.New("falha ao comunicar com o Portal Nacional ADN")
	// ErrNFSeADNRejeicao: HTTP 400 StatusProcessamento=REJEICAO (códigos de Erros no log).
	ErrNFSeADNRejeicao = errors.New("consulta rejeitada pelo Portal Nacional ADN")
	// ErrNFSeADNTransitorio: HTTP 429 ou 5xx, ou falha de rede transitória (timeout, conexão
	// recusada/resetada, DNS, EOF no meio da resposta — ver ehErroRedeTransitorioADN) — o
	// chamador pode tentar de novo (ver NFSeADNErroTransitorio.RetryAfter). PercorrerFilaADN nunca
	// devolve este erro: esgotadas as tentativas, encerra o ciclo com sucesso parcial.
	ErrNFSeADNTransitorio = errors.New("Portal Nacional ADN temporariamente indisponível (429/5xx/rede)")
)

// NFSeADNErroTransitorio detalha um ErrNFSeADNTransitorio (errors.Is funciona).
type NFSeADNErroTransitorio struct {
	// StatusHTTP é 0 quando a falha foi de rede (sem resposta HTTP) — ver Causa.
	StatusHTTP int
	// RetryAfter é o valor do cabeçalho Retry-After (segundos ou data HTTP) já convertido em
	// duração; 0 quando ausente/inválido.
	RetryAfter time.Duration
	// Causa é o erro de rede original (só quando StatusHTTP == 0).
	Causa error
}

func (e *NFSeADNErroTransitorio) Error() string {
	if e.StatusHTTP == 0 {
		return fmt.Sprintf("%v (falha de rede: %v)", ErrNFSeADNTransitorio, e.Causa)
	}
	return fmt.Sprintf("%v (HTTP %d, Retry-After=%s)", ErrNFSeADNTransitorio, e.StatusHTTP, e.RetryAfter)
}

// descricao resume o transitório para log ("HTTP 503" ou "falha de rede").
func (e *NFSeADNErroTransitorio) descricao() string {
	if e.StatusHTTP == 0 {
		return "falha de rede"
	}
	return fmt.Sprintf("HTTP %d", e.StatusHTTP)
}

// ehErroRedeTransitorioADN classifica um erro de transporte (sem resposta HTTP, ou na leitura
// do corpo) como TRANSITÓRIO — timeout, conexão recusada/resetada/abortada, rede/host
// inalcançável, falha de DNS, EOF inesperado — para seguir o caminho de retentativa em vez de
// ser reportado como falha de autenticação. Erros de TLS (alerta do servidor — "remote error:
// tls: ..." —, falha local de handshake, verificação de certificado) NUNCA são transitórios: no
// mTLS do ADN são a forma de o servidor recusar o certificado do cliente.
func ehErroRedeTransitorioADN(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && (opErr.Op == "remote error" || opErr.Op == "local error") {
		return false
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return false
	}
	var recErr tls.RecordHeaderError
	if errors.As(err, &recErr) {
		return false
	}
	var unkAuth x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invErr x509.CertificateInvalidError
	if errors.As(err, &unkAuth) || errors.As(err, &hostErr) || errors.As(err, &invErr) {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "remote error: tls") || strings.Contains(msg, "bad record MAC") || strings.Contains(msg, "handshake failure") {
		return false
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	for _, errno := range []syscall.Errno{syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED, syscall.EPIPE,
		syscall.ETIMEDOUT, syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.ENETDOWN, syscall.EHOSTDOWN} {
		if errors.Is(err, errno) {
			return true
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	if opErr != nil && opErr.Op == "dial" {
		return true
	}
	return false
}

func (e *NFSeADNErroTransitorio) Unwrap() error { return ErrNFSeADNTransitorio }

// ResolveNFSeADNBaseURL resolve a base URL do Portal Nacional ADN. NFSE_ADN_BASE_URL
// sobrescreve diretamente (usado pelos testes para apontar para o httptest.Server local);
// na ausência dela, NFSE_ADN_AMBIENTE=producao_restrita seleciona o ambiente de produção
// restrita, qualquer outro valor (ou ausência) cai em produção. Mesmo padrão de
// NewRFBClient/RFB_API_URL em rfb.go.
func ResolveNFSeADNBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("NFSE_ADN_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("NFSE_ADN_AMBIENTE")), "producao_restrita") {
		return NFSeADNBaseURLProducaoRestrita
	}
	return NFSeADNBaseURLProducao
}

// NFSeADNCertificado reúne o certificado/senha JÁ DESCRIPTOGRAFADOS necessários para montar o
// client mTLS. Nunca é logado nem persistido — só vive na memória da requisição em andamento.
type NFSeADNCertificado struct {
	PfxBase64 string // conteúdo do .pfx em base64 (igual ao que SaveNFSeCertificadoHandler recebeu)
	Senha     string // senha do .pfx
}

// DecryptNFSeCertificado descriptografa os dois campos sensíveis de nfse_certificados
// (certificado_pfx, certificado_senha), ambos protegidos com crypto.EncryptField (AES-256-GCM).
func DecryptNFSeCertificado(pfxEncrypted, senhaEncrypted string) (NFSeADNCertificado, error) {
	pfx, err := crypto.DecryptField(pfxEncrypted)
	if err != nil {
		return NFSeADNCertificado{}, fmt.Errorf("falha ao descriptografar certificado_pfx: %w", err)
	}
	senha, err := crypto.DecryptField(senhaEncrypted)
	if err != nil {
		return NFSeADNCertificado{}, fmt.Errorf("falha ao descriptografar certificado_senha: %w", err)
	}
	return NFSeADNCertificado{PfxBase64: pfx, Senha: senha}, nil
}

// BuildNFSeMTLSClient decodifica o .pfx (PKCS#12, via software.sslmate.com/src/go-pkcs12) e
// monta um *http.Client cujo Transport apresenta o certificado cliente (mTLS) em toda
// chamada — o Portal Nacional ADN não usa OAuth2/Bearer, autentica via certificado digital A1
// (ver Design Notes da spec-nfse-captura-plano.md). Devolve também o *x509.Certificate
// decodificado — usado por ExtractCNPJFromCertificateSubject para identificar o CNPJ titular
// do certificado sem precisar decodificar o .pfx de novo.
func BuildNFSeMTLSClient(cert NFSeADNCertificado) (*http.Client, *x509.Certificate, error) {
	pfxBytes, err := base64.StdEncoding.DecodeString(cert.PfxBase64)
	if err != nil {
		return nil, nil, fmt.Errorf("certificado_pfx não é base64 válido: %w", err)
	}

	// DecodeChain (não Decode) — certificados ICP-Brasil reais frequentemente vêm acompanhados
	// da cadeia de CA intermediária no mesmo .pfx; Decode rejeita esse caso com "expected
	// exactly two safe bags in the PFX PDU" (confirmado lendo vendor/.../pkcs12.go). caCerts só
	// é usado para montar a cadeia completa do tls.Certificate abaixo (leaf primeiro, depois
	// as intermediárias, nessa ordem) — necessário para o handshake mTLS funcionar quando o
	// certificado real tem cadeia.
	privateKey, x509Cert, caCerts, err := pkcs12.DecodeChain(pfxBytes, cert.Senha)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao decodificar .pfx (senha incorreta ou arquivo inválido): %w", err)
	}

	chain := make([][]byte, 0, 1+len(caCerts))
	chain = append(chain, x509Cert.Raw)
	for _, ca := range caCerts {
		chain = append(chain, ca.Raw)
	}

	tlsCert := tls.Certificate{
		Certificate: chain,
		PrivateKey:  privateKey,
		// Leaf é o certificado do titular (não a CA) — é ele que
		// ExtractCNPJFromCertificateSubject usa para extrair o CNPJ.
		Leaf: x509Cert,
	}

	// GetClientCertificate (não Certificates): com Certificates, o Go só envia o certificado
	// se o emissor dele casar com uma das CAs listadas pelo servidor no CertificateRequest — o
	// ADN lista só as RAÍZES ICP-Brasil, que nunca são o emissor direto de um certificado final
	// (sempre há AC intermediária), então o Go não enviava certificado nenhum. Aqui o
	// certificado (com a cadeia completada no upload, ver nfse_cadeia.go) é SEMPRE enviado e o
	// servidor decide.
	tlsConfig := &tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &tlsCert, nil
		},
	}
	if caFile := strings.TrimSpace(os.Getenv(nfseADNExtraCAFileEnv)); caFile != "" {
		caPEM, rerr := os.ReadFile(caFile)
		if rerr != nil {
			return nil, nil, fmt.Errorf("falha ao ler %s=%q: %w", nfseADNExtraCAFileEnv, caFile, rerr)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, nil, fmt.Errorf("%s=%q não contém nenhum certificado PEM válido", nfseADNExtraCAFileEnv, caFile)
		}
		tlsConfig.RootCAs = pool
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	return &http.Client{Transport: transport, Timeout: nfseADNHTTPTimeout}, x509Cert, nil
}

// ExtractCNPJFromCertificateSubject extrai o CNPJ titular do certificado a partir do Common
// Name (CN) do Subject — formato padrão ICP-Brasil para certificados e-CNPJ A1:
// CN = "RAZAO SOCIAL:14DIGITOSDOCNPJ" (14 dígitos após o último ':').
//
// DECISÃO (não estava no Code Map/Tasks da spec — necessária para implementar a Task 3
// literalmente): a tabela "companies" NÃO tem coluna de CNPJ (removida na migration 023 —
// o projeto modela filiais/CNPJs dinamicamente a partir dos documentos já importados, ver
// filiais_importadas.go) e nenhuma tabela nova desta sub-spec guarda um CNPJ "da empresa".
// Sem uma fonte de CNPJ, não há como decidir se um documento do ADN é tomado ou prestado pela
// empresa. Usar o Subject do próprio certificado mTLS (que já precisa estar carregado para a
// chamada) evita inventar schema novo ou modificar a migration 133 (congelada). O formato
// "NOME:CNPJ"/"NOME:CPF" é o padrão documentado da ICP-Brasil (DOC-ICP-04) para e-CNPJ/e-CPF —
// mesmo assim, fica marcado como suposição a confirmar com um certificado de teste real, igual
// ao parser de payload abaixo.
func ExtractCNPJFromCertificateSubject(cert *x509.Certificate) (string, error) {
	cn := cert.Subject.CommonName
	parts := strings.Split(cn, ":")
	last := strings.TrimSpace(parts[len(parts)-1])
	digits := OnlyDigits(last)
	if len(digits) != 14 {
		return "", fmt.Errorf(
			"não foi possível extrair um CNPJ (14 dígitos) do Subject do certificado (CN=%q) — formato ICP-Brasil e-CNPJ esperado é \"RAZAO SOCIAL:CNPJ\"",
			cn,
		)
	}
	return digits, nil
}

// OnlyDigits devolve apenas os caracteres numéricos de s — usado para normalizar
// CPF/CNPJ vindos de fontes externas (Subject de certificado, payload do ADN) antes de
// comparar ou persistir. Exportada para reaproveitamento em outros pacotes (ex.:
// handlers/nfse_busca.go), evitando duplicar a mesma lógica.
func OnlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nfseADNEnum aceita um enum do ADN serializado como string (forma documentada no Swagger) ou,
// defensivamente, como número (serialização padrão de enum .NET) — nesse caso guarda os dígitos
// como string, e o valor aparece no log de diagnóstico em vez de quebrar o lote inteiro.
type nfseADNEnum string

func (e *nfseADNEnum) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		*e = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*e = nfseADNEnum(strings.ToUpper(strings.TrimSpace(v)))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("enum do ADN em formato inesperado: %s", TruncateRunes(s, 50))
	}
	*e = nfseADNEnum(n.String())
	return nil
}

// NFSeADNMensagem é o MensagemProcessamento do Swagger (itens de Alertas/Erros). Mensagem é um
// objeto opaco no Swagger ("Enum" sem propriedades) — guardado cru, nunca interpretado.
type NFSeADNMensagem struct {
	Codigo      string          `json:"Codigo"`
	Descricao   string          `json:"Descricao"`
	Complemento string          `json:"Complemento"`
	Mensagem    json.RawMessage `json:"Mensagem"`
	Parametros  []string        `json:"Parametros"`
}

// NFSeADNDistribuicao é o DistribuicaoNSU do Swagger: UM documento da fila.
type NFSeADNDistribuicao struct {
	NSU             *int64      `json:"NSU"`
	ChaveAcesso     string      `json:"ChaveAcesso"`
	TipoDocumento   nfseADNEnum `json:"TipoDocumento"`
	TipoEvento      nfseADNEnum `json:"TipoEvento"`
	ArquivoXml      string      `json:"ArquivoXml"` // XML em gzip + base64 (ver nfse_adn_xml.go)
	DataHoraGeracao string      `json:"DataHoraGeracao"`
}

// NSUValor devolve o NSU (0 quando ausente).
func (d NFSeADNDistribuicao) NSUValor() int64 {
	if d.NSU == nil {
		return 0
	}
	return *d.NSU
}

// NFSeADNLote é o LoteDistribuicaoNSUResponse do Swagger.
type NFSeADNLote struct {
	StatusProcessamento   nfseADNEnum           `json:"StatusProcessamento"`
	LoteDFe               []NFSeADNDistribuicao `json:"LoteDFe"`
	Alertas               []NFSeADNMensagem     `json:"Alertas"`
	Erros                 []NFSeADNMensagem     `json:"Erros"`
	TipoAmbiente          nfseADNEnum           `json:"TipoAmbiente"`
	VersaoAplicativo      string                `json:"VersaoAplicativo"`
	DataHoraProcessamento string                `json:"DataHoraProcessamento"`
}

// FimDaFila indica que o ADN não tem mais documentos a partir do NSU consultado.
func (l NFSeADNLote) FimDaFila() bool {
	return string(l.StatusProcessamento) == NFSeADNStatusNenhumDocumento
}

// FaixaNSU devolve o menor e o maior NSU do lote (0,0 se nenhum item tiver NSU).
func (l NFSeADNLote) FaixaNSU() (menor, maior int64) {
	primeiro := true
	for _, d := range l.LoteDFe {
		if d.NSU == nil {
			continue
		}
		n := *d.NSU
		if primeiro || n < menor {
			menor = n
		}
		if primeiro || n > maior {
			maior = n
		}
		primeiro = false
	}
	return menor, maior
}

// codigosMensagensADN devolve "Codigo: Descricao" de cada mensagem (para log com %q — nunca XML).
func codigosMensagensADN(msgs []NFSeADNMensagem) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		s := strings.TrimSpace(m.Codigo)
		if d := strings.TrimSpace(m.Descricao); d != "" {
			s += ": " + TruncateRunes(d, 200)
		}
		out = append(out, s)
	}
	return out
}

// parseNFSeADNLote decodifica o corpo da resposta do ADN (LoteDistribuicaoNSUResponse). Um corpo
// que não é JSON, ou sem StatusProcessamento, é formato inesperado — nunca grava "lixo" (foi
// exatamente o que aconteceu com o formato suposto anterior: o JSON real decodificava pra uma
// struct vazia sem erro).
func parseNFSeADNLote(body []byte) (NFSeADNLote, error) {
	var lote NFSeADNLote
	if err := json.Unmarshal(body, &lote); err != nil {
		return NFSeADNLote{}, fmt.Errorf("corpo não é o LoteDistribuicaoNSUResponse esperado: %w", err)
	}
	if lote.StatusProcessamento == "" {
		return NFSeADNLote{}, errors.New("corpo sem StatusProcessamento")
	}
	return lote, nil
}

// parseRetryAfter interpreta o cabeçalho Retry-After (segundos ou data HTTP). 0 se ausente ou
// inválido; nunca negativo.
func parseRetryAfter(v string, agora time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		if secs > 24*3600 {
			secs = 24 * 3600
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(agora); d > 0 {
			return d
		}
	}
	return 0
}

// explicarErroHandshakeADN devolve uma explicação para o operador (SÓ para o log) quando o erro
// de transporte tem uma causa provável não óbvia, ou "" caso contrário. "bad record MAC" no TLS
// 1.3 é como o crypto/tls do Go reporta o alerta do servidor que chega logo depois de o cliente
// enviar (ou não enviar) o certificado — com certificado descartável, em TLS 1.2 o mesmo
// servidor responde "handshake failure". É a causa PROVÁVEL, não certa: corrupção/interferência
// no caminho de rede (proxy, inspeção TLS) produz o mesmo erro.
func explicarErroHandshakeADN(err error) string {
	if err == nil || !strings.Contains(err.Error(), "bad record MAC") {
		return ""
	}
	return "no TLS 1.3, 'bad record MAC' tem como causa provável a rejeição do certificado do cliente pelo servidor " +
		"(cadeia ICP-Brasil incompleta, CNPJ não habilitado no ADN ou certificado não aceito); também pode ser " +
		"interferência de rede (proxy/inspeção TLS) — reenvie o certificado (o upload completa a cadeia via AIA), " +
		"confira a habilitação do CNPJ e, se persistir, o caminho de rede até o ADN"
}

// BuscarLoteDFeADN chama GET {baseURL}/contribuintes/DFe/{nsu}?lote=true UMA vez (sem retry).
//
//   - 200 DOCUMENTOS_LOCALIZADOS → lote (FimDaFila()=false);
//   - 200/404 NENHUM_DOCUMENTO_LOCALIZADO → lote com FimDaFila()=true, SEM erro;
//   - 400 REJEICAO → ErrNFSeADNRejeicao (códigos de Erros no log com %q);
//   - 401/403 ou handshake mTLS recusado → ErrNFSeADNAutenticacaoFalhou;
//   - 429/5xx → *NFSeADNErroTransitorio (errors.Is ErrNFSeADNTransitorio), com Retry-After;
//   - falha de rede transitória (ehErroRedeTransitorioADN) → *NFSeADNErroTransitorio (StatusHTTP 0);
//   - corpo acima de nfseADNMaxBodyBytes → ErrNFSeADNFormatoInesperado;
//   - ctx cancelado/expirado → ErrNFSeADNComunicacaoFalhou envolvendo ctx.Err();
//   - corpo irreconhecível → ErrNFSeADNFormatoInesperado; demais → ErrNFSeADNComunicacaoFalhou.
func BuscarLoteDFeADN(ctx context.Context, httpClient *http.Client, baseURL string, nsu int64) (NFSeADNLote, error) {
	if nsu < 0 {
		nsu = 0
	}
	endpoint := fmt.Sprintf("%s/contribuintes/DFe/%d?lote=true", strings.TrimRight(baseURL, "/"), nsu)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return NFSeADNLote{}, fmt.Errorf("falha ao montar requisição ao Portal Nacional ADN: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return NFSeADNLote{}, fmt.Errorf("%w: chamada interrompida: %w", ErrNFSeADNComunicacaoFalhou, cerr)
		}
		// Falha de rede transitória (timeout, conexão recusada/resetada, DNS, EOF) → retentativa,
		// nunca "autenticação".
		if ehErroRedeTransitorioADN(err) {
			return NFSeADNLote{}, &NFSeADNErroTransitorio{Causa: err}
		}
		// Sem resposta HTTP nenhuma e não transitório: handshake mTLS falhou (certificado
		// rejeitado pelo servidor). Tratado como falha de autenticação.
		if explicacao := explicarErroHandshakeADN(err); explicacao != "" {
			// Explicação (causa provável) só no log do operador — o erro devolvido e as
			// mensagens HTTP fixas do handler não mudam.
			log.Printf("[NFSeADN] chamada a %q falhou: %q — %s", endpoint, err.Error(), explicacao)
		}
		return NFSeADNLote{}, fmt.Errorf("%w: %v", ErrNFSeADNAutenticacaoFalhou, err)
	}
	defer resp.Body.Close()

	// Lê até limite+1 bytes: passar do limite é DETECTADO (formato inesperado), nunca truncado.
	body, err := io.ReadAll(io.LimitReader(resp.Body, nfseADNMaxBodyBytes+1))
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return NFSeADNLote{}, fmt.Errorf("%w: leitura interrompida: %w", ErrNFSeADNComunicacaoFalhou, cerr)
		}
		if ehErroRedeTransitorioADN(err) {
			return NFSeADNLote{}, &NFSeADNErroTransitorio{Causa: err}
		}
		return NFSeADNLote{}, fmt.Errorf("%w: falha ao ler corpo da resposta: %v", ErrNFSeADNComunicacaoFalhou, err)
	}
	if int64(len(body)) > nfseADNMaxBodyBytes {
		return NFSeADNLote{}, fmt.Errorf("%w: corpo da resposta (HTTP %d) excede o limite de %d bytes", ErrNFSeADNFormatoInesperado, resp.StatusCode, nfseADNMaxBodyBytes)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return NFSeADNLote{}, fmt.Errorf("%w (HTTP %d): %s", ErrNFSeADNAutenticacaoFalhou, resp.StatusCode, TruncateRunes(string(body), 300))
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return NFSeADNLote{}, &NFSeADNErroTransitorio{
			StatusHTTP: resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
	}

	lote, perr := parseNFSeADNLote(body)

	switch resp.StatusCode {
	case http.StatusOK:
		if perr != nil {
			return NFSeADNLote{}, fmt.Errorf("%w: %v", ErrNFSeADNFormatoInesperado, perr)
		}
		switch string(lote.StatusProcessamento) {
		case NFSeADNStatusDocumentosLocalizados, NFSeADNStatusNenhumDocumento:
			return lote, nil
		case NFSeADNStatusRejeicao:
			return NFSeADNLote{}, rejeicaoADN(resp.StatusCode, nsu, lote)
		default:
			return NFSeADNLote{}, fmt.Errorf("%w: StatusProcessamento desconhecido %q", ErrNFSeADNFormatoInesperado, lote.StatusProcessamento)
		}
	case http.StatusNotFound:
		// Fim da fila SÓ com o corpo documentado — um 404 sem esse corpo (rota errada, proxy) é
		// falha de comunicação, nunca "nada a buscar" silencioso.
		if perr == nil && lote.FimDaFila() {
			return lote, nil
		}
		return NFSeADNLote{}, fmt.Errorf("%w (HTTP 404 sem NENHUM_DOCUMENTO_LOCALIZADO): %s", ErrNFSeADNComunicacaoFalhou, TruncateRunes(string(body), 300))
	case http.StatusBadRequest:
		if perr == nil {
			return NFSeADNLote{}, rejeicaoADN(resp.StatusCode, nsu, lote)
		}
		return NFSeADNLote{}, fmt.Errorf("%w (HTTP 400): %s", ErrNFSeADNRejeicao, TruncateRunes(string(body), 300))
	}
	return NFSeADNLote{}, fmt.Errorf("%w (HTTP %d): %s", ErrNFSeADNComunicacaoFalhou, resp.StatusCode, TruncateRunes(string(body), 300))
}

// rejeicaoADN loga e devolve o erro de uma REJEICAO, com os códigos de Erros/Alertas (%q).
func rejeicaoADN(status int, nsu int64, lote NFSeADNLote) error {
	erros := codigosMensagensADN(lote.Erros)
	log.Printf("[NFSeADN] REJEICAO HTTP %d NSU=%d ambiente=%q erros=%q alertas=%q",
		status, nsu, lote.TipoAmbiente, erros, codigosMensagensADN(lote.Alertas))
	return fmt.Errorf("%w (HTTP %d, NSU %d): erros=%q", ErrNFSeADNRejeicao, status, nsu, erros)
}

// buscarLoteComRetry chama BuscarLoteDFeADN repetindo diante de 429/5xx: espera o Retry-After
// (se houver) ou backoff exponencial a partir de cfg.BackoffBase, ambos limitados a
// cfg.EsperaMax, até cfg.MaxTentativas tentativas. Esgotadas, devolve o último
// *NFSeADNErroTransitorio. Toda espera respeita ctx.
func buscarLoteComRetry(ctx context.Context, httpClient *http.Client, baseURL string, nsu int64, cfg nfseADNFilaConfig) (NFSeADNLote, error) {
	tentativas := cfg.MaxTentativas
	if tentativas < 1 {
		tentativas = 1
	}
	var ultimoErr error
	for i := 1; i <= tentativas; i++ {
		lote, err := BuscarLoteDFeADN(ctx, httpClient, baseURL, nsu)
		var trans *NFSeADNErroTransitorio
		if err == nil || !errors.As(err, &trans) {
			return lote, err
		}
		ultimoErr = err
		if i == tentativas {
			break
		}
		espera := trans.RetryAfter
		if espera <= 0 {
			espera = cfg.BackoffBase << (i - 1)
		}
		if cfg.EsperaMax > 0 && espera > cfg.EsperaMax {
			espera = cfg.EsperaMax
		}
		log.Printf("[NFSeADN] %s no NSU=%d (tentativa %d/%d) — nova tentativa em %s: %v", trans.descricao(), nsu, i, tentativas, espera, err)
		if serr := cfg.Sleep(ctx, espera); serr != nil {
			return NFSeADNLote{}, fmt.Errorf("%w: espera interrompida: %w", ErrNFSeADNComunicacaoFalhou, serr)
		}
	}
	return NFSeADNLote{}, ultimoErr
}

// Motivos de encerramento de PercorrerFilaADN (NFSeADNResultadoFila.Encerramento).
const (
	NFSeADNEncerramentoFimDaFila       = "fim_da_fila"
	NFSeADNEncerramentoLimiteLotes     = "limite_lotes"
	NFSeADNEncerramentoLimiteTempo     = "limite_tempo"
	NFSeADNEncerramentoTransitorio     = "transitorio_esgotado"
	NFSeADNEncerramentoCancelado       = "cancelado"
	NFSeADNEncerramentoCursorSemAvanco = "cursor_sem_avanco"
)

// NFSeADNResultadoFila resume um percurso da fila. Lotes/Documentos contam só lotes já
// PROCESSADOS com sucesso (processar devolveu nil); UltimoNSU é o cursor após o último deles.
type NFSeADNResultadoFila struct {
	Lotes        int
	Documentos   int
	UltimoNSU    int64
	Encerramento string
}

// Parcial indica que a fila NÃO foi esgotada (limite/transitório/cancelamento) — o próximo
// ciclo continua do UltimoNSU salvo.
func (r NFSeADNResultadoFila) Parcial() bool {
	return r.Encerramento != NFSeADNEncerramentoFimDaFila
}

// PercorrerFilaADN percorre a fila de distribuição a partir de nsuInicial (exclusivo — o ADN
// devolve os documentos com NSU maior que o informado), chamando processar(lote, maiorNSU) a
// cada lote com documentos; o próximo lote começa no maior NSU do anterior. processar é quem
// grava o lote e o cursor numa transação: se devolver erro, o percurso para e devolve esse erro
// (lotes anteriores continuam gravados).
//
// Encerra SEM erro (sucesso, possivelmente parcial — ver Encerramento) em: fim da fila (404
// NENHUM_DOCUMENTO_LOCALIZADO), cfg.MaxLotes atingido, cfg.MaxDuracao atingida, 429/5xx com
// tentativas esgotadas, ctx cancelado, ou lote cujo maior NSU não avança o cursor (proteção
// contra loop). Encerra COM erro em rejeição (400), autenticação, formato inesperado e demais
// falhas de comunicação.
func PercorrerFilaADN(ctx context.Context, httpClient *http.Client, baseURL string, nsuInicial int64, cfg nfseADNFilaConfig,
	processar func(lote NFSeADNLote, maiorNSU int64) error) (NFSeADNResultadoFila, error) {

	if cfg.Sleep == nil {
		cfg.Sleep = nfseSleepCtx
	}
	parentCtx := ctx
	if cfg.MaxDuracao > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.MaxDuracao)
		defer cancel()
	}
	encerramentoPorCtx := func() string {
		if parentCtx.Err() != nil {
			return NFSeADNEncerramentoCancelado
		}
		return NFSeADNEncerramentoLimiteTempo
	}

	res := NFSeADNResultadoFila{UltimoNSU: nsuInicial}
	nsu := nsuInicial
	for {
		if cfg.MaxLotes > 0 && res.Lotes >= cfg.MaxLotes {
			res.Encerramento = NFSeADNEncerramentoLimiteLotes
			return res, nil
		}
		if ctx.Err() != nil {
			res.Encerramento = encerramentoPorCtx()
			return res, nil
		}
		if res.Lotes > 0 && cfg.PausaEntreLotes > 0 {
			if err := cfg.Sleep(ctx, cfg.PausaEntreLotes); err != nil {
				res.Encerramento = encerramentoPorCtx()
				return res, nil
			}
		}

		lote, err := buscarLoteComRetry(ctx, httpClient, baseURL, nsu, cfg)
		if err != nil {
			if errors.Is(err, ErrNFSeADNTransitorio) {
				log.Printf("[NFSeADN] tentativas esgotadas no NSU=%d (%v) — ciclo encerrado com progresso salvo", nsu, err)
				res.Encerramento = NFSeADNEncerramentoTransitorio
				return res, nil
			}
			if ctx.Err() != nil {
				res.Encerramento = encerramentoPorCtx()
				return res, nil
			}
			return res, err
		}

		menor, maior := lote.FaixaNSU()
		log.Printf("[NFSeADN] lote NSU>%d status=%q ambiente=%q versao=%q documentos=%d faixaNSU=%d..%d alertas=%q erros=%q",
			nsu, lote.StatusProcessamento, lote.TipoAmbiente, lote.VersaoAplicativo, len(lote.LoteDFe), menor, maior,
			codigosMensagensADN(lote.Alertas), codigosMensagensADN(lote.Erros))

		if lote.FimDaFila() {
			res.Encerramento = NFSeADNEncerramentoFimDaFila
			return res, nil
		}
		if len(lote.LoteDFe) == 0 || maior <= nsu {
			log.Printf("[NFSeADN] lote sem NSU maior que %d (documentos=%d, maior=%d) — encerrando para não repetir a mesma consulta", nsu, len(lote.LoteDFe), maior)
			res.Encerramento = NFSeADNEncerramentoCursorSemAvanco
			return res, nil
		}

		if err := processar(lote, maior); err != nil {
			return res, err
		}
		res.Lotes++
		res.Documentos += len(lote.LoteDFe)
		res.UltimoNSU = maior
		nsu = maior
	}
}
