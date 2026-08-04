---
title: 'RFB: "Re-solicitar" não reenvia à Receita Federal (fica órfã até timeout falso)'
type: 'bugfix'
created: '2026-08-04'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '920d6850dc1481aeb9e424212beaf26d700b3c3b'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O botão "Re-solicitar" (tela RFB Apuração) só reseta a linha de `rfb_requests` para `status='pending'` (`RessolicitarHandler`, `backend/handlers/rfb_apuracao.go:338-376`) — nunca rechama a API da RFB. Nenhum job consome `status='pending'`; a linha fica órfã até o watchdog de 5h (`AbortStuckRFBRequests`) marcá-la `error/TIMEOUT` com mensagem enganosa ("sem resposta por mais de 5 horas" — nada foi enviado). Confirmado em produção: `docker compose logs api --since 2026-07-08 | grep abortada` mostrou 1 ocorrência real (`2026/07/30 16:51:10`). O próprio doc comment do handler já afirma algo falso ("permitindo que o scheduler a reenvie") — o scheduler nunca varre por `status='pending'`.

**Approach:** `RessolicitarHandler` passa a: (1) claim atômico via `UPDATE ... RETURNING COALESCE(tipo,'debito')` (só avança se `status='error' AND raw_json IS NULL`, evita duplo-envio em duplo-clique); (2) checar limite diário por tipo (mesmo padrão de `SolicitarApuracaoHandler`/`SolicitarCreditosHandler`); (3) chamar `services.SolicitarApuracaoParaEmpresa` (tipo debito) ou `services.SolicitarCreditoParaEmpresa` (tipo credito) — ambas já fazem INSERT de uma linha nova com tiquete fresco; (4) em sucesso, deletar a linha antiga (`status='pending'`); (5) em qualquer falha, reverter a linha para `error` com o motivo real desta tentativa (nunca deixar presa em `pending`) e mapear a mensagem HTTP com o mesmo switch/case já usado nos handlers manuais.

## Boundaries & Constraints

**Always:**
- Claim atômico (`UPDATE ... WHERE status='error' AND raw_json IS NULL ... RETURNING`) antes de qualquer chamada externa — nenhuma janela onde a linha fica "reivindicada" sem seguir para sucesso ou reversão.
- Em qualquer falha após o claim, a linha volta para `status='error'` com `error_code`/`error_message` refletindo o motivo desta tentativa — nunca fica em `pending`.
- Reusar exatamente o switch/case de mapeamento de erro já usado em `SolicitarApuracaoHandler` (`rfb_apuracao.go:109-127`) e `SolicitarCreditosHandler` (`rfb_creditos.go:80-93`) — mesmas mensagens, mesmos status HTTP.
- Checagem de limite diário (2/dia por tipo) obrigatória — `SolicitarCreditoParaEmpresa` não tem checagem interna própria (diferente de débito, que já bloqueia em `todayCount >= 1` dentro do próprio service), então sem essa checagem o Ressolicitar de créditos ignoraria o limite.

**Ask First:** Nenhuma — já confirmado com o usuário.

**Never:** Não alterar `frontend/RFBApuracao.tsx`/`handleResolicitar` — já trata a resposta genericamente, independente do corpo.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Re-solicitar débito com sucesso | Linha `status='error', tipo='debito'`, slot do dia livre | Nova linha `requested` com tiquete fresco criada; linha antiga deletada; 201 | N/A |
| Re-solicitar crédito com sucesso | Linha `status='error', tipo='credito'`, < 2 tentativas hoje | Idêntico ao acima, via `SolicitarCreditoParaEmpresa` | N/A |
| Limite diário já atingido | 2ª tentativa de créditos no mesmo dia (ou débito com slot do scheduler já usado) | Linha volta para `error` com mensagem de limite; 429 | Mensagem clara, não timeout |
| Credenciais RFB removidas entre o erro original e o Re-solicitar | `rfb_credentials` sem linha ativa | Linha volta para `error/TOKEN_ERROR` ou equivalente; 400 | N/A |
| Duplo clique rápido no mesmo request | 2 chamadas concorrentes pro mesmo `request_id` | Só a 1ª claim (UPDATE) afeta linha; a 2ª recebe 404 "não encontrada, não está em erro..." | Sem duplo envio à RFB |
| Linha já tem `raw_json` (webhook já respondeu antes do erro) | `raw_json IS NOT NULL` | Claim não avança (0 rows); 404, mesma mensagem já existente | N/A |

</frozen-after-approval>

## Code Map

- `backend/handlers/rfb_apuracao.go` -- reescrever `RessolicitarHandler` (linhas 338-377): claim atômico com `RETURNING`, checagem de limite diário por tipo, chamada ao service correto, `DELETE` em sucesso ou `UPDATE`-de-volta-pra-error com mapeamento de mensagem em falha. Atualizar doc comment (linha 336-337, hoje afirma algo falso sobre o scheduler).

## Tasks & Acceptance

**Execution:**
- [x] `backend/handlers/rfb_apuracao.go` -- reescrever `RessolicitarHandler` conforme Approach -- corrige o botão "Re-solicitar" que hoje nunca reenvia à RFB e produz timeout falso após 5h

**Acceptance Criteria:**
- Given uma solicitação em `error` sem `raw_json`, when o usuário clica "Re-solicitar" e há slot disponível, then uma nova solicitação real é criada na RFB (tiquete novo) e a linha antiga some da lista.
- Given o limite diário já foi atingido, when o usuário clica "Re-solicitar", then recebe erro imediato e claro (não uma linha presa que só erra 5h depois).
- Given duas chamadas concorrentes ao mesmo `request_id`, when ambas chegam quase juntas, then só uma reivindica a linha e dispara a chamada real à RFB.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: sem erros de compilação
- `cd backend && go vet ./handlers/...` -- expected: limpo

**Manual checks (if no CLI):**
- Sem Postgres local nesta sessão — verificação só estática. Rodar manualmente quando houver acesso a banco real: forçar um erro (ex: credencial temporariamente inválida), clicar "Re-solicitar" e confirmar que uma nova linha `requested` aparece e a antiga desaparece.

## Suggested Review Order

**Correção principal**

- Claim atômico via `UPDATE ... RETURNING`, chamada ao service correto e limpeza/reversão da linha — o núcleo da correção.
  [`rfb_apuracao.go:341`](../../backend/handlers/rfb_apuracao.go#L341)

**Bug crítico encontrado pela revisão adversarial e corrigido nesta mesma rodada**

- A contagem diária (própria do handler) se autocontava — a linha sendo reenviada hoje sempre entrava no `COUNT(*)`, mascarando o limite real. Excluído `status != 'pending'`.
  [`rfb_apuracao.go:399`](../../backend/handlers/rfb_apuracao.go#L399)

- Mesma autocontagem, mais grave, dentro do limite interno de 1/dia de `SolicitarApuracaoParaEmpresa` — sem essa correção, Re-solicitar de débito nunca funcionava no mesmo dia (bloqueava sempre).
  [`rfb_scheduler.go:70`](../../backend/services/rfb_scheduler.go#L70)

- Mensagem do limite interno (1/dia) estava reusando o texto "máximo 2" do limite externo — corrigida para refletir o limite real que de fato disparou.
  [`rfb_apuracao.go:429`](../../backend/handlers/rfb_apuracao.go#L429)

**Robustez (achados menores corrigidos)**

- Guarda de método HTTP ausente — adicionada, alinhando com os handlers vizinhos do mesmo arquivo.
  [`rfb_apuracao.go:344`](../../backend/handlers/rfb_apuracao.go#L344)

- `db.Exec` de reversão-pra-erro e de limpeza da linha antiga não checavam erro — agora logam se falharem, em vez de silenciosamente deixar a linha inconsistente sem rastro.
  [`rfb_apuracao.go:385`](../../backend/handlers/rfb_apuracao.go#L385)

**Achados da revisão adversarial — registrados em `deferred-work.md`, nenhum bloqueia esta correção**

- Re-solicitar débito dispara, como efeito colateral pré-existente da função reusada, uma tentativa de crédito em segundo plano não visível ao usuário.
- Créditos com HTTP 404 (endpoint ainda não liberado pela RFB) caem no `default:` e mostram "erro" genérico em vez de uma condição conhecida — relacionado à próxima solicitação do usuário (créditos como "Alertas").
- Ordem do switch (`RATE_LIMIT` antes de `TOKEN_ERROR`) é ambígua num caso raro — padrão pré-existente, idêntico nos outros 2 handlers.
- Sem lock/transação coordenando chamadas concorrentes (Ressolicitar × Ressolicitar, Ressolicitar × scheduler) — mesma classe de concorrência já aceita em outras partes do projeto.
