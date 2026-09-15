---
title: 'RFB: agregação de rfb_resumo/rfb_creditos_resumo via SQL (prep para API incremental)'
type: 'bugfix'
created: '2026-09-15'
status: 'done'
review_loop_iteration: 1
context: []
baseline_commit: 'ea32a369e7243ffef385a6120606f3c824c7f2e7'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `ProcessarDownloadRFB` e `ProcessarDownloadCreditosRFB` (e seus gêmeos de reprocessamento) somam os totais de `rfb_resumo`/`rfb_creditos_resumo` **em memória, só com os itens da resposta da chamada atual**, e depois sobrescrevem (UPDATE) o resumo do período inteiro. A RFB vai migrar as APIs de débitos/créditos de CBS para consulta incremental (retorno parcial) a partir de outubro/2026 — quando isso acontecer, o resumo passará a ser silenciosamente sobrescrito com a soma de só as poucas notas que mudaram, produzindo apuração fiscal incorreta sem erro visível.

**Approach:** trocar a origem dos totais de "acumulador Go sobre o payload da resposta" para uma agregação SQL (`SUM`/`COUNT` agrupado por `tipo_apuracao`) sobre `rfb_debitos`/`rfb_creditos` — tabelas já mantidas corretamente via UPSERT por `(company_id, chave_dfe)` — filtrando por `company_id + data_apuracao`, dentro da mesma transação, logo antes do UPSERT em `rfb_resumo`/`rfb_creditos_resumo`. Isso corrige o bug independente de o payload da RFB ser completo ou incremental. Complementarmente, extrair a versão de API (`v1`) hardcoded em `rfb.go` para uma constante.

## Boundaries & Constraints

**Always:**
- A agregação SQL roda dentro da mesma `tx` já aberta (`db.Begin()`), depois de todos os `insertDebito`/`insertCredito`, antes do `tx.Commit()`.
- Filtrar por `company_id = $1 AND data_apuracao = $2` (sem filtrar por `request_id`), pois o objetivo é refletir o estado real acumulado da tabela para o período, não só as linhas tocadas pela chamada atual.
- Usar `COALESCE(SUM(...), 0)` para os campos monetários — `SUM` sobre zero linhas retorna `NULL` em Postgres, e hoje o resumo é gravado mesmo com `total_debitos = 0`.
- Bucket créditos: `tipo_apuracao <> 'ajuste'` (incl. `extemporaneo`/flat) conta em `total_corrente`. Decisão do humano: comportamento CORRIGIDO, manter — não é regressão.
- **Linhas com `chave_dfe` NULL/vazio ficam FORA da agregação SQL** (decisão do humano): query filtra `AND chave_dfe IS NOT NULL AND chave_dfe <> ''`; para essas linhas, manter soma em memória só dos itens sem chave da chamada atual, somada ao resultado SQL. Motivo: sem chave não há UPSERT/dedup — agregação pura dobraria/triplicaria em chamadas repetidas (limitação já conhecida, `deferred-work.md`/story 3-1).
- Corrigir as 4 funções (`ProcessarDownloadRFB`, `ReprocessarRawJSON`, `ProcessarDownloadCreditosRFB`, `ReprocessarRawJSONCreditosRFB`) **e também** o UPSERT de `rfb_creditos_resumo` embutido em `ProcessarDownloadRFB`/`ReprocessarRawJSON` (créditos vindos junto da resposta de débitos) — a 1ª implementação deixou esse ponto de fora.
- `log.Printf("[RFB] ...")` com os totais finais pós-agregação (não os acumuladores brutos); `defer tx.Rollback()` existente não deve ser removido.

**Ask First:** nenhum backfill dos `rfb_resumo`/`rfb_creditos_resumo` já gravados incorretamente está incluso — a correção só vale a partir da próxima consulta bem-sucedida por empresa/período. Se o humano quiser corrigir retroativamente os resumos históricos, isso é uma decisão separada (script de backfill), perguntar antes de propor.

**Never:** não implementar o parâmetro/header de consulta incremental em si, não criar migration de cursor/última-consulta, não mexer em pagamentos/recolhimentos/DARF (não existem no código), não corrigir o gap pré-existente de `totalFlat` não mapeado além de preservar o comportamento atual, não alterar schema de nenhuma tabela.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Payload completo (hoje) | Resposta com todos os débitos do período | Resumo idêntico ao calculado hoje (regressão zero) | N/A |
| Payload incremental futuro | Resposta só com chaves alteradas desde a última consulta | Resumo reflete SOMA de todas as linhas já persistidas em `rfb_debitos`/`rfb_creditos` para o período, não só o delta recebido | N/A |
| Período sem nenhuma linha persistida | `company_id+data_apuracao` sem registros em `rfb_debitos` | Resumo gravado com totais = 0 (via COALESCE), sem erro de NULL constraint | Query de agregação deve usar COALESCE |
| Reprocessamento (`ReprocessarRawJSON`) | DELETE + reinsert das linhas do `request_id` | Resumo pós-commit reflete o estado atual da tabela após o reprocessamento, sem duplicar totais de requests antigos | N/A |

</frozen-after-approval>

## Code Map

- `backend/services/rfb_processor.go` -- `ProcessarDownloadRFB` (linhas 84-346) e `ReprocessarRawJSON` (393-664): alvo da correção de resumo de débitos
- `backend/services/rfb_creditos_processor.go` -- `ProcessarDownloadCreditosRFB` (51-339) e `ReprocessarRawJSONCreditosRFB` (342-480): alvo da correção de resumo de créditos
- `backend/services/rfb.go` -- `SolicitarApuracao` (L215), `SolicitarCredito` (L293), `DownloadArquivo` (L353): extrair constante de versão de API
- `backend/services/rfb_scheduler_test.go` -- padrão de teste existente (`openTestDB`, `newFakeRFB`) a reutilizar nos novos testes

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/rfb_processor.go` -- Em `ProcessarDownloadRFB` E `ReprocessarRawJSON`: (a) antes do UPSERT em `rfb_resumo`, somar agregação SQL (rows com `chave_dfe`, `GROUP BY tipo_apuracao`, `COALESCE(SUM,0)`) + acumulador em memória (itens sem `chave_dfe` do payload atual); (b) mesma correção no UPSERT embutido de `rfb_creditos_resumo`; (c) `log.Printf` com totais finais.
- [x] `backend/services/rfb_creditos_processor.go` -- Mesma correção híbrida em `ProcessarDownloadCreditosRFB`/`ReprocessarRawJSONCreditosRFB`, `tipo_apuracao <> 'ajuste'` → `total_corrente`, log com totais finais.
- [x] `backend/services/rfb.go` -- `const rfbAPIVersion = "v1"`, usar via `fmt.Sprintf` nos 3 endpoints (L215, L293, L353).
- [x] `backend/services/rfb_processor_test.go` (novo) -- 2 chamadas com chaves diferentes → soma cumulativa; 2 chamadas repetindo item sem chave → não duplica; período vazio → zeros. Fake server usa `rfbAPIVersion`, não literal `"v1"`. Sincronizar com goroutine `TriggerSAPSync` antes do `db.Close()`.
- [x] `backend/services/rfb_creditos_processor_test.go` (novo) -- mesma cobertura + bucket `extemporaneo`→`total_corrente`.

**Acceptance Criteria:**
- Given duas chamadas sucessivas para a mesma empresa/período com chaves diferentes, when processadas, then o resumo contém a soma de ambas, não só a última.
- Given duas chamadas sucessivas repetindo o mesmo item sem `chave_dfe`, when processadas, then esse item conta uma vez só no resumo (não duplica).
- Given um período sem nenhuma linha persistida, when o resumo é calculado, then os totais saem 0 sem erro de NULL.
- Given a suíte de testes existente (`rfb_scheduler_test.go`), when rodada, then continua passando sem alteração de valores esperados.

## Spec Change Log

### Loopback 1 (2026-09-15)

**Trigger:** revisão adversarial achou (a) `chave_dfe` NULL/vazio quebra a premissa de dedup por UPSERT → agregação pura dobraria/triplicaria essas linhas (intent_gap); (b) Tasks não cobriam o UPSERT de `rfb_creditos_resumo` embutido em `ProcessarDownloadRFB`/`ReprocessarRawJSON` (bad_spec).

**Emenda:** cálculo híbrido (SQL p/ linhas com chave + acumulador em memória p/ linhas sem chave, decisão do humano); Tasks agora cobrem o UPSERT embutido e os logs.

**KEEP:** fold de créditos `extemporaneo`/"flat" em `total_corrente` — confirmado pelo humano, não é regressão.

**Adiado** (`deferred-work.md`): corrida de concorrência sem lock; agregação nunca encolhe; bucket `(company_id,'')` acumulativo em requests malformados.

## Design Notes

Exemplo de query de agregação (débitos), ilustrativo — adaptar a sintaxe exata ao driver `lib/pq`:

```sql
SELECT tipo_apuracao,
       COUNT(*),
       COALESCE(SUM(valor_cbs_total), 0),
       COALESCE(SUM(valor_cbs_extinto), 0),
       COALESCE(SUM(valor_cbs_nao_extinto), 0)
FROM rfb_debitos
WHERE company_id = $1 AND data_apuracao = $2
GROUP BY tipo_apuracao
```
O código então distribui as linhas do resultado nos buckets `corrente`/`ajuste`/`extemporaneo` (débitos) ou `corrente`/`ajuste` (créditos, com fallback de qualquer outro tipo para `corrente`) e soma os totais gerais fora do GROUP BY (ou soma os buckets em Go após a query).

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: compila sem erros
- `cd backend && go test ./services/... -run RFB -v` -- expected: todos os testes de RFB (existentes + novos) passam
- `cd backend && go vet ./services/...` -- expected: sem warnings

### Patch round (2ª rodada de revisão adversarial, 2026-09-15)

A 2ª rodada de revisão adversarial (2 revisores independentes) sobre o código já implementado achou 2 findings reais, classificados como `patch` (correção mecânica, sem decisão de design pendente) — não dispararam loopback:

1. **NULL em `tipo_apuracao` (créditos) quebrava a transação inteira.** `rfb_creditos.tipo_apuracao` é `VARCHAR(50)` sem `NOT NULL` (migration `096_rfb_creditos.sql`), diferente de `rfb_debitos.tipo_apuracao` (`NOT NULL`). `aggregateRfbCreditosSQL` escaneava a coluna direto para `string` — um valor NULL fazia o `rows.Scan` falhar e abortava a `tx` inteira (rollback de tudo, inserts já bem-sucedidos incluídos). Corrigido com `COALESCE(tipo_apuracao, '')` no SELECT/GROUP BY dessa query (só créditos — débitos não precisa, coluna é NOT NULL lá).
2. **`dataApuracao` não cobria os blocos extemporâneo/ajuste em todas as 4 funções, zerando silenciosamente o resumo do período real.** A captura de `dataApuracao` só rodava em alguns blocos (varia por função — ver Code Map abaixo), nunca em todos. Uma resposta só com itens extemporâneos (sem corrente/ajuste) deixava `dataApuracao=""`, e a agregação SQL filtrava por `data_apuracao=''` — enquanto os próprios itens eram inseridos em `rfb_debitos`/`rfb_creditos` com o período REAL. Pior que o comportamento antigo (que ao menos gravava o total certo, só que sob a chave errada). Corrigido estendendo a captura para todos os blocos (corrente/ajuste/extemporâneo/flat) nas 4 funções.
   - **Créditos embutidos em `ProcessarDownloadRFB`/`ReprocessarRawJSON` (rfb_processor.go):** essas 2 funções processam débitos E créditos comprador embutidos na mesma resposta, mas usavam a MESMA variável `dataApuracao` (derivada dos débitos) para agregar/gravar `rfb_creditos_resumo`. Como o período dos créditos embutidos pode divergir do período dos débitos (ex.: débitos no corrente, créditos só no grupo extemporâneo), foi introduzida uma variável separada `dataApuracaoCreditos`, capturada dos próprios itens de crédito processados (corrente/ajuste/extemporâneo), com fallback para `dataApuracao` (débitos) só se nenhum crédito tiver período próprio. `aggregateRfbCreditosSQL` e o UPSERT de `rfb_creditos_resumo` embutido passaram a usar `dataApuracaoCreditos`, não `dataApuracao`.
   - `ProcessarDownloadCreditosRFB`/`ReprocessarRawJSONCreditosRFB` (rfb_creditos_processor.go) processam só créditos — usam uma única `dataApuracao` (sem necessidade de variável separada). `ReprocessarRawJSONCreditosRFB` já capturava o período uniformemente via closure `insertOne` (nenhuma mudança necessária ali); só `ProcessarDownloadCreditosRFB` tinha blocos inline faltando a captura (ajuste e extemporâneo).

**Testes novos:** `TestProcessarDownloadRFB_ApenasExtemporaneo_PeriodoReal`, `TestProcessarDownloadRFB_CreditosEmbutidos_PeriodoProprioDivergente` (rfb_processor_test.go), `TestProcessarDownloadCreditosRFB_ApenasExtemporaneo_PeriodoReal` (rfb_creditos_processor_test.go) — todos cobrem payload só-extemporâneo e/ou período divergente entre débitos e créditos embutidos.

## Suggested Review Order

**Patch 1 — NULL em tipo_apuracao (créditos) quebrando a transação**

- COALESCE evita que `rows.Scan` falhe com uma linha NULL e aborte a tx inteira.
  [`rfb_processor.go:148`](../../backend/services/rfb_processor.go#L148)

**Patch 2 — dataApuracao não cobria todos os blocos**

- Nova variável `dataApuracaoCreditos`, separada de `dataApuracao` (débitos), para o período real dos créditos embutidos.
  [`rfb_processor.go:295`](../../backend/services/rfb_processor.go#L295)
- Captura de `dataApuracao` estendida ao bloco ApuracaoAjuste (antes só corrente tinha).
  [`rfb_processor.go:343`](../../backend/services/rfb_processor.go#L343)
- Captura de `dataApuracao` estendida ao bloco DebitosExtemporaneos — o caso central do bug (resposta só-extemporânea).
  [`rfb_processor.go:377`](../../backend/services/rfb_processor.go#L377)
- Fallback: `dataApuracaoCreditos` reaproveita `dataApuracao` só se nenhum crédito embutido tiver período próprio.
  [`rfb_processor.go:441`](../../backend/services/rfb_processor.go#L441)
- Agregação SQL dos créditos embutidos agora usa `dataApuracaoCreditos`, não `dataApuracao`.
  [`rfb_processor.go:466`](../../backend/services/rfb_processor.go#L466)
- UPSERT de `rfb_creditos_resumo` embutido grava sob `dataApuracaoCreditos` — período real dos créditos, não o dos débitos.
  [`rfb_processor.go:515`](../../backend/services/rfb_processor.go#L515)
- Mesmo padrão replicado em `ReprocessarRawJSON` (débitos + créditos embutidos).
  [`rfb_processor.go:686`](../../backend/services/rfb_processor.go#L686)
- `ProcessarDownloadCreditosRFB`: captura de `dataApuracao` estendida ao bloco ApuracaoAjuste (inline, faltava).
  [`rfb_creditos_processor.go:228`](../../backend/services/rfb_creditos_processor.go#L228)
- `ProcessarDownloadCreditosRFB`: captura de `dataApuracao` estendida ao bloco DebitosExtemporaneos (inline, faltava).
  [`rfb_creditos_processor.go:271`](../../backend/services/rfb_creditos_processor.go#L271)

**Testes — provam os dois patches**

- Payload só-extemporâneo (débitos): resumo gravado sob o período real, não zerado nem sob `""`.
  [`rfb_processor_test.go:354`](../../backend/services/rfb_processor_test.go#L354)
- Período dos créditos embutidos diverge do período dos débitos — prova a variável separada `dataApuracaoCreditos`.
  [`rfb_processor_test.go:399`](../../backend/services/rfb_processor_test.go#L399)
- Payload só-extemporâneo no endpoint dedicado de créditos.
  [`rfb_creditos_processor_test.go:191`](../../backend/services/rfb_creditos_processor_test.go#L191)
