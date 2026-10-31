---
title: 'RFB v2: pgtoAntecipado, validação do NI do arquivo, tEASegundos no poller e Accept na situação'
type: 'feature'
created: '2026-10-30'
status: 'done'
baseline_commit: '153fa46b47eb6dae74ca891e04d92c410c5c62ac'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A auditoria de 30/10 contra a documentação atual da API CBS v2 (débito v1.2 de 14/10, crédito, situação) achou lacunas que afetam a primeira captura real: o débito ignora `cbs.pgtoAntecipado` (extinto gravado a menor); a raiz do arquivo (`ni`, `niConsumidor`, `tiqueteSolicitacao`, `geradoEm`) é ignorada, então um arquivo de outro contribuinte seria gravado no tenant da solicitação; o poller v2 consulta a situação ~45 vezes por solicitação sem usar a estimativa `tEASegundos` da RFB (há relatos de 429 nessa rota); a consulta de situação não manda `Accept: application/json`.

**Approach:** Somar `pgtoAntecipado` ao extinto; validar `ni` contra o CNPJ base da solicitação antes de gravar (download e reprocessamento, débitos e créditos); guardar `tEASegundos` na solicitação e usar na primeira consulta, espaçando as seguintes; incluir o `Accept`.

## Boundaries & Constraints

**Always:** O `ni` real vem com 14 dígitos (CNPJ completo, ex. `38149716000128`, captura de 30/10), apesar de a doc dizer 8: comparar os 8 PRIMEIROS dígitos do `ni` (só dígitos) com o CNPJ base; aceitar `ni` de 8 ou 14 dígitos. `ni` divergente → linha em `error` com código `NI_DIVERGENTE`, nenhuma linha de débito/crédito gravada e `raw_json` limpo (não guardar dado de outro contribuinte). `ni` ausente → aceita com log (compatibilidade). `tiqueteSolicitacao` divergente do da linha → só log. `tea_segundos` em migration NOVA e aditiva (137). Webhook continua sendo o atalho.

**Ask First:** Qualquer deploy ou push (dispara deploy em produção).

**Never:** Alterar o webhook, o fluxo v1, migrations existentes ou mensagens de erro existentes.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Pagamento antecipado | débito com `cbs.extinto=10`, `pgtoAntecipado=5` | `valor_cbs_extinto=15`; `saldoDevedor` como veio | N/A |
| Sem pagamento antecipado | campo omitido | igual a hoje | N/A |
| NI confere | `ni` = `38149716000128` (ou `38149716`), cnpj_base `38149716` | processa normal | N/A |
| NI divergente | 8 primeiros dígitos do `ni` != cnpj_base (download ou reprocessamento) | `error`/`NI_DIVERGENTE`, nada gravado, `raw_json` nulo | log com os dois NI |
| NI ausente ou malformado | arquivo sem `ni`, ou com menos de 8 dígitos | ausente: processa com log; malformado: `NI_DIVERGENTE` | log |
| tEA conhecido | 201 com `tEASegundos=120` | 1ª consulta em created_at + max(120 s, 2 min) | N/A |
| tEA sem estimativa | `-1`, ausente ou inválido | 1ª consulta em created_at + 5 min | N/A |
| Consultas seguintes v2 | situação não final | +10, +20, +30 min e depois a cada 30 min, dentro dos 240 min | N/A |
| v1 | linha v1 | cadência atual de 10 min | N/A |

</frozen-after-approval>

## Code Map

- `backend/migrations/137_rfb_requests_tea_segundos.sql` (novo) -- coluna `tea_segundos INTEGER` nula
- `backend/services/rfb.go` -- `SolicitarApuracao`/`SolicitarCredito` (~385/463): expor o tEA da resposta; `consultarSituacaoOnce`: header Accept
- `backend/services/rfb_scheduler.go` -- INSERTs com tíquete (~185, ~283): gravar `tea_segundos`
- `backend/services/rfb_processor.go` -- `RFBApuracaoV2JSON`/`RFBCBSV2Debito`/`adaptDebitoV2` (~251-330); ramos v2 do download (~630) e do reprocessamento (~1079)
- `backend/services/rfb_creditos_processor.go` -- `RFBCreditosV2JSON` (~71); ramos v2 do download (~369) e do reprocessamento (~742)
- `backend/services/rfb_poller.go` -- consulta lê `tea_segundos`; cadência v2

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/137_rfb_requests_tea_segundos.sql` -- `ALTER TABLE ... ADD COLUMN IF NOT EXISTS tea_segundos INTEGER` -- persistir a estimativa
- [x] `backend/services/rfb.go` -- campo `UltimoTEASegundos` no client, preenchido na 201 (sem mudar a assinatura); `Accept` na situação -- tEA e conformidade
- [x] `backend/services/rfb_scheduler.go` -- gravar `tea_segundos` nos dois INSERTs com tíquete -- tEA disponível ao poller
- [x] `backend/services/rfb_processor.go`, `rfb_creditos_processor.go` -- campos da raiz + `PgtoAntecipado`; helper único de validação do NI usado nos 4 ramos v2 -- multi-tenant e valor correto
- [x] `backend/services/rfb_poller.go` -- 1ª consulta v2 pelo tEA e intervalos 10/20/30/30 por número de consultas -- menos chamadas
- [x] `backend/services/*_test.go` -- cobrir a matriz -- regressão

**Acceptance Criteria:**
- Given um arquivo v2 de débitos com `ni` de outro CNPJ, when é processado ou reprocessado, then nenhuma linha em `rfb_debitos`/`rfb_resumo` é gravada e a solicitação fica `error`/`NI_DIVERGENTE` sem `raw_json`.
- Given uma solicitação v2 sem webhook que conclui após 25 min, when o poller roda, then a captura acontece com no máximo 4 consultas de situação.
- Given `go build` e `go test`, then passam.

## Spec Change Log

## Design Notes

Primeira captura v2 real (30/10, Fortes Bezerra): raiz com `tiqueteSolicitacao`, `ni` de 14 dígitos, `niConsumidor` de 11 (CPF do detentor da credencial), `geradoEm`; `tEASegundos=-1`; débitos de NFS-e (`documento=91`) só com `apurado`/`saldoDevedor`; créditos com `apuracao: []`. O aviso do webhook chegou 1 s após a abertura, de 189.9.61.71 e 189.9.61.91 (não do .23 da doc).


O tEA vai para coluna (não memória) porque deploys reiniciam o container com frequência e o poller perderia a estimativa. Ele é exposto por um campo do client (`UltimoTEASegundos`) para não mudar a assinatura de `SolicitarApuracao`/`SolicitarCredito`, usadas também no reprocessamento manual. A contagem de consultas já feitas fica em memória: um restart só antecipa uma consulta.

`pgtoAntecipado` é extinção (pagamento antecipado em nota de débito vinculada), por isso soma ao extinto; não há coluna própria e a soma mantém `extinto + saldoDevedor` coerente com o apurado.

## Verification

**Commands:**
- `cd backend && go build -mod=vendor ./... && go test -mod=vendor -count=1 ./services/ ./handlers/` -- expected: ok

## Suggested Review Order

**Proteção multi-tenant (ponto de entrada)**

- Valida a raiz do arquivo antes de gravar; download apaga o raw_json, reprocessamento o mantém como evidência.
  [`rfb_processor.go:378`](../../backend/services/rfb_processor.go#L378)

- Compara as 8 primeiras posições (ni real tem 14; CNPJ alfanumérico aceito).
  [`rfb_processor.go:361`](../../backend/services/rfb_processor.go#L361)

- Chamada no download de débitos (e nos outros 3 ramos v2).
  [`rfb_processor.go:725`](../../backend/services/rfb_processor.go#L725)

**Valores**

- `pgtoAntecipado` soma ao extinto, com guarda contra contagem dupla.
  [`rfb_processor.go:294`](../../backend/services/rfb_processor.go#L294)

**Cadência do poller v2**

- 1ª consulta pelo tEA da RFB (mín. 2 min, máx. 210 min; sem estimativa = 5 min).
  [`rfb_poller.go:30`](../../backend/services/rfb_poller.go#L30)

- Intervalos de 10, 20 e 30 min; só conta consulta que a RFB respondeu.
  [`rfb_poller.go:139`](../../backend/services/rfb_poller.go#L139)

- O tEA vem da 201 (limitado a 1 dia) e é gravado na abertura.
  [`rfb.go:145`](../../backend/services/rfb.go#L145)
  [`rfb_scheduler.go:188`](../../backend/services/rfb_scheduler.go#L188)

**Testes**

- NI divergente (download e reprocessamento), 14 posições, pgtoAntecipado, tEA gravado e respeitado.
  [`rfb_v2_conformidade_test.go`](../../backend/services/rfb_v2_conformidade_test.go)

## Design Notes (revisão)

Ajustes da revisão: CNPJ alfanumérico (jul/2026) na comparação; no reprocessamento o raw_json não é apagado (evidência de linhas importadas antes da validação existir); `ni` ausente + tíquete de outra solicitação rejeita; raiz em FlexString (número não quebra o parse); tEA limitado (1ª espera ≤ 210 min; > 1 dia = sem estimativa); só conta consulta respondida pela RFB e retentativa de 2 min no 404 após CONCLUIDA; guarda contra dupla contagem do pgtoAntecipado (a semântica exata só se confirma com um arquivo real que traga o campo).
