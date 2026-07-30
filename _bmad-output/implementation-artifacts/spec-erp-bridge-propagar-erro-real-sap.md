---
title: 'ERP Bridge: propaga erro real do envio SAP pra tela de Histórico'
type: 'bugfix'
created: '2026-07-30'
status: 'done'
review_loop_iteration: 0
context: []
route: 'one-shot'
---

# ERP Bridge: propaga erro real do envio SAP pra tela de Histórico

## Intent

**Problem:** Quando o envio de um lote SAP falha (`enviar_batch`), o erro real (ex: HTTP 401 "API key inválida") só era logado localmente no servidor AWS do cliente via `log.error(...)` — nunca chegava ao backend FBTax. A tela "ERP Bridge — Histórico" sempre mostrava "Nenhum detalhe disponível" mesmo com 100% dos lotes falhando, exigindo acesso SSH e leitura de `journalctl` pra descobrir a causa — exatamente o que aconteceu no incidente de hoje (30/07/2026, FCCORP, chave de API regenerada e não atualizada no servidor).

**Approach:** Fazer o erro real percorrer os 3 saltos que já existem no código até o campo `erro_msg` (já persistido e exposto pelo backend, sem nenhuma mudança de Go/TypeScript necessária): `enviar_batch` agora lança uma exceção com o corpo real da resposta HTTP; `processar_sap` grava essa mensagem em `stats["sap_batch"]["erro_msg"]` na primeira falha de lote; `executar_importacao` passa esse valor pra `finalize_run`. Escopo limitado ao caminho SAP S4/HANA (o único envolvido no incidente); caminho Oracle XML legado não foi tocado.

## Suggested Review Order

**Correção principal**

- `enviar_batch` lança exceção com o corpo real da resposta (antes: mensagem genérica de `raise_for_status()`).
  [`bridge.py:354`](../../erp-bridge-aws/bridge.py#L354)

- `processar_sap` captura a mensagem na primeira falha de lote.
  [`bridge.py:783`](../../erp-bridge-aws/bridge.py#L783)

- `executar_importacao` propaga a mensagem pro `finalize_run`.
  [`bridge.py:1018`](../../erp-bridge-aws/bridge.py#L1018)

**Correção de regressão encontrada pela revisão adversarial**

- `finalize_run` tinha `if erro_msg: status = "error"` — isso forçaria uma execução majoritariamente bem-sucedida (999/1000 lotes OK, 1 com erro transitório) a aparecer como "Erro" (vermelho) em vez de "Parcial" (amarelo), assim que qualquer `erro_msg` fosse passado. Removido — o status volta a depender só das contagens (`total_erros`/`total_enviados`), como já era antes desta correção; `erro_msg` agora só complementa a mensagem, sem alterar a severidade.
  [`bridge.py:592`](../../erp-bridge-aws/bridge.py#L592)

- Mensagem vazia (`str(exc) == ""`) faria o erro voltar a ficar invisível (`if erro_msg:`/`run.erro_msg &&` tratam string vazia como falsy). Adicionado fallback `or "erro desconhecido ao enviar lote"`.
  [`bridge.py:789`](../../erp-bridge-aws/bridge.py#L789)

**Achados da revisão adversarial — registrados em `deferred-work.md`, nenhum bloqueia esta correção**

- Não cobre falhas de validação por documento (HTTP 200 com `errors`/`error_details` do backend) — só falhas de transporte (não-2xx, rede). Não era o cenário do incidente de hoje.
- Só a primeira falha de lote é capturada por execução — falhas por múltiplas causas distintas na mesma execução mostram só a primeira.
- `RuntimeError` genérico substitui `HTTPError` estruturado — perde `.response.status_code` (sem impacto hoje, único chamador só usa a mensagem).
- Truncamento de 300 caracteres nunca foi reavaliado para exibição ao usuário (adequado para os erros atuais do backend, que são JSON curto).
- Caminho Oracle XML legado (multi-servidor) mantém a mesma lacuna no nível de execução — deixado fora de escopo deliberadamente.
- Log duplicado (mensagem mais verbosa agora aparece 2x por lote com falha) — padrão pré-existente.
- Sem teste automatizado cobrindo a cadeia de propagação — consistente com a política já aceita no projeto.
