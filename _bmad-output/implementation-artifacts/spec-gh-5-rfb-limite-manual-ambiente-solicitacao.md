---
title: 'RFB débitos: 2ª chamada manual do dia + ambiente gravado por solicitação'
type: 'bugfix'
created: '2026-09-11'
status: 'done'
baseline_commit: '617fdb2'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** (1) O botão manual de débitos nunca consegue a 2ª chamada do dia que a RFB permite: o handler checa 2/dia, mas chama `SolicitarApuracaoParaEmpresa`, que tem limite próprio de 1/dia — depois da coleta automática, sempre "Limite diário atingido". (2) `rfb_requests.ambiente` nunca é gravado (fica sempre no default `producao`), e (3) o download resolve o ambiente pelo cadastro da credencial, não pela solicitação — trocar o ambiente entre solicitar e baixar faz o download ir no prefixo errado. Isso inviabiliza o experimento comparativo `rtc` × `prr-rtc` da captura de débitos CBS (parada desde 07/2026).

**Approach:** O limite diário vira parâmetro do service: agendamento passa 1 (preserva o slot manual), handlers manuais passam 2 (teto da RFB). Todo INSERT em `rfb_requests` grava o ambiente da credencial usada; os dois processadores de download usam o ambiente da linha da solicitação e só buscam client_id/secret na credencial.

## Boundaries & Constraints

**Always:** Nunca permitir mais de 2 solicitações de débito/dia por empresa (teto da RFB), em nenhum caminho. Agendamento continua usando no máximo 1 e não dispara se já houve qualquer solicitação de débito no dia. Linhas de erro (TOKEN_ERROR, RATE_LIMIT, REQUEST_ERROR) também gravam o ambiente.

**Ask First:** Qualquer migration nova ou alteração de dados em produção.

**Never:** Mexer em `rfb_debitos` (segregação por ambiente fica para depois, se necessário); alterar migrations existentes; mudar prefixos/URLs da RFB; alterar os limites de créditos.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Manual após agendamento | 1 débito hoje (qualquer status ≠ pending), limite 2 | Solicita à RFB, grava linha nova | N/A |
| Agendamento com débito no dia | 1 débito hoje, limite 1 | Não chama a RFB | Erro `DAILY_LIMIT: ...`, nenhuma linha criada |
| Teto RFB | 2 débitos hoje, limite 2 | Não chama a RFB | Erro `DAILY_LIMIT`, handler responde 429 |
| Credencial `producao_restrita` | Solicitação OK | Linha débito e linha crédito com `ambiente='producao_restrita'`, POST em `/prr-rtc/...` | N/A |
| Ambiente trocado antes do download | Linha `producao_restrita`, credencial agora `producao` | Download em `/prr-rtc/download/v1/...` | N/A |

</frozen-after-approval>

## Code Map

- `backend/services/rfb_scheduler.go` -- `SolicitarApuracaoParaEmpresa` (limite 1/dia hoje, linhas ~92-106; 3 INSERTs), `SolicitarCreditoParaEmpresa` (3 INSERTs, comentário ~220 assume gate de 1/dia), loop do scheduler (~317)
- `backend/services/rfb_processor.go` -- `ProcessarDownloadRFB`: ambiente lido de `rfb_credentials` (~103-113)
- `backend/services/rfb_creditos_processor.go` -- `ProcessarDownloadCreditosRFB`: idem (~67-77)
- `backend/handlers/rfb_apuracao.go` -- `SolicitarApuracaoHandler` (~104-116) e `RessolicitarHandler` (~448-461) tratam a string "slot automático já utilizado"
- `backend/services/sap_sync_trigger_test.go` -- helpers reutilizáveis `openTestDB`, `setupTestCompany`

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/rfb_scheduler.go` -- adicionar constantes `LimiteDebitosDiaAgendamento = 1` e `LimiteDebitosDiaRFB = 2`; `SolicitarApuracaoParaEmpresa(db, companyID, limiteDiario int)` compara a contagem com o parâmetro e retorna erro prefixado `DAILY_LIMIT:`; scheduler passa `LimiteDebitosDiaAgendamento`; incluir `ambiente` nos 6 INSERTs; atualizar o comentário do crédito (até 2/dia agora) -- corrige (1) e (2)
- [x] `backend/handlers/rfb_apuracao.go` -- passar `services.LimiteDebitosDiaRFB`; trocar o case "slot automático já utilizado" por `DAILY_LIMIT` (429, mensagem "máximo 2 por dia") nos dois handlers -- remove código morto e mensagem enganosa
- [x] `backend/services/rfb_processor.go`, `backend/services/rfb_creditos_processor.go` -- ler `COALESCE(r.ambiente,'producao')` na query da solicitação; credencial só para client_id/secret; logar o ambiente usado -- corrige (3)
- [x] `backend/services/rfb_scheduler_test.go` -- testes de integração (pulam sem DB) com `httptest` simulando token/apuração/créditos/download, cobrindo as 5 linhas da matriz

**Acceptance Criteria:**
- Given o código alterado, when `go build ./... && go vet ./services/ ./handlers/`, then sem erros
- Given Postgres local, when `go test ./services/ -run RFB`, then todos passam (e nada fica no banco após o teste)

## Design Notes

Parâmetro em vez de mover o check para o scheduler: mantém um único ponto de contagem (mesma query) e um teto defensivo no service para qualquer chamador futuro. Os pré-checks de 2/dia nos handlers ficam como estão (redundantes, mas inofensivos).

Linhas de 08–11/09/2026 feitas em `prr-rtc` estão gravadas como `producao`; correção de dados é operação separada (Ask First), fora do código.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./services/ ./handlers/` -- expected: sem erros
- `cd backend && DATABASE_URL=<.env> go test ./services/ -run 'RFB' -v` -- expected: PASS, sem SKIP

## Suggested Review Order

**Limite diário por chamador**

- Ponto de entrada: os dois limites nomeados e por que o de 1 saiu de dentro do service.
  [`rfb_scheduler.go:67`](../../backend/services/rfb_scheduler.go#L67)

- Única contagem, teto vindo do chamador; erro `DAILY_LIMIT` antes de qualquer chamada à RFB.
  [`rfb_scheduler.go:117`](../../backend/services/rfb_scheduler.go#L117)

- Agendamento continua usando só 1 slot.
  [`rfb_scheduler.go:330`](../../backend/services/rfb_scheduler.go#L330)

- Botão manual: 2/dia; mensagem enganosa "slot automático" some.
  [`rfb_apuracao.go:114`](../../backend/handlers/rfb_apuracao.go#L114)

- Ressolicitar: mesmo tratamento, sem criar linha nova.
  [`rfb_apuracao.go:456`](../../backend/handlers/rfb_apuracao.go#L456)

**Ambiente por solicitação**

- Download de débitos segue o ambiente da linha, credencial só fornece client_id/secret.
  [`rfb_processor.go:95`](../../backend/services/rfb_processor.go#L95)

- Mesmo ajuste no download de créditos.
  [`rfb_creditos_processor.go:59`](../../backend/services/rfb_creditos_processor.go#L59)

**Testes**

- RFB falsa via httptest registra o prefixo (rtc/prr-rtc) de cada chamada.
  [`rfb_scheduler_test.go:22`](../../backend/services/rfb_scheduler_test.go#L22)

- Agendamento recusado, 2ª manual aceita, 3ª recusada — sem chamada extra à RFB.
  [`rfb_scheduler_test.go:144`](../../backend/services/rfb_scheduler_test.go#L144)

- Cadastro trocado depois da solicitação: download continua em `/prr-rtc/`.
  [`rfb_scheduler_test.go:188`](../../backend/services/rfb_scheduler_test.go#L188)
