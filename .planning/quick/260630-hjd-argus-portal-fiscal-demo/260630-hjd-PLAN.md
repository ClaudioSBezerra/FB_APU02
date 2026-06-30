---
phase: quick-260630-hjd
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/public/argus-portal-fiscal.html
  - frontend/src/pages/ArgusPortal.tsx
  - frontend/src/App.tsx
  - frontend/src/lib/navigation.ts
autonomous: false
requirements:
  - DEMO-ARGUS-01
must_haves:
  truths:
    - "O arquivo /argus-portal-fiscal.html é servido estaticamente pelo Vite e abre o portal Argus standalone"
    - "Acessar /argus no app autenticado mostra o portal Argus dentro da área de conteúdo via iframe"
    - "Um novo módulo 'Portal Fiscal CBS/IBS (Demo)' aparece na navegação logo após 'Malha Fina' e leva a /argus"
    - "Nenhuma chamada de backend/API real é feita pelo módulo de demonstração"
  artifacts:
    - path: "frontend/public/argus-portal-fiscal.html"
      provides: "HTML standalone Argus (mock 100% estático, CSS/JS embutidos)"
    - path: "frontend/src/pages/ArgusPortal.tsx"
      provides: "Página React que embute o portal via iframe"
      exports: ["default"]
    - path: "frontend/src/App.tsx"
      provides: "Rota protegida /argus"
      contains: "ArgusPortal"
    - path: "frontend/src/lib/navigation.ts"
      provides: "Módulo argus + reconhecimento em getActiveModule"
      contains: "argus"
  key_links:
    - from: "frontend/src/pages/ArgusPortal.tsx"
      to: "/argus-portal-fiscal.html"
      via: "iframe src"
      pattern: "argus-portal-fiscal\\.html"
    - from: "frontend/src/App.tsx"
      to: "ArgusPortal"
      via: "Route path=/argus"
      pattern: "path=\"/argus\""
    - from: "frontend/src/lib/navigation.ts"
      to: "/argus"
      via: "module tab + getActiveModule startsWith"
      pattern: "argus"
---

<objective>
Adicionar o módulo de demonstração "Portal Fiscal CBS/IBS (Argus)" ao FB_APU02 para apresentação ao sponsor, usando o HTML standalone fornecido pelo usuário, 100% mocado e isolado do restante do sistema.

Purpose: Permitir demonstrar a visão "Argus" ao sponsor sem reconstruir telas em shadcn/Tailwind e sem risco de colisão de estilos globais com o app React (o HTML tem reset `*`, `body`, `html`). O isolamento via iframe garante que o CSS do Argus não afete o app e vice-versa.

Output: Arquivo estático em `public/`, página React com iframe, rota protegida `/argus`, e entrada de navegação após "Malha Fina".
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@.planning/STATE.md

<interfaces>
<!-- Contratos existentes que o executor deve seguir. Extraídos do código. -->

frontend/src/lib/navigation.ts (estrutura do registry de módulos):
```typescript
export interface ModuleTab { label: string; path: string; disabled?: boolean; danger?: boolean; adminOnly?: boolean }
export interface ModuleConfig { label: string; tabs: ModuleTab[] }
export const modules: Record<string, ModuleConfig> = { ...painel, notas, importacoes, cgibs, rfb, malha, config }
export function getActiveModule(pathname: string): string
```
O módulo `malha` termina na linha ~96; `config` vem logo em seguida. O novo módulo `argus` deve ser inserido ENTRE `malha` e `config`.

`getActiveModule` é uma cadeia de `if` com `return` antecipado. A regra do `argus` deve ser adicionada ANTES do bloco final de `config` e antes do `return 'painel'` default, para que `/argus` resolva para `'argus'`.

frontend/src/App.tsx (padrão de rota protegida dentro de AppLayout):
```tsx
// As rotas dentro de <AppLayout> já estão sob <ProtectedRoute> no nível do App root (path "/*").
// O <main> envolve <Routes> com <div className="p-4">. Há um header h-12 + barra de abas h-10.
<Route path="/malha-fina/cte" element={<MalhaFinaCTe />} />
```
A área de conteúdo tem padding `p-4` (1rem em cada lado) aplicado pelo wrapper em `<main>`. A altura disponível abaixo do header (h-12 = 3rem) e da barra de abas (h-10 = 2.5rem) é `100vh - 3rem - 2.5rem`, menos o padding vertical `p-4` (2 * 1rem). O iframe deve ocupar essa área sem barra de rolagem dupla.
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Criar o HTML standalone do portal Argus em public/</name>
  <files>frontend/public/argus-portal-fiscal.html</files>
  <action>
    Criar o arquivo `frontend/public/argus-portal-fiscal.html` com o conteúdo LITERAL e INTEGRAL do documento HTML "argus-portal-fiscal_V2.html" fornecido pelo usuário nesta sessão (documento anexado à conversa do orquestrador, ~700 linhas de HTML/CSS/JS autocontidas, com tema claro/escuro, sidebar e as 10 seções: Visão Geral, DF-e's, Split Payment, Apuração, Créditos, Certificados, Integrações, Relatórios, Configurações, Administração).

    Regras OBRIGATÓRIAS:
    - Reproduzir o conteúdo EXATAMENTE como fornecido — não alterar texto, dados, estilos, scripts, números de mock, nem estrutura. É um mock estático já pronto.
    - Não adicionar nenhuma chamada fetch/API real; o arquivo deve permanecer autocontido (CSS e JS embutidos, sem dependências externas além das que já estiverem no próprio HTML).
    - Não converter para componentes React; é um único arquivo `.html` servido como asset estático por Vite (pasta `public/` é exposta na raiz do servidor).
    - Se o conteúdo exato do HTML não estiver acessível no contexto de execução, PARAR e solicitar o arquivo ao usuário em vez de inventar conteúdo.
  </action>
  <verify>
    <automated>test -f frontend/public/argus-portal-fiscal.html &amp;&amp; grep -qi "<!doctype html\|<html" frontend/public/argus-portal-fiscal.html &amp;&amp; echo OK</automated>
  </verify>
  <done>Arquivo existe em frontend/public/argus-portal-fiscal.html, é um documento HTML completo (doctype/html), reproduzindo fielmente o mock Argus fornecido.</done>
</task>

<task type="auto">
  <name>Task 2: Criar a página ArgusPortal com iframe e registrar rota + navegação</name>
  <files>frontend/src/pages/ArgusPortal.tsx, frontend/src/App.tsx, frontend/src/lib/navigation.ts</files>
  <action>
    1) Criar `frontend/src/pages/ArgusPortal.tsx` como `export default function ArgusPortal()` (padrão de página: default export único, sem custom hook). A página deve renderizar um único `<iframe>` que aponta para `/argus-portal-fiscal.html`:
       - `src="/argus-portal-fiscal.html"`
       - `title="Portal Fiscal CBS/IBS (Argus)"` (acessibilidade)
       - sem borda (`style={{ border: 'none' }}` ou classe Tailwind equivalente)
       - largura 100% e altura preenchendo a área de conteúdo. Como o wrapper `<main>` aplica `p-4` (1rem) ao redor, calcular a altura para evitar rolagem dupla: usar `height: 'calc(100vh - 3rem - 2.5rem - 2rem)'` (header h-12 = 3rem, barra de abas h-10 = 2.5rem, padding vertical p-4 = 2rem total). Largura `100%`. Aplicar `display: block` para remover gap inline do iframe.
       - Não fazer NENHUMA chamada fetch/API; a página é puramente um host de iframe.

    2) Em `frontend/src/App.tsx`:
       - Adicionar o import `import ArgusPortal from './pages/ArgusPortal'` junto aos demais imports de páginas.
       - Adicionar a rota dentro do `<Routes>` de `AppLayout` (já está sob `<ProtectedRoute>` via path `/*` no App root, seguindo o mesmo padrão das outras rotas internas): `<Route path="/argus" element={<ArgusPortal />} />`. Inserir num bloco comentado próprio, por exemplo `{/* Portal Argus (Demo) */}`.

    3) Em `frontend/src/lib/navigation.ts`:
       - Inserir um novo módulo `argus` no objeto `modules`, ENTRE o módulo `malha` e o módulo `config`:
         label `'Portal Fiscal CBS/IBS (Demo)'`, com um único tab `{ label: 'Portal Argus', path: '/argus' }`.
       - Em `getActiveModule(pathname)`, adicionar `if (pathname.startsWith('/argus')) return 'argus'` antes do bloco de `config` e do `return 'painel'` default.
  </action>
  <verify>
    <automated>cd frontend &amp;&amp; npx tsc -p tsconfig.app.json --noEmit &amp;&amp; grep -q "path=\"/argus\"" src/App.tsx &amp;&amp; grep -q "import ArgusPortal" src/App.tsx &amp;&amp; grep -q "argus:" src/lib/navigation.ts &amp;&amp; grep -q "startsWith('/argus')" src/lib/navigation.ts &amp;&amp; grep -q "argus-portal-fiscal.html" src/pages/ArgusPortal.tsx</automated>
  </verify>
  <done>tsc passa sem erros; rota /argus registrada e importada em App.tsx; módulo argus existe em navigation.ts entre malha e config; getActiveModule reconhece /argus; iframe aponta para /argus-portal-fiscal.html.</done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <what-built>Módulo de demonstração Argus: HTML estático em public/, página ArgusPortal com iframe, rota /argus e entrada de navegação "Portal Fiscal CBS/IBS (Demo)" após "Malha Fina".</what-built>
  <how-to-verify>
    1. Rodar o frontend em dev: `cd frontend && npm run dev` (porta 3000) com o backend ativo (login necessário) — ou acessar o ambiente onde o app já roda.
    2. Fazer login normalmente.
    3. Na navegação (AppRail/abas), localizar o novo módulo "Portal Fiscal CBS/IBS (Demo)" logo após "Malha Fina" e clicar nele / acessar a aba "Portal Argus".
    4. Confirmar que a URL vai para /argus e que o portal Argus carrega dentro da área de conteúdo (iframe), com seu próprio tema/sidebar.
    5. Verificar visualmente: sem barra de rolagem dupla evidente, o iframe preenche a área disponível, e o restante do app (header, abas, AppRail) continua com o estilo normal (sem vazamento de CSS do Argus).
    6. Navegar pelas seções internas do portal Argus (Visão Geral, DF-e's, Split Payment, etc.) para confirmar que o mock funciona isolado.
    7. Acessar diretamente /argus-portal-fiscal.html no navegador para confirmar que o asset estático é servido.
  </how-to-verify>
  <resume-signal>Digite "approved" se o portal carrega corretamente e o estilo do app permanece intacto, ou descreva os problemas (ex.: altura do iframe, vazamento de CSS, posição na navegação).</resume-signal>
</task>

</tasks>

<verification>
- `frontend/public/argus-portal-fiscal.html` existe e é documento HTML completo.
- `cd frontend && npx tsc -p tsconfig.app.json --noEmit` passa sem erros.
- Rota `/argus` registrada e ArgusPortal importado em App.tsx.
- Módulo `argus` em navigation.ts entre `malha` e `config`; `getActiveModule` reconhece `/argus`.
- Nenhuma alteração em backend, migrations ou handlers Go.
</verification>

<success_criteria>
- Acessar `/argus` no app autenticado renderiza o portal Argus em iframe ocupando a área de conteúdo, sem vazamento de estilos.
- Novo módulo "Portal Fiscal CBS/IBS (Demo)" aparece na navegação logo após "Malha Fina".
- O HTML é mock 100% estático, sem chamadas a backend real, reproduzido fielmente do arquivo fornecido.
- Checkpoint humano aprovado.
</success_criteria>

<output>
Create `.planning/quick/260630-hjd-argus-portal-fiscal-demo/260630-hjd-SUMMARY.md` when done
</output>
