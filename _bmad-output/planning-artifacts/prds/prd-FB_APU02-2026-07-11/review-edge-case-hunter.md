# Edge Case Hunter — PRD "Carga de Pagamentos SAP S/4HANA e Evolução da Conciliação CBS/IBS"

**Método:** percurso mecânico de cada FR, enum e cruzamento combinatório do PRD + addendum, confrontado com o código existente (`backend/handlers/rfb_pagamentos_fornecedores.go`, `backend/migrations/110_pagamentos_fornecedores.sql`, `111_pagamentos_imports.sql`). Reporta apenas ramificações sem tratamento explícito no documento — não avalia qualidade, tom ou completude geral.

Documentos-fonte:
- `/home/claudiobezerra/projetos/FB_APU02/_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`
- `/home/claudiobezerra/projetos/FB_APU02/_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/addendum.md`

---

## 1. Arquitetura da consulta de conciliação é orientada a pagamento, não a crédito

`cteBase` em `rfb_pagamentos_fornecedores.go:52-113` monta `pagamentos_agg` a partir de `FROM pagamentos_fornecedores` e só depois faz `LEFT JOIN rfb_creditos`. Ou seja, **uma chave só aparece na tela/sumário se já existir ao menos uma linha em `pagamentos_fornecedores`**.

- FR-4 determina que `EM_ABERTO` e `NAO_LOCALIZADO` **não geram linha** em `pagamentos_fornecedores` (só ficam no log de FR-7).
- FR-8 afirma que "um item `NAO_LOCALIZADO` no ciclo mais recente de sincronização exibe a data da última tentativa" — ou seja, pressupõe que esse item aparece na tela `/rfb/pagamentos-fornecedores`.
- Essas duas afirmações são **incompatíveis com a query atual**: um crédito RFB sem nenhum pagamento nunca entra em `pagamentos_agg`, logo nunca chega ao `LEFT JOIN rfb_creditos`, logo nunca aparece nem no sumário nem na lista — independentemente de quantas tentativas de sincronização o log registrar.
- O PRD não especifica a mudança de direção da query (ex.: partir de `rfb_creditos` com `LEFT JOIN` para pagamentos, incluindo créditos sem nenhum pagamento) necessária para que FR-8 seja realizável. Nenhuma FR trata explicitamente "crédito RFB existe, zero pagamentos de qualquer origem" como um caminho a exibir.

## 2. Combinação `paymentStatus` × `matchType` × `origem`

- O par `matchType` só é documentado com 3 valores (`CHAVE`, `FALLBACK`, `NAO_LOCALIZADO`), mas não há indicação de qual `matchType` acompanha `paymentStatus = EM_ABERTO` (fatura localizada por `CHAVE`, mas sem pagamento) vs. `paymentStatus = NAO_LOCALIZADO` (fatura não localizada, ver `matchType = NAO_LOCALIZADO`). FR-4/FR-8 não diferenciam o texto de "motivo" exibido para esses dois casos, embora sejam semanticamente distintos ("sabemos da fatura, ainda não paga" vs. "não achamos nada").
- Não há tratamento para o caso `paymentStatus = PAGO_TOTAL`/`PAGO_PARCIAL` combinado com `matchType = NAO_LOCALIZADO` — combinação logicamente inconsistente que a spec SAP não descarta explicitamente e o PRD não menciona como caso a filtrar/alertar caso a API devolva essa combinação inesperada.
- `origem = 'csv'` nunca carrega `payment_status`/`match_type` (campos novos do addendum, populados só pela sincronização SAP). FR-8 não diz o que a tela exibe para a coluna "origem e motivo" quando o item é 100% `csv` — presume-se "nada", mas isso não está escrito, e o mesmo item pode ter parcelas `csv` E `sap_api` simultaneamente (ver item 3), caso em que não há regra de qual `match_type`/`payment_status` prevalece para exibição em nível de chave agregada (a UI hoje agrega por `chave_doc`, uma linha por chave, não por parcela).

## 3. FR-4 (extinção por valor) com parcelas de origens diferentes para a mesma chave

- A chave de dedup (FR-3) é `(company_id, chave_doc, num_doc_pagamento)`. Uma parcela CSV legada sem `num_doc_pagamento` preenchido (o próprio addendum §C reconhece isso: "dados de CSV legado podem não ter `num_doc_pagamento` preenchido") **não colide** com uma parcela SAP que descreve o mesmo pagamento real. Resultado: o mesmo pagamento é contado duas vezes na soma usada pela regra de extinção de FR-4 (`SUM(valor_pagamento)` em `pagamentos_agg`), inflando `total_pago` e podendo fechar um crédito como `extinto` antes do débito real ser extinto — o inverso do risco de fallback ambíguo que o PRD já mitiga em FR-8/SM-C1, mas por uma via diferente (duplicidade CSV↔SAP sem dedup) que não tem mitigação equivalente.
- FR-9 só cobre "conflito de valor **para a mesma chave de deduplicação**" (mesmo `num_doc_pagamento`/`clearingDocument`). Não cobre o caso em que CSV e SAP descrevem o mesmo pagamento **sob identificadores diferentes** (ou CSV sem identificador), que é justamente o caso realista descrito no addendum.

## 4. FR-9: CSV sozinho já excede o valor da nota, depois SAP retorna mais pagamentos

- Nenhuma FR define um teto (`total_pago` não pode exceder `valor_nota`) nem uma ação quando isso ocorre. Se um analista lançou via CSV um valor que já cobre 100% da nota (por exemplo incluindo juros/multa no campo `valor_pagamento`) e, em ciclo posterior, a sincronização SAP localiza pagamentos adicionais para a mesma chave (via `clearingDocument` distinto do que o CSV registrou), o sistema simplesmente soma mais uma linha — sem verificação de overpayment, sem alerta, sem reavaliação do crédito já fechado como `extinto`.
- Não há regra sobre o que acontece quando um crédito **já fechado como `extinto`** (por CSV) recebe posteriormente dados SAP conflitantes (ex.: SAP aponta `PAGO_PARCIAL`, não `PAGO_TOTAL`, para a mesma chave que o CSV já tinha marcado como quitada). FR-9's `[ASSUMPTION]` só resolve conflito de **valor no mesmo registro de dedup**, não o caso de origens divergindo sobre o status geral da chave depois que uma decisão de extinção já foi tomada.

## 5. FR-6: mapeamento `company_id` ↔ N `BUKRS` — falha parcial

- Não há definição de escopo da chamada `SearchPayments` por `BUKRS`: se o lote é montado por `company_id` (agregando chaves de N `BUKRS`) ou se há uma chamada por `BUKRS`. Isso determina o comportamento de falha parcial, que o PRD não resolve:
  - Se **1 de N** `BUKRS` falhar (ex.: credencial ou instância daquele código de empresa fora do ar) enquanto os demais respondem, FR-7 só define status `sucesso`/`parcial`/`falha` no nível da execução — não diz se as chaves do `BUKRS` que falhou são marcadas como falha isolada (retry independente) ou se toda a execução da empresa é considerada `parcial`/reagendada por igual.
  - Não há retry por `BUKRS` individual descrito — FR-2 descreve retry por lote de chaves, não por código de empresa SAP.
- A chave de dedup de FR-3 (`company_id, chave_doc, num_doc_pagamento`) não inclui `BUKRS`/`companyCode`. Não está confirmado que `clearingDocument` (usado como `num_doc_pagamento`) é único apenas dentro de um `BUKRS` — se dois `BUKRS` do mesmo `company_id` gerarem, por coincidência, o mesmo número de documento de compensação para chaves diferentes, o upsert idempotente (FR-3) colidiria incorretamente entre os dois códigos de empresa.

## 6. Janela de disponibilidade 7h–22h — retry em voo quando a janela fecha

- NFR (§9) diz que o disparo fora da janela "aguarda o próximo horário disponível em vez de falhar repetidamente" — mas não trata o caso em que uma tentativa (ou seu backoff exponencial de FR-2) já está agendada/em execução e o horário de corte (22h) chega no meio dela:
  - Uma resposta 429 às 21:59 dispara backoff que agenda o próximo retry para 22:03 — não há regra dizendo se esse retry deve ser adiado para o próximo dia útil às 7h ou se prossegue mesmo fora da janela.
  - Não há tratamento para requisições HTTP já em voo no momento do fechamento da janela (presumivelmente devem apenas concluir, mas isso não é dito).
- "Dias úteis" não é definido com precisão de calendário: não há menção a feriados nacionais/regionais. Uma apuração concluída em feriado (dia de semana, portanto "útil" por uma checagem ingênua de fim de semana) dispararia tentativas contra uma API que a spec SAP já avisa que pode estar em janela de manutenção — sem uma regra de calendário de feriados, o sistema poderia esgotar tentativas e gerar alerta de falha recorrente falso-positivo (FR-7).

## 7. Disparo automático (FR-1) — idempotência do próprio gatilho

- FR-3 garante upsert idempotente na gravação de pagamentos, mas nenhuma FR trata o disparo em si: se o webhook/processamento RFB que grava `rfb_creditos` for reprocessado (reentrega, retry de infraestrutura) e `onDBConnected`/`rfb_processor.go` rodar novamente para a mesma apuração, FR-1 dispara uma nova sincronização para as mesmas chaves. O resultado final seria idempotente graças a FR-3, mas não há menção a suprimir a chamada redundante à API SAP (custo de rate limit, SM-C2) nem a um identificador de execução (`sap_sync_runs`) que prevença duplo registro de execução para a mesma apuração.

## 8. Inconsistência de schema entre §7 (SM-2) e o addendum

- SM-2 mede "contagem em `pagamentos_imports` com `origem = 'csv'`" — mas a tabela real `pagamentos_imports` (migration 111) **não tem coluna `origem`**; todas as suas linhas já são exclusivamente CSV por definição (é a tabela de log de import manual). O addendum §C adiciona `origem` apenas em `pagamentos_fornecedores`, não em `pagamentos_imports`. A métrica SM-2, como escrita, não é computável sem alterar o schema de `pagamentos_imports` (não previsto em nenhuma FR) ou trocar a fonte da métrica para `pagamentos_fornecedores WHERE origem='csv'` (que mede linhas, não uploads/eventos de import).

## 9. FR-5: teste de conexão não cobre granularidade de `BUKRS`

- "Testar conexão" (FR-5) valida credenciais OAuth2 client credentials de forma genérica. Com FR-6 permitindo N `BUKRS` por empresa, um teste de conexão bem-sucedido não garante que cada `BUKRS` individualmente cadastrado é válido/acessível — um `BUKRS` digitado errado só seria descoberto no primeiro ciclo real de sincronização (FR-1), não no cadastro (UJ-3 descreve o "clímax" como o teste retornando sucesso e a próxima apuração já disparando sync — sem prever esse tipo de falha tardia).

---

## Resumo (achados sem tratamento)

| # | Área | Lacuna |
|---|---|---|
| 1 | Arquitetura de conciliação | Query atual é payment-driven; créditos sem nenhum pagamento nunca aparecem, contradizendo FR-8 |
| 2 | `paymentStatus`×`matchType`×`origem` | Combinações EM_ABERTO/NAO_LOCALIZADO × matchType não diferenciadas; combinação logicamente inválida (PAGO_x + matchType NAO_LOCALIZADO) não tratada |
| 3 | FR-4 × múltiplas origens | Parcela CSV sem `num_doc_pagamento` não deduplica contra parcela SAP do mesmo pagamento real → dupla contagem no critério de extinção |
| 4 | FR-9 overpayment | Sem teto/alerta quando `total_pago` excede `valor_nota`; sem regra para reavaliar crédito já `extinto` quando SAP diverge depois |
| 5 | FR-6 múltiplos BUKRS | Falha parcial (1 de N BUKRS) sem regra de retry/status; dedup key sem BUKRS pode colidir entre códigos de empresa |
| 6 | Janela 7h-22h | Retry/backoff em voo no fechamento da janela sem regra; "dias úteis" não considera feriados |
| 7 | Idempotência do disparo | Reentrega/reprocessamento do webhook RFB pode redisparar sync completo sem supressão |
| 8 | SM-2 | Métrica referencia coluna `origem` inexistente em `pagamentos_imports` |
| 9 | FR-5 teste de conexão | Não valida BUKRS individualmente, só o nível OAuth2 |
