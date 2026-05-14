---
quick_id: 260514-mnw
slug: limpeza-base-dados
description: Criar funcionalidade Limpeza de Base de Dados granular em Configurações
date: 2026-05-14
---

# Quick Task: Limpeza de Base de Dados

## Goal
Substituir a página "Limpar Dados" por uma funcionalidade granular que permita ao admin selecionar quais tabelas (nfe_entradas, nfe_saidas, cte_entradas) e qual período (mes_ano) deseja excluir, com pré-visualização de contagens antes da confirmação.

## Must-haves
- [ ] `GET /api/admin/limpeza-base` retorna períodos disponíveis e contagens por tabela (filtrado por mes_ano opcional)
- [ ] `DELETE /api/admin/limpeza-base` aceita `{tabelas, mes_ano}`, valida allowlist, executa DELETEs com escopo company_id
- [ ] Admin-only via `withAuth(..., "admin")` e double-check de claims dentro do handler
- [ ] Quando todas as 3 tabelas E mes_ano vazio → reseta `erp_bridge_config.reset_tracker=true`
- [ ] Página `LimparDadosApuracao.tsx` reescrita com checkboxes de tabelas, dropdown de período, preview com contagens e confirmação inline
- [ ] Navegação atualizada: label "Limpar Dados" → "Limpeza de Base" (mesma rota, mesmas flags)
- [ ] Nenhum header `Authorization` manual no frontend (interceptor cuida)

## Tasks

### TASK-1: Backend — criar `backend/handlers/limpeza_base.go`

**Arquivo:** `backend/handlers/limpeza_base.go` (NEW)

**Ação:**
Criar `LimpezaBaseHandler(db *sql.DB) http.HandlerFunc` com roteamento por `r.Method`:

- Header inicial: `w.Header().Set("Content-Type", "application/json")`
- Role check: extrair `claims := r.Context().Value(ClaimsKey).(jwt.MapClaims)`, validar `claims["role"].(string) == "admin"`, senão `jsonErr(w, 403, "acesso negado")`
- Resolver `userID := GetUserIDFromContext(r)` e `companyID, err := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))`
- Allowlist constante: `var tabelasValidas = map[string]bool{"nfe_entradas": true, "nfe_saidas": true, "cte_entradas": true}`

**GET branch** (`r.URL.Query().Get("mes_ano")`):
- Query de períodos: `SELECT DISTINCT mes_ano FROM (SELECT mes_ano FROM nfe_entradas WHERE company_id=$1 UNION SELECT mes_ano FROM nfe_saidas WHERE company_id=$1 UNION SELECT mes_ano FROM cte_entradas WHERE company_id=$1) t WHERE mes_ano IS NOT NULL AND mes_ano <> '' ORDER BY SPLIT_PART(mes_ano,'/',2) DESC, SPLIT_PART(mes_ano,'/',1) DESC`
- Para cada tabela na allowlist, `SELECT COUNT(*) FROM <tbl> WHERE company_id=$1` (+ `AND mes_ano=$2` se mes_ano não vazio)
- Resposta: `{"periodos": [...], "contagens": {"nfe_entradas": N, "nfe_saidas": N, "cte_entradas": N}}`

**DELETE branch**:
- `json.NewDecoder(r.Body).Decode(&req)` para `{Tabelas []string, MesAno string}` (json tags `tabelas`, `mes_ano`)
- Validar `len(req.Tabelas) > 0` → `jsonErr(w, 400, "tabelas obrigatórias")`
- Para cada t em req.Tabelas, se `!tabelasValidas[t]` → `jsonErr(w, 400, "tabela inválida: "+t)`
- `tx, _ := db.Begin()`; `defer tx.Rollback()`; loop: se mes_ano vazio `DELETE FROM <t> WHERE company_id=$1` senão `DELETE FROM <t> WHERE company_id=$1 AND mes_ano=$2`; coletar `RowsAffected()` em `totais`
- Se `len(req.Tabelas) == 3 && req.MesAno == ""`: upsert em `erp_bridge_config` setando `reset_tracker=true` (mesmo SQL usado em `admin.go::LimparDadosApuracaoHandler`); marcar `resetTracker=true`
- `tx.Commit()`
- Resposta: `{"message": "limpeza concluída", "totais": totais, "reset_tracker": resetTracker}`
- Usar `sanitizeDBErr(w, 500, "erro ao limpar", err, "[LimpezaBase]")` para erros de DB
- Default branch: `jsonErr(w, 405, "método não permitido")`

**Verify:**
- `cd backend && go build ./...` compila sem erros
- `grep -n "LimpezaBaseHandler" backend/handlers/limpeza_base.go` retorna a definição
- `grep -n "tabelasValidas" backend/handlers/limpeza_base.go` confirma allowlist

**Done:**
Handler compila, valida role admin, resolve company_id via tenant helper, allowlist bloqueia tabelas não permitidas, GET retorna períodos+contagens, DELETE remove apenas linhas do company atual e dispara reset_tracker quando aplicável.

---

### TASK-2: Backend — registrar rota em `backend/main.go`

**Arquivo:** `backend/main.go`

**Ação:**
Adicionar após a linha 316 (logo abaixo da rota `/api/admin/limpar-apuracao`):

```
http.HandleFunc("/api/admin/limpeza-base", withAuth(handlers.LimpezaBaseHandler, "admin"))
```

Não remover a rota antiga `/api/admin/limpar-apuracao` neste quick (mantém compatibilidade para qualquer chamada legada até que seja removida em milestone futuro).

**Verify:**
- `cd backend && go build ./...` compila
- `grep -n "limpeza-base" backend/main.go` mostra a nova rota registrada

**Done:**
Rota `/api/admin/limpeza-base` registrada com middleware `withAuth(..., "admin")` e binário compila.

---

### TASK-3: Frontend — reescrever `frontend/src/pages/LimparDadosApuracao.tsx`

**Arquivo:** `frontend/src/pages/LimparDadosApuracao.tsx` (REWRITE)

**Ação:**
Substituir o conteúdo completo por uma nova implementação React:

- Imports:
  - `import { useState, useEffect } from 'react'`
  - `import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'`
  - `import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'`
  - `import { Button } from '@/components/ui/button'`
- Tipos locais:
  - `type Contagens = { nfe_entradas: number; nfe_saidas: number; cte_entradas: number }`
  - `const TABELAS = [{ id: 'nfe_entradas', label: 'NF-e Entradas' }, { id: 'nfe_saidas', label: 'NF-e Saídas' }, { id: 'cte_entradas', label: 'CT-e Entradas' }] as const`
- State:
  - `periodos: string[]` (default `[]`)
  - `mesSelecionado: string` (default `''`)
  - `tabelasSelecionadas: string[]` (default `['nfe_entradas','nfe_saidas','cte_entradas']`)
  - `preview: Contagens | null`
  - `loadingPreview: boolean`
  - `executing: boolean`
  - `resultado: Record<string, number> | null`
  - `erro: string | null`
  - `confirmando: boolean`
- `useEffect(() => { fetch('/api/admin/limpeza-base').then(r=>r.json()).then(d=> setPeriodos(d.periodos||[])).catch(e=> setErro(String(e))) }, [])`
- `handlePreview()`: GET `/api/admin/limpeza-base?mes_ano=${encodeURIComponent(mesSelecionado)}` (ou sem query quando vazio), setar `preview` com `d.contagens` (a renderização filtra para `tabelasSelecionadas`)
- `handleExecutar()`: `fetch('/api/admin/limpeza-base', { method: 'DELETE', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ tabelas: tabelasSelecionadas, mes_ano: mesSelecionado }) })` → setar `resultado` com `d.totais`; resetar `preview` e `confirmando`
- Toggle de tabela (checkbox): adicionar/remover de `tabelasSelecionadas`
- Botão "Selecionar todas" (link button) seta as 3 ids
- **Layout** (`<div className="max-w-2xl mx-auto p-6 space-y-4">`):
  1. `<div><h1 className="text-2xl font-semibold">Limpeza de Base de Dados</h1><p className="text-sm text-muted-foreground">Remova documentos fiscais de forma granular por tabela e período.</p></div>`
  2. Card "Tabelas" com 3 checkboxes (`<input type="checkbox">`) + botão linkado "Selecionar todas"
  3. Card "Período" com `<Select value={mesSelecionado} onValueChange={setMesSelecionado}>` cujo primeiro `<SelectItem value="">Todos os períodos</SelectItem>` seguido dos `periodos.map(p => <SelectItem key={p} value={p}>{p}</SelectItem>)`
  4. `<Button variant="outline" onClick={handlePreview} disabled={tabelasSelecionadas.length===0 || loadingPreview}>Pré-visualizar</Button>`
  5. Quando `preview` existe: card com borda/bg ambar (`border-amber-300 bg-amber-50`) listando linhas `{label}: {count}` somente para tabelas selecionadas + linha "Total" (soma). Se `!confirmando`, mostrar `<Button variant="destructive" onClick={()=>setConfirmando(true)}>Confirmar Exclusão</Button>`. Quando `confirmando`, mostrar texto de aviso + dois botões: `<Button variant="destructive" onClick={handleExecutar} disabled={executing}>Sim, remover</Button>` e `<Button variant="ghost" onClick={()=>setConfirmando(false)}>Cancelar</Button>`
  6. Quando `resultado` existe: card verde (`border-green-300 bg-green-50`) listando `{tabela}: {n} registros removidos` para cada chave de `resultado`
  7. Quando `erro`: `<p className="text-sm text-destructive">{erro}</p>`
- NÃO adicionar headers `Authorization`/`X-Company-ID` — o interceptor de `AuthContext` injeta automaticamente
- Default export: `export default function LimparDadosApuracao()`

**Verify:**
- `cd frontend && npx tsc -p tsconfig.app.json --noEmit` passa sem erros
- `grep -n "/api/admin/limpeza-base" frontend/src/pages/LimparDadosApuracao.tsx` retorna pelo menos 2 ocorrências (GET e DELETE)
- `grep -n "Authorization" frontend/src/pages/LimparDadosApuracao.tsx` retorna vazio

**Done:**
Página carrega lista de períodos, permite selecionar tabelas + período, mostra preview com contagens, exige confirmação inline antes de DELETE, exibe resultado com totais; sem manipulação manual de auth headers; TypeScript compila.

---

### TASK-4: Navegação — renomear entrada em `frontend/src/lib/navigation.ts`

**Arquivo:** `frontend/src/lib/navigation.ts`

**Ação:**
Linha 99: substituir `label: 'Limpar Dados'` por `label: 'Limpeza de Base'`. Manter `path: '/config/limpar-dados'`, `danger: true`, `adminOnly: true` inalterados.

**Verify:**
- `grep -n "Limpeza de Base" frontend/src/lib/navigation.ts` retorna a linha 99
- `grep -n "Limpar Dados" frontend/src/lib/navigation.ts` retorna vazio

**Done:**
Sidebar exibe "Limpeza de Base" no módulo Configurações, rota permanece `/config/limpar-dados`, flags de permissão preservadas.

---

## Verification (end-to-end)

1. `cd backend && go build ./...` — backend compila
2. `cd frontend && npx tsc -p tsconfig.app.json --noEmit` — frontend compila
3. Subir backend localmente, logar como admin, acessar `/config/limpar-dados`:
   - Dropdown de períodos populado com meses existentes
   - Marcar apenas `nfe_entradas`, escolher "05/2026", clicar "Pré-visualizar" → preview mostra contagem só de NF-e Entradas
   - Confirmar exclusão → card verde mostra `nfe_entradas: N removidos`
   - Verificar via `psql` que `nfe_saidas` do mesmo período permanece intacta
4. Reabrir página, selecionar todas as 3 tabelas + "Todos os períodos" → executar → confirmar via DB que `erp_bridge_config.reset_tracker = true`
5. Login com role não-admin → rota `/api/admin/limpeza-base` responde 403

## Success Criteria
- Admin consegue deletar 1, 2 ou 3 tabelas opcionalmente filtrado por mes_ano
- DELETE jamais afeta linhas de outro company_id (todas as queries usam `WHERE company_id=$1`)
- Allowlist no backend impede injeção de nomes de tabela arbitrários
- Reset do tracker ERP só dispara quando limpeza é total (3 tabelas + todos os períodos)
- Sidebar mostra "Limpeza de Base"; rota antiga `/config/limpar-dados` continua funcionando
