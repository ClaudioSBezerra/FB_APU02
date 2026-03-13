# Planejamento Técnico de Migração: Lovable (fbapp_rt) -> FB_APU01 (Go/React)

**Versão:** 1.1 (Detalhamento Pós-Análise)
**Status:** Planejamento
**Data:** 29/01/2026

---

## 1. Análise da Aplicação Atual (fbapp_rt)

**Origem:** [GitHub - fbapp_rt](https://github.com/ClaudioSBezerra/fbapp_rt) (Clonado em `temp_lovable_analysis`)

### 1.1 Stack Tecnológica Confirmada
- **Frontend:** React 18, Vite, TypeScript.
- **UI Kit:** Shadcn-ui (Components/ui), Tailwind CSS.
- **Gerenciamento de Estado/Data Fetching:** TanStack Query (`@tanstack/react-query`).
- **Roteamento:** React Router DOM (`BrowserRouter`).
- **Backend (Atual):** Supabase (Auth, Postgres, Edge Functions).
- **Lógica de Negócio (Crítica):** 
    - Supabase Edge Functions (`supabase/functions/`):
        - `parse-efd-v13`: Lógica pesada de parsing de arquivos SPED.
        - `process-efd-job`: Processamento assíncrono.
        - `admin-users`, `demo-signup`: Gestão de usuários.

### 1.2 Mapeamento de Rotas e Páginas (src/App.tsx)
As seguintes rotas precisam ser migradas para o FB_APU01:

| Rota | Componente | Descrição | Prioridade |
|------|------------|-----------|------------|
| `/` | `Index` | Home Page | Baixa |
| `/landing` | `Landing` | Landing Page Comercial | Baixa |
| `/auth` | `Auth` | Login/Registro | Alta |
| `/dashboard` | `Dashboard` | Visão Geral Analítica | Alta |
| `/importar-efd` | `ImportarEFD` | Upload e Processamento SPED | Crítica |
| `/importar-efd-icms`| `ImportarEFDIcms`| Upload SPED ICMS | Alta |
| `/mercadorias` | `Mercadorias` | Tabela de Produtos/Itens | Média |
| `/servicos` | `Servicos` | Tabela de Serviços | Média |
| `/aliquotas` | `Aliquotas` | Configuração de Impostos | Alta |
| `/empresas` | `Empresas` | Gestão Multi-empresa | Alta |
| `/configuracoes` | `Configuracoes` | Configurações do Sistema | Média |
| `/uso-consumo` | `UsoConsumoImobilizado` | Análise Específica | Baixa |

---

## 2. Arquitetura do Novo Sistema (FB_APU01)

A arquitetura migra de **Supabase BaaS** para **Go Monolith + React SPA**.

### 2.1 Backend (Go) - Novos Pacotes Necessários
A estrutura atual do FB_APU01 será expandida para acomodar a lógica migrada das Edge Functions.

```go
backend/
├── handlers/
│   ├── auth.go         // Migrar lógica de `supabase/gotrue`
│   ├── dashboard.go    // Dados para `/dashboard`
│   ├── sped.go         // Substitui `importar-efd`
│   └── tax_rules.go    // Lógica de `aliquotas`
├── services/
│   ├── parser/         // Migrar lógica de `supabase/functions/parse-efd-v13` (TypeScript -> Go)
│   └── reports/        // Geração de relatórios
└── models/             // Structs refletindo o schema do `supabase/migrations`
```

### 2.2 Frontend (React Integrado)
Fusão do código `fbapp_rt/src` dentro de `FB_APU01/frontend/src`.
- **Desafio:** Remover dependência do `supabase-js` client.
- **Solução:** Criar uma camada de abstração de API (`src/services/api.ts`) que substitui `supabase.from('table').select()` por chamadas `axios.get('/api/table')`.

---

## 3. Estratégia de Migração (Passo a Passo)

### Fase 1: Fundação e Dependências (Imediato)
1.  **Instalar Shadcn-ui no FB_APU01:** Garantir que `components/ui` do Lovable funcione no nosso projeto.
2.  **Configurar TanStack Query:** Instalar e configurar o Provider no `App.tsx` do FB_APU01.
3.  **Copiar Componentes UI:** Trazer pasta `src/components/ui` inteira.

### Fase 2: Migração de Páginas "Core" (Dias 1-3)
1.  **Página `ImportarEFD`:**
    - Copiar `pages/ImportarEFD.tsx`.
    - Adaptar para usar nosso endpoint atual de upload (`/api/upload`) em vez da Edge Function.
2.  **Página `Dashboard`:**
    - Copiar layout.
    - Criar endpoint Go `/api/dashboard/stats` para alimentar os gráficos.

### Fase 3: Migração de Lógica de Negócio (Backend) (Dias 4-7)
1.  **Parser SPED (Crítico):**
    - Reescrever a lógica de `supabase/functions/parse-efd-v13/index.ts` em Go. O Go é muito mais rápido para isso.
    - *Nota:* Já temos um parser básico em `worker.go`. Precisamos aprimorá-lo com as regras de negócio do Lovable (validações, deduplicação).
2.  **Banco de Dados:**
    - Analisar `supabase/migrations` e converter para scripts de migração Go (`golang-migrate` ou SQL puro).
    - Foco nas tabelas: `empresas`, `aliquotas`, `mercadorias`.

### Fase 4: Autenticação e Multi-Tenancy (Dias 8-10)
- O sistema Lovable usa RLS (Row Level Security) do Postgres pesadamente.
- **Decisão:** Manter RLS ou mover para lógica de aplicação?
- **Recomendação:** Mover para lógica de aplicação (Middleware Go) para simplificar a manutenção e reduzir dependência do Postgres específico.

---

## 4. Ações Imediatas (Próximos Passos)

1. [ ] **Frontend:** Copiar `src/components/ui` do `temp_lovable_analysis` para `frontend/src/components/ui`.
2. [ ] **Frontend:** Instalar dependências faltantes (`lucide-react`, `clsx`, `tailwind-merge`, `@radix-ui/*`).
3. [ ] **Backend:** Criar migration SQL para as tabelas de `aliquotas` e `dashboard`.


**Versão:** 1.0
**Status:** Planejamento
**Data:** 29/01/2026

---

## 1. Análise da Aplicação Atual (fbapp_rt)

**Origem:** [GitHub - fbapp_rt](https://github.com/ClaudioSBezerra/fbapp_rt)
**Tecnologias Identificadas:**
- **Frontend:** Vite, React 18, TypeScript.
- **UI Kit:** Shadcn-ui, Tailwind CSS, Lucide React (ícones).
- **Estado/Dados:** TanStack Query (provável), Supabase Client (padrão Lovable) ou Mock Data.
- **Roteamento:** React Router DOM.

**Levantamento de Componentes (Preliminar):**
*Necessário clonar o repositório para listagem exata.*
- [ ] Dashboards Analíticos.
- [ ] Formulários de Cadastro.
- [ ] Relatórios/Tabelas de Dados.

---

## 2. Arquitetura do Novo Sistema

A arquitetura migra de um modelo **Serverless/BaaS (Supabase)** para um modelo **Monolito Modular (Go + React)** hospedado em VPS.

### 2.1 Backend (Go)
Estrutura baseada em pacotes ("Clean Architecture" simplificada):
```
backend/
├── cmd/server/         # Entrypoint
├── config/             # Carregamento de envs
├── handlers/           # Controllers HTTP (Gin ou Stdlib)
│   ├── auth.go         # Login, Refresh Token
│   ├── dashboard.go    # Dados agregados
│   └── reports.go      # Geração de relatórios
├── middleware/         # Auth, CORS, Logging
├── models/             # Structs e Interfaces de Banco
├── services/           # Lógica de Negócio (Regras Tributárias)
└── db/                 # Migrations e Conexão
```

### 2.2 Frontend (React Integrado)
Fusão do código Lovable dentro de `frontend/src`:
- Manter `shadcn-ui` (configurar `tailwind.config.js` para suportar as variáveis CSS do Lovable).
- Substituir chamadas `supabase.from('table')` por `fetch('/api/v1/resource')`.
- Autenticação via Context API consumindo endpoints Go (`/api/login`).

### 2.3 Banco de Dados (PostgreSQL)
- Schema único para APU01 e módulos migrados.
- Tabelas novas prefixadas ou organizadas por domínio (ex: `crm_clientes`, `tax_reports`).

---

## 3. Estratégia de Migração (Fases)

### Fase 1: Fundação e Setup (Dias 1-2)
- [ ] Clonar `fbapp_rt` para análise profunda.
- [ ] Instalar dependências de UI no `frontend` atual (`npx shadcn-ui@latest init`).
- [ ] Configurar rotas no Go para servir a nova API (`/api/v1/...`).

### Fase 2: Migração de Frontend e Mocking (Dias 3-5)
- [ ] Copiar componentes React do Lovable para `frontend/src/features/lovable`.
- [ ] Ajustar rotas no `App.tsx` para incluir as novas páginas.
- [ ] Criar "Mock Handlers" no Go para retornar dados estáticos e validar o Frontend.

### Fase 3: Implementação de Backend e Banco (Dias 6-10)
- [ ] Criar tabelas no Postgres (Migrations).
- [ ] Implementar Handlers Go reais substituindo os Mocks.
- [ ] Implementar Autenticação JWT completa.

### Fase 4: Integração com Receita Federal (Dias 11-15)
- [ ] Criar módulo `services/receita`.
- [ ] Implementar client HTTP com mTLS (se necessário) para APIs da Reforma Tributária.
- [ ] Criar endpoints de proxy: `Frontend -> Go Backend -> API Receita`.

### Fase 5: Testes e Go-Live (Dias 16-20)
- [ ] Testes de Carga (K6).
- [ ] Deploy em Staging (Subdomínio de teste).
- [ ] Virada de Chave (Merge para Main).

---

## 4. Requisitos Técnicos

- **Go:** 1.22+ (já em uso).
- **Framework Web:** Standard Library (`net/http`) para manter leveza, ou `Chi` para roteamento melhorado. Evitar frameworks pesados.
- **Banco:** PostgreSQL 15+ (já em uso).
- **Cache:** Redis (para filas e cache de respostas da Receita).
- **Frontend:** React 18 + Vite (manter compatibilidade).

---

## 5. Plano de Testes

### 5.1 Unitários (Backend)
- Testar lógica de cálculo tributário isoladamente.
- Coverage alvo: > 80% em `services/`.

### 5.2 Integração (API)
- Testar endpoints com banco de dados de teste (Docker).
- Validar fluxos de erro (401, 403, 500).

### 5.3 Regressão Visual
- Garantir que a importação do CSS do Lovable não quebrou o estilo existente do APU01.

---

## 6. Documentação

- **API Specs:** Swagger/OpenAPI (gerado via comentários no Go).
- **Guia de Instalação:** Atualizar `README.md`.
- **Dicionário de Dados:** Documentar novas tabelas e colunas.

---

## 7. Garantia de Isolamento

Para garantir que a migração não afete a operação atual:
1.  **Branching:** Todo trabalho na branch `feature/lovable-migration`.
2.  **API Versioning:** Novas rotas em `/api/v2` ou `/api/lovable`, mantendo `/api/upload` e `/api/jobs` (v1) intocadas.
3.  **Namespace CSS:** Se houver conflito de estilos, isolar o CSS do Lovable com prefixos ou Shadow DOM (último caso).
4.  **Feature Flags:** Habilitar novas telas no menu apenas para usuários admin ou via env var `ENABLE_LOVABLE_MODULE=true`.