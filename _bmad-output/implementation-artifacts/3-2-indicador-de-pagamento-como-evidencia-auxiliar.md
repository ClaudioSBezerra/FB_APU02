---
baseline_commit: eeffaed3719ea6d54f6ad1c21d687d3a0811999d
---

# Story 3.2: Indicador de Pagamento como Evidência Auxiliar

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a Ana (analista fiscal),
I want ver um indicador de pagamento (localizado/não localizado, origem, motivo) em cada crédito sem que ele jamais altere a decisão de extinção da RFB,
so that eu sempre saiba que o status oficial veio da RFB.

Realiza FR-4/FR-8 do PRD `_bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md`. Segunda story da Epic 3 — ver `_bmad-output/planning-artifacts/epics.md`. Depende da Story 3.1 (`done`, inversão da query de conciliação — `rfb_pagamentos_fornecedores.go`) e das Stories 2.1-2.5 da Epic 2 (`done`, pipeline de sincronização SAP). **Esta story toca dois pontos que a Epic 2 já considerava fechados**: `services/sap_payments_processor.go` (persistência) e `handlers/rfb_pagamentos_fornecedores.go` (exibição) — ver a decisão de arquitetura abaixo antes de codificar.

## ⚠️ Decisão de arquitetura já tomada (confirmada com o usuário antes desta story ser escrita)

O AC #3 exige mostrar, por crédito, se a última tentativa do SAP foi `EM_ABERTO` ou `NAO_LOCALIZADO`, com a data da tentativa. **Isso não é possível com o schema atual**: `persistPayments` (`sap_payments_processor.go:433-486`) só grava linha em `pagamentos_fornecedores` quando `len(item.Payments) > 0` — itens `EM_ABERTO`/`NAO_LOCALIZADO` (sem `payments[]`) são propositalmente descartados (comentário na linha 421: "não gera nenhuma linha, propositalmente"), confirmado também pelo PRD (`prd.md:126`: "EM_ABERTO e NAO_LOCALIZADO não geram linha em pagamentos_fornecedores, mas ficam registrados no log de execução (FR-7)"). Hoje esse dado só existe como **contagem agregada por execução** (`sap_sync_runs`, Story 2.4) — nunca por chave individual.

**Decisão adotada (confirmada com o usuário)**: criar uma nova tabela (`sap_resultados_busca`) que grava, por `(company_id, chave_dfe)`, o resultado da última tentativa de busca quando ela NÃO gerou pagamento (`EM_ABERTO`/`NAO_LOCALIZADO`), com `match_type`, `fallback_note` e a data da tentativa. Isso exige tocar `persistPayments` (Epic 2, já `done`) para também gravar esses casos, além da tela de conciliação. Ver Task 1.

## Acceptance Criteria

1. **Given** um crédito com pagamento `payment_status` `PAGO_TOTAL`/`PAGO_PARCIAL` do SAP, **When** exibido na tela de conciliação, **Then** mostra um indicador "pagamento localizado" — sem alterar `status_conciliacao` (que continua vindo exclusivamente de `rfb_creditos.valor_cbs_nao_extinto`, nunca do SAP).
2. **Given** `status_conciliacao` sempre derivado de `rfb_creditos.valor_cbs_nao_extinto` (nunca do SAP), **When** a soma de pagamentos localizados cobre o valor da nota (`total_pago >= valor_nota`) mas o crédito continua `pendente` (RFB não confirmou extinção), **Then** a tela mostra uma mensagem distinta — "pagamento localizado, aguardando confirmação da RFB" — sem jamais reclassificar o crédito.
3. **Given** um crédito cuja última tentativa de busca no SAP foi `NAO_LOCALIZADO`, **When** exibido, **Then** mostra mensagem distinta de um crédito `EM_ABERTO` ("não localizamos o documento" vs. "localizamos o documento, mas ele ainda não foi pago"), com a data da última tentativa.
4. **Given** um crédito com pagamento de `origem = sap_api` vs `origem = csv`, **When** exibido na tela de conciliação (não apenas no drill-down de parcelas, já feito na Story 2.5), **Then** a origem é visualmente distinguível no item agregado.

## Tasks / Subtasks

- [x] **Task 1 — Nova tabela `sap_resultados_busca` para EM_ABERTO/NAO_LOCALIZADO por chave** (pré-requisito da AC #3)
  - [x] Migration `backend/migrations/121_sap_resultados_busca.sql` criada, aplicada com sucesso no banco de teste
  - [x] `persistPayments` (`sap_payments_processor.go`): o branch `if len(item.Payments) == 0` agora faz `UPSERT` em `sap_resultados_busca` quando `item.PaymentStatus` é `'EM_ABERTO'`/`'NAO_LOCALIZADO'`, antes do `continue`
  - [x] Quando um pagamento real é persistido com sucesso, `DELETE FROM sap_resultados_busca` limpa qualquer tentativa anterior sem sucesso para a mesma chave
  - [x] `company_id`/`bukrs` gravados são sempre os parâmetros do contexto da sincronização, nunca da resposta do SAP (mesmo padrão já documentado)
  - [x] 4 testes novos em `sap_payments_persist_test.go`: `TestPersistPayments_NaoLocalizado_GravaSapResultadosBusca`, `TestPersistPayments_EmAberto_GravaSapResultadosBusca`, `TestPersistPayments_ReprocessamentoNaoLocalizado_AtualizaUltimaTentativa`, `TestPersistPayments_PagamentoRealAposNaoLocalizado_ApagaSapResultadosBusca` — fase RED confirmada (4 falharam antes da implementação), todos os 16 testes de `persistPayments` passando depois (12 originais + 4 novos, sem regressão)

- [x] **Task 2 — Expor `payment_status`/`match_type`/`fallback_ambiguous`/`origem` no nível do item agregado de conciliação** (AC: #1, #4)
  - [x] `pagamentos_agg` expõe valor representativo via `(ARRAY_AGG(... ORDER BY data_pagamento DESC))[1]` (valor da parcela mais recente, não `MAX()` alfabético) para `payment_status`/`match_type`/`origem`; `BOOL_OR` para `fallback_ambiguous`
  - [x] `ConciliacaoItem`: `PaymentStatus *string`, `MatchType *string`, `FallbackAmbiguous bool`, `Origem string` adicionados
  - [x] Frontend: componente `OrigemBadge` (nova coluna "Origem" na tabela principal, mesmo padrão CSV/SAP das parcelas) + `PaymentIndicator` mostrando "Pagamento localizado" quando `payment_status` é `PAGO_TOTAL`/`PAGO_PARCIAL`

- [x] **Task 3 — Mensagem "aguardando confirmação da RFB"** (AC: #2)
  - [x] `PagamentoCobreValorNota bool` em `ConciliacaoItem`, calculado em `conciliacao_creditos`: `pa.chave_doc IS NOT NULL AND rc.valor_cbs_nao_extinto > 0 AND valor_nota IS NOT NULL AND total_pago >= valor_nota` (mesma checagem `IS NOT NULL` de `possivel_duplicidade`, Story 2.5, reaproveitada)
  - [x] Frontend: `PaymentIndicator` mostra "Pagamento localizado, aguardando confirmação da RFB" quando `true`, abaixo do `StatusBadge` (que continua `pendente`, sem alteração de cor/categoria)

- [x] **Task 4 — Mensagens distintas para créditos sem pagamento (`aguardando_pagamento`)** (AC: #3)
  - [x] `conciliacao_creditos` faz `LEFT JOIN sap_resultados_busca srb ON srb.company_id = $1 AND srb.chave_dfe = rc.chave_dfe` — como `persistPayments` já apaga a linha de `sap_resultados_busca` quando um pagamento real é encontrado (Task 1), o LEFT JOIN naturalmente só traz dado quando realmente não há pagamento, sem precisar de CASE extra
  - [x] `UltimoStatusBusca *string`, `UltimaTentativaBusca *string` adicionados a `ConciliacaoItem`
  - [x] Frontend: `PaymentIndicator` mostra "Não localizamos o documento no SAP" (+ data) para `NAO_LOCALIZADO`, "Localizamos o documento, mas ainda não foi pago" (+ data) para `EM_ABERTO`, nenhuma mensagem extra quando nunca tentado (badge genérico "Aguardando Pagamento" já basta)

- [x] **Task 5 — Testes e verificação**
  - [x] Harness reaproveitado; adicionados `insertPagamentoSAPComStatus`, `insertSapResultadoBusca`, `queryIndicadoresPagamento` (não `insertSapResultadoBusca` sozinho — helpers extras necessários para controlar `payment_status`/`match_type` explicitamente)
  - [x] `TestConciliacao_PagoTotal_IndicadorLocalizado_NaoAlteraStatus` — AC #1
  - [x] `TestConciliacao_PagamentoCobreNota_AguardandoConfirmacaoRFB` — AC #2 (caso cobre + caso normal)
  - [x] `TestConciliacao_UltimoStatusBusca_NaoLocalizadoVsEmAberto` — AC #3 (NAO_LOCALIZADO, EM_ABERTO, nunca tentado)
  - [x] `TestConciliacao_Origem_VisivelNoItemAgregado` — AC #4
  - [x] `TestPersistPayments_PagamentoRealAposNaoLocalizado_ApagaSapResultadosBusca` (Task 1, `services`) — pagamento real limpa `sap_resultados_busca`
  - [x] Regressão: os 14 testes já existentes de `rfb_pagamentos_fornecedores_test.go` (Stories 2.5 + 3.1) passam sem nenhuma alteração — 18/18 testes de `handlers`, 16/16 de `persistPayments` (`services`)
  - [x] `go build ./...`, `go vet ./...`, `go test ./...` sem erros/regressões
  - [x] `npx tsc --noEmit`: 26 erros — mesma baseline pré-existente, nenhum novo

### Review Findings

*(Revisão adversarial em Opus — Blind Hunter + Acceptance Auditor + verificação direta do orquestrador. Edge Case Hunter FALHOU por limite de sessão da API (reseta 14h), não por achado de qualidade — mitigado: as 5 investigações que ele receberia foram conduzidas diretamente pelo orquestrador, incluindo reprodução empírica do bug crítico contra Postgres real.)*

- [x] [Review][Patch] **Bug crítico confirmado empiricamente**: `pagamento_cobre_valor_nota` resolvia para `NULL` quando `rc.valor_cbs_nao_extinto` é `NULL` (coluna NULLABLE — migration 096, `DEFAULT 0` mas sem `NOT NULL`) + nota casada + pagamento cobrindo → `TRUE AND NULL AND TRUE AND TRUE = NULL`. O `Scan` em `bool` (não `*bool`) derrubava a **listagem inteira** com 500 (mesma classe do bug de `tipo_doc` NULL corrigido na 3.1). [backend/handlers/rfb_pagamentos_fornecedores.go] — corrigido: expressão envolvida em `COALESCE(..., false)`. Reproduzido contra Postgres real (`converting NULL to bool`) antes/depois da correção; teste `TestConciliacao_ValorCbsNaoExtintoNull_NaoQuebraScanDeCobreValorNota`
- [x] [Review][Patch] Desempate não determinístico nos 3 `ARRAY_AGG` independentes (`payment_status`/`match_type`/`origem`) em `pagamentos_agg`: 2 parcelas da mesma chave/tipo/CNPJ na MESMA `data_pagamento` mas com origem/status diferentes podiam fazer cada agregação escolher uma linha distinta, gerando uma tripla incoerente (indicador auxiliar, nunca decide status — FR-4 — mas ainda assim confuso) [backend/handlers/rfb_pagamentos_fornecedores.go] — corrigido: desempate estável `ORDER BY pf.data_pagamento DESC, pf.id DESC` nas 3 agregações
- [x] [Review][Patch] Índice `idx_sap_resultados_busca_chave` redundante — a constraint `UNIQUE (company_id, chave_dfe)` já cria um índice B-tree idêntico sobre as mesmas colunas [backend/migrations/121_sap_resultados_busca.sql] — corrigido: `CREATE INDEX` removido (comentário explica que o UNIQUE já cobre o LEFT JOIN)
- [x] [Review][Patch] Contagem de testes inconsistente na prosa da Task 1 ("5 testes novos" mas lista 4; "17 testes / 13+4") [este arquivo] — corrigido para o valor real (4 novos em services, 12 originais = 16)
- [ ] [Review][Defer] Coexistência `sap_resultados_busca` + pagamento real: se uma chave tem pagamento real e DEPOIS o SAP retorna NAO_LOCALIZADO/EM_ABERTO (estorno/correção/doc some do SAP), o branch `len(Payments)==0` grava em `sap_resultados_busca` sem remover a linha de pagamento — as duas coexistem. Razão do adiamento: não é visivelmente errado hoje (o frontend só mostra `ultimo_status_busca` quando `status_conciliacao='aguardando_pagamento'`, o que não ocorre quando há pagamento); apagar o pagamento numa resposta "não localizado" seria perigoso (um glitch transitório do SAP apagaria histórico real de pagamento). A linha `srb` órfã fica inerte. **Revisitar** se cenários de estorno SAP se tornarem comuns e a evidência inerte causar confusão
- [ ] [Review][Defer] `INSERT`/`DELETE` de `sap_resultados_busca` são `db.Exec` separados sem transação — duas sincronizações concorrentes para a mesma `(company_id, chave_dfe)` podem intercalar e deixar uma linha `srb` órfã ao lado de um pagamento real. Razão do adiamento: mesma classe dos itens de concorrência já adiados nas Stories 2.1 (goroutines fire-and-forget sem lock) e 2.4 (sem lock no check-and-send de alertas); corrigir corretamente exige trava por chave em todo o pipeline SAP, fora do escopo desta story. **Revisitar** junto com o item de concorrência da Story 2.4
- [ ] [Review][Defer] `DELETE FROM sap_resultados_busca` roda dentro do loop de `payments` (N vezes por item, idempotente porém redundante) e, se TODOS os pagamentos de um item falharem ao persistir, a linha `srb` de uma tentativa anterior sobrevive afirmando "não localizado". Razão do adiamento: o DELETE repetido é inofensivo (idempotente); o edge de "todos os pagamentos falham no INSERT" exige erro de banco (raro, já logado) e, nesse caso, o pagamento de fato não foi persistido — mostrar o último status conhecido não está claramente errado. Baixo impacto, restruturar o loop logo após um fix crítico traz mais risco que benefício. **Revisitar** se falhas de persistência se mostrarem recorrentes
- [ ] [Review][Defer] `conciliacao = SELECT * FROM conciliacao_creditos UNION ALL SELECT * FROM pagamentos_orfaos` depende de ordem posicional idêntica das 22 colunas nas duas pernas; com `SELECT *`, uma reordenação/adição futura em só uma das CTEs desalinha silenciosamente colunas de mesmo tipo. Razão do adiamento: padrão `SELECT *` foi estabelecido na Story 3.1 (já revisada e aceita); listar as 22 colunas explicitamente nas duas pernas é um refactor maior da estrutura da 3.1, com risco próprio de erro de transcrição. **Revisitar** se a CTE crescer mais (ex.: Story 3.3)
- [ ] [Review][Defer] `sap_resultados_busca.payment_status` sem `CHECK` constraint (domínio só no comentário) — o guard existe apenas no Go (`persistPayments`). Razão do adiamento: consistente com `pagamentos_fornecedores.payment_status`, que também não tem CHECK; caminho de escrita é único e controlado. Endurecimento de baixa severidade
- [x] [Review][Dismiss] "Variável `fallbackAmbiguous` não usada / build quebrado" (Blind Hunter) — falso alarme: o diff colado elidiu o INSERT com `...`; no código real a variável é usada no INSERT de `pagamentos_fornecedores`, e `go build`/`go vet` passam limpos
- [x] [Review][Dismiss] "PAGO_PARCIAL apaga a evidência EM_ABERTO do saldo remanescente" (Blind Hunter) — investigado: o SAP retorna UM `paymentStatus` por chave por resposta; PAGO_PARCIAL vem com `payments[]` e não há entrada EM_ABERTO separada na mesma resposta. O DELETE remover uma linha EM_ABERTO de uma tentativa ANTERIOR quando agora há PAGO_PARCIAL é correto (a tentativa antiga foi superada por um pagamento real)
- [x] [Review][Dismiss] `sumSQL`/`countSQL` não mudaram enquanto `listSQL` ganhou o LEFT JOIN `srb` — correto e seguro: `srb` é 1:1 (LEFT JOIN não multiplica linhas), e os cards de resumo não devem contar as colunas novas; FR-4 confirmada pelo Acceptance Auditor (nenhum ramo de decisão de status lê dados do SAP)

## Dev Notes

### Por que esta story tem risco arquitetural real — leia antes de codificar

Diferente da Story 3.1 (só tocou a camada de exibição), esta story **também** exige mudar `services/sap_payments_processor.go` (Epic 2, já `done` e revisado). Qualquer mudança em `persistPayments` deve preservar 100% do comportamento já testado nas Stories 2.2/2.3/2.4/2.5 — não altere a lógica de persistência de pagamentos reais, apenas ADICIONE o tratamento do caso `len(item.Payments) == 0`.

### Estado atual de `persistPayments` (ponto de integração exato)

`backend/services/sap_payments_processor.go:433-486`:
```go
func persistPayments(db *sql.DB, companyID, bukrs string, items []dfeResponse) (failedCount int) {
	for _, item := range items {
		if len(item.Payments) == 0 {
			continue  // ← EM_ABERTO/NAO_LOCALIZADO caem aqui, hoje sem nenhum efeito
		}
		fallbackAmbiguous := item.MatchType == "FALLBACK" && item.FallbackNote != ""
		for _, p := range item.Payments {
			// ... INSERT ... ON CONFLICT ... DO UPDATE (pagamentos_fornecedores) ...
		}
	}
	return
}
```
`item.PaymentStatus`, `item.MatchType`, `item.FallbackNote`, `item.DFeKey` (todos em `dfeResponse`, `sap_payments_processor.go:58-74`) já estão disponíveis para QUALQUER item, independente de ter `payments[]` — não é necessário nenhum campo novo na struct.

### Schema atual de `rfb_creditos`/`pagamentos_fornecedores` relevante

- `pagamentos_fornecedores.payment_status`/`match_type`/`fallback_ambiguous` já existem desde a migration 118 (Story 2.3), mas **nunca foram expostos** em `rfb_pagamentos_fornecedores.go` até agora — só são gravados, nunca lidos de volta para a tela.
- `rfb_creditos.chave_dfe` é a mesma chave usada em `pagamentos_fornecedores.chave_doc` e no `DFeKey` retornado pelo SAP (`sap_sync_trigger.go:80`: `SELECT chave_dfe FROM rfb_creditos` alimenta `chaves []string` enviado ao SAP) — `sap_resultados_busca.chave_dfe` deve usar exatamente essa mesma convenção.

### `possivel_duplicidade` (Story 2.5) como precedente para AC #2

O padrão de "exigir um `valor_nota` real antes de comparar" já existe e foi corrigido em revisão na Story 2.5 (NFSE/documento sem nota casada não pode gerar falso positivo) — reaproveitar a MESMA checagem `COALESCE(CASE...) IS NOT NULL` para o novo campo `PagamentoCobreValorNota`, não reinventar.

### Fora de escopo desta story (não implementar)

- Filtro/contador dedicado para `matchType = FALLBACK` ambíguo — Story 3.3
- Qualquer mudança em `status_conciliacao` a partir de dados do SAP — **nunca**, FR-4 é uma invariante do projeto inteiro, não só desta story
- Reprocessamento/kill-switch para corrigir lotes de sincronização com indicador incorreto (mencionado no PRD como Open Question #8) — fora do escopo desta story

### Project Structure Notes

- Migration 121 é a primeira migration nova desde a 120 (Story 2.4) — confirmar que nenhuma outra story em paralelo já usou esse número antes de criar o arquivo
- Mesmo arquivo único (`rfb_pagamentos_fornecedores.go`) para a conciliação; `sap_payments_processor.go` já existe, só ganha um trecho novo dentro de `persistPayments`

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.2] — ACs originais
- [Source: _bmad-output/planning-artifacts/prds/prd-FB_APU02-2026-07-11/prd.md:78-79,126,128,171-172] — definição de paymentStatus/matchType, confirmação de que EM_ABERTO/NAO_LOCALIZADO não geram linha em pagamentos_fornecedores
- [Source: backend/services/sap_payments_processor.go:55-74,433-486] — dfeResponse, persistPayments, ponto de integração exato
- [Source: backend/handlers/rfb_pagamentos_fornecedores.go] — CTE completa pós Story 3.1 (creditos_tipados/conciliacao_creditos/pagamentos_orfaos)
- [Source: backend/handlers/rfb_pagamentos_fornecedores_test.go] — harness de teste a reaproveitar
- [Source: _bmad-output/implementation-artifacts/3-1-creditos-sem-pagamento-localizado-aparecem-na-tela.md] — Dev Notes/Review Findings da story anterior, especialmente os 2 bugs críticos de `status_conciliacao`/`tipo_doc` já corrigidos (não reintroduzir)

## Dev Agent Record

### Agent Model Used

Claude Sonnet 5 (claude-sonnet-5)

### Debug Log References

- `go build ./...`, `go vet ./...` — limpos após cada task
- Migration 121 aplicada com sucesso no banco de teste local (`psql -f migrations/121_sap_resultados_busca.sql`)
- **Fase RED confirmada duas vezes**: (1) 4 dos 5 testes novos de `sap_payments_persist_test.go` falharam antes de implementar a gravação em `sap_resultados_busca`; (2) 4 testes novos de `rfb_pagamentos_fornecedores_test.go` falharam com `column "payment_status" does not exist` antes de estender a CTE
- `go test ./services/... -run TestPersistPayments -v` — 16/16 passando (12 originais + 4 novos)
- `go test ./handlers/... -run TestConciliacao -v` — 18/18 passando (14 originais das Stories 2.5/3.1 + 4 novos)
- `go test ./...` (suíte completa) — sem regressões
- `npx tsc -p tsconfig.app.json --noEmit` — 26 erros pré-existentes (mesma baseline), nenhum no código novo
- **Limitação registrada**: sem ferramenta de navegador neste ambiente — a tela foi verificada por leitura de código (novo componente `PaymentIndicator`/`OrigemBadge`, coluna "Origem" nova, `colSpan` da linha expandida atualizado de 9 para 10), não por captura visual

### Completion Notes List

- `sap_resultados_busca` (migration 121) grava só `EM_ABERTO`/`NAO_LOCALIZADO` por chave (não `PAGO_TOTAL`/`PAGO_PARCIAL`, que já têm linha real em `pagamentos_fornecedores`) — `UNIQUE (company_id, chave_dfe)` sem `bukrs`, porque o objetivo é "qual foi a última tentativa", não histórico por bukrs.
- `persistPayments` (Epic 2, já `done`) foi tocada de forma cirúrgica: só o branch `if len(item.Payments) == 0` ganhou lógica nova (UPSERT em `sap_resultados_busca`), e um `DELETE` foi adicionado após o INSERT/UPDATE de pagamento real bem-sucedido — nenhuma linha da lógica de persistência de pagamentos reais (Stories 2.2/2.3) foi alterada. Os 12 testes originais de `persistPayments` continuam passando sem nenhuma modificação.
- `pagamentos_agg` usa `(ARRAY_AGG(coluna ORDER BY data_pagamento DESC))[1]` para pegar o valor da parcela mais recente (`payment_status`/`match_type`/`origem`), em vez de `MAX()` (que seria uma ordenação alfabética sem sentido semântico) — decisão já antecipada nos Dev Notes da story, implementada exatamente como recomendado.
- `LEFT JOIN sap_resultados_busca` em `conciliacao_creditos` não precisou de nenhum `CASE`/filtro extra para "só valer quando não há pagamento" — como `persistPayments` já apaga a linha correspondente assim que um pagamento real é encontrado, o `LEFT JOIN` simplesmente não traz nada nesse caso, por construção.
- `pagamento_cobre_valor_nota` reaproveita literalmente a mesma checagem `IS NOT NULL` de `possivel_duplicidade` (Story 2.5, corrigida em revisão para não gerar falso positivo em NFSE) — evitando reintroduzir aquele bug de forma diferente.
- Frontend: nova coluna "Origem" na tabela principal (não só no drill-down de parcelas, que já a tinha desde a Story 2.5); `PaymentIndicator` é um texto complementar abaixo do `StatusBadge`, nunca altera a cor/categoria do badge em si — reforça visualmente o princípio FR-4 (SAP nunca decide `status_conciliacao`).
- Escopo mantido dentro da story: nenhuma mudança em `status_conciliacao`/`ProcessSAPSync`/`ON CONFLICT` de pagamentos reais; nenhum filtro dedicado de `FALLBACK` (Story 3.3); nenhum kill-switch de reprocessamento (Open Question do PRD, fora do escopo).

### File List

- `backend/migrations/121_sap_resultados_busca.sql` (novo)
- `backend/services/sap_payments_processor.go` (editado — `persistPayments` grava/atualiza `sap_resultados_busca` para EM_ABERTO/NAO_LOCALIZADO; apaga a linha quando um pagamento real é persistido)
- `backend/services/sap_payments_persist_test.go` (editado — +4 testes: `TestPersistPayments_NaoLocalizado_GravaSapResultadosBusca`, `TestPersistPayments_EmAberto_GravaSapResultadosBusca`, `TestPersistPayments_ReprocessamentoNaoLocalizado_AtualizaUltimaTentativa`, `TestPersistPayments_PagamentoRealAposNaoLocalizado_ApagaSapResultadosBusca`; +helper `querySapResultadoBusca`)
- `backend/handlers/rfb_pagamentos_fornecedores.go` (editado — `pagamentos_agg` expõe `payment_status`/`match_type`/`fallback_ambiguous`/`origem`; `conciliacao_creditos` ganha `pagamento_cobre_valor_nota` e `LEFT JOIN sap_resultados_busca`; `pagamentos_orfaos` ganha colunas equivalentes para o `UNION ALL`; `ConciliacaoItem` e `Scan` estendidos. Revisão: `pagamento_cobre_valor_nota` envolvido em `COALESCE(..., false)`; desempate `pf.id DESC` nos `ARRAY_AGG`)
- `backend/handlers/rfb_pagamentos_fornecedores_test.go` (editado — +5 testes: os 4 de AC + `TestConciliacao_ValorCbsNaoExtintoNull_NaoQuebraScanDeCobreValorNota` (regressão do bug crítico da revisão); +helpers `insertPagamentoSAPComStatus`, `insertSapResultadoBusca`, `queryIndicadoresPagamento`)
- `frontend/src/pages/RFBPagamentosFornecedores.tsx` (editado — `ConciliacaoItem` estendida; componentes `OrigemBadge`/`PaymentIndicator`; nova coluna "Origem"; exportação Excel com 3 colunas novas)

### Change Log

- 2026-07-12: Implementação completa da Story 3.2 (segunda da Epic 3) — nova tabela `sap_resultados_busca` (migration 121) para rastrear a última tentativa de busca no SAP por chave quando não há pagamento (`EM_ABERTO`/`NAO_LOCALIZADO`), populada por uma extensão cirúrgica de `persistPayments` (Epic 2, já `done`). Indicador de pagamento como evidência auxiliar exposto na tela de conciliação: "pagamento localizado" para `PAGO_TOTAL`/`PAGO_PARCIAL`, "aguardando confirmação da RFB" quando o pagamento cobre a nota mas o crédito continua pendente, mensagens distintas para `NAO_LOCALIZADO`/`EM_ABERTO` com data da última tentativa, e origem (CSV/SAP) visível no item agregado (antes só no drill-down de parcelas). Nenhuma mudança em `status_conciliacao` a partir de dados do SAP (FR-4 preservada). 8 testes novos (4 em `services`, 4 em `handlers`), fase RED confirmada duas vezes, sem regressões nos 26 testes já existentes entre os dois pacotes.
- 2026-07-12: Code review em Opus (Blind Hunter + Acceptance Auditor + verificação direta; Edge Case Hunter falhou por limite de sessão da API, mitigado por verificação direta do orquestrador) — 4 patches aplicados: (1) **bug crítico** `pagamento_cobre_valor_nota` resolvia para NULL com `valor_cbs_nao_extinto` NULL + nota casada + pagamento cobrindo, derrubando a listagem com 500 no `Scan` para `bool` — reproduzido empiricamente e corrigido com `COALESCE(..., false)` + teste de regressão `TestConciliacao_ValorCbsNaoExtintoNull_...`; (2) desempate estável `pf.id DESC` nos `ARRAY_AGG`; (3) índice redundante removido da migration 121; (4) correção de contagem de testes na prosa. Acceptance Auditor confirmou as 4 ACs satisfeitas e a invariante FR-4 preservada (nenhum ramo de decisão de status lê dados do SAP; 22/22/22 alinhamento struct/SQL/TS). 5 achados adiados (`deferred-work.md`: coexistência srb+pagamento em estorno SAP; concorrência INSERT/DELETE sem transação; DELETE em loop; `SELECT *` no UNION ALL; CHECK constraint). 3 descartados (variável "não usada" — falso alarme; PAGO_PARCIAL apaga EM_ABERTO — correto por design; sumSQL/countSQL inalterados — correto). Total 9 testes novos após a revisão. `go build`/`go vet`/`go test ./...` e `tsc` limpos.
