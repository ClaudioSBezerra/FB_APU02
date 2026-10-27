---
title: 'RFB: download sem depender do webhook (v1 sem fallback de tíquete, 401 classificado, compose AWS com RFB_API_VERSION)'
type: 'bugfix'
created: '2026-10-27'
status: 'done'
baseline_commit: 'db9a4030074d707aa38a328296de07c90658bb02'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Em `rtc`/v1 a RFB aceita a solicitação (201) mas o webhook nunca chega (8 dias seguidos, `tiquete_download` NULL, TIMEOUT). "Download Manual/Tentar Recuperar" então baixa com o tíquete da **solicitação**, a RFB responde 401 "Tíquete inexistente ou download já realizado", o código rotula como "token inválido ou expirado" e o erro sobrescreve o TIMEOUT real da linha. Além disso o compose da AWS (copiado pelo CI a cada deploy) não repassa `RFB_API_VERSION*`, então a v2 (polling de situação, sem webhook) nunca roda lá.

**Approach:** Não tentar download v1 sem `tiquete_download`/`url_assinada` (devolve "ainda não pronto" sem tocar na linha), classificar o 401 de tíquete, e repassar as variáveis de versão/segredo no compose mantendo v1 como padrão.

## Boundaries & Constraints

**Always:** v1 com `tiquete_download` presente se comporta exatamente como hoje. Padrão vazio de `RFB_API_VERSION*` = v1. v2 continua baixando por `urlAssinada`/situação. Migrations existentes intocadas. Compat Hostinger/Coolify.

**Ask First:** Qualquer ativação de v2 em produção ou disparo do "Solicitar" de teste (consome 1 das 4 chamadas/dia da v2) — é ação do usuário após o deploy.

**Never:** Alterar o fluxo do webhook, o parser, a cota diária ou o frontend; fazer push/merge sem aprovação.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Recuperação v1 sem webhook | linha v1, `tiquete_download` e `url_assinada` vazios, status `error`/TIMEOUT | Download Manual responde 409 com mensagem clara; linha INALTERADA (continua TIMEOUT) | sem chamada à RFB |
| Processador v1 sem fonte | mesmo estado, chamado por qualquer caminho | retorna erro `NOT_READY` antes do claim; sem UPDATE | log |
| v1 com webhook | `tiquete_download` presente | download normal por tíquete de download | N/A |
| v2 sem webhook | linha v2, sem url | segue para situação (polling) como hoje | N/A |
| 401 de tíquete | 401, corpo contém "Tíquete" | erro `TICKET_INVALIDO` ("inexistente ou já utilizado") | N/A |
| 401 de auth | 401, corpo sem "tíquete" | `DOWNLOAD_ERROR` com texto de falha de autenticação | N/A |

</frozen-after-approval>

## Code Map

- `backend/services/rfb_download.go` -- `BaixarArquivoRFB` (fallback v1 ~444), `RFBDownloadInput`, constantes `RFBErr*`
- `backend/services/rfb.go` -- `DownloadArquivo` (switch de status, 401 ~516)
- `backend/services/rfb_processor.go`, `backend/services/rfb_creditos_processor.go` -- claim `downloading` e `updateRequestError` que sobrescrevem a linha
- `backend/handlers/rfb_apuracao.go` -- `DownloadManualHandler` (~143), "Tentar Recuperar"
- `installer/cliente-aws/docker-compose.yml` -- lista explícita de env do serviço `api`
- `backend/services/rfb_v2_test.go` -- testes existentes (~877 usa o fallback e precisa de `TiqueteDownload`)

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/rfb_download.go` -- constante `RFBErrTicketInvalido`, método `SemFonteDeDownload()` e `BaixarArquivoRFB` devolve `NOT_READY` em vez do fallback v1 -- elimina a chamada que sabidamente falha
- [x] `backend/services/rfb.go` -- 401 com "tíquete" no corpo vira `RFBDownloadError{TICKET_INVALIDO}`; demais 401 mantêm rótulo de auth -- mensagem verdadeira
- [x] `backend/services/rfb_processor.go`, `backend/services/rfb_creditos_processor.go` -- se `SemFonteDeDownload()`, retornar `NOT_READY` ANTES do claim, sem escrever na linha -- preserva o TIMEOUT
- [x] `backend/handlers/rfb_apuracao.go` -- `DownloadManualHandler` lê `api_versao`/`url_assinada` e responde 409 síncrono nesse caso (v2 segue igual) -- feedback imediato, sem goroutine
- [x] `installer/cliente-aws/docker-compose.yml` -- `RFB_API_VERSION`, `RFB_API_VERSION_RESTRITA`, `RFB_WEBHOOK_SECRET` com padrão vazio, comentados -- v2 passa a ser ativável pelo `.env`
- [ ] testes em `backend/services/*_test.go` e `backend/handlers/*_test.go` -- cobrir a matriz; ajustar `rfb_v2_test.go:877` para passar `TiqueteDownload`

**Acceptance Criteria:**
- Given linha v1 TIMEOUT sem `tiquete_download`, when Download Manual, then 409 e `error_code` continua `TIMEOUT`.
- Given `go build ./... && go test ./...`, then passam; testes v1 com webhook seguem verdes.

## Spec Change Log

## Design Notes

A guarda fica ANTES do claim `downloading` porque é o claim + `updateRequestError` que apagam o TIMEOUT; mudar só a mensagem não basta. O webhook tardio continua aceito para linhas `error` (já existente), então nada se perde.

Teste pós-deploy (ação do usuário, fora do código): definir `RFB_API_VERSION=v2` no `.env` da AWS, recriar o container, clicar "Solicitar" UMA vez e ler o log por `no Route matched` (rota ausente) ou `situacao`/`urlAssinada` (funcionando). Reverter para vazio se 404.

## Verification

**Commands:**
- `cd backend && go build -mod=vendor ./... && go test -mod=vendor -count=1 ./...` -- expected: ok

## Suggested Review Order

**Regra única: sem fonte de download na v1, não há o que baixar (ponto de entrada)**

- A v1 só baixa com o tíquete de download do webhook; a v2 sempre tem a situação.
  [`rfb_download.go:375`](../../backend/services/rfb_download.go#L375)

- O fallback do tíquete da solicitação some; erro real da URL não é mascarado.
  [`rfb_download.go:469`](../../backend/services/rfb_download.go#L469)

**Preservar a linha (TIMEOUT)**

- Checagem ANTES do claim `downloading`, que é o que apagava o erro verdadeiro.
  [`rfb_processor.go:562`](../../backend/services/rfb_processor.go#L562)
  [`rfb_creditos_processor.go:306`](../../backend/services/rfb_creditos_processor.go#L306)

- Download Manual responde 409 na hora, com a mesma regra e mensagem por status.
  [`rfb_apuracao.go:219`](../../backend/handlers/rfb_apuracao.go#L219)

**401 classificado**

- Só o texto específico de tíquete vira `TICKET_INVALIDO`; auth continua auth.
  [`rfb.go:522`](../../backend/services/rfb.go#L522)
  [`rfb_download.go:28`](../../backend/services/rfb_download.go#L28)

**Compose da AWS**

- `RFB_API_VERSION*` e segredo do webhook repassados, padrão vazio = v1.
  [`docker-compose.yml:30`](../../installer/cliente-aws/docker-compose.yml#L30)

**Testes**

- Linha TIMEOUT preservada (débito e crédito) sem nenhuma chamada à RFB.
  [`rfb_download_sem_webhook_test.go:118`](../../backend/services/rfb_download_sem_webhook_test.go#L118)

- Classificação do 401 e não-chamada ao download.
  [`rfb_download_sem_webhook_test.go:47`](../../backend/services/rfb_download_sem_webhook_test.go#L47)

- 409 do handler com a linha intacta.
  [`rfb_download_manual_test.go:16`](../../backend/handlers/rfb_download_manual_test.go#L16)

- Helper de teste agora cria a linha com `tiquete_download`, como o webhook real faria.
  [`rfb_processor_test.go:129`](../../backend/services/rfb_processor_test.go#L129)
