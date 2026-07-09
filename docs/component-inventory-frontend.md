# Inventário de Componentes — Frontend React

**Total de páginas:** 44  
**Componentes compartilhados:** 9  
**Componentes UI (shadcn/ui):** 46  
**Contextos:** 2 (`AuthContext`, `FilialContext`)  
**Hooks customizados:** 1 (`use-mobile.tsx`)

---

## Páginas (`src/pages/`)

### Autenticação (Públicas)

| Componente | Path | Padrão de form |
|-----------|------|---------------|
| `Login.tsx` | `/login` | React Hook Form + Zod |
| `Register.tsx` | `/register` | React Hook Form + Zod |
| `ForgotPassword.tsx` | `/forgot-password` | React Hook Form + Zod |
| `ResetPassword.tsx` | `/reset-senha` | React Hook Form + Zod |

### Painel e Dashboard

| Componente | Path | Dados |
|-----------|------|-------|
| `DashboardResumo.tsx` | `/painel/resumo-fiscal` | `GET /api/dashboard/resumo` |
| `ApuracaoCredPerdidos.tsx` | `/apuracao/creditos-perdidos` | `GET /api/apuracao/creditos-perdidos` |

### Notas Importadas

| Componente | Path | Dados |
|-----------|------|-------|
| `ConsultaNFesEntradas.tsx` | `/apuracao/entrada/notas` | TanStack Query — `GET /api/nfe-entradas` |
| `ConsultaNFeSaidas.tsx` | `/apuracao/saida/notas` | TanStack Query — `GET /api/nfe-saidas` |
| `ConsultaCTesEntradas.tsx` | `/apuracao/cte-entrada/notas` | TanStack Query — `GET /api/cte-entradas` |

### Importações

| Componente | Path | Funcionalidade |
|-----------|------|---------------|
| `ImportarXMLsEntrada.tsx` | `/importacoes/nfe-entrada` | Upload multipart XML NF-e entradas |
| `ImportarXMLsSaida.tsx` | `/importacoes/nfe-saida` | Upload multipart XML NF-e saídas |
| `ImportarXMLsCTe.tsx` | `/importacoes/cte-entrada` | Upload multipart XML CT-e entradas |
| `ImportarPagamentosFornecedores.tsx` | `/importacoes/pagamentos-fornecedores` | Import CSV + listagem |
| `ERPBridgeConfig.tsx` | `/importacoes/erp-bridge` *(admin)* | TanStack Query — config + trigger manual |
| `ERPBridgeLogs.tsx` | `/importacoes/erp-bridge/logs` *(admin)* | TanStack Query — histórico de runs |

### CGIBS — Apuração IBS

| Componente | Path | Funcionalidade |
|-----------|------|---------------|
| `CGIBSPainel.tsx` | `/cgibs/apuracao-ibs` | Painel de solicitações IBS |
| `CGIBSApuracao.tsx` | `/cgibs/apuracao` | Importar movimento + solicitar apuração |
| `CGIBSDebitos.tsx` | `/cgibs/debitos` | Listagem de débitos IBS |
| `CGIBSCredentials.tsx` | `/cgibs/credenciais` *(admin)* | CRUD credenciais OAuth2 CGIBS |

### Receita Federal — Apuração CBS

| Componente | Path | Funcionalidade |
|-----------|------|---------------|
| `GestaoCredIBSCBS.tsx` | `/rfb/gestao-creditos` | Visão consolidada créditos IBS/CBS por período |
| `RFBApuracao.tsx` | `/rfb/apuracao` | Solicitar + gerenciar solicitações CBS |
| `RFBDebitos.tsx` | `/rfb/debitos` | TanStack Query — listagem de débitos CBS com filtros |
| `RFBCreditosCBS.tsx` | `/rfb/creditos-cbs` | Listagem de créditos CBS |
| `RFBPagamentosFornecedores.tsx` | `/rfb/pagamentos-fornecedores` | Conciliação pagamentos × CBS |
| `RFBCredentials.tsx` | `/rfb/credenciais` | Credenciais OAuth2 RFB por empresa |

### Malha Fina

| Componente | Path | Funcionalidade |
|-----------|------|---------------|
| `MalhaFinaNFeEntradas.tsx` | `/malha-fina/nfe-entradas` | Divergências NF-e entradas |
| `MalhaFinaNFeSaidas.tsx` | `/malha-fina/nfe-saidas` | Divergências NF-e saídas |
| `MalhaFinaCTe.tsx` | `/malha-fina/cte` | Divergências CT-e |
| `MalhaFinaPanel.tsx` | — | TanStack Query — painel consolidado de malha fina |
| `MalhaFinaResumoGeral.tsx` | — | TanStack Query — resumo geral |

### Portal Demo

| Componente | Path | Funcionalidade |
|-----------|------|---------------|
| `ArgusPortal.tsx` | `/argus` | iframe → `/argus-portal-fiscal.html` (mock estático) |

### Configurações

| Componente | Path | Admin |
|-----------|------|-------|
| `TabelaAliquotas.tsx` | `/config/aliquotas` | Não |
| `TabelaCFOP.tsx` | `/config/cfop` | Não — React Hook Form + Zod |
| `TabelaFornSimples.tsx` | `/config/forn-simples` | Não |
| `ApelidosFiliais.tsx` | `/config/apelidos-filiais` | Não |
| `Managers.tsx` | `/config/gestores` | Não |
| `GestaoAmbiente.tsx` | `/config/ambiente` | Não |
| `AdminUsers.tsx` | `/config/usuarios` | **Sim** — TanStack Query |
| `UserActivity.tsx` | `/config/user-activity` | **Sim** — TanStack Query |
| `LimparDadosApuracao.tsx` | `/config/limpar-dados` | **Sim** |
| `ERPBridgeCredenciais.tsx` | `/config/erp-bridge` | **Sim** — TanStack Query |

### Páginas Legadas / Sem Rota Ativa

| Componente | Status |
|-----------|--------|
| `Painel.tsx` | Legado — não utilizado nas rotas atuais |
| `PainelApuracaoCBS.tsx` | Rota `/rfb/apuracao-cbs` |
| `PainelApuracaoIBS.tsx` | Rota `/rfb/apuracao-ibs` |

---

## Componentes Compartilhados (`src/components/`)

| Componente | Propósito | Props notáveis |
|-----------|-----------|---------------|
| `AppRail.tsx` | Barra lateral de módulos — ícones navegáveis | Usa `getActiveModule(pathname)` para highlight ativo |
| `AppSidebar.tsx` | Sidebar alternativa (legado/experimental) | — |
| `CompanySwitcher.tsx` | Dropdown de troca de empresa | `compact?: boolean` — versão reduzida para header |
| `FilialSelector.tsx` | Seletor de filiais | Consome `FilialContext` |
| `FileUpload.tsx` | Upload de arquivos reutilizável | Suporte drag-and-drop + seleção múltipla |
| `UploadProgress.tsx` | Barra de progresso de upload | `progress: number`, `fileName: string` |
| `InsightCard.tsx` | Card de KPI/métrica para dashboards | `title`, `value`, `trend`, `icon` |
| `ParticipantList.tsx` | Lista de participantes de NF-e ou CT-e | Exibe emitente + destinatário |
| `Footer.tsx` | Rodapé padrão do app | — |

---

## Componentes UI (`src/components/ui/`) — shadcn/ui

Todos os 46 componentes seguem o padrão Radix UI + Tailwind + CVA:

| Categoria | Componentes |
|-----------|------------|
| **Layout** | `card`, `separator`, `resizable`, `scroll-area`, `aspect-ratio` |
| **Navegação** | `breadcrumb`, `navigation-menu`, `pagination`, `tabs`, `menubar` |
| **Formulários** | `form`, `input`, `textarea`, `label`, `select`, `checkbox`, `radio-group`, `switch`, `slider`, `input-otp` |
| **Feedback** | `alert`, `alert-dialog`, `sonner` (toast), `progress`, `skeleton` |
| **Overlays** | `dialog`, `drawer`, `sheet`, `popover`, `hover-card`, `context-menu`, `dropdown-menu` |
| **Dados** | `table`, `badge`, `avatar`, `collapsible`, `accordion`, `carousel` |
| **Ação** | `button`, `toggle`, `toggle-group`, `command` |
| **Visualização** | `chart`, `tooltip` |
| **Misc** | `sidebar` |

---

## Contextos Globais

### `AuthContext` (`src/contexts/AuthContext.tsx`)

**Exports:** `AuthProvider`, `useAuth`

```typescript
interface AuthContextType {
    user: User | null
    token: string | null
    companyId: string | null
    company: string | null
    cnpj: string | null
    environment: { id: string; name: string } | null
    group: { id: string; name: string } | null
    loading: boolean
    login: (data: LoginResponse) => void
    logout: () => void
    switchCompany: (id: string, name: string, cnpj: string) => void
}
```

### `FilialContext` (`src/contexts/FilialContext.tsx`)

**Exports:** `FilialProvider`, `useFiliais`

```typescript
interface FilialContextType {
    filiais: Filial[]
    selectedFiliais: string[]    // CNPJs selecionados ([] = todas)
    loadingFiliais: boolean
    toggleFilial: (cnpj: string) => void
    selectAll: () => void
    isSelected: (cnpj: string) => boolean
}
```

---

## Hooks Customizados

### `use-mobile.tsx`

```typescript
const isMobile = useIsMobile()  // true se viewport < 768px
```

---

## Utilitários (`src/lib/`)

| Arquivo | Exports | Uso |
|---------|---------|-----|
| `utils.ts` | `cn(...)`, `formatCurrency(value)` | Merge de classes Tailwind, formatação BRL |
| `formatFilial.ts` | `formatCNPJ`, `formatDocumento`, `formatFilialDisplay`, ... | Formatação fiscal |
| `navigation.ts` | `modules`, `getActiveModule(pathname)` | Navegação |
| `exportToExcel.ts` | `exportToExcel(data, fileName, sheetName?)` | Export Excel via xlsx |
| `storageKeys.ts` | `COMPANY_ID_KEY`, `TOKEN_KEY`, `COMPANY_PREF_KEY_PREFIX` | Chaves localStorage/sessionStorage |
| `logger.ts` | `logger.info()`, `logger.error()`, ... | Logging estruturado (uso inconsistente) |

---

## Assets Estáticos (`frontend/public/`)

| Arquivo | Descrição |
|---------|-----------|
| `argus-portal-fiscal.html` | Portal demo Argus (mock 100% estático, ~800 linhas, tema claro/escuro, 10 seções) |

Servido diretamente pela URL `/argus-portal-fiscal.html` (sem processamento Vite).
