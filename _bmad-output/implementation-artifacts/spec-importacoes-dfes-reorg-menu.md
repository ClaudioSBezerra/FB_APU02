---
title: 'Reorganizar menus "Importações" e "Importação DFe-s": mover Pag. Fornecedores e renomear labels'
type: 'feature'
created: '2026-07-16'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '8b13662c6ed05aac90ac8a3d06537bd922e30af2'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O módulo "Importações" mistura duas categorias que deveriam ficar separadas: uploads de XML de documento fiscal (NF-e/CT-e) — já duplicados pelo módulo "Importação DFe-s", mais completo e com mais tipos — e integrações reais via ERP (Importar via ERP, Logs ERP). "Pag. Fornecedores (CSV)" hoje vive em Importações mas é conceitualmente um "outro documento" a importar, mais próximo do propósito de DFe-s.

**Approach:** Mover "Pag. Fornecedores (CSV)" para dentro de "Importação DFe-s" como um novo item na mesma lista vertical de tipos de documento (`TIPOS` em `DFesModulo.tsx`), renderizando o componente `ImportarPagamentosFornecedores` já existente (upload+template+histórico+desfazer) reaproveitado sem alterar sua lógica interna, sem seletor Entrada/Saída (não aplicável). Renomear "dfes" para "Importação DFe-s e Outros Docs". Em "Importações", remover os 3 itens de XML (NF-e Entradas/Saídas, CT-e Entradas) e o item de Pag. Fornecedores migrado; renomear o módulo para "Importações VIA ERP", já que os itens restantes (NFS-e/CT-e Saídas desabilitados, Importar via ERP, Logs ERP) são todos sobre integração ERP.

## Boundaries & Constraints

**Always:**
- Nenhuma rota é removida de `App.tsx` — `ImportarXMLsEntrada/Saida/CTe.tsx` e a rota `/importacoes/pagamentos-fornecedores` (`ImportarPagamentosFornecedores.tsx`) continuam registradas e acessíveis por link direto, apenas saem do menu (mesmo padrão já usado nos módulos DFe-s/Notas/Malha Fina para páginas órfãs).
- `ImportarPagamentosFornecedores.tsx` não tem sua lógica interna (fetch, upload, template, histórico, desfazer) alterada — é importado e renderizado como está dentro de `DFesModulo.tsx`, mesmo padrão de reaproveitamento já usado para os componentes XML.
- `DIRECOES_DISPONIVEIS` não ganha entrada para o novo tipo `pagamentos-fornecedores` — como o lookup já usa `?? []` como fallback, o seletor Entrada/Saída já fica oculto automaticamente (`direcoesDoTipo.length > 0` já é o gate existente, linha ~151 de `DFesModulo.tsx`) sem precisar de nova lógica condicional.
- Módulos `rfb`, `cgibs`, `painel`, `notas`, `malha`, `argus`, `config` não mudam.

**Ask First:** Nenhuma — as duas decisões de UX (onde entra Pag. Fornecedores na lista, o que fazer com rotas órfãs) já foram resolvidas com o usuário antes desta spec.

**Never:** Não remover ou modificar a rota `/importacoes/pagamentos-fornecedores` em `App.tsx` — mantém acessível por link direto/histórico, mesmo com o conteúdo agora também acessível via `/dfes`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Selecionar "Pagamentos Fornecedores" em /dfes | Usuário clica no novo item da lista vertical | Área de conteúdo troca para `ImportarPagamentosFornecedores` completo (upload CSV, template, histórico, desfazer), sem seletor Entrada/Saída | N/A |
| Alternar entre NF-e e Pagamentos Fornecedores | Usuário clica em "NF-e" depois em "Pagamentos Fornecedores" e volta | Cada tipo mostra seu próprio conteúdo sem vazamento de estado entre eles (upload pendente, resultado anterior) | N/A |
| Acesso direto à rota antiga | Usuário acessa `/importacoes/pagamentos-fornecedores` diretamente (link salvo/histórico) | Página carrega normalmente, componente idêntico ao usado dentro de /dfes | N/A |
| Menu Importações após remoção | Usuário abre módulo "Importações VIA ERP" | Restam apenas: NFS-e Entradas (disabled), NFS-e Saídas (disabled), CT-e Saídas (disabled), Importar via ERP (admin), Logs ERP (admin) | N/A |

</frozen-after-approval>

## Code Map

- `frontend/src/lib/navigation.ts` -- renomear label `dfes` para "Importação DFe-s e Outros Docs"; renomear label `importacoes` para "Importações VIA ERP"; remover do array `tabs` de `importacoes` os itens NF-e Entradas/Saídas, CT-e Entradas e Pag. Fornecedores.
- `frontend/src/pages/DFesModulo.tsx` -- adicionar `{ key: 'pagamentos-fornecedores', label: 'Pagamentos Fornecedores', enabled: true }` ao array `TIPOS` (primeiras linhas, junto com nfe/cte); importar `ImportarPagamentosFornecedores`; no bloco de renderização condicional do conteúdo (linha ~172, hoje `configAtual ? <Card upload XML> : <Card indisponível>`), adicionar um terceiro ramo: se `tipoAtivo === 'pagamentos-fornecedores'`, renderizar `<ImportarPagamentosFornecedores />` (com `key="pagamentos-fornecedores"` para remount limpo, mesmo padrão já usado em `MalhaFinaModulo.tsx`).
- `frontend/src/pages/ImportarPagamentosFornecedores.tsx` -- nenhuma mudança de lógica; apenas reaproveitado como import.
- `frontend/src/App.tsx` -- nenhuma mudança (rotas de `/importacoes/*` e `/dfes` já existem e continuam como estão).

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/lib/navigation.ts` -- renomear labels de `dfes` e `importacoes`, remover 4 itens de `importacoes.tabs` -- ponto único de configuração de menu
- [x] `frontend/src/pages/DFesModulo.tsx` -- adicionar tipo `pagamentos-fornecedores` a `TIPOS`, importar e renderizar `ImportarPagamentosFornecedores` condicionalmente com `key` para remount limpo -- reaproveita componente existente sem duplicar lógica

**Acceptance Criteria:**
- Given o usuário abre `/dfes`, when clica em "Pagamentos Fornecedores" na lista vertical, then vê a tela completa de upload CSV (idêntica à antiga `/importacoes/pagamentos-fornecedores`), sem seletor Entrada/Saída.
- Given o usuário abre o módulo "Importações VIA ERP", when olha a lista de abas, then não vê mais NF-e Entradas/Saídas, CT-e Entradas nem Pag. Fornecedores — só os itens restantes de integração ERP.
- Given o usuário acessa `/importacoes/pagamentos-fornecedores` diretamente, when a página carrega, then funciona normalmente (rota não removida).

## Verification

**Commands:**
- `npx tsc --noEmit -p frontend` -- expected: sem novos erros de tipo
- `npm run build` (em `frontend/`) -- expected: build passa

**Manual checks (if no CLI):**
- Abrir `/dfes`, alternar entre NF-e, CT-e e Pagamentos Fornecedores — confirmar troca de conteúdo sem vazamento de estado (upload pendente/resultado anterior não deve aparecer ao trocar de tipo).
- Abrir `/importacoes/pagamentos-fornecedores` diretamente — confirmar que a página ainda funciona.
- Abrir módulo "Importações VIA ERP" no rail — confirmar novo label e lista de abas reduzida.

## Suggested Review Order

**Configuração de menu**

- Ponto único de configuração: labels renomeados e 4 itens removidos de `importacoes.tabs`.
  [`navigation.ts:27`](../../frontend/src/lib/navigation.ts#L27)

- Label de `dfes` atualizado para refletir o novo escopo ("e Outros Docs").
  [`navigation.ts:68`](../../frontend/src/lib/navigation.ts#L68)

**Integração do novo tipo de documento**

- `pagamentos-fornecedores` adicionado a `TIPOS`, sem entrada em `DIRECOES_DISPONIVEIS` — omissão intencional que já esconde o seletor Entrada/Saída via gate existente.
  [`DFesModulo.tsx:24`](../../frontend/src/pages/DFesModulo.tsx#L24)

- Ramo condicional renderiza `ImportarPagamentosFornecedores` reaproveitado (sem alterar sua lógica interna), suprimindo o heading duplicado da página.
  [`DFesModulo.tsx:145`](../../frontend/src/pages/DFesModulo.tsx#L145)

**Achados da revisão adversarial — aceitos como trade-off, não corrigidos**

- Módulo "Importações VIA ERP" fica sem abas habilitadas para não-admin; rota órfã de pagamentos-fornecedores destaca módulo sem aba ativa — ambos consequência de decisões já aprovadas, registrados em `deferred-work.md` para revisão futura.
