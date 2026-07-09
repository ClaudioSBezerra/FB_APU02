# Arquitetura — Frontend React (SPA)

**Parte:** `frontend/`  
**Tipo:** Single Page Application — React 18.3, TypeScript 5.2, Vite 5.2  
**Padrão:** Componentes funcionais + Context API + fetch monkey-patch + shadcn/ui

---

## Sumário Executivo

O frontend é uma SPA React construída com Vite e TypeScript em modo strict. A navegação é gerenciada pelo React Router DOM 6. O estado global é mantido por dois contextos: `AuthContext` (sessão JWT + interceptor de fetch global) e `FilialContext` (seleção de filiais).

O acesso à API usa `window.fetch` nativo, com um interceptor global instalado pelo `AuthContext` que injeta automaticamente os headers `Authorization` e `X-Company-ID` em todas as chamadas `/api/*`. Não é necessário — e não se deve — adicionar esses headers manualmente nas páginas.

O design system segue o padrão **shadcn/ui**: componentes Radix UI + Tailwind CSS + Class Variance Authority (CVA), com suporte a dark mode via `class` no elemento root.

---

## Stack

| Categoria | Tecnologia | Versão |
|-----------|-----------|--------|
| Framework | React | 18.3.1 |
| Linguagem | TypeScript | 5.2.2 |
| Bundler | Vite + SWC | 5.2.0 / 3.5.0 |
| Roteamento | React Router DOM | 6.22.3 |
| Data fetching | TanStack React Query | 5.90.20 |
| Formulários | React Hook Form + Zod | 7.71.1 / 4.3.6 |
| UI base | Radix UI (25 pacotes) | — |
| Estilização | Tailwind CSS | 3.4.3 |
| Ícones | Lucide React | 0.363.0 |
| Gráficos | Recharts | 3.7.0 |
| Toast | Sonner | 2.0.7 |
| Excel export | xlsx | 0.18.5 |
| Datas | date-fns | 4.1.0 |

---

## Padrão Arquitetural

### Estrutura de Componentes

```
App.tsx                           ← Router root + guards
├── ProtectedRoute                ← Redireciona para /login se não autenticado
│   └── AppLayout                 ← Header + AppRail + Tabs + conteúdo
│       └── [páginas protegidas]
├── AdminRoute                    ← Adicional: role === 'admin'
│   └── [páginas admin]
└── [rotas públicas]              ← Login, Register, Forgot/Reset Password
```

### Injeção de Headers (fetch interceptor)

```typescript
// AuthContext.tsx — monkey-patch global instalado no mount
const originalFetch = window.fetch
window.fetch = (url, options) => {
    if (url.toString().startsWith('/api')) {
        options = {
            ...options,
            headers: {
                ...options?.headers,
                'Authorization': `Bearer ${tokenRef.current}`,
                'X-Company-ID': companyIdRef.current,
            }
        }
    }
    // Auto-logout em 401 de /api/* (exceto /api/auth/)
    return originalFetch(url, options).then(res => {
        if (res.status === 401 && url.toString().startsWith('/api/') && !url.toString().includes('/api/auth/')) {
            logout()
        }
        return res
    })
}
```

---

## Roteamento

### Rotas Públicas (sem autenticação)

| Path | Componente |
|------|-----------|
| `/login` | `Login` |
| `/register` | `Register` |
| `/forgot-password` | `ForgotPassword` |
| `/reset-senha` | `ResetPassword` |

### Rotas Protegidas (autenticação obrigatória)

Todas dentro de `AppLayout` sob `ProtectedRoute`. Redirecionamento padrão `/` → `/rfb/gestao-creditos`.

| Módulo | Paths | Admin only |
|--------|-------|-----------|
| Painel | `/painel/resumo-fiscal`, `/apuracao/creditos-perdidos` | Não |
| Notas | `/apuracao/entrada/notas`, `/apuracao/saida/notas`, `/apuracao/cte-entrada/notas` | Não |
| Importações | `/importacoes/nfe-entrada`, `nfe-saida`, `cte-entrada`, `pagamentos-fornecedores` | Não |
| Importações (admin) | `/importacoes/erp-bridge`, `/importacoes/erp-bridge/logs` | **Sim** |
| CGIBS | `/cgibs/apuracao-ibs`, `/cgibs/apuracao`, `/cgibs/debitos` | Não |
| CGIBS (admin) | `/cgibs/credenciais` | **Sim** |
| RFB | `/rfb/gestao-creditos`, `/rfb/apuracao`, `/rfb/debitos`, `/rfb/creditos-cbs`, `/rfb/pagamentos-fornecedores` | Não |
| Malha Fina | `/malha-fina/nfe-entradas`, `nfe-saidas`, `cte` | Não |
| Argus (Demo) | `/argus` | Não |
| Config | `/config/aliquotas`, `cfop`, `forn-simples`, `apelidos-filiais`, `gestores`, `ambiente` | Não |
| Config (admin) | `/config/usuarios`, `user-activity`, `limpar-dados`, `erp-bridge` | **Sim** |
| Credenciais | `/rfb/credenciais` | Não |

---

## Estado Global

### AuthContext (`src/contexts/AuthContext.tsx`)

**Estado gerenciado:**

```typescript
interface AuthState {
    user: { id: string; email: string; full_name: string; role?: string; trial_ends_at?: string } | null
    token: string | null
    environment: { id: string; name: string } | null
    group: { id: string; name: string } | null
    company: string | null        // nome da empresa ativa
    companyId: string | null      // UUID da empresa ativa
    cnpj: string | null           // CNPJ da empresa ativa
    loading: boolean
}
```

**Persistência:**
- `sessionStorage`: `token`, `user`, `environment`, `group`, `company`, `companyId`, `cnpj`
- `localStorage`: `pref_company_{userId}` — preferência de empresa (sobrevive ao logout)

**Ações principais:**
- `login(data)` — hidrata estado + sessionStorage + restaura preferência de empresa
- `logout()` — limpa sessionStorage (preserva `pref_company_*` em localStorage) + redireciona
- `switchCompany(id, name, cnpj)` — atualiza estado + localStorage + chama `PATCH /api/user/preferred-company` + reload da página

### FilialContext (`src/contexts/FilialContext.tsx`)

**Estado gerenciado:**

```typescript
interface FilialState {
    filiais: Filial[]               // lista completa da empresa ativa
    selectedFiliais: string[]       // CNPJs selecionados ([] = todas)
    loadingFiliais: boolean
}
```

**Comportamento:**
- Busca `GET /api/filiais` quando `companyId` muda
- `selectedFiliais` vazio = todas as filiais selecionadas (`isSelected()` sempre retorna `true`)
- Persistido em `localStorage` com chave `filiais_selecionadas_{companyId}`

---

## Módulos de Navegação

Definidos em `src/lib/navigation.ts` — **fonte única da verdade** para sidebar e abas:

| ID | Label | Abas ativas |
|----|-------|------------|
| `painel` | Painel | Resumo Fiscal, Créditos em Risco |
| `notas` | Notas Importadas | NF-e Entradas, NF-e Saídas, CT-e Entradas |
| `importacoes` | Importações | XML Entrada/Saída/CT-e, CSV Pagamentos (+ admin: ERP Bridge) |
| `cgibs` | CGIBS - Apuração Assistida IBS | Apuração IBS, Importar Movimento, Débitos IBS |
| `rfb` | Receita Federal - Apuração Assistida | Gestão CBS, Movimento, Débitos, Créditos, Pgtos Fornecedores |
| `malha` | Malha Fina | NF-e Entradas, NF-e Saídas, CT-e Entradas |
| `argus` | Portal Fiscal CBS/IBS (Demo) | Portal Argus |
| `config` | Configurações | Alíquotas, CFOP, Simples, Apelidos, Gestores, Ambiente (+ admin: demais) |

Resolução do módulo ativo: `getActiveModule(pathname: string): string` — cadeia de `if` com `startsWith`.

---

## Busca de Dados

### Padrão 1 — fetch nativo (predominante)

```typescript
// Padrão em páginas mais antigas
useEffect(() => {
    fetch('/api/nfe-entradas')
        .then(r => r.ok ? r.json() : Promise.reject(r))
        .then(setData)
        .catch(setError)
}, [companyId])
```

Headers injetados automaticamente pelo interceptor no `AuthContext`.

### Padrão 2 — TanStack React Query (páginas recentes)

```typescript
const { data, isLoading, error } = useQuery({
    queryKey: ['nfe-entradas', companyId, mes],
    queryFn: async () => {
        const r = await fetch(`/api/nfe-entradas?mes=${mes}`)
        if (!r.ok) throw new Error(await r.text())
        return r.json()
    }
})

const mutation = useMutation({
    mutationFn: (payload) => fetch('/api/...', { method: 'POST', body: JSON.stringify(payload) }),
    onSuccess: () => { toast.success('OK'); queryClient.invalidateQueries({ queryKey: ['chave'] }) },
    onError: (e: Error) => toast.error(e.message)
})
```

**Páginas que usam TanStack Query:** `AdminUsers`, `ConsultaCTesEntradas`, `ConsultaNFeSaidas`, `ConsultaNFesEntradas`, `ERPBridgeConfig`, `ERPBridgeCredenciais`, `ERPBridgeLogs`, `MalhaFinaPanel`, `MalhaFinaResumoGeral`, `RFBDebitos`, `UserActivity`.

---

## Design System

### shadcn/ui Pattern

Componentes em `src/components/ui/` seguem o padrão:
```typescript
const buttonVariants = cva(
    "base-classes",
    {
        variants: {
            variant: { default: "...", destructive: "...", outline: "..." },
            size: { default: "...", sm: "...", lg: "..." }
        },
        defaultVariants: { variant: "default", size: "default" }
    }
)
```

### Cores Semânticas Fiscais (Tailwind)

```javascript
// tailwind.config.js — cores de domínio fiscal
'pis-cofins': { DEFAULT: '#...', foreground: '#...' },  // PIS/COFINS (legado)
'ibs-cbs':    { DEFAULT: '#...', foreground: '#...' },  // IBS/CBS (Reforma Tributária)
'positive':   { DEFAULT: '#...' },                       // valores positivos (créditos)
'negative':   { DEFAULT: '#...' },                       // valores negativos (débitos)
```

### Dark Mode

- Estratégia: `darkMode: ['class']` no Tailwind
- `next-themes v0.4.6` gerencia a classe `dark` no `<html>`
- Todos os componentes shadcn/ui suportam dark mode via variáveis CSS

---

## Formulários

| Abordagem | Onde usar |
|-----------|-----------|
| React Hook Form + Zod + `<Form>` | Autenticação (Login, Register, ForgotPassword, ResetPassword) + tabelas de referência (TabelaCFOP) |
| `useState` + submit manual | Maioria das páginas de listagem e filtros |

---

## Toast Notifications

```typescript
import { toast } from 'sonner'

toast.success('Operação realizada com sucesso')
toast.error(error.message || 'Erro inesperado')
```

`<Toaster />` instalado em `AppLayout` no `App.tsx`.

---

## Export Excel

```typescript
import { exportToExcel } from '@/lib/exportToExcel'

// Uso recomendado:
exportToExcel(data, 'relatorio-debitos', 'Débitos CBS')
```

Helper centralizado em `src/lib/exportToExcel.ts`. Algumas páginas usam `xlsx` diretamente — padronizar para o helper.

---

## Build e Deploy

### Desenvolvimento

```bash
cd frontend && npm run dev   # Porta 3000, proxy /api → localhost:8081
```

### Produção

```bash
cd frontend && npm run build  # Saída em dist/
```

O Dockerfile do frontend usa multi-stage: `node:18-alpine` para build → `nginx:alpine` para servir os arquivos estáticos.

### Path Aliases

```json
// tsconfig.app.json
"paths": { "@/*": ["./src/*"] }
// Vite: resolve.alias: { "@": "./src" }
```

---

## Rastreamento de Atividade

`useRouteActivityLogger()` em `AppLayout` rastreia permanência por rota:
- Envia `POST /api/activity/log { module, duration_seconds }` quando o usuário muda de rota
- Condição: duração ≥ 3 segundos e módulo ≠ `painel`
- Usa `keepAlive: true` no `fetch` para garantir envio no `beforeunload`
