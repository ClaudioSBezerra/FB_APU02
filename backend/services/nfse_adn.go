// Package services — cliente mTLS do Portal Nacional ADN (NFS-e).
//
// O contrato real da API do ADN NÃO está totalmente confirmado por fonte primária
// (pesquisa de 2026-10-23 — ver spec-nfse-cliente-adn.md, bloco Boundaries). O endpoint em
// si é confirmado (manual oficial "APIs ADN Sistema Nacional NFS-e" v1.0, gov.br/nfse):
// GET /contribuintes/DFe/{NSU} — mas autenticação (mTLS puro vs. mTLS + token adicional),
// paginação (nome do parâmetro de NSU/CNPJ, tamanho de lote) e formato de resposta
// (JSON/XML, schema do payload) são suposições. Este arquivo é o PONTO ÚNICO de ajuste:
// quando um certificado de teste real permitir inspecionar o Swagger de Produção Restrita
// (https://adn.producaorestrita.nfse.gov.br/contribuintes/docs/index.html), as correções
// entram aqui — não espalhar suposições de formato para handlers/persistência.
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
	"net/http"
	"net/url"
	"os"
	"strings"
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
const nfseADNMaxBodyBytes = 20 << 20 // 20 MiB

// nfseADNMaxLotes limita o número de lotes buscados num único ciclo manual — protege contra
// loop infinito caso o ADN nunca devolva loteVazio=true (ex.: por divergência do formato real
// de paginação, suposição documentada abaixo). Alto o bastante para não truncar capturas
// normais de um ciclo manual.
const nfseADNMaxLotes = 1000

// Erros sentinela — permitem ao handler escolher a mensagem HTTP certa (ambos os casos abaixo
// mapeiam para 502, mas com mensagens diferentes, ver I/O Matrix da spec) sem acoplar o handler
// aos detalhes de formato/transporte tratados aqui.
var (
	// ErrNFSeADNAutenticacaoFalhou cobre tanto falha de handshake mTLS (certificado
	// rejeitado antes de qualquer resposta HTTP) quanto HTTP 401/403 do ADN.
	ErrNFSeADNAutenticacaoFalhou = errors.New("autenticação mTLS recusada pelo Portal Nacional ADN")
	// ErrNFSeADNFormatoInesperado cobre corpo de resposta que o parser (suposição de
	// formato, ver parseNFSeADNPayload) não reconhece.
	ErrNFSeADNFormatoInesperado = errors.New("formato de resposta inesperado do Portal Nacional ADN")
	// ErrNFSeADNComunicacaoFalhou cobre qualquer outra falha de comunicação (HTTP
	// diferente de 200/401/403) que não seja nem falha de autenticação nem de formato.
	ErrNFSeADNComunicacaoFalhou = errors.New("falha ao comunicar com o Portal Nacional ADN")
)

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

	privateKey, x509Cert, err := pkcs12.Decode(pfxBytes, cert.Senha)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao decodificar .pfx (senha incorreta ou arquivo inválido): %w", err)
	}

	tlsCert := tls.Certificate{
		Certificate: [][]byte{x509Cert.Raw},
		PrivateKey:  privateKey,
		Leaf:        x509Cert,
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
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

// NFSeADNDocumento representa UM documento NFS-e devolvido pelo Portal Nacional ADN.
//
// SUPOSIÇÃO DE FORMATO (sem exemplo de payload real disponível — ver Boundaries da spec):
// JSON com os metadados do documento e o XML da NFS-e assinada embutido em base64, por
// analogia ao DF-e da NF-e. Os campos tributários (ISS/PIS/COFINS/...) de nfse_entradas e
// nfse_saidas NÃO são preenchidos a partir daqui nesta sub-spec — extraí-los exigiria parsear
// o XML embutido assinado, cujo schema real também não está confirmado; ficam no valor padrão
// 0 da migration 133 até uma sub-spec futura com acesso a um payload real para ajustar este
// parser. PRECISA DE AJUSTE quando houver confirmação do contrato real.
type NFSeADNDocumento struct {
	NSU               string  `json:"nsu"`
	IdentificadorNFSe string  `json:"identificadorNfse"`
	CNPJPrestador     string  `json:"cnpjPrestador"`
	CNPJTomador       string  `json:"cnpjTomador"`
	DataEmissao       string  `json:"dataEmissao"` // suposição: "AAAA-MM-DD"
	ValorServicos     float64 `json:"valorServicos"`
	XMLBase64         string  `json:"xml"` // XML da NFS-e assinada embutido em base64 — suposição
}

// NFSeADNResponse é a resposta assumida de GET /contribuintes/DFe/{NSU}.
//
// SUPOSIÇÃO DE PAGINAÇÃO (não confirmada — ver Boundaries da spec): implementada por analogia
// ao padrão já usado no DF-e da NF-e — "loteVazio" indica que não há mais documentos a buscar;
// "ultimoNSU" é o maior NSU retornado no lote, usado como NSU do próximo lote (ver
// BuscarTodosDocumentosADN). PRECISA DE AJUSTE quando houver confirmação do contrato real.
type NFSeADNResponse struct {
	LoteVazio  bool               `json:"loteVazio"`
	UltimoNSU  string             `json:"ultimoNSU"`
	Documentos []NFSeADNDocumento `json:"documentos"`
}

// parseNFSeADNPayload decodifica o corpo da resposta do ADN. ÚNICA função que conhece o
// formato assumido do payload (ver NFSeADNResponse/NFSeADNDocumento acima) — isolada aqui
// para ser o ponto único de ajuste quando o formato real for confirmado. Qualquer corpo que
// não decodifique como o JSON assumido, ou que contenha um documento sem identificador, é
// tratado como formato inesperado (nunca grava "lixo").
func parseNFSeADNPayload(body []byte) (NFSeADNResponse, error) {
	var resp NFSeADNResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return NFSeADNResponse{}, fmt.Errorf("corpo não é o JSON esperado: %w", err)
	}
	for i, doc := range resp.Documentos {
		if strings.TrimSpace(doc.IdentificadorNFSe) == "" {
			return NFSeADNResponse{}, fmt.Errorf("documento #%d do lote sem identificadorNfse", i)
		}
	}
	return resp, nil
}

// BuscarDFeADN chama GET {baseURL}/contribuintes/DFe/{nsu} usando o client mTLS fornecido.
// Endpoint confirmado por fonte oficial; autenticação além do mTLS, parâmetros de paginação e
// formato de resposta exatos NÃO são confirmados — ver comentários de NFSeADNResponse e
// parseNFSeADNPayload acima, os pontos únicos de ajuste quando o contrato real for conhecido.
func BuscarDFeADN(ctx context.Context, httpClient *http.Client, baseURL, nsu string) (NFSeADNResponse, error) {
	if strings.TrimSpace(nsu) == "" {
		nsu = "0"
	}
	endpoint := fmt.Sprintf("%s/contribuintes/DFe/%s", strings.TrimRight(baseURL, "/"), url.PathEscape(nsu))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return NFSeADNResponse{}, fmt.Errorf("falha ao montar requisição ao Portal Nacional ADN: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		// Sem resposta HTTP nenhuma: handshake mTLS falhou (certificado rejeitado pelo
		// servidor) ou falha de rede. Tratado como falha de autenticação — é o cenário mais
		// provável contra um servidor que exige certificado de cliente.
		return NFSeADNResponse{}, fmt.Errorf("%w: %v", ErrNFSeADNAutenticacaoFalhou, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, nfseADNMaxBodyBytes))
	if err != nil {
		return NFSeADNResponse{}, fmt.Errorf("%w: falha ao ler corpo da resposta: %v", ErrNFSeADNComunicacaoFalhou, err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return NFSeADNResponse{}, fmt.Errorf("%w (HTTP %d): %s", ErrNFSeADNAutenticacaoFalhou, resp.StatusCode, TruncateRunes(string(body), 300))
	}
	if resp.StatusCode != http.StatusOK {
		return NFSeADNResponse{}, fmt.Errorf("%w (HTTP %d): %s", ErrNFSeADNComunicacaoFalhou, resp.StatusCode, TruncateRunes(string(body), 300))
	}

	parsed, perr := parseNFSeADNPayload(body)
	if perr != nil {
		return NFSeADNResponse{}, fmt.Errorf("%w: %v", ErrNFSeADNFormatoInesperado, perr)
	}
	return parsed, nil
}

// BuscarTodosDocumentosADN executa UM ciclo de busca manual ("Buscar Agora"): chama
// BuscarDFeADN repetidamente (NSU inicial "0", depois o ultimoNSU de cada resposta) até
// loteVazio=true, agregando todos os documentos retornados. Não há persistência de "último
// NSU processado" entre ciclos nesta sub-spec — não existe coluna dedicada no schema
// (migration 133 está congelada) e o escopo aqui é só o ciclo manual, sem agendamento
// automático (próxima sub-spec). Duplicação entre ciclos é prevenida na PERSISTÊNCIA
// (UNIQUE(company_id, identificador_nfse)), não aqui.
func BuscarTodosDocumentosADN(ctx context.Context, httpClient *http.Client, baseURL string) ([]NFSeADNDocumento, error) {
	// Timeout geral do ciclo inteiro de paginação — protege contra um ADN que responde
	// devagar por muitas páginas em sequência, deixando o ciclo manual rodando por tempo
	// desproporcional (cada chamada individual já tem seu próprio timeout menor, ver
	// nfseADNHTTPTimeout, mas isso não limita a soma de várias chamadas em sequência).
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var todos []NFSeADNDocumento
	nsu := "0"
	for i := 0; i < nfseADNMaxLotes; i++ {
		resp, err := BuscarDFeADN(ctx, httpClient, baseURL, nsu)
		if err != nil {
			return todos, err
		}
		todos = append(todos, resp.Documentos...)
		if resp.LoteVazio || resp.UltimoNSU == "" || resp.UltimoNSU == nsu {
			return todos, nil
		}
		nsu = resp.UltimoNSU
	}
	// Limite de lotes atingido sem o ADN jamais sinalizar loteVazio=true — devolver os
	// documentos já coletados como se fosse uma captura completa esconderia uma captura
	// incompleta (ver nfseADNMaxLotes acima).
	return todos, fmt.Errorf("limite de %d lotes atingido sem loteVazio=true — captura pode estar incompleta", nfseADNMaxLotes)
}
