---
title: 'RFB: polling de situação e download pelo contrato publicado, sem depender do webhook'
type: 'feature'
created: '2026-10-29'
status: 'done'
baseline_commit: '799ff75b67c784ef9d6bf47c8c166e3160ab1156'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Desde 20/10/2026 a RFB aceita a solicitação (201) mas o webhook nunca chega (Cloudflare descartado; endpoint responde 200 a HEAD/POST), então nenhuma apuração fecha. Além disso o cliente de situação/download não segue o contrato publicado (docs.receitafederal.gov.br/apuracao-cbs): lê `estado` (a RFB publica `situacao`, com `EXPIRADA`) e espera `urlAssinada` na situação (ela vem do endpoint `GET .../v2/download/{tiqueteSolicitacao}`). Com a v2 liberada, o código atual ficaria preso em NOT_READY.

**Approach:** Alinhar o cliente ao contrato publicado e adicionar um poller no scheduler que, para solicitações `requested` sem fonte de download, consulta a situação com backoff dentro de 240 min e, ao `CONCLUIDA`, obtém a `urlAssinada` e segue o fluxo de download existente. O webhook continua como atalho.

## Boundaries & Constraints

**Always:** Webhook, parser dos dados, cota diária e `urlRetorno` enviados como hoje. Migrations existentes intocadas (sem nova coluna: backoff em memória). `GET` na `urlAssinada` sem Bearer. Nunca logar `urlAssinada` nem token. 401/403/404 da situação viram log de diagnóstico explícito, sem alterar a linha nem derrubar o ciclo. Linha preservada (TIMEOUT intacto) quando não há o que baixar.

**Ask First:** Qualquer deploy, troca de `RFB_API_VERSION` ou disparo de "Solicitar" de teste (gasta cota).

**Never:** Alterar frontend, schema, o parser de débitos/créditos ou o envio de `urlRetorno`; fazer push/merge sem aprovação.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Concluída | linha `requested`, situação `CONCLUIDA`, download devolve urlAssinada | persiste url, baixa e processa pelo fluxo existente (débitos ou créditos) | N/A |
| Em andamento | `PENDENTE` ou `EM_PROCESSAMENTO` | só log e reagenda com backoff | N/A |
| ERRO / EXPIRADA | situação com `codigoErro`/`mensagemErro` | linha vira `error` com o código e a mensagem da RFB | log |
| Acesso negado | situação 401/403/404 | log único de diagnóstico por linha, para de consultar essa linha; linha intacta | sem alterar status |
| Download 404 | `ARQUIVO_INDISPONIVEL` após `CONCLUIDA` | reagenda, sem erro na linha | log |
| Contrato legado | resposta com `estado` e `urlAssinada` na situação | continua aceita | N/A |
| Fora da janela | linha com mais de 240 min | poller ignora | `AbortStuck` segue como hoje |

</frozen-after-approval>

## Code Map

- `backend/services/rfb.go` -- `RFBSituacao`, `consultarSituacaoOnce`, `buildURL`/ops; adicionar leitura de `situacao` e `ObterURLDownload`
- `backend/services/rfb_download.go` -- `BaixarArquivoRFB` passo 2 (situação) e `persistURLRenovada`
- `backend/services/rfb_scheduler.go` -- `StartRFBScheduler` (tickers); `AbortStuckRFBRequests`
- `backend/services/rfb_processor.go`, `rfb_creditos_processor.go` -- `ProcessarDownload*`, `updateRequestError`
- `backend/services/rfb_v2_test.go` -- fixtures com `estado`/`urlAssinada` (devem continuar verdes)

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/rfb.go` -- `RFBSituacao` aceita `situacao` e `estado`; `ObterURLDownload` (GET `{seg}/v2/download/{tiquete}`) -- contrato publicado
- [x] `backend/services/rfb_download.go` -- no passo de situação, `CONCLUIDA` obtém a url pelo download (usa a da situação só se vier), trata `EXPIRADA` -- evita travar em NOT_READY
- [x] `backend/services/rfb_poller.go` (novo) -- `PollarSituacaoRFB(db)` com backoff em memória, diagnóstico de 401/403/404, despacho por `tipo` -- fecha o ciclo sem webhook
- [x] `backend/services/rfb_scheduler.go` -- ticker de 2 min chamando o poller -- ativação
- [x] `backend/services/rfb_*_test.go` -- cobrir a matriz com servidor falso -- regressão

**Acceptance Criteria:**
- Given solicitação `requested` há 5 min sem tíquete de download, when situação responde `CONCLUIDA` e o download devolve urlAssinada, then a linha chega a `completed` sem webhook.
- Given o gateway responde 403 à situação, when o poller roda, then um único log de diagnóstico aparece e a linha permanece `requested`.
- Given `go build` e `go test`, then passam, inclusive `rfb_v2_test.go`.

## Spec Change Log

## Design Notes

A situação pode não aceitar tíquetes da rota legada `rtc/v1`; a documentação só publica a v2. Por isso o 404/403 é tratado como resultado de diagnóstico e não como falha: o primeiro ciclo real dirá se o polling funciona na v1 ou só destrava com a v2. O poller usa o ambiente e a versão gravados na linha, como o processador.

## Verification

**Commands:**
- `cd backend && go build -mod=vendor ./... && go test -mod=vendor -count=1 ./services/...` -- expected: ok

## Suggested Review Order

**Poller (ponto de entrada)**

- Um ciclo: lista `requested` sem fonte de download, com relógio por item e `recover()`.
  [`rfb_poller.go:101`](../../backend/services/rfb_poller.go#L101)

- Decide por situação: CONCLUIDA baixa, ERRO/EXPIRADA grava erro, demais só reagendam.
  [`rfb_poller.go:159`](../../backend/services/rfb_poller.go#L159)

- 401/403/404 viram diagnóstico único, sem tocar na linha (decisão de risco, ver Design Notes).
  [`rfb_poller.go:255`](../../backend/services/rfb_poller.go#L255)

- Escritas só com `status='requested'`: o webhook pode concluir a linha durante a consulta.
  [`rfb_poller.go:274`](../../backend/services/rfb_poller.go#L274)

**Contrato publicado da RFB**

- `situacao` (e `estado` legado) normalizados num único ponto.
  [`rfb.go:155`](../../backend/services/rfb.go#L155)

- A urlAssinada vem do endpoint de download, com renovação de token em 401.
  [`rfb.go:643`](../../backend/services/rfb.go#L643)

- No fluxo existente, CONCLUIDA usa o download e o 404 cai no degrau do tíquete.
  [`rfb_download.go:427`](../../backend/services/rfb_download.go#L427)

**Ativação**

- Ticker de 2 min em goroutine própria, ao lado do abortador de travadas.
  [`rfb_scheduler.go:328`](../../backend/services/rfb_scheduler.go#L328)

**Testes**

- Integração do poller: concluída sem webhook, 403, backoff, ERRO, 404, corrida com webhook.
  [`rfb_poller_test.go:171`](../../backend/services/rfb_poller_test.go#L171)

## Design Notes (revisão)

A spec manda desistir da linha no primeiro 401/403/404 da situação. A revisão apontou que um 404 logo após a solicitação pode ser atraso de propagação, e que o estado de desistência vive só em memória (um restart reabilita a linha). Mantido como está porque o 403 real observado é permanente (ACL do gateway) e repetir a consulta só geraria ruído. Se o primeiro ciclo real mostrar 404 em tíquete recém-criado, trocar por 3 falhas consecutivas.
