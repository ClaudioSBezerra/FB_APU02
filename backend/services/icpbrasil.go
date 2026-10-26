// Package services — raízes ICP-Brasil embutidas (ver spec-nfse-cadeia-icp-brasil.md).
//
// O Portal Nacional ADN só aceita certificados de cliente cuja cadeia feche numa das raízes
// ICP-Brasil v5/v10/v11/v12. Essas 4 raízes ficam embutidas no binário (go:embed) e são a
// ÚNICA âncora de confiança usada por CompletarCadeiaICPBrasil (nfse_cadeia.go) — nunca o pool
// de CAs do sistema operacional. A integridade de cada arquivo é conferida contra o SHA-256
// abaixo no carregamento: um arquivo adulterado/trocado no repositório faz o carregamento
// falhar (upload responde 500, problema de ambiente/deploy), nunca é aceito silenciosamente.
package services

import (
	"crypto/sha256"
	"crypto/x509"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

//go:embed icpbrasil_raizes/*.crt
var icpBrasilRaizesFS embed.FS

// icpBrasilRaizesSHA256 — SHA-256 do DER de cada raiz oficial, baixadas de
// acraiz.icpbrasil.gov.br/credenciadas/RAIZ/ICP-Brasilv{N}.crt (por http e por https, cópias
// idênticas) e conferidos em 26/10/2026. O nome do arquivo é a chave; qualquer .crt embutido
// sem entrada aqui, ou entrada sem arquivo, também faz o carregamento falhar.
//
//	v5  -> vence 2029-03-02
//	v10 -> vence 2032-07-01
//	v11 -> vence 2032-07-01
//	v12 -> vence 2037-10-22
var icpBrasilRaizesSHA256 = map[string]string{
	"ICP-Brasilv5.crt":  "caa53fc6091c6951887c976e378f6ef89aa6377c55d97b6475422b71ed7e9b17",
	"ICP-Brasilv10.crt": "6e0bff069a26994c15de2c4888cc54af84882e5495b7fbf66be9ccffec7489f6",
	"ICP-Brasilv11.crt": "1406710058180fa4081aab3f246f1702429c552a11fa3143b84c88cb3ab8e5e7",
	"ICP-Brasilv12.crt": "d8478e37ce19c690cf657381e68fe600e4e1a042536830f06847e03e554c4b01",
}

// raizesConfiaveis agrupa o pool usado em x509.VerifyOptions.Roots e a lista dos mesmos
// certificados (CertPool não permite procurar por Subject — a lista é usada para detectar
// quando o emissor da ponta da cadeia já é uma raiz, ver nfse_cadeia.go).
type raizesConfiaveis struct {
	pool  *x509.CertPool
	certs []*x509.Certificate
}

var (
	icpBrasilRaizesOnce sync.Once
	icpBrasilRaizesVal  *raizesConfiaveis
	icpBrasilRaizesErr  error
)

// nfseRaizesTeste, quando não-nil, substitui as raízes ICP-Brasil embutidas. NÃO exportada e
// sem nenhum caminho de configuração (env var/config) — só testes deste mesmo pacote
// (nfse_cadeia_test.go) conseguem alterá-la, para validar cadeias geradas no próprio teste
// (ninguém tem a chave privada de uma raiz ICP-Brasil real para assinar um certificado de teste).
var nfseRaizesTeste *raizesConfiaveis

// raizesICPBrasil devolve as raízes confiáveis (embutidas, carregadas e conferidas uma única
// vez), ou o override de teste.
func raizesICPBrasil() (*raizesConfiaveis, error) {
	if nfseRaizesTeste != nil {
		return nfseRaizesTeste, nil
	}
	icpBrasilRaizesOnce.Do(func() {
		icpBrasilRaizesVal, icpBrasilRaizesErr = carregarRaizes(icpBrasilRaizesFS, "icpbrasil_raizes", icpBrasilRaizesSHA256)
	})
	return icpBrasilRaizesVal, icpBrasilRaizesErr
}

// carregarRaizes lê todos os .crt (DER) de dir em fsys, confere o SHA-256 de cada um contra
// esperados e devolve o pool. Falha se qualquer hash divergir, se houver arquivo sem hash
// esperado ou hash esperado sem arquivo, ou se algum certificado não for uma CA.
func carregarRaizes(fsys fs.FS, dir string, esperados map[string]string) (*raizesConfiaveis, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("falha ao listar raízes ICP-Brasil embutidas: %w", err)
	}
	vistos := make(map[string]bool, len(entries))
	r := &raizesConfiaveis{pool: x509.NewCertPool()}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".crt") {
			continue
		}
		esperado, ok := esperados[e.Name()]
		if !ok {
			return nil, fmt.Errorf("raiz embutida %s sem SHA-256 esperado documentado", e.Name())
		}
		der, rerr := fs.ReadFile(fsys, dir+"/"+e.Name())
		if rerr != nil {
			return nil, fmt.Errorf("falha ao ler raiz embutida %s: %w", e.Name(), rerr)
		}
		soma := sha256.Sum256(der)
		if got := hex.EncodeToString(soma[:]); got != strings.ToLower(esperado) {
			return nil, fmt.Errorf("SHA-256 da raiz embutida %s diverge do esperado (esperado %s, obtido %s)", e.Name(), esperado, got)
		}
		cert, perr := x509.ParseCertificate(der)
		if perr != nil {
			return nil, fmt.Errorf("raiz embutida %s não é um certificado DER válido: %w", e.Name(), perr)
		}
		if !cert.IsCA {
			return nil, fmt.Errorf("raiz embutida %s não é uma CA", e.Name())
		}
		r.pool.AddCert(cert)
		r.certs = append(r.certs, cert)
		vistos[e.Name()] = true
	}
	var faltando []string
	for nome := range esperados {
		if !vistos[nome] {
			faltando = append(faltando, nome)
		}
	}
	if len(faltando) > 0 {
		sort.Strings(faltando)
		return nil, fmt.Errorf("raízes ICP-Brasil esperadas ausentes do binário: %s", strings.Join(faltando, ", "))
	}
	if len(r.certs) == 0 {
		return nil, errors.New("nenhuma raiz ICP-Brasil embutida")
	}
	return r, nil
}
