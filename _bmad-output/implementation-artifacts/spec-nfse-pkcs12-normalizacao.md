---
title: 'NFS-e: normalização automática de certificado .pfx BER→DER no upload'
type: 'bugfix'
created: '2026-10-26'
status: 'done'
review_loop_iteration: 0
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-captura-plano.md', '{project-root}/_bmad-output/implementation-artifacts/spec-nfse-certificado-schema.md']
baseline_commit: '7cf0960d6f6e497aae5c4fb6549a999893e012ab'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** certificado real de produção (Ferreira Costa, 26/10/2026) falhou com `pkcs12: error reading P12 data: asn1: syntax error: indefinite length found (not DER)` — `encoding/asn1` do Go só lê PKCS#12 estritamente DER, mas boa parte dos certificados A1 reais usa BER (confirmado: limitação conhecida e nunca corrigida de `software.sslmate.com/src/go-pkcs12`, sem alternativa em nenhuma lib Go). Pedir que cada cliente reexporte o `.pfx` manualmente via OpenSSL não escala.

**Approach:** normalizar automaticamente no upload (`SaveNFSeCertificadoHandler`) — se `pkcs12.Decode` falhar, tentar um round-trip via `openssl` (processo auxiliar, só aqui, nunca no caminho de busca) e persistir a versão já corrigida. Cliente nunca vê esse detalhe técnico.

## Boundaries & Constraints

**Always:** nova função em `services` (ex: `services.NormalizarPKCS12ParaDER`) tenta `pkcs12.Decode` primeiro (caminho rápido, sem subprocesso, pra certificados já DER-compatíveis); só na falha invoca `openssl` via `os/exec` — **primeiro uso de `os/exec` em todo o backend**, documentar como novo padrão arquitetural; senha do certificado NUNCA em argv de processo (`-passin file:`/`-passout file:` com arquivo temporário 0600, nunca `-passin pass:`); todos os arquivos temporários (incluindo o PEM intermediário com a chave privada decriptada) em diretório próprio criado via `os.MkdirTemp`, removido via `defer os.RemoveAll` mesmo em caminho de erro; `exec.LookPath("openssl")` checado antes de tentar, erro claro e específico se o binário não existir; timeout via `exec.CommandContext` (ex: 10s) pra não travar o upload se o `openssl` pendurar; o resultado da normalização é re-validado com `pkcs12.Decode` antes de aceitar (nunca persistir um `.pfx` "corrigido" sem confirmar que o próprio Go consegue lê-lo); `backend/Dockerfile` (estágio de execução, `alpine:latest`) ganha o pacote `openssl` no `apk add` já existente; o que fica em `nfse_certificados.certificado_pfx` após upload bem-sucedido é a versão normalizada (DER), nunca a original BER enviada pelo usuário.

**Ask First:** nenhuma — abordagem e decisões de arquitetura já resolvidas com o usuário em 2026-10-26 (normalizar no upload, não na busca; usuário já aceitou que o certificado atual da Ferreira Costa precisa ser reenviado depois desta correção).

**Never:** tentar normalização no caminho de busca (`BuildNFSeMTLSClient`/`ExecutarCicloNFSe`) — só no upload; migrar/corrigir retroativamente o certificado já persistido da Ferreira Costa (reenvio manual, fora de escopo); mudar o parser de payload do Portal Nacional ADN (`services/nfse_adn.go`, problema separado, ainda não confirmado por falta de certificado funcional até agora); passar a senha do certificado como argumento de linha de comando em qualquer chamada a `openssl`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Certificado já DER-compatível | `pkcs12.Decode` funciona de primeira | Salva normalmente, sem invocar `openssl` | — |
| Certificado BER (caso real de hoje) | `pkcs12.Decode` falha, `openssl` corrige | Salva a versão normalizada; log indica que houve normalização | — |
| Senha realmente incorreta | `pkcs12.Decode` falha E `openssl` também falha (MAC/senha) | 400, mensagem clara de senha/arquivo inválido | `jsonErr(w, 400, ...)` |
| `openssl` ausente no ambiente (deploy incompleto) | `exec.LookPath("openssl")` falha | 500, log explícito pro operador corrigir a imagem — nunca um erro genérico de "senha incorreta" | `sanitizeDBErr` + log |
| `openssl` corrige o arquivo mas o resultado ainda não decodifica no Go | Round-trip não resolveu (caso raro) | 400, mensagem indicando que o certificado não pôde ser processado | `jsonErr(w, 400, ...)` |

</frozen-after-approval>

## Code Map

- `backend/handlers/nfse_credentials.go` -- `SaveNFSeCertificadoHandler`, ponto de integração logo após a checagem XOR pfx/senha (antes de `crypto.EncryptField`)
- `backend/services/nfse_adn.go` -- `BuildNFSeMTLSClient` (referência de uso de `pkcs12.Decode`, NÃO mexer — essa função continua simples, sem fallback)
- `backend/Dockerfile` -- estágio `alpine:latest`, linha `RUN apk --no-cache add tzdata ca-certificates` ganha `openssl`
- `backend/handlers/xml_upload.go` -- `maxUploadBytes()`, já limita o corpo do request (nenhuma mudança necessária, só referência de que o tamanho já está bounded)

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/nfse_pkcs12.go` (novo) -- `NormalizarPKCS12ParaDER(pfxBytes []byte, senha string) (normalizado []byte, foiNormalizado bool, err error)`: tenta `pkcs12.Decode` direto; na falha, roda o round-trip via `openssl` (arquivos temporários 0600, senha via `-passin file:`/`-passout file:`, timeout 10s via `exec.CommandContext`), limpando tudo via `defer os.RemoveAll`; revalida o resultado com `pkcs12.Decode` antes de retornar sucesso.
- [x] `backend/handlers/nfse_credentials.go` -- em `SaveNFSeCertificadoHandler`, logo após a checagem XOR, se `req.CertificadoPfx != ""`: decodifica base64, chama `services.NormalizarPKCS12ParaDER`, substitui `req.CertificadoPfx` pelo resultado (re-encodado em base64) antes de criptografar; erro mapeado pra 400 (senha/arquivo inválido) ou 500 (openssl ausente/falha de ambiente, logado com detalhe pro operador).
- [x] `backend/Dockerfile` -- adicionar `openssl` ao `apk add` do estágio de execução.
- [x] `backend/services/nfse_pkcs12_test.go` -- cobrir a I/O Matrix: certificado DER válido (sem invocar openssl — pode confirmar via um certificado de teste gerado já em DER); certificado BER sintético (gerar um .pfx BER-encoded de teste, ex. via manipulação direta de bytes ASN.1 ou fixture fixa); senha incorreta; `openssl` ausente (mockar `exec.LookPath` ou testar só a lógica de erro, não precisa desinstalar openssl do ambiente de CI).
- [x] `backend/handlers/nfse_credentials_test.go` -- teste cobrindo que um upload com certificado BER sintético é aceito e o que fica persistido já decodifica direto com `pkcs12.Decode` (sem precisar do fallback na leitura).

**Acceptance Criteria:**
- Given um certificado `.pfx` BER-encoded válido (senha correta), when enviado via `POST /api/nfse/certificado`, then o upload é aceito e o valor persistido em `certificado_pfx` decodifica direto com `pkcs12.Decode` (já normalizado).
  - **Coberto por:** `services.TestNormalizarPKCS12ParaDER_BERSintetico_NormalizaViaOpenssl` (unidade, confirma `foiNormalizado=true` e revalidação com `pkcs12.Decode`) e `handlers.TestSaveNFSeCertificadoHandler_CertificadoBER_NormalizaEPersisteDER` (ponta a ponta via HTTP real: POST aceito com 201, lê `certificado_pfx` do banco, descriptografa e confirma que decodifica DIRETO com `pkcs12.Decode`, sem fallback). O .pfx BER é SINTÉTICO (gerado via a técnica de mangle de comprimento ASN.1 descrita no Change Log abaixo) — reproduz byte-a-byte a mesma mensagem de erro vista em produção ("asn1: syntax error: indefinite length found (not DER)"), mas não é o certificado real da Ferreira Costa. A validação definitiva com o certificado real só ocorre quando o usuário reenviar o arquivo em produção (fora do escopo automatizável desta spec).
- Given um certificado com senha incorreta, when enviado, then retorna 400 com mensagem clara, nunca um 500 nem uma mensagem confusa de "openssl falhou".
  - **Coberto por:** `services.TestNormalizarPKCS12ParaDER_SenhaIncorreta_Erro` (unidade: confirma erro != nil e `!errors.Is(err, ErrNFSeOpensslIndisponivel)`) e `handlers.TestSaveNFSeCertificadoHandler_SenhaIncorreta_Retorna400` (ponta a ponta: POST com .pfx válido + senha errada retorna 400, e a mensagem de erro no corpo JSON não contém a palavra "openssl").
- Given o binário `openssl` ausente do ambiente, when um certificado BER é enviado, then retorna 500 com log explícito pro operador (não confundido com senha incorreta).
  - **Coberto por:** `services.TestNormalizarPKCS12ParaDER_OpensslAusente_ErroEspecifico` (unidade: `PATH` esvaziado via `os.Setenv` dentro do teste, restaurado no `defer` — não desinstala o openssl do ambiente real; confirma `errors.Is(err, ErrNFSeOpensslIndisponivel)`) e `handlers.TestSaveNFSeCertificadoHandler_OpensslAusente_Retorna500` (ponta a ponta: mesma técnica de `PATH` vazio, confirma HTTP 500). O log explícito pro operador (`log.Printf` com prefixo `[NFSeCertificado] openssl indisponível...`) foi conferido manualmente na saída do `go test -v` (ver linha de log capturada na execução), não por asserção automatizada sobre o conteúdo exato do log.
- Given um certificado já DER-compatível, when enviado, then o upload funciona exatamente como antes (sem regressão), sem invocar `openssl`.
  - **Coberto por:** `services.TestNormalizarPKCS12ParaDER_JaCompativel_NaoNormaliza` (unidade: confirma `foiNormalizado=false` e bytes devolvidos idênticos aos originais) e `handlers.TestSaveNFSeCertificadoHandler_CertificadoJaDER_NaoRegride` + os testes pré-existentes de upload (`TestSaveNFSeCertificadoHandler_PrimeiroCadastroValido_...`, `TestSaveNFSeCertificadoHandler_ReUpload_...`, etc., todos atualizados para usar `genValidPfxBase64` — um .pfx de teste real e DER-compatível — em vez do conteúdo "fake" anterior, que deixou de ser aceito depois que o handler passou a decodificar PKCS#12 de verdade). Nenhum desses testes depende de `openssl` estar disponível — não "não invocar openssl" não é observável diretamente (não há mock/assert de chamada de processo), mas é garantido pela ordem do código em `NormalizarPKCS12ParaDER`: o `exec.LookPath`/round-trip só é alcançado depois de um `pkcs12.Decode` que já teria retornado sucesso para um arquivo DER.

**Nota de desvio (Change Log):** a spec original não previa impacto nos testes pré-existentes de `nfse_credentials_test.go`. Vários desses testes usavam um `certificado_pfx` "fake" (texto arbitrário em base64, não um PKCS#12 de verdade) só para exercitar a validação de base64 — suficiente antes desta spec, porque o handler nunca decodificava o PKCS#12 de fato. Com a normalização chamando `pkcs12.Decode`/`openssl` de verdade, esse conteúdo fake passou a ser rejeitado (400), quebrando 6 testes que esperavam sucesso (`PrimeiroCadastroValido`, `ReUpload_SubstituiNaoDuplica`, `ReUploadSemValidade_...`, `ReUploadSemPfxSenha_...`, e os 2 testes de isolamento de tenant que fazem upload pela empresa A/B). Esses testes foram atualizados para usar `genValidPfxBase64(t, senha)` — um certificado de teste real, gerado via `crypto/rsa`+`crypto/x509`+`pkcs12.Modern.Encode`, sempre com a MESMA senha usada no corpo da requisição (já que agora a senha precisa realmente destrancar o arquivo). Nenhuma mudança de comportamento do handler fora do escopo desta spec foi feita — só os dados de teste foram corrigidos para continuar válidos sob a nova validação real.

## Spec Change Log

**Loopback 1 (2026-10-26)** — 2 rodadas de revisão adversarial (Blind Hunter + Edge Case Hunter) sobre a 1ª implementação acharam 1 achado crítico e 9 de robustez. Nenhum exigiu decisão do usuário (todos têm uma única leitura sensata, sem tradeoff de produto/segurança real) — resolvidos diretamente:

1. **CRÍTICO — cadeia de CA intermediária**: `pkcs12.Decode` (confirmado no código vendorizado) rejeita qualquer `.pfx` com CA intermediária junto ("expected exactly two safe bags"), comum em certificados ICP-Brasil reais — isso afetava tanto o normalizador novo quanto `BuildNFSeMTLSClient` (já existente, fora do escopo original desta spec, mas com a MESMA causa raiz). Trocado por `pkcs12.DecodeChain` nos 2 lugares; `BuildNFSeMTLSClient` agora monta a cadeia completa (leaf + intermediárias) no `tls.Certificate`, necessário pro handshake mTLS funcionar de verdade com um certificado real que tenha cadeia. Isso NÃO contradiz o "Never" original (que proíbe a normalização via openssl no caminho de busca, não uma troca de função de decode sem subprocesso).
2. `-legacy` removido do passo de EXPORTAÇÃO (mantido na extração) — sem necessidade, forçava o arquivo PERSISTIDO a usar cifra interna fraca (RC2-40/3DES/SHA-1) em vez do padrão moderno (AES-256/PBKDF2), que `go-pkcs12` já lê sem problema.
3. Timeout de 10s agora é por chamada ao openssl (extração e exportação), não compartilhado entre as duas — o comentário já dizia isso, o código não fazia.
4. Senha com `\n`/`\r` embutido rejeitada explicitamente — `-passin/-passout file:` do openssl lê só a primeira linha, truncando silenciosamente uma senha com quebra de linha no meio.
5. Nova sentinela `ErrNFSeAmbienteTemporarioIndisponivel` para falhas de `os.MkdirTemp`/`os.WriteFile` — antes caíam no mesmo 400 genérico de senha incorreta, escondendo um problema real de disco/permissão.
6. `exec.ErrNotFound` detectado também na execução (não só no `exec.LookPath` prévio) — cobre o binário desaparecer entre a checagem e o uso.
7. Erro de `os.RemoveAll` agora logado em vez de descartado silenciosamente.
8. Decodificação de base64 de `certificado_pfx` unificada (era feita 2x, com um branch de erro morto).
9. Testes que mexiam em `PATH` trocados de `os.Setenv`+defer manual para `t.Setenv`.
10. Novo teste cobrindo o caminho "openssl corrige mas Go ainda não lê o resultado" (usando um openssl fake determinístico, já que reproduzir isso com criptografia real seria frágil).

**Achados "defer" (registrados em `deferred-work.md`):** superfície de ataque do parser nativo do openssl (CVE conhecido, tradeoff inerente à abordagem); pacote `openssl` no Dockerfile não pinado (mesmo padrão de `tzdata`/`ca-certificates` já existentes); sem wipe seguro nem sweep de órfãos para os arquivos temporários com a chave privada em claro; sem limite de concorrência de subprocessos.

## Design Notes

Esta é a primeira vez que o backend Go deste projeto invoca um processo externo (`os/exec`) — até agora toda a lógica era pura Go ou SQL. É uma mudança de superfície de segurança pequena mas real (o processo `openssl` roda com os mesmos privilégios do `appuser`, lê/escreve só em diretório temporário próprio, nunca recebe a senha via argv) — documentar isso aqui serve de referência caso o padrão precise ser revisado ou reaproveitado depois.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: compila sem erro
- `cd backend && go test ./services/... ./handlers/... -run NFSe -v` -- expected: todos os testes passam, incluindo os novos de BER sintético
- `cd backend && go vet ./...` -- expected: sem avisos

**Manual checks (se CLI não cobrir):**
- Confirmar que a imagem Docker reconstruída tem `openssl` disponível (`docker run --rm <imagem> openssl version`). -- **Feito nesta sessão**: build real da imagem + `docker run --rm ... openssl version` (3.5.9) + confirmado suporte a `-legacy` dentro do container.
- Depois do deploy, pedir pro usuário reenviar o certificado real da Ferreira Costa e confirmar que o upload funciona e o "Buscar Agora" ao menos tenta a chamada mTLS (não falha mais no `pkcs12.Decode`). -- pendente, só possível após deploy + reenvio real.

## Suggested Review Order

**O achado crítico: suporte a cadeia de CA (afeta tanto o upload quanto a busca real)**

- `BuildNFSeMTLSClient` monta a cadeia completa (leaf + intermediárias) no handshake mTLS — sem isso, um certificado real com CA intermediária nunca funcionaria, mesmo depois do fix de BER/DER.
  [`nfse_adn.go:127`](../../backend/services/nfse_adn.go#L127)

**O normalizador: nunca confia no openssl sem revalidar**

- `NormalizarPKCS12ParaDER` — caminho rápido sem subprocesso, fallback via openssl só na falha, revalidação obrigatória com `pkcs12.DecodeChain` antes de aceitar.
  [`nfse_pkcs12.go:60`](../../backend/services/nfse_pkcs12.go#L60)

- As 2 sentinelas de erro — a diferença entre "ambiente do servidor com problema" (500) e "senha/arquivo errado" (400) é o que evita confundir o operador com o usuário.
  [`nfse_pkcs12.go:37`](../../backend/services/nfse_pkcs12.go#L37)

**Onde isso entra no fluxo de upload**

- `SaveNFSeCertificadoHandler` — normalização acontece uma vez, no upload; o que fica salvo já é sempre DER.
  [`nfse_credentials.go:127`](../../backend/handlers/nfse_credentials.go#L127)

**Infra**

- `openssl` adicionado à imagem de execução — verificado rodando de verdade (3.5.9, suporte a `-legacy` confirmado).
  [`Dockerfile`](../../backend/Dockerfile)
