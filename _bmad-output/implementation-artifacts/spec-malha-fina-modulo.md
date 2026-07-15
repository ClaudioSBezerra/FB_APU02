---
title: 'Módulo Malha Fina: submenu vertical por tipo de documento'
type: 'feature'
created: '2026-07-15'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '6941276'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O módulo "Malha Fina" hoje usa abas horizontais (NF-e Entradas, NF-e Saídas, CT-e Entradas + 10 tipos desabilitados) — mesma limitação de escala já resolvida em "Importação DFe-s" e "Notas Importadas": não comporta bem os 12 tipos de documento fiscal do negócio.

**Approach:** Replicar o shell de navegação já validado (submenu vertical por tipo de documento, tipos habilitados nas primeiras linhas, sem badge "Em breve") em um novo componente `MalhaFinaModulo.tsx`, renderizando o já existente `MalhaFinaPanel` (parametrizado por prop `tipo`) para cada combinação `{tipo, direção}` disponível, sem alterar sua lógica interna (filtros, tabelas, paginação, toggle RFB).

## Boundaries & Constraints

**Always:**
- "Malha Fina" continua sendo um item de menu independente no `AppRail`, mesmo ícone/rota base.
- `MalhaFinaPanel.tsx` permanece com a lógica interna 100% intacta — mesmas queries, colunas, filtros, paginação, prop `rfbDisponivel`. A única mudança é onde/como é invocado (prop `tipo` e navegação em volta).
- Seguir o mesmo shell visual de `DFesModulo.tsx`/`NotasImportadasModulo.tsx`: lista vertical à esquerda com os 12 tipos de documento (3 habilitados: NF-e Entrada, NF-e Saída, CT-e Entrada; 9 desabilitados), sem badge "Em breve", tipos habilitados nas primeiras linhas do array `TIPOS`, seletor Entrada/Saída à direita.
- Rota antiga do item do rail (`/malha-fina/nfe-entradas`) continua funcionando — mesmo tratamento dado às rotas antigas de DFe-s/Notas Importadas.
- CT-e no módulo Malha Fina só tem direção "Entrada" (não existe `MalhaFinaCTeSaidas`) — mesmo padrão de `DIRECOES_DISPONIVEIS` já usado nos módulos anteriores.

**Ask First:** Se surgir necessidade de extrair um layout genérico compartilhado entre os 3 módulos de submenu vertical já existentes (em vez de duplicar o shell pela 3ª vez), HALT e perguntar — não estava no escopo combinado.

**Never:** Não alterar a lógica interna de `MalhaFinaPanel.tsx` (filtros, ordenação, cálculo de totais, toggle RFB, etc.). Não implementar consulta real para os 9 tipos sem tela hoje — aparecem desabilitados. Não mexer nos módulos CGIBS, Receita Federal ou na remoção de "Importações" — adiados para specs seguintes (ver `deferred-work.md`).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Usuário clica "Malha Fina" no rail | Navegação | Abre módulo com submenu vertical, NF-e ativo, Entrada como direção padrão, renderiza `MalhaFinaPanel tipo="nfe-entradas"` | N/A |
| Seleciona NF-e + Saída | N/A | Renderiza `MalhaFinaPanel tipo="nfe-saidas"` sem alterar seu comportamento interno | N/A |
| Seleciona CT-e | N/A | Direção mostra só "Entrada" habilitada (só existe `tipo="cte"`); "Saída" desabilitada | N/A |
| Clica em tipo desabilitado (ex: NFS-e) | N/A | Item não navega, sem indicação de badge (removida por decisão de UI já aplicada), sem erro | N/A |
| Acesso via rota antiga `/malha-fina/nfe-saidas` | Navegação direta | Abre a nova tela já com NF-e + Saída pré-selecionados | N/A |

</frozen-after-approval>

## Code Map

- `frontend/src/pages/NotasImportadasModulo.tsx` -- referência de shell mais recente (submenu vertical + seletor de direção + sanitização de props inválidas) a replicar
- `frontend/src/pages/MalhaFinaModulo.tsx` -- novo componente (a criar): mesmo shell, renderiza `MalhaFinaPanel` parametrizado por `{tipo, direção}` em vez de montar um componente inteiro diferente por combinação
- `frontend/src/pages/MalhaFinaPanel.tsx` -- não alterado; recebe prop `tipo` (`'nfe-entradas' | 'nfe-saidas' | 'cte'`) e `rfbDisponivel`
- `frontend/src/pages/MalhaFinaNFeEntradas.tsx`, `MalhaFinaNFeSaidas.tsx`, `MalhaFinaCTe.tsx` -- wrappers finos existentes que hoje são o alvo direto das rotas; permanecem como estão (não removidos), o novo módulo reimplementa a mesma composição `tipo`+`title`+`description`+`rfbDisponivel` inline
- `frontend/src/lib/navigation.ts:65-82` -- módulo `malha` atual (13 abas horizontais); ajustar para `tabs: []` como feito com `dfes`/`notas`
- `frontend/src/components/AppRail.tsx:40` -- item `malha` em `mainItems`, mantém posição própria no rail, já aponta para rota válida (`/malha-fina/nfe-entradas`)
- `frontend/src/App.tsx:250-252` -- rotas atuais de malha fina, substituir pelos 2 apontamentos (NF-e usa 1 componente com 2 props de seleção inicial + CT-e) para `MalhaFinaModulo` com seleção inicial diferente

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/pages/MalhaFinaModulo.tsx` -- criar componente com submenu vertical dos tipos de documento (2 habilitados: NF-e Entrada/Saída, CT-e Entrada, nas primeiras linhas do array, sem badge "Em breve"), seletor de direção, e um mapa `{tipo, direção} -> {tipoPanel, title, description, rfbDisponivel}` que invoca `<MalhaFinaPanel />` com as mesmas props usadas hoje pelos wrappers -- aplica o padrão de navegação já validado, mantendo `MalhaFinaPanel` intocado. Nota: a lista final ficou com 10 tipos (não 12) — ver Spec Change Log.
- [x] `frontend/src/lib/navigation.ts` -- mudou `modules.malha.tabs` para `[]`
- [x] `frontend/src/App.tsx` -- importado `MalhaFinaModulo`, as 3 rotas existentes (`/malha-fina/nfe-entradas`, `/malha-fina/nfe-saidas`, `/malha-fina/cte`) apontam para o mesmo componente com seleção inicial diferente via prop -- preserva links/bookmarks existentes
- [x] `frontend/src/components/AppRail.tsx` -- conferido: item `malha` já apontava para rota válida (`/malha-fina/nfe-entradas`) -- sem mudança necessária

**Acceptance Criteria:**
- Given o usuário está autenticado, when clica em "Malha Fina" no rail, then vê o submenu vertical com 12 tipos (NF-e e CT-e sem badge, nas primeiras linhas), NF-e ativo, e o painel `MalhaFinaPanel` com tipo="nfe-entradas" renderizado com filtros/paginação funcionando normalmente
- Given o usuário navega para `/malha-fina/nfe-saidas` (rota salva/antiga), when a página carrega, then NF-e + Saída vêm pré-selecionados e `MalhaFinaPanel tipo="nfe-saidas"` é exibido
- Given "Importação DFe-s" e "Notas Importadas" já existem como módulos com o mesmo shell, when o usuário alterna entre os três no rail, then cada um mantém seu próprio ícone, rota e estado de seleção independente

## Spec Change Log

- 2026-07-15 (implementação): a spec (seção congelada) menciona "12 tipos de documento (3 habilitados, 9 desabilitados)" — número herdado por engano do padrão de DFe-s/Notas Importadas, que inclui NFC-e/Água/Gás/Eventos/Outros. A lista original de Malha Fina em `navigation.ts` (antes desta mudança) tinha apenas 10 tipos distintos (NF-e, CT-e, NFS-e, BP-e, NF3-e, NFCom-e, NFag-e, NF-e ABI, ND-e, NC-e). Implementado com os 10 tipos reais (2 habilitados: NF-e, CT-e; 8 desabilitados), preservando fielmente o que já existia na aba horizontal — não foi adicionado nenhum tipo novo que não estivesse na lista anterior.
- 2026-07-15 (review, patch): adicionado `key={`${tipoAtivo}-${direcao}`}` em `<MalhaFinaPanel>` (`MalhaFinaModulo.tsx`) — achado real e de alta severidade do Edge Case Hunter. Diferente de `NotasImportadasModulo.tsx`/`DFesModulo.tsx` (que trocam o componente inteiro por tipo, remontando automaticamente), aqui uma ÚNICA instância de `MalhaFinaPanel` é reaproveitada entre todas as combinações tipo/direção — sem a `key`, o estado interno do painel (filtros, página, ordenação, modal de detalhe aberto) sobrevivia à troca de tipo, e o `keepPreviousData` do TanStack Query podia mostrar linhas do tipo anterior por um instante. Validado via Playwright: título do painel muda corretamente a cada troca após o fix.
- 2026-07-15 (review, patch): `MalhaFinaModuloProps.tipoInicial` apertado de `string` solto para `'nfe' | 'cte'` — achado do Blind Hunter (tipagem fraca permitia typo silencioso em `App.tsx` sem erro de compilação, caindo em fallback silencioso de runtime). `tipoAtivo` (estado interno, que pode assumir qualquer chave de `TIPOS` ao clicar em um item do submenu) permanece `string` — o gap era só na fronteira pública do componente.

## Design Notes

Diferente de `NotasImportadasModulo.tsx` (mapa `{tipo}-{direção} -> componente inteiro diferente`), aqui as 3 combinações habilitadas já convergem para o MESMO componente (`MalhaFinaPanel`), só variando as props `tipo`/`title`/`description`/`rfbDisponivel` — mais parecido com o padrão `UPLOAD_CONFIG` de `DFesModulo.tsx`. Sugestão:
```tsx
const PANEL_CONFIG: Record<string, { tipo: string; title: string; description: string; rfbDisponivel: boolean } | undefined> = {
  'nfe-entrada': { tipo: 'nfe-entradas', title: 'Malha Fina — NF-e Entradas', description: '...', rfbDisponivel: false },
  'nfe-saida':   { tipo: 'nfe-saidas',   title: 'Malha Fina — NF-e Saídas',   description: '...', rfbDisponivel: true },
  'cte-entrada': { tipo: 'cte',          title: 'Malha Fina — CT-e',          description: '...', rfbDisponivel: false },
}
```
E renderizar `const cfg = PANEL_CONFIG[\`${tipo}-${direcao}\`]; return cfg ? <MalhaFinaPanel {...cfg} /> : <EmBreve />`. Textos de `description` copiados literalmente dos 3 wrappers existentes (`MalhaFinaNFeEntradas.tsx`, `MalhaFinaNFeSaidas.tsx`, `MalhaFinaCTe.tsx`) para preservar exatamente o texto atual.

## Verification

**Commands:**
- `cd frontend && npx tsc --noEmit --project tsconfig.app.json` -- expected: sem erros de tipo novos (ignorar erros pré-existentes não relacionados a este arquivo)
- `cd frontend && npm run build` -- expected: build passa

**Manual checks (if no CLI):**
- Abrir `/malha-fina/nfe-entradas`, `/malha-fina/nfe-saidas`, `/malha-fina/cte` e confirmar que cada painel carrega com filtros/paginação/toggle RFB funcionando, dentro do novo shell
- Confirmar que "Importação DFe-s" e "Notas Importadas" continuam funcionando de forma independente (não afetados)

## Suggested Review Order

**Componente novo: shell + reuso de MalhaFinaPanel**

- Entrada principal: mapa `PANEL_CONFIG` que substitui os 3 wrappers antigos por props inline.
  [`MalhaFinaModulo.tsx:38`](../../frontend/src/pages/MalhaFinaModulo.tsx#L38)

- `key` forçando remount do painel ao trocar tipo/direção — fix crítico do Edge Case Hunter (sem isso, filtros/página/modal vazavam entre tipos).
  [`MalhaFinaModulo.tsx:131`](../../frontend/src/pages/MalhaFinaModulo.tsx#L131)

- Sanitização do estado inicial de `direcao` contra props incompatíveis (ex: CT-e + Saída), mesmo padrão já usado em `NotasImportadasModulo.tsx`.
  [`MalhaFinaModulo.tsx:69`](../../frontend/src/pages/MalhaFinaModulo.tsx#L69)

- Lista `TIPOS` reordenada (habilitados primeiro) e badge "Em breve" já removida por decisão de UI anterior — 10 tipos reais, não 12 (ver Spec Change Log).
  [`MalhaFinaModulo.tsx:16`](../../frontend/src/pages/MalhaFinaModulo.tsx#L16)

**Roteamento e navegação**

- As 3 rotas de Malha Fina agora apontam para o novo componente com seleção inicial via prop.
  [`App.tsx:248-250`](../../frontend/src/App.tsx#L248-L250)

- `modules.malha.tabs` esvaziado — oculta a barra horizontal automaticamente via `ModuleTabs`.
  [`navigation.ts:65`](../../frontend/src/lib/navigation.ts#L65)

**Follow-up**

- Achados adiados desta revisão (sync de URL, divergência entre as 3 listas `TIPOS`, gap futuro em `DIRECOES_DISPONIVEIS`, arquivos wrapper órfãos).
  [`deferred-work.md`](deferred-work.md)
