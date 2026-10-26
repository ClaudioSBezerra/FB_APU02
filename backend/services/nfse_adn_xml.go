// Package services — decodificação do ArquivoXml do Portal Nacional ADN (gzip + base64) e
// extração dos campos fiscais da NFS-e (leiaute nacional, namespace
// http://www.sped.fazenda.gov.br/nfse, raiz NFSe/infNFSe).
package services

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// nfseADNMaxXMLBytes limita o XML DESCOMPRIMIDO de um documento (anti zip-bomb).
const nfseADNMaxXMLBytes = 5 << 20 // 5 MiB

// ErrNFSeXMLGrandeDemais: XML descomprimido acima de nfseADNMaxXMLBytes.
var ErrNFSeXMLGrandeDemais = errors.New("XML da NFS-e excede o limite de 5 MiB descomprimido")

// decodificarArquivoXmlADN decodifica o ArquivoXml (base64 de gzip) devolvendo o XML, com
// limite de nfseADNMaxXMLBytes descomprimidos — passou disso, ErrNFSeXMLGrandeDemais (a
// leitura para no limite + 1 byte, nunca descomprime o resto).
func decodificarArquivoXmlADN(arquivoXml string) ([]byte, error) {
	limpo := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return r
	}, arquivoXml)
	if limpo == "" {
		return nil, errors.New("ArquivoXml vazio")
	}
	gz, err := decodificarBase64Tolerante(limpo)
	if err != nil {
		return nil, fmt.Errorf("ArquivoXml não é base64 válido: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("ArquivoXml não é gzip válido: %w", err)
	}
	defer zr.Close()
	xmlBytes, err := io.ReadAll(io.LimitReader(zr, nfseADNMaxXMLBytes+1))
	if err != nil {
		return nil, fmt.Errorf("falha ao descomprimir ArquivoXml: %w", err)
	}
	if len(xmlBytes) > nfseADNMaxXMLBytes {
		return nil, ErrNFSeXMLGrandeDemais
	}
	return xmlBytes, nil
}

// decodificarBase64Tolerante tenta, em ordem, base64 padrão (forma documentada), padrão sem
// padding, URL-safe e URL-safe sem padding — o mesmo conteúdo já chegou de integradores
// diferentes nas quatro formas. O espaço em branco já foi removido pelo chamador. Devolve o erro
// da forma padrão quando nenhuma serve.
func decodificarBase64Tolerante(s string) ([]byte, error) {
	var primeiroErr error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		b, err := enc.DecodeString(s)
		if err == nil {
			return b, nil
		}
		if primeiroErr == nil {
			primeiroErr = err
		}
	}
	return nil, primeiroErr
}

// nfseXMLDoc mapeia só os caminhos usados. Tags sem namespace: o encoding/xml casa pelo nome
// local, qualquer que seja o namespace (o leiaute usa o namespace default
// http://www.sped.fazenda.gov.br/nfse em todos os elementos).
type nfseXMLDoc struct {
	XMLName xml.Name `xml:"NFSe"`
	Inf     struct {
		ID    string `xml:"Id,attr"`
		NNFSe string `xml:"nNFSe"`
		Emit  struct {
			CNPJ string `xml:"CNPJ"`
			CPF  string `xml:"CPF"`
		} `xml:"emit"`
		Valores struct {
			VBC        string `xml:"vBC"`
			PAliqAplic string `xml:"pAliqAplic"`
			VISSQN     string `xml:"vISSQN"`
		} `xml:"valores"`
		IBSCBS *nfseXMLIBSCBSNota `xml:"IBSCBS"`
		DPS    struct {
			Inf struct {
				DhEmi   string `xml:"dhEmi"`
				DCompet string `xml:"dCompet"`
				Prest   struct {
					CNPJ string `xml:"CNPJ"`
					CPF  string `xml:"CPF"`
				} `xml:"prest"`
				Toma struct {
					CNPJ string `xml:"CNPJ"`
					CPF  string `xml:"CPF"`
				} `xml:"toma"`
				Valores struct {
					VServPrest struct {
						VServ string `xml:"vServ"`
					} `xml:"vServPrest"`
					Trib struct {
						TribMun struct {
							TpRetISSQN string `xml:"tpRetISSQN"`
						} `xml:"tribMun"`
						TribFed struct {
							PisCofins struct {
								VPis    string `xml:"vPis"`
								VCofins string `xml:"vCofins"`
							} `xml:"piscofins"`
							VRetCP   string `xml:"vRetCP"`
							VRetIRRF string `xml:"vRetIRRF"`
							VRetCSLL string `xml:"vRetCSLL"`
						} `xml:"tribFed"`
					} `xml:"trib"`
				} `xml:"valores"`
				IBSCBS *nfseXMLIBSCBSDPS `xml:"IBSCBS"`
			} `xml:"infDPS"`
		} `xml:"DPS"`
	} `xml:"infNFSe"`
}

// nfseXMLIBSCBSNota é o grupo infNFSe/IBSCBS (leiaute 1.01): valores de IBS/CBS calculados pelo
// emissor/ADN. Ponteiro no nfseXMLDoc: nil = grupo ausente (nota anterior à reforma).
type nfseXMLIBSCBSNota struct {
	CLocalidadeIncid string `xml:"cLocalidadeIncid"`
	Valores          struct {
		VBC string `xml:"vBC"`
		UF  struct {
			PIBSUF      string `xml:"pIBSUF"`
			PAliqEfetUF string `xml:"pAliqEfetUF"`
		} `xml:"uf"`
		Mun struct {
			PIBSMun      string `xml:"pIBSMun"`
			PAliqEfetMun string `xml:"pAliqEfetMun"`
		} `xml:"mun"`
		Fed struct {
			PCBS         string `xml:"pCBS"`
			PAliqEfetCBS string `xml:"pAliqEfetCBS"`
		} `xml:"fed"`
	} `xml:"valores"`
	TotCIBS struct {
		VTotNF string `xml:"vTotNF"`
		GIBS   struct {
			VIBSTot   string `xml:"vIBSTot"`
			GIBSUFTot struct {
				VIBSUF string `xml:"vIBSUF"`
			} `xml:"gIBSUFTot"`
			GIBSMunTot struct {
				VIBSMun string `xml:"vIBSMun"`
			} `xml:"gIBSMunTot"`
		} `xml:"gIBS"`
		GCBS struct {
			VCBS string `xml:"vCBS"`
		} `xml:"gCBS"`
	} `xml:"totCIBS"`
}

// nfseXMLIBSCBSDPS é o grupo infNFSe/DPS/infDPS/IBSCBS: classificação informada na DPS.
type nfseXMLIBSCBSDPS struct {
	FinNFSe string `xml:"finNFSe"`
	CIndOp  string `xml:"cIndOp"`
	IndDest string `xml:"indDest"`
	Valores struct {
		Trib struct {
			GIBSCBS struct {
				CST        string `xml:"CST"`
				CClassTrib string `xml:"cClassTrib"`
			} `xml:"gIBSCBS"`
		} `xml:"trib"`
	} `xml:"valores"`
}

// NFSeXMLParserVersao é a versão do parser gravada em xml_parser_versao (migration 135): linhas
// com versão menor são reextraídas de xml_gzip_b64 no boot (ReprocessarXMLNFSe). Versão 2 = IBS/CBS
// e base/alíquota do ISS (spec-nfse-ibscbs-xml.md).
const NFSeXMLParserVersao = 2

// NFSeIBSCBSDados são os campos de IBS/CBS da NFS-e. nil = elemento (ou grupo IBSCBS inteiro)
// ausente no XML, ou valor presente mas não interpretável — gravado como NULL, distinto de 0.
type NFSeIBSCBSDados struct {
	CodLocalidadeIncid    *string  // infNFSe/IBSCBS/cLocalidadeIncid
	Base                  *float64 // IBSCBS/valores/vBC
	IBSUFAliquota         *float64 // valores/uf/pIBSUF
	IBSUFAliquotaEfetiva  *float64 // valores/uf/pAliqEfetUF
	IBSMunAliquota        *float64 // valores/mun/pIBSMun
	IBSMunAliquotaEfetiva *float64 // valores/mun/pAliqEfetMun
	CBSAliquota           *float64 // valores/fed/pCBS
	CBSAliquotaEfetiva    *float64 // valores/fed/pAliqEfetCBS
	ValorTotalNF          *float64 // totCIBS/vTotNF
	IBSTotal              *float64 // totCIBS/gIBS/vIBSTot
	IBSUF                 *float64 // gIBS/gIBSUFTot/vIBSUF
	IBSMun                *float64 // gIBS/gIBSMunTot/vIBSMun
	CBS                   *float64 // totCIBS/gCBS/vCBS
	FinNFSe               *string  // infDPS/IBSCBS/finNFSe
	IndOp                 *string  // infDPS/IBSCBS/cIndOp
	IndDest               *string  // infDPS/IBSCBS/indDest
	CST                   *string  // infDPS/IBSCBS/valores/trib/gIBSCBS/CST
	CClassTrib            *string  // infDPS/IBSCBS/valores/trib/gIBSCBS/cClassTrib
}

// NFSeDadosXML são os campos fiscais extraídos de uma NFS-e. Campo ausente = "" / 0 (exceto
// IBSCBS, em que ausente = nil).
type NFSeDadosXML struct {
	IDInfNFSe     string // atributo Id de infNFSe ("NFS" + chave de acesso)
	NumeroNFSe    string // infNFSe/nNFSe
	CNPJEmitente  string // infNFSe/emit/CNPJ (normalizado)
	CPFEmitente   string
	CNPJPrestador string // infNFSe/DPS/infDPS/prest/CNPJ (normalizado)
	CPFPrestador  string
	CNPJTomador   string // infNFSe/DPS/infDPS/toma/CNPJ (normalizado)
	CPFTomador    string
	DataEmissao   string // AAAA-MM-DD (data de dhEmi, no fuso informado no próprio XML)
	MesAno        string // MM/YYYY de dCompet (competência); sem dCompet, o mês de dhEmi
	ValorServicos float64
	ISS           float64
	ISSRetido     float64 // = ISS quando tpRetISSQN ∈ {2,3} (retido pelo tomador/intermediário)
	PIS           float64
	COFINS        float64
	IRRF          float64
	CSLL          float64
	INSS          float64 // tribFed/vRetCP
	BaseISS       float64 // infNFSe/valores/vBC
	AliquotaISS   float64 // infNFSe/valores/pAliqAplic
	IBSCBS        NFSeIBSCBSDados
}

// extrairDadosNFSeXML interpreta o XML de uma NFS-e. Erro só quando o XML é malformado ou a raiz
// não é NFSe; campo ausente ou numérico inválido vira 0 (ver extrairDadosNFSeXMLDetalhado para
// saber QUAIS campos numéricos vieram inválidos).
func extrairDadosNFSeXML(xmlBytes []byte) (NFSeDadosXML, error) {
	d, _, err := extrairDadosNFSeXMLDetalhado(xmlBytes)
	return d, err
}

// extrairDadosNFSeXMLDetalhado é extrairDadosNFSeXML devolvendo também os nomes dos campos
// decimais PRESENTES mas não interpretáveis (gravados como 0; nos campos IBS/CBS, como NULL) — o
// processor loga com o NSU e conta, em vez de zerar valores fiscais em silêncio.
func extrairDadosNFSeXMLDetalhado(xmlBytes []byte) (NFSeDadosXML, []string, error) {
	var doc nfseXMLDoc
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	dec.CharsetReader = nfseCharsetReader
	if err := dec.Decode(&doc); err != nil {
		return NFSeDadosXML{}, nil, fmt.Errorf("XML da NFS-e inválido: %w", err)
	}
	inf := doc.Inf
	dps := inf.DPS.Inf

	var invalidos []string
	dec64 := func(campo, v string) float64 {
		f, ok := decimalNFSeOK(v)
		if !ok {
			invalidos = append(invalidos, campo)
		}
		return f
	}

	d := NFSeDadosXML{
		IDInfNFSe:     strings.TrimSpace(inf.ID),
		NumeroNFSe:    strings.TrimSpace(inf.NNFSe),
		CNPJEmitente:  normalizarCNPJNFSe(inf.Emit.CNPJ),
		CPFEmitente:   OnlyDigits(inf.Emit.CPF),
		CNPJPrestador: normalizarCNPJNFSe(dps.Prest.CNPJ),
		CPFPrestador:  OnlyDigits(dps.Prest.CPF),
		CNPJTomador:   normalizarCNPJNFSe(dps.Toma.CNPJ),
		CPFTomador:    OnlyDigits(dps.Toma.CPF),
		ValorServicos: dec64("vServ", dps.Valores.VServPrest.VServ),
		ISS:           dec64("vISSQN", inf.Valores.VISSQN),
		PIS:           dec64("vPis", dps.Valores.Trib.TribFed.PisCofins.VPis),
		COFINS:        dec64("vCofins", dps.Valores.Trib.TribFed.PisCofins.VCofins),
		IRRF:          dec64("vRetIRRF", dps.Valores.Trib.TribFed.VRetIRRF),
		CSLL:          dec64("vRetCSLL", dps.Valores.Trib.TribFed.VRetCSLL),
		INSS:          dec64("vRetCP", dps.Valores.Trib.TribFed.VRetCP),
		BaseISS:       dec64("vBC", inf.Valores.VBC),
		AliquotaISS:   dec64("pAliqAplic", inf.Valores.PAliqAplic),
	}

	// IBS/CBS: elemento ausente ou vazio = nil (NULL); presente mas inválido (ou fora da precisão
	// da coluna, que faria o INSERT falhar) = nil + campo em invalidos.
	decPtr := func(campo, v string, limite float64) *float64 {
		if strings.TrimSpace(v) == "" {
			return nil
		}
		f, ok := decimalNFSeOK(v)
		if !ok || math.Abs(f) >= limite {
			invalidos = append(invalidos, campo)
			return nil
		}
		return &f
	}
	valor := func(campo, v string) *float64 { return decPtr(campo, v, nfseLimiteValor) }
	aliq := func(campo, v string) *float64 { return decPtr(campo, v, nfseLimiteAliquota) }
	if g := inf.IBSCBS; g != nil {
		v, tot := &g.Valores, &g.TotCIBS
		d.IBSCBS.CodLocalidadeIncid = textoNFSePtr(g.CLocalidadeIncid)
		d.IBSCBS.Base = valor("IBSCBS/vBC", v.VBC)
		d.IBSCBS.IBSUFAliquota = aliq("pIBSUF", v.UF.PIBSUF)
		d.IBSCBS.IBSUFAliquotaEfetiva = aliq("pAliqEfetUF", v.UF.PAliqEfetUF)
		d.IBSCBS.IBSMunAliquota = aliq("pIBSMun", v.Mun.PIBSMun)
		d.IBSCBS.IBSMunAliquotaEfetiva = aliq("pAliqEfetMun", v.Mun.PAliqEfetMun)
		d.IBSCBS.CBSAliquota = aliq("pCBS", v.Fed.PCBS)
		d.IBSCBS.CBSAliquotaEfetiva = aliq("pAliqEfetCBS", v.Fed.PAliqEfetCBS)
		d.IBSCBS.ValorTotalNF = valor("vTotNF", tot.VTotNF)
		d.IBSCBS.IBSTotal = valor("vIBSTot", tot.GIBS.VIBSTot)
		d.IBSCBS.IBSUF = valor("vIBSUF", tot.GIBS.GIBSUFTot.VIBSUF)
		d.IBSCBS.IBSMun = valor("vIBSMun", tot.GIBS.GIBSMunTot.VIBSMun)
		d.IBSCBS.CBS = valor("vCBS", tot.GCBS.VCBS)
	}
	if g := dps.IBSCBS; g != nil {
		d.IBSCBS.FinNFSe = textoNFSePtr(g.FinNFSe)
		d.IBSCBS.IndOp = textoNFSePtr(g.CIndOp)
		d.IBSCBS.IndDest = textoNFSePtr(g.IndDest)
		d.IBSCBS.CST = textoNFSePtr(g.Valores.Trib.GIBSCBS.CST)
		d.IBSCBS.CClassTrib = textoNFSePtr(g.Valores.Trib.GIBSCBS.CClassTrib)
	}
	switch strings.TrimSpace(dps.Valores.Trib.TribMun.TpRetISSQN) {
	case "2", "3":
		d.ISSRetido = d.ISS
	}

	var emissao time.Time
	if t, ok := parseDataHoraNFSe(dps.DhEmi); ok {
		emissao = t
		d.DataEmissao = t.Format("2006-01-02")
	}
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(dps.DCompet)); err == nil {
		d.MesAno = t.Format("01/2006")
	} else if !emissao.IsZero() {
		d.MesAno = emissao.Format("01/2006")
	}
	return d, invalidos, nil
}

// Limites de magnitude dos campos IBS/CBS (migration 135): valores NUMERIC(15,2) e alíquotas
// NUMERIC(7,4). Acima disso o Postgres recusaria o INSERT (e o documento iria para a quarentena por
// um único campo) — o valor é tratado como inválido (NULL + log).
const (
	nfseLimiteValor    = 1e13
	nfseLimiteAliquota = 1e3
)

// nfseMaxCodigoIBSCBS é o tamanho das colunas VARCHAR de códigos IBS/CBS (migration 135).
const nfseMaxCodigoIBSCBS = 20

// textoNFSePtr devolve nil para texto vazio; senão o texto aparado (truncado ao tamanho da coluna).
func textoNFSePtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	v = TruncateRunes(v, nfseMaxCodigoIBSCBS)
	return &v
}

// ─── Charset ────────────────────────────────────────────────────────────────

// nfseCharsetReader é o xml.Decoder.CharsetReader: o leiaute exige UTF-8 (tratado pelo próprio
// encoding/xml, que só chama esta função para OUTROS encodings), mas emissores municipais
// legados declaram ISO-8859-1/windows-1252. golang.org/x/text/encoding/charmap não está vendorado
// (e o projeto não ganha módulos novos por isso), então a conversão é feita aqui: ISO-8859-1 é
// identidade byte→rune; windows-1252 difere só em 0x80–0x9F (tabela abaixo).
func nfseCharsetReader(label string, input io.Reader) (io.Reader, error) {
	var tabela *[32]rune
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "iso-8859-1", "iso8859-1", "iso_8859-1", "latin1", "latin-1", "l1", "iso-ir-100", "csisolatin1":
		tabela = nil
	case "windows-1252", "cp1252", "x-cp1252":
		tabela = &nfseCP1252Altos
	case "us-ascii", "ascii":
		return input, nil
	default:
		return nil, fmt.Errorf("encoding %q não suportado no XML da NFS-e", label)
	}
	// O XML já está inteiro em memória e limitado a nfseADNMaxXMLBytes — converter de uma vez.
	raw, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(raw)+len(raw)/8)
	for _, b := range raw {
		switch {
		case b < 0x80:
			out = append(out, b)
		case b < 0xA0 && tabela != nil:
			out = utf8.AppendRune(out, tabela[b-0x80])
		default:
			out = utf8.AppendRune(out, rune(b))
		}
	}
	return bytes.NewReader(out), nil
}

// nfseCP1252Altos mapeia 0x80–0x9F do windows-1252 (posições indefinidas viram o próprio code
// point C1, como no ISO-8859-1).
var nfseCP1252Altos = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// ─── Data/hora e decimais ───────────────────────────────────────────────────

// nfseLocBrasilia é America/Sao_Paulo (fallback -03:00 fixo se o tzdata faltar), usado quando a
// data/hora vem SEM fuso.
func nfseLocBrasilia() *time.Location {
	if loc, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		return loc
	}
	return time.FixedZone("BRT", -3*3600)
}

// parseDataHoraNFSe aceita date-time com fuso (RFC 3339, forma do leiaute), com fuso sem ':'
// (-0300) ou só horas (-03), e, defensivamente, sem fuso (assumido America/Sao_Paulo) ou só a
// data. O instante é mantido no fuso informado (a data de emissão é a do próprio documento, não
// convertida para UTC).
func parseDataHoraNFSe(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999-0700", "2006-01-02T15:04:05.999999999-07"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	loc := nfseLocBrasilia()
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// decimalNFSe converte um decimal do leiaute para float64; ausente/inválido = 0.
func decimalNFSe(v string) float64 {
	f, _ := decimalNFSeOK(v)
	return f
}

// decimalNFSeOK converte um decimal aceitando "1234.56" (forma do leiaute), "1234,56" e
// "1.234,56"/"1,234.56" (separador de milhar — o último separador é o decimal). ok=false só
// quando o valor NÃO é vazio e não é interpretável (o valor devolvido é 0). Rejeita NaN/Inf/hex
// (que strconv.ParseFloat aceitaria).
func decimalNFSeOK(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, true
	}
	for _, r := range v {
		if !(r >= '0' && r <= '9') && r != '.' && r != ',' && r != '-' && r != '+' {
			return 0, false
		}
	}
	ultimoPonto, ultimaVirgula := strings.LastIndex(v, "."), strings.LastIndex(v, ",")
	switch {
	case ultimaVirgula >= 0 && ultimoPonto >= 0:
		if ultimaVirgula > ultimoPonto { // 1.234,56
			v = strings.ReplaceAll(v, ".", "")
			v = strings.Replace(v, ",", ".", 1)
		} else { // 1,234.56
			v = strings.ReplaceAll(v, ",", "")
		}
	case ultimaVirgula >= 0:
		if strings.Count(v, ",") > 1 {
			return 0, false
		}
		v = strings.Replace(v, ",", ".", 1)
	case strings.Count(v, ".") > 1:
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// normalizarCNPJNFSe mantém só [0-9A-Z] (maiúsculas) — preserva o CNPJ alfanumérico (IN RFB
// 2.229/2024) em vez de descartar as letras como OnlyDigits faria.
func normalizarCNPJNFSe(v string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(v) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// raizCNPJNFSe devolve os 8 primeiros caracteres de um CNPJ de 14 posições (a "raiz", comum a
// matriz e filiais), ou "" se não for um CNPJ de 14 posições.
func raizCNPJNFSe(cnpj string) string {
	n := normalizarCNPJNFSe(cnpj)
	if len(n) != 14 {
		return ""
	}
	return n[:8]
}
