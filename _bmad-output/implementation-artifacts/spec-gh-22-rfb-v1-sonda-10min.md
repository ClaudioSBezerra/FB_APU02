---
title: 'RFB v1: reconsultar a situação e sondar o download v1 a cada 10 min'
type: 'feature'
created: '2026-10-29'
status: 'done'
baseline_commit: '54a6be8595d258b615f28aef6b6dc7509430f60e'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Em solicitações da rota legada v1 a situação v2 devolve 403 e o poller (#20) desiste da linha no primeiro erro; o webhook não chega, e a linha fica `requested` até o TIMEOUT de 5h sem nenhuma outra tentativa de obter o resultado.

**Approach:** Para linhas v1 sem fonte de download, a cada 10 minutos dentro da janela de 240 min: reconsultar a situação e sondar o download legado `GET /{rtc|prr-rtc}/download/v1/{tíquete da solicitação}`. A sonda nunca grava erro na linha; se o download devolver o arquivo, processa-o sem baixar de novo (o tíquete é de uso único).

## Boundaries & Constraints

**Always:** Linhas v2 mantêm o comportamento atual (backoff 2/5/10 min, desistir em 401/403/404). Cadência fixa de 10 min só para v1. Em 401/403/404 da situação ou do download v1 a linha fica intacta e o poller NÃO desiste. Log de diagnóstico só quando a resposta MUDA (status HTTP ou corpo), não a cada ciclo. Se a situação responder CONCLUIDA/ERRO/EXPIRADA, comportamento atual. Arquivo recebido é processado com a guarda de status dos processadores (claim `downloading`) e `raw_json` gravado antes do parse. Estado de backoff/log só em memória.

**Ask First:** Qualquer deploy ou push (dispara deploy em produção).

**Never:** Marcar a linha como `downloading`, gravar erro ou sobrescrever o TIMEOUT por causa de 401/403/404 da sonda. Reintroduzir o fallback de download no `BaixarArquivoRFB` (commit 174243e). Alterar parser, webhook ou migrations.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Sem resposta (caso atual) | v1 `requested`, situação 403 e download v1 401 "tíquete inexistente" | linha intacta; reconsulta em 10 min; 1 log por mudança de resposta | N/A |
| Download v1 devolve arquivo | download 200 com JSON | `raw_json` gravado, processado, linha `completed` (sem 2º download) | falha de parse → erro normal do processador |
| Situação passa a CONCLUIDA | situação 200 `CONCLUIDA` | fluxo atual: obtém urlAssinada e baixa | N/A |
| Erro 5xx/rede | situação ou download com 5xx | sem log de diagnóstico de acesso; tenta no próximo ciclo | log curto |
| Linha v2 | api_versao v2, situação 403 | desiste como hoje | N/A |
| Fora da janela | linha com mais de 240 min | ignorada | abortador de 5h segue |

</frozen-after-approval>

## Code Map

- `backend/services/rfb_poller.go` -- `pollarUma`, `rfbPollSemAcesso`, `rfbPollEstado`: cadência e sonda
- `backend/services/rfb_download.go` -- `RFBDownloadInput`, `BaixarArquivoRFB`, `SemFonteDeDownload`
- `backend/services/rfb_processor.go` (~520) e `rfb_creditos_processor.go` (~264) -- `ProcessarDownload*`: aceitar arquivo já baixado
- `backend/services/rfb.go` -- `DownloadArquivo` (401 de tíquete vira `RFBErrTicketInvalido`)
- `backend/services/rfb_poller_test.go` -- fakes `v2Fake` e helpers de integração

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/rfb_download.go` -- campo `PreBaixado []byte` em `RFBDownloadInput`: devolvido direto por `BaixarArquivoRFB` e conta como fonte para `SemFonteDeDownload` -- processar o arquivo da sonda sem baixar de novo
- [x] `backend/services/rfb_processor.go`, `rfb_creditos_processor.go` -- `ProcessarDownload*ComArquivo(db, client, id, dados)`; os atuais viram wrappers -- reaproveitar claim, `raw_json` e parse
- [x] `backend/services/rfb_poller.go` -- v1: intervalo fixo de 10 min, sem desistir em 401/403/404, log por mudança, sonda `DownloadArquivo` após a situação; v2 inalterado -- pedido do usuário
- [x] `backend/services/rfb_poller_test.go` -- cobrir a matriz (v1 sem resposta, arquivo recebido, v2 inalterado, log por mudança) -- regressão

**Acceptance Criteria:**
- Given linha v1 `requested` com situação 403 e download 401, when passam 3 ciclos de 10 min, then a linha segue `requested` sem erro gravado e houve 3 consultas de situação e 3 sondas de download.
- Given o download v1 devolve um JSON de apuração, when o poller roda, then a linha chega a `completed` e o tíquete foi baixado uma única vez.
- Given `go build` e `go test`, then passam, inclusive os testes do poller da issue #20.

## Spec Change Log

## Design Notes

A sonda usa o tíquete da SOLICITAÇÃO, que a RFB já recusou com 401 "inexistente" em testes anteriores (gh-18). É um experimento barato: o resultado esperado é o mesmo 401, mas se a RFB passar a atender (por exemplo ao concluir o processamento) o arquivo é capturado. Como o tíquete de download v1 é de uso único, o arquivo recebido precisa ser entregue ao processador (`PreBaixado`), nunca baixado uma segunda vez.

O log por mudança guarda em memória a última resposta de cada linha (status + início do corpo): enquanto for igual, o ciclo fica em silêncio; a primeira resposta diferente (inclusive um 200) volta a logar.

## Verification

**Commands:**
- `cd backend && go build -mod=vendor ./... && go test -mod=vendor -count=1 ./services/ ./handlers/` -- expected: ok

## Suggested Review Order

**Sonda do download v1 (ponto de entrada)**

- Reconfere a linha, grava o `raw_json` na hora, só processa JSON; o GET gasta o tíquete.
  [`rfb_poller.go:325`](../../backend/services/rfb_poller.go#L325)

- Quando a situação v2 é negada, v1 segue para a sonda em vez de desistir.
  [`rfb_poller.go:223`](../../backend/services/rfb_poller.go#L223)

**Cadência e log por mudança**

- v1 fixo em 10 min; v2 mantém o backoff por idade.
  [`rfb_poller.go:103`](../../backend/services/rfb_poller.go#L103)

- 401/403/404: v2 desiste, v1 continua e só loga quando a resposta muda.
  [`rfb_poller.go:294`](../../backend/services/rfb_poller.go#L294)

- Memória do último resultado por canal (situação/download).
  [`rfb_poller.go:83`](../../backend/services/rfb_poller.go#L83)

**Entregar o arquivo sem baixar de novo**

- `PreBaixado`: devolvido direto e conta como fonte de download.
  [`rfb_download.go:372`](../../backend/services/rfb_download.go#L372)

- Processador de débitos: sem credencial nem token quando o arquivo já veio.
  [`rfb_processor.go:530`](../../backend/services/rfb_processor.go#L530)

- Mesmo para créditos.
  [`rfb_creditos_processor.go:274`](../../backend/services/rfb_creditos_processor.go#L274)

- Cliente silencioso na sonda, para não repetir log a cada ciclo.
  [`rfb.go:117`](../../backend/services/rfb.go#L117)

**Testes**

- v1 sem resposta, arquivo recebido, corpo não JSON, linha já assumida, sem credencial.
  [`rfb_poller_test.go`](../../backend/services/rfb_poller_test.go)

## Design Notes (revisão)

O GET do download v1 gasta o tíquete, então a ordem é: reconferir a linha → baixar → gravar `raw_json` → validar JSON → processar. Com `PreBaixado` o processador pula a credencial e o token, que só serviriam para baixar. O experimento usa o tíquete da SOLICITAÇÃO, que a RFB já recusou com 401 "inexistente" antes (gh-18); o esperado é o mesmo 401 (logado uma vez), e se a RFB passar a atender o arquivo é capturado.

Aceito: um erro depois do tíquete consumido (arquivo grande demais, falha de leitura) não é distinguível de "ainda não está pronto" e fica só no log.
