---
title: 'Credenciais RFB: padrão Produção Restrita + aviso ao usar Produção'
type: 'bugfix'
created: '2026-09-08'
status: 'done'
route: 'one-shot'
---

# Credenciais RFB: padrão Produção Restrita + aviso ao usar Produção

## Intent

**Problem:** A captura de débitos CBS ficou quebrada de julho a 08/09/2026 (~2 meses) porque o campo `ambiente` da credencial estava como `producao` (URL `rtc`) em vez de `producao_restrita` (`prr-rtc`). A falha é silenciosa e cara: o token autentica normalmente (endpoint igual nos dois ambientes) e o gateway de produção aceita a solicitação com HTTP 201 + tíquete válido — mas nunca processa nem chama o webhook, porque a Apuração Assistida de 2026 só existe no Ambiente de Produção Beta/Restrita. Sintoma: solicitação presa em `requested` até o watchdog de 5h abortar por TIMEOUT, sem erro em lugar nenhum. Ao corrigir o ambiente, a mesma solicitação completou em 16 segundos.

**Approach:** Duas defesas na tela de Credenciais RFB: (1) o padrão de credencial nova passa de `producao` para `producao_restrita`, eliminando o caminho mais fácil para o valor errado (cadastro do zero ao renovar); (2) um aviso destacado quando o ambiente é Produção — exibido tanto no formulário de edição quanto **na visualização**, já que "só abrir a tela e não notar" foi exatamente o cenário que durou meses. O aviso não bloqueia o salvamento: em 2027+ Produção pode passar a ser o correto.

## Suggested Review Order

- Componente do aviso: `role="alert"` para leitor de tela, variantes `dark:` (o projeto usa `darkMode: class`), e texto que instrui a ação em vez de só descrever o problema.
  [`RFBCredentials.tsx:13`](../../frontend/src/pages/RFBCredentials.tsx#L13)

- Padrão do formulário — o comentário explica que este default só vale para credencial nova, e por que isso importa.
  [`RFBCredentials.tsx:70`](../../frontend/src/pages/RFBCredentials.tsx#L70)

- Aviso na visualização (não só na edição) — correção de um furo apontado na revisão adversarial: sem isto, quem apenas abre a tela com o ambiente errado continua sem ver nada.
  [`RFBCredentials.tsx:361`](../../frontend/src/pages/RFBCredentials.tsx#L361)
