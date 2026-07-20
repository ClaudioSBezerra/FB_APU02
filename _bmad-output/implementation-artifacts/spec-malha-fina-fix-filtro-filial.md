---
title: 'Malha Fina: corrige coluna do filtro de filial (invertida para NF-e Saídas)'
type: 'bugfix'
created: '2026-07-20'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: 'af2910d6920e43e4c08532c6864fec284f91f8f6'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O filtro de filial (`filial_cnpj`, o seletor global "minhas filiais") em `malhaFinaList` sempre compara contra `rd.ni_adquirente`, mas essa coluna só representa "a própria empresa" para `rfb_creditos` (a empresa é a adquirente de uma compra). Para `rfb_debitos` — a tabela da aba "NF-e Saídas", a única habilitada hoje (`rfbDisponivel: true`) — a empresa é a **emitente** do documento de venda, então a coluna certa é `ni_emitente`. Confirmado por evidência independente: o campo vem direto do JSON da própria API da RFB (`niEmitente`/`niAdquirente`, papéis fixos do documento fiscal, não redefinidos por tabela), o schema local de `nfe_saidas` (`emit_cnpj` = a própria empresa em vendas), e um comentário já existente no código (`rfb_pagamentos_fornecedores.go`) confirmando o papel espelhado para créditos. Resultado prático: o filtro de filial provavelmente nunca funcionou na aba "NF-e Saídas" — filtra a lista de filiais da empresa contra a coluna do cliente externo.

**Approach:** Derivar `filialCol` internamente em `malhaFinaList`, a partir do `sourceTable` já recebido (`sourceTable == "rfb_debitos"` → `"ni_emitente"`; caso contrário → `"ni_adquirente"`), usado só no filtro `filial_cnpj` — nada mais muda. Ajuste feito em relação ao plano original (que propunha um parâmetro `filialCol string` independente, mesmo padrão de `sourceTable`/`situacaoCol`): como o valor é 100% determinado por `sourceTable`, um parâmetro separado permitiria um call site futuro passar os dois desalinhados e compilar/rodar silenciosamente contra a coluna errada — exatamente a classe de bug que esta spec corrige. Derivar internamente elimina essa possibilidade. Nenhum call site precisa mais passar `filialCol` — os 3 handlers continuam passando só `sourceTable`/`situacaoCol`, como já faziam antes desta correção.

## Boundaries & Constraints

**Always:**
- O filtro de busca livre por `emit_cnpj` (campo "CNPJ Emitente" na UI) continua fixo em `rd.ni_emitente` — esse já está correto e consistente nas 3 abas (sempre mostra o emitente real do documento fiscal, seja quem for), não é tocado por esta spec.
- Nenhuma mudança de frontend — `filial_cnpj` já é montado e enviado da mesma forma pelas 3 abas; só a coluna usada no backend muda por tipo.
- `MalhaFinaResumoFromMV`/`mv_malha_fina_resumo` não usam filtro de filial hoje — não precisam de mudança.

**Ask First:** Nenhuma — já confirmado com o usuário que o achado deve ser corrigido.

**Never:** Não alterar o filtro `emit_cnpj`/`ni_emitente` da busca livre — é um conceito diferente (emitente do documento) do filtro de filial (a própria empresa).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Filtrar por filial em "NF-e Saídas" | Usuário seleciona 1+ filiais no seletor global | Filtra `rd.ni_emitente IN (filiais selecionadas)` — agora casa de verdade | N/A |
| Filtrar por filial em "NF-e Entradas"/"CT-e" | Usuário seleciona 1+ filiais | Continua filtrando `rd.ni_adquirente IN (...)`, comportamento inalterado (já correto) | N/A |
| Busca por "CNPJ Emitente" em qualquer aba | Usuário digita um CNPJ no campo de busca | Continua filtrando `rd.ni_emitente LIKE ...`, inalterado | N/A |

</frozen-after-approval>

## Code Map

- `backend/handlers/malha_fina.go` -- dentro de `malhaFinaList`, derivar `filialCol` localmente a partir de `sourceTable` (`"ni_emitente"` se `sourceTable == "rfb_debitos"`, senão `"ni_adquirente"`); usar essa variável local no lugar do `ni_adquirente` hardcoded na linha do filtro `filial_cnpj` (`where += fmt.Sprintf(" AND rd.%s IN (%s)", filialCol, ...)`); atualizar o comentário da seção do filtro (hoje dizia "CNPJs da empresa (ni_adquirente)", generalização incorreta herdada do caso créditos). Nenhum parâmetro novo na assinatura da função, nenhum call site muda.

## Tasks & Acceptance

**Execution:**
- [x] `backend/handlers/malha_fina.go` -- derivar `filialCol` internamente em `malhaFinaList` a partir de `sourceTable`, corrigir a linha do filtro `filial_cnpj` -- corrige o filtro de filial que provavelmente nunca funcionou em NF-e Saídas, sem abrir espaço para um call site futuro passar `sourceTable`/`filialCol` desalinhados

**Acceptance Criteria:**
- Given uma empresa com múltiplas filiais e dados reais em `rfb_debitos`, when o usuário seleciona uma filial específica na aba "NF-e Saídas", then a lista filtra corretamente por `ni_emitente`, retornando só os documentos daquela filial.
- Given a mesma seleção de filial, when o usuário troca para a aba "NF-e Entradas" ou "CT-e", then o comportamento é idêntico ao de antes desta correção (já filtrava certo por `ni_adquirente`).

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: sem erros de compilação
- `cd backend && go vet ./handlers/...` -- expected: limpo

**Manual checks (if no CLI):**
- Sem Postgres local disponível nesta sessão — verificação só estática. Rodar manualmente quando houver acesso a banco real: selecionar uma filial na aba "NF-e Saídas" e confirmar que a lista muda (hoje, suspeita-se que não mude nunca, já que `ni_adquirente` nunca bate com o CNPJ da própria empresa).

## Suggested Review Order

**Correção principal**

- Derivação interna de `filialCol` em `malhaFinaList` a partir de `sourceTable`, e a correção da linha do filtro `filial_cnpj` que passa a usar essa coluna em vez de `ni_adquirente` hardcoded.
  [`malha_fina.go:66`](../../backend/handlers/malha_fina.go#L66)

**Ajuste em relação ao plano original**

- O plano previa um parâmetro `filialCol string` explícito (mesmo padrão de `sourceTable`/`situacaoCol`). A revisão adversarial apontou que, como o valor é 100% determinado por `sourceTable`, um parâmetro independente permitiria um call site futuro passar os dois desalinhados e quebrar silenciosamente — exatamente a classe de bug corrigida aqui. Optou-se por derivar `filialCol` internamente; nenhum call site precisou mudar.

**Achados da revisão adversarial — registrados em `deferred-work.md`, nenhum bloqueia esta correção**

- Mesmo bug de coluna existe no frontend (`MalhaFinaPanel.tsx:580`, resolução de apelido de filial sempre via `ni_emitente`) — dormente hoje pelas mesmas razões (`rfbDisponivel: false` nas abas de crédito), fora do escopo desta spec (frozen intent exclui frontend).
- Demais achados (devolução/papéis invertidos, filial nova fora do seletor, sem validação server-side de pertencimento, NULL/vazio excluído silenciosamente do filtro, sobreposição `emit_cnpj`/`filial_cnpj` em NF-e Saídas, ausência de teste automatizado) — todos de baixa severidade ou pré-existentes, detalhados em `deferred-work.md`.
