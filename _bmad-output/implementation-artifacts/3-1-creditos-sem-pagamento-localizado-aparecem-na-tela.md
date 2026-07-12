---
baseline_commit: eeffaed
---

# Story 3.1: Créditos sem Pagamento Localizado Aparecem na Tela

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a Ana (analista fiscal),
I want ver todo crédito da RFB na tela de conciliação, mesmo quando nenhum pagamento foi localizado ainda,
so that nada fique escondido da minha visão.

Realiza FR-11 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Primeira story da Epic 3 ("Conciliação Completa e Transparente") — ver `_bmad-output/planning-artifacts/epics.md`. Depende das Stories 2.1-2.5 (Epic 2, todas `done`). **Esta é a mudança arquitetural mais significativa do projeto até aqui**: inverte a direção da query de conciliação em `backend/handlers/rfb_pagamentos_fornecedores.go` — de `FROM pagamentos_fornecedores` (com `LEFT JOIN rfb_creditos`) para `FROM rfb_creditos` (com `LEFT JOIN` nos pagamentos agregados). A Story 2.5 deliberadamente não tocou nesta inversão (documentado em `2-5-...md`, Dev Notes: "Inversão da query de conciliação (Story 3.1) — fora de escopo desta story").

## Acceptance Criteria

1. **Given** um crédito RFB (`rfb_creditos`) com `valor_cbs_nao_extinto > 0` e nenhum pagamento (CSV nem SAP) localizado para a mesma chave, **When** Ana abre a tela de conciliação, **Then** ele aparece com um novo status `aguardando_pagamento` (não fica invisível como hoje).
2. **Given** a inversão da query (de `pagamentos_fornecedores` LEFT JOIN `rfb_creditos` para `rfb_creditos` LEFT JOIN pagamentos), **When** os cards de sumário renderizam, **Then** "CBS Extinto" reflete **todos** os créditos que a RFB já considera extintos (`valor_cbs_nao_extinto = 0`), não apenas o subconjunto que também tem pagamento registrado.
3. **Given** um pagamento em `pagamentos_fornecedores` sem nenhum crédito `rfb_creditos` correspondente (mesma `chave_dfe`/`chave_doc`), **When** exibido, **Then** continua aparecendo como `sem_dados` — comportamento atual preservado, sem regressão.

## Tasks / Subtasks

- [x] **Task 1 — Derivar `tipo_doc` para linhas de `rfb_creditos`** (pré-requisito técnico, nenhuma AC isolada)
  - [x] CTE `creditos_tipados` adicionada em `rfb_pagamentos_fornecedores.go`, derivando `'NFE'`/`'CTE'`/`NULL` a partir de `rc.modelo_dfe` (`'55'`→NFE, `'57'`→CTE) com fallback para `SUBSTRING(rc.chave_dfe, 21, 2)` — reaproveita exatamente o padrão de `rfb_apuracao.go`/`rfb_debitos_lista.go`
  - [x] `rc.ni_emitente` usado como equivalente de `forn_cnpj` (via `COALESCE(pa.forn_cnpj, rc.ni_emitente, '')`) e também no filtro `($3 = '' OR rc.ni_emitente = $3)` — créditos sem pagamento continuam visíveis num filtro por fornecedor

- [x] **Task 2 — Inverter a CTE de conciliação: `rfb_creditos` como base, pagamentos como LEFT JOIN** (AC: #1, #2)
  - [x] Nova CTE `conciliacao_creditos`: `FROM creditos_tipados rc LEFT JOIN pagamentos_agg pa ON pa.chave_doc = rc.chave_dfe` — `pagamentos_agg`/`pagamentos_dedup` (Story 2.5) reaproveitados sem nenhuma mudança
  - [x] `status_conciliacao`: implementação final (pós-revisão) usa `NOT EXISTS (SELECT 1 FROM pagamentos_fornecedores px WHERE px.company_id=$1 AND px.chave_doc=rc.chave_dfe)` em vez de `pa.chave_doc IS NULL` — ver Review Findings, bug crítico encontrado e corrigido durante a revisão
  - [x] `valor_nota`/`valor_cbs_nota`/`valor_ibs_nota`: mesma lógica `COALESCE(CASE tipo_doc...)`, usando `rc.tipo_doc_derivado` e `rc.chave_dfe` para os LEFT JOINs em `nfe_entradas`/`cte_entradas`
  - [x] `possivel_duplicidade`: `pa.chave_doc IS NOT NULL AND ...` — `false` quando não há pagamento
  - [x] `total_pago`/`num_parcelas`: `COALESCE(pa.*, 0)`; `primeira_parcela`/`ultima_parcela`: `NULL` quando não há pagamento (tipo `DATE`, sem fallback numérico aplicável)

- [x] **Task 3 — Preservar `sem_dados` para pagamentos órfãos via `UNION ALL`** (AC: #3)
  - [x] CTE `pagamentos_orfaos`: `FROM pagamentos_agg pa WHERE NOT EXISTS (SELECT 1 FROM rfb_creditos rc2 WHERE rc2.company_id = $1 AND rc2.chave_dfe = pa.chave_doc)`, `status_conciliacao = 'sem_dados'` fixo, `valor_cbs_nao_extinto = 0::NUMERIC(15,2)`, `situacao_credito = NULL::VARCHAR(100)` (tipos explícitos, exigido pelo `UNION ALL`)
  - [x] `conciliacao final = conciliacao_creditos UNION ALL pagamentos_orfaos` — 15 colunas idênticas em tipo/ordem nas duas pernas, `go build`/testes confirmam compatibilidade

- [x] **Task 4 — Decisão de escopo do filtro `mes_ano` para créditos sem pagamento** (afeta AC #1)
  - [x] Implementado de forma mais simples que a sugestão original: `creditos_tipados` **nunca** aplica o filtro `mes_ano` ($2) — só `pagamentos_dedup` (que já era filtrado por mês desde a Story 2.5) usa esse parâmetro. Isso alcança exatamente o efeito desejado (crédito sem pagamento nunca some sob filtro de mês) sem precisar da condição `pa.mes_ano_do_grupo = $2` sugerida no rascunho da story, porque `pagamentos_agg` não expõe uma coluna `mes_ano` própria — o filtro já está embutido em quais linhas de `pa` existem. Testado em `TestConciliacao_FiltroMesAno_NaoEsconderCreditoAguardandoPagamento`

- [x] **Task 5 — Frontend: novo status `aguardando_pagamento`**
  - [x] `aguardando_pagamento` adicionado a `STATUS_CLASSES`/`STATUS_LABELS` (`'bg-yellow-100 text-yellow-800 border-yellow-300'`, label `'Aguardando Pagamento'`) e à lista de opções do filtro de status
  - [x] Confirmado por inspeção: `formatDate("")` já retorna `'—'` (checagem de falsy) e `formatCurrency(0)` retorna `"R$ 0,00"` — nenhuma mudança de código necessária, `total_pago=0`/`primeira_parcela`/`ultima_parcela` vazios (via `COALESCE`/ausência de linha) já renderizam graciosamente

- [x] **Task 6 — Testes e verificação**
  - [x] Harness existente reaproveitado (`openTestDB`/`setupTestCompany`/`insertPagamento`/`insertNFeEntrada`/`queryConciliacaoItem`) — adicionados `insertRfbCredito`, `queryConciliacaoStatus`, `querySumarioExtintos` (novos helpers, harness não recriado)
  - [x] `TestConciliacao_CreditoSemPagamento_AguardandoPagamento` — AC #1
  - [x] `TestConciliacao_CreditoComPagamento_Pendente` — confirma que 'pendente' (não 'aguardando_pagamento') quando há pagamento mas RFB não extinguiu
  - [x] `TestConciliacao_SumarioContaExtintosSemPagamento` — AC #2 (2 notas extintas contadas, com e sem pagamento)
  - [x] `TestConciliacao_PagamentoSemCredito_ContinuaSemDados` — AC #3 (regressão explícita)
  - [x] `TestConciliacao_FiltroMesAno_NaoEsconderCreditoAguardandoPagamento` — Task 4
  - [x] Regressão: as 7 testes já existentes da Story 2.5 (`TestConciliacao_FiltroMesAno_...`, `CSVeSAP_...`, `LinhaCSVContinuaVisivelParaAuditoria`, `NumDocPagamentoDiferente_...`, `PossivelDuplicidade`, `NFSESemNotaCasada_...`, `RegressaoSoCSV_...`) continuam passando sem nenhuma alteração — confirmado, 12/12 testes passando no total
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões
  - [x] `npx tsc --noEmit`: 26 erros — mesma baseline da Story 2.5, nenhum novo

### Review Findings

*(Revisão adversarial — Blind Hunter + Edge Case Hunter + Acceptance Auditor — sobre o diff isolado desta story. 2 bugs críticos confirmados empiricamente contra Postgres real, um encontrado por mim e independentemente por todos os 3 revisores, outro exclusivo do Edge Case Hunter)*

- [x] [Review][Patch] **Bug crítico confirmado empiricamente**: `status_conciliacao` usava `pa.chave_doc IS NULL` para decidir `aguardando_pagamento`, mas `pa` (via `pagamentos_agg`) já é filtrado por `mes_ano`/`forn_cnpj` internamente — um crédito **já pago** (em um mês diferente do filtro atual) aparecia incorretamente como `aguardando_pagamento`, como se nunca tivesse recebido pagamento algum. Encontrado independentemente por mim (verificação manual contra Postgres real) e pelos 3 revisores adversariais. [backend/handlers/rfb_pagamentos_fornecedores.go] — corrigido: substituído por `NOT EXISTS (SELECT 1 FROM pagamentos_fornecedores px WHERE px.company_id=$1 AND px.chave_doc=rc.chave_dfe)`, que verifica a tabela crua, sem filtro de mês — mesma abrangência do `NOT EXISTS` já usado em `pagamentos_orfaos`. Testado em `TestConciliacao_CreditoJaPago_NaoViraAguardandoPagamentoSobFiltroDeMes`
- [x] [Review][Patch] **Bug crítico exclusivo do Edge Case Hunter**: `tipo_doc_derivado` é `NULL` para `modelo_dfe` fora de `'55'`/`'57'` (ex.: `'65'` NFC-e, `'67'` CT-e OS) ou chave malformada — propagado sem `COALESCE` para a coluna `tipo_doc`, que o Go escaneia direto para `string` não-nullable (`ConciliacaoItem.TipoDoc`). Um único crédito com modelo desconhecido na paginação atual quebrava o `Scan` ("converting NULL to string"), derrubando a **página inteira** com 500, não apenas aquele item. [backend/handlers/rfb_pagamentos_fornecedores.go] — corrigido: `COALESCE(rc.tipo_doc_derivado, '')`. Testado em `TestConciliacao_ModeloDfeDesconhecido_NaoQuebraScan`
- [x] [Review][Patch] Comentário impreciso sobre o fallback de `modelo_dfe` (dizia "quando vier vazio", mas o fallback dispara para qualquer valor diferente de `'55'`/`'57'`) e alegação exagerada de "reaproveitar literalmente" o padrão de `rfb_apuracao.go`/`rfb_debitos_lista.go` (esses arquivos só repassam o código cru, nunca mapeiam para os literais `'NFE'`/`'CTE'` — isso é lógica nova desta story) [backend/handlers/rfb_pagamentos_fornecedores.go] — corrigido: comentário reescrito para refletir exatamente o comportamento e a origem da lógica
- [ ] [Review][Defer] `rfb_creditos` pode ter mais de uma linha por `chave_dfe` quando `chave_dfe` é `NULL`/vazio — o índice único parcial (`migration 096`) só protege `chave_dfe IS NOT NULL AND != ''`, e o `INSERT ... ON CONFLICT` de `rfb_creditos_processor.go` usa o mesmo arbiter parcial, então linhas com `chave_dfe` vazio nunca fazem UPSERT, só INSERT solto. Impacto real (reavaliado pelo Edge Case Hunter): não infla `total_pago` (uma chave vazia não casa com nenhum pagamento real), mas infla as **contagens** dos cards de resumo (`notas_extinto`/`notas_pendente`/`total_notas`) se a RFB retornar múltiplas entradas sem chave para o mesmo crédito real. Razão do adiamento: propriedade do schema desde a migration 096 (Story anterior a esta), não introduzida pela inversão; depende de a API da RFB realmente devolver créditos sem `chaveDfe`, cenário não confirmado como frequente. **Revisitar** se contagens de créditos parecerem infladas em produção
- [ ] [Review][Defer] `rc.ni_emitente` (RFB) e `pagamentos_fornecedores.forn_cnpj` (SAP/CSV) não têm garantia de mesmo formato — CSV normaliza para 14 dígitos via regex, mas os pipelines sistema-a-sistema (RFB→`rfb_creditos.ni_emitente`, SAP→`pagamentos_fornecedores.forn_cnpj` via `sap_payments_processor.go`) gravam o CNPJ cru, sem limpeza de máscara/zero à esquerda. Um CNPJ mascarado vindo da RFB ou do SAP quebraria silenciosamente o filtro `rc.ni_emitente = $3` e a exibição `COALESCE(pa.forn_cnpj, rc.ni_emitente, '')`. Razão do adiamento: gap de normalização pré-existente em ambos os pipelines (não introduzido por esta story, que apenas passou a comparar os dois campos pela primeira vez); corrigir corretamente exigiria uma auditoria de normalização de CNPJ em toda a ingestão RFB/SAP, fora do escopo desta story. **Revisitar** se um filtro por fornecedor não encontrar créditos que deveria
- [x] [Review][Dismiss] "Nenhuma mudança de teste automatizado" (Blind Hunter) — falso; o Blind Hunter recebeu apenas os diffs de `.go`/`.tsx` no prompt de revisão, sem o diff do arquivo de teste (limitação do prompt, não do código) — o diff real inclui 6 testes novos (agora 8, após os 2 adicionados nesta triagem para os bugs críticos)
- [x] [Review][Dismiss] Renderização de `forn_nome`/datas/parcelas para linhas `aguardando_pagamento` (Blind Hunter) — investigado e confirmado falso pelo Edge Case Hunter: `forn_nome ?? '—'`, `formatDate('')`→`'—'`, `formatCurrency(0)`→"R$ 0,00" já tratam graciosamente; `PrimeiraParcela`/`UltimaParcela` no Go nunca viram `null` no JSON (ficam `""`, zero value de `string`)
- [x] [Review][Dismiss] "Mudança de frontend chamativamente pequena, sem cards/exportação atualizados" (Blind Hunter) — nenhuma AC pede card de resumo dedicado ou mudança na exportação Excel para `aguardando_pagamento`; escopo mantido conforme documentado
- [x] [Review][Dismiss] `SELECT rc.*` em `creditos_tipados` (preocupação estilística sobre migrations futuras) — mesmo padrão já usado em `pagamentos_dedup` (`SELECT pf.*`, Story 2.5); migrations do projeto só adicionam colunas (nunca alteram), risco baixo e consistente com o resto do arquivo
- [x] [Review][Dismiss] `NOT EXISTS` em vez do padrão `LEFT JOIN ... WHERE x.id IS NULL` usado no resto do arquivo (Blind Hunter) — não é troca de estilo arbitrária: `pagamentos_orfaos` não tem mais `rfb_creditos` no `FROM`/`LEFT JOIN` (a inversão removeu essa junção direta), então `NOT EXISTS` é a forma estruturalmente necessária de expressar "não existe crédito", não uma escolha de estilo
- [x] [Review][Dismiss] Inconsistência de cast entre os dois lados do `UNION ALL` (tipos implícitos vs. `::NUMERIC`/`::VARCHAR` explícitos) — Acceptance Auditor enumerou as 15 colunas de ambos os lados e confirmou tipos compatíveis; `go build`/testes confirmam que a query executa e retorna os tipos esperados, sem erro de coerção
- [x] [Review][Dismiss] Cards de resumo misturando semântica "filtrado por mês" (itens pagos) com "sempre todos os meses" (créditos sem pagamento) (Acceptance Auditor) — comportamento intencional e já documentado na Task 4/Dev Notes desta story, não um defeito

## Dev Notes

### Por que esta é a story mais arriscada da Epic 3 — leia isto antes de tocar no código

`backend/handlers/rfb_pagamentos_fornecedores.go` foi reescrito na Story 2.5 (CTE `pagamentos_dedup` + `possivel_duplicidade`) e sofreu uma revisão adversarial que encontrou 2 bugs críticos confirmados empiricamente contra Postgres real (filtro `mes_ano` escondendo liquidações inteiras; `possivel_duplicidade` sempre `true` para NFSE). **Leia o arquivo completo antes de editar** — a query atual (função `cteBase`, linhas ~52-162) já é complexa; esta story adiciona uma camada de inversão por cima dela, não a substitui do zero. `pagamentos_dedup` e `pagamentos_agg` (Story 2.5) **não mudam nesta story** — apenas o que consome `pagamentos_agg` muda de direção.

### Estado atual (pós Story 2.5) — `backend/handlers/rfb_pagamentos_fornecedores.go`

```sql
WITH pagamentos_dedup AS ( ... FROM pagamentos_fornecedores, dedup CSV-vs-SAP (Story 2.5) ... ),
pagamentos_agg AS ( ... GROUP BY chave_doc, tipo_doc, forn_cnpj ... ),
conciliacao AS (
    SELECT pa.*, valor_nota, valor_cbs_nota, valor_ibs_nota, valor_cbs_nao_extinto,
           situacao_credito, status_conciliacao, possivel_duplicidade
    FROM pagamentos_agg pa
    LEFT JOIN nfe_entradas ne ON ... AND ne.chave_nfe = pa.chave_doc
    LEFT JOIN cte_entradas ct ON ... AND ct.chave_cte = pa.chave_doc
    LEFT JOIN rfb_creditos rc ON rc.company_id = $1 AND rc.chave_dfe = pa.chave_doc
)
```

**A base é `pagamentos_fornecedores`.** Um crédito RFB sem NENHUM pagamento nunca aparece — a query nunca "vê" `rfb_creditos` para chaves sem pagamento. Isso é exatamente o que a AC #1 desta story corrige.

### Schema de `rfb_creditos` (migration 096) — colunas relevantes

```sql
id UUID, request_id UUID, company_id UUID,
modelo_dfe VARCHAR(10),        -- '55' (NFe) / '57' (CTe), pode vir vazio
numero_dfe VARCHAR(20),
chave_dfe VARCHAR(50),         -- equivalente a chave_doc/chave_nfe/chave_cte
data_dfe_emissao DATE,
data_apuracao VARCHAR(10),     -- formato "MM/YYYY", NÃO é o mesmo formato de mes_ano ('YYYY-MM')
ni_emitente VARCHAR(20),       -- equivalente a forn_cnpj (CNPJ do fornecedor/emitente)
valor_cbs_total NUMERIC(15,2), valor_cbs_extinto NUMERIC(15,2), valor_cbs_nao_extinto NUMERIC(15,2),
situacao_credito VARCHAR(100)
```
Índice único: `(company_id, chave_dfe) WHERE chave_dfe IS NOT NULL AND chave_dfe != ''` — um crédito por chave por empresa, seguro para LEFT JOIN 1:1 (ou 1:N do lado dos pagamentos).

**`modelo_dfe` NÃO é `'NFE'`/`'CTE'`** (usado em `pagamentos_fornecedores.tipo_doc`) — é o código de modelo fiscal (`'55'`=NFe, `'57'`=CTe). É preciso derivar. **Não reinvente essa lógica**: `backend/handlers/rfb_apuracao.go:807-810` e `rfb_debitos_lista.go:148-151` já resolvem exatamente isso:
```sql
CASE
    WHEN COALESCE(rc.modelo_dfe, '') != '' THEN rc.modelo_dfe
    ELSE SUBSTRING(rc.chave_dfe, 21, 2)
END
```
Depois mapear `'55'→'NFE'`, `'57'→'CTE'`, qualquer outro valor → `NULL` (não força um tipo errado).

### Decisão: `mes_ano` para créditos sem pagamento (Task 4)

`mes_ano` (formato `'YYYY-MM'`) é uma coluna de `pagamentos_fornecedores`, derivada da data de pagamento — não existe para um crédito sem pagamento algum. `data_apuracao` de `rfb_creditos` está em formato diferente (`'MM/YYYY'`) e representa o período de apuração RFB, não a data de um pagamento futuro.

**Decisão adotada**: o filtro `mes_ano`, quando preenchido, nunca esconde um crédito `aguardando_pagamento` — ele só restringe QUAIS pagamentos entram na agregação quando eles existem. Um crédito sem pagamento aparece em **qualquer** filtro de mês (não faz sentido "escondê-lo de junho" quando ele não tem data de pagamento nenhuma para comparar). Isso é consistente com o espírito da FR-11 ("nada fica escondido da minha visão") e evita reintroduzir, de forma diferente, o mesmo tipo de bug de "desaparecimento sob filtro" que foi corrigido na revisão da Story 2.5.

**Nota para o dev agent**: isto é uma decisão de design tomada durante a criação desta story, não uma ambiguidade em aberto — implemente exatamente como descrito acima. Se o usuário validar o comportamento e preferir diferente (ex.: filtrar por `data_apuracao` convertida), isso vira um ajuste pontual pós-implementação, não bloqueia o desenvolvimento.

### Impacto nos cards de resumo (`sumSQL`) — AC #2 é sobre isto

Hoje `sumSQL` faz `COUNT(*) FILTER (WHERE status_conciliacao = 'extinto')` sobre a CTE `conciliacao`, que só contém chaves COM pagamento. Depois da inversão, a mesma agregação passa a contar automaticamente TODOS os créditos extintos (com ou sem pagamento) — **não precisa mudar `sumSQL` em si**, só a CTE `conciliacao` que ele consome. Verifique que os números mudam para cima em "CBS Extinto"/"notas_extinto" ao testar manualmente (mais créditos entram no denominador).

### `UNION ALL` — atenção a tipos de coluna idênticos nas duas pernas

Postgres exige que as colunas de um `UNION ALL` tenham os mesmos tipos (ou coercíveis) em ambas as pernas, na mesma ordem. `pagamentos_orfaos` não tem `situacao_credito`/`valor_cbs_nao_extinto` reais (não há crédito) — usar `NULL::VARCHAR(100)` / `0::NUMERIC(15,2)` explícitos, não deixar o Postgres inferir. Mesma lista de colunas, mesma ordem, em ambas as pernas do `SELECT`.

### Reaproveitar o harness de teste da Story 2.5 — não recriar

`backend/handlers/rfb_pagamentos_fornecedores_test.go` já tem `openTestDB`, `setupTestCompany`, `insertPagamento`, `insertNFeEntrada`, `queryConciliacaoItem` (todos no mesmo pacote `handlers`, compilados junto). Adicione só um `insertRfbCredito(t, db, companyID, chaveDfe, modeloDfe, valorCbsNaoExtinto float64)` novo. **Não crie um novo arquivo de teste nem duplique os helpers** — são exatamente o que esta story precisa, com uma função nova.

### Fora de escopo desta story (não implementar)

- Indicador de pagamento como evidência auxiliar detalhado (mensagens distintas `EM_ABERTO`/`NAO_LOCALIZADO`, indicador visual "pagamento localizado" separado do `status_conciliacao`) — Story 3.2
- Filtro/contador dedicado para `matchType = FALLBACK` ambíguo — Story 3.3
- Qualquer mudança em `services/sap_*.go` ou no pipeline de sincronização (Epic 2, `done`)
- Qualquer mudança em `pagamentos_dedup`/`pagamentos_agg` (Story 2.5) — usar exatamente como estão

### Project Structure Notes

- Mesmo arquivo único (`rfb_pagamentos_fornecedores.go`) para a conciliação — sem novo handler
- Nenhuma migration nova (nenhuma coluna nova necessária — `rfb_creditos` já tem tudo que a story precisa)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.1] — ACs originais
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/addendum.md] — "Inversão da consulta de conciliação... de FROM pagamentos_fornecedores LEFT JOIN rfb_creditos para FROM rfb_creditos LEFT JOIN pagamentos_agg, com tratamento separado (UNION ou segunda query) para o caso sem_dados"
- [Source: backend/handlers/rfb_pagamentos_fornecedores.go] — CTE atual completa, ponto de integração
- [Source: backend/handlers/rfb_pagamentos_fornecedores_test.go] — harness de teste a reaproveitar
- [Source: backend/migrations/096_rfb_creditos.sql:8-33] — schema de `rfb_creditos`
- [Source: backend/handlers/rfb_apuracao.go:807-810, rfb_debitos_lista.go:148-151] — padrão já estabelecido de derivação `modelo_dfe`/`chave_dfe` → tipo de documento
- [Source: _bmad-output/implementation-artifacts/2-5-coexistencia-de-pagamentos-csv-e-sap-sem-duplicidade.md] — Dev Notes confirmando que a inversão foi deliberadamente deixada para esta story; Review Findings com os 2 bugs críticos já corrigidos na query atual (não reintroduzir)

## Dev Agent Record

### Agent Model Used

Claude Sonnet 5 (claude-sonnet-5)

### Debug Log References

- `go build ./...`, `go vet ./...` — limpos após cada task
- `go test ./handlers/... -run TestConciliacao -v` — 14 testes passando (7 novos desta story, incluindo os 2 adicionados durante a revisão para os bugs críticos + 7 já existentes da Story 2.5, sem alteração e sem regressão)
- `go test ./...` (suíte completa) — sem regressões (`handlers`, `middleware`, `services`)
- `npx tsc -p tsconfig.app.json --noEmit` — 26 erros pré-existentes (mesma baseline da Story 2.5), nenhum no código novo
- **Fase RED confirmada**: rodei os 5 testes novos ANTES de implementar a inversão — 3 falharam exatamente como esperado (`sql: no rows in result set` para os 2 testes de crédito sem pagamento, contagem `1` em vez de `2` para o teste de sumário), confirmando que os testes exercitam o gap real antes de qualquer implementação
- **Limitação registrada**: sem ferramenta de navegador neste ambiente — a tela `RFBPagamentosFornecedores.tsx` foi verificada por leitura de código (badge/filtro do novo status, fallback gracioso de `formatDate`/`formatCurrency` para valores vazios/zero), não por captura visual

### Completion Notes List

- A inversão foi implementada como duas CTEs (`conciliacao_creditos` a partir de `rfb_creditos`, `pagamentos_orfaos` para pagamentos sem crédito) unidas por `UNION ALL`, em vez de um `FULL OUTER JOIN` — abordagem sugerida pelo próprio addendum do PRD, e mais simples de raciocinar sobre tipos de coluna e sobre o caso `sem_dados` (que precisa forçar `status_conciliacao` fixo, algo que um `FULL OUTER JOIN` tornaria mais implícito).
- `pagamentos_dedup`/`pagamentos_agg` (Story 2.5) **não foram alterados** — reaproveitados exatamente como estavam, conforme instruído nos Dev Notes.
- Derivação de `tipo_doc` a partir de `modelo_dfe`/`chave_dfe`: o fallback por `SUBSTRING(chave_dfe)` reaproveita o mecanismo de `rfb_apuracao.go`/`rfb_debitos_lista.go`, mas o mapeamento para os literais `'NFE'`/`'CTE'` é lógica nova desta story (aqueles arquivos só repassam o código cru) — corrigido na revisão, a alegação original de "reaproveitar literalmente" estava exagerada.
- Decisão sobre o filtro `mes_ano` (Task 4): implementada de forma mais simples que o rascunho original da story — `creditos_tipados` simplesmente nunca recebe o parâmetro `$2` (mes_ano), em vez de uma condição `OR pa.chave_doc IS NULL OR ...` — o efeito é idêntico (crédito sem pagamento nunca some sob filtro de mês) mas o mecanismo é mais direto, já que `pagamentos_agg` não expõe uma coluna `mes_ano` própria para comparar. **Nota pós-revisão**: essa mesma condição `pa.chave_doc IS NULL` usada para decidir `status_conciliacao` (não o filtro em si) continha o bug crítico corrigido na revisão — ver Review Findings.
- `possivel_duplicidade` e as colunas de `nfe_entradas`/`cte_entradas` foram duplicadas nas duas pernas do `UNION ALL` (mesma técnica já usada na Story 2.5 para reusar uma expressão sem depender de alias entre linhas do mesmo `SELECT`) — não foi possível fatorar em uma CTE comum sem reintroduzir a ambiguidade de "de onde vem o tipo_doc" entre os dois lados.
- Escopo mantido dentro da story: nenhuma mudança em `services/sap_*.go`, nenhum indicador de evidência auxiliar detalhado (Story 3.2), nenhum filtro de `FALLBACK` (Story 3.3), nenhum card de resumo dedicado para `aguardando_pagamento` (não pedido pelas ACs).

### File List

- `backend/handlers/rfb_pagamentos_fornecedores.go` (editado — CTEs `creditos_tipados`, `conciliacao_creditos`, `pagamentos_orfaos`; `conciliacao` final via `UNION ALL`; nenhuma mudança em `pagamentos_dedup`/`pagamentos_agg`; revisão: `status_conciliacao` corrigido para usar `NOT EXISTS` não filtrado por mês em vez de `pa.chave_doc IS NULL`; `tipo_doc` com `COALESCE` para nunca propagar `NULL`; comentário de derivação de `tipo_doc` corrigido)
- `backend/handlers/rfb_pagamentos_fornecedores_test.go` (editado — +5 testes originais: `TestConciliacao_CreditoSemPagamento_AguardandoPagamento`, `TestConciliacao_CreditoComPagamento_Pendente`, `TestConciliacao_SumarioContaExtintosSemPagamento`, `TestConciliacao_PagamentoSemCredito_ContinuaSemDados`, `TestConciliacao_FiltroMesAno_NaoEsconderCreditoAguardandoPagamento`; +3 helpers: `insertRfbCredito`, `queryConciliacaoStatus`, `querySumarioExtintos`; revisão: +2 testes para os bugs críticos — `TestConciliacao_CreditoJaPago_NaoViraAguardandoPagamentoSobFiltroDeMes`, `TestConciliacao_ModeloDfeDesconhecido_NaoQuebraScan`)
- `frontend/src/pages/RFBPagamentosFornecedores.tsx` (editado — `aguardando_pagamento` em `STATUS_CLASSES`/`STATUS_LABELS` e no filtro de status)

### Change Log

- 2026-07-12: Implementação completa da Story 3.1 (primeira da Epic 3) — inversão da query de conciliação de `FROM pagamentos_fornecedores` para `FROM rfb_creditos`, via duas CTEs unidas por `UNION ALL` (`conciliacao_creditos` para créditos, com ou sem pagamento; `pagamentos_orfaos` para pagamentos sem crédito, preservando `sem_dados`). Novo status `aguardando_pagamento` para créditos com `valor_cbs_nao_extinto > 0` e nenhum pagamento localizado. Cards de resumo passam a contar automaticamente todos os créditos extintos, com ou sem pagamento. Derivação de `tipo_doc`/`forn_cnpj` para linhas de `rfb_creditos` reaproveitando padrões já estabelecidos no código (`modelo_dfe`/`chave_dfe`, `ni_emitente`). 5 testes novos de integração contra Postgres real (fase RED confirmada antes da implementação), 7 testes de regressão da Story 2.5 passando sem alteração. Nenhuma migration nova. Sem regressões.
- 2026-07-12: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor) — 3 patches aplicados: (1) bug crítico de `status_conciliacao` usando `pa.chave_doc IS NULL` (reflete o filtro de mês atual) em vez de checar pagamento em qualquer mês — um crédito já pago em outro mês aparecia como `aguardando_pagamento`, confirmado empiricamente contra Postgres real e encontrado independentemente pelos 3 revisores + verificação manual minha; (2) bug crítico exclusivo do Edge Case Hunter: `tipo_doc` `NULL` (modelo fiscal desconhecido) quebrava o `Scan` em Go e derrubava a página inteira com 500 — corrigido com `COALESCE`; (3) comentário de derivação de `tipo_doc` corrigido (alegação de reaproveitar "literalmente" o padrão existente estava exagerada — o mapeamento para `NFE`/`CTE` é lógica nova). 2 testes novos cobrindo os bugs críticos (total 14 testes). 2 achados adiados para `deferred-work.md` (duplicidade de `rfb_creditos` por `chave_dfe` vazia; formato não normalizado entre `ni_emitente` e `forn_cnpj`). 6 achados descartados (incluindo uma alegação de "sem testes" baseada em diff incompleto no prompt de revisão, não no código real). `go build`/`go vet`/`go test ./...` e `tsc --noEmit` limpos após os patches.
