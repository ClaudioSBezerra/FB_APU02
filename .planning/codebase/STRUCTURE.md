# Codebase Structure
<!-- Last mapped: 2026-05-12 -->

## Directory Layout

```
FB_APU02/
├── backend/                    # Go API server
│   ├── main.go                 # Entrypoint — router, DB, migrations
│   ├── handlers/               # HTTP handlers (um arquivo por domínio)
│   ├── middleware/             # Middleware de autenticação/tenant
│   ├── migrations/             # SQL migrations numeradas sequencialmente
│   ├── services/               # Serviços de background (RFB, email)
│   ├── tools/                  # Ferramentas de debug (não produção)
│   ├── vendor/                 # Dependências Go vendorizadas
│   ├── go.mod / go.sum         # Manifesto de dependências
│   └── Dockerfile
│
├── frontend/                   # React + TypeScript SPA
│   ├── src/
│   │   ├── App.tsx             # Roteamento principal (React Router)
│   │   ├── main.tsx            # Entrypoint React
│   │   ├── index.css           # Estilos globais (Tailwind)
│   │   ├── pages/              # Páginas (uma por rota)
│   │   ├── components/         # Componentes de negócio reutilizáveis
│   │   │   └── ui/             # Shadcn/ui primitivos (gerados)
│   │   ├── contexts/           # AuthContext, FilialContext
│   │   ├── hooks/              # Hooks customizados
│   │   └── lib/                # Utilitários (export Excel, formatters, logger)
│   ├── public/                 # Assets estáticos (logos, favicons)
│   ├── dist/                   # Build de produção (não versionar)
│   ├── package.json
│   ├── vite.config.ts
│   ├── tailwind.config.js
│   └── Dockerfile / Dockerfile.dev
│
├── erp-bridge-aws/             # Serviço Python no AWS
│   ├── bridge.py               # Daemon que puxa dados do ERP para o APU02
│   ├── config.yaml             # Configuração da bridge
│   └── erp-bridge.service      # Systemd unit file
│
├── .github/workflows/          # CI/CD GitHub Actions
│   ├── deploy-production.yml
│   ├── deploy-staging.yml
│   └── deploy-cliente-aws.yml
│
├── config/                     # Configurações gerais
├── docs/                       # Documentação
├── scripts/                    # Scripts utilitários
├── scripts_oracle/             # Scripts Oracle para integração ERP
├── installer/                  # Scripts de instalação
├── Bmad-output/                # Artefatos BMAD (planejamento)
├── _bmad/                      # Framework BMAD (agentes/workflows)
├── docker-compose.yml          # Dev local
├── docker-compose.prod.yml     # Produção
├── Dockerfile.production       # Imagem multi-stage (backend+frontend)
└── .env / .env.production      # Variáveis de ambiente
```

## Key File Locations

### Backend

| Propósito | Localização |
|-----------|-------------|
| Entrypoint / router | `backend/main.go` |
| Handler de autenticação | `backend/handlers/auth.go` |
| Handler de filiais | `backend/handlers/filiais.go` |
| Handler NF-e entradas | `backend/handlers/nfe_entradas.go` |
| Handler NF-e saídas | `backend/handlers/nfe_saidas.go` |
| Handler CT-e entradas | `backend/handlers/cte_entradas.go` |
| Handler ERP bridge | `backend/handlers/erp_bridge.go` |
| Handler RFB | `backend/handlers/rfb_apuracao.go` |
| Handler CGIBS | `backend/handlers/cgibs_apuracao.go` |
| Handler admin | `backend/handlers/admin.go` |
| Middleware JWT/tenant | `backend/handlers/middleware.go` |
| Middleware multi-tenant | `backend/middleware/tenant.go` |
| Serviço RFB | `backend/services/rfb.go` |
| Serviço email | `backend/services/email.go` |
| Última migration | `backend/migrations/104_cte_entradas_tributos.sql` |

### Frontend

| Propósito | Localização |
|-----------|-------------|
| Roteamento | `frontend/src/App.tsx` |
| Autenticação (context) | `frontend/src/contexts/AuthContext.tsx` |
| Filial selecionada (context) | `frontend/src/contexts/FilialContext.tsx` |
| Painel principal | `frontend/src/pages/Painel.tsx` |
| Login | `frontend/src/pages/Login.tsx` |
| Gestão de usuários | `frontend/src/pages/AdminUsers.tsx` |
| Consulta NF-e entradas | `frontend/src/pages/ConsultaNFesEntradas.tsx` |
| Consulta NF-e saídas | `frontend/src/pages/ConsultaNFeSaidas.tsx` |
| Consulta CT-e | `frontend/src/pages/ConsultaCTesEntradas.tsx` |
| Malha fina | `frontend/src/pages/MalhaFinaPanel.tsx` |
| ERP bridge config | `frontend/src/pages/ERPBridgeConfig.tsx` |
| RFB credentials | `frontend/src/pages/RFBCredentials.tsx` |
| CGIBS apuração | `frontend/src/pages/CGIBSApuracao.tsx` |
| Sidebar / navegação | `frontend/src/components/AppSidebar.tsx` |
| Export Excel | `frontend/src/lib/exportToExcel.ts` |
| Utilitários de navigação | `frontend/src/lib/navigation.ts` |

## Naming Conventions

### Backend (Go)

- **Handlers:** `snake_case.go` → função `PascalCaseHandler` (ex: `nfe_entradas.go` → `GetNFeEntradasHandler`)
- **Migrations:** numeração sequencial `NNN_descricao_snake.sql` (ex: `103_nfe_tributos_icms_pis_cofins.sql`)
- **Services:** `snake_case.go` → struct `PascalCase` com métodos (ex: `rfb.go` → `RFBService`)
- **Middleware:** `snake_case.go` → função/middleware em snake_case

### Frontend (TypeScript/React)

- **Pages:** `PascalCase.tsx` (ex: `ConsultaNFesEntradas.tsx`) — uma página por arquivo
- **Components (negócio):** `PascalCase.tsx` (ex: `FilialSelector.tsx`)
- **Components (UI primitivos):** kebab-case dentro de `ui/` (ex: `alert-dialog.tsx`) — gerados pelo Shadcn
- **Contexts:** `PascalCaseContext.tsx` (ex: `AuthContext.tsx`)
- **Hooks:** `use-kebab-case.tsx` (ex: `use-mobile.tsx`)
- **Lib/utils:** `camelCase.ts` (ex: `exportToExcel.ts`, `formatFilial.ts`)

## Where to Add New Code

| O que adicionar | Onde criar |
|-----------------|-----------|
| Novo endpoint de API | `backend/handlers/novo_modulo.go` + registrar rota em `backend/main.go` |
| Nova página no frontend | `frontend/src/pages/NovaPagina.tsx` + rota em `frontend/src/App.tsx` + link em `AppSidebar.tsx` |
| Nova migration de DB | `backend/migrations/NNN_descricao.sql` (incrementar número) |
| Novo serviço background | `backend/services/novo_servico.go` + inicializar em `main.go` |
| Novo componente reutilizável | `frontend/src/components/NovoComponente.tsx` |
| Novo primitivo UI | Usar `npx shadcn add <component>` → gera em `frontend/src/components/ui/` |
| Nova tabela/view PostgreSQL | Nova migration SQL numerada sequencialmente |
| Novo context React | `frontend/src/contexts/NovoContext.tsx` |
