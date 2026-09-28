---
title: 'CGIBS: Nova Solicitação/Cancelamento + reescrever handlers + dropar tabelas legadas'
type: 'feature'
created: '2026-09-28'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'c85e03b9badeab069af483326ce0746275be4115'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-cgibs-conta-corrente-plano.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `cgibs_apuracao.go` (4 handlers) e `limpeza_total.go` ainda leem/escrevem `cgibs_requests`/`cgibs_resumo`/`cgibs_debitos` — por isso as 2 sub-specs anteriores não puderam dropar essas tabelas (loopback documentado em `spec-cgibs-schema.md`). `SolicitarCGIBSApuracaoHandler` ainda devolve 503 fixo, nunca chama a API de verdade.

**Approach:** cliente HTTP ganha `NovaSolicitacao()`/`CancelarSolicitacao()` (mesmo padrão de `Habilitar()`/`ObterArquivo()`: `ClientID`+`ClientSecret` no corpo, URL via env sem fallback); os 4 handlers de `cgibs_apuracao.go` migram de `cgibs_requests`/`cgibs_resumo` para `cgibs_solicitacoes` (+ 2 endpoints novos de leitura do extrato, já que o modelo não tem mais "resumo por request" — é ledger por operação); `limpeza_total.go` troca os 3 nomes antigos pelos novos; SÓ DEPOIS disso, migration `132` dropa `cgibs_requests`/`cgibs_debitos`/`cgibs_resumo`. Sem frontend nesta sub-spec (próxima).

## Boundaries & Constraints

**Always:**
- `CGIBS_NOVA_SOLICITACAO_URL`/`CGIBS_CANCELAMENTO_URL` (env, sem fallback), mesmo padrão de erro tipado das outras chamadas já implementadas.
- `NovaSolicitacao()`: corpo `{ClientID, ClientSecret, CNPJ, DataTransacaoIni, DataTransacaoFim}`; resposta `{TokenContrib, TipoSolicitacao, SituacaoSolicitacao, IDSolicitacao, DataTransacaoIni, DataTransacaoFim, Resultado}` — grava/atualiza `cgibs_solicitacoes` com `id_solicitacao_externo`/`situacao_solicitacao` retornados.
- `CancelarSolicitacao()`: corpo `{ClientID, ClientSecret, IDSolicitacao}`; resposta `{Resultado}` — só permite cancelar solicitação em `situacao_solicitacao='solicitada'` (ainda não gerada/enviada, conforme MOC).
- `StatusCGIBSApuracaoHandler` lista `cgibs_solicitacoes` (não mais `cgibs_requests`) — sem "resumo por solicitação" embutido (não existe mais esse conceito no modelo de conta corrente).
- 2 endpoints novos: `GET /api/cgibs/operacoes` (lista `cgibs_operacoes` da empresa, paginado) e `GET /api/cgibs/operacoes/{id}/lancamentos` (extrato de uma operação) — é a "apuração de verdade" agora, live via SQL sobre o ledger, nunca cacheado (mesmo princípio já fixado na sub-spec de schema).
- `limpeza_total.go`: troca `cgibs_debitos, cgibs_requests, cgibs_resumo` por `cgibs_lancamentos, cgibs_operacoes, cgibs_arquivos, cgibs_solicitacoes` (ordem de FK) — NÃO incluir `cgibs_credentials` (dado de configuração, não transacional, mesmo padrão de `rfb_credentials` fora dessa lista).
- Migration `132`: `DROP TABLE cgibs_resumo, cgibs_debitos, cgibs_requests` — só depois de confirmar (grep) que nenhum `.go` restante referencia esses 3 nomes.
- Handlers exigem `habilitado=true` na credencial antes de chamar `NovaSolicitacao`/`CancelarSolicitacao` (mesma checagem já usada no resto do CGIBS).

**Never:** implementar Listagem de Solicitações (5.4) como endpoint chamado de verdade nesta rodada, a menos que sobre tempo — é o endpoint de menor prioridade (só serve pra reconciliação externa, não pro fluxo principal); frontend (próxima sub-spec); mudar o schema além do DROP das 3 tabelas legadas.

</frozen-after-approval>

## Code Map

- `backend/services/cgibs.go` -- adicionar `NovaSolicitacao()`, `CancelarSolicitacao()`
- `backend/handlers/cgibs_apuracao.go` -- reescrever os 4 handlers (structs `CGIBSRequest`/`CGIBSResumo` saem, novo shape baseado em `cgibs_solicitacoes`)
- `backend/handlers/cgibs_operacoes.go` (novo) -- os 2 endpoints de leitura do ledger
- `backend/handlers/limpeza_total.go:26` -- lista de tabelas
- `backend/migrations/132_cgibs_drop_legado.sql` (novo)
- `backend/main.go` -- registrar as 2 rotas novas

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/cgibs.go` -- `NovaSolicitacao(clientID, clientSecret, cnpj string, dataIni, dataFim time.Time) (CGIBSNovaSolicitacaoResp, error)`, `CancelarSolicitacao(clientID, clientSecret string, idSolicitacao int64) (resultado string, err error)`.
- [x] `backend/handlers/cgibs_apuracao.go` -- `StatusCGIBSApuracaoHandler` lista `cgibs_solicitacoes`; `SolicitarCGIBSApuracaoHandler` chama `NovaSolicitacao` de verdade e persiste a `cgibs_solicitacoes` criada; `ClearErrorsCGIBSHandler`/`DetalheCGIBSHandler` operam sobre `cgibs_solicitacoes` (Detalhe com DELETE deve chamar `CancelarSolicitacao` antes de apagar local, se a situação ainda permitir cancelamento externo).
- [x] `backend/handlers/cgibs_operacoes.go` -- os 2 GETs (paginação simples por `company_id`+`created_at`/`chave_acesso`).
- [x] `backend/handlers/limpeza_total.go` -- trocar os 3 nomes pelos 4 novos.
- [x] `backend/migrations/132_cgibs_drop_legado.sql` -- os 3 DROP, só depois de confirmar via grep que nada mais referencia esses nomes.
- [x] `backend/main.go` -- registrar `/api/cgibs/operacoes` e `/api/cgibs/operacoes/{id}/lancamentos`.
- [x] Testes (padrão do pacote): `NovaSolicitacao`/`CancelarSolicitacao` sem env configurada e com fake server; `SolicitarCGIBSApuracaoHandler` sem 503, cria `cgibs_solicitacoes` de verdade; `StatusCGIBSApuracaoHandler` não referencia mais `cgibs_requests`; os 2 GETs novos retornam dado correto e escopado por `company_id`; `limpeza_total` (se tiver teste hoje) continua passando com os nomes novos.

**Acceptance Criteria:**
- Given `grep -rn "cgibs_requests\|cgibs_debitos\|cgibs_resumo" backend/handlers backend/services`, when rodado após esta sub-spec, then não retorna nenhuma ocorrência em código Go (só em migrations antigas, que não se alteram).
- Given a migration 132 aplicada, when inspecionado, then as 3 tabelas não existem mais e nada quebra no boot.
- Given uma empresa com credencial `habilitado=false`, when tenta `SolicitarCGIBSApuracaoHandler`, then recebe erro claro (não tenta a chamada externa).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test -count=1 ./services/... ./handlers/... -run CGIBS`
- `grep -rn "cgibs_requests\|cgibs_debitos\|cgibs_resumo" backend/handlers backend/services` -- deve vir vazio antes de aplicar a migration 132.
