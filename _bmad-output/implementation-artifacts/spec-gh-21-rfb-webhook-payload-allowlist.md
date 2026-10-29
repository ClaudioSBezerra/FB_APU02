---
title: 'RFB webhook: aceitar o payload de sucesso documentado e allowlist opcional de IP'
type: 'feature'
created: '2026-10-29'
status: 'done'
baseline_commit: '203e19b50a5730970c86a8b9d0d18b9e3d718ad4'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O guia oficial da RFB define o aviso de sucesso como `tiqueteSolicitacao` + `situacao` (`CONCLUIDA`), com o download obtido depois no endpoint de download. `RFBWebhookHandler` só aceita sucesso com `tiqueteDownload` ou `urlAssinada`; com o payload documentado responde 200 e descarta o aviso. Além disso o endpoint público não tem a allowlist de IP que o guia recomenda (produção: 189.9.61.23/32).

**Approach:** Tratar `situacao=CONCLUIDA` sem dados de download como sucesso e disparar o download pelo fluxo existente. Adicionar allowlist opcional de IP/CIDR (`RFB_WEBHOOK_ALLOWED_IPS`), aplicada só ao POST, calculando o IP real do cliente atrás de Cloudflare/nginx.

## Boundaries & Constraints

**Always:** Payload legado (`tiqueteDownload`/`urlAssinada`) e o ramo de erro seguem como hoje. `HEAD` responde 200 a qualquer origem. Variável vazia = sem restrição. Resposta 200 rápida, download assíncrono. Migrations existentes intocadas.

**Ask First:** Qualquer deploy, e definir `RFB_WEBHOOK_ALLOWED_IPS` em algum servidor.

**Never:** Configurar `RFB_WEBHOOK_SECRET` (a RFB não assina). Alterar o parser de débitos/créditos, `GetClientIP` ou o rate limit do login. Fazer push/merge sem aprovação.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Sucesso documentado, linha v2 | `{tiqueteSolicitacao, situacao:"CONCLUIDA"}`, linha `requested` v2 | status `webhook_received`, dispara download; 200 `processing` | N/A |
| Sucesso documentado, linha v1 | mesmo payload, linha v1 sem fonte de download | linha INALTERADA; 200 com aviso | log |
| Não final | `situacao` PENDENTE/EM_PROCESSAMENTO, sem download | só log; 200 com aviso | N/A |
| Erro por situação | `situacao` ERRO/EXPIRADA (com ou sem `codigoErro`) | ramo de erro existente; código `EXPIRADA`/`RFB_ERRO` se ausente | N/A |
| Legado | `tiqueteDownload` ou `urlAssinada` | exatamente como hoje | N/A |
| Allowlist: dentro | POST de IP/CIDR listado | processa normalmente | N/A |
| Allowlist: fora | POST de IP não listado | 403, nada é lido nem gravado | log do IP |
| Allowlist: HEAD | HEAD de qualquer IP | 200 | N/A |
| Allowlist inválida | variável preenchida, nenhuma entrada válida | 403 em todo POST (fail-closed) | log de erro |

</frozen-after-approval>

## Code Map

- `backend/handlers/rfb_apuracao.go` -- `RFBWebhookHandler` (~617): HEAD, assinatura, ramo de erro, validação de campos (~744), claim e dispatch
- `backend/handlers/rfb_webhook_ip.go` (novo) -- parse da allowlist e IP real do cliente
- `backend/handlers/rfb_v2_webhook_test.go` -- helpers (`postWebhook`, `insertRFBReq`, `readReqRow`) e casos existentes que devem seguir verdes
- `installer/cliente-aws/docker-compose.yml` -- repasse de env do serviço `api` (~33)
- `frontend/nginx.conf` -- só leitura: acrescenta o IP do proxy ao `X-Forwarded-For`

## Tasks & Acceptance

**Execution:**
- [x] `backend/handlers/rfb_webhook_ip.go` -- `webhookClientIP(r)`: entrada mais à direita do `X-Forwarded-For` que não seja loopback/privada/CGNAT; senão `X-Real-IP` público; senão `RemoteAddr`. `rfbWebhookIPPermitido(r)`: lê `RFB_WEBHOOK_ALLOWED_IPS` a cada chamada (IP ou CIDR) -- IP real atrás do proxy, sem confiar no cabeçalho do cliente
- [x] `backend/handlers/rfb_apuracao.go` -- checar a allowlist após o HEAD e antes de ler o corpo; tratar `situacao` (CONCLUIDA sem download = sucesso, só em linha v2; ERRO/EXPIRADA = erro; demais só log) -- cumprir o guia
- [x] `installer/cliente-aws/docker-compose.yml` -- `RFB_WEBHOOK_ALLOWED_IPS=${RFB_WEBHOOK_ALLOWED_IPS:-}` -- ativável pelo `.env`
- [x] `backend/handlers/rfb_webhook_ip_test.go` e `rfb_v2_webhook_test.go` -- cobrir a matriz -- regressão

**Acceptance Criteria:**
- Given linha v2 `requested`, when chega `{tiqueteSolicitacao, situacao:"CONCLUIDA"}`, then a linha vai a `webhook_received` e o download é disparado.
- Given allowlist `189.9.61.23/32` e `X-Forwarded-For: 189.9.61.23, 10.0.9.6`, when POST, then 200; com `X-Forwarded-For: 1.2.3.4, 10.0.9.6`, then 403.
- Given `go build` e `go test ./...`, then passam, inclusive os testes de webhook existentes.

## Spec Change Log

## Design Notes

Na cadeia Cloudflare → túnel → nginx → API, o nginx faz `X-Forwarded-For: <recebido>, <IP do proxy>`. O `GetClientIP` atual pega a última entrada, que é o proxy interno e não o cliente — por isso a allowlist calcula o IP por conta própria: a entrada mais à direita que não seja interna é a que a nossa infraestrutura acrescentou (o Cloudflare anexa o IP que viu) e não pode ser forjada pelo cliente, que só consegue prefixar entradas à esquerda.

Aviso `CONCLUIDA` em linha v1 sem fonte de download não muda o status: o processador devolveria NOT_READY e a linha ficaria presa em `webhook_received`, fora do alcance do poller (que olha `requested`).

## Verification

**Commands:**
- `cd backend && go build -mod=vendor ./... && go test -mod=vendor -count=1 ./handlers/ ./services/` -- expected: ok

## Suggested Review Order

**Allowlist de origem (ponto de entrada)**

- IP real atrás de Cloudflare/nginx: entrada mais à direita não interna; ilegível nega.
  [`rfb_webhook_ip.go:37`](../../backend/handlers/rfb_webhook_ip.go#L37)

- Lê a variável a cada chamada; vazia libera, preenchida sem entrada válida nega.
  [`rfb_webhook_ip.go:89`](../../backend/handlers/rfb_webhook_ip.go#L89)

- Aplicada só ao POST, antes de ler o corpo (o HEAD segue aberto).
  [`rfb_apuracao.go:640`](../../backend/handlers/rfb_apuracao.go#L640)

**Payload documentado**

- `CONCLUIDA` sem dados de download vira sucesso; `ERRO`/`EXPIRADA` por situação vão ao ramo de erro.
  [`rfb_apuracao.go:704`](../../backend/handlers/rfb_apuracao.go#L704)

- Situação não final só loga e responde 200.
  [`rfb_apuracao.go:778`](../../backend/handlers/rfb_apuracao.go#L778)

- Linha v1 fica intacta: não prender em `webhook_received`, fora do alcance do poller.
  [`rfb_apuracao.go:803`](../../backend/handlers/rfb_apuracao.go#L803)

**Ativação**

- Repasse da variável no compose da AWS (vazia por padrão).
  [`docker-compose.yml:34`](../../installer/cliente-aws/docker-compose.yml#L34)

**Testes**

- Cadeia de proxies, lista parcial, IPv4 mapeado e handler 403/HEAD.
  [`rfb_webhook_ip_test.go`](../../backend/handlers/rfb_webhook_ip_test.go)

- Sucesso documentado em linha v2/v1, situação não final e erro por situação.
  [`rfb_webhook_situacao_test.go:12`](../../backend/handlers/rfb_webhook_situacao_test.go#L12)

## Design Notes (revisão)

**Antes de preencher `RFB_WEBHOOK_ALLOWED_IPS` em qualquer servidor**, confira o IP que o sistema enxerga: o log `[RFB Webhook] ... IP de origem calculado: X` aparece em todo POST. Se houver um proxy público extra na cadeia (ex.: Traefik atrás do Cloudflare), o IP calculado será o do proxy e a allowlist negaria a RFB; nesse caso é preciso suportar hops confiáveis (não implementado). Pelo log do nginx do Hostinger a cadeia é `cloudflared` (privado) → nginx; na AWS não foi verificada. O cabeçalho é forjável por quem alcançar a porta da API sem passar pelo nginx; a API não publica porta.
