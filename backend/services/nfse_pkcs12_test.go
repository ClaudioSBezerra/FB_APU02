package services

import (
	"bytes"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"testing"

	"software.sslmate.com/src/go-pkcs12"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildTestPfxBytes gera um .pfx (PKCS#12) de teste VÁLIDO e DER-compatível (via
// pkcs12.Modern.Encode, que usa encoding/asn1.Marshal — sempre produz DER, nunca BER) —
// reaproveita genTestCA/genTestLeaf já definidos em nfse_adn_test.go (mesmo pacote). Devolve os
// bytes crus, não base64 (diferente de buildTestPfxBase64, usado pelos testes do client mTLS).
func buildTestPfxBytes(t *testing.T, senha string) []byte {
	t.Helper()
	ca, caKey := genTestCA(t)
	leaf, leafKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "teste-nfse-pkcs12"})
	pfxBytes, err := pkcs12.Modern.Encode(leafKey, leaf, nil, senha)
	if err != nil {
		t.Fatalf("falha ao empacotar .pfx de teste: %v", err)
	}
	return pfxBytes
}

// buildTestPfxBytesComCadeia gera um .pfx (PKCS#12) de teste DER-compatível, mas — diferente de
// buildTestPfxBytes — inclui a CA intermediária que assinou o leaf no próprio .pfx (terceiro
// argumento de pkcs12.Modern.Encode, caCerts), reproduzindo o formato comum em certificados
// ICP-Brasil A1 reais que têm cadeia de CA intermediária embutida. pkcs12.Decode rejeita esse
// formato com "expected exactly two safe bags in the PFX PDU" (confirmado lendo
// vendor/software.sslmate.com/src/go-pkcs12/pkcs12.go) — só pkcs12.DecodeChain aceita.
func buildTestPfxBytesComCadeia(t *testing.T, senha string) []byte {
	t.Helper()
	ca, caKey := genTestCA(t)
	leaf, leafKey := genTestLeaf(t, ca, caKey, leafOpts{commonName: "teste-nfse-pkcs12-com-cadeia"})
	pfxBytes, err := pkcs12.Modern.Encode(leafKey, leaf, []*x509.Certificate{ca}, senha)
	if err != nil {
		t.Fatalf("falha ao empacotar .pfx de teste com cadeia de CA: %v", err)
	}
	return pfxBytes
}

// mangleToIndefiniteLengthBER reescreve o byte de comprimento da SEQUENCE externa de um .pfx
// DER válido para "indefinite length" (0x80) e adiciona o marcador de fim-de-conteúdo (0x00
// 0x00) ao final — reproduz de verdade o erro visto em produção com o certificado real da
// Ferreira Costa ("asn1: syntax error: indefinite length found (not DER)"), sem depender de uma
// fixture de certificado real. Confirmado experimentalmente (ver histórico de implementação)
// que: (1) encoding/asn1 (pkcs12.Decode) rejeita o resultado com a MESMA mensagem de erro do
// caso real; (2) openssl (usado como fallback) consegue ler esse BER perfeitamente e, após o
// round-trip, o resultado volta a decodificar com pkcs12.Decode.
func mangleToIndefiniteLengthBER(t *testing.T, pfxBytes []byte) []byte {
	t.Helper()
	if len(pfxBytes) < 2 || pfxBytes[0] != 0x30 {
		t.Fatalf("esperava .pfx começando com SEQUENCE (0x30), veio %x", pfxBytes[:2])
	}
	var headerLen int
	if pfxBytes[1]&0x80 == 0 {
		headerLen = 2 // forma curta de comprimento (1 byte)
	} else {
		numLenBytes := int(pfxBytes[1] & 0x7F)
		headerLen = 2 + numLenBytes // forma longa: byte inicial + N bytes de comprimento
	}
	content := pfxBytes[headerLen:]
	mangled := append([]byte{0x30, 0x80}, content...) // 0x80 = comprimento indefinido (BER)
	mangled = append(mangled, 0x00, 0x00)             // marcador de fim-de-conteúdo (EOC)
	return mangled
}

// ---------------------------------------------------------------------------
// Testes
// ---------------------------------------------------------------------------

// TestNormalizarPKCS12ParaDER_JaCompativel_NaoNormaliza cobre o caminho rápido da I/O Matrix:
// certificado já DER-compatível decodifica de primeira, sem invocar openssl — confirmado aqui
// porque o teste NÃO depende do binário openssl estar disponível (se LookPath falhasse aqui, o
// teste falharia e exporia um bug: o caminho rápido nunca deveria chegar a checar openssl).
func TestNormalizarPKCS12ParaDER_JaCompativel_NaoNormaliza(t *testing.T) {
	senha := "senha-correta-123"
	original := buildTestPfxBytes(t, senha)

	normalizado, foiNormalizado, err := NormalizarPKCS12ParaDER(original, senha)
	if err != nil {
		t.Fatalf("esperava sucesso no caminho rápido, veio erro: %v", err)
	}
	if foiNormalizado {
		t.Error("esperava foiNormalizado=false para certificado já DER-compatível")
	}
	if !bytes.Equal(normalizado, original) {
		t.Error("esperava que o caminho rápido devolvesse exatamente os bytes originais, sem transformação")
	}
}

// TestNormalizarPKCS12ParaDER_BERSintetico_NormalizaViaOpenssl cobre a linha mais importante da
// I/O Matrix: certificado BER (o caso real de produção) — pkcs12.Decode falha, o round-trip via
// openssl corrige, e o resultado final decodifica direto com pkcs12.Decode. Requer o binário
// openssl disponível no ambiente de teste (confirmado disponível em CI/dev — ver Dockerfile,
// que agora também o inclui na imagem de execução).
func TestNormalizarPKCS12ParaDER_BERSintetico_NormalizaViaOpenssl(t *testing.T) {
	senha := "senha-ber-456"
	original := buildTestPfxBytes(t, senha)

	// Confirma que o arquivo original é DER válido antes de mangle — isolando a mudança de
	// comportamento exclusivamente ao BER sintético abaixo.
	if _, _, err := pkcs12.Decode(original, senha); err != nil {
		t.Fatalf("pré-condição falhou: certificado original deveria decodificar direto: %v", err)
	}

	ber := mangleToIndefiniteLengthBER(t, original)

	// Confirma que o arquivo mangled reproduz de verdade o erro visto em produção.
	if _, _, err := pkcs12.Decode(ber, senha); err == nil {
		t.Fatal("pré-condição falhou: certificado BER sintético deveria falhar no pkcs12.Decode direto")
	}

	normalizado, foiNormalizado, err := NormalizarPKCS12ParaDER(ber, senha)
	if err != nil {
		t.Fatalf("esperava normalização via openssl com sucesso, veio erro: %v", err)
	}
	if !foiNormalizado {
		t.Error("esperava foiNormalizado=true após round-trip via openssl")
	}
	privKey, cert, decErr := pkcs12.Decode(normalizado, senha)
	if decErr != nil {
		t.Fatalf("resultado normalizado deveria decodificar direto com pkcs12.Decode, veio erro: %v", decErr)
	}
	if privKey == nil || cert == nil {
		t.Error("esperava chave privada e certificado não nulos após decodificar o resultado normalizado")
	}
}

// TestNormalizarPKCS12ParaDER_SenhaIncorreta_Erro cobre a linha "Senha realmente incorreta" da
// I/O Matrix: pkcs12.Decode falha E openssl também falha (MAC/senha) — deve retornar um erro que
// NÃO é ErrNFSeOpensslIndisponivel (o handler mapeia esse caso para 400, nunca 500).
func TestNormalizarPKCS12ParaDER_SenhaIncorreta_Erro(t *testing.T) {
	pfxBytes := buildTestPfxBytes(t, "senha-correta")

	_, foiNormalizado, err := NormalizarPKCS12ParaDER(pfxBytes, "senha-ERRADA")
	if err == nil {
		t.Fatal("esperava erro para senha incorreta, veio sucesso")
	}
	if errors.Is(err, ErrNFSeOpensslIndisponivel) {
		t.Fatal("senha incorreta nunca deveria ser confundida com openssl ausente")
	}
	if foiNormalizado {
		t.Error("esperava foiNormalizado=false quando a normalização falha")
	}
}

// TestNormalizarPKCS12ParaDER_ArquivoCorrompido_Erro cobre o caso em que nem o pkcs12.Decode
// direto, nem o round-trip via openssl conseguem processar o arquivo (não é um PKCS#12 de
// verdade, corrompido/lixo) — deve retornar um erro claro, nunca ErrNFSeOpensslIndisponivel.
func TestNormalizarPKCS12ParaDER_ArquivoCorrompido_Erro(t *testing.T) {
	lixo := []byte("isto definitivamente nao é um arquivo pkcs12 valido, so texto qualquer para forcar falha total")

	_, foiNormalizado, err := NormalizarPKCS12ParaDER(lixo, "qualquer-senha")
	if err == nil {
		t.Fatal("esperava erro para arquivo corrompido/não-PKCS12, veio sucesso")
	}
	if errors.Is(err, ErrNFSeOpensslIndisponivel) {
		t.Fatal("arquivo corrompido nunca deveria ser confundido com openssl ausente")
	}
	if foiNormalizado {
		t.Error("esperava foiNormalizado=false quando a normalização falha")
	}
}

// TestNormalizarPKCS12ParaDER_OpensslAusente_ErroEspecifico cobre a linha "openssl ausente do
// ambiente" da I/O Matrix: exec.LookPath("openssl") falhando deve retornar um erro claro e
// específico (ErrNFSeOpensslIndisponivel), nunca confundido com senha incorreta — mesmo quando
// o certificado é genuinamente BER (precisaria do fallback). Simulado via PATH vazio (não
// desinstala o openssl do ambiente de CI/dev, só o torna momentaneamente inacessível para este
// processo de teste).
func TestNormalizarPKCS12ParaDER_OpensslAusente_ErroEspecifico(t *testing.T) {
	senha := "senha-para-teste-sem-openssl"
	pfxBytes := buildTestPfxBytes(t, senha)
	ber := mangleToIndefiniteLengthBER(t, pfxBytes) // precisaria do fallback se openssl existisse

	t.Setenv("PATH", "")

	_, foiNormalizado, err := NormalizarPKCS12ParaDER(ber, senha)
	if !errors.Is(err, ErrNFSeOpensslIndisponivel) {
		t.Fatalf("esperava ErrNFSeOpensslIndisponivel com PATH vazio, veio: %v", err)
	}
	if foiNormalizado {
		t.Error("esperava foiNormalizado=false quando openssl está ausente")
	}
}

// TestNormalizarPKCS12ParaDER_SenhaComQuebraDeLinha_ErroClaro cobre o Patch 4 (revisão
// adversarial): -passin/-passout file: do openssl só lê a PRIMEIRA linha do arquivo — uma senha
// com \n/\r embutido seria silenciosamente truncada, causando uma falha de senha "errada" mesmo
// quando a senha real estava correta. Deve ser rejeitada antes de qualquer tentativa (nem
// chamando pkcs12.DecodeChain nem openssl), com um erro claro e específico.
func TestNormalizarPKCS12ParaDER_SenhaComQuebraDeLinha_ErroClaro(t *testing.T) {
	pfxBytes := buildTestPfxBytes(t, "senha-qualquer")

	for _, senha := range []string{"senha\ncom-quebra", "senha\rcom-cr", "senha\r\ncom-crlf"} {
		_, foiNormalizado, err := NormalizarPKCS12ParaDER(pfxBytes, senha)
		if err == nil {
			t.Fatalf("esperava erro para senha com quebra de linha (%q), veio sucesso", senha)
		}
		if errors.Is(err, ErrNFSeOpensslIndisponivel) || errors.Is(err, ErrNFSeAmbienteTemporarioIndisponivel) {
			t.Fatalf("senha com quebra de linha nunca deveria ser confundida com erro de ambiente, veio: %v", err)
		}
		if foiNormalizado {
			t.Error("esperava foiNormalizado=false para senha com quebra de linha")
		}
	}
}

// TestNormalizarPKCS12ParaDER_ComCadeiaIntermediaria_Aceita cobre o Patch 1 (CRÍTICO, revisão
// adversarial): certificados ICP-Brasil reais frequentemente trazem a cadeia de CA
// intermediária embutida no mesmo .pfx — pkcs12.Decode rejeita esse formato com "expected
// exactly two safe bags in the PFX PDU" (confirmado lendo o vendor), mas pkcs12.DecodeChain
// aceita. Certificado já é DER-compatível (via pkcs12.Modern.Encode), então deve passar pelo
// caminho rápido sem precisar do fallback via openssl.
func TestNormalizarPKCS12ParaDER_ComCadeiaIntermediaria_Aceita(t *testing.T) {
	senha := "senha-com-cadeia-intermediaria"
	comCadeia := buildTestPfxBytesComCadeia(t, senha)

	// Pré-condição: confirma que é justamente o cenário que pkcs12.Decode (sem Chain) rejeita —
	// isolando a mudança de comportamento exclusivamente ao uso de DecodeChain.
	if _, _, err := pkcs12.Decode(comCadeia, senha); err == nil {
		t.Fatal("pré-condição falhou: pkcs12.Decode deveria rejeitar .pfx com cadeia de CA intermediária")
	}

	normalizado, foiNormalizado, err := NormalizarPKCS12ParaDER(comCadeia, senha)
	if err != nil {
		t.Fatalf("esperava sucesso para .pfx com cadeia de CA intermediária (caminho rápido via DecodeChain), veio erro: %v", err)
	}
	if foiNormalizado {
		t.Error("certificado já DER-compatível com cadeia não deveria precisar do fallback via openssl")
	}
	if !bytes.Equal(normalizado, comCadeia) {
		t.Error("esperava que o caminho rápido devolvesse exatamente os bytes originais, sem transformação")
	}
}

// fakeOpensslQueAceitaMasProduzLixo instala no PATH (via t.Setenv, isolado por teste) um
// executável "openssl" FALSO que sempre sai com status 0 (sucesso) para qualquer subcomando,
// mas escreve conteúdo que não é um PEM/PKCS#12 válido em qualquer "-out <arquivo>" que receber.
// Usado para exercitar de forma confiável e determinística o branch "openssl aceita mas o
// resultado final ainda não é lido pelo pkcs12.DecodeChain" (Patch 10) — depender de uma
// combinação real de cifra/senha que o openssl aceitasse mas o go-pkcs12 rejeitasse seria frágil
// e dependente da versão do openssl instalada; forjar o binário auxiliar dá controle total e
// determinístico sobre esse caminho de erro específico, sem alterar o código de produção.
func fakeOpensslQueAceitaMasProduzLixo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
prev=""
out=""
for arg in "$@"; do
  if [ "$prev" = "-out" ]; then
    out="$arg"
  fi
  prev="$arg"
done
if [ -n "$out" ]; then
  printf 'nao sou um pem nem pkcs12 valido, so lixo de teste' > "$out"
fi
exit 0
`
	scriptPath := dir + "/openssl"
	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		t.Fatalf("falha ao escrever openssl fake de teste: %v", err)
	}
	t.Setenv("PATH", dir)
}

// TestNormalizarPKCS12ParaDER_OpensslCorrigeMasGoNaoLe_Erro400 cobre o Patch 10 (revisão
// adversarial): a I/O Matrix da spec já previa o cenário "openssl corrige o arquivo mas o
// resultado ainda não decodifica no Go" (mapeia para 400), mas não havia nenhum teste exercitando
// esse branch específico (a revalidação final com pkcs12.DecodeChain falhando mesmo depois do
// round-trip via openssl "ter sucesso"). Usa um openssl FALSO (ver
// fakeOpensslQueAceitaMasProduzLixo) que sempre sai com status 0 mas nunca produz um PKCS#12
// válido — isolando deterministicamente esse caminho de erro, sem depender de uma combinação
// real de cifra que o openssl instalado aceite e o go-pkcs12 rejeite.
func TestNormalizarPKCS12ParaDER_OpensslCorrigeMasGoNaoLe_Erro400(t *testing.T) {
	fakeOpensslQueAceitaMasProduzLixo(t)

	// Conteúdo irrelevante para o openssl fake (que ignora a entrada), mas precisa falhar no
	// pkcs12.DecodeChain direto para cair no caminho do fallback.
	lixo := []byte("qualquer coisa que nao decodifica direto como pkcs12")

	_, foiNormalizado, err := NormalizarPKCS12ParaDER(lixo, "qualquer-senha")
	if err == nil {
		t.Fatal("esperava erro quando o resultado do openssl (falso) não decodifica no Go, veio sucesso")
	}
	if errors.Is(err, ErrNFSeOpensslIndisponivel) || errors.Is(err, ErrNFSeAmbienteTemporarioIndisponivel) {
		t.Fatalf("esse cenário é falha de arquivo/revalidação, não de ambiente/openssl ausente, veio: %v", err)
	}
	if foiNormalizado {
		t.Error("esperava foiNormalizado=false quando a revalidação final falha")
	}
	if !strings.Contains(err.Error(), "DecodeChain") {
		t.Errorf("esperava mensagem de erro mencionando a revalidação via pkcs12.DecodeChain, veio: %v", err)
	}
}

// Patch 5 (revisão adversarial da cadeia ICP-Brasil): os subprocessos openssl da normalização
// rodam com ambiente mínimo (só PATH) — nada de OPENSSL_CONF/OPENSSL_ENGINES/variáveis do
// processo do servidor (ver opensslQueGravaAmbiente em nfse_cadeia_test.go).
func TestNormalizarPKCS12ParaDER_OpensslAmbienteMinimo(t *testing.T) {
	arquivoEnv := opensslQueGravaAmbiente(t)
	if _, _, err := NormalizarPKCS12ParaDER([]byte("nao e pkcs12, cai no fallback via openssl"), "senha"); err == nil {
		t.Fatal("openssl falso falha — esperava erro")
	}
	exigirAmbienteMinimo(t, arquivoEnv)
}
