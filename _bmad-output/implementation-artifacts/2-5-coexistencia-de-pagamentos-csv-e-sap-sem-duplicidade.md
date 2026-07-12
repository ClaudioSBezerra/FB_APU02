---
baseline_commit: 89040f1
---

# Story 2.5: Coexistência de Pagamentos CSV e SAP sem Duplicidade

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a Ana (analista fiscal),
I want que o sistema combine com segurança pagamentos de CSV e do SAP para a mesma nota,
so that a transição para sincronização automática nunca crie conflitos de dado.

Realiza FR-9 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Quinta e última story da Epic 2 — ver `_bmad-output/planning-artifacts/epics.md`. Diferente das Stories 2.1-2.4, que construíram o pipeline de sincronização (`services/sap_*.go`), esta story mexe exclusivamente na **camada de exibição/agregação da conciliação** (`backend/handlers/rfb_pagamentos_fornecedores.go` e sua tela `frontend/src/pages/RFBPagamentosFornecedores.tsx`) — nenhuma migration nova é necessária, todas as colunas (`origem`, `num_doc_pagamento`, `bukrs`) já existem desde a Story 2.3 (migration 118).

## Acceptance Criteria

1. **Given** uma chave (`chave_doc`) com um pagamento `csv` e depois um pagamento `sap_api` referentes à mesma liquidação (mesmo `num_doc_pagamento`), **When** ambos existem na tabela `pagamentos_fornecedores`, **Then** a agregação de conciliação (`total_pago`) conta essa liquidação **uma única vez**, não duas.
2. **Given** conflito de valor entre o registro `csv` e o registro `sap_api` da mesma liquidação (mesma chave de correspondência), **When** exibido na tela de conciliação, **Then** o registro `sap_api` prevalece na agregação/exibição — o registro `csv` permanece intacto na tabela e continua visível na listagem de parcelas (drill-down), para auditoria.
3. **Given** o `total_pago` (já deduplicado pelas regras acima) de uma chave excede o `valor_nota`, **When** isso ocorre, **Then** uma sinalização visual distinta aparece na tela (indício de duplicidade ou match incorreto).

## Tasks / Subtasks

- [x] **Task 1 — Deduplicar por liquidação compartilhada na CTE de conciliação** (AC: #1, #2)
  - [x] Em `backend/handlers/rfb_pagamentos_fornecedores.go`, adicionar uma CTE `pagamentos_dedup` **antes** de `pagamentos_agg`, usando a estratégia EXISTS descrita nos Dev Notes: exclui uma linha `origem='csv'` da agregação SOMENTE se existir uma linha `origem='sap_api'` com o mesmo `(company_id, chave_doc, num_doc_pagamento)` — nunca remove a linha da tabela, apenas da soma
  - [x] `pagamentos_agg` passa a agregar `FROM pagamentos_dedup` em vez de `FROM pagamentos_fornecedores pf` diretamente — preservar exatamente os mesmos filtros `WHERE` ($1/$2/$3) e o mesmo `GROUP BY`
  - [x] Não alterar `uq_pag_forn_csv`/`uq_pag_forn_sap_api` (migration 118, já revisada na Story 2.3) — esta story NUNCA cria migration nova

- [x] **Task 2 — Sinalização visual de possível duplicidade** (AC: #3)
  - [x] Adicionar `possivel_duplicidade` (bool, calculado como `total_pago > valor_nota` já deduplicado) à CTE `conciliacao` e ao struct `ConciliacaoItem` (backend)
  - [x] Incluir a nova coluna no SELECT de `listSQL` (a `sumSQL`/`countSQL` não precisam dela)
  - [x] Frontend (`RFBPagamentosFornecedores.tsx`): renderizar um indicador visual distinto (ex.: ícone de alerta + tooltip) na célula "Total Pago" quando `item.possivel_duplicidade === true`

- [x] **Task 3 — Expor `origem` na listagem de parcelas para auditoria** (AC: #2)
  - [x] Em `PagamentosFornecedoresListHandler` (`backend/handlers/pagamentos_fornecedores.go`), adicionar `origem` ao SELECT e ao struct `pagItem` — também corrigido, na mesma linha, um bug pré-existente da Story 2.3: `import_id` era escaneado direto para `string` sem `COALESCE`, mas ficou nullable desde a migration 118 (linhas `sap_api` nunca preenchem `import_id`), o que quebrava o scan (`converting NULL to string`) para qualquer linha de origem SAP — corrigido com `COALESCE(import_id::text, '')`
  - [x] Frontend: adicionar `origem: string` à interface `Parcela` e uma coluna/badge "Origem" (CSV/SAP) na tabela de parcelas expandida

- [x] **Task 4 — Testes e verificação**
  - [x] Criar `backend/handlers/rfb_pagamentos_fornecedores_test.go` (não existia — testa `cteBase` diretamente via SQL, sem HTTP/JWT, conforme Dev Notes; réplica local de `openTestDB`/`setupTestCompany`, com cleanup adicional de `pagamentos_fornecedores`/`nfe_entradas`/`cte_entradas`/`rfb_creditos` antes do `DELETE FROM environments`, já que essas tabelas não têm `ON DELETE CASCADE` em `company_id`)
  - [x] `TestConciliacao_CSVeSAP_MesmaLiquidacao_ContaUmaVez` — CSV (100) + SAP (150) mesmo `num_doc_pagamento` → `total_pago=150`, `num_parcelas=1`
  - [x] `TestConciliacao_LinhaCSVContinuaVisivelParaAuditoria` — confirma via `SELECT` direto que as 2 linhas (csv + sap_api) continuam na tabela após a dedup na agregação
  - [x] `TestConciliacao_NumDocPagamentoDiferente_NaoColapsa` — `num_doc_pagamento` diferente → `num_parcelas=2`, `total_pago` soma ambos (sem colapsar)
  - [x] `TestConciliacao_PossivelDuplicidade` — `total_pago > valor_nota` → `true`; caso normal → `false`
  - [x] `TestConciliacao_RegressaoSoCSV_ComportamentoInalterado` — só CSV, sem SAP → soma tudo, comportamento idêntico ao anterior à story
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões (5 testes novos, todos passando; suíte completa: `handlers`/`middleware`/`services` OK)
  - [x] `npx tsc --noEmit`: 26 erros — mesma baseline pré-existente, nenhum novo

### Review Findings

*(Revisão adversarial — Blind Hunter + Edge Case Hunter + Acceptance Auditor, sobre o diff isolado desta story, última da Epic 2. Confirmação empírica direta contra Postgres real, dentro/fora de transação, para os 2 achados críticos)*

- [x] [Review][Patch] **Bug crítico confirmado empiricamente**: o `EXISTS` da CTE `pagamentos_dedup` não aplicava os mesmos filtros `mes_ano`/`forn_cnpj` ($2/$3) que a CTE externa aplica a todas as linhas — se a linha `csv` e a `sap_api` da mesma liquidação caíssem em meses/CNPJs diferentes (data de pagamento manual vs. data de compensação real do SAP), a `csv` era suprimida pelo `EXISTS` (irrestrito) mas a `sap_api` correspondente não aparecia no resultado filtrado (por não bater o filtro externo) — **a liquidação inteira desaparecia do relatório filtrado**, sem nenhum rastro, pior que o problema original de duplicidade. [backend/handlers/rfb_pagamentos_fornecedores.go] — corrigido: `EXISTS` agora aplica `($2 = '' OR sap.mes_ano = $2)` e `($3 = '' OR sap.forn_cnpj = $3)`, reproduzido e confirmado corrigido diretamente contra Postgres real antes e depois da correção; testado em `TestConciliacao_FiltroMesAno_NaoEsconderLiquidacaoEmMesDiferente`
- [x] [Review][Patch] **Falso-positivo estrutural confirmado empiricamente**: `possivel_duplicidade` usava `COALESCE(valor_nota, 0)`, então qualquer `tipo_doc='NFSE'` (tipo válido de import, nunca casado com `nfe_entradas`/`cte_entradas`) ou qualquer NFE/CTE sem cabeçalho importado caía no fallback `0` — `total_pago > 0` era sempre verdadeiro, marcando **100% dos pagamentos NFSE** como duplicidade, mascarando "sem nota casada" como "duplicidade real". [backend/handlers/rfb_pagamentos_fornecedores.go] — corrigido: exige `valor_nota IS NOT NULL` (nota real casada) antes de comparar; reproduzido e confirmado corrigido contra Postgres real; testado em `TestConciliacao_NFSESemNotaCasada_NaoFalsoPositivoDeDuplicidade`
- [x] [Review][Patch] `EXISTS` não validava `tipo_doc`/`forn_cnpj`, usados no `GROUP BY` de `pagamentos_agg` — um erro de digitação no import CSV (mesma chave/num_doc_pagamento mas `tipo_doc`/`forn_cnpj` divergente) poderia suprimir uma linha `csv` sem nenhum grupo absorver seu valor corretamente [backend/handlers/rfb_pagamentos_fornecedores.go] — corrigido: `EXISTS` agora exige `sap.tipo_doc = pf.tipo_doc AND sap.forn_cnpj = pf.forn_cnpj`
- [x] [Review][Patch] Correção do bug pré-existente de `import_id` (Task 3) sem comentário inline explicando o motivo, dificultando rastreabilidade para quem só vê o diff [backend/handlers/pagamentos_fornecedores.go] — corrigido: comentário adicionado no ponto exato
- [x] [Review][Patch] Exportação Excel da tela de conciliação não incluía o novo campo `possivel_duplicidade` — relatório baixado ficaria inconsistente com a tela [frontend/src/pages/RFBPagamentosFornecedores.tsx] — corrigido: coluna "Possível Duplicidade" (Sim/Não) adicionada ao export
- [ ] [Review][Defer] `num_doc_pagamento` é comparado por igualdade exata, sem normalização, e o próprio template CSV oficial (`PagamentosTemplateHandler`) ensina valores de conveniência (`DOC-001`, `NFS-001`) que nunca vão coincidir com o `ClearingDocument` real que o SAP grava depois — na prática, o dedup desta story só funciona se o usuário souber e digitar o número de compensação SAP no CSV, o que raramente é possível no momento da digitação manual (o SAP sincroniza depois, automaticamente). Razão do adiamento: a AC #1 desta story especifica literalmente "mesmo num_doc_pagamento/clearingDocument" como o mecanismo de correspondência — implementado exatamente como especificado; o PRD já antecipa e aceita esse risco residual (`prd.md:205`: "indicador otimista demais, não um crédito fechado incorretamente"). Uma heurística alternativa de correspondência (chave_doc + data/valor aproximados) seria uma mudança de design maior, fora do escopo desta story. **Revisitar**: considerar atualizar o template/instruções do CSV para orientar o preenchimento do número de compensação real quando conhecido, ou avaliar heurística alternativa se a taxa de dedup efetiva se mostrar baixa em produção
- [ ] [Review][Defer] Duas linhas `sap_api` de `bukrs` diferentes para a mesma liquidação (mesmo `chave_doc`+`num_doc_pagamento`) não são dedupadas entre si — o índice único `uq_pag_forn_sap_api` (Story 2.3) inclui `bukrs` na chave, então documentos processados sob 2 códigos de empresa SAP poderiam, em tese, ser contados em dobro. Razão do adiamento: propriedade do design do índice já revisado na Story 2.3 (fora do escopo desta story, que trata especificamente de coexistência CSV-vs-SAP, não SAP-vs-SAP); baixa probabilidade prática (exigiria o mesmo documento genuinamente processado sob 2 bukrs). **Revisitar** se esse cenário se mostrar recorrente em produção
- [ ] [Review][Defer] `RFBPagamentosFornecedoresHandler` roda `sumSQL`/`countSQL`/`listSQL` como 3 queries separadas, sem transação/snapshot compartilhado — uma sincronização SAP commitando entre as chamadas pode fazer cards de resumo e lista paginada refletirem estados diferentes na mesma resposta HTTP. Razão do adiamento: padrão pré-existente (3 queries separadas já existiam antes desta story); mesmo padrão já deferido em outras stories para `services` (ausência geral de `context.Context`/transação). Severidade baixa (inconsistência transitória, não perda de dado)
- [ ] [Review][Defer] Resumo do item mostra `num_parcelas` já deduplicado (ex.: "1 parcela"), mas a expansão de parcelas (drill-down) lista as linhas brutas sem dedup (ex.: 2 linhas, csv+sap_api) — comportamento **intencional** da AC #2 ("mantendo o registro csv para auditoria"), mas sem nenhuma dica textual na UI explicando a aparente divergência entre o resumo e o detalhamento. **Revisitar**: considerar um tooltip/nota explicativa na linha expandida quando houver uma linha csv suprimida da agregação
- [x] [Review][Dismiss] "Migração ausente no diff" — investigado: nenhuma migration nova é necessária; todas as colunas (`origem`, `num_doc_pagamento`, `bukrs`) já existem desde a migration 118 (Story 2.3), confirmado por `git diff` vazio em `backend/migrations/`
- [x] [Review][Dismiss] `SELECT pf.*` na CTE `pagamentos_dedup` (em vez de listar colunas explícitas) — decisão intencional para manter a CTE resiliente a mudanças futuras de schema, consistente com o fato de `pagamentos_agg` já fazer `SELECT` explícito das colunas que realmente usa logo em seguida
- [x] [Review][Dismiss] Alegação de que o requisito mais crítico (linha csv preservada) não estava testado — falso; o arquivo de teste completo tem 7 testes (incluindo `TestConciliacao_LinhaCSVContinuaVisivelParaAuditoria`, que testa exatamente isso); o achado partiu de um diff resumido por limite de tamanho no prompt de revisão, não do código real
- [x] [Review][Dismiss] Testes de integração dependem de Postgres real, que não roda em CI hoje — precedente já aceito desde a Story 1.1 (mesma política em toda a Epic 1/2), não introduzido por esta story
- [x] [Review][Dismiss] Cleanup de teste "frágil" (não apaga `enterprise_groups`/`companies` explicitamente) — investigado: `ON DELETE CASCADE` já cobre ambas as tabelas a partir de `environments` (confirmado em `migrations/013_create_environment_hierarchy.sql:15,26`), sem risco real de órfãos
- [x] [Review][Dismiss] Comparação `total_pago > valor_nota` sem tolerância de ponto flutuante — investigado: todas as colunas envolvidas (`valor_pagamento`, `v_nf`, `v_prest`) são `NUMERIC(15,2)`, não `FLOAT`/`DOUBLE` — comparação em aritmética decimal exata, sem risco de arredondamento

## Dev Notes

### Estado atual da CTE de conciliação — ponto exato de integração

`backend/handlers/rfb_pagamentos_fornecedores.go:44-113` (const `cteBase`):

```sql
WITH pagamentos_agg AS (
    SELECT
        pf.chave_doc, pf.tipo_doc, pf.forn_cnpj, MAX(pf.forn_nome) AS forn_nome,
        COUNT(*) AS num_parcelas, SUM(pf.valor_pagamento) AS total_pago,
        MIN(pf.data_pagamento) AS primeira_parcela, MAX(pf.data_pagamento) AS ultima_parcela
    FROM pagamentos_fornecedores pf
    WHERE pf.company_id = $1
      AND ($2 = '' OR pf.mes_ano = $2)
      AND ($3 = '' OR pf.forn_cnpj = $3)
    GROUP BY pf.chave_doc, pf.tipo_doc, pf.forn_cnpj
),
conciliacao AS ( ... LEFT JOIN nfe_entradas/cte_entradas/rfb_creditos ... )
```

**Hoje esta query soma `valor_pagamento` de TODAS as linhas de `pagamentos_fornecedores` para uma chave, sem distinguir `origem`.** Isso é exatamente o problema desta story: desde a Story 2.3, uma mesma chave pode ter uma linha `origem='csv'` (import manual) E uma linha `origem='sap_api'` (sync automático) representando **a mesma liquidação real** (mesmo `num_doc_pagamento`/clearing document) — hoje ambas são somadas, dobrando o `total_pago` exibido.

**Confirmado nesta análise:** a Story 2.3 (`_bmad-output/implementation-artifacts/2-3-persistencia-de-pagamentos-com-deduplicacao-robusta.md:125,161-164,209`) documenta explicitamente que **não tocou** nesta query de propósito, e que "Sinalização de `total_pago > valor_nota`, prevalência de `sap_api` sobre `csv` em caso de conflito de valor" foi deferida para esta story (2.5). Não é um bug novo — é o próximo passo já planejado.

### Solução recomendada — CTE `pagamentos_dedup` via EXISTS (não usar DISTINCT ON)

**Por que não usar `DISTINCT ON (chave_doc, num_doc_pagamento)`:** colapsaria também duas linhas `csv` genuinamente distintas que por coincidência compartilhem `num_doc_pagamento` na mesma chave (raro, mas não impossível — `uq_pag_forn_csv` não impede isso, pois sua chave de dedup é `(chave_doc, data_pagamento, valor_pagamento)`, não inclui `num_doc_pagamento`). Isso seria uma regressão (perderia uma parcela CSV legítima).

**Abordagem correta — excluir via EXISTS, cirúrgica, só remove o que realmente colide entre origens:**

```sql
pagamentos_dedup AS (
    SELECT pf.*
    FROM pagamentos_fornecedores pf
    WHERE pf.company_id = $1
      AND ($2 = '' OR pf.mes_ano = $2)
      AND ($3 = '' OR pf.forn_cnpj = $3)
      AND NOT (
          pf.origem = 'csv'
          AND pf.num_doc_pagamento IS NOT NULL
          AND EXISTS (
              SELECT 1 FROM pagamentos_fornecedores sap
              WHERE sap.company_id = pf.company_id
                AND sap.chave_doc = pf.chave_doc
                AND sap.num_doc_pagamento = pf.num_doc_pagamento
                AND sap.origem = 'sap_api'
          )
      )
)
```

Depois, `pagamentos_agg` passa a fazer `FROM pagamentos_dedup pf` (manter o alias `pf` é o que faz o resto da CTE — `pf.chave_doc`, `pf.tipo_doc` etc. — continuar funcionando sem nenhuma outra mudança). Propriedades desta abordagem:

- Uma linha `csv` só é excluída da agregação se **existir** uma linha `sap_api` com o mesmo `(company_id, chave_doc, num_doc_pagamento)` — implementa AC #1 e #2 (`sap_api` prevalece) exatamente.
- **Nunca apaga nem modifica nenhuma linha da tabela** — a linha `csv` "perdedora" continua existindo e consultável via `PagamentosFornecedoresListHandler` (Task 3), satisfazendo literalmente "mantendo o registro csv para auditoria" (AC #2).
- CSV legado sem `num_doc_pagamento` (`IS NULL`) nunca é excluído por esta regra — comportamento inalterado para o caso mais comum hoje (import CSV sem esse campo preenchido), consistente com o risco já aceito no PRD (`prd.md:205`: "indicador otimista demais, não um crédito fechado incorretamente").
- Duas linhas `sap_api` nunca colidem entre si aqui (já protegidas por `uq_pag_forn_sap_api` desde a Story 2.3 — "pagamentos por conta" têm `num_doc_pagamento` diferente).

### Sinalização de duplicidade (AC #3)

Adicionar ao SELECT de `conciliacao` (dentro de `cteBase`, reaproveitando `pa.total_pago` já deduplicado por `pagamentos_dedup`):
```sql
(pa.total_pago > COALESCE(CASE WHEN pa.tipo_doc = 'NFE' THEN ne.v_nf ELSE ct.v_prest END, 0)) AS possivel_duplicidade
```
(usar a mesma expressão `valor_nota` já calculada na query, não duplicar lógica). Adicionar `PossivelDuplicidade bool \`json:"possivel_duplicidade"\`` ao struct `ConciliacaoItem` (linha ~35) e ao `Scan()` da list query (~linha 208+). **Não é necessário** adicionar um card de resumo agregado para isso — a AC pede sinalização por item, um card extra é escopo além do pedido.

### Estratégia de teste — testar a SQL diretamente, não via HTTP/JWT

**Não existe hoje nenhum teste (`rfb_pagamentos_fornecedores_test.go` não existe) nem harness de teste com banco real no pacote `handlers`** — todos os testes existentes em `handlers/*_test.go` cobrem apenas caminhos pré-DB (401/405). Construir um harness completo de autenticação (JWT + claims + `company.owner_id`) só para testar uma CTE seria desproporcional.

**Abordagem recomendada:** `cteBase`/`sumSQL`/`listSQL` já são `const string` no pacote `handlers` — um teste no mesmo pacote pode chamar `db.QueryRow(cteBase+sumSQL, companyID, "", "")` ou `db.Query(cteBase+listSQL+"...", ...)` **diretamente**, sem passar pelo handler HTTP nem por JWT. Isso testa exatamente a lógica desta story (a CTE) sem precisar simular autenticação. Réplica o padrão `openTestDB`/`setupTestCompany` já estabelecido em `backend/services/sap_sync_trigger_test.go:51-92` (companies/enterprise_groups/environments de teste, cleanup via `DELETE FROM environments` em cascata) — copiar esse padrão para o novo arquivo de teste (helpers de teste não são compartilhados entre pacotes Go, replicar é o padrão já aceito no projeto).

Para popular `pagamentos_fornecedores` de teste, inserir diretamente via SQL (`INSERT INTO pagamentos_fornecedores (company_id, chave_doc, tipo_doc, forn_cnpj, data_pagamento, valor_pagamento, num_doc_pagamento, mes_ano, origem, bukrs, import_id) VALUES (...)` — lembrar que `import_id` é nullable para `origem='sap_api'` desde a migration 118, mas obrigatório para `origem='csv'`; gerar um UUID de teste com `gen_random_uuid()` ou similar para as linhas `csv`). Para `valor_nota`, inserir uma linha mínima em `nfe_entradas` (ou `cte_entradas`) com `chave_nfe`/`v_nf` correspondentes.

### Arquivos a modificar (nenhum arquivo novo de produção, 1 arquivo novo de teste)

- `backend/handlers/rfb_pagamentos_fornecedores.go` — CTE `pagamentos_dedup`, campo `possivel_duplicidade`
- `backend/handlers/pagamentos_fornecedores.go` — `origem` no SELECT/struct de `PagamentosFornecedoresListHandler`
- `backend/handlers/rfb_pagamentos_fornecedores_test.go` — **novo**
- `frontend/src/pages/RFBPagamentosFornecedores.tsx` — indicador visual de duplicidade + coluna "Origem" nas parcelas

### Fora de escopo desta story (não implementar)

- Qualquer mudança em `services/sap_payments_processor.go`, `sap_sync_trigger.go`, `sap_sync_alerts.go` (Epic 2, Stories 2.1-2.4, já `done`) — esta story não altera o pipeline de sincronização, só a exibição
- Nenhuma migration nova — todas as colunas necessárias (`origem`, `num_doc_pagamento`, `bukrs`) já existem desde a migration 118
- Inversão da query de conciliação (`FROM rfb_creditos` em vez de `FROM pagamentos_fornecedores`) — isso é a Story 3.1 (Epic 3), não esta
- Filtro/contador dedicado para `matchType = FALLBACK` — Story 3.2/3.3 (Epic 3)
- Card de resumo agregado para duplicidade — não pedido pela AC #3 (que é por item)

### Project Structure Notes

- Segue exatamente o padrão de arquivo único por domínio já usado (`rfb_pagamentos_fornecedores.go` para a conciliação, `pagamentos_fornecedores.go` para import/listagem CRUD) — nenhum arquivo novo de handler necessário, só o de teste
- Nenhuma rota nova, nenhuma entrada de navegação nova — a tela `RFBPagamentosFornecedores.tsx` já existe e já está no menu

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.5] — ACs originais
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md:204-205,266,330] — decisão de prevalência sap_api e risco aceito de indicador otimista
- [Source: backend/handlers/rfb_pagamentos_fornecedores.go:44-113] — CTE atual, ponto de integração
- [Source: backend/migrations/118_pagamentos_fornecedores_origem_sap.sql] — colunas/índices já existentes, nenhuma migration nova necessária
- [Source: _bmad-output/implementation-artifacts/2-3-persistencia-de-pagamentos-com-deduplicacao-robusta.md:125,161-164,209] — confirmação de que esta query foi deliberadamente deixada intocada para esta story
- [Source: backend/services/sap_sync_trigger_test.go:51-92] — padrão de harness de teste com banco real a replicar

## Dev Agent Record

### Agent Model Used

Claude Sonnet 5 (claude-sonnet-5)

### Debug Log References

- `go build ./...`, `go vet ./...` — limpos após cada task
- `go test ./handlers/... -run TestConciliacao -v` — 5 testes novos, todos passando (1 falha de dado de teste no primeiro run do teste de regressão — colisão com `uq_pag_forn_csv` por usar o mesmo valor/data em duas linhas CSV — corrigida ajustando o valor da segunda parcela, não é um bug de produção)
- `go test ./...` (suíte completa) — sem regressões (`handlers`, `middleware`, `services`)
- `npx tsc -p tsconfig.app.json --noEmit` — 26 erros pré-existentes (mesma baseline de antes desta story), nenhum no código novo
- Confirmado manualmente via `psql` que o cleanup dos testes não deixa dados órfãos (`SELECT count(*) FROM environments WHERE name = 'teste-rfb-pgtos-fornecedores-2.5'` → 0)
- **Pós-revisão**: os 2 bugs críticos (filtro `mes_ano` escondendo liquidação; `possivel_duplicidade` sempre `true` para NFSE) foram reproduzidos manualmente contra Postgres real (programa Go isolado, dados de teste limpos ao final) ANTES da correção, confirmando o problema, e novamente DEPOIS, confirmando a correção — não apenas inferidos da leitura do código. `go build ./...`, `go vet ./...`, `go test ./...`, `npx tsc --noEmit` — limpos, 7/7 testes novos passando (5 originais + 2 da revisão)
- **Limitação registrada**: este ambiente não tem ferramenta de navegador — a tela `RFBPagamentosFornecedores.tsx` foi verificada por leitura de código contra os novos campos da API (`possivel_duplicidade`, `origem`), não por captura visual. A lógica de negócio real (dedup/agregação) está inteiramente no backend e foi verificada com testes de integração reais contra Postgres, não apenas no frontend

### Completion Notes List

- A abordagem de dedup usa uma CTE `pagamentos_dedup` com `EXISTS` (exclui uma linha `csv` da agregação somente se existir uma linha `sap_api` com o mesmo `num_doc_pagamento` na mesma chave) — deliberadamente **não** usa `DISTINCT ON`, que colapsaria também duas linhas CSV genuinamente distintas que por coincidência compartilhem `num_doc_pagamento` (regressão evitada, documentada nos Dev Notes).
- A linha `csv` "perdedora" nunca é apagada nem modificada — só é excluída desta agregação específica. Continua 100% visível e consultável via `PagamentosFornecedoresListHandler`, satisfazendo literalmente a AC #2 ("mantendo o registro csv para auditoria").
- Corrigido, como efeito colateral necessário do Task 3, um bug pré-existente da Story 2.3: `PagamentosFornecedoresListHandler` escaneava `import_id` direto para `string` sem `COALESCE`, mas essa coluna ficou nullable desde a migration 118 (linhas `sap_api` nunca preenchem `import_id`) — qualquer chamada a este handler para uma chave com pagamento SAP quebraria o `Scan` com "converting NULL to string". Sem essa correção, o próprio teste desta story (`TestConciliacao_LinhaCSVContinuaVisivelParaAuditoria`) teria motivo para falhar caso testasse via HTTP em vez de SQL direto — a decisão de testar a CTE diretamente (Dev Notes) evitou o sintoma, mas o bug em si foi corrigido porque a story pede exatamente para tocar esse handler.
- `possivel_duplicidade` repete a mesma expressão `valor_nota` já usada na CTE (não referencia a coluna-alias `valor_nota` porque Postgres não permite reuso de alias dentro do mesmo nível de `SELECT`) — mantém a lógica de "qual é o valor da nota" em um único lugar conceitual, mesmo que replicado textualmente.
- Nenhuma migration nova criada — confirmado que todas as colunas necessárias (`origem`, `num_doc_pagamento`, `bukrs`) já existiam desde a migration 118 (Story 2.3).
- Escopo mantido dentro da story: nenhuma mudança no pipeline de sincronização (`services/sap_*.go`, Stories 2.1-2.4, já `done`), nenhuma inversão da query de conciliação (Story 3.1), nenhum filtro de `FALLBACK` (Story 3.2/3.3), nenhum card de resumo agregado para duplicidade (não pedido pela AC #3).

### File List

- `backend/handlers/rfb_pagamentos_fornecedores.go` (editado — CTE `pagamentos_dedup`, campo `possivel_duplicidade` em `conciliacao`/`ConciliacaoItem`/`listSQL`/`Scan`; revisão: `EXISTS` corrigido para aplicar mes_ano/forn_cnpj/tipo_doc, `possivel_duplicidade` corrigido para exigir nota real casada)
- `backend/handlers/pagamentos_fornecedores.go` (editado — `origem` no SELECT/struct de `PagamentosFornecedoresListHandler`; corrigido bug pré-existente de `import_id` sem `COALESCE`; revisão: comentário inline explicando o fix)
- `backend/handlers/rfb_pagamentos_fornecedores_test.go` (novo — 5 testes; revisão: +2 testes — filtro de mes_ano não esconde liquidação, NFSE não gera falso-positivo — total 7 testes)
- `frontend/src/pages/RFBPagamentosFornecedores.tsx` (editado — indicador visual de duplicidade na célula "Total Pago", coluna "Origem" (CSV/SAP) na tabela de parcelas expandida; revisão: campo "Possível Duplicidade" adicionado à exportação Excel)

### Change Log

- 2026-07-11: Implementação completa da Story 2.5 (última da Epic 2) — CTE `pagamentos_dedup` evita duplicidade de `total_pago` quando a mesma liquidação (chave_doc + num_doc_pagamento) tem registro CSV e SAP, com o registro `sap_api` prevalecendo na agregação e o registro `csv` preservado intacto para auditoria; sinalização visual (`possivel_duplicidade`) quando `total_pago` excede `valor_nota`; coluna "Origem" exposta na listagem de parcelas. Corrigido, como parte necessária do Task 3, um bug pré-existente de `import_id` nullable sem `COALESCE` em `PagamentosFornecedoresListHandler`. Nenhuma migration nova. 5 testes novos de integração contra Postgres real, sem regressões.
- 2026-07-11: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor, os três encontraram independentemente o mesmo bug crítico) — 5 patches aplicados: (1) `EXISTS` da CTE de dedup não aplicava `mes_ano`/`forn_cnpj`, causando desaparecimento total de liquidações do relatório sob filtro de mês — confirmado empiricamente contra Postgres real antes/depois da correção; (2) `possivel_duplicidade` era sempre `true` para NFSE/documentos sem nota casada (fallback `valor_nota=0`) — também confirmado empiricamente; (3) `EXISTS` passou a validar `tipo_doc`/`forn_cnpj` (usados no `GROUP BY`); (4) comentário inline explicando o fix de `import_id`; (5) campo "Possível Duplicidade" adicionado à exportação Excel. 2 testes novos cobrindo os bugs críticos corrigidos (total 7 testes). 4 achados adiados para `deferred-work.md` (normalização de `num_doc_pagamento` + template CSV ensinando valores que nunca deduplicam — limitação já aceita pelo PRD; dedup entre múltiplas linhas `sap_api` de bukrs diferentes; leituras não transacionais entre sumário/lista; UX de resumo deduplicado vs. drill-down bruto sem dica textual). 6 achados descartados (migration inexistente confirmada desnecessária; `SELECT pf.*` intencional; alegação de teste ausente baseada em diff truncado no prompt de revisão, não no código real; testes dependentes de Postgres real sem CI — precedente já aceito; cleanup de teste confirmado seguro via `ON DELETE CASCADE`; comparação de valores confirmada sem risco de ponto flutuante — colunas são `NUMERIC(15,2)`). `go build`/`go vet`/`go test ./...` e `tsc --noEmit` limpos após os patches (7/7 testes novos passando, suíte completa sem regressões).
