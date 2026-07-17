---
title: 'Split Payment 2027: cadastro de adesão do fornecedor (parceiros)'
type: 'feature'
created: '2026-07-17'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '559ea1f7d3ede98a545f22d48002e90ef5d9321f'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A Reforma Tributária (LC 214/2025) introduz o Split Payment para operações B2B a partir de 2027, inicialmente facultativo (Fase 1). O direito ao crédito de IBS/CBS do comprador passa a depender da extinção efetiva do débito do fornecedor — se o fornecedor não aderiu/não recolheu via split, o crédito fica em risco. O sistema não tem hoje nenhum lugar para registrar se um fornecedor aderiu voluntariamente ao Split Payment: a tabela `parceiros` (cadastro de fornecedor/cliente por CNPJ) é populada só automaticamente via sync do ERP Bridge, sem nenhuma tela de edição manual.

**Approach:** Adicionar `aderiu_split_payment BOOLEAN` + `data_adesao_split_payment DATE` em `parceiros`. Criar os primeiros endpoints de listagem/edição manual dessa tabela (hoje só tem endpoint de sync automático) e uma tela nova "Cadastro de Fornecedores" dentro do módulo "Importações VIA ERP", para consultar fornecedores e marcar a adesão. Esta é a base para 2 entregas seguintes já mapeadas (flag equivalente na Empresa, indicador na tela de Pagamentos a Fornecedores) — fora do escopo desta spec, ver `deferred-work.md`.

## Boundaries & Constraints

**Always:**
- `parceiros` continua sendo populada automaticamente via `ERPBridgeParceirosSyncHandler` (sync não muda) — a nova tela/endpoint de edição só ajusta a flag/data de adesão de uma linha já existente, nunca cria ou exclui parceiro.
- Novo endpoint de listagem é paginado e escopado por `company_id`, como todo o resto do sistema — `parceiros` pode ter volume bem maior que outras tabelas de cadastro simples do projeto (ex: `forn_simples`).
- Edição da flag é um PATCH idempotente por chave primária composta (`company_id`+`cnpj`) — não recria a linha.
- Nova tela "Cadastro de Fornecedores" entra como item de aba dentro do módulo `importacoes` (`Importações VIA ERP`), ao lado de "Importar via ERP"/"Logs ERP" — sem `adminOnly`, mesmo padrão desses 2 itens.
- Nenhuma lógica de cálculo/bloqueio de crédito de IBS/CBS é implementada nesta entrega — só cadastro.

**Ask First:** Nenhuma — escopo, localização da tela e divisão em 3 entregas já resolvidos com o usuário via AskUserQuestion antes desta spec.

**Never:** Não implementar aqui a flag da Empresa nem o indicador em Pagamentos a Fornecedores — ficam registrados em `deferred-work.md` como próximas entregas, dependentes desta.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Marcar fornecedor como aderente | Usuário ativa o toggle "Aderiu Split Payment" para um CNPJ na lista | Grava `aderiu_split_payment=true` + `data_adesao_split_payment=hoje` (se não informada) em `parceiros`, por `company_id`+`cnpj` | N/A |
| Desmarcar adesão | Usuário desativa o toggle | Grava `aderiu_split_payment=false`; `data_adesao_split_payment` mantém o valor histórico (não é limpo) | N/A |
| Buscar fornecedor | Usuário digita CNPJ ou nome no campo de busca | Lista filtra por `cnpj ILIKE` ou `nome ILIKE`, paginada, escopada por `company_id` | N/A |
| Volume alto de parceiros | Empresa com centenas/milhares de linhas em `parceiros` | Listagem pagina (ex: 50/página) em vez de carregar tudo de uma vez | N/A |

</frozen-after-approval>

## Code Map

- `backend/migrations/123_split_payment_adesao.sql` (nova; numeração ajustada na implementação -- 119 já existia como `sap_sync_runs_contagens.sql`) -- `ALTER TABLE parceiros ADD COLUMN aderiu_split_payment BOOLEAN NOT NULL DEFAULT false, ADD COLUMN data_adesao_split_payment DATE`.
- `backend/handlers/erp_bridge_parceiros.go` -- adicionar `ParceirosListHandler` (GET, paginado, filtro `cnpj`/`nome`, escopado por `company_id`, mesmo padrão de paginação de `PagamentosFornecedoresListHandler`) e `ParceirosUpdateSplitPaymentHandler` (PATCH por `company_id`+`cnpj`, body `{aderiu_split_payment bool, data_adesao_split_payment *string}`).
- `backend/main.go` -- registrar `GET /api/parceiros` e `PATCH /api/parceiros/split-payment` (nomes de rota sujeitos a ajuste na implementação, mantendo o padrão `/api/{recurso}` já usado no projeto).
- `frontend/src/pages/CadastroFornecedores.tsx` (nova) -- lista paginada de `parceiros` com busca por CNPJ/nome e `Switch` (shadcn, já usado no design system) para `aderiu_split_payment` + input de data, seguindo o padrão visual de `TabelaFornSimples.tsx` adaptado para paginação.
- `frontend/src/lib/navigation.ts` -- adicionar `{ label: 'Cadastro de Fornecedores', path: '/importacoes/fornecedores' }` em `importacoes.tabs`.
- `frontend/src/App.tsx` -- registrar rota `/importacoes/fornecedores` → `CadastroFornecedores`.

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/123_split_payment_adesao.sql` -- adicionar as 2 colunas em `parceiros` -- base de dados para a flag
- [x] `backend/handlers/erp_bridge_parceiros.go` -- `ParceirosListHandler` + `ParceirosUpdateSplitPaymentHandler` -- primeira interface de edição manual de `parceiros`
- [x] `backend/main.go` -- registrar as 2 novas rotas -- expõe os handlers acima
- [x] `frontend/src/pages/CadastroFornecedores.tsx` -- nova tela de listagem/edição paginada -- interface de gestão do cadastro
- [x] `frontend/src/lib/navigation.ts` + `frontend/src/App.tsx` -- novo item de menu e rota -- integra a tela nova à navegação

**Acceptance Criteria:**
- Given um fornecedor sem adesão registrada, when o usuário abre "Cadastro de Fornecedores", busca o CNPJ e ativa o toggle de Split Payment, then a mudança persiste e é refletida ao recarregar a listagem.
- Given uma empresa com muitos parceiros cadastrados, when a tela "Cadastro de Fornecedores" é aberta, then a lista é paginada (não carrega todos de uma vez) e a busca por CNPJ/nome funciona.
- Given o usuário desativa uma adesão previamente marcada, when salva, then `data_adesao_split_payment` permanece com o valor histórico, não é apagada.

## Design Notes

`parceiros` nunca teve endpoint de listagem/edição manual — só o sync automático do ERP Bridge (`ERPBridgeParceirosSyncHandler`). O padrão de paginação/filtro a seguir é o mesmo já usado em `PagamentosFornecedoresListHandler` (query dinâmica com `WHERE` construído por filtros opcionais, `LIMIT`/`OFFSET`), não o padrão simples sem paginação de `forn_simples.go` — `parceiros` é preenchida automaticamente e pode crescer bem mais que uma lista mantida manualmente.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: sem erros de compilação
- `npx tsc --noEmit -p frontend` -- expected: sem novos erros de tipo
- `npm run build` (em `frontend/`) -- expected: build passa

**Manual checks (if no CLI):**
- Abrir "Cadastro de Fornecedores" (dentro de Importações VIA ERP), buscar um CNPJ, ativar o toggle de Split Payment, salvar, recarregar a página e confirmar que o estado persiste.
- Testar a paginação/busca com uma empresa que tenha múltiplos parceiros cadastrados.
- Buscar/atualizar usando um CNPJ formatado (com pontos/traço) e confirmar que ainda casa com o registro (achado da revisão adversarial, corrigido nesta rodada).

## Suggested Review Order

**Endpoints novos**

- Ponto de entrada: listagem paginada de `parceiros`, reaproveitando o padrão de `PagamentosFornecedoresListHandler`.
  [`erp_bridge_parceiros.go:118`](../../backend/handlers/erp_bridge_parceiros.go#L118)

- Normalização de CNPJ (dígitos puros) no filtro de busca e no PATCH — achado da revisão adversarial, corrigido nesta rodada para casar com o formato gravado pelo sync.
  [`erp_bridge_parceiros.go:136`](../../backend/handlers/erp_bridge_parceiros.go#L136)

- `PATCH` com regra de não limpar a data histórica ao desmarcar adesão — `COALESCE` só grava data quando há valor definido.
  [`erp_bridge_parceiros.go:232`](../../backend/handlers/erp_bridge_parceiros.go#L232)

**Integração no menu**

- Novo item de aba dentro de "Importações VIA ERP", sem `adminOnly`.
  [`navigation.ts:27`](../../frontend/src/lib/navigation.ts#L27)

**Tela nova**

- Lista paginada + toggle inline por linha, mesmo padrão visual de `ImportarPagamentosFornecedores.tsx`.
  [`CadastroFornecedores.tsx:22`](../../frontend/src/pages/CadastroFornecedores.tsx#L22)
