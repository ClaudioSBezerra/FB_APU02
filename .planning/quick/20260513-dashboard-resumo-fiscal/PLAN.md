---
quick_id: 20260513-dashboard-resumo-fiscal
description: "Dashboard de Resumo Fiscal — Cockpit IBS/CBS com KPI cards, gráfico de documentos por tipo e painel de créditos/débitos, filtrado por mês/ano."
type: quick
files_modified:
  - backend/handlers/dashboard.go
  - backend/main.go
  - frontend/src/pages/DashboardResumo.tsx
  - frontend/src/App.tsx
  - frontend/src/lib/navigation.ts
---

<objective>
Adicionar página "Dashboard de Resumo Fiscal" no módulo `painel` modelada sobre o "Cockpit Tax True" da ROIT: 5 KPI cards (documentos, créditos IBS/CBS, débitos IBS/CBS, créditos a apropriar, alíquota efetiva), gráfico horizontal de documentos por tipo com toggle Quantidade/Valores, e painel de resumo créditos/débitos com saldo a recolher. Filtrado por mês/ano selecionável.

Purpose: Dar ao usuário visão consolidada da apuração IBS/CBS em um único lugar.
Output: Endpoint `GET /api/dashboard/resumo` + página React `/painel/resumo-fiscal` + nova tab no módulo `painel`.
</objective>

<context>
@CLAUDE.md
@backend/handlers/nfe_entradas.go
@backend/handlers/config.go
@backend/main.go
@frontend/src/contexts/AuthContext.tsx
@frontend/src/lib/navigation.ts
@frontend/src/lib/utils.ts
@frontend/src/App.tsx

<interfaces>
<!-- Contratos extraídos do código existente. O executor deve usar diretamente, sem explorar mais o codebase. -->

Backend (Go):
- `func GetEffectiveCompanyID(db *sql.DB, userID, headerCompanyID string) (string, error)` — em handlers/auth.go
- `func GetUserIDFromContext(r *http.Request) string` — em handlers/auth.go
- `func jsonErr(w http.ResponseWriter, status int, msg string, extra ...map[string]string)` — em handlers/config.go
- `func sanitizeDBErr(w http.ResponseWriter, status int, userMsg string, err error, prefix string)` — em handlers/config.go
- Factory pattern obrigatório: `func XYZHandler(db *sql.DB) http.HandlerFunc { return func(w, r) { ... } }`
- `withAuth` em main.go assinatura: `withAuth(handlerFactory func(*sql.DB) http.HandlerFunc, role string) http.HandlerFunc` — passar `""` para "sem restrição de role, apenas autenticado".

Frontend (TS):
- `useAuth()` retorna `{ companyId: string | null, ... }` — de `@/contexts/AuthContext`
- `formatCurrency(value: number): string` — de `@/lib/utils`
- shadcn Card: `import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'`
- Interceptor global de `window.fetch` injeta `Authorization` e `X-Company-ID` automaticamente — NÃO setar headers manualmente.
- Recharts disponível em package.json: `import { BarChart, Bar, XAxis, YAxis, Tooltip, Legend, ResponsiveContainer } from 'recharts'`
- `modules.painel` em `navigation.ts` atualmente tem `tabs: []`.

Tabelas (PostgreSQL) — colunas IBS/CBS confirmadas:
- `nfe_entradas(company_id, mes_ano, v_nf, pis, cofins, icms, v_ibs, v_cbs, v_bc_ibs_cbs)`
- `nfe_saidas(company_id, mes_ano, v_nf, pis, cofins, icms, v_ibs, v_cbs, v_bc_ibs_cbs)`
- `cte_entradas(company_id, mes_ano, v_rec, pis, cofins, icms, v_ibs, v_cbs, v_bc_ibs_cbs)` — sem v_nf, usar v_rec.
- `mes_ano` formato `'YYYY-MM'` (string).

Layout de referência (ROIT "Cockpit Tax True"):
- 5 KPI cards: Total Documentos | Total Créditos | Total Débitos | Créditos a Apropriar | Alíquota Efetiva IBS
- Coluna esquerda: gráfico horizontal "Documentos por Tipo" com toggle Quantidade/Valores
- Coluna direita: tabela "Resumo de Créditos e Débitos" com IBS/CBS e Saldo a Recolher
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Criar handler backend/handlers/dashboard.go</name>
  <files>backend/handlers/dashboard.go</files>
  <action>
Criar `backend/handlers/dashboard.go` no pacote `handlers`. Exportar UMA função `DashboardResumoHandler(db *sql.DB) http.HandlerFunc`.

Comportamento:
1. `Content-Type: application/json`.
2. `userID` via `GetUserIDFromContext(r)` — se vazio, `jsonErr(w, 401, "Não autenticado")` e retornar.
3. `companyID` via `GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))` — em erro, `sanitizeDBErr(w, 500, "Erro ao identificar empresa", err, "[DashboardResumo]")`.
4. Param `mes` de `r.URL.Query().Get("mes")`. Validar com `regexp.MustCompile(`^\d{4}-\d{2}$`)` (var de pacote). Inválido ou vazio → `time.Now().Format("2006-01")`.
5. Executar 3 queries SEPARADAS via `db.QueryRow`:

```sql
-- nfe_entradas
SELECT
  COUNT(*),
  COALESCE(SUM(v_nf), 0),
  COALESCE(SUM(v_ibs), 0),
  COALESCE(SUM(v_cbs), 0),
  COALESCE(SUM(v_bc_ibs_cbs), 0)
FROM nfe_entradas
WHERE company_id = $1 AND mes_ano = $2

-- nfe_saidas (mesma estrutura)
SELECT
  COUNT(*),
  COALESCE(SUM(v_nf), 0),
  COALESCE(SUM(v_ibs), 0),
  COALESCE(SUM(v_cbs), 0),
  COALESCE(SUM(v_bc_ibs_cbs), 0)
FROM nfe_saidas
WHERE company_id = $1 AND mes_ano = $2

-- cte_entradas (v_rec em vez de v_nf)
SELECT
  COUNT(*),
  COALESCE(SUM(v_rec), 0),
  COALESCE(SUM(v_ibs), 0),
  COALESCE(SUM(v_cbs), 0),
  COALESCE(SUM(v_bc_ibs_cbs), 0)
FROM cte_entradas
WHERE company_id = $1 AND mes_ano = $2
```

6. Para cada query, Scan em: `var count int; var vNF, vIBS, vCBS, vBcIBS float64`. Em `err != nil`, `sanitizeDBErr` com `[DashboardResumo]` e retornar. Não tratar `sql.ErrNoRows` separadamente (COUNT sempre retorna linha).

7. Calcular campos derivados APÓS os 3 Scans:
```go
totalCreditosIBS := entIBS + cteIBS           // entradas + cte
totalCreditosCBS := entCBS + cteCBS
totalDebitosIBS  := saiIBS
totalDebitosCBS  := saiCBS
saldoIBS         := totalDebitosIBS - totalCreditosIBS
saldoCBS         := totalDebitosCBS - totalCreditosCBS

var aliquotaEfetivaIBS *float64  // nil = "--" no frontend
if saiVBcIBS > 0 {
    v := (totalDebitosIBS / saiVBcIBS) * 100
    aliquotaEfetivaIBS = &v
}
creditosApropriar := totalCreditosIBS + totalCreditosCBS - (totalDebitosIBS + totalDebitosCBS)
if creditosApropriar < 0 { creditosApropriar = 0 }
```

8. Montar struct inline:
```go
type blocoDoc struct {
    Count   int     `json:"count"`
    VNF     float64 `json:"v_nf"`
    VIBS    float64 `json:"v_ibs"`
    VCBS    float64 `json:"v_cbs"`
    VBcIBS  float64 `json:"v_bc_ibs_cbs"`
}
resp := struct {
    MesAno            string   `json:"mes_ano"`
    NfeEntradas       blocoDoc `json:"nfe_entradas"`
    NfeSaidas         blocoDoc `json:"nfe_saidas"`
    CteEntradas       blocoDoc `json:"cte_entradas"`
    TotalCreditosIBS  float64  `json:"total_creditos_ibs"`
    TotalCreditosCBS  float64  `json:"total_creditos_cbs"`
    TotalDebitosIBS   float64  `json:"total_debitos_ibs"`
    TotalDebitosCBS   float64  `json:"total_debitos_cbs"`
    SaldoIBS          float64  `json:"saldo_ibs"`
    SaldoCBS          float64  `json:"saldo_cbs"`
    CreditosApropriar float64  `json:"creditos_apropriar"`
    AliquotaEfetivaIBS *float64 `json:"aliquota_efetiva_ibs"`
}{...}
```

9. `json.NewEncoder(w).Encode(resp)` — sem status explícito (200 implícito).

Imports: `database/sql`, `encoding/json`, `net/http`, `regexp`, `time`. Bloco único stdlib.
  </action>
  <verify>
    <automated>cd /home/claudiobezerra/projetos/FB_APU02/backend &amp;&amp; go build ./... &amp;&amp; go vet ./handlers/...</automated>
  </verify>
  <done>Arquivo compila sem erros; `go vet` limpo; `DashboardResumoHandler` exportada.</done>
</task>

<task type="auto">
  <name>Task 2: Registrar rota em backend/main.go</name>
  <files>backend/main.go</files>
  <action>
Em `backend/main.go`, localizar a seção de rotas com `withAuth`. Adicionar exatamente UMA linha próxima às rotas de config:

```go
http.HandleFunc("/api/dashboard/resumo", withAuth(handlers.DashboardResumoHandler, ""))
```

Passar a referência da factory (`handlers.DashboardResumoHandler`), não a invocação (`handlers.DashboardResumoHandler(db)`). Não alterar outras rotas.
  </action>
  <verify>
    <automated>cd /home/claudiobezerra/projetos/FB_APU02/backend &amp;&amp; go build -o /tmp/fb_apu02_build ./... &amp;&amp; grep -c '"/api/dashboard/resumo"' main.go</automated>
  </verify>
  <done>Build bem-sucedido; `grep` retorna `1`.</done>
</task>

<task type="auto">
  <name>Task 3: Criar página frontend/src/pages/DashboardResumo.tsx</name>
  <files>frontend/src/pages/DashboardResumo.tsx</files>
  <action>
Criar `frontend/src/pages/DashboardResumo.tsx` — `export default function DashboardResumo()`.

**Interfaces locais:**
```typescript
interface BlocoDoc {
  count: number
  v_nf?: number
  v_rec?: number   // apenas cte_entradas
  v_ibs: number
  v_cbs: number
  v_bc_ibs_cbs: number
}

interface DashboardData {
  mes_ano: string
  nfe_entradas: BlocoDoc
  nfe_saidas: BlocoDoc
  cte_entradas: BlocoDoc
  total_creditos_ibs: number
  total_creditos_cbs: number
  total_debitos_ibs: number
  total_debitos_cbs: number
  saldo_ibs: number
  saldo_cbs: number
  creditos_apropriar: number
  aliquota_efetiva_ibs: number | null
}
```

**Estado:**
```typescript
const [mes, setMes] = useState<string>(new Date().toISOString().slice(0, 7))
const [data, setData] = useState<DashboardData | null>(null)
const [loading, setLoading] = useState(false)
const [erro, setErro] = useState<string | null>(null)
const [chartMode, setChartMode] = useState<'quantidade' | 'valores'>('quantidade')
const { companyId } = useAuth()
```

**useEffect([mes, companyId]):** se `!companyId` retornar cedo. `setLoading(true); setErro(null)`. `fetch(\`/api/dashboard/resumo?mes=${mes}\`)` → se `!res.ok` → parse erro → `setErro(...)`. Em sucesso `setData(json)`. Em catch `setErro(e.message)`. finally `setLoading(false)`.

**Render — estrutura completa:**

```tsx
<div className="p-6 space-y-6">
  {/* Header */}
  <div className="flex items-center justify-between">
    <div>
      <h1 className="text-2xl font-bold">Resumo Fiscal</h1>
      <p className="text-muted-foreground text-sm">Visão consolidada dos documentos fiscais e apuração IBS/CBS</p>
    </div>
    <input
      type="month"
      value={mes}
      onChange={e => setMes(e.target.value)}
      className="border rounded px-3 py-2 text-sm"
    />
  </div>

  {loading && <p className="text-muted-foreground">Carregando...</p>}
  {erro && <p className="text-red-600">{erro}</p>}

  {data && (
    <>
      {/* KPI Cards — 5 cards, grid cols responsive */}
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-4">
        {/* Card 1: Total de Documentos */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Total de Documentos</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">
              {(data.nfe_entradas.count + data.nfe_saidas.count + data.cte_entradas.count).toLocaleString('pt-BR')}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              Entrada: {(data.nfe_entradas.count + data.cte_entradas.count).toLocaleString('pt-BR')} | Saída: {data.nfe_saidas.count.toLocaleString('pt-BR')}
            </p>
          </CardContent>
        </Card>

        {/* Card 2: Total de Créditos */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Total de Créditos</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{formatCurrency(data.total_creditos_ibs + data.total_creditos_cbs)}</p>
            <p className="text-xs text-muted-foreground mt-1">
              IBS: {formatCurrency(data.total_creditos_ibs)} | CBS: {formatCurrency(data.total_creditos_cbs)}
            </p>
          </CardContent>
        </Card>

        {/* Card 3: Total de Débitos */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Total de Débitos</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{formatCurrency(data.total_debitos_ibs + data.total_debitos_cbs)}</p>
            <p className="text-xs text-muted-foreground mt-1">
              IBS: {formatCurrency(data.total_debitos_ibs)} | CBS: {formatCurrency(data.total_debitos_cbs)}
            </p>
          </CardContent>
        </Card>

        {/* Card 4: Créditos a Apropriar */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Créditos a Apropriar</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{formatCurrency(data.creditos_apropriar)}</p>
          </CardContent>
        </Card>

        {/* Card 5: Alíquota Efetiva IBS */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Alíquota Efetiva IBS</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">
              {data.aliquota_efetiva_ibs !== null ? `${data.aliquota_efetiva_ibs.toFixed(2)}%` : '--'}
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Bottom section: Chart + Summary */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Gráfico de Documentos por Tipo */}
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-base">Documentos por Tipo</CardTitle>
              <div className="flex gap-2">
                <button
                  onClick={() => setChartMode('quantidade')}
                  className={`text-xs px-3 py-1 rounded ${chartMode === 'quantidade' ? 'bg-primary text-primary-foreground' : 'border'}`}
                >Quantidade</button>
                <button
                  onClick={() => setChartMode('valores')}
                  className={`text-xs px-3 py-1 rounded ${chartMode === 'valores' ? 'bg-primary text-primary-foreground' : 'border'}`}
                >Valores</button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={200}>
              <BarChart
                layout="vertical"
                data={[
                  {
                    tipo: 'NF-e Entradas',
                    entrada: chartMode === 'quantidade' ? data.nfe_entradas.count : (data.nfe_entradas.v_nf ?? 0),
                    saida: 0,
                  },
                  {
                    tipo: 'NF-e Saídas',
                    entrada: 0,
                    saida: chartMode === 'quantidade' ? data.nfe_saidas.count : (data.nfe_saidas.v_nf ?? 0),
                  },
                  {
                    tipo: 'CT-e Entradas',
                    entrada: chartMode === 'quantidade' ? data.cte_entradas.count : (data.cte_entradas.v_rec ?? 0),
                    saida: 0,
                  },
                ]}
                margin={{ top: 0, right: 20, left: 80, bottom: 0 }}
              >
                <XAxis type="number" tick={{ fontSize: 11 }} />
                <YAxis type="category" dataKey="tipo" tick={{ fontSize: 11 }} width={80} />
                <Tooltip
                  formatter={(value: number) =>
                    chartMode === 'valores' ? formatCurrency(value) : value.toLocaleString('pt-BR')
                  }
                />
                <Legend wrapperStyle={{ fontSize: 11 }} />
                <Bar dataKey="entrada" name="Entrada" fill="#22c55e" radius={[0, 4, 4, 0]} />
                <Bar dataKey="saida" name="Saída" fill="#ef4444" radius={[0, 4, 4, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        {/* Resumo de Créditos e Débitos */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Resumo de Créditos e Débitos</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="grid grid-cols-3 text-xs font-medium text-muted-foreground border-b pb-2">
              <span>Item</span><span className="text-right">IBS</span><span className="text-right">CBS</span>
            </div>
            {[
              { label: 'Débitos', ibs: data.total_debitos_ibs, cbs: data.total_debitos_cbs },
              { label: 'Créditos', ibs: data.total_creditos_ibs, cbs: data.total_creditos_cbs },
              { label: 'Saldo a Recolher', ibs: data.saldo_ibs, cbs: data.saldo_cbs },
            ].map(row => (
              <div key={row.label} className="grid grid-cols-3 text-sm py-1 border-b last:border-0">
                <span className="font-medium">{row.label}</span>
                <span className="text-right">{formatCurrency(row.ibs)}</span>
                <span className="text-right">{formatCurrency(row.cbs)}</span>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </>
  )}
</div>
```

**Imports obrigatórios:**
```typescript
import { useState, useEffect } from 'react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, Legend, ResponsiveContainer } from 'recharts'
import { useAuth } from '@/contexts/AuthContext'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatCurrency } from '@/lib/utils'
```

Não usar `localStorage`. Não setar headers manualmente no fetch.
  </action>
  <verify>
    <automated>cd /home/claudiobezerra/projetos/FB_APU02/frontend &amp;&amp; npx tsc --noEmit -p tsconfig.app.json</automated>
  </verify>
  <done>TypeScript compila sem erros; arquivo exporta default `DashboardResumo`; nenhuma referência a `localStorage`.</done>
</task>

<task type="auto">
  <name>Task 4: Registrar rota em App.tsx + adicionar tab em navigation.ts</name>
  <files>frontend/src/App.tsx, frontend/src/lib/navigation.ts</files>
  <action>
**Em `frontend/src/lib/navigation.ts`:** Atualizar o módulo `painel` para incluir a nova tab. Trocar `tabs: []` por:
```typescript
tabs: [
  { label: 'Resumo Fiscal', path: '/painel/resumo-fiscal' },
],
```
Manter o resto do arquivo intacto.

**Em `frontend/src/App.tsx`:**
1. Adicionar import no bloco de páginas (manter ordem coerente):
   ```typescript
   import DashboardResumo from './pages/DashboardResumo'
   ```
2. Dentro do bloco `ProtectedRoute` / `<Routes>` existente, adicionar:
   ```tsx
   <Route path="/painel/resumo-fiscal" element={<DashboardResumo />} />
   ```
3. Não remover nem reordenar rotas existentes.
  </action>
  <verify>
    <automated>cd /home/claudiobezerra/projetos/FB_APU02/frontend &amp;&amp; npx tsc --noEmit -p tsconfig.app.json &amp;&amp; grep -c "/painel/resumo-fiscal" src/App.tsx src/lib/navigation.ts</automated>
  </verify>
  <done>TS compila; `grep` mostra `2` ocorrências total de `/painel/resumo-fiscal`; `DashboardResumo` importado em App.tsx.</done>
</task>

</tasks>

<verification>
1. `cd backend && go build ./...` → sem erro.
2. `cd frontend && npx tsc --noEmit -p tsconfig.app.json` → sem erro.
3. Smoke test manual: autenticar, navegar para `/painel/resumo-fiscal`, verificar:
   - 5 KPI cards visíveis com valores ou zeros (não NaN, não crash).
   - Gráfico horizontal renderiza para NF-e e CT-e.
   - Toggle Quantidade/Valores recarrega o gráfico com valores formatados.
   - Painel "Resumo de Créditos e Débitos" mostra 3 linhas (Débitos/Créditos/Saldo).
   - Alterar o mês rebusca os dados.
</verification>

<success_criteria>
- `GET /api/dashboard/resumo?mes=YYYY-MM` retorna JSON com `mes_ano`, os 3 blocos de documentos (incluindo `v_ibs`, `v_cbs`, `v_bc_ibs_cbs`) e os campos derivados (`total_creditos_ibs`, `total_creditos_cbs`, `total_debitos_ibs`, `total_debitos_cbs`, `saldo_ibs`, `saldo_cbs`, `creditos_apropriar`, `aliquota_efetiva_ibs`).
- `aliquota_efetiva_ibs` é `null` quando `v_bc_ibs_cbs` da nfe_saidas = 0.
- Endpoint exige autenticação (sem token → 401 via `withAuth`).
- Multi-tenant garantido por `GetEffectiveCompanyID` + `WHERE company_id=$1` nas 3 queries.
- Erros de DB não expõem `err.Error()` ao cliente (`sanitizeDBErr` obrigatório).
- Página renderiza 5 KPI cards, gráfico horizontal e painel de créditos/débitos.
- Toggle Quantidade/Valores no gráfico funciona sem re-fetch.
- Tab "Resumo Fiscal" visível no módulo `painel`.
- Nenhum uso de `localStorage`. Nenhum header manual em `fetch`.
- `go build ./...` e `npx tsc --noEmit` limpos.
</success_criteria>

<output>
Criar `.planning/quick/20260513-dashboard-resumo-fiscal/SUMMARY.md` documentando: arquivos alterados, decisões (por que 3 queries separadas vs UNION, por que `aliquota_efetiva_ibs` é ponteiro/null, como o toggle de gráfico foi implementado sem re-fetch).
</output>
