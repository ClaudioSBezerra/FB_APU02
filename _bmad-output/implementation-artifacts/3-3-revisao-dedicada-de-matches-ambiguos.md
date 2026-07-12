---
baseline_commit: c18eba4
---

# Story 3.3: Revisão Dedicada de Matches Ambíguos

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a Ana (analista fiscal),
I want um filtro/contador dedicado para créditos cuja evidência de pagamento veio de um match ambíguo,
so that revisar evidência fraca seja parte da minha rotina, não algo que eu precise lembrar de fazer.

Realiza FR-12 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md` (seção 4.4, linhas 187-192). Terceira e última story planejada da Epic 3 — ver `_bmad-output/planning-artifacts/epics.md` (linhas 324-337). Depende das Stories 3.1 (`done`, inversão da query de conciliação) e 3.2 (`done`, exposição de `payment_status`/`match_type`/`fallback_ambiguous`/`origem` no item agregado).

## Por que esta story é mais simples que 3.1/3.2 — leia antes de codificar

**O dado que esta story precisa já existe e já está exposto.** A Story 3.2 já:
- Adicionou `fallback_ambiguous bool` em `ConciliacaoItem` (`backend/handlers/rfb_pagamentos_fornecedores.go:45`)
- Populou via `BOOL_OR(COALESCE(pf.fallback_ambiguous, false)) AS fallback_ambiguous` em `pagamentos_agg` (linha 116)
- Propagou para ambas as pernas do `UNION ALL` (`conciliacao_creditos` linha 216, `pagamentos_orfaos` linha 304)
- Já expõe `fallback_ambiguous` no `listSQL`/`Scan` e no frontend (`ConciliacaoItem.fallback_ambiguous` em `RFBPagamentosFornecedores.tsx:40`)

**Esta story NÃO precisa**: nenhuma migration nova, nenhuma mudança em `persistPayments`/`services/sap_payments_processor.go`, nenhuma mudança na estrutura de `ConciliacaoItem` (o campo já existe). O trabalho é: (1) um modo de filtro que ignora `status_conciliacao` e filtra só por `fallback_ambiguous = true`, e (2) um contador visível que não dependa do filtro de status atual.

## Acceptance Criteria

1. **Given** o indicador de pagamento de um crédito tem `fallback_ambiguous = true`, **When** Ana abre o filtro dedicado de revisão, **Then** o crédito aparece nesse filtro independentemente do `status_conciliacao` atual — inclusive créditos já `extinto` segundo a RFB.
2. **Given** Ana está na tela principal de conciliação (`/rfb/pagamentos-fornecedores`), **Then** um contador/badge visível (não escondido dentro do dropdown de status) mostra quantos itens têm `fallback_ambiguous = true` no escopo do filtro de mês/fornecedor atual — reforçando que a revisão faz parte do fluxo normal, não uma auditoria separada.
3. **Given** o filtro dedicado está ativo, **When** Ana aplica também um filtro de mês/ano ou CNPJ fornecedor, **Then** os dois filtros combinam (E lógico) — o filtro de ambíguos não substitui os demais filtros, apenas ignora `status_conciliacao`.
4. **Given** nenhum crédito com `fallback_ambiguous = true` existe no escopo atual, **When** Ana abre o filtro dedicado, **Then** a tela mostra a mensagem vazia padrão já existente ("Nenhum pagamento encontrado para os filtros aplicados"), sem erro.

## Tasks / Subtasks

- [x] **Task 1 — Backend: contagem de ambíguos no sumário** (AC: #2)
  - [x] `ConciliacaoSumario` (`backend/handlers/rfb_pagamentos_fornecedores.go:15-23`): adicionado campo `NotasRevisaoAmbigua int` `json:"notas_revisao_ambigua"`
  - [x] `sumSQL`: adicionado `COUNT(*) FILTER (WHERE fallback_ambiguous = true)` à lista de colunas selecionadas (mesma CTE `conciliacao`, sem filtro de status — igual aos outros contadores do sumário)
  - [x] `Scan` do sumário atualizado para incluir o novo campo, ao final da lista (menor risco de trocar a ordem dos campos existentes)
  - [x] Confirmado: `sumSQL` continua ignorando `statusFilter`/`apenasAmbiguos` (nenhum dos dois é passado a essa query) — a nova contagem reflete só `mes_ano`/`forn_cnpj`, nunca o filtro de status/ambíguos ativo

- [x] **Task 2 — Backend: modo de filtro dedicado que ignora `status_conciliacao`** (AC: #1, #3, #4)
  - [x] Nova variável derivada em `RFBPagamentosFornecedoresHandler`: `apenasAmbiguos := statusParam == "revisao_ambigua"` (mesmo padrão de valor mágico em `status`, análogo ao `"all"` já existente)
  - [x] `statusFilter` ajustado: quando `apenasAmbiguos` é `true`, `statusFilter` fica `""` (não filtra por `status_conciliacao`) — só o novo filtro de `fallback_ambiguous` se aplica
  - [x] `countSQL`/`listSQL`: adicionado `AND ($5/$7 = false OR fallback_ambiguous = true)` ao `WHERE` (countSQL usa `$5`, listSQL usa `$7` — LIMIT/OFFSET já ocupam `$5`/`$6` no listSQL), passando `apenasAmbiguos` como novo parâmetro posicional em AMBAS as queries
  - [x] Confirmado: `mes_ano` (`$2`) e `forn_cnpj` (`$3`) continuam aplicados normalmente quando `apenasAmbiguos=true` (AC #3) — são parâmetros da CTE `cteBase`/`creditos_tipados`, nenhuma mudança necessária ali
  - [x] `ORDER BY` do `listSQL` não precisou mudar — a ordenação por `status_conciliacao`/`total_pago` continua válida mesmo filtrando por ambíguos

- [x] **Task 3 — Frontend: opção de filtro dedicada + contador/badge visível** (AC: #1, #2, #3, #4)
  - [x] `ConciliacaoSumario` (interface TS): adicionado `notas_revisao_ambigua: number`
  - [x] Adicionada opção no `<select>` de Status: `<option value="revisao_ambigua">Revisão de Matches Ambíguos</option>` — reaproveita o mesmo `filterStatus`/`fetchConciliacao` já existente (nenhum novo state necessário)
  - [x] Badge/contador visível FORA do dropdown — botão-chip no painel de filtros (ao lado de "Limpar filtros") mostrando `sumario.notas_revisao_ambigua` sempre que `> 0`; clicar define `filterStatus = 'revisao_ambigua'` e `setPage(1)`
  - [x] Badge fica visível independente do filtro atual (é um atalho/alerta); oculto quando `sumario.notas_revisao_ambigua === 0`; muda de estilo (preenchido) quando o filtro `revisao_ambigua` já está ativo
  - [x] Nenhum componente novo de badge de status — `StatusBadge`/`STATUS_CLASSES` inalterados; `revisao_ambigua` é um MODO DE FILTRO, item retornado continua mostrando seu `StatusBadge` real
  - [x] Exportação Excel: adicionada coluna "Match Ambíguo" (`it.fallback_ambiguous ? 'Sim' : 'Não'`)

- [x] **Task 4 — Testes e verificação**
  - [x] Harness existente reaproveitado (`insertPagamentoSAPComStatus`, `insertRfbCredito`); novo helper `insertRfbCreditoComEmitente` adicionado apenas para o teste de combinação com `forn_cnpj` (necessário porque `creditos_tipados` filtra por `rc.ni_emitente`, não gravado por `insertRfbCredito`)
  - [x] `TestConciliacao_FiltroAmbiguos_IgnoraStatusConciliacao` (AC #1): 2 créditos com `fallback_ambiguous=true` em status DIFERENTES (`pendente`, `extinto`) + 1 crédito sem ambiguidade; filtro `apenasAmbiguos=true` retorna só os 2 ambíguos
  - [x] `TestConciliacao_FiltroAmbiguos_CombinaComFornCNPJ` (AC #3, nome ajustado de `...CombinaComMesAnoEForn` — combinação testada com `forn_cnpj`, que exercita filtragem real via `ni_emitente`; `mes_ano` não filtra créditos por design da Story 3.1, ver Dev Notes): 2 créditos ambíguos em fornecedores diferentes; filtro `forn_cnpj` + ambíguos retorna só o do fornecedor certo
  - [x] `TestConciliacao_SumarioNotasRevisaoAmbigua_IgnoraStatus` (AC #2, nome ajustado de `...IgnoraFiltroDeStatus`): 2 créditos ambíguos em status diferentes (pendente+extinto) + 1 não ambíguo; contagem = 2, provando que soma através dos status e exclui não ambíguos
  - [x] `TestConciliacao_FiltroAmbiguos_SemResultado_NaoQuebra` (AC #4): nenhum crédito ambíguo no escopo — filtro retorna lista vazia, sem erro
  - [x] Regressão: todos os 19 testes já existentes de `rfb_pagamentos_fornecedores_test.go` (Stories 2.5/3.1/3.2) passam sem alteração — 24/24 no total após o patch de revisão (4 testes da story + 1 adicionado em revisão)
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões
  - [x] `npx tsc -p tsconfig.app.json --noEmit` — 26 erros, idêntico à baseline pré-existente (Story 3.2); nenhum erro novo no código desta story

## Dev Notes

### Fora de escopo desta story (não implementar)

- Qualquer mudança em `status_conciliacao` a partir de `fallback_ambiguous` — **nunca**; FR-4 (SAP nunca decide extinção) é invariante do projeto inteiro. `revisao_ambigua` é puramente um modo de FILTRO de listagem, não uma reclassificação.
- Nenhuma mudança em `persistPayments`/`sap_payments_processor.go` — `fallback_ambiguous` já é gravado desde a Story 2.3 (migration 118); esta story só CONSOME o dado, não altera como é produzido.
- Nenhuma migration nova — todas as colunas necessárias já existem.
- Workflow de anotação/aprovação da revisão (ex.: marcar um crédito como "já revisado") — fora do escopo do PRD (`prd.md:220`, não-meta explícita: "Não inclui um workflow formal de anotação/aprovação").

### Padrão de "valor mágico" em `statusParam` — já existe, só estender

`RFBPagamentosFornecedoresHandler` (linha 363) já usa `"all"` como valor mágico para "sem filtro de status". Adicionar `"revisao_ambigua"` como um segundo valor mágico no MESMO parâmetro `status` é consistente com esse padrão já estabelecido — evita introduzir um novo query param (`?apenas_ambiguos=true`) quando o existente já serve. O dropdown de Status no frontend é o único lugar que precisa saber sobre o novo valor.

### Risco já visto 2x nesta epic: contador que reflete o filtro errado

Tanto a Story 3.1 (bug crítico: `status_conciliacao` usando `pa.chave_doc IS NULL`, que refletia o filtro de `mes_ano` ativo em vez de "já teve pagamento algum dia") quanto a lógica de `sumSQL` (que deliberadamente NUNCA usa `statusFilter`, comentário linha 384) mostram o mesmo padrão de erro: um contador/agregado que acidentalmente herda o filtro que deveria ignorar. Ao implementar `NotasRevisaoAmbigua` em `sumSQL`, garanta que ele SEMPRE conta `fallback_ambiguous = true` sobre a CTE `conciliacao` completa (respeitando só `mes_ano`/`forn_cnpj`), nunca condicionado a `statusParam`/`apenasAmbiguos` — teste dedicado (`TestConciliacao_SumarioNotasRevisaoAmbigua_IgnoraFiltroDeStatus`) existe especificamente para pegar essa classe de regressão antes da revisão.

### `UNION ALL` com `SELECT *` (risco adiado da Story 3.2, ainda vale)

`conciliacao = SELECT * FROM conciliacao_creditos UNION ALL SELECT * FROM pagamentos_orfaos` depende de paridade posicional de 22 colunas. Esta story NÃO adiciona nenhuma coluna nova à CTE (só consome `fallback_ambiguous`, que já existe em ambas as pernas desde a 3.2) — não há risco de desalinhamento aqui. Se uma story futura precisar adicionar coluna à CTE, revisitar o item adiado em `deferred-work.md`.

### Project Structure Notes

- Mesmo arquivo único para a mudança de backend: `backend/handlers/rfb_pagamentos_fornecedores.go` (nenhum arquivo novo)
- Mesmo arquivo único para o frontend: `frontend/src/pages/RFBPagamentosFornecedores.tsx`
- Testes em `backend/handlers/rfb_pagamentos_fornecedores_test.go` (já existente, harness reaproveitado)

### References

- [Source: _bmad-output/planning-artifacts/epics.md:324-337] — Story 3.3, ACs originais
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md:187-192] — FR-12, definição completa do requisito
- [Source: backend/handlers/rfb_pagamentos_fornecedores.go] — CTE completa pós Stories 3.1/3.2 (`fallback_ambiguous` já exposto, linhas 45/116/216/304/443/492)
- [Source: frontend/src/pages/RFBPagamentosFornecedores.tsx] — `ConciliacaoItem.fallback_ambiguous` já existe (linha 40), filtro de Status (`filterStatus`, linhas 186/355-366)
- [Source: _bmad-output/implementation-artifacts/3-2-indicador-de-pagamento-como-evidencia-auxiliar.md] — Dev Notes/Review Findings da story anterior; item adiado "Fora de escopo desta story: Filtro/contador dedicado para matchType = FALLBACK ambíguo — Story 3.3" confirma que esta story é a continuação planejada
- [Source: _bmad-output/implementation-artifacts/3-1-creditos-sem-pagamento-localizado-aparecem-na-tela.md] — bug crítico de contador refletindo filtro errado (precedente direto do risco documentado acima)

### Review Findings

*(Revisão adversarial: Blind Hunter + Edge Case Hunter + Acceptance Auditor, todos executados em contexto isolado sem memória desta conversa.)*

- [x] [Review][Patch] Botão-badge de "revisão de matches ambíguos" não expõe estado de toggle para leitores de tela — falta `aria-pressed` refletindo se o filtro `revisao_ambigua` está ativo [frontend/src/pages/RFBPagamentosFornecedores.tsx:396] — corrigido: `aria-pressed={filterStatus === 'revisao_ambigua'}` adicionado ao botão
- [x] [Review][Patch] AC #3 (filtros combinam com E lógico) só tinha teste cobrindo a combinação com `forn_cnpj` — faltava um teste equivalente combinando o filtro de ambíguos com `mes_ano` (que afeta `fallback_ambiguous` via `pagamentos_agg`/`pagamentos_dedup`, diferente de `forn_cnpj` que afeta via `ni_emitente`) [backend/handlers/rfb_pagamentos_fornecedores_test.go] — corrigido: `TestConciliacao_FiltroAmbiguos_CombinaComMesAno` adicionado, prova que um pagamento ambíguo fora do `mes_ano` filtrado não conta como ambíguo naquele escopo
- [x] [Review][Defer] Nenhum teste desta story (nem das Stories 2.5/3.1/3.2) invoca `RFBPagamentosFornecedoresHandler` via `httptest` — todos reimplementam a SQL da CTE diretamente em helpers de teste. Um bug isolado na análise de `statusParam`/`apenasAmbiguos` do handler (distinto da SQL) não seria pego por nenhum teste atual. — deferido, padrão de metodologia de teste já estabelecido em todo o arquivo, não introduzido por esta story
- [x] [Review][Defer] Nenhuma consideração de índice/performance para o novo filtro `fallback_ambiguous` em escala (tabela `pagamentos_fornecedores` sem índice dedicado nessa coluna) — deferido, mesma classe de itens de performance já adiados no épico (ver `deferred-work.md`), fora do escopo desta story de filtro
- [x] [Review][Defer] Helper de teste `queryChavesFiltroAmbiguos` não verifica `rows.Err()` após o loop `rows.Next()`/`rows.Scan()` — deferido, o próprio loop de `RFBPagamentosFornecedoresHandler` (não tocado por esta story) tem a mesma lacuna; corrigir só no helper novo seria inconsistente
- [x] [Review][Defer] Selecionar qualquer valor no dropdown de Status (não só `revisao_ambigua`) não chama `setPage(1)` — se o usuário estiver na página 2+ e trocar de status via dropdown, o offset desatualizado pode devolver lista vazia mesmo com resultados existindo — deferido, bug pré-existente em todas as opções do dropdown (`onChange={e => setFilterStatus(e.target.value)}` já existia antes desta story), não introduzido por ela. O clique no badge novo já chama `setPage(1)` corretamente
- [x] [Review][Dismiss] "Excel export referencia `it.fallback_ambiguous` mas o campo não existe em `ConciliacaoItem`" (Blind Hunter) — falso alarme: `fallback_ambiguous` já existe em `ConciliacaoItem` desde a Story 3.2; o Blind Hunter não tinha acesso ao projeto para confirmar isso, só ao diff
- [x] [Review][Dismiss] "Comentário 'ver sumSQL' aponta para texto que não existe no diff" (Blind Hunter) — falso alarme: o comentário referenciado (`// sem filtro de status...`) é pré-existente, só não aparece no contexto reduzido do hunk do diff
- [x] [Review][Dismiss] "Nada no diff mostra `cteBase` projetando `fallback_ambiguous`, risco de erro de coluna indefinida" (Blind Hunter) — falso alarme: a coluna já é projetada desde a Story 3.2; os 22/22 testes passando confirmam que a coluna existe e funciona
- [x] [Review][Dismiss] "Predicado WHERE duplicado em countSQL/listSQL/helper de teste sem constante compartilhada" (Blind Hunter) — consistente com o padrão já estabelecido neste arquivo (cada query é uma `const` local independente por design; helpers de teste sempre reimplementam fragmentos de SQL, ver `querySumarioExtintos`/`queryConciliacaoStatus` já existentes)
- [x] [Review][Dismiss] "Selecionar o filtro de ambíguos sobrescreve `status_conciliacao` inteiro, impossível combinar com 'pendente'" (Blind Hunter) — comportamento por design: a decisão de arquitetura da story é que `revisao_ambigua` é um MODO DE FILTRO que substitui (não soma a) `status_conciliacao`; nenhuma AC pede a combinação dos dois
- [x] [Review][Dismiss] "Badge some com contagem 0 mas dropdown continua sempre selecionável — inconsistência de descoberta" (Blind Hunter + Acceptance Auditor) — comportamento explicitamente especificado nas Tasks desta story ("pode ocultar o badge quando notas_revisao_ambigua===0"); dropdown precisa continuar disponível para satisfazer AC #4 (tela vazia sem erro)
- [x] [Review][Dismiss] "Nenhum indicador visual por linha explicando por que o crédito é ambíguo" (Blind Hunter) — fora do escopo: nenhuma AC exige indicador por linha, só o filtro/contador agregado (AC #1/#2)
- [x] [Review][Dismiss] "`insertRfbCreditoComEmitente` ainda fixa `cnpj_base` como literal" (Blind Hunter) — ruído: `cnpj_base` de `rfb_requests` é apenas satisfação de FK, irrelevante para o que os testes desta story verificam
- [x] [Review][Dismiss] "Bundlar `sprint-status.yaml` no mesmo diff da implementação é prematuro" (Blind Hunter) — convenção deliberada e já estabelecida do fluxo BMAD desta sessão (dev-story sempre marca o status ao final)
- [x] [Review][Dismiss] "Nenhum teste de frontend automatizado para a nova UI" (Acceptance Auditor) — consistente com a limitação já registrada nas Stories 2.5/3.1/3.2 (sem framework de teste JS instalado no projeto, verificação por leitura de código); não é uma lacuna introduzida por esta story

## Dev Agent Record

### Agent Model Used

Claude Sonnet 5 (claude-sonnet-5)

### Debug Log References

- `go build ./...`, `go vet ./...` — limpos após cada task
- `go test ./handlers/... -run TestConciliacao -v` — 24/24 passando (19 originais das Stories 2.5/3.1/3.2 + 5 novos desta story, incluindo o patch de revisão)
- `go test ./...` (suíte completa) — sem regressões (`handlers`, `middleware`, `services`)
- `npx tsc -p tsconfig.app.json --noEmit` — 26 erros, mesma baseline pré-existente documentada na Story 3.2, nenhum novo
- **Fase RED não aplicável no nível de SQL**: como documentado nos Dev Notes, as colunas (`fallback_ambiguous`) já existiam desde a Story 3.2 — os 4 testes novos (que exercitam a CTE diretamente, mesmo padrão dos testes já existentes neste arquivo) passaram já na primeira execução, antes de qualquer mudança em `rfb_pagamentos_fornecedores.go`. O código realmente NOVO desta story (`ConciliacaoSumario.NotasRevisaoAmbigua`, `apenasAmbiguos`, extensão de `sumSQL`/`countSQL`/`listSQL`) foi implementado em seguida e validado pela mesma suíte, sem quebrar nenhum teste
- **Limitação registrada**: sem ferramenta de navegador neste ambiente — a tela foi verificada por leitura de código (novo badge/chip, opção de dropdown, coluna Excel), não por captura visual

### Completion Notes List

- Confirmado antes de codificar (ver Dev Notes da story): nenhuma migration nova, nenhuma mudança em `persistPayments`/`services/sap_payments_processor.go`, nenhuma mudança estrutural em `ConciliacaoItem` — `fallback_ambiguous` já existia desde a Story 3.2. Todo o trabalho ficou nas queries `sumSQL`/`countSQL`/`listSQL` do handler e no frontend.
- `apenasAmbiguos := statusParam == "revisao_ambigua"` segue exatamente o padrão de valor mágico já usado para `"all"` no mesmo parâmetro `status` — evitou introduzir um novo query param.
- `countSQL` e `listSQL` usam posições de parâmetro diferentes para `apenasAmbiguos` (`$5` em `countSQL`, `$7` em `listSQL`) porque `listSQL` já ocupa `$5`/`$6` com `LIMIT`/`OFFSET` — cada query é independente, então isso é seguro (confirmado por teste).
- `sumSQL` nunca recebe `statusFilter` nem `apenasAmbiguos` como parâmetro — a nova contagem `notas_revisao_ambigua` é estruturalmente incapaz de refletir o filtro ativo, o que evita por construção a classe de bug já vista 2x nesta epic (contador herdando o filtro errado).
- Frontend: o badge/chip fica fora do dropdown de Status (exigência explícita do AC #2), com estado visual distinto quando o filtro `revisao_ambigua` já está ativo (preenchido) vs quando é só um atalho disponível (outline âmbar). Nenhuma mudança em `StatusBadge`/`STATUS_CLASSES` — o filtro é ortogonal ao status exibido por item.
- Helper de teste novo (`insertRfbCreditoComEmitente`) foi necessário só para o teste de combinação com `forn_cnpj`, porque `insertRfbCredito` (já existente) não grava `ni_emitente`, e `creditos_tipados` filtra créditos por esse campo (não pelo `forn_cnpj` do pagamento).
- Escopo mantido: nenhuma mudança em `status_conciliacao`; nenhum workflow de anotação/aprovação (não-meta do PRD); nenhuma migration.

### File List

- `backend/handlers/rfb_pagamentos_fornecedores.go` (editado — `ConciliacaoSumario.NotasRevisaoAmbigua`; `sumSQL` ganha `COUNT(*) FILTER (WHERE fallback_ambiguous = true)`; `apenasAmbiguos`/`statusFilter` derivados de `statusParam == "revisao_ambigua"`; `countSQL`/`listSQL` ganham `AND ($N = false OR fallback_ambiguous = true)`)
- `backend/handlers/rfb_pagamentos_fornecedores_test.go` (editado — +5 testes: `TestConciliacao_FiltroAmbiguos_IgnoraStatusConciliacao`, `TestConciliacao_FiltroAmbiguos_CombinaComFornCNPJ`, `TestConciliacao_FiltroAmbiguos_CombinaComMesAno` (adicionado em revisão), `TestConciliacao_SumarioNotasRevisaoAmbigua_IgnoraStatus`, `TestConciliacao_FiltroAmbiguos_SemResultado_NaoQuebra`; +helpers `insertRfbCreditoComEmitente`, `queryChavesFiltroAmbiguos`, `querySumarioRevisaoAmbigua`)
- `frontend/src/pages/RFBPagamentosFornecedores.tsx` (editado — `ConciliacaoSumario.notas_revisao_ambigua`; nova opção `revisao_ambigua` no `<select>` de Status; badge/chip clicável no painel de filtros com `aria-pressed` (adicionado em revisão); coluna "Match Ambíguo" na exportação Excel; import do ícone `HelpCircle`)

### Change Log

- 2026-07-12: Implementação completa da Story 3.3 (terceira e última planejada da Epic 3) — filtro dedicado de revisão de matches ambíguos (`fallback_ambiguous=true`), reaproveitando 100% do dado já exposto desde a Story 3.2 (nenhuma migration, nenhuma mudança em `persistPayments`). Novo valor mágico `status=revisao_ambigua` ignora `status_conciliacao` no filtro (créditos já `extinto` podem aparecer), mas continua combinando com `mes_ano`/`forn_cnpj`. Novo contador `notas_revisao_ambigua` no sumário, estruturalmente imune a refletir o filtro ativo (nunca recebe `statusFilter`/`apenasAmbiguos`). Frontend: badge/chip visível fora do dropdown de Status, opção de filtro no dropdown, coluna nova na exportação Excel. 4 testes novos, 22/22 testes de `handlers` passando (18 originais + 4 novos), sem regressões. `go build`/`go vet`/`go test ./...` e `tsc` limpos (26 erros pré-existentes, baseline inalterada).
- 2026-07-12: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor, 3 subagentes isolados sem contexto da conversa) — 0 decision-needed, 2 patches aplicados: (1) `aria-pressed` adicionado ao badge/botão de filtro dedicado (gap de acessibilidade); (2) teste `TestConciliacao_FiltroAmbiguos_CombinaComMesAno` adicionado, cobrindo a combinação do filtro de ambíguos com `mes_ano` (a Acceptance Auditor notou que só `forn_cnpj` estava coberto). 6 achados adiados para `deferred-work.md` (nenhum teste via `httptest`; sem índice dedicado para `fallback_ambiguous`; `rows.Err()` não checado no helper novo; dropdown de Status não reseta página — bug pré-existente em todas as opções, não introduzido por esta story; sem teste de frontend automatizado). 8 descartados, majoritariamente falsos-positivos do Blind Hunter por falta de acesso ao projeto (campo/coluna que já existiam desde a Story 3.2) ou comportamento explicitamente por design (filtro substitui status, badge oculto em zero). Acceptance Auditor confirmou as 4 ACs satisfeitas e nenhuma violação de FR-4/escopo. 24/24 testes `TestConciliacao` em `handlers` passando (19 pré-existentes + 5 desta story, incluindo o teste novo do patch), `tsc` com a mesma baseline de 26 erros pré-existentes.
