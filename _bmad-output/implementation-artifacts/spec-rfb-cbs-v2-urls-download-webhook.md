---
title: 'RFB v2: URLs, download (urlAssinada) e webhook'
type: 'feature'
created: '2026-09-24'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'f04b768152956a8b798144acda3f54173874a23c'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-rfb-cbs-v2-migracao.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** o parser v2 (commit `f04b768`) está dormente porque `rfb.go` só fala v1: monta URLs `{prefix}/apuracao-cbs|creditos-cbs/v1/{cnpj}`, lê `tiquete` na resposta, baixa via `GET /download/v1/{tiquete}` e o webhook só entende `tiqueteSolicitacao`+`tiqueteDownload` (descarta o resto, inclusive erros). A API v2 usa `apuracao-cbs[-prr]/v2/{debitos|creditos}/{cnpj}`, responde `tiqueteSolicitacao`, entrega o arquivo só por `urlAssinada` no webhook (ou via `GET .../v2/situacao/{tiquete}`) e notifica falha com `codigoErro`/`mensagemErro`.

**Approach:** cliente RFB fala v1 e v2 (versão escolhida por configuração e gravada por solicitação); download tem fallback em camadas (`urlAssinada` → `situacao` → download por tíquete v1); webhook aceita os dois formatos e trata erro; limite diário de débitos sobe de 2 para 4.

## Boundaries & Constraints

**Always:**
- **Default seguro, zero mudança no deploy:** `RFB_API_VERSION` (default `v1`) e `RFB_API_VERSION_RESTRITA` (default = `RFB_API_VERSION`) escolhem a versão por ambiente; virar para v2 é só trocar env, sem novo deploy.
- A versão usada é gravada em `rfb_requests.api_versao` no INSERT da solicitação (default `'v1'` para linhas antigas) e o download/reprocesso usa a versão **da solicitação**, não a config atual (mesmo princípio já aplicado ao `ambiente`).
- Prefixo v2: `ambiente='producao_restrita'` → `apuracao-cbs-prr`, demais → `apuracao-cbs`. Resposta da solicitação: aceitar `tiquete` (v1) ou `tiqueteSolicitacao` (v2).
- Ordem do download: `url_assinada` não expirada → GET **sem** `Authorization` (URL pré-assinada; Bearer pode invalidar a assinatura) → se ausente/expirada/403 e versão v2 → `GET .../v2/situacao/{tiquete}` (com Bearer) para obter `urlAssinada` nova (`PENDENTE`/`EM_PROCESSAMENTO` = ainda não pronto, erro recuperável; `ERRO` = grava `codigoErro`/`mensagemErro`) → se houver `tiquete_download`, `DownloadArquivo` v1.
- **SSRF:** o webhook é público; `urlAssinada` só é aceita se `https` e o host não resolver para IP loopback/privado/link-local (checar no dial). Nunca logar a URL assinada completa (só o host); zerar `url_assinada` após `completed`.
- Webhook de erro (`codigoErro`/`mensagemErro`): com `tiqueteSolicitacao` correlacionável → `status='error'`, `error_code` (truncado a 50), `error_message`; sem tíquete → log de aviso com o corpo (a doc não garante o tíquete no erro) e 200. Preservar HMAC, HEAD, idempotência e o claim atômico existentes.
- Limite diário de débitos: `LimiteDebitosDiaRFB = 4` (`LimiteDebitosDiaAgendamento` segue 1); atualizar comentários/testes que assumam 2.
- Migration aditiva `130` em `rfb_requests`: `api_versao VARCHAR(4) NOT NULL DEFAULT 'v1'`, `url_assinada TEXT`, `url_assinada_expira_em TIMESTAMPTZ`.

**Never:** tocar em parser/schema de débitos/créditos, frontend, pagamentos/recolhimentos/DARF; adicionar limite diário novo para créditos (hoje não há; fora de escopo); apagar o caminho v1.

</frozen-after-approval>

## Code Map

- `backend/services/rfb.go` -- `rfbAPIVersion` (L20), `SetAmbiente` (L113), `SolicitarApuracao` (L219), `SolicitarCredito` (L297), `DownloadArquivo` (L357), `RFBApuracaoResponse` (L78)
- `backend/services/rfb_scheduler.go` -- INSERTs de `rfb_requests` (L132-159, L216-254) gravar `api_versao`; limites (L66-69)
- `backend/services/rfb_processor.go:512-577`, `rfb_creditos_processor.go:~265-325` -- passo de download → helper compartilhado com as camadas de fallback
- `backend/handlers/rfb_apuracao.go` -- `RFBWebhookHandler` (L580-716), Ressolicitar reset (L381: limpar também `url_assinada*`)
- `backend/handlers/rfb_creditos.go:~180` -- recusa download manual sem `tiquete_download`; passar a aceitar `url_assinada` ou v2 (fallback `situacao` só precisa do `tiquete`)
- `backend/migrations/130_rfb_requests_v2.sql` (novo)

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/130_rfb_requests_v2.sql` -- 3 colunas acima com `IF NOT EXISTS`.
- [x] `backend/services/rfb.go` -- versão por env/ambiente + função única de montagem de URL (v1/v2, solicitar/situacao/download); `RFBApuracaoResponse` aceita as duas chaves de tíquete; `ConsultarSituacao`; `DownloadURLAssinada` (sem Bearer, https, bloqueio de IP privado no dialer, timeout 15 min).
- [x] `backend/services/rfb_scheduler.go` -- gravar `api_versao` nos INSERTs; `LimiteDebitosDiaRFB=4` + comentários.
- [x] `backend/services/rfb_processor.go` + `rfb_creditos_processor.go` -- ler `api_versao`/`url_assinada*`; helper único de download com as camadas de fallback e erros tipados (`NOT_READY`, `URL_EXPIRADA`, `DOWNLOAD_ERROR`); limpar `url_assinada` ao concluir.
- [x] `backend/handlers/rfb_apuracao.go` -- webhook: aceitar `urlAssinada`/`urlAssinadaExpiraEm` e `tiqueteDownload`; ramo de erro; validação da URL; persistir e despachar processor; Ressolicitar limpa `url_assinada*`.
- [x] `backend/handlers/rfb_creditos.go` -- relaxar a checagem de `tiquete_download`.
- [x] Testes (`openTestDB`, `httptest`, padrão do pacote): URL v1/v2 por ambiente; solicitação com resposta `tiquete` e `tiqueteSolicitacao`; download por `urlAssinada` sem Bearer; expirada → `situacao` → CONCLUIDA; `situacao` PENDENTE/ERRO; fallback v1; webhook v1, v2, erro com e sem tíquete, `http://` e IP privado rejeitados; limite 4/dia; Ressolicitar limpa a URL.

**Acceptance Criteria:**
- Given nenhuma env de versão, when o backend sobe e solicita apuração, then chama exatamente as URLs v1 de hoje e grava `api_versao='v1'`.
- Given `RFB_API_VERSION_RESTRITA=v2` e ambiente restrita, when solicita, then POST em `.../apuracao-cbs-prr/v2/debitos/{cnpj8}` (créditos: `.../v2/creditos/...`) e a request fica `api_versao='v2'`.
- Given webhook v2 com `urlAssinada`, when processado, then o arquivo é baixado sem `Authorization`, o parser v2 roda e `url_assinada` fica NULL ao concluir.
- Given webhook com `codigoErro`, when o tíquete casa uma request em `requested`, then ela vira `error` com código/mensagem da RFB.
- Given 4 solicitações de débito hoje, when a 5ª é pedida, then `DAILY_LIMIT`.

### Patch round (revisão adversarial, 2026-09-24)

2 revisores independentes; todos os achados triados como patch (sem loopback). Aplicados: limite diário **por versão** (v1=2, v2=4 — a v1 documentava 2; subir para 4 antes de ativar v2 causaria 429 reais; corrige também `todayCount >= 2` fixo no botão manual de débitos); `urlAssinada` só para linhas `api_versao='v2'`; URL inválida não descarta `tiqueteDownload` válido (senão `URL_INVALIDA`); guarda de status + `RowsAffected` no UPDATE do webhook; expiração ilegível → TTL conservador sem sobrescrever com NULL; truncamento UTF-8-safe; logs sem headers e com URLs redigidas; `LimitReader` (1 GiB arquivo / 1 MiB APIs); qualquer falha da URL persistida cai para `situacao` → `tiquete_download`; URL renovada persistida antes do download; retry de 401 na `situacao`; SSRF ampliado (NAT64, 6to4, Teredo, faixas de documentação, porta 443, allow-list opcional `RFB_URL_ASSINADA_HOSTS`, aviso sem HMAC); CHECK em `api_versao`. Adiados: ver `deferred-work.md` (seção 2026-09-24, urls-download-webhook).

## Design Notes

Webhook de erro sem `tiqueteSolicitacao` não é correlacionável — o watchdog de 5h (`AbortStuckRFBRequests`) continua sendo a rede de segurança. `situacao` também serve de rede de segurança para webhook perdido, mas polling ativo fica fora desta spec.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test -count=1 ./services/... ./handlers/...`
- Rodar a migration 130 no Postgres local 2x (idempotência).

**Manual checks:**
- Antes do corte: setar `RFB_API_VERSION_RESTRITA=v2` no ambiente restrito e confirmar um ciclo completo (solicitar → webhook → download → resumo) antes de virar produção.
