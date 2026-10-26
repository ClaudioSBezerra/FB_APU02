// Package services — normalização de certificados PKCS#12 (.pfx) BER->DER no upload.
//
// Contexto (ver spec-nfse-pkcs12-normalizacao.md): certificado real de produção (Ferreira
// Costa, 26/10/2026) falhou com "pkcs12: error reading P12 data: asn1: syntax error:
// indefinite length found (not DER)" — encoding/asn1 do Go (usado por
// software.sslmate.com/src/go-pkcs12, ver nfse_adn.go) só lê PKCS#12 estritamente DER, mas boa
// parte dos certificados A1 reais usa BER. Pedir reexportação manual via OpenSSL para cada
// cliente não escala, então a normalização acontece automaticamente aqui, no upload
// (SaveNFSeCertificadoHandler), nunca no caminho de busca (BuildNFSeMTLSClient continua simples,
// sem fallback — ver Boundaries da spec).
package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// nfseOpensslTimeout limita cada chamada ao openssl — protege o upload de travar
// indefinidamente se o processo auxiliar pendurar. Cada uma das 2 chamadas (extração e
// exportação) recebe seu PRÓPRIO context.WithTimeout de nfseOpensslTimeout — não um único
// contexto compartilhado entre as duas, que daria um orçamento de 10s pras duas juntas em vez
// de 10s pra cada uma.
const nfseOpensslTimeout = 10 * time.Second

// ErrNFSeOpensslIndisponivel sinaliza que o binário openssl não está disponível no ambiente do
// servidor — problema de ambiente/deploy, nunca confundir com senha incorreta (ver I/O Matrix
// da spec: deve mapear para HTTP 500 com log explícito pro operador, não 400).
var ErrNFSeOpensslIndisponivel = errors.New("openssl não disponível no ambiente do servidor")

// ErrNFSeAmbienteTemporarioIndisponivel sinaliza falha de AMBIENTE/infra (disco cheio,
// permissão, diretório temporário) ao preparar os arquivos auxiliares da normalização — nunca
// confundir com senha incorreta ou arquivo inválido. Mesmo tratamento de ErrNFSeOpensslIndisponivel
// no handler: HTTP 500 com log explícito pro operador, não 400.
var ErrNFSeAmbienteTemporarioIndisponivel = errors.New("ambiente temporário do servidor indisponível para normalizar o certificado")

// NormalizarPKCS12ParaDER tenta decodificar pfxBytes diretamente com pkcs12.Decode (caminho
// rápido, sem subprocesso, cobre certificados já DER-compatíveis). Se isso falhar — o caso
// real confirmado em produção são certificados A1 codificados em BER, que o
// encoding/asn1 padrão do Go não lê — tenta um round-trip via openssl (único uso de
// os/exec em todo o backend, ver Design Notes da spec) para reconverter o arquivo para DER, e
// revalida o resultado com pkcs12.Decode antes de aceitar: o openssl NUNCA é confiado às
// cegas, e o normalizado só é devolvido se o próprio Go conseguir lê-lo de volta com a mesma
// senha.
//
// Retorna (pfxBytes original, false, nil) se o caminho rápido já funcionou — o chamador não
// deve persistir nada diferente do que recebeu. Retorna (bytes normalizados, true, nil) se o
// round-trip via openssl corrigiu o arquivo. Em qualquer outro caso, retorna um erro — o
// chamador distingue "openssl ausente" (ErrNFSeOpensslIndisponivel, mapeia para 500) de
// qualquer outra falha (senha incorreta ou arquivo realmente inválido mesmo após a tentativa de
// correção, mapeia para 400).
func NormalizarPKCS12ParaDER(pfxBytes []byte, senha string) (normalizado []byte, foiNormalizado bool, err error) {
	// -passin/-passout file: do openssl só lê a PRIMEIRA linha do arquivo — uma senha com
	// quebra de linha embutida seria silenciosamente truncada, causando uma falha de senha
	// "errada" mesmo quando a senha real estava correta. Isso é erro de dados do usuário, não
	// ausência de openssl — rejeitado antes de qualquer tentativa.
	if strings.ContainsAny(senha, "\n\r") {
		return nil, false, errors.New("senha do certificado não pode conter quebra de linha")
	}

	if _, _, _, decErr := pkcs12.DecodeChain(pfxBytes, senha); decErr == nil {
		return pfxBytes, false, nil
	}

	if _, lookErr := exec.LookPath("openssl"); lookErr != nil {
		return nil, false, ErrNFSeOpensslIndisponivel
	}

	tmpDir, mkErr := os.MkdirTemp("", "nfse-pfx-*")
	if mkErr != nil {
		return nil, false, fmt.Errorf("%w: falha ao criar diretório temporário para normalização do certificado: %v", ErrNFSeAmbienteTemporarioIndisponivel, mkErr)
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			log.Printf("[NFSePkcs12] falha ao remover diretório temporário %s: %v", tmpDir, rmErr)
		}
	}()

	originalPath := filepath.Join(tmpDir, "original.pfx")
	passPath := filepath.Join(tmpDir, "pass.txt")
	pemPath := filepath.Join(tmpDir, "temp.pem")
	fixedPath := filepath.Join(tmpDir, "fixed.pfx")

	// A senha NUNCA vai como argumento de linha de comando (visível via /proc/<pid>/cmdline
	// para qualquer outro processo no mesmo host) — sempre via -passin/-passout file:, lendo de
	// um arquivo temporário 0600 só legível pelo próprio appuser.
	if wErr := os.WriteFile(originalPath, pfxBytes, 0600); wErr != nil {
		return nil, false, fmt.Errorf("%w: falha ao escrever certificado temporário para normalização: %v", ErrNFSeAmbienteTemporarioIndisponivel, wErr)
	}
	if wErr := os.WriteFile(passPath, []byte(senha), 0600); wErr != nil {
		return nil, false, fmt.Errorf("%w: falha ao escrever senha temporária para normalização: %v", ErrNFSeAmbienteTemporarioIndisponivel, wErr)
	}

	// Passo 1: decripta o .pfx original (BER ou DER, openssl lê ambos) para um PEM
	// intermediário contendo a chave privada em claro — por isso vive só no diretório
	// temporário próprio, removido via defer acima mesmo em caminho de erro.
	extractCtx, extractCancel := context.WithTimeout(context.Background(), nfseOpensslTimeout)
	defer extractCancel()
	extractCmd := exec.CommandContext(extractCtx, "openssl", "pkcs12",
		"-in", originalPath,
		"-out", pemPath,
		"-nodes",
		"-legacy",
		"-passin", "file:"+passPath,
	)
	if out, runErr := extractCmd.CombinedOutput(); runErr != nil {
		if errors.Is(runErr, exec.ErrNotFound) {
			return nil, false, ErrNFSeOpensslIndisponivel
		}
		// stderr/stdout do openssl pode conter detalhes técnicos (caminhos, parâmetros de
		// algoritmo) — nunca repassado ao usuário final, só ao log para o operador investigar.
		return nil, false, fmt.Errorf("openssl pkcs12 (extração) falhou: %w: %s", runErr, string(out))
	}

	// Passo 2: reexporta o PEM intermediário como um novo .pfx — sempre DER-compatível,
	// já que é o próprio openssl (e não o certificado original) que o produz desta vez. Sem
	// -legacy aqui: diferente da extração (que precisa ler as cifras antigas do arquivo
	// original), a exportação usa o padrão moderno do openssl (AES-256/PBKDF2) — go-pkcs12 já
	// suporta PBES2/AES em Decode/DecodeChain, então não há motivo para regravar com cifras
	// fracas (RC2-40-CBC/3DES-CBC + SHA-1) no arquivo que fica persistido.
	exportCtx, exportCancel := context.WithTimeout(context.Background(), nfseOpensslTimeout)
	defer exportCancel()
	exportCmd := exec.CommandContext(exportCtx, "openssl", "pkcs12",
		"-export",
		"-in", pemPath,
		"-out", fixedPath,
		"-passout", "file:"+passPath,
	)
	if out, runErr := exportCmd.CombinedOutput(); runErr != nil {
		if errors.Is(runErr, exec.ErrNotFound) {
			return nil, false, ErrNFSeOpensslIndisponivel
		}
		return nil, false, fmt.Errorf("openssl pkcs12 (exportação) falhou: %w: %s", runErr, string(out))
	}

	fixedBytes, readErr := os.ReadFile(fixedPath)
	if readErr != nil {
		return nil, false, fmt.Errorf("falha ao ler certificado normalizado: %w", readErr)
	}

	// Revalidação obrigatória: nunca aceitar o resultado do openssl sem confirmar que o
	// próprio Go (pkcs12.DecodeChain, a mesma família de função usada em BuildNFSeMTLSClient)
	// consegue lê-lo de volta com a mesma senha.
	if _, _, _, decErr := pkcs12.DecodeChain(fixedBytes, senha); decErr != nil {
		return nil, false, fmt.Errorf("não foi possível normalizar o certificado — o resultado ainda não é lido pelo pkcs12.DecodeChain: %w", decErr)
	}

	return fixedBytes, true, nil
}
