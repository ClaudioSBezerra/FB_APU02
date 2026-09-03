---
title: 'RFB: aceitar HEAD no webhook (RFB descartava entrega ao receber 405)'
type: 'bugfix'
created: '2026-09-03'
status: 'done'
route: 'one-shot'
---

# RFB: aceitar HEAD no webhook (RFB descartava entrega ao receber 405)

## Intent

**Problem:** Em 8 tentativas diárias consecutivas (26/08 a 02/09/2026), toda solicitação de débito CBS autenticava e era aceita pela RFB (HTTP 201, tíquete gerado), mas o webhook de callback nunca chegava — sempre timeout de 5h. Credencial e nome do campo JSON (`urlRetorno`) já haviam sido descartados como causa por evidência direta. Investigação nos logs de acesso do nginx (não só logs do app) achou a causa: a própria RFB faz um `HEAD /api/rfb/webhook` no mesmo segundo em que aceita a solicitação — em 5 dias diferentes, sempre 405 (nosso handler só aceitava `POST`), e o `POST` real nunca chegou em nenhum desses dias.

**Approach:** `RFBWebhookHandler` passa a responder `HEAD` com 200 vazio (sem ler corpo, sem validar assinatura/payload), antes do restante da lógica — que continua exatamente igual para `POST`. Hipótese com forte respaldo em log, mas não confirmada pela RFB (documentado no código); validação real só após a próxima solicitação agendada.

## Suggested Review Order

- Branch novo, único ponto alterado — responde 200 pra HEAD antes de qualquer outra lógica do handler, com log dedicado pra rastrear futuras ocorrências.
  [`rfb_apuracao.go:586`](../../backend/handlers/rfb_apuracao.go#L586)
