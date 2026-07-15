---
title: 'Módulos RFB e CGIBS: barra de abas horizontal vira submenu vertical'
type: 'feature'
created: '2026-07-15'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '3a4ec9ac4a87827196c5b2861ab0d150b08a9631'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Os módulos "Receita Federal - Apuração Assistida" (rfb, 8 abas) e "CGIBS - Apuração Assistida IBS" (cgibs, 7 abas) ainda usam a barra horizontal `ModuleTabs` (`frontend/src/App.tsx:130`) — mesma limitação de escala já resolvida em Importação DFe-s, Notas Importadas e Malha Fina. Diferente desses 3 módulos, aqui as abas (`Gestão CBS RFB`, `Importar Movimento`, `Débitos mês`, etc.) são estágios de fluxo com rotas/páginas próprias já reais, não tipos de documento fiscal renderizados por um painel único — então não se cria um novo componente `*Modulo.tsx` shell.

**Approach:** Generalizar o `ModuleTabs` existente para suportar orientação vertical via um novo campo opcional `orientation?: 'horizontal' | 'vertical'` em `ModuleConfig` (`navigation.ts`). Quando `orientation === 'vertical'`, o `AppLayout` renderiza as mesmas abas (mesmos links, mesmos estados ativo/disabled/danger) como um `<aside>` à esquerda do `<main>`, em vez da barra horizontal acima dele. Setar `orientation: 'vertical'` apenas para `rfb` e `cgibs`. Nenhuma rota, página ou label muda.

## Boundaries & Constraints

**Always:**
- Nenhuma rota em `App.tsx` é criada, removida ou redirecionada — `GestaoCredIBSCBS.tsx`, `CGIBSPainel.tsx` e as demais páginas de rfb/cgibs permanecem 100% intocadas.
- Todos os outros módulos (`painel`, `importacoes`, `notas`, `dfes`, `malha`, `argus`, `config`) continuam renderizando horizontal — comportamento padrão quando `orientation` está ausente (default `'horizontal'`), nenhuma regressão visual nesses módulos.
- Mesma lógica de estado ativo/disabled/danger do `ModuleTabs` atual é reaproveitada — apenas o eixo do layout muda (flex-col em vez de flex-row), nenhum novo mecanismo de destaque.
- `getActiveModule()` não muda — mapeamento módulo→rota já cobre `rfb`/`cgibs`.

**Ask First:** Nenhuma — mudança de layout compartilhado com blast radius contido a `navigation.ts` + `App.tsx`, sem tocar páginas.

**Never:** Não introduzir um componente de shell tipo `RFBModulo.tsx`/`CGIBSModulo.tsx` (padrão dos 3 módulos anteriores) — não se aplica aqui pois não há painel único parametrizável por tipo de documento.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Navegação em módulo rfb | Usuário acessa `/rfb/gestao-creditos` | Submenu vertical à esquerda com as 8 abas (3 desabilitadas), item ativo destacado, `<main>` ocupa o restante da largura | N/A |
| Navegação em módulo cgibs | Usuário acessa `/cgibs/apuracao-ibs` | Submenu vertical à esquerda com as 7 abas (4 desabilitadas), item ativo destacado | N/A |
| Módulo horizontal inalterado | Usuário acessa `/malha-fina/nfe-entradas` (tabs vazio) ou `/importacoes/nfe-entrada` (orientation ausente) | Comportamento idêntico ao atual — nenhuma barra (tabs vazio) ou barra horizontal (default) | N/A |
| Clique em aba desabilitada | Usuário clica em "Pagamentos CBS" (disabled) no submenu vertical | Não navega, cursor `not-allowed`, mesmo estilo acinzentado já usado no horizontal | N/A |

</frozen-after-approval>

## Code Map

- `frontend/src/lib/navigation.ts` -- adicionar `orientation?: 'horizontal' | 'vertical'` em `ModuleConfig`; setar `orientation: 'vertical'` nos configs `rfb` e `cgibs`.
- `frontend/src/App.tsx:130-174` (`ModuleTabs`) -- extrair a lista de abas renderizada para aceitar orientação; ou criar `ModuleSideNav` irmão reaproveitando o mesmo mapeamento de estilos.
- `frontend/src/App.tsx:196-206` (`AppLayout`) -- quando módulo ativo tem `orientation === 'vertical'`, não renderizar `ModuleTabs` horizontal acima do `<main>`; em vez disso, envolver `<main>` num container flex-row com o novo `<aside>` à esquerda.

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/lib/navigation.ts` -- adicionar campo `orientation?: 'horizontal' | 'vertical'` à interface `ModuleConfig` e setar `orientation: 'vertical'` em `rfb` e `cgibs` -- ponto único de configuração, sem tocar nos demais módulos
- [x] `frontend/src/App.tsx` -- criar `ModuleSideNav()` (mesma lógica de `visibleTabs`/isActive/isDisabled/danger do `ModuleTabs`, layout `flex flex-col` num `<aside className="w-56 shrink-0 border-r bg-white overflow-y-auto">`) -- reaproveita estado e regras de estilo já validadas, só muda o container
- [x] `frontend/src/App.tsx` (`AppLayout`) -- ler `moduleCfg?.orientation`; se `'vertical'` e `tabs.length > 0`, renderizar `<ModuleSideNav />` ao lado de `<main>` dentro de um wrapper `flex flex-1 min-h-0`, e **não** renderizar `<ModuleTabs />` acima; caso contrário manter comportamento atual -- único ponto de decisão de layout

**Acceptance Criteria:**
- Given o usuário está em `/rfb/*` ou `/cgibs/*`, when a página carrega, then o submenu aparece verticalmente à esquerda do conteúdo, com os mesmos links/labels/estados de `navigation.ts` atuais.
- Given o usuário está em qualquer outro módulo com `tabs` não vazio, when a página carrega, then a barra horizontal `ModuleTabs` continua aparecendo exatamente como hoje (sem `orientation` = comportamento default horizontal).
- Given uma aba está com `disabled: true`, when renderizada no submenu vertical, then não é clicável e mantém o estilo acinzentado já usado no horizontal.

## Design Notes

O `<aside>` vertical fica dentro da coluna já existente (`flex flex-col flex-1` que contém `AppHeader`), como um novo `flex flex-row` que envolve `ModuleSideNav` + `<main>` — não mexe no `AppRail` (ícones de módulo) nem no `AppHeader`. Largura fixa `w-56` (mais estreita que o `AppRail` de ícones, mais larga que os itens do submenu de DFe-s/Malha Fina que usam `w-64` para labels mais longos — aqui os labels são mais curtos, `w-56` é suficiente, ajustável durante implementação se algum label quebrar linha).

## Verification

**Commands:**
- `npx tsc --noEmit -p frontend` -- expected: sem novos erros de tipo
- `npm run build` (em `frontend/`) -- expected: build passa

**Manual checks (if no CLI):**
- Abrir `/rfb/gestao-creditos` e `/cgibs/apuracao-ibs` no navegador local — confirmar submenu vertical com abas corretas, navegação funcional, abas desabilitadas não clicáveis.
- Abrir `/malha-fina/nfe-entradas`, `/importacoes/nfe-entrada`, `/config/aliquotas` — confirmar que nada mudou visualmente nesses módulos.

## Suggested Review Order

**Configuração da orientação**

- Ponto único de configuração: novo campo opcional, default horizontal para todos os módulos exceto os dois setados aqui.
  [`navigation.ts:11`](../../frontend/src/lib/navigation.ts#L11)

- `rfb`/`cgibs` são os únicos módulos com `orientation: 'vertical'` — nenhum outro módulo muda.
  [`navigation.ts:43`](../../frontend/src/lib/navigation.ts#L43)

**Novo componente de navegação vertical**

- `ModuleSideNav` espelha a lógica de `visibleTabs`/isActive/isDisabled/danger de `ModuleTabs` (linha 130), só troca o container de `flex-row` horizontal para `flex-col` num `<aside>`.
  [`App.tsx:177`](../../frontend/src/App.tsx#L177)

**Integração no layout — decisão de review corrigida**

- `AppLayout` decide `isVertical` uma vez e renderiza `ModuleSideNav`/`ModuleTabs` como irmãos condicionais dentro do MESMO container `<main>`, em vez de trocar toda a subárvore — corrige um remount indevido de `<main>` ao cruzar a fronteira vertical/horizontal (ex: navegar de `/rfb/gestao-creditos` para `/rfb/credenciais`, que pertence ao módulo `config` horizontal), achado pela revisão adversarial e corrigido nesta rodada (ver `deferred-work.md` para os achados que ficaram para depois).
  [`App.tsx:319`](../../frontend/src/App.tsx#L319)

- `routes` extraído para uma constante reaproveitada pelos dois ramos — indentação normalizada durante a correção acima.
  [`App.tsx:252`](../../frontend/src/App.tsx#L252)
