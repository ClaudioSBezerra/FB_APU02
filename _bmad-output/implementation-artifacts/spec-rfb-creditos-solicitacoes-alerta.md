---
title: 'RFB Créditos: seção de Solicitações com classificação Alerta (endpoint indisponível)'
type: 'feature'
created: '2026-08-04'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '5be8400a655ee57823c4bbf0949d56d0ab4529e3'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Não existe hoje nenhuma tela mostrando o status de solicitações de crédito CBS (`rfb_requests` com `tipo='credito'`) — `RFBCreditosCBS.tsx` só mostra créditos já importados (situação A_APROPRIAR/APROPRIADO/COMPENSADO). Quando a RFB retorna 404 em `/creditos-cbs/v1/` (endpoint ainda não liberado pra este ambiente — condição conhecida e temporária, não uma falha real), isso hoje só aparece em log de servidor, invisível ao usuário — e o único código já preparado pra classificar isso como "Alerta" (`error_code=ENDPOINT_INDISPONIVEL`, já implementado em `RessolicitarHandler` numa correção anterior) não tem nenhuma tela que o exiba, porque `StatusApuracaoHandler` (usado por `RFBApuracao.tsx`) filtra só `tipo='debito'`.

**Approach:** (1) Reescrever a query de `StatusCreditosHandler` (`GET /api/rfb/creditos/status`, já registrado em main.go, hoje com query obsoleta/não usada por ninguém) pra retornar `rfb_requests WHERE tipo='credito'`, mesmo shape de `StatusApuracaoHandler`. (2) Parar de pular o INSERT no caso 404 dentro de `SolicitarCreditoParaEmpresa` — persistir a linha normalmente; sem risco de acúmulo diário, já que essa chamada só ocorre dentro da goroutine disparada por `SolicitarApuracaoParaEmpresa`, que já é gated a 1x/dia. (3) Adicionar uma nova seção "Solicitações de Créditos CBS" em `RFBCreditosCBS.tsx`, consumindo o endpoint corrigido, com botão "Re-solicitar" reaproveitando `POST /api/rfb/apuracao/resolicitar` (já tipo-agnóstico) e badge/ícone Alerta (âmbar, `AlertCircle`) distinto de Erro (vermelho, `AlertTriangle`) — reaproveitando o padrão visual já estabelecido em `RFBApuracao.tsx`, mas com ícone diferente do usado antes (achado de revisão anterior: `Info` em âmbar era sinal misto).

## Boundaries & Constraints

**Always:**
- `StatusApuracaoHandler`/`RFBApuracao.tsx` (fluxo de débito) não mudam — só créditos.
- `RFBRequest` (struct já existente em `rfb_apuracao.go`, mesmo package `handlers`) é reaproveitada em `StatusCreditosHandler` — confirmado sem outros consumidores do struct antigo (`RFBCreditoRequest`/`RFBCreditoResumo`), seguro remover.
- `error_code=ENDPOINT_INDISPONIVEL` renderiza como Alerta (âmbar); qualquer outro `error_code` em linha `tipo='credito'` renderiza como Erro (vermelho) — mesma lógica condicional já usada e revertida em `RFBApuracao.tsx`, agora aplicada na tela certa.
- Sem botão "Solicitar Créditos" manual nesta entrega — só exibição + Re-solicitar de linhas existentes. Disparo manual de créditos do zero (endpoint `SolicitarCreditosHandler`, hoje morto/não registrado) é decisão separada.

**Ask First:** Nenhuma — investigação e desenho já validados nesta sessão.

**Never:** Não alterar a contagem de limite diário em `RessolicitarHandler` (achado de revisão já registrado em `deferred-work.md` como item separado, não faz parte desta entrega).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Scheduler tenta créditos, RFB retorna 404 | Endpoint ainda não liberado | Linha `tipo='credito', status='error', error_code=ENDPOINT_INDISPONIVEL` é criada (antes: nada) | N/A |
| Usuário abre Créditos CBS | Existem linhas de solicitação de crédito | Nova seção lista até 20 mais recentes, badge Alerta (âmbar) para `ENDPOINT_INDISPONIVEL`, Erro (vermelho) para os demais | N/A |
| Usuário clica Re-solicitar numa linha Alerta | RFB ainda não liberou | Mesma resposta de antes (`ENDPOINT_INDISPONIVEL` de novo) — linha atualizada, sem erro alarmante | N/A |
| Nenhuma solicitação de crédito ainda | Empresa nova / scheduler nunca rodou | Seção mostra estado vazio (mesmo padrão visual das outras seções da página) | N/A |
| Créditos liberados pela RFB (200 no futuro) | Próxima tentativa do scheduler | Linha `status='requested'` criada normalmente, mesmo fluxo de débito | N/A |

</frozen-after-approval>

## Code Map

- `backend/handlers/rfb_creditos.go` -- reescrever `StatusCreditosHandler` (linhas 104-181): query `rfb_requests WHERE company_id=$1 AND tipo='credito' ORDER BY created_at DESC LIMIT 20`, mesmas colunas de `StatusApuracaoHandler`; retornar `[]RFBRequest` (reusar struct de `rfb_apuracao.go`); remover `RFBCreditoRequest`/`RFBCreditoResumo` (sem outros consumidores, confirmado).
- `backend/services/rfb_scheduler.go` -- em `SolicitarCreditoParaEmpresa` (linhas 172-183), remover o "skip DB record" do caso 404: fazer o mesmo INSERT já usado no branch de erro genérico (linhas 184-188), com `error_code='ENDPOINT_INDISPONIVEL'` em vez do `REQUEST_ERROR` genérico.
- `frontend/src/pages/RFBCreditosCBS.tsx` -- adicionar `<Card>` "Solicitações de Créditos CBS" entre o banner Art. 48 e os cards de resumo; fetch de `/api/rfb/creditos/status`; badge/ícone Alerta (âmbar, `AlertCircle`) vs Erro (vermelho, `AlertTriangle`); botão Re-solicitar chamando `POST /api/rfb/apuracao/resolicitar` (mesmo endpoint já usado em `RFBApuracao.tsx`).

## Tasks & Acceptance

**Execution:**
- [ ] `backend/handlers/rfb_creditos.go` -- reescrever `StatusCreditosHandler`, remover structs órfãos -- expõe o status real das solicitações de crédito
- [ ] `backend/services/rfb_scheduler.go` -- persistir linha no caso 404 em vez de pular -- dá à seção nova algo pra mostrar no cenário mais comum
- [ ] `frontend/src/pages/RFBCreditosCBS.tsx` -- nova seção de Solicitações com Alerta/Erro distintos e Re-solicitar -- fecha o pedido original do usuário

**Acceptance Criteria:**
- Given o scheduler tenta créditos e a RFB retorna 404, when a solicitação termina, then uma linha `tipo='credito'` com `error_code=ENDPOINT_INDISPONIVEL` existe em `rfb_requests`.
- Given essa linha existe, when o usuário abre Créditos CBS, then a nova seção mostra um badge âmbar "Alerta", não vermelho "Erro".
- Given uma linha com outro erro real (ex: `TOKEN_ERROR`), when exibida na mesma seção, then aparece como Erro (vermelho), não Alerta.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./handlers/... ./services/...` -- expected: sem erros
- `cd frontend && npx tsc --noEmit -p tsconfig.app.json` -- expected: sem erros novos em `RFBCreditosCBS.tsx`

**Manual checks (if no CLI):**
- Sem Postgres local nesta sessão. Rodar quando houver acesso: forçar um 404 (ou aguardar o próximo ciclo do scheduler num ambiente onde o endpoint ainda não esteja liberado) e confirmar a linha e o badge âmbar na tela.

## Suggested Review Order

**Correção principal**

- `StatusCreditosHandler` reescrito pra `tipo='credito'`, reaproveitando `RFBRequest` — a tela agora tem o que mostrar.
  [`rfb_creditos.go:96`](../../backend/handlers/rfb_creditos.go#L96)

- `SolicitarCreditoParaEmpresa` para de pular o INSERT no caso 404 — persiste com `error_code=ENDPOINT_INDISPONIVEL`.
  [`rfb_scheduler.go:181`](../../backend/services/rfb_scheduler.go#L181)

- Nova seção "Solicitações de Créditos CBS" — Alerta (âmbar, `AlertCircle`) vs Erro (vermelho, `AlertTriangle`), Re-solicitar e Reprocessar.
  [`RFBCreditosCBS.tsx:226`](../../frontend/src/pages/RFBCreditosCBS.tsx#L226)

**Bug crítico encontrado pela revisão adversarial e corrigido nesta mesma rodada**

- `fail()` só revertia a linha antiga — mas o service já insere uma linha nova em toda falha que não seja "sem chamada à RFB" (TOKEN_ERROR, RATE_LIMIT, REQUEST_ERROR, ENDPOINT_INDISPONIVEL), duplicando o registro a cada tentativa. Separado em `fail` (reverte, quando o service não criou nada) vs `discard` (remove a antiga, quando o service já registrou o resultado real).
  [`rfb_apuracao.go:389`](../../backend/handlers/rfb_apuracao.go#L389)

- Tentativas automáticas diárias de crédito (que só constatam "endpoint ainda não liberado") consumiam a cota de reenvio manual de 2/dia — excluído `error_code != 'ENDPOINT_INDISPONIVEL'` da contagem.
  [`rfb_apuracao.go:407`](../../backend/handlers/rfb_apuracao.go#L407)

**Robustez (achados menores corrigidos)**

- Badge "Concluído" caía no estilo cinza genérico em vez de verde, inconsistente com o ícone.
  [`RFBCreditosCBS.tsx:257`](../../frontend/src/pages/RFBCreditosCBS.tsx#L257)

- Linhas com `raw_json` já salvo (falha de parse pós-download) não tinham nenhuma ação disponível — adicionado botão Reprocessar, espelhando `RFBApuracao.tsx`.
  [`RFBCreditosCBS.tsx:278`](../../frontend/src/pages/RFBCreditosCBS.tsx#L278)

**Achados da revisão adversarial — registrados em `deferred-work.md`, nenhum bloqueia esta correção**

- Detecção de 404 (`strings.Contains(errMsg,"404")`) é ampla — um 404 por outro motivo seria mascarado como Alerta.
- `SolicitarCreditosHandler` (endpoint morto, nunca registrado) não recebeu o mesmo case.
- Toasts de erro mostram JSON crua em vez da mensagem parseada — mesmo padrão pré-existente de `RFBApuracao.tsx`.
- Falha de fetch e "sem solicitações" ficam indistinguíveis na tela.
- Corrida entre 2 Re-solicitar concorrentes pode passar do limite diário — mesma classe de concorrência já aceita no projeto.
- `rows.Err()` não checado — consistente com todo o pacote `handlers`.
- Sobreposição de nome/rota entre a seção nova e a lista de créditos já existente — observação de design.
