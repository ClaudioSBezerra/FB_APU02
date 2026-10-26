---
title: 'NFS-e: completar cadeia ICP-Brasil (AIA) no upload + sempre enviar certificado no mTLS'
type: 'bugfix'
created: '2026-10-26'
status: 'done'
review_loop_iteration: 0
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-pkcs12-normalizacao.md']
baseline_commit: 'cc312c66e84bd4de6560f8260c27a3bca0f8a232'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** depois do fix #10, o certificado real normaliza mas o ADN rejeita o handshake (`local error: tls: bad record MAC` no TLS 1.3 = artefato do Go para rejeição do certificado; provado com certificado descartável, TLS 1.2 mostra `handshake failure`). O ADN só lista as raízes ICP-Brasil v5/v10/v11/v12 como CAs aceitas; o `.pfx` real (4 KB) não traz as ACs intermediárias, então o Go não envia certificado nenhum e o servidor não monta o caminho até a raiz. Windows/Java completam a cadeia via AIA; o Go não.

**Approach:** no upload, completar a cadeia seguindo a URL AIA "CA Issuers" de cada certificado até uma raiz ICP-Brasil embutida, validar com `x509.Verify` e persistir o `.pfx` re-encodado já com a cadeia; no cliente mTLS, `GetClientCertificate` sempre envia o certificado.

## Boundaries & Constraints

**Always:** completar a cadeia só no upload (`SaveNFSeCertificadoHandler`, depois de `NormalizarPKCS12ParaDER`), nunca no caminho de busca; raízes ICP-Brasil v5/v10/v11/v12 embutidas via `go:embed` com SHA-256 documentado no código (conferidos em 26/10/2026, ver Design Notes); validação final obrigatória com `x509.Certificate.Verify` contra SÓ essas raízes — a integridade das intermediárias baixadas vem dessa verificação, não da origem; persistir via `pkcs12.Modern.Encode(chave, cert, intermediarias, senha)`; downloads AIA protegidos contra SSRF: só `http`/`https`, bloqueio de IP privado/loopback/link-local/CGNAT/unspecified/multicast checado no IP efetivamente conectado (`net.Dialer.Control` ou `DialContext`, não só pré-resolução), redirect só se o destino também passar no mesmo filtro, timeout 10s por download, resposta máx. 1 MiB, máx. 5 saltos; formatos aceitos na resposta AIA: DER, PEM e PKCS#7 certs-only (.p7b) — DER/PEM pela stdlib, .p7b via `openssl pkcs7 -print_certs` com o mesmo padrão seguro de subprocesso do #10 (arquivo temporário 0600, `defer os.RemoveAll`, timeout, `exec.ErrNotFound` mapeado pra indisponibilidade de ambiente); `BuildNFSeMTLSClient` usa `tls.Config.GetClientCertificate` que sempre devolve o certificado com a cadeia completa; log de erro do ADN com `bad record MAC` explica que no TLS 1.3 isso normalmente é rejeição do certificado pelo servidor.

**Ask First:** nenhuma — abordagem aprovada pelo usuário em 26/10/2026.

**Never:** salvar um certificado cuja cadeia não fecha numa raiz ICP-Brasil embutida; desabilitar o filtro anti-SSRF em produção (o desvio para testes só pode ser alterável de dentro do pacote `services`, nunca por env var/config); checar revogação (CRL/OCSP); mudar o parser de payload do ADN; migrar o certificado já salvo (usuário reenvia).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cadeia já completa no .pfx | intermediárias presentes | Não baixa nada, valida e salva | — |
| Só certificado final, AIA válida | intermediária(s) publicadas na AIA | Baixa, valida, salva .pfx com cadeia | — |
| AIA aponta para IP privado/loopback | URL controlada pelo tenant | Download bloqueado | 400, certificado não pôde ter a cadeia validada |
| Download falha / timeout | AC fora do ar | Upload rejeitado | 502, mensagem "tente novamente" |
| Cadeia não termina em raiz ICP-Brasil | cert de outra PKI ou autoassinado | Upload rejeitado | 400, "certificado não é ICP-Brasil" |
| Certificado/intermediária vencido | `Verify` falha por data | Upload rejeitado | 400, mensagem indicando vencimento |

</frozen-after-approval>

## Code Map

- `backend/services/nfse_pkcs12.go` -- `NormalizarPKCS12ParaDER`, padrão de subprocesso openssl a reaproveitar pro .p7b
- `backend/services/nfse_adn.go` -- `BuildNFSeMTLSClient` (trocar `Certificates` por `GetClientCertificate`), mapeamento de erro com `bad record MAC`
- `backend/handlers/nfse_credentials.go` -- `SaveNFSeCertificadoHandler`, integração depois da normalização, mapeamento 400/502/500
- Raízes oficiais já baixadas e conferidas: `/tmp/claude-1000/-home-claudio-projetos-FB-APU02/6f20dcb4-4ede-451b-92a7-72367c193e87/scratchpad/icproots/v{5,10,11,12}.crt` (DER) -- copiar para `backend/services/icpbrasil_raizes/`

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/icpbrasil_raizes/*.crt` + `backend/services/icpbrasil.go` (novo) -- embute as 4 raízes, expõe pool de raízes confiáveis (variável não exportada substituível só em teste do pacote) e confere o SHA-256 de cada uma no carregamento.
- [x] `backend/services/nfse_cadeia.go` (novo) -- `CompletarCadeiaICPBrasil(cert, intermediarias) ([]*x509.Certificate, error)`: segue AIA com cliente HTTP anti-SSRF, parseia DER/PEM/.p7b, valida com `Verify`, devolve intermediárias na ordem certa; sentinelas distintas para cadeia inválida (400), falha de download (502) e ambiente indisponível (500).
- [x] `backend/handlers/nfse_credentials.go` -- após a normalização, decodificar com `DecodeChain`, completar a cadeia, re-encodar com `pkcs12.Modern.Encode`, persistir; mapear as sentinelas.
- [x] `backend/services/nfse_adn.go` -- `GetClientCertificate` sempre devolvendo o certificado; log com explicação do `bad record MAC`.
- [x] `backend/services/nfse_cadeia_test.go` + ajustes em testes existentes -- cobrir a I/O Matrix com raiz/intermediária/final falsas geradas no teste e AIA servida por `httptest.Server` (desvio anti-SSRF de loopback habilitado só no teste); formatos DER/PEM/.p7b; resposta > 1 MiB; mais de 5 saltos.

**Acceptance Criteria:**
- Given um certificado final sem intermediárias e AIA válida, when enviado no upload, then o `.pfx` persistido decodifica com `DecodeChain` trazendo a cadeia completa até antes da raiz.
  - **Coberto por:** `services.TestCompletarCadeia_SoFinal_BaixaViaAIA_FormatosDERPEMP7B` (DER/PEM/.p7b DER/.p7b PEM servidos por `httptest.Server`), `services.TestCompletarCadeia_DoisNiveis_OrdemCorretaEPfxComCadeia` (2 ACs, ordem final→raiz, re-encode + `DecodeChain`), `services.TestCompletarCadeia_CincoSaltos_Aceita` e `handlers.TestSaveNFSeCertificadoHandler_SoCertificadoFinal_PersistePfxComCadeiaCompleta` (ponta a ponta: lê `certificado_pfx` do banco, `DecodeChain` traz a intermediária e não traz a raiz). No pacote `handlers` a chamada passa pelo seam `completarCadeiaNFSe` com um stub que faz `x509.Verify` real contra uma raiz de teste (ver desvio abaixo).
- Given o cliente mTLS montado a partir desse `.pfx`, when o servidor pede certificado com uma lista de CAs que não casa, then o certificado é enviado mesmo assim.
  - **Coberto por:** `services.TestNFSeBuildNFSeMTLSClient_ListaDeCAsNaoCasa_EnviaCertificadoMesmoAssim` (servidor `RequireAnyClientCert` listando só uma raiz que não é o emissor; conferido que o teste FALHA com o antigo `tls.Config.Certificates` — "tls: certificate required") e `services.TestNFSeBuildNFSeMTLSClient_CadeiaCompletada_ServidorListandoSoARaiz_Conecta` (`RequireAndVerifyClientCert` com `ClientCAs` = só a raiz falsa, .pfx produzido por `CompletarCadeiaICPBrasil` + `pkcs12.Modern.Encode`).
- Given uma AIA apontando para `127.0.0.1`/`169.254.169.254`/rede privada, when o upload roda em produção, then nenhuma conexão é feita a esse destino.
  - **Coberto por:** `services.TestCompletarCadeia_AIALoopbackEmProducao_BloqueadaSemConexao` (desvio de loopback DESLIGADO, AIA aponta pro `httptest.Server` em 127.0.0.1: erro `ErrNFSeCadeiaAIABloqueada`, 0 conexões e 0 requisições contadas via `ConnState`), `services.TestCompletarCadeia_AIADestinosInternos_Bloqueados` (169.254.169.254, 10/8, 192.168/16, 172.16/12, CGNAT, `[::1]`, `[fe80::1]`, 0.0.0.0, `localhost`, ldap/file/gopher — todos bloqueados imediatamente), `services.TestCompletarCadeia_AIARedirectParaDestinoBloqueado`, `services.TestNFSeAIAIPBloqueado_Faixas` e `handlers.TestSaveNFSeCertificadoHandler_ErrosDeCadeia_MapeamentoHTTP` (400 sem vazar URL/IP).
- Given a cadeia já completa, when enviado, then nenhuma requisição AIA é feita.
  - **Coberto por:** `services.TestCompletarCadeia_JaCompleta_NenhumaRequisicaoAIA` (0 requisições e 0 conexões no servidor AIA) e `handlers.TestSaveNFSeCertificadoHandler_CadeiaJaCompleta_NaoBaixaERemoveRaiz` (0 downloads simulados; a raiz enviada no .pfx é descartada).

**Demais linhas da I/O Matrix:** download falha/timeout → `services.TestCompletarCadeia_DownloadFalha_HTTP500EConexaoRecusada` (HTTP 500, conexão recusada, timeout) + mapeamento 502 no teste de handler; não ICP-Brasil → `services.TestCompletarCadeia_OutraPKI_NaoICPBrasil`, `services.TestCompletarCadeia_RaizesReais_CertificadoForaDaICPBrasil_Rejeitado` (com as raízes REAIS embutidas), `services.TestCompletarCadeia_AIAAusente_NaoICPBrasil`, `handlers.TestSaveNFSeCertificadoHandler_CadeiaNaoFecha_Retorna400ENaoSalva`; vencido → `services.TestCompletarCadeia_Vencido` (final, intermediária no .pfx, intermediária baixada). Extras: resposta > 1 MiB (`TestCompletarCadeia_RespostaMaiorQue1MiB_FalhaDownload`), > 5 saltos (`TestCompletarCadeia_MaisDeCincoSaltos_Rejeita`), redirect em loop, resposta lixo, .p7b sem openssl (→ ambiente/500), integridade das raízes embutidas (`TestRaizesICPBrasil_*`), explicação do `bad record MAC` (`TestNFSeExplicarErroHandshakeADN_BadRecordMAC`).

**Desvios de implementação:**
1. Os arquivos em `scratchpad/icproots/*.crt` estavam em PEM (não DER). Os SHA-256 da spec são do DER (fingerprint) e bateram os 4; foram convertidos com `openssl x509 -outform DER` para `backend/services/icpbrasil_raizes/ICP-Brasilv{5,10,11,12}.crt`, cujo `sha256sum` agora é igual ao valor da spec.
2. Testes de `handlers` não conseguem gerar certificado que feche numa raiz ICP-Brasil real (ninguém tem a chave) e o override de raízes é não exportado em `services` (por desenho). Solução: seam `var completarCadeiaNFSe = services.CompletarCadeiaICPBrasil` (não exportada, sem env/config) em `handlers/nfse_credentials.go`; os testes do pacote trocam por um stub que faz `x509.Verify` real contra uma raiz de teste. Helpers `genValidPfxBase64`/`genBERPfxBase64` agora geram certificados que fecham nessa raiz (intermediária dentro do .pfx / assinado pela raiz). `TestSaveNFSeCertificadoHandler_ReUploadSemPfxSenha_PreservaValoresAnteriores` passou a comparar com o valor persistido pelo 1º save (o upload agora persiste o .pfx re-encodado, não os bytes enviados) — mesma garantia de preservação.
3. ~~Classificação: resposta AIA > 1 MiB, HTTP != 200, conteúdo não parseável e loop de redirect → 502~~ — **SUPERADO no loopback 1** (ver Spec Change Log): falhas permanentes de conteúdo/formato da AIA (resposta > 1 MiB, HTTP 4xx, conteúdo inválido, > 10 certificados, excesso de redirects, URL malformada) agora → 400 (`ErrNFSeCadeiaAIAInvalida`); esquema não suportado (ldap/file/ftp/gopher) → 400 (`ErrNFSeCadeiaAIAEsquemaNaoSuportado`); 502 ficou SÓ para rede/timeout/HTTP 5xx/prazo esgotado. Limite de 5 saltos continua 400.
4. Faixas bloqueadas além das pedidas: 0.0.0.0/8, 192.0.0.0/24, 198.18.0.0/15, 240.0.0.0/4, NAT64 64:ff9b::/96 e 6to4 2002::/16 (podem embutir IPv4 interno); `Proxy: nil` no transport (o filtro precisa ver o IP real).

## Spec Change Log

**Loopback 1 (2026-10-26)** — 2 rodadas de revisão adversarial (Blind Hunter + Edge Case Hunter). Nenhum achado exigiu decisão do usuário; 18 patches aplicados direto:
- **SSRF**: portas restritas a 80/443 (URL, redirect e dial); faixas IPv6/IPv4 adicionais bloqueadas (`::/96`, `fec0::/10`, Teredo `2001::/32`, `64:ff9b:1::/48`, `100::/64`, `192.88.99.0/24`, faixas de documentação); pré-filtro ASN.1/OID antes de qualquer fork do openssl; ambiente mínimo (`PATH` só) em todos os subprocessos openssl (inclusive os do #10).
- **Contexto**: `CompletarCadeiaICPBrasil(ctx, ...)` usa `r.Context()`, checa prazo antes de cada salto/URL, openssl também herda o ctx.
- **Validação**: `KeyUsages` = `ClientAuth` (era `Any`); certificado final com `IsCA` rejeitado; vencimento detectado pelas datas do caminho (não mais por texto do erro do Go); raiz detectada por Subject + chave pública (variante re-emitida/cross-signed nunca entra no .pfx).
- **Montagem**: tenta a próxima URL AIA se a resposta não traz o emissor real; aproveita todos os certificados de um bundle; intermediária vencida no .pfx não bloqueia se a AIA tem cópia renovada; percorre emissores cruzados; com vários caminhos, escolhe a raiz de validade mais longa.
- **Parsing**: DER com espaço no fim, base64 puro, PEM `X509 CERTIFICATE`/`TRUSTED CERTIFICATE`, PEM misto CERTIFICATE+PKCS7; AIA https confia no pool do sistema + raízes ICP-Brasil.
- **Erros**: falhas permanentes da AIA → 400 acionável; 502 só pra falha transitória; `ldap://` → "esquema não suportado".
- **Logs**: `%q` em URL/host/stderr do openssl (anti log-injection); `bad record MAC` explicado como "causa provável".
- **Testes do handler**: um teste com a função REAL (sem stub); stub passou a exigir emissor real como a produção.

**Achados "defer"** (em `deferred-work.md`): limite de concorrência no upload; validade exibida não considera a cadeia (raiz v5 vence 02/03/2029); certificados antigos não são reprocessados; IP público do próprio host não bloqueado (mitigado pela restrição de portas); ganchos de teste globais sem sincronização.

## Design Notes

SHA-256 das raízes (baixadas de `acraiz.icpbrasil.gov.br/credenciadas/RAIZ/ICP-Brasilv{N}.crt` por http e https, cópias idênticas): v5 `CAA53FC6091C6951887C976E378F6EF89AA6377C55D97B6475422B71ED7E9B17` (vence 2029-03-02); v10 `6E0BFF069A26994C15DE2C4888CC54AF84882E5495B7FBF66BE9CCFFEC7489F6` (2032-07-01); v11 `1406710058180FA4081AAB3F246F1702429C552A11FA3143B84C88CB3AB8E5E7` (2032-07-01); v12 `D8478E37CE19C690CF657381E68FE600E4E1A042536830F06847E03E554C4B01` (2037-10-22). O `https` do site do ITI não valida na cadeia padrão do sistema (curl falha sem `-k`) — mais um motivo pra AIA aceitar `http` (as ACs ICP-Brasil publicam em http; a integridade vem do `Verify`).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erro
- `cd backend && go test ./services/... ./handlers/...` -- expected: tudo passa

**Manual checks (se CLI não cobrir):**
- Depois do deploy, usuário reenvia o certificado real e clica "Buscar Agora"; o erro `bad record MAC` deve sumir (podendo aparecer o próximo problema real, ex: formato do payload, que é pendência conhecida).

## Suggested Review Order

**Segurança: o filtro anti-SSRF (as URLs AIA vêm do certificado enviado pelo cliente)**

- IP checado no momento exato da conexão — cobre DNS rebinding, redirects e porta.
  [`nfse_cadeia.go:177`](../../backend/services/nfse_cadeia.go#L177)

- Quais faixas são bloqueadas (privado, loopback, link-local/metadata, CGNAT, IPv6 de transição).
  [`nfse_cadeia.go:146`](../../backend/services/nfse_cadeia.go#L146)

**A completação da cadeia**

- Entrada: segue AIA até uma raiz ICP-Brasil embutida, valida com `Verify` (ClientAuth), nunca persiste nada não verificado.
  [`nfse_cadeia.go:265`](../../backend/services/nfse_cadeia.go#L265)

- Com vários caminhos possíveis, escolhe a raiz de validade mais longa.
  [`nfse_cadeia.go:592`](../../backend/services/nfse_cadeia.go#L592)

- Raízes embutidas com SHA-256 conferido no carregamento.
  [`icpbrasil.go:24`](../../backend/services/icpbrasil.go#L24)

**Onde entra no fluxo**

- Upload: normaliza (#10) → completa a cadeia → re-encoda e salva.
  [`nfse_credentials.go:288`](../../backend/handlers/nfse_credentials.go#L288)

- Cliente mTLS: `GetClientCertificate` sempre envia o certificado com a cadeia.
  [`nfse_adn.go:128`](../../backend/services/nfse_adn.go#L128)
