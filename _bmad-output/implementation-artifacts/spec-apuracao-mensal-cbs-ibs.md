---
title: 'Apuração Mensal CBS/IBS: saldo a pagar/transportar + calendário + decadência'
type: 'feature'
created: '2026-10-05'
status: 'done'
baseline_commit: 'b998e96'
review_loop_iteration: 1
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-apuracao-mensal-cbs-ibs-plano.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** hoje o sistema só mostra o ledger bruto de IBS (CGIBS, `cgibs_lancamentos`) e CBS (RFB, `rfb_resumo`/`rfb_creditos_resumo`) sem nenhuma camada de "apuração mensal" por cima — não fica claro qual é o saldo a pagar do mês, se esse saldo já é definitivo ou ainda pode mudar (Período de Ajuste), nem se algum crédito está perto de expirar (decadência de 5 anos, confirmada por pesquisa só pro IBS — o CBS v2 vai trazer isso pronto, não replicar aqui).

**Approach:** um módulo Go compartilhado de calendário (piloto 2026 vs. real pós-2027) + extensão dos endpoints de resumo já existentes (IBS: `ResumoCGIBSHandler`; CBS: novo endpoint lendo `rfb_resumo`/`rfb_creditos_resumo`) com `status_periodo`/`data_fechamento`/saldo rotulado, mais um alerta de decadência pro IBS. Decisões já resolvidas em `spec-apuracao-mensal-cbs-ibs-plano.md` (ler primeiro, não repetir aqui): escopo IBS+CBS juntos, saldo sempre visível com rótulo prévia/definitivo, decadência só IBS, suportar os 2 modos de calendário.

## Boundaries & Constraints

**Always:**
- Novo arquivo `backend/services/apuracao_calendario.go`:
  - `type ModoCalendario string` com constantes `ModoCalendarioPiloto = "piloto"` e `ModoCalendarioReal = "real"`.
  - `ResolveModoCalendario() string` lê env `APURACAO_CALENDARIO_MODO` (default `"piloto"` se vazio/inválido — 2026 é ano piloto).
  - `StatusPeriodoApuracao(pa time.Time, modo string) (status string, dataFechamento time.Time)` — `pa` é qualquer data dentro do mês de referência (normalizar pro dia 1). Retorna:
    - `"em_andamento"` se `time.Now()` ainda está dentro do mês `pa` (documentos ainda podem ser emitidos/incluídos).
    - `"em_ajuste"` se estamos no mês `pa+1`, antes de `dataFechamento`.
    - `"fechado"` se `time.Now() >= dataFechamento`.
  - `dataFechamento` no modo `piloto`: dia 26 do mês `pa+1`, hora 00:00 America/Sao_Paulo (ajuste observado nos manuais RFB: "Período de Ajuste 01-25, Concluída a partir de 26").
  - `dataFechamento` no modo `real`: último dia útil do mês `pa+1` (pular sábado/domingo E feriados nacionais fixos: 01/01, 21/04, 01/05, 07/09, 12/10, 02/11, 15/11, 25/12 — feriados móveis como Carnaval/Sexta-feira Santa/Corpus Christi e feriados estaduais/municipais do domicílio **não são calculados nesta rodada**, documentar como limitação conhecida/deferida, não é bloqueante).
  - Testes unitários cobrindo os 2 modos, as 3 transições de status, e ao menos um caso de "último dia útil" caindo num fim de semana/feriado fixo (ex. fevereiro terminando num sábado).
- `backend/handlers/cgibs_resumo.go` (`ResumoCGIBSHandler`, `GET /api/cgibs/resumo?data_ini=&data_fim=`): resposta `CGIBSResumoPeriodo` ganha 3 campos novos:
  - `status_periodo string` e `data_fechamento string` (RFC3339), calculados com `StatusPeriodoApuracao` sobre `data_fim` do range pedido (normalizado pro mês de `data_fim`).
  - `saldo_a_pagar float64` = soma de `debito_em_aberto` já calculada no range (é literalmente o que já é somado hoje — só expor com esse nome/rótulo, sem mudar a query de agregação existente).
  - `alertas_decadencia []CGIBSAlertaDecadencia` — lista agregada por operação (chave_acesso, valor, dias restantes) de lançamentos com `credito_nao_utilizado > 0` cujo `dth_lancto` já passou de 4 anos (alerta com antecedência de 1 ano antes do prazo de 5) — nova query separada, não interfere na agregação de saldo existente. `CGIBSAlertaDecadencia{ChaveAcesso, OperacaoID, Valor, DthLancto, DiasRestantes}`.
- Novo `backend/handlers/rfb_resumo_mensal.go`, `GET /api/rfb/resumo-mensal?periodo=mm-aaaa` (formato igual ao já usado em `data_apuracao`, ex. `"01/2026"`):
  - Lê `rfb_resumo`/`rfb_creditos_resumo` pelo `company_id` efetivo + `data_apuracao = periodo` (cada tabela já tem no máximo 1 linha por `(company_id, data_apuracao)` — migrations 054/096, não precisa agregar).
  - `status_periodo`/`data_fechamento` via `StatusPeriodoApuracao` (parseando `periodo` como mês de referência).
  - `saldo_a_pagar_aproximado float64` = `rfb_resumo.valor_cbs_nao_extinto − rfb_creditos_resumo.valor_cbs_extinto` (COALESCE 0 se uma das linhas não existir) — **rotular explicitamente como "aproximado"** na resposta (campo `aproximado: true` sempre presente, fixo) e em comentário no código explicando que o schema v1 só tem o corte binário extinto/não-extinto, sem a granularidade de apropriação que o v2 vai trazer.
  - Registrar rota em `main.go` (`withAuth`, sem role específica — mesmo padrão do `/api/cgibs/resumo`).
- Frontend:
  - `frontend/src/pages/CGIBSExtrato.tsx`: adicionar um card de destaque com `saldo_a_pagar`, badge de `status_periodo` ("Em andamento"/"Em ajuste — prévia até DD/MM"/"Fechado"), e uma seção (lista ou tabela simples) de `alertas_decadencia` quando não-vazia (ícone de alerta, chave de acesso truncada, valor, dias restantes).
  - `frontend/src/pages/GestaoCredIBSCBS.tsx`: adicionar uma seção nova "Apuração Mensal CBS" consumindo `GET /api/rfb/resumo-mensal?periodo=` (seletor de mês/ano), mostrando `saldo_a_pagar_aproximado` com aviso visível "valor aproximado — detalhamento completo chega com a API v2 da RFB" e o mesmo badge de `status_periodo`/`data_fechamento`.
- Testes Go (padrão do pacote `openTestDB`): `StatusPeriodoApuracao` (unitário, sem DB); `ResumoCGIBSHandler` com os campos novos (fixture com lançamento antigo pra disparar alerta de decadência); novo handler de resumo mensal CBS (fixture com `rfb_resumo`/`rfb_creditos_resumo` preenchidos e vazios).

**Never:** calcular decadência pro CBS (vem pronto na v2, não replicar); implementar feriados móveis/estaduais/municipais nesta rodada (documentar como limitação); mexer em `cgibs_lancamentos`/`rfb_resumo`/`rfb_creditos_resumo` (schema de captura, não alterar); implementar split payment estruturado; mudar o cálculo de agregação já existente em `ResumoCGIBSHandler` (só adicionar campos).

</frozen-after-approval>

## Code Map

- `backend/services/apuracao_calendario.go` (novo) — calendário compartilhado
- `backend/handlers/cgibs_resumo.go` (`ResumoCGIBSHandler`) — estender resposta
- `backend/handlers/rfb_resumo_mensal.go` (novo) — resumo mensal CBS aproximado
- `backend/main.go` — registrar `GET /api/rfb/resumo-mensal`
- `backend/migrations/054_create_rfb_resumo.sql`, `096_rfb_creditos.sql`, `131_cgibs_conta_corrente_fiscal.sql` — schema de referência (não alterar)
- `frontend/src/pages/CGIBSExtrato.tsx` — card de saldo + alertas de decadência (IBS)
- `frontend/src/pages/GestaoCredIBSCBS.tsx` — seção nova de apuração mensal (CBS)

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/apuracao_calendario.go` — `ResolveModoCalendario`, `StatusPeriodoApuracao`, cálculo de último dia útil (feriados fixos nacionais).
- [x] `backend/handlers/cgibs_resumo.go` — campos novos na resposta + query de `alertas_decadencia`.
- [x] `backend/handlers/rfb_resumo_mensal.go` — novo handler + registro da rota.
- [x] `frontend/src/pages/CGIBSExtrato.tsx` — card de saldo/status + lista de alertas.
- [x] `frontend/src/pages/GestaoCredIBSCBS.tsx` — seção de apuração mensal CBS.
- [x] Testes Go (calendário unitário + os 2 handlers com fixture).

**Acceptance Criteria:**
- Given um período cujo mês de referência já passou do dia 26 (piloto) ou do último dia útil (real), when consultado via `GET /api/cgibs/resumo`, then `status_periodo = "fechado"`.
- Given um lançamento IBS com `credito_nao_utilizado > 0` e `dth_lancto` há mais de 4 anos, when consultado o resumo do período correspondente, then ele aparece em `alertas_decadencia`.
- Given uma empresa sem nenhuma linha em `rfb_creditos_resumo` pro período, when consultado `/api/rfb/resumo-mensal`, then não quebra (COALESCE 0) e `aproximado` continua `true`.

## Design Notes

`StatusPeriodoApuracao` recebe `pa` como "qualquer data do mês de referência" (não precisa ser dia 1) pra poder ser chamada direto com `data_fim` do range pedido no resumo IBS, ou com o `periodo` parseado (`"mm/aaaa"` → primeiro dia do mês) no resumo CBS — mesma função, duas chamadas diferentes.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test -count=1 ./services/... ./handlers/... -run "Apuracao|CGIBSResumo|RFBResumoMensal"`
- `cd frontend && npx tsc --noEmit`

## Spec Change Log

**Revisão adversarial (rodada 1, patch, sem loopback):**
- Modo "real" do calendário fechava o período já às 00:00 do próprio último dia útil, cortando o prazo que a lei garante por inteiro nesse dia (só deveria fechar no dia seguinte) — corrigido extraindo `statusComLimite` (função pura, testável sem `time.Now()`) com `fechaNoInicioDoDia` diferenciando os 2 modos; testes de fronteira adicionados.
- Créditos já decaídos (>5 anos, sem limite superior na query de alerta) apareciam com `dias_restantes` negativo sem rótulo — adicionado campo `ja_decaido` na resposta e rótulo "Decaído há Nd" no frontend.
- Card de alertas de decadência não deixava claro que a lista é da empresa inteira, não do período selecionado — adicionada legenda explícita.
- `go build`/`go vet`/`go test` e `tsc --noEmit` limpos após os patches.
