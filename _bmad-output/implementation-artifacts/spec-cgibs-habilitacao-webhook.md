---
title: 'CGIBS: Habilitação do Contribuinte + webhook receptor'
type: 'feature'
created: '2026-09-28'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'b7d04851ee0fd3e0f4cd9d8f8be7087d21b5b339'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-cgibs-conta-corrente-plano.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** o schema novo (sub-spec anterior) não tem nada que o popule — falta o primeiro passo real de integração: registrar o contribuinte na CGIBS (Habilitação) e receber as notificações de arquivo disponível (webhook). Sem URL base/autenticação confirmadas pela CGIBS (seções do manual não extraíveis — imagens), o cliente é construído configurável por env var, sem valor real ainda.

**Approach:** `backend/services/cgibs.go` com `Habilitar()` (POST configurável via env, `ClientID`+`ClientSecret`+`QueroHabilitar`+`WebhookURL`+`TokenContrib` no corpo — inferido dos parâmetros documentados em todo endpoint da API, não é OAuth2 Bearer como a RFB); endpoint `POST /api/cgibs/credentials/habilitar` (admin aciona, persiste resultado); `POST /api/cgibs/webhook` (público, recebe notificação de arquivo, valida `TokenContrib`, faz upsert em `cgibs_solicitacoes`/`cgibs_arquivos`). Esta sub-spec NÃO baixa arquivo (isso é a próxima).

## Boundaries & Constraints

**Always:**
- URL da Habilitação via `CGIBS_HABILITACAO_URL` (env, sem default/fallback — não temos valor real). Se vazia, `Habilitar()` retorna erro tipado claro (`CGIBS_NAO_CONFIGURADO`), sem tentar rede.
- `WebhookURL` enviada na Habilitação vem de `CGIBS_WEBHOOK_URL` (env, mesmo padrão de `RFB_WEBHOOK_URL`) — não é campo que o admin digita por empresa.
- `TokenContrib` é **sempre gerado automaticamente** (`crypto/rand`, nunca fica vazio) — evita reproduzir o problema real que achamos com `RFB_WEBHOOK_SECRET` vazio em produção (webhook sem nenhuma validação).
- `client_secret`/`token_contrib` em `cgibs_credentials` usam o mesmo helper de criptografia já usado por `rfb_credentials` (`backend/crypto`) — confirmar se `cgibs_credentials.client_secret` já usa esse padrão hoje; se não, corrigir junto (mesma sensibilidade que credenciais RFB).
- Webhook público (`/api/cgibs/webhook`, sem JWT) identifica a empresa por `CNPJ` (8 dígitos) batendo com `cgibs_credentials.cnpj_matriz` **e** valida `TokenContrib` daquela credencial especificamente — sem os dois, rejeita.
- Upsert de `cgibs_solicitacoes` por `(company_id, id_solicitacao_externo)` — uma solicitação muda de situação ao longo do tempo (solicitada→gerada→enviada), o webhook pode chegar mais de uma vez pra mesma `IDSolicitacao`.
- Upsert de `cgibs_arquivos` por `(solicitacao_id, numero_sequencial)`.
- Esta sub-spec só persiste — não dispara download (isso é `spec-cgibs-obter-arquivo-parser.md`, próxima).

**Never:** implementar o download/parser do arquivo; mexer no botão "Solicitar Apuração" existente (fica pra sub-spec 4, que também cobre Nova Solicitação); assumir OAuth2 Bearer (não há evidência disso na doc, ao contrário — todo endpoint lista ClientID/ClientSecret como parâmetro do corpo).

</frozen-after-approval>

## Code Map

- `backend/services/cgibs.go` (novo) -- `Habilitar()`, tipos de request/response
- `backend/handlers/cgibs_credentials.go` -- endpoint novo de habilitar; padrão de criptografia a confirmar
- `backend/handlers/cgibs_webhook.go` (novo) -- `CGIBSWebhookHandler`
- `backend/main.go` -- registrar `POST /api/cgibs/webhook` (público) e `POST /api/cgibs/credentials/habilitar`
- `backend/services/rfb.go`, `backend/handlers/rfb_apuracao.go` (`RFBWebhookHandler`) -- padrão de código de referência (não copiar auth/schema, só estrutura: client HTTP, handler de webhook público)
- `backend/migrations/131_cgibs_conta_corrente_fiscal.sql` -- schema já criado na sub-spec anterior (não alterar)

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/cgibs.go` -- `Habilitar(clientID, clientSecret, webhookURL, tokenContrib string) (habilitado string, dataHabilitacao time.Time, err error)`, lendo `CGIBS_HABILITACAO_URL`; erro tipado se env vazia; JSON no corpo com os 5 campos.
- [x] `backend/handlers/cgibs_credentials.go` -- `POST /api/cgibs/credentials/habilitar`: carrega credencial da empresa, gera `token_contrib` (se ainda não tiver), chama `Habilitar()` com `CGIBS_WEBHOOK_URL`, persiste `habilitado`/`data_habilitacao`/`webhook_url`/`token_contrib`; confirmar/aplicar criptografia de `client_secret`/`token_contrib`.
- [x] `backend/handlers/cgibs_webhook.go` -- `CGIBSWebhookHandler`: parse do payload (`TipoSolicitacao`, `SituacaoSolicitacao`, `CNPJ`, `IDSolicitacao`, `DataSolicitacao`, `DataTransacaoIni/Fim`, `QtdOperacoes`, `QtdArqVinculados`, `DataValidadeSolicitacao`, `Arquivos[]`); identifica empresa por CNPJ+TokenContrib; upsert `cgibs_solicitacoes` + `cgibs_arquivos` (status `pendente`).
- [x] `backend/main.go` -- registrar as 2 rotas.
- [x] Testes (padrão do pacote, `openTestDB`, fake `httptest`): `Habilitar()` sem env configurada retorna erro tipado sem chamar rede; `Habilitar()` com fake server monta o corpo certo; webhook com CNPJ+token corretos faz upsert; webhook com token errado rejeita; webhook chamado 2x pra mesma `IDSolicitacao` atualiza (não duplica) a solicitação; `Arquivos[]` viram linhas em `cgibs_arquivos` sem duplicar em reenvio.

**Acceptance Criteria:**
- Given `CGIBS_HABILITACAO_URL` não configurada, when `/api/cgibs/credentials/habilitar` é chamado, then retorna erro claro sem tentar rede.
- Given um webhook válido com 2 arquivos, when processado, then existe 1 `cgibs_solicitacoes` e 2 `cgibs_arquivos` com status `pendente`.
- Given o mesmo webhook reenviado, when processado de novo, then não duplica linhas (upsert).
- Given `TokenContrib` incorreto, when o webhook chega, then é rejeitado e nada é gravado.

## Design Notes

Autenticação inferida (risco documentado na spec-mãe): todo endpoint especificado no manual lista `ClientID`+`ClientSecret` como parâmetros obrigatórios do próprio corpo/tabela de parâmetros — diferente da RFB (token Bearer obtido uma vez). Se a CGIBS na prática usar Bearer por cima disso, ajustar `Habilitar()` é uma mudança localizada (a função já isola a montagem do request).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test -count=1 ./services/... ./handlers/... -run CGIBS`
