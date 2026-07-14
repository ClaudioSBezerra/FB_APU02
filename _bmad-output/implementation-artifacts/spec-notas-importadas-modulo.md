---
title: 'Módulo Notas Importadas: submenu vertical por tipo de documento'
type: 'feature'
created: '2026-07-14'
status: 'done'
review_loop_iteration: 0
context: []
baseline_commit: '81e155d466250598769387c141bb113990452e34'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O módulo "Notas Importadas" hoje usa abas horizontais (NF-e Entradas, NF-e Saídas, CT-e Entradas + 9 tipos desabilitados) — mesma limitação de escala já resolvida no módulo "Importação DFe-s" (`DFesModulo.tsx`): não comporta bem os 12 tipos de documento fiscal do negócio.

**Approach:** Replicar o shell de navegação do `DFesModulo.tsx` (submenu vertical por tipo de documento + seletor Entrada/Saída) para um novo componente `NotasImportadasModulo.tsx`, mantendo "Notas Importadas" como módulo de menu PRÓPRIO e separado de "Importação DFe-s" — não fundir os dois. As telas de consulta existentes (`ConsultaNFesEntradas`, `ConsultaNFeSaidas`, `ConsultaCTesEntradas`) são componentes grandes e distintos (colunas, tipos e queries próprias) — apenas realocadas para dentro do novo shell, sem alterar sua lógica interna, filtros ou tabelas.

## Boundaries & Constraints

**Always:**
- "Notas Importadas" continua sendo um item de menu independente no `AppRail`, com seu próprio ícone/rota — não absorvido por "Importação DFe-s".
- As 3 telas de consulta (`ConsultaNFesEntradas.tsx`, `ConsultaNFeSaidas.tsx`, `ConsultaCTesEntradas.tsx`) permanecem com a lógica interna 100% intacta — mesmas queries, colunas, filtros, paginação. A única mudança é onde/como são exibidas na navegação.
- Seguir exatamente o mesmo shell visual de `DFesModulo.tsx` (`frontend/src/pages/DFesModulo.tsx`): lista vertical à esquerda com os 12 tipos de documento (3 habilitados — NF-e, CT-e — e 9 desabilitados/"Em breve"), seletor Entrada/Saída à direita, mesmo componente `cn`/`Badge`/estilo.
- Rotas antigas (`/apuracao/entrada/notas`, `/apuracao/saida/notas`, `/apuracao/cte-entrada/notas`) continuam funcionando — mesmo tratamento dado a `/importacoes/*` na spec anterior.

**Ask First:** Se surgir necessidade de extrair um layout genérico compartilhado entre `DFesModulo.tsx` e o novo `NotasImportadasModulo.tsx` (em vez de duplicar o shell), HALT e perguntar — não estava no escopo combinado.

**Never:** Não alterar a lógica interna das 3 telas de consulta (filtros, ordenação, cálculo de totais, exportação, etc.). Não fundir "Notas Importadas" com "Importação DFe-s" em um único menu. Não implementar consulta real para os 9 tipos sem tela hoje — aparecem desabilitados, igual ao padrão já usado em `DFesModulo.tsx`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Usuário clica "Notas Importadas" no rail | Navegação | Abre módulo com submenu vertical, NF-e ativo, Entrada como direção padrão, renderiza `ConsultaNFesEntradas` |  N/A |
| Seleciona NF-e + Saída | N/A | Renderiza `ConsultaNFeSaidas` sem alterar seu comportamento interno | N/A |
| Seleciona CT-e | N/A | Direção mostra só "Entrada" habilitada (só existe `ConsultaCTesEntradas`); "Saída" desabilitada | N/A |
| Clica em tipo desabilitado (ex: NFC-e) | N/A | Item não navega, mostra "Em breve", sem erro | N/A |
| Acesso via rota antiga `/apuracao/saida/notas` | Navegação direta | Abre a nova tela já com NF-e + Saída pré-selecionados | N/A |

</frozen-after-approval>

## Code Map

- `frontend/src/pages/DFesModulo.tsx` -- referência de shell (submenu vertical + seletor de direção) a replicar, não a importar diretamente (tipos de config diferentes: upload vs. consulta)
- `frontend/src/pages/NotasImportadasModulo.tsx` -- novo componente (a criar): mesmo shell, renderiza o componente de consulta correspondente a `{tipo, direção}` em vez de um formulário de upload
- `frontend/src/pages/ConsultaNFesEntradas.tsx`, `ConsultaNFeSaidas.tsx`, `ConsultaCTesEntradas.tsx` -- não alterados; passam a ser renderizados dentro do novo shell em vez de via rota direta
- `frontend/src/lib/navigation.ts:22-39` -- módulo `notas` atual (abas horizontais); ajustar para `tabs: []` como foi feito com `dfes`, e branch em `getActiveModule` para reconhecer as rotas de notas
- `frontend/src/components/AppRail.tsx:35` -- item `notas` em `mainItems`, mantém posição própria no rail, só precisa continuar apontando para uma rota válida
- `frontend/src/App.tsx:239-245` -- rotas atuais de notas (`/apuracao/entrada/notas`, etc.), substituir pelos 3 apontamentos para `NotasImportadasModulo` com seleção inicial diferente (mesmo padrão usado para `/dfes` vs. as rotas antigas de `/importacoes/*`)

## Tasks & Acceptance

**Execution:**
- [x] `frontend/src/pages/NotasImportadasModulo.tsx` -- criar componente com submenu vertical dos 12 tipos (3 habilitados: NF-e Entrada/Saída, CT-e Entrada), seletor de direção, e um mapa `{tipo, direção} -> componente de consulta` que renderiza `ConsultaNFesEntradas`/`ConsultaNFeSaidas`/`ConsultaCTesEntradas` sem alterá-los -- aplica o padrão de navegação já validado, mantendo as telas de consulta intocadas
- [x] `frontend/src/lib/navigation.ts` -- mudar `modules.notas.tabs` para `[]` (branch em `getActiveModule` já existia via `pathname.includes('/notas')`, não precisou de ajuste) -- oculta a barra horizontal automaticamente via `ModuleTabs`
- [x] `frontend/src/App.tsx` -- importar `NotasImportadasModulo`, apontar as 3 rotas existentes de notas para o mesmo componente com seleção inicial diferente via prop -- preserva links/bookmarks existentes
- [x] `frontend/src/components/AppRail.tsx` -- conferido: item `notas` em `mainItems` já apontava para rota válida (`/apuracao/saida/notas`) -- sem mudança necessária

**Acceptance Criteria:**
- Given o usuário está autenticado, when clica em "Notas Importadas" no rail, then vê o submenu vertical com 12 tipos, NF-e ativo, e a tabela de `ConsultaNFesEntradas` renderizada com seus filtros/paginação funcionando normalmente
- Given o usuário navega para `/apuracao/saida/notas` (rota salva/antiga), when a página carrega, then NF-e + Saída vêm pré-selecionados e `ConsultaNFeSaidas` é exibida
- Given "Importação DFe-s" já existe como módulo separado, when o usuário alterna entre os dois módulos no rail, then cada um mantém seu próprio ícone, rota e estado de seleção independente

## Spec Change Log

- 2026-07-14 (review): removida variável morta `tipoAtivoInfo` (computada via `useMemo`, nunca lida no JSX) em `NotasImportadasModulo.tsx` — achado real do Blind Hunter, confirmado via `npx tsc --noEmit --project tsconfig.app.json` (TS6133).
- 2026-07-14 (review): estado inicial de `direcao` agora valida se `direcaoInicial` está entre as direções disponíveis para `tipoInicial` (`DIRECOES_DISPONIVEIS`); se não estiver, cai para a primeira direção válida do tipo. Corrige um estado inconsistente possível se o componente for invocado futuramente com props incompatíveis (ex: CT-e + Saída) — achado do Edge Case Hunter.
- Demais achados de ambos revisores classificados como falso-positivo, já aceitos como padrão pré-existente em `DFesModulo.tsx`, ou adiados — ver `deferred-work.md`, seção "Deferred from: code review of spec-notas-importadas-modulo (2026-07-14)".

## Design Notes

Diferente do `DFesModulo.tsx` (que tem UM corpo de upload parametrizado por config), aqui cada combinação `{tipo, direção}` mapeia para um **componente inteiro diferente** — não dá para extrair uma tabela de config genérica com a mesma facilidade. Sugestão:
```tsx
const CONSULTA_POR_TIPO: Record<string, React.ComponentType | undefined> = {
  'nfe-entrada': ConsultaNFesEntradas,
  'nfe-saida': ConsultaNFeSaidas,
  'cte-entrada': ConsultaCTesEntradas,
}
```
E renderizar `const Componente = CONSULTA_POR_TIPO[\`${tipo}-${direcao}\`]; return Componente ? <Componente /> : <EmBreve />`. Isso evita duplicar o corpo de cada tela dentro do shell novo — cada consulta continua 100% autocontida.

## Verification

**Commands:**
- `cd frontend && npx tsc --noEmit` -- expected: sem erros de tipo
- `cd frontend && npm run build` -- expected: build passa

**Manual checks (if no CLI):**
- Abrir `/apuracao/entrada/notas`, `/apuracao/saida/notas`, `/apuracao/cte-entrada/notas` e confirmar que cada consulta carrega com filtros/paginação funcionando, dentro do novo shell
- Confirmar que o módulo "Importação DFe-s" continua funcionando de forma independente (não foi afetado)

## Suggested Review Order

1. [NotasImportadasModulo.tsx](../../frontend/src/pages/NotasImportadasModulo.tsx) -- componente novo: submenu vertical, seletor de direção, mapa `CONSULTA_POR_TIPO`, sanitização do estado inicial de `direcao`
2. [App.tsx:239-243](../../frontend/src/App.tsx#L239-L243) -- as 3 rotas de notas agora apontam para `NotasImportadasModulo` com props de seleção inicial
3. [navigation.ts:22-25](../../frontend/src/lib/navigation.ts#L22-L25) -- `modules.notas.tabs` esvaziado (abas horizontais removidas)
4. [deferred-work.md](deferred-work.md) -- achados adiados desta revisão (duplicação de `TIPOS`, falta de sync com URL, ausência de code-splitting, lacuna de validação de props na spec)
