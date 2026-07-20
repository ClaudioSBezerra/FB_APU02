---
title: 'Split Payment 2027: cadastro de adesão da Empresa'
type: 'feature'
created: '2026-07-17'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '7c8ce0bec60e4e0b0c1a1862daaddea19115c413'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A entrega anterior (spec-split-payment-cadastro-fornecedor, commit `7c8ce0b`) registra se um FORNECEDOR aderiu ao Split Payment. Falta o lado complementar: se a própria EMPRESA (compradora) aderiu voluntariamente. Hoje `companies` não tem esse campo, e a tela de gestão de empresas (`GestaoAmbiente.tsx`) só permite criar/excluir — não editar nada.

**Approach:** Adicionar `aderiu_split_payment BOOLEAN` + `data_adesao_split_payment DATE` em `companies`. Criar o primeiro `UpdateCompanyHandler` (PUT), registrado no switch de método já existente para `/api/config/companies` — sem rota nova. Adicionar um toggle na lista de empresas já renderizada em `GestaoAmbiente.tsx`, reaproveitando a mesma regra de negócio (não limpar data ao desmarcar) já implementada em `ParceirosUpdateSplitPaymentHandler`.

## Boundaries & Constraints

**Always:**
- `UpdateCompanyHandler` só atualiza `name`, `trade_name`, `aderiu_split_payment`, `data_adesao_split_payment` de uma empresa já existente (por `id`) — não altera `group_id` nem recria a linha.
- Registrado como `case http.MethodPut:` no switch já existente da rota `/api/config/companies` (`backend/main.go`) — não cria endpoint/rota nova.
- Mesma regra de data da entrega anterior: ao marcar adesão (`true`) sem data informada, grava data de hoje; ao desmarcar (`false`), `data_adesao_split_payment` mantém o valor histórico (não é limpo) — mesmo padrão de `COALESCE` condicional de `ParceirosUpdateSplitPaymentHandler`.
- Toggle entra na lista de empresas já existente em `GestaoAmbiente.tsx` (linha ~601), ao lado do botão de excluir — não cria tela nova.
- Nenhuma lógica de cálculo/bloqueio de crédito de IBS/CBS é implementada nesta entrega — só cadastro.

**Ask First:** Nenhuma — escopo e regra de negócio já resolvidos (mesma decisão da entrega anterior, aplicada por analogia).

**Never:** Não criar rota `/api/config/companies/:id` nem qualquer endpoint dedicado novo — reaproveitar o switch existente. Não implementar aqui a 3ª entrega (badge em Pagamentos a Fornecedores) — continua em `deferred-work.md`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Marcar empresa como aderente | Usuário ativa o toggle para uma empresa na lista | PUT grava `aderiu_split_payment=true` + `data_adesao_split_payment=hoje` (se não informada) | N/A |
| Desmarcar adesão | Usuário desativa o toggle | PUT grava `aderiu_split_payment=false`; `data_adesao_split_payment` mantém o valor histórico | N/A |
| Empresa sem adesão | Empresa nunca teve o campo alterado | Toggle aparece desligado, sem erro (default `false`) | N/A |
| PUT para empresa inexistente | `id` não existe na tabela | Retorna 404 | N/A |

</frozen-after-approval>

## Code Map

- `backend/migrations/124_split_payment_adesao_empresa.sql` (nova) -- `ALTER TABLE companies ADD COLUMN aderiu_split_payment BOOLEAN NOT NULL DEFAULT false, ADD COLUMN data_adesao_split_payment DATE`.
- `backend/handlers/environment.go` -- adicionar os 2 campos à struct `Company` (linha ~28); adicionar `UpdateCompanyHandler` (PUT, body `{id, name, trade_name, aderiu_split_payment, data_adesao_split_payment}`, mesma regra de `COALESCE` condicional para a data já usada em `ParceirosUpdateSplitPaymentHandler`); `GetCompaniesHandler` (linha ~233) ganha os 2 campos no SELECT/Scan.
- `backend/main.go` -- adicionar `case http.MethodPut:` ao switch já existente da rota `/api/config/companies` (em torno da linha ~393-400), chamando `handlers.UpdateCompanyHandler(db)(w, r)`.
- `frontend/src/pages/GestaoAmbiente.tsx` -- adicionar os 2 campos à interface `Company` (linha ~48); adicionar `handleToggleSplitPayment` (mesmo padrão de `CadastroFornecedores.tsx`: PUT otimista, atualiza estado local, toast de erro/sucesso); renderizar `Switch` + data formatada na lista de empresas (linha ~601-621), ao lado do botão de excluir.

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/124_split_payment_adesao_empresa.sql` -- adicionar as 2 colunas em `companies` -- base de dados para a flag
- [x] `backend/handlers/environment.go` -- `UpdateCompanyHandler` + campos em `Company`/`GetCompaniesHandler` -- primeira edição de empresa além de criar/excluir
- [x] `backend/main.go` -- adicionar PUT ao switch existente de `/api/config/companies` -- expõe o handler acima sem rota nova
- [x] `frontend/src/pages/GestaoAmbiente.tsx` -- toggle + data na lista de empresas -- completa o cadastro do lado da empresa

**Acceptance Criteria:**
- Given uma empresa sem adesão registrada, when o usuário ativa o toggle de Split Payment na lista, then a mudança persiste (PUT) e é refletida ao recarregar a tela.
- Given uma empresa com adesão marcada, when o usuário desativa o toggle, then `data_adesao_split_payment` permanece com o valor histórico, não é apagada.
- Given um PUT para um `id` de empresa inexistente, when a requisição é enviada, then retorna 404, não 500.

## Design Notes

Reaproveita deliberadamente o endpoint `/api/config/companies` já registrado (GET/POST/DELETE) em vez de criar um novo — o switch em `main.go` já despacha por `r.Method`, então adicionar `case http.MethodPut` é a mudança de menor blast radius. A regra de "não limpar data ao desmarcar" é copiada por analogia direta de `ParceirosUpdateSplitPaymentHandler` (mesma lógica: `dataAdesao *string` computado em Go antes do UPDATE, `COALESCE($N::date, data_adesao_split_payment)` na query).

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: sem erros de compilação
- `npx tsc --noEmit -p frontend` -- expected: sem novos erros de tipo
- `npm run build` (em `frontend/`) -- expected: build passa

**Manual checks (if no CLI):**
- Abrir Gestão de Ambiente (`/config/ambiente`), selecionar uma empresa, ativar o toggle de Split Payment, confirmar persistência ao recarregar.
- Desativar o toggle e confirmar (via API ou re-marcando) que a data histórica não foi apagada.

## Suggested Review Order

**Endpoint novo**

- `UpdateCompanyHandler`, mesma regra de "não limpar data" de `ParceirosUpdateSplitPaymentHandler` — validação de string vazia e `updated_at` corrigidos nesta rodada (achados da revisão adversarial).
  [`environment.go:324`](../../backend/handlers/environment.go#L324)

- PUT adicionado ao switch já existente de `/api/config/companies`, sem rota nova.
  [`main.go:400`](../../backend/main.go#L400)

**Frontend**

- Toggle + data na lista de empresas já existente.
  [`GestaoAmbiente.tsx:310`](../../frontend/src/pages/GestaoAmbiente.tsx#L310)

**Achado sério da revisão — não corrigido nesta spec, registrado com destaque**

- Nenhuma das 3 mutações em `companies` (Create/Update/Delete) verifica relação real do usuário com o grupo/ambiente da empresa — escrita sem escopo de tenant, agora incluindo um campo de compliance. Pré-existente, não introduzido aqui, mas priorizado em `deferred-work.md` por colidir com o valor central de isolamento entre tenants do projeto.
