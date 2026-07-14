---
title: 'Módulo DF-e''s: novo item de menu com submenu vertical por tipo de documento'
type: 'feature'
created: '2026-07-14'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '7a6a6f54dc587c665305ddc034fe154be2605b1e'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O módulo "Importações" hoje lista NF-e Entrada/Saída e CT-e Entrada como 3 abas horizontais separadas — não escala para os 12 tipos de documento fiscal que o negócio processa (NF-e, NFC-e, NFS-e, CT-e, NC-e, ND-e, BP-e, NF3e, NFCom, Água, Gás, Eventos, Outros). O gestor gostou do padrão do Portal Fiscal CBS/IBS (`argus-portal-fiscal.html`): um submenu vertical "DF-e's" que lista tipos de documento, cada um abrindo uma tela com seletor Entrada/Saída.

**Approach:** Criar um NOVO item de módulo "DF-e's" no `AppRail`, logo abaixo de "Importações" (que permanece 100% intocado). Layout interno: submenu vertical (à esquerda do conteúdo, dentro do próprio módulo) listando os 12 tipos de documento; ao selecionar um tipo, mostra um seletor Entrada/Saída e a tela de upload correspondente. Começar com os 3 combos que já têm backend (NF-e Entrada, NF-e Saída, CT-e Entrada); os demais 9 tipos aparecem na lista mas desabilitados ("Em breve").

## Boundaries & Constraints

**Always:**
- Módulo "Importações" (rotas `/importacoes/*`, componentes `ImportarXMLsEntrada/Saida/CTe`, `ImportarPagamentosFornecedores`) permanece inalterado — nenhuma rota, componente ou label existente é removido ou migrado.
- Reaproveitar a lógica de upload já validada (upload por pasta `webkitdirectory`, filtro `.xml`, toasts, badges de resultado, lista de erros por arquivo) para os 3 combos com backend, sem alterar os endpoints (`/api/nfe-entradas/upload`, `/api/nfe-saidas/upload`, `/api/cte-entradas/upload`).
- Seguir o padrão já existente de `disabled: true` em `navigation.ts`/`ModuleTabs` para os 9 tipos sem backend — não inventar um mecanismo novo de "em breve".
- Novo módulo registrado em `frontend/src/lib/navigation.ts` (`modules.dfes`) e `frontend/src/components/AppRail.tsx` (`mainItems`), seguindo exatamente a mesma convenção de tipos/objetos já usada pelos módulos existentes.

**Ask First:** Se o submenu vertical exigir um componente de layout que não existe ainda (o projeto só tem `ModuleTabs` horizontal) e a criação desse componente parecer maior que o esperado, HALT e perguntar antes de expandir escopo.

**Never:** Não migrar/alterar Malha Fina, RFB, CGIBS, Notas Importadas ou qualquer outro módulo para o padrão vertical — isso é trabalho futuro já registrado em `deferred-work.md`. Não implementar upload real para os 9 tipos sem backend (NFC-e, NFS-e, NC-e, ND-e, BP-e, NF3e, NFCom, Água, Gás, Eventos, Outros) — só aparecem desabilitados.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Usuário clica "DF-e's" no rail | Navegação | Abre módulo com submenu vertical, NF-e pré-selecionado, Entrada como direção padrão | N/A |
| Seleciona NF-e no submenu, depois Entrada | Pasta com .xml válidos, botão Importar | POST `/api/nfe-entradas/upload`, badges de resultado idênticos ao comportamento atual | Erros por arquivo listados como hoje |
| Seleciona CT-e no submenu | N/A | Direção mostra só "Entrada" habilitada; "Saída" desabilitada (sem endpoint) | N/A |
| Clica em um tipo desabilitado (ex: NFC-e) | N/A | Item não navega / mostra estado "Em breve", sem erro | N/A |
| Troca de tipo de documento com resultado de upload na tela | Resultado anterior visível | Limpa `result`/`xmlFiles` ao trocar tipo ou direção | N/A |

</frozen-after-approval>

## Code Map

- `frontend/src/lib/navigation.ts` -- adicionar `modules.dfes` (novo módulo, tabs internas não usadas do mesmo jeito — ver Design Notes) e ajustar `getActiveModule` para reconhecer `/dfes/*`
- `frontend/src/components/AppRail.tsx:31-38` -- adicionar entrada `{ id: 'dfes', icon: ..., label: "DF-e's", path: '/dfes/nfe' }` em `mainItems`, logo após `importacoes`
- `frontend/src/App.tsx:27-29,138-177,231-236` -- novo import do componente do módulo DF-e's, nova rota `/dfes/*`, `ModuleTabs`/`AppHeader` continuam funcionando via `getActiveModule`
- `frontend/src/pages/DFesModulo.tsx` -- nova página (a criar): submenu vertical de tipos + seletor Entrada/Saída + corpo de upload reaproveitado das 3 telas de XML atuais
- `frontend/src/pages/ImportarXMLsEntrada.tsx`, `ImportarXMLsSaida.tsx`, `ImportarXMLsCTe.tsx` -- não alterados; servem de referência para extrair a lógica de upload (não remover, não eliminar duplicação nesta spec)
- `frontend/public/argus-portal-fiscal.html:198-252` -- referência visual do submenu vertical (`nav-parent`/`submenu`), não é código a ser reaproveitado literalmente (é HTML estático de demo)

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/lib/navigation.ts` -- adicionar entrada `dfes` em `modules` e um branch em `getActiveModule` para `pathname.startsWith('/dfes/')` -- registra o novo módulo no sistema de rotas/labels existente
- [x] `frontend/src/components/AppRail.tsx` -- adicionar item `dfes` em `mainItems` logo após `importacoes`, com ícone distinto (ex: `Files` do lucide-react) -- expõe o novo módulo no rail sem alterar os existentes
- [x] `frontend/src/pages/DFesModulo.tsx` -- criar componente com: (1) lista vertical dos 12 tipos de documento (NF-e, NFC-e, NFS-e, CT-e, NC-e, ND-e, BP-e, NF3e, NFCom, Água, Gás, Eventos, Outros) — 9 marcados desabilitados; (2) seletor Entrada/Saída para o tipo ativo, desabilitando combinações sem backend (CT-e Saída); (3) corpo de upload (input de pasta, botão importar, badges de resultado) parametrizado por endpoint conforme tabela `{tipo, direção} -> config` -- implementa o padrão pedido mantendo os 3 fluxos de upload já validados
- [x] `frontend/src/App.tsx` -- importar `DFesModulo`, adicionar rota `/dfes/:tipo?` (ou rota fixa `/dfes` com estado interno de seleção) dentro de `AppLayout` -- conecta o novo módulo ao roteamento existente

**Acceptance Criteria:**
- Given o usuário está autenticado, when clica em "DF-e's" no rail, then vê o submenu vertical com 12 tipos, NF-e ativo por padrão, e consegue importar um XML de entrada com sucesso via `/api/nfe-entradas/upload`
- Given o módulo "Importações" já existente, when o usuário acessa qualquer uma de suas rotas atuais, then nada muda em relação ao comportamento anterior a esta spec
- Given um tipo de documento sem backend (ex: NFS-e), when o usuário olha o submenu, then o item aparece visualmente desabilitado e não navega para nenhuma tela de upload

## Spec Change Log

## Design Notes

O submenu vertical é uma novidade estrutural — hoje `ModuleTabs` só renderiza abas horizontais lidas de `moduleCfg.tabs`. Duas opções de implementação, decidir a mais simples ao codificar:
1. Módulo `dfes` em `navigation.ts` com `tabs: []` (esconde `ModuleTabs` automaticamente, já que `ModuleTabs` retorna `null` quando `tabs.length === 0`), e o próprio `DFesModulo.tsx` desenha seu submenu vertical internamente — mais simples, não mexe em `App.tsx`/`ModuleTabs`.
2. Componente de layout novo reutilizável para submenu vertical — mais correto a longo prazo (serve de base para a migração futura de outros módulos já registrada em `deferred-work.md`), mas maior escopo.
Recomenda-se a opção 1 para esta spec, deixando a extração de um componente reutilizável para quando a migração dos demais módulos for planejada.

## Verification

**Commands:**
- `cd frontend && npx tsc --noEmit` -- expected: sem erros de tipo
- `cd frontend && npm run build` -- expected: build passa

**Manual checks (if no CLI):**
- Abrir `/dfes`, confirmar submenu vertical com 12 tipos, NF-e ativo, upload real de um XML de entrada funcionando
- Confirmar que `/importacoes/nfe-entrada`, `/importacoes/nfe-saida`, `/importacoes/cte-entrada` continuam idênticos a antes desta mudança

## Suggested Review Order

**Componente novo — módulo DF-e's**

- Entrypoint: componente que substitui as 3 telas antigas de upload por uma única tela com submenu + seletor de direção.
  [`DFesModulo.tsx:69`](../../frontend/src/pages/DFesModulo.tsx#L69)

- Lista dos 12 tipos de documento; 9 desabilitados até terem backend próprio.
  [`DFesModulo.tsx:20`](../../frontend/src/pages/DFesModulo.tsx#L20)

- Tabela de config por combinação tipo+direção, aponta para os 3 endpoints já existentes sem alterá-los.
  [`DFesModulo.tsx:43`](../../frontend/src/pages/DFesModulo.tsx#L43)

- Guard contra troca de tipo/direção durante upload em andamento — corrigido após achado de review adversarial (condição de corrida).
  [`DFesModulo.tsx:80`](../../frontend/src/pages/DFesModulo.tsx#L80)

- Submenu vertical renderizado inline no componente (não um layout genérico reutilizável) — decisão da spec para não expandir escopo.
  [`DFesModulo.tsx:127`](../../frontend/src/pages/DFesModulo.tsx#L127)

**Integração com navegação existente**

- Novo módulo registrado com `tabs: []`, o que faz `ModuleTabs` (App.tsx) ocultar a barra horizontal automaticamente.
  [`navigation.ts:97`](../../frontend/src/lib/navigation.ts#L97)

- `getActiveModule` reconhece `/dfes` para destacar o ícone certo no rail.
  [`navigation.ts:135`](../../frontend/src/lib/navigation.ts#L135)

- Novo ícone no rail, posicionado logo abaixo de "Importações" (que permanece intocado).
  [`AppRail.tsx:36`](../../frontend/src/components/AppRail.tsx#L36)

- Rota `/dfes` registrada dentro de `AppLayout`, ao lado das rotas antigas de `/importacoes/*` sem removê-las.
  [`App.tsx:240`](../../frontend/src/App.tsx#L240)

**Decisões de processo (fora do código)**

- Restrição "sem novas features" removida do CLAUDE.md após achado de review adversarial — decisão explícita do usuário para permitir este trabalho.
  [`CLAUDE.md:14`](../../CLAUDE.md#L14)
