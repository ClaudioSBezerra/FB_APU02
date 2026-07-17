---
title: 'Pagamentos a Fornecedores: buscar Nome Fornecedor, Num Doc e Data Emissão do documento de entrada'
type: 'feature'
created: '2026-07-17'
status: 'done'
review_loop_iteration: 1
context: []
baseline_commit: 'd3db84fe580d04b5698317171f985714be104289'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A tabela de "Pagamentos a Fornecedores" hoje exibe "Nome Fornecedor" e "Num Doc" vazios com frequência, porque esses valores vêm apenas do CSV importado pelo usuário (colunas opcionais, muitas vezes não preenchidas). Não existe hoje nenhuma exibição de data de emissão do documento fiscal de entrada que originou o pagamento.

**Approach:** Buscar os 3 dados automaticamente a partir dos documentos de entrada já importados (`nfe_entradas`/`cte_entradas`, casados por `chave_doc`), em vez de depender apenas do CSV: nome do fornecedor via tabela `parceiros` (lookup por CNPJ, mesmo padrão já usado em `creditos_perdidos.go`), número do documento fiscal (`numero_nfe`/`numero_cte`) e data de emissão (`data_emissao`) via `LEFT JOIN` condicional por `tipo_doc`. Quando não houver match no JOIN (documento ainda não importado, tipo `NFSE`, ou pagamento de origem SAP sem NF-e/CT-e correspondente), a exibição cai de volta para os valores já gravados em `pagamentos_fornecedores` (`forn_nome`/`num_doc_pagamento`, vindos do CSV ou do SAP) em vez de mostrar vazio.

## Boundaries & Constraints

**Always:**
- Nome do fornecedor prioriza `parceiros` por `(company_id, forn_cnpj)` — não `nfe_entradas`/`cte_entradas` (a coluna `forn_nome` foi removida de `nfe_entradas` pela migration 090; o comentário da migration 107 que diz "forn_nome já existe" está desatualizado/incorreto; `cte_entradas.emit_nome` existe mas pode estar vazia para documentos importados antes da migration 109 — `parceiros` é a fonte já consolidada e usada no restante do backend). Se `parceiros` não tiver registro para o CNPJ, cai para `pf.forn_nome` (CSV/SAP).
- Número do documento e data de emissão vêm de `LEFT JOIN nfe_entradas ON tipo_doc='NFE'` e `LEFT JOIN cte_entradas ON tipo_doc='CTE'`, ambos escopados por `company_id` e `chave_doc = chave_nfe`/`chave_cte`; usar `COALESCE` entre os dois lados do JOIN para produzir uma única coluna de saída por campo.
- `tipo_doc` aceita 3 valores hoje (`NFE`, `CTE`, `NFSE` — ver validação em `pagamentos_fornecedores.go:134`) e a tabela também recebe linhas de origem SAP (`origem='sap_api'`, gravadas por `backend/services/sap_payments_processor.go`) cujo `num_doc_pagamento` é o documento de compensação SAP, não um documento fiscal casável por `chave_doc`. O JOIN só cobre NFE/CTE — para `NFSE`, para SAP sem match, e para qualquer NFE/CTE cujo documento de entrada ainda não foi importado, "Num Doc" e "Nome Fornecedor" devem cair de volta (`COALESCE`) para os valores já gravados em `pf.num_doc_pagamento`/`pf.forn_nome`, em vez de aparecer vazio. "Data Emissão" não tem fallback equivalente (não existe campo de data de emissão fora do JOIN) — continua vazia/"—" quando não há match, isso é esperado e correto.
- Quando nem o JOIN nem os campos da própria tabela tiverem valor (ex: CSV realmente não preencheu `forn_nome` e não há match), os 3 campos aparecem como "—" (mesmo tratamento visual já existente) — nunca um erro.
- As colunas `forn_nome`/`num_doc_pagamento` continuam existindo em `pagamentos_fornecedores` e continuam sendo gravadas normalmente pelo import de CSV e pelo processor SAP (nenhum dos dois muda) — agora servem como fallback da exibição, não mais como única fonte.
- Nenhuma migration nova é necessária — todas as colunas usadas no JOIN já existem.

**Ask First:** Nenhuma — fonte de dados, estratégia de JOIN e fallback já validados contra o padrão existente no código (`creditos_perdidos.go`) e aprovados com o usuário (incluindo a correção desta iteração após revisão adversarial apontar a lacuna de NFSE/SAP).

**Never:** Não reintroduzir dependência de `nfe_entradas.forn_nome`/`cte_entradas.emit_nome` para nome do fornecedor — ambas incompletas/inconsistentes para dados históricos. Não remover o fallback para `pf.forn_nome`/`pf.num_doc_pagamento` — sem ele, pagamentos NFSE e SAP sem match perdem dados que já existiam.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Pagamento com NF-e de entrada já importada | `tipo_doc='NFE'`, `chave_doc` casa com `nfe_entradas.chave_nfe` da mesma empresa | Tabela exibe nome do fornecedor (via `parceiros`), número da NF-e (`numero_nfe`) e data de emissão (`data_emissao`) | N/A |
| Pagamento com CT-e de entrada já importado | `tipo_doc='CTE'`, `chave_doc` casa com `cte_entradas.chave_cte` da mesma empresa | Tabela exibe nome do emitente (via `parceiros`), número do CT-e (`numero_cte`) e data de emissão | N/A |
| Documento de entrada ainda não importado (NFE/CTE) | `chave_doc` não casa com nenhuma linha de `nfe_entradas`/`cte_entradas` | "Num Doc"/"Nome Fornecedor" caem para os valores já gravados em `pagamentos_fornecedores` (se houver); "Data Emissão" aparece como "—" | N/A |
| Pagamento tipo NFSE | `tipo_doc='NFSE'` (não coberto pelo JOIN) | "Num Doc"/"Nome Fornecedor" vêm de `pagamentos_fornecedores` (CSV); "Data Emissão" aparece como "—" | N/A |
| Pagamento de origem SAP sem documento casado | `origem='sap_api'`, `chave_doc` não casa com NF-e/CT-e importada | "Num Doc" mostra o documento de compensação SAP (`pf.num_doc_pagamento`); "Nome Fornecedor" cai para `parceiros` ou `pf.forn_nome`; "Data Emissão" aparece como "—" | N/A |
| Fornecedor sem nome cadastrado em `parceiros` nem no CSV | CNPJ sem registro em `parceiros` e `pf.forn_nome` vazio | "Nome Fornecedor" aparece como "—" | N/A |

</frozen-after-approval>

## Spec Change Log

- **Achado (revisão adversarial, iteração 1):** a versão original da spec determinava que os campos `forn_nome`/`num_doc_pagamento` do JOIN substituiriam totalmente os valores do CSV/SAP na exibição, sem fallback. Dois revisores independentes (blind hunter + edge case hunter) apontaram que isso causa regressão real para pagamentos `tipo_doc='NFSE'` (tipo válido e documentado, fora do escopo do JOIN que só cobre NFE/CTE) e para pagamentos de origem SAP sem documento de entrada casado — ambos os casos já tinham "Num Doc"/"Nome Fornecedor" preenchidos na própria tabela antes desta mudança, e passariam a exibir vazio.
- **Emenda:** "Always" e a Matrix I/O atualizados para exigir fallback via `COALESCE` — o JOIN tem prioridade quando existe match, mas os valores já gravados em `pf.forn_nome`/`pf.num_doc_pagamento` continuam servindo de fallback quando o JOIN não encontra correspondência (NFSE, SAP sem match, ou NFE/CTE ainda não importada).
- **Estado conhecido evitado:** sem esta emenda, o código re-derivado apagaria dados visíveis hoje para dois cenários reais e documentados no próprio schema/validação do backend (`NFSE` em `validTipos`, origem `sap_api` em `sap_payments_processor.go`).
- **KEEP:** a estratégia de JOIN condicional por `tipo_doc` para NFE/CTE, a subquery em `parceiros` para nome do fornecedor, e o tratamento nullable de `data_emissao_doc` (sem fallback, pois não há data de emissão fora do JOIN) — nada disso mudou, só a prioridade de fallback para os 2 campos que já existiam.

- **Patch (verificação da correção acima):** `parceiros.nome` é `TEXT NOT NULL DEFAULT ''` — se existir um registro de parceiro com nome vazio (sincronizado antes do nome ser preenchido), `COALESCE` original considerava essa string vazia como valor válido e não caía para `pf.forn_nome`, reintroduzindo o mesmo bug em escopo mais estreito. Corrigido com `NULLIF(subquery, '')` antes do `COALESCE`, convertendo nome vazio em NULL para permitir o fallback correto.

## Code Map

- `backend/handlers/pagamentos_fornecedores.go:393-401` (query SQL de `PagamentosFornecedoresListHandler`) -- substituir o `SELECT` simples em `pagamentos_fornecedores` por uma versão com `LEFT JOIN nfe_entradas ne ON ne.company_id = pf.company_id AND ne.chave_nfe = pf.chave_doc AND pf.tipo_doc = 'NFE'` e `LEFT JOIN cte_entradas ce ON ce.company_id = pf.company_id AND ce.chave_cte = pf.chave_doc AND pf.tipo_doc = 'CTE'`. Nome: `COALESCE(NULLIF((SELECT nome FROM parceiros WHERE company_id = pf.company_id AND cnpj = pf.forn_cnpj LIMIT 1), ''), pf.forn_nome, '')` (o `NULLIF` evita que um nome vazio cadastrado em `parceiros` bloqueie o fallback para `pf.forn_nome`). Número do documento: `COALESCE(ne.numero_nfe, ce.numero_cte, pf.num_doc_pagamento, '')`. Data de emissão (sem fallback, nullable): `COALESCE(ne.data_emissao, ce.data_emissao)`. Tabela `pagamentos_fornecedores` precisa de alias (`pf`) para não colidir nomes de coluna com `nfe_entradas`/`cte_entradas` no JOIN.
- `backend/handlers/pagamentos_fornecedores.go:409-423` (`type pagItem struct`) -- renomear/reatribuir `FornNome`/`NumDocPagamento` para virem do JOIN (mesmos nomes de campo JSON, para não quebrar o frontend); adicionar campo `DataEmissaoDoc string \`json:"data_emissao_doc"\`` (nullable — usar `sql.NullTime` ou `sql.NullString` no Scan, convertendo para string vazia se NULL).
- `backend/handlers/pagamentos_fornecedores.go:430-437` (`rows.Scan`) -- ajustar para escanear a nova coluna de data de emissão com tratamento de NULL.
- `frontend/src/pages/ImportarPagamentosFornecedores.tsx:11-24` (`interface Pagamento`) -- adicionar `data_emissao_doc: string | null`.
- `frontend/src/pages/ImportarPagamentosFornecedores.tsx:433-457` (tabela) -- adicionar coluna "Data Emissão" (formatada com a mesma função `formatDate` já usada para `data_pagamento`), posicionada ao lado de "Chave Doc"; manter colunas "Nome Fornecedor" e "Num Doc" como já estão (mesmo binding `pag.forn_nome`/`pag.num_doc_pagamento`, já que o backend passa a preencher esses mesmos campos JSON com os dados corretos vindos do JOIN).

## Tasks & Acceptance

**Execution:**
- [x] `backend/handlers/pagamentos_fornecedores.go` -- reescrever a query de listagem com os LEFT JOINs (nfe_entradas/cte_entradas), subquery em `parceiros`, e `COALESCE` com fallback para `pf.forn_nome`/`pf.num_doc_pagamento`; ajustar `pagItem` e o `Scan` para incluir `data_emissao_doc` com tratamento de NULL -- centraliza a nova fonte de dados com fallback num único ponto, sem alterar o import/CSV nem o processor SAP
- [x] `frontend/src/pages/ImportarPagamentosFornecedores.tsx` -- adicionar `data_emissao_doc` à interface `Pagamento` e nova coluna "Data Emissão" na tabela, ao lado de "Chave Doc" -- exibe o novo dado sem alterar layout das colunas existentes

**Acceptance Criteria:**
- Given um pagamento cujo `chave_doc` corresponde a uma NF-e de entrada já importada, when a lista é carregada, then "Nome Fornecedor" (via `parceiros`), "Num Doc" (`numero_nfe`) e a nova coluna "Data Emissão" aparecem preenchidos.
- Given um pagamento `tipo_doc='NFSE'` ou de origem SAP sem documento de entrada casado, when a lista é carregada, then "Num Doc"/"Nome Fornecedor" exibem os valores já gravados em `pagamentos_fornecedores` (não vazio), e "Data Emissão" aparece como "—".
- Given um pagamento cujo `chave_doc` não corresponde a nenhum documento de entrada importado nem tem valores próprios gravados, when a lista é carregada, then os 3 campos aparecem como "—", sem erro na requisição.

## Design Notes

O JOIN usa `tipo_doc` como discriminador (`'NFE'` vs `'CTE'`) para decidir qual tabela casar — os dois LEFT JOINs coexistem na mesma query mas só um deles produz linha não-nula por registro, daí o `COALESCE` para consolidar em uma coluna de saída. Isso segue o mesmo padrão dual-JOIN já usado implicitamente no projeto para NF-e/CT-e (queries separadas por tipo em outros handlers), mas aqui unificado numa única query paginada para não duplicar a lógica de paginação/filtros já existente no handler.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: sem erros de compilação
- `npx tsc --noEmit -p frontend` -- expected: sem novos erros de tipo
- `npm run build` (em `frontend/`) -- expected: build passa

**Manual checks (if no CLI):**
- Abrir `/dfes` → "Pagamentos Fornecedores" (ou `/importacoes/pagamentos-fornecedores` diretamente) — conferir que a nova coluna "Data Emissão" aparece e que "Nome Fornecedor"/"Num Doc" agora vêm preenchidos para pagamentos cuja NF-e/CT-e de entrada já foi importada.
- Conferir um pagamento cujo documento de entrada NÃO foi importado ainda — os 3 campos devem aparecer como "—" sem quebrar a listagem.
- Conferir um pagamento `tipo_doc='NFSE'` (ou de origem SAP) — "Num Doc"/"Nome Fornecedor" devem exibir os valores já gravados na tabela, não "—".

## Suggested Review Order

**Query com JOIN condicional e fallback**

- Ponto de entrada: LEFT JOINs condicionais por `tipo_doc` (NFE/CTE) + fallback em cascata para os campos que já existiam antes desta mudança.
  [`pagamentos_fornecedores.go:403`](../../backend/handlers/pagamentos_fornecedores.go#L403)

- `NULLIF` evita que um nome vazio em `parceiros` bloqueie o fallback para `pf.forn_nome` — achado na 2ª rodada de revisão adversarial.
  [`pagamentos_fornecedores.go:405`](../../backend/handlers/pagamentos_fornecedores.go#L405)

- `WHERE` da paginação prefixado com `pf.` para não colidir com as colunas de mesmo nome em `nfe_entradas`/`cte_entradas` trazidas pelo JOIN.
  [`pagamentos_fornecedores.go:351`](../../backend/handlers/pagamentos_fornecedores.go#L351)

**Novo campo nullable**

- `data_emissao_doc` sem fallback (só existe via JOIN) — `sql.NullTime` no Scan, `*string` na resposta JSON para permitir `null` explícito.
  [`pagamentos_fornecedores.go:438`](../../backend/handlers/pagamentos_fornecedores.go#L438)

**Exibição**

- Nova coluna "Data Emissão" ao lado de "Chave Doc", mesmo padrão visual de fallback "—" já usado para os demais campos ausentes.
  [`ImportarPagamentosFornecedores.tsx:453`](../../frontend/src/pages/ImportarPagamentosFornecedores.tsx#L453)
