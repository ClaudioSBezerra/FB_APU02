---
title: 'RFB v1: sonda de download 1x/hora após 401 definitivo e pausa de 1h no 429'
type: 'bugfix'
created: '2026-10-30'
status: 'done'
baseline_commit: 'f9de69efb6afa1f5c61bcc729f509ae0c8491f83'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A sonda do download v1 (#22) bate a cada 10 min na rota `GET /rtc/download/v1/{tíquete}`. Em 30/10 ela recebeu 401 "Tíquete inexistente ou download já realizado" por 90 minutos sem informação nova e então o gateway passou a devolver 429 `API rate limit exceeded` — na MESMA rota que um download legítimo usaria. A sonda pode gastar a quota de que o download real dependeria e segue batendo a cada 10 min mesmo em 429.

**Approach:** Espaçar a sonda por resposta: após 401 de tíquete inexistente, no máximo 1 sonda de download por hora; em 429, pausa de 1 hora. Qualquer resposta diferente volta a ser logada e retoma os 10 min. A reconsulta da situação não muda.

## Boundaries & Constraints

**Always:** A cadência da situação v2 segue a cada 10 min. Log só quando a resposta muda, incluindo quando a pausa começa. Estado só em memória. Mensagens de erro existentes de `DownloadArquivo` inalteradas (testes e logs dependem delas). Pausa e espaçamento valem por solicitação.

**Ask First:** Qualquer deploy ou push (dispara deploy em produção).

**Never:** Alterar linhas v2, o webhook, o `BaixarArquivoRFB` ou a cadência da situação. Gravar na linha por causa da sonda. Migrations.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| 401 de tíquete | download v1 → 401 "Tíquete inexistente ou download já realizado" | próxima sonda só após 1h; situação segue a cada 10 min | 1 log por mudança |
| 429 | download v1 → 429 | pausa de 1h sem nenhuma chamada ao download; log ao entrar na pausa | 1 log por mudança |
| 401 genérico (auth) / outro erro | 401 sem texto de tíquete, 403, 404, 5xx | cadência de 10 min como hoje | log por mudança |
| Resposta nova | após pausa/espaçamento, 200 ou status diferente | loga a mudança; 200 segue o fluxo existente do arquivo | N/A |
| Fora da janela / v2 | linha > 240 min ou api_versao v2 | inalterado | N/A |

</frozen-after-approval>

## Code Map

- `backend/services/rfb_poller.go` -- `rfbPollEstado`, `sondarDownloadV1` (~325), chamada em `pollarUma` (~223): estado e decisão de sondar
- `backend/services/rfb.go` -- `DownloadArquivo` (~540-565): ramos 401/403/404/default; falta tipar o status para reconhecer o 429
- `backend/services/rfb_poller_test.go` -- `TestPoller_V1SemResposta_Reconsulta10MinESondaDownload` (fake com texto de tíquete) e `TestSondarDownloadV1_LinhaJaAssumidaNaoBaixa` (chama `sondarDownloadV1`)

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/rfb.go` -- erro tipado `rfbStatusError{Status}` com a MESMA mensagem de hoje nos ramos não-200 de `DownloadArquivo` -- reconhecer o 429 sem mudar logs/testes
- [x] `backend/services/rfb_poller.go` -- campo `proximaSonda` no estado, `sondarDownloadV1` recebe `agora`, pula se antes de `proximaSonda`, define +1h após `RFBErrTicketInvalido` ou 429 e zera nas demais respostas -- backoff por resposta
- [x] `backend/services/rfb_poller_test.go` -- ajustar o teste de 3 sondas (o fake usa texto de tíquete: passa a 1 sonda em 20 min e a 2ª em +60) e cobrir 429, 401 genérico, resposta nova e pausa -- regressão

**Acceptance Criteria:**
- Given download v1 → 401 de tíquete, when passam 3 ciclos (t0, +10, +20 min), then houve 3 consultas de situação e 1 sonda; em +60 min uma 2ª sonda.
- Given download v1 → 429, when passam ciclos de 10 min dentro de 1h, then nenhuma chamada nova ao download; após 1h uma nova tentativa.
- Given 401 sem texto de tíquete, when 3 ciclos, then 3 sondas (cadência de 10 min).
- Given `go build` e `go test`, then passam.

## Spec Change Log

## Design Notes

O 401 de tíquete inexistente é resposta estável (mesma em todas as 9 tentativas de 30/10): repeti-la a cada 10 min só gasta quota. O 429 pode ser por hora ou por dia; 1h é um compromisso que reduz a pressão sem abandonar a sonda — se o 429 persistir, a sonda volta a ser pausada por mais 1h.

## Verification

**Commands:**
- `cd backend && go build -mod=vendor ./... && go test -mod=vendor -count=1 ./services/` -- expected: ok

## Suggested Review Order

**Decisão de sondar (ponto de entrada)**

- A sonda só bate na RFB quando liberada; 401 de tíquete inexistente e 429 espaçam em 1h, o resto zera.
  [`rfb_poller.go:352`](../../backend/services/rfb_poller.go#L352)

- A resposta definitiva (tíquete inexistente) é reconhecida pelo código tipado já existente.
  [`rfb_poller.go:372`](../../backend/services/rfb_poller.go#L372)

- O 429 é reconhecido pelo status preservado no erro.
  [`rfb_poller.go:376`](../../backend/services/rfb_poller.go#L376)

**Estado em memória**

- Espaçamento de 1 hora e as duas operações de estado (liberada / definir próxima).
  [`rfb_poller.go:106`](../../backend/services/rfb_poller.go#L106)

**Erro tipado, mesmas mensagens**

- `rfbStatusError` guarda o status e devolve exatamente a mensagem de antes.
  [`rfb.go:180`](../../backend/services/rfb.go#L180)

**Testes**

- Situação a cada 10 min, sonda espaçada após o 401 e volta em 1h (ajustado de propósito).
  [`rfb_poller_test.go:443`](../../backend/services/rfb_poller_test.go#L443)

- 429: pausa de 1h; erro genérico mantém 10 min.
  [`rfb_poller_test.go:487`](../../backend/services/rfb_poller_test.go#L487)

## Design Notes (revisão)

Riscos aceitos, a revisitar se os logs mostrarem: (1) o 401 "tíquete inexistente" pode ser transitório enquanto o relatório é gerado, e a pausa de 1h atrasaria a captura — a evidência de 30/10 foi de 9 respostas idênticas em 90 min; (2) a pausa do 429 é por solicitação, não por credencial, e o `Retry-After` é ignorado (não exposto por `DownloadArquivo`); (3) o estado é em memória e cada deploy zera o espaçamento (no máximo 1 sonda extra por deploy).
