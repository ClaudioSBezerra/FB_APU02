package services

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// nfseXMLCompletoTeste: NFS-e tomada com todos os campos fiscais preenchidos.
func nfseXMLCompletoTeste() nfseXMLOpts {
	return nfseXMLOpts{
		Chave:      "26116062212345678000199000000000000126100000000001",
		NNFSe:      "1261",
		EmitCNPJ:   "99.888.777/0001-66",
		PrestCNPJ:  "99888777000166",
		TomaCNPJ:   "12345678000199",
		DhEmi:      "2026-09-30T23:30:00-03:00",
		DCompet:    "2026-09-01",
		VServ:      "10000.00",
		VISSQN:     "500.00",
		TpRetISSQN: "2",
		VPis:       "65.00",
		VCofins:    "300.00",
		VRetCP:     "1100.00",
		VRetIRRF:   "150.00",
		VRetCSLL:   "100.00",
	}
}

func TestNFSeExtrairDadosNFSeXML_TodosOsCampos(t *testing.T) {
	d, err := extrairDadosNFSeXML([]byte(nfseXMLTeste(nfseXMLCompletoTeste())))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	want := NFSeDadosXML{
		IDInfNFSe:     "NFS26116062212345678000199000000000000126100000000001",
		NumeroNFSe:    "1261",
		CNPJEmitente:  "99888777000166",
		CNPJPrestador: "99888777000166",
		CNPJTomador:   "12345678000199",
		DataEmissao:   "2026-09-30", // data no fuso do próprio XML (não UTC, que seria 01/10)
		MesAno:        "09/2026",
		ValorServicos: 10000, ISS: 500, ISSRetido: 500,
		PIS: 65, COFINS: 300, INSS: 1100, IRRF: 150, CSLL: 100,
	}
	if d != want {
		t.Errorf("extração divergiu:\n veio %+v\nesperado %+v", d, want)
	}
}

func TestNFSeExtrairDadosNFSeXML_TpRetISSQN(t *testing.T) {
	for tp, retido := range map[string]float64{"1": 0, "2": 500, "3": 500, "": 0} {
		o := nfseXMLCompletoTeste()
		o.TpRetISSQN = tp
		d, err := extrairDadosNFSeXML([]byte(nfseXMLTeste(o)))
		if err != nil {
			t.Fatalf("tpRetISSQN=%q: %v", tp, err)
		}
		if d.ISS != 500 || d.ISSRetido != retido {
			t.Errorf("tpRetISSQN=%q: iss=%v iss_retido=%v (esperava retido=%v)", tp, d.ISS, d.ISSRetido, retido)
		}
	}
}

func TestNFSeExtrairDadosNFSeXML_CamposAusentes_Zero(t *testing.T) {
	d, err := extrairDadosNFSeXML([]byte(nfseXMLTeste(nfseXMLOpts{Chave: "1", TomaCPF: "123.456.789-01", PrestCPF: "98765432100"})))
	if err != nil {
		t.Fatalf("campos ausentes não podem ser erro: %v", err)
	}
	if d.ValorServicos != 0 || d.ISS != 0 || d.PIS != 0 || d.COFINS != 0 || d.IRRF != 0 || d.CSLL != 0 || d.INSS != 0 || d.ISSRetido != 0 {
		t.Errorf("esperava valores zerados: %+v", d)
	}
	if d.DataEmissao != "" || d.MesAno != "" || d.NumeroNFSe != "" || d.CNPJTomador != "" {
		t.Errorf("esperava vazios: %+v", d)
	}
	if d.CPFTomador != "12345678901" || d.CPFPrestador != "98765432100" {
		t.Errorf("CPF: %+v", d)
	}
}

func TestNFSeExtrairDadosNFSeXML_SemDCompet_UsaMesDeDhEmi(t *testing.T) {
	o := nfseXMLCompletoTeste()
	o.DCompet = ""
	d, err := extrairDadosNFSeXML([]byte(nfseXMLTeste(o)))
	if err != nil || d.MesAno != "09/2026" {
		t.Fatalf("mes_ano=%q err=%v", d.MesAno, err)
	}
}

func TestNFSeExtrairDadosNFSeXML_ValorInvalido_Zero(t *testing.T) {
	o := nfseXMLCompletoTeste()
	o.VPis = "abc"
	d, err := extrairDadosNFSeXML([]byte(nfseXMLTeste(o)))
	if err != nil || d.PIS != 0 || d.COFINS != 300 {
		t.Fatalf("pis=%v cofins=%v err=%v", d.PIS, d.COFINS, err)
	}
}

func TestNFSeExtrairDadosNFSeXML_XMLInvalidoOuRaizErrada_Erro(t *testing.T) {
	if _, err := extrairDadosNFSeXML([]byte("<NFSe><infNFSe>")); err == nil {
		t.Error("XML truncado deveria ser erro")
	}
	if _, err := extrairDadosNFSeXML([]byte(`<evento xmlns="http://www.sped.fazenda.gov.br/nfse"><infEvento/></evento>`)); err == nil {
		t.Error("raiz diferente de NFSe deveria ser erro")
	}
}

func TestNFSeDecodificarArquivoXmlADN_RoundTrip(t *testing.T) {
	xmlOrig := nfseXMLTeste(nfseXMLCompletoTeste())
	b64 := gzipB64Teste(t, []byte(xmlOrig))
	// quebras de linha no base64 (MIME) são toleradas
	quebrado := b64[:10] + "\n" + b64[10:]
	got, err := decodificarArquivoXmlADN(quebrado)
	if err != nil || string(got) != xmlOrig {
		t.Fatalf("round-trip falhou: err=%v", err)
	}
}

func TestNFSeDecodificarArquivoXmlADN_Invalidos(t *testing.T) {
	if _, err := decodificarArquivoXmlADN(""); err == nil {
		t.Error("vazio deveria ser erro")
	}
	if _, err := decodificarArquivoXmlADN("!!!nao-base64!!!"); err == nil {
		t.Error("base64 inválido deveria ser erro")
	}
	if _, err := decodificarArquivoXmlADN(base64.StdEncoding.EncodeToString([]byte("<NFSe/> sem gzip"))); err == nil {
		t.Error("conteúdo não-gzip deveria ser erro")
	}
	b64 := gzipB64Teste(t, []byte(strings.Repeat("x", 1000)))
	raw, _ := base64.StdEncoding.DecodeString(b64)
	truncado := base64.StdEncoding.EncodeToString(raw[:len(raw)/2])
	if _, err := decodificarArquivoXmlADN(truncado); err == nil {
		t.Error("gzip truncado deveria ser erro")
	}
}

// Zip-bomb: poucos KB comprimidos que expandem para > 5 MiB → rejeitado sem descomprimir tudo.
func TestNFSeDecodificarArquivoXmlADN_ZipBomb_Rejeitado(t *testing.T) {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(bytes.Repeat([]byte{' '}, 64<<20)) // 64 MiB descomprimidos
	zw.Close()
	if buf.Len() > 1<<20 {
		t.Fatalf("pré-condição: bomba deveria ser pequena comprimida, tem %d bytes", buf.Len())
	}
	_, err := decodificarArquivoXmlADN(base64.StdEncoding.EncodeToString(buf.Bytes()))
	if !errors.Is(err, ErrNFSeXMLGrandeDemais) {
		t.Fatalf("esperava ErrNFSeXMLGrandeDemais, veio %v", err)
	}
}

func TestNFSeDecodificarArquivoXmlADN_ExatamenteNoLimite_Aceito(t *testing.T) {
	b64 := gzipB64Teste(t, bytes.Repeat([]byte{' '}, nfseADNMaxXMLBytes))
	if got, err := decodificarArquivoXmlADN(b64); err != nil || len(got) != nfseADNMaxXMLBytes {
		t.Fatalf("5 MiB exatos deveriam passar: len=%d err=%v", len(got), err)
	}
}

func TestNFSeRaizCNPJ(t *testing.T) {
	casos := map[string]string{
		"12.345.678/0001-99": "12345678",
		"12345678000280":     "12345678",
		"12.ABC.345/01DE-35": "12ABC345", // CNPJ alfanumérico
		"12345678901":        "",         // CPF
		"":                   "",
	}
	for in, want := range casos {
		if got := raizCNPJNFSe(in); got != want {
			t.Errorf("raizCNPJNFSe(%q) = %q, esperava %q", in, got, want)
		}
	}
}

// latin1Teste converte texto UTF-8 (só runes ≤ 0xFF) para bytes ISO-8859-1.
func latin1Teste(t *testing.T, s string) []byte {
	t.Helper()
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			t.Fatalf("rune %U fora do ISO-8859-1", r)
		}
		out = append(out, byte(r))
	}
	return out
}

func TestNFSeExtrairDadosNFSeXML_ISO88591EWindows1252(t *testing.T) {
	base := strings.Replace(nfseXMLTeste(nfseXMLCompletoTeste()), `encoding="UTF-8"`, `encoding="ISO-8859-1"`, 1)
	if !strings.Contains(base, "técnico") {
		t.Fatal("pré-condição: fixture deveria conter acento")
	}
	d, err := extrairDadosNFSeXML(latin1Teste(t, base))
	if err != nil {
		t.Fatalf("XML ISO-8859-1 deveria ser aceito: %v", err)
	}
	if d.ValorServicos != 10000 || d.CNPJTomador != "12345678000199" {
		t.Errorf("extração ISO-8859-1 divergiu: %+v", d)
	}

	cp := strings.Replace(nfseXMLTeste(nfseXMLCompletoTeste()), `encoding="UTF-8"`, `encoding="windows-1252"`, 1)
	raw := latin1Teste(t, cp)
	raw = bytes.Replace(raw, []byte("Suporte"), []byte("Suporte \x80"), 1) // € em cp1252
	if _, err := extrairDadosNFSeXML(raw); err != nil {
		t.Fatalf("XML windows-1252 deveria ser aceito: %v", err)
	}
	r, err := nfseCharsetReader("windows-1252", bytes.NewReader([]byte{'a', 0x80, 0xE9}))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.String() != "a€é" {
		t.Errorf("cp1252 → UTF-8: %q", buf.String())
	}
	if _, err := nfseCharsetReader("shift_jis", bytes.NewReader(nil)); err == nil {
		t.Error("encoding não suportado deveria ser erro")
	}
}

func TestNFSeDecimalNFSeOK_FormatosAceitos(t *testing.T) {
	casos := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1234.56", 1234.56, true},
		{"1234,56", 1234.56, true},
		{"1.234,56", 1234.56, true},
		{"1.234.567,89", 1234567.89, true},
		{"1,234.56", 1234.56, true},
		{" 10 ", 10, true},
		{"", 0, true},
		{"-5.00", -5, true},
		{"abc", 0, false},
		{"NaN", 0, false},
		{"Inf", 0, false},
		{"0x10", 0, false},
		{"1.2.3", 0, false},
		{"1,2,3", 0, false},
	}
	for _, c := range casos {
		got, ok := decimalNFSeOK(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("decimalNFSeOK(%q) = %v,%t; esperava %v,%t", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNFSeExtrairDadosNFSeXML_DecimalComVirgulaECamposInvalidos(t *testing.T) {
	o := nfseXMLCompletoTeste()
	o.VServ = "10.000,00"
	o.VISSQN = "500,00"
	o.VPis = "abc"
	d, invalidos, err := extrairDadosNFSeXMLDetalhado([]byte(nfseXMLTeste(o)))
	if err != nil {
		t.Fatal(err)
	}
	if d.ValorServicos != 10000 || d.ISS != 500 || d.PIS != 0 {
		t.Errorf("decimais com vírgula: %+v", d)
	}
	if len(invalidos) != 1 || invalidos[0] != "vPis" {
		t.Errorf("campos inválidos reportados: %v", invalidos)
	}
}

func TestNFSeParseDataHoraNFSe_Fusos(t *testing.T) {
	brt := time.FixedZone("", -3*3600)
	casos := map[string]time.Time{
		"2026-09-30T23:30:00-03:00":    time.Date(2026, 9, 30, 23, 30, 0, 0, brt),
		"2026-09-30T23:30:00-0300":     time.Date(2026, 9, 30, 23, 30, 0, 0, brt),
		"2026-09-30T23:30:00.123-0300": time.Date(2026, 9, 30, 23, 30, 0, 123e6, brt),
		"2026-09-30T23:30:00-03":       time.Date(2026, 9, 30, 23, 30, 0, 0, brt),
		"2026-09-30T23:30:00Z":         time.Date(2026, 9, 30, 23, 30, 0, 0, time.UTC),
		"2026-09-30T23:30:00":          time.Date(2026, 9, 30, 23, 30, 0, 0, brt), // sem fuso = Brasília
		"2026-09-30":                   time.Date(2026, 9, 30, 0, 0, 0, 0, brt),
	}
	for in, want := range casos {
		got, ok := parseDataHoraNFSe(in)
		if !ok || !got.Equal(want) {
			t.Errorf("parseDataHoraNFSe(%q) = %v,%t; esperava %v", in, got, ok, want)
		}
	}
	if _, ok := parseDataHoraNFSe("30/09/2026"); ok {
		t.Error("formato desconhecido não deveria ser aceito")
	}
	// -0300 mantém a data do documento (não converte para UTC, que seria 01/10).
	o := nfseXMLCompletoTeste()
	o.DhEmi = "2026-09-30T23:30:00-0300"
	o.DCompet = ""
	d, err := extrairDadosNFSeXML([]byte(nfseXMLTeste(o)))
	if err != nil || d.DataEmissao != "2026-09-30" || d.MesAno != "09/2026" {
		t.Errorf("dhEmi -0300: data=%q mes=%q err=%v", d.DataEmissao, d.MesAno, err)
	}
}

func TestNFSeDecodificarArquivoXmlADN_VariantesBase64(t *testing.T) {
	xmlOrig := []byte(nfseXMLTeste(nfseXMLCompletoTeste()))
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(xmlOrig)
	zw.Close()
	for nome, enc := range map[string]*base64.Encoding{
		"std": base64.StdEncoding, "rawstd": base64.RawStdEncoding, "url": base64.URLEncoding, "rawurl": base64.RawURLEncoding,
	} {
		got, err := decodificarArquivoXmlADN(enc.EncodeToString(buf.Bytes()))
		if err != nil || !bytes.Equal(got, xmlOrig) {
			t.Errorf("base64 %s: err=%v", nome, err)
		}
	}
}
