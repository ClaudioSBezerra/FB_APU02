---
title: 'NFS-e: cliente mTLS + captura via Portal Nacional ADN (um ciclo manual)'
type: 'feature'
created: '2026-10-23'
status: 'done'
review_loop_iteration: 0
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-nfse-captura-plano.md', '{project-root}/_bmad-output/implementation-artifacts/spec-nfse-certificado-schema.md']
baseline_commit: 'd4c8182c969c4fa8c1db8593e873fcf50407b51d'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** o certificado A1 já está armazenado (`nfse_certificados`, sub-spec anterior #6), mas nada consome esse certificado ainda — `nfse_entradas`/`nfse_saidas` continuam vazias. Falta o cliente HTTP que autentica via mTLS no Portal Nacional ADN e grava os documentos capturados.

**Approach:** cliente mTLS + parser + persistência de UM ciclo de busca manual (endpoint "Buscar Agora", sem agendamento automático ainda — isso é a próxima sub-spec). O contrato real da API do ADN **não está totalmente confirmado por fonte primária** (pesquisa 2026-10-23: endpoint confirmado, autenticação/paginação/formato de resposta não). Decisão do usuário: construir mesmo assim, isolando os pontos incertos em pontos únicos de mudança fáceis de ajustar quando houver certificado de teste real — mesmo padrão usado na reconstrução do CGIBS.

## Boundaries & Constraints

**Always:** endpoint confirmado por fonte oficial (manual "APIs ADN Sistema Nacional NFS-e" v1.0, 12/02/2026, gov.br/nfse) — `GET https://adn.nfse.gov.br/contribuintes/DFe/{NSU}` em produção, `https://adn.producaorestrita.nfse.gov.br/contribuintes/DFe/{NSU}` em produção restrita (plural "contribuintes" — forum ACBr confirma 404 no singular); usar `software.sslmate.com/src/go-pkcs12` pra extrair cert+chave do `.pfx` descriptografado (decisão do usuário); montar `tls.Config.Certificates` com esse par por empresa, novo `http.Client` por chamada ou cacheado por `company_id` com invalidação na troca de certificado (mesmo princípio de `rfb.go`); endpoint novo `POST /api/nfse/buscar` (manual, admin-only, mesma role de `/api/nfse/certificado`) que dispara UM ciclo síncrono de busca pra empresa efetiva; parser do payload de resposta isolado em função(ões) própria(s), nunca inline no handler, pra ser o ponto único de ajuste quando o formato real for confirmado; erro de mTLS (certificado rejeitado) e certificado vencido/ausente retornam mensagem clara ao usuário (não um 500 genérico).

**Ask First:** nenhuma — as 2 decisões necessárias (abordagem apesar da incerteza; biblioteca PKCS#12) já foram resolvidas pelo usuário em 2026-10-23 (ver acima).

**Risco técnico a validar quando houver certificado de teste real (não é decisão do usuário, documentar e seguir com o melhor esforço):**
- Autenticação: só há indício indireto de mTLS puro (falha de handshake TLS sem certificado ao tentar acessar o Swagger) — manual oficial não afirma explicitamente, não cita token adicional. Implementar como mTLS puro; se a API também exigir um header/token, isso é um ponto único de mudança no cliente HTTP.
- Paginação/parâmetros: nome exato do parâmetro de CNPJ alternativo, tamanho de lote, limite de registros por chamada — NÃO confirmados. Implementar assumindo o padrão já usado no DF-e da NF-e (maior NSU retornado = próximo lote, repetir até lote vazio), documentado como suposição a confirmar.
- Formato de resposta (JSON vs XML, schema do payload) — NÃO confirmado, sem exemplo de payload disponível. Parser inicial estruturado pra um formato plausível (JSON com XML da NFS-e assinada embutido, por analogia ao DF-e) — marcado explicitamente como PRECISA DE AJUSTE quando houver acesso real.
- Fluxo de habilitação prévia (análogo ao da CGIBS) — não confirmado se existe no ADN. Não implementar habilitação nenhuma nesta sub-spec; se a API retornar erro indicando necessidade de habilitação, documentar o erro cru pro usuário decidir o próximo passo.

**Never:** implementar scheduler/agendamento automático (próxima sub-spec); criar telas de consulta de NFS-e capturada (próxima sub-spec); implementar municípios fora do ADN Nacional (decisão já tomada na spec-mãe); tentar adivinhar/inventar um schema de resposta sem documentar que é suposição.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Busca manual sem certificado cadastrado | Empresa sem `nfse_certificados` | 400, mensagem clara "certificado NFS-e não cadastrado" | `jsonErr(w, 400, ...)` |
| Certificado vencido/revogado | `nfse_certificados.status != 'ativo'` (recalculado) | 400, mensagem indicando certificado vencido, não tenta a chamada mTLS | `jsonErr(w, 400, ...)` |
| mTLS rejeitado pela API ADN | Handshake TLS falha ou API retorna 401/403 | 502, mensagem indicando falha de autenticação junto à Receita, log com detalhe técnico | `sanitizeDBErr`/log |
| Resposta em formato inesperado | Parser não reconhece o payload | 502, mensagem indicando formato de resposta inesperado (não quebra, não grava lixo) | log detalhado pra ajuste do parser |
| Ciclo com documentos novos | API retorna lote com NFS-e | Grava em `nfse_entradas`/`nfse_saidas` conforme CNPJ prestador/tomador, sem duplicar (`UNIQUE(company_id, identificador_nfse)`) | — |

</frozen-after-approval>

## Code Map

- `backend/handlers/nfse_credentials.go` -- schema/criptografia do certificado já existente, consumir via `crypto.DecryptField`
- `backend/migrations/133_nfse_certificados_e_schema.sql` -- schema de `nfse_entradas`/`nfse_saidas`, não mudar, só popular
- `backend/services/rfb.go` -- padrão de cliente HTTP de referência (trocar Bearer por `tls.Config.Certificates`)
- `backend/go.mod` -- adicionar `software.sslmate.com/src/go-pkcs12`
- `backend/main.go` -- registrar `POST /api/nfse/buscar` (novo), role `"admin"` como `/api/nfse/certificado`
- Manual oficial (referência externa, não arquivo do repo): "APIs ADN Sistema Nacional NFS-e" v1.0 — `GET /contribuintes/DFe/{NSU}`, produção e produção restrita

## Tasks & Acceptance

**Execution:**
- [x] `backend/go.mod`/`go.sum` -- adicionada `software.sslmate.com/src/go-pkcs12 v0.7.4` (diff aditivo — `golang.org/x/text` e `lib/pq v1.10.9` pré-existentes no go.sum não foram tocados; `go mod vendor` adicionou `vendor/software.sslmate.com/` e `vendor/golang.org/x/crypto/pbkdf2/`, dependência transitiva do go-pkcs12).
- [x] `backend/services/nfse_adn.go` (novo) -- `DecryptNFSeCertificado` (via `crypto.DecryptField`), `BuildNFSeMTLSClient` (decodifica `.pfx` via `pkcs12.Decode`, monta `http.Client` com `tls.Config.Certificates`; suporta `NFSE_ADN_TLS_CA_FILE` opcional só para fixar uma CA extra de verificação do servidor — usado pelos testes), `ExtractCNPJFromCertificateSubject`, `BuscarDFeADN`/`BuscarTodosDocumentosADN` (`GET /contribuintes/DFe/{NSU}`, produção `https://adn.nfse.gov.br` e produção restrita `https://adn.producaorestrita.nfse.gov.br` via `ResolveNFSeADNBaseURL`), `parseNFSeADNPayload` isolado (comentário documenta a suposição de formato).
- [x] `backend/handlers/nfse_busca.go` (novo) -- `BuscarNFSeHandler`, `POST /api/nfse/buscar`: reaproveita a query de status calculado de `GetNFSeCertificadoHandler`, valida certificado ativo antes de qualquer chamada de rede, chama `services.nfse_adn`, persiste via `persistirDocumentosNFSe` em `nfse_entradas`/`nfse_saidas` por CNPJ prestador/tomador (`ON CONFLICT (company_id, identificador_nfse) DO NOTHING`), retorna `documentos_novos` + `documentos_recebidos`.
- [x] `backend/main.go` -- registrada `POST /api/nfse/buscar` via `withAuth(handlers.BuscarNFSeHandler, "admin")`, mesmo padrão de `/api/nfse/certificado`.
- [x] `backend/services/nfse_adn_test.go`, `backend/handlers/nfse_busca_test.go` -- 17 + 7 testes cobrindo a I/O Matrix com certificado auto-assinado gerado no teste (`httptest.NewUnstartedServer` + `tls.RequireAndVerifyClientCert`); nenhum teste acessa a API real do ADN.

**Acceptance Criteria:**
- Given uma empresa sem certificado cadastrado, when `POST /api/nfse/buscar` é chamado, then retorna 400 sem tentar nenhuma chamada de rede. ✅ Coberto por `TestBuscarNFSeHandler_SemCertificado_Retorna400SemChamadaRede` (aponta `NFSE_ADN_BASE_URL` para uma porta fechada — se o handler tentasse rede antes de checar o certificado, o teste veria 502 em vez de 400, detectando a regressão).
- Given um certificado com `status` calculado como `vencido`, when a busca é chamada, then retorna 400 sem tentar a chamada mTLS. ✅ Coberto por `TestBuscarNFSeHandler_CertificadoVencido_Retorna400SemChamadaRede` (mesma técnica de detecção de chamada de rede indevida).
- Given um servidor de teste mTLS que aceita o certificado de teste e devolve um payload simulado, when a busca roda, then os documentos simulados aparecem em `nfse_entradas`/`nfse_saidas` sem duplicar numa segunda chamada. ✅ Coberto por `TestBuscarNFSeHandler_ServidorAceita_GravaDocumentosSemDuplicarNaSegundaChamada` (1 doc em `nfse_entradas`, 1 em `nfse_saidas`, 2ª chamada devolve `documentos_novos=0` e as contagens não mudam) e, a nível de serviço, por `TestNFSeBuscarTodosDocumentosADN_PaginaAteLoteVazio` (paginação por `ultimoNSU`/`loteVazio`).
- Given um servidor de teste que rejeita o certificado (handshake falha), when a busca roda, then retorna 502 com mensagem clara, sem gravar nada no banco. ✅ Coberto por `TestBuscarNFSeHandler_ServidorRejeitaCertificado_Retorna502SemGravarNada` (502, 0 linhas em `nfse_entradas`/`nfse_saidas`) e, a nível de serviço, por `TestNFSeBuscarDFeADN_CertificadoRejeitado_RetornaErroAutenticacao`/`TestNFSeBuscarDFeADN_HTTP401_RetornaErroAutenticacao`/`TestNFSeBuscarDFeADN_FormatoInesperado_RetornaErroFormato`.

## Design Notes

O contrato real da API ainda não está confirmado (ver Boundaries). O parser e a função de chamada HTTP ficam isolados em `services/nfse_adn.go` especificamente para serem o ponto único de ajuste quando um certificado de teste real permitir inspecionar o Swagger de Produção Restrita (`https://adn.producaorestrita.nfse.gov.br/contribuintes/docs/index.html`) — não espalhar suposições de formato pelo handler ou pelo persistidor.

## Spec Change Log

**Decisão de implementação (2026-10-23, dentro do escopo desta sub-spec, sem loopback)** — a Task 3 ("persiste resultado em `nfse_entradas`/`nfse_saidas` por CNPJ prestador/tomador") pressupõe saber qual é o CNPJ "da própria empresa" para comparar com `cnpjPrestador`/`cnpjTomador` de cada documento. Nem o Code Map nem o Boundaries desta spec apontam de onde tirar esse CNPJ, e a tabela `companies` não tem mais coluna de CNPJ (removida na migration 023 — o projeto modela CNPJs/filiais dinamicamente a partir de documentos já importados, ver `filiais_importadas.go`). Decisão: extrair o CNPJ do próprio certificado mTLS que já precisa estar carregado para a chamada — `ExtractCNPJFromCertificateSubject` lê o Common Name do Subject do certificado X.509, no formato padrão ICP-Brasil para e-CNPJ A1 (`"RAZAO SOCIAL:CNPJ"`). Isso evita inventar schema novo ou alterar a migration 133 (congelada), mas é uma suposição de formato adicional (não confirmada com um certificado A1 real) — documentada em `backend/services/nfse_adn.go` junto da função, mesmo tratamento dado às suposições de payload/paginação.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: compila sem erro
- `cd backend && go test ./services/... ./handlers/... -run NFSe` -- expected: todos os testes passam, usando certificado de teste auto-assinado (sem depender de certificado A1 real)
- `cd frontend && npx tsc -p tsconfig.app.json --noEmit` -- expected: zero erros novos (sempre com `-p tsconfig.app.json`)

**Manual checks (se CLI não cobrir):**
- Nenhuma validação end-to-end contra a API real do ADN é possível nesta sub-spec (sem certificado A1 de teste disponível) — registrar isso como pendência explícita, não como item concluído.

## Suggested Review Order

**Cliente mTLS e os pontos únicos de ajuste (contrato externo não confirmado)**

- Montagem do client mTLS a partir do `.pfx` descriptografado — PKCS#12 via `go-pkcs12`.
  [`nfse_adn.go:124`](../../backend/services/nfse_adn.go#L124)

- Parser do payload e modelo de paginação — toda suposição de formato isolada aqui, marcada "PRECISA DE AJUSTE".
  [`nfse_adn.go:238`](../../backend/services/nfse_adn.go#L238)

- Loop de paginação: timeout de 5min, cancelamento via contexto, erro explícito ao esgotar o limite de lotes sem dado incompleto silencioso.
  [`nfse_adn.go:302`](../../backend/services/nfse_adn.go#L302)

**Tratamento de erro distinto por causa (achado da revisão: antes só existia uma mensagem genérica)**

- 3 ramos de erro (formato/comunicação/autenticação) com mensagem específica — evita dizer "certificado rejeitado" numa falha temporária do governo.
  [`nfse_busca.go:103`](../../backend/handlers/nfse_busca.go#L103)

**Persistência (transação + classificação por CNPJ)**

- Todo o lote grava dentro de uma única transação — sem persistência parcial se um insert falhar no meio.
  [`nfse_busca.go:203`](../../backend/handlers/nfse_busca.go#L203)

- Documento autoemitido (empresa é prestadora e tomadora) grava nos dois lados em vez de perder metade da realidade.
  [`nfse_busca.go:246`](../../backend/handlers/nfse_busca.go#L246)

- Query de INSERT por literais fixos (sem `fmt.Sprintf` no SQL) escolhidos por tipo, não por string.
  [`nfse_busca.go:141`](../../backend/handlers/nfse_busca.go#L141)

**Proteção operacional contra uma API de governo**

- Rate limit de 1 chamada/30s por empresa — mesmo padrão de `LoginRL`/`RegisterRL`.
  [`nfse_busca.go:20`](../../backend/handlers/nfse_busca.go#L20)

- Rota admin-only, mesmo padrão de `/api/nfse/certificado`.
  [`main.go:504`](../../backend/main.go#L504)

**Testes**

- Garante que o loop realmente para (com erro, não sucesso silencioso incompleto) ao esgotar o limite de lotes.
  [`nfse_adn_test.go`](../../backend/services/nfse_adn_test.go) — `TestNFSeBuscarTodosDocumentosADN_NuncaLoteVazio_RetornaErroAoAtingirLimite`
