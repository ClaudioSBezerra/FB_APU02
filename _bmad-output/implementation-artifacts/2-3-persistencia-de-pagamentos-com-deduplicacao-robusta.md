---
baseline_commit: f13b4a78132afcdcdcf0e955dd8064f82fe320be
---

# Story 2.3: Persistência de Pagamentos com Deduplicação Robusta

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a sistema,
I want persistir cada pagamento retornado pelo SAP com uma chave de deduplicação robusta,
so that reprocessamentos nunca criem duplicidade.

Realiza FR-3 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Terceira story da Epic 2 — ver `_bmad-output/planning-artifacts/epics.md`. Continuação direta da Story 2.2 (`done`): `services.ProcessSAPSync` já consulta o SAP, faz retry/rate-limit e parseia a resposta em `dfeResponse`/`paymentItem`, mas hoje apenas loga um resumo (`logBatchSummary`) — **nada é persistido**. Esta story grava os pagamentos retornados em `pagamentos_fornecedores`.

## Acceptance Criteria

1. **Given** uma resposta de `SearchPayments` com `payments[]` não vazio para uma chave, **When** processada, **Then** cada item de `payments[]` vira uma linha própria em `pagamentos_fornecedores` com `origem = 'sap_api'`.
2. **Given** a nova chave de dedup `(company_id, chave_doc, num_doc_pagamento, bukrs)` para linhas de origem SAP, **When** uma chave é reprocessada, **Then** nenhuma linha duplicada é criada — a linha existente é atualizada (upsert idempotente), não duplicada.
3. **Given** a migration desta story, **When** aplicada, **Then** adiciona as colunas `origem`, `payment_status`, `match_type`, `bukrs`, `settlement_mode`, `fallback_ambiguous` a `pagamentos_fornecedores`, **sem quebrar** a deduplicação existente do fluxo de import CSV (`uq_pag_forn`, usada por `ON CONFLICT ON CONSTRAINT uq_pag_forn` em `pagamentos_fornecedores.go`) nem seus dados legados.
4. **Given** um pagamento de um BUKRS que não pertence ao escopo da empresa que originou a consulta, **When** persistido, **Then** nunca é associado a outro `company_id` — a gravação usa exclusivamente o `company_id` já resolvido no contexto da sincronização (nunca um valor vindo da resposta do SAP).

## Tasks / Subtasks

- [x] **Task 1 — Migration: novas colunas + nova constraint de dedup isolada** (AC: #3)
  - [x] Criada `backend/migrations/118_pagamentos_fornecedores_origem_sap.sql`
  - [x] Colunas novas adicionadas (`origem`, `payment_status`, `match_type`, `bukrs`, `settlement_mode`, `fallback_ambiguous`)
  - [x] `import_id` tornado nullable
  - [x] **Desvio do plano original, descoberto via teste falho:** a constraint `uq_pag_forn` (tabela inteira, sem filtro de `origem`) colide com pagamentos SAP legítimos de "pagamentos por conta" (mesmo valor/data, `num_doc_pagamento` diferente) — Postgres não suporta `WHERE` em `CONSTRAINT UNIQUE`, só em índices. Corrigido: `uq_pag_forn` foi **removida** e substituída por dois **índices únicos parciais**, `uq_pag_forn_csv` (`WHERE origem = 'csv'`, mesmas colunas de antes) e `uq_pag_forn_sap_api` (`WHERE origem = 'sap_api'`, novas colunas) — ver Completion Notes para a análise completa
  - [x] **Desvio adicional:** `tipo_doc VARCHAR(10)` estourava com `dfeType = "DESCONHECIDO"` (12 chars) retornado pelo SAP para documentos fora de NF-e/CT-e — alargado para `VARCHAR(20)` na mesma migration (achado via verificação E2E real, não só teste unitário)
  - [x] `backend/handlers/pagamentos_fornecedores.go:249` **precisou ser editado** (1 linha) — `ON CONFLICT ON CONSTRAINT uq_pag_forn` → `ON CONFLICT (company_id, chave_doc, data_pagamento, valor_pagamento) WHERE origem = 'csv'`, consequência direta da mudança acima

- [x] **Task 2 — Função de persistência dos pagamentos** (AC: #1, #2, #4)
  - [x] `persistPayments(db *sql.DB, companyID, bukrs string, items []dfeResponse)` criada em `backend/services/sap_payments_processor.go`
  - [x] Upsert idempotente via `ON CONFLICT (company_id, chave_doc, num_doc_pagamento, bukrs) WHERE origem = 'sap_api' DO UPDATE SET ...`
  - [x] `dfeResponse` sem `payments[]` não gera linha
  - [x] Mapeamento de campos implementado conforme Dev Notes; `bukrs`/`company_id` sempre do parâmetro do contexto, nunca de `dfeResponse.CompanyCode` (AC #4)
  - [x] `import_id`/`importado_por` `NULL` para linhas de origem SAP
  - [x] Heurística de `fallback_ambiguous` implementada e documentada no código

- [x] **Task 3 — Integração no fluxo de sincronização** (AC: todos)
  - [x] `ProcessSAPSync` estendida para `(db *sql.DB, companyID, bukrs, clientID, clientSecret, baseURL string, chaves []string)`
  - [x] `batchOutcome` estendida com `items []dfeResponse`; `processBatch` continua sem acesso a `db`/persistência
  - [x] `persistPayments` chamada após cada lote (mesmo com `outcome.success=false` parcial — itens de sub-lotes bem-sucedidos dentro de uma divisão por 400 ainda são persistidos)
  - [x] Call site em `syncBukrs` (`sap_sync_trigger.go`) atualizado

- [x] **Task 4 — Testes e verificação**
  - [x] 9 testes novos de integração contra Postgres real cobrindo: criação de linha, ausência de `payments[]`, reprocessamento (upsert), "pagamentos por conta" (mesmo valor/data, `num_doc_pagamento` diferente — o cenário que motivou o desvio da Task 1), BUKRS diferentes não colidem, `CompanyCode` da resposta ignorado (AC #4), `clearingDate` inválida ignorada sem abortar o lote, `dfeType` longo (`DESCONHECIDO`)
  - [x] `TestUqPagForn_CSVContinuaFuncionando` — confirma que a dedup do CSV continua funcionando com o novo `ON CONFLICT` baseado em índice parcial
  - [x] Verificação manual E2E contra o `sap-mock-server` real: chave determinística caindo no cenário `PAGO_PARCIAL` (2 parcelas/"pagamento por conta") do mock — 2 linhas persistidas corretamente na 1ª sincronização, 0 linhas novas na 2ª (idempotência real, não só em teste). Foi essa verificação que revelou o achado de `tipo_doc`/`DESCONHECIDO`
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões (29 testes no pacote `services`, incluindo os herdados das Stories 2.1/2.2)

### Review Findings

*(Revisão adversarial — Blind Hunter + Edge Case Hunter — sobre o diff isolado desta story. Acceptance Auditor falhou por limite de sessão da API, não por achado de qualidade; verificação dos 4 ACs suplementada diretamente pelo orquestrador)*

- [x] [Review][Patch] Falha ao persistir não era refletida no status final [backend/services/sap_payments_processor.go] — corrigido: `persistPayments` agora retorna `failedCount int`; `ProcessSAPSync` acumula `persistFailures` e reporta `status='falha'` se qualquer pagamento retornado pelo SAP não puder ser persistido, mesmo com a chamada HTTP bem-sucedida
- [x] [Review][Patch] `DO UPDATE SET` não atualizava `mes_ano`/`tipo_doc`/`forn_cnpj` [backend/services/sap_payments_processor.go] — corrigido: os 3 campos adicionados ao `DO UPDATE SET`; testado em `TestPersistPayments_Reprocessamento_AtualizaEmVezDeDuplicar` (agora também muda de mês/tipo/CNPJ e confirma o refresh)
- [x] [Review][Patch] `ClearingDocument` vazio podia causar overwrite silencioso entre pagamentos do mesmo item [backend/services/sap_payments_processor.go] — corrigido: pagamento com `ClearingDocument` vazio é ignorado e contado como falha, nunca persistido; testado em `TestPersistPayments_ClearingDocumentVazio_NaoSobrescreveOutroPagamento`
- [x] [Review][Patch] Sem `CHECK` constraint restringindo `origem` [backend/migrations/118_pagamentos_fornecedores_origem_sap.sql] — corrigido: `CHECK (origem IN ('csv', 'sap_api'))` adicionada
- [x] [Review][Patch] `DROP CONSTRAINT uq_pag_forn` sem `IF EXISTS` [backend/migrations/118_pagamentos_fornecedores_origem_sap.sql] — corrigido: `DROP CONSTRAINT IF EXISTS`, migration segura para reexecução após falha parcial
- [x] [Review][Patch] Faltava teste de isolamento multi-tenant real (company_id diferente) [backend/services/sap_payments_persist_test.go] — corrigido: `TestPersistPayments_CompanyIdDiferentes_NaoColidem`, duas empresas com mesma chave/bukrs/num_doc_pagamento
- [x] [Review][Patch] `PaidAmount <= 0` não tratado explicitamente [backend/services/sap_payments_processor.go] — corrigido: filtrado antes do INSERT com log determinístico; testado em `TestPersistPayments_PaidAmountZeroOuNegativo_Ignorado`
- [x] [Review][Patch] Faltava teste do caminho `FALLBACK`/`fallback_ambiguous=true` [backend/services/sap_payments_persist_test.go] — corrigido: `TestPersistPayments_FallbackAmbiguo_MarcaTrue`
- [x] [Review][Patch] Handler sem comentário local explicando o `WHERE origem = 'csv'` [backend/handlers/pagamentos_fornecedores.go] — corrigido: comentário adicionado no ponto de uso

- [x] [Review][Defer] Índices únicos novos criados sem `CONCURRENTLY` (bloqueiam escritas durante o build) — deferred, consistente com o padrão já usado em todas as migrations existentes do projeto (nenhuma usa `CONCURRENTLY`); exigiria mudar a arquitetura do runner de migrations (hoje executa cada arquivo como um único `Exec`, incompatível com `CONCURRENTLY` dentro de transação implícita)
- [x] [Review][Defer] `persistPayments` sem transação nem `context.Context` (N round-trips seriais ao Postgres, sem cancelamento coordenado) — deferred, mesmo padrão já deferido nas Stories 2.1/2.2, ausente em todo o pacote `services`
- [x] [Review][Defer] `DFeType`/`SupplierCNPJ` poderiam teoricamente estourar os novos limites (`VARCHAR(20)`/`VARCHAR(14)`) — deferred, baixo risco real (CNPJ é formato fixo de 14 dígitos; `DFeType` já foi alargado com folga após o achado de `DESCONHECIDO`)
- [x] [Review][Defer] `DFeKey` da resposta não é revalidado (`isValidDFeKey`) antes de persistir — deferred, baixo risco (o SAP ecoa a mesma chave que foi enviada, já validada por `filterValidKeys` antes do request)
- [x] [Review][Defer] Nenhuma checagem prévia de dados legados duplicados antes de criar `uq_pag_forn_csv` — deferred, na prática não é risco real nesta migration específica: a constraint `uq_pag_forn` anterior já garantia unicidade de todo o histórico CSV existente antes desta story, e um subconjunto de um conjunto já único continua único — documentado o raciocínio para builds futuras que reaproveitem esse padrão

### Schema atual de `pagamentos_fornecedores` (migration 110, já em produção)

```sql
CREATE TABLE IF NOT EXISTS pagamentos_fornecedores (
    id                BIGSERIAL PRIMARY KEY,
    company_id        UUID          NOT NULL REFERENCES companies(id),
    chave_doc         VARCHAR(100)  NOT NULL,
    tipo_doc          VARCHAR(10)   NOT NULL,
    forn_cnpj         VARCHAR(14)   NOT NULL,
    forn_nome         VARCHAR(255),
    data_pagamento    DATE          NOT NULL,
    valor_pagamento   NUMERIC(15,2) NOT NULL CHECK (valor_pagamento > 0),
    num_doc_pagamento VARCHAR(100),
    descricao         TEXT,
    mes_ano           VARCHAR(7)    NOT NULL,
    import_id         UUID          NOT NULL,
    importado_em      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    importado_por     UUID          REFERENCES users(id),
    CONSTRAINT uq_pag_forn UNIQUE (company_id, chave_doc, data_pagamento, valor_pagamento)
);
```
`num_doc_pagamento` já é nullable hoje — muitas linhas de CSV legado o têm `NULL` (confirmado em `pagamentos_fornecedores.go`, o handler de import só preenche se a coluna vier não-vazia do CSV). `bukrs` não existe ainda (adicionado por esta story).

### ⚠️ Risco crítico identificado: NÃO trocar a constraint de dedup por uma só geral

O addendum do PRD (seção C) sugere trocar a constraint de `(company_id, chave_doc, data_pagamento, valor_pagamento)` para `(company_id, chave_doc, num_doc_pagamento, bukrs)` de forma geral. **Isso é perigoso e não deve ser feito assim**: no Postgres, `NULL` nunca é considerado igual a outro `NULL` em uma constraint `UNIQUE` — como `num_doc_pagamento` e `bukrs` são `NULL` em parte relevante do CSV legado, uma constraint única "geral" com essas colunas **para de deduplicar completamente as linhas de CSV que tiverem esses campos nulos**, silenciosamente, sem erro nenhum. Isso é uma regressão de integridade de dados grave para um sistema fiscal.

**Decisão desta story:** manter `uq_pag_forn` **intocada** (dedup do CSV continua exatamente como está) e criar um **índice único parcial** `uq_pag_forn_sap_api`, escopado só a `WHERE origem = 'sap_api'` — como todo pagamento de origem SAP grava `num_doc_pagamento` e `bukrs` sempre preenchidos (nunca `NULL`, ver Task 2), esse índice funciona como dedup real e robusto só para o universo de linhas que a Story 2.3 cria, sem qualquer interação com o universo de linhas de CSV.

### ⚠️ `import_id` é `NOT NULL` hoje — precisa ficar nullable

Pagamentos de origem SAP não têm um "batch de importação CSV" associado. A migration desta story precisa `ALTER COLUMN import_id DROP NOT NULL` antes de gravar qualquer linha com `import_id = NULL`. Constraint aditiva, segura, não quebra nada do fluxo CSV existente (`import_id` continua sendo preenchido normalmente por ele).

### Fluxo de import CSV existente — não pode quebrar (`backend/handlers/pagamentos_fornecedores.go:243-253`)

```go
result, execErr := tx.Exec(`
    INSERT INTO pagamentos_fornecedores
        (company_id, chave_doc, tipo_doc, forn_cnpj, forn_nome,
         data_pagamento, valor_pagamento, num_doc_pagamento, descricao,
         mes_ano, import_id, importado_por)
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::uuid, $12::uuid)
    ON CONFLICT ON CONSTRAINT uq_pag_forn DO NOTHING`,
    ...)
```
Este código referencia `uq_pag_forn` **pelo nome** — se essa constraint for renomeada ou removida, esse `INSERT` quebra em runtime (erro do Postgres "constraint does not exist"), não em tempo de compilação Go. Confirmar ao final da story que este handler continua funcionando sem nenhuma alteração.

### Query de conciliação existente — não referencia as colunas novas (`backend/handlers/rfb_pagamentos_fornecedores.go:52-113`)

A CTE `pagamentos_agg`/`conciliacao` faz `SELECT` explícito de colunas (não `SELECT *`) e agrega por `chave_doc`/`tipo_doc`/`forn_cnpj` — adicionar colunas novas a `pagamentos_fornecedores` **não quebra** essa query. As colunas `origem`/`payment_status`/etc. só serão consumidas por stories futuras (2.5, 3.x) — esta story não precisa (e não deve) alterar essa query.

### Mapeamento de campos: `dfeResponse`/`paymentItem` → `pagamentos_fornecedores`

Structs atuais em `backend/services/sap_payments_processor.go` (Story 2.2, já `done`):
```go
type dfeResponse struct {
    DFeKey, DFeType, MatchType, CompanyCode, SupplierCNPJ, SupplierID string
    InvoiceDocument, InvoiceFiscalYear, InvoiceDate                  string
    InvoiceAmount, TotalPaidAmount                                   float64
    Currency, PaymentStatus                                          string
    Payments                                                         []paymentItem
    FallbackNote                                                     string // omitempty
}
type paymentItem struct {
    ClearingDocument, ClearingDate, PaymentPostingDate string
    PaidAmount                                         float64
    PaymentMethod, PaymentMethodDesc, SettlementMode    string
    IsPartial                                           bool
}
```
- `ClearingDate`/`InvoiceDate` chegam como string `"2006-01-02"` (confirmado no `sap-mock-server/main.go`, ex. `ClearingDate: "2026-06-10"`) — usar `time.Parse("2006-01-02", ...)` para converter antes de gravar em `data_pagamento DATE`.
- `dfeResponse.CompanyCode` **não deve ser usado** para preencher a coluna `bukrs` nem para decidir `company_id` — usar sempre o `bukrs`/`companyID` já resolvidos no contexto da chamada (parâmetros de `ProcessSAPSync`), nunca um valor vindo da resposta externa (AC #4, isolamento multi-tenant).
- `fallback_ambiguous`: não existe campo booleano explícito no contrato. Heurística adotada (verificada contra o comportamento real do mock, `sap-mock-server/main.go:391`, que só popula `FallbackNote` no cenário de múltiplos candidatos): `MatchType == "FALLBACK" && FallbackNote != ""`. Documentar essa heurística no código — se o SAP real vier a expor um campo explícito de ambiguidade, ajustar aqui.
- `payment_status`/`match_type` são por `dfeResponse` (por chave/documento), não por `paymentItem` — todas as linhas de `payments[]` de uma mesma chave recebem o mesmo `payment_status`/`match_type` do `dfeResponse` pai.

### Ponto exato de integração

`ProcessSAPSync` (`sap_payments_processor.go:392`) hoje: `func ProcessSAPSync(companyID, clientID, clientSecret, baseURL string, chaves []string) SyncResult` — sem `db` nem `bukrs`. `processBatch` (linha 284) não tem acesso a banco nem a `companyID`/`bukrs` — mantenha essa separação (HTTP/retry vs. persistência) estendendo o retorno de `processBatch`/`batchOutcome` para incluir os itens parseados (`[]dfeResponse`), e faça a persistência em `ProcessSAPSync`, não dentro de `processBatch`.

Call site a atualizar: `backend/services/sap_sync_trigger.go`, dentro de `syncBukrs` — `result := ProcessSAPSync(companyID, clientID, clientSecret, baseURL, chaves)` precisa virar algo como `result := ProcessSAPSync(db, companyID, bukrs, clientID, clientSecret, baseURL, chaves)` (`db`/`bukrs` já estão disponíveis no escopo de `syncBukrs`).

### Próximo número de migration livre

Maior migration existente: `117_sap_sync_runs_falha_credencial.sql` → **próximo número livre é `118`**.

### Fora de escopo desta story (não implementar)

- Contagens detalhadas por `paymentStatus` em `sap_sync_runs`, tela de histórico, alertas — Story 2.4
- Sinalização de `total_pago > valor_nota`, prevalência de `sap_api` sobre `csv` em caso de conflito de valor — Story 2.5
- Inversão da query de conciliação para exibir créditos sem pagamento localizado — Story 3.1
- Filtro dedicado de revisão de `fallback_ambiguous` na tela — Story 3.3 (esta story só grava o dado; a UI de revisão é depois)
- "Estorno"/cancelamento de pagamento: filtro puramente do lado SAP (campo `BLART`, Open Question #6 do PRD ainda não resolvida) — o FB_APU02 não tem e não precisa ter lógica própria para isso; os `payments[]` que chegam já vêm filtrados do lado SAP

### Project Structure Notes

- Novo arquivo: `backend/migrations/118_pagamentos_fornecedores_origem_sap.sql`
- Editado: `backend/services/sap_payments_processor.go` (`ProcessSAPSync`/`processBatch`/`batchOutcome` estendidos; nova função de persistência)
- Editado: `backend/services/sap_sync_trigger.go` (call site de `ProcessSAPSync` atualizado)
- **Não editar**: `backend/handlers/pagamentos_fornecedores.go`, `backend/handlers/rfb_pagamentos_fornecedores.go`, `backend/migrations/110_pagamentos_fornecedores.sql`, `111_pagamentos_imports.sql` — apenas confirmar que continuam funcionando

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 2, Story 2.3] — AC originais (linhas 208-231)
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md#FR-3]
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/addendum.md#C] — esboço de migration (ponto de partida; esta story diverge dele no design da constraint, ver Dev Notes acima)
- [Source: backend/migrations/110_pagamentos_fornecedores.sql, 111_pagamentos_imports.sql] — schema atual
- [Source: backend/handlers/pagamentos_fornecedores.go:226-266] — fluxo de import CSV, dependência do nome `uq_pag_forn`
- [Source: backend/handlers/rfb_pagamentos_fornecedores.go:51-113] — query de conciliação existente, confirmado que não quebra com as novas colunas
- [Source: backend/services/sap_payments_processor.go] — `dfeResponse`/`paymentItem`/`ProcessSAPSync`/`processBatch` (Story 2.2, já `done`)
- [Source: backend/services/sap_sync_trigger.go] — `syncBukrs`, call site de `ProcessSAPSync`
- [Source: sap-mock-server/main.go:391] — comportamento real de `FallbackNote`, base da heurística de `fallback_ambiguous`
- [Source: _bmad-output/implementation-artifacts/2-2-consulta-em-lote-com-rate-limit-e-tratamento-de-erros.md] — story anterior, código que esta story estende diretamente

## Dev Agent Record

### Agent Model Used

Claude Sonnet 5 (claude-sonnet-5)

### Debug Log References

- `go build ./...`, `go vet ./...` — limpos após cada task
- Migration `118` aplicada em `fiscal_db` real via `psql`, iterada 2 vezes durante a implementação (constraint `uq_pag_forn` corrigida, `tipo_doc` alargado) conforme achados de teste/E2E
- `go test ./services/... -v` — 33 testes passando no total (13 de persistência + os 20 herdados/atualizados das Stories 2.1/2.2), após a rodada de patches do code review (5 testes novos: `ClearingDocument` vazio, `PaidAmount` ≤0, isolamento por `company_id`, `FALLBACK`/`fallback_ambiguous`, e o reforço do teste de reprocessamento)
- `go test ./...` (suíte completa) — sem regressões, antes e depois dos patches
- **Verificação manual E2E contra `sap-mock-server` real**: chave de 44 dígitos escolhida deliberadamente (via `digitSum(chave) % 5 == 1`) para cair no cenário `PAGO_PARCIAL` do mock (2 parcelas — "pagamento por conta"). 1ª sincronização: 2 linhas persistidas em `pagamentos_fornecedores` com todos os campos corretos (`origem=sap_api`, `payment_status=PAGO_PARCIAL`, `bukrs=1000`, `match_type=CHAVE`, `settlement_mode=OUTROS_MEIOS_PAGAMENTO`, `fallback_ambiguous=false`). 2ª sincronização (mesmo `requestID`): 0 linhas novas (idempotência de disparo da Story 2.1 já impede nova chamada ao SAP). **Esta verificação revelou o achado do `tipo_doc`/`DESCONHECIDO`** — a chave de teste não teve os dígitos de modelo NF-e/CT-e (`[20:22] = "55"/"57"`), então o mock retornou `dfeType="DESCONHECIDO"` (12 chars), estourando o `VARCHAR(10)` original. Dados de teste removidos ao final.

### Completion Notes List

- **Achado crítico descoberto por teste, não por análise prévia:** a constraint original `uq_pag_forn` (migration 110) é *table-wide*, sem filtro de `origem`. Meu primeiro design da migration 118 (aditivo, só criando `uq_pag_forn_sap_api` e deixando `uq_pag_forn` intocada) parecia seguro na análise, mas um teste (`TestPersistPayments_BukrsDiferentes_NaoColidem`) falhou com "duplicate key value violates unique constraint uq_pag_forn": dois pagamentos SAP legítimos com mesma `chave_doc`/`data_pagamento`/`valor_pagamento` mas `bukrs`/`num_doc_pagamento` diferentes colidiam na constraint ANTIGA, que não sabe nada sobre BUKRS ou num_doc_pagamento. Isso é exatamente o cenário de negócio "pagamentos por conta" que o usuário descreveu na fase de PRD (parcelas de mesmo valor no mesmo dia) — ou seja, o próprio requisito que motivou a nova chave de dedup ficaria quebrado pela constraint antiga continuar valendo para todo mundo. Corrigido substituindo `uq_pag_forn` por dois índices únicos parciais (`uq_pag_forn_csv`/`uq_pag_forn_sap_api`), cada um só "enxergando" sua própria `origem`. Isso exigiu editar `pagamentos_fornecedores.go` (1 linha), o que a story original explicitamente dizia para não fazer — decisão corrigida durante a implementação, com justificativa registrada na migration e neste changelog.
- **Segundo achado, descoberto só na verificação E2E manual (não pelos testes com dados sintéticos "NFE"):** `tipo_doc VARCHAR(10)` estourava com `dfeType = "DESCONHECIDO"` do SAP. Meus testes unitários usavam sempre `DFeType: "NFE"` (8 caracteres, cabia), então só a verificação E2E contra o mock real (que decide o `dfeType` a partir dos dígitos da própria chave, não de um valor fixo que eu controlava) expôs o problema. Corrigido alargando a coluna para `VARCHAR(20)` — seguro porque a conciliação só compara `tipo_doc` a `'NFE'`/`'CTE'` literalmente.
- `persistPayments` isola falhas por item (um erro de `INSERT` em uma linha não impede as demais) e por lote (falha ao persistir nunca aborta o restante da sincronização) — mesmo princípio de isolamento já usado em toda a Epic 2.
- `fallback_ambiguous` usa uma heurística (`MatchType == "FALLBACK" && FallbackNote != ""`) verificada contra o comportamento real do `sap-mock-server` (que só popula `FallbackNote` no cenário de múltiplos candidatos) — não há campo booleano explícito no contrato atual; documentado no código para ajuste futuro se o SAP real expuser um sinal mais preciso.
- Escopo mantido dentro da story: nenhuma mudança em `rfb_pagamentos_fornecedores.go` (conciliação) nem nas contagens/histórico de `sap_sync_runs` (Story 2.4) nem coexistência CSV/SAP com sinalização de conflito (Story 2.5).
- **Pós code-review (Blind Hunter + Edge Case Hunter — Acceptance Auditor falhou por limite de sessão da API, não por achado de qualidade; os 4 ACs foram verificados diretamente pelo orquestrador):** achado mais relevante — falha ao persistir um pagamento não era refletida no status final da sincronização (`ProcessSAPSync` só considerava a chamada HTTP ao SAP); corrigido propagando uma contagem de falhas de `persistPayments` até `SyncResult`. Também corrigidos: `DO UPDATE SET` incompleto (não refrescava `mes_ano`/`tipo_doc`/`forn_cnpj`), `ClearingDocument` vazio podendo causar overwrite silencioso entre pagamentos do mesmo item, `PaidAmount` ≤0 sem tratamento explícito, `CHECK` constraint faltando em `origem`, `DROP CONSTRAINT` sem `IF EXISTS` (risco de travar reexecução da migration), e 2 lacunas de cobertura de teste (isolamento por `company_id`, caminho `FALLBACK`/`fallback_ambiguous=true`). 5 itens deferidos (ver `deferred-work.md`) — principalmente padrões já deferidos em stories anteriores ou de baixo risco real dado o formato fixo dos dados envolvidos.

### File List

- `backend/migrations/118_pagamentos_fornecedores_origem_sap.sql` (novo, revisado pós code-review — `CHECK` de `origem`, `DROP CONSTRAINT IF EXISTS`)
- `backend/services/sap_payments_processor.go` (editado, revisado pós code-review — `persistPayments` retorna `failedCount`; `DO UPDATE SET` completo; guardas de `ClearingDocument`/`PaidAmount`; `ProcessSAPSync`/`batchOutcome`/`processBatch` estendidos)
- `backend/services/sap_payments_persist_test.go` (novo, revisado pós code-review — 13 testes)
- `backend/services/sap_payments_processor_test.go` (editado — 3 chamadas a `ProcessSAPSync` atualizadas para a nova assinatura)
- `backend/services/sap_sync_trigger.go` (editado — call site de `ProcessSAPSync` atualizado)
- `backend/handlers/pagamentos_fornecedores.go` (editado, revisado pós code-review — 1 linha `ON CONFLICT` + comentário explicativo; desvio do plano original, ver Completion Notes)
- `_bmad-output/implementation-artifacts/deferred-work.md` (editado — 5 achados de code review desta story adicionados)

### Change Log

- 2026-07-11: Implementação completa da Story 2.3 — migration com novas colunas e dedup isolada por origem (com correção de design durante a implementação: `uq_pag_forn` trocada por dois índices parciais em vez de mantida intocada, e `tipo_doc` alargado), função `persistPayments` com upsert idempotente, integração em `ProcessSAPSync`/`syncBukrs`, 9 testes novos + verificação E2E manual contra o `sap-mock-server` real (cenário "pagamento por conta"). Sem regressões.
- 2026-07-11: Code review (Blind Hunter + Edge Case Hunter; Acceptance Auditor falhou por limite de sessão da API) — 9 patches aplicados (falha de persistência refletida no status, upsert completo, guardas de `ClearingDocument`/`PaidAmount`, `CHECK` de `origem`, `DROP CONSTRAINT IF EXISTS`, comentário no handler, 2 testes de cobertura novos), 5 testes novos, 5 itens deferidos.
