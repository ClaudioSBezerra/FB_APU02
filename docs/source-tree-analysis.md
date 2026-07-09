# Análise da Árvore de Fontes — FB_APU02

**Tipo de repositório:** Monorepo multi-parte  
**Partes:** Backend Go (API), Frontend React (SPA), ERP Bridge Python (Daemon)

---

## Estrutura Geral do Repositório

```
FB_APU02/                              ← Raiz do monorepo
├── backend/                           ← Parte 1: API Go (porta 8081)
│   ├── main.go                        ← Ponto de entrada — bootstrap, rotas, DB, scheduler
│   ├── go.mod / go.sum                ← Módulos Go + checksums
│   ├── Dockerfile                     ← Multi-stage: golang:1.22-alpine → alpine
│   ├── .env                           ← Configuração local (não versionada)
│   ├── handlers/                      ← Todos os handlers HTTP (1 arquivo por domínio)
│   │   ├── auth.go                    ← JWT, login, register, refresh, reset — 1139 linhas
│   │   ├── middleware.go              ← CORS, security headers, rate limiters
│   │   ├── config.go                  ← jsonErr(), sanitizeDBErr(), GetTaxRatesHandler
│   │   ├── rfb_apuracao.go            ← Apuração CBS RFB — solicitar, status, abort, webhook
│   │   ├── rfb_credentials.go         ← CRUD credenciais OAuth2 RFB (criptografadas)
│   │   ├── rfb_creditos.go            ← Listagem créditos CBS retornados pela RFB
│   │   ├── rfb_debitos_lista.go       ← Listagem débitos CBS por período
│   │   ├── rfb_pagamentos_fornecedores.go ← Conciliação pagamentos × CBS
│   │   ├── cgibs_apuracao.go          ← Apuração IBS via CGIBS (espelha rfb_apuracao)
│   │   ├── cgibs_credentials.go       ← CRUD credenciais CGIBS
│   │   ├── cgibs_debitos.go           ← Débitos IBS via CGIBS
│   │   ├── nfe_entradas.go            ← Upload XML + listagem NF-e entradas
│   │   ├── nfe_saidas.go              ← Upload XML + listagem NF-e saídas
│   │   ├── cte_entradas.go            ← Upload XML + listagem CT-e entradas
│   │   ├── erp_bridge.go              ← Config, runs, trigger manual ERP Bridge
│   │   ├── erp_bridge_batch.go        ← POST /api/erp-bridge/import/batch (X-API-Key)
│   │   ├── erp_bridge_parceiros.go    ← Sync parceiros ERP
│   │   ├── malha_fina.go              ← Divergências RFB × base local
│   │   ├── apuracao_painel.go         ← Painel consolidado IBS/CBS
│   │   ├── creditos_perdidos.go       ← Créditos em risco (notas sem confirmação RFB)
│   │   ├── dashboard.go               ← Resumo fiscal para dashboard
│   │   ├── admin.go                   ← Reset DB, limpeza, CRUD usuários, environments
│   │   ├── pagamentos_fornecedores.go ← Import CSV + listagem pagamentos
│   │   ├── xml_upload.go              ← Upload genérico de XMLs fiscais
│   │   └── tenant.go                  ← GetUserIDFromContext, GetEffectiveCompanyID (auth.go)
│   ├── services/                      ← Serviços background e integrações externas
│   │   ├── rfb.go                     ← RFBClient: OAuth2 token cache, solicitar, download
│   │   ├── rfb_scheduler.go           ← Scheduler cron + AbortStuckRFBRequests
│   │   ├── rfb_processor.go           ← Processa débitos CBS do webhook RFB
│   │   ├── rfb_creditos_processor.go  ← Processa créditos CBS retornados
│   │   ├── rfb_janitor.go             ← Limpa raw_json antigo (economia de espaço)
│   │   ├── email.go                   ← Envio de e-mails transacionais (SMTP)
│   │   └── mv_refresh.go              ← Refresh de materialized views
│   ├── middleware/                    ← Middleware de multi-tenancy
│   │   └── tenant.go                  ← TenantContext struct, WithTenant() (RLS helper)
│   ├── migrations/                    ← 113 arquivos SQL auto-executados na inicialização
│   │   ├── 001_create_jobs_table.sql  ← [primeiro migration]
│   │   ├── ...                        ← Sequencial com gaps intencionais (003-004, 011)
│   │   ├── 113_*.sql                  ← [último migration]
│   │   └── 000_reset_db.sql.disabled  ← Ignorados pelo loader (.disabled)
│   └── vendor/                        ← Dependências Go vendorizadas (go mod vendor)
│
├── frontend/                          ← Parte 2: SPA React/TypeScript (porta 3000 dev)
│   ├── index.html                     ← Ponto de entrada HTML (Vite SPA shell)
│   ├── package.json / package-lock.json ← Dependências npm
│   ├── vite.config.ts                 ← Vite: porta 3000, proxy /api → :8081, alias @/*
│   ├── tsconfig.app.json              ← TypeScript strict mode, ESNext, bundler
│   ├── tailwind.config.js             ← Cores customizadas, dark mode class, Inter font
│   ├── postcss.config.js              ← Autoprefixer
│   ├── Dockerfile                     ← Multi-stage: node build → nginx:alpine
│   ├── public/                        ← Assets estáticos servidos na raiz (/ path)
│   │   └── argus-portal-fiscal.html   ← Portal demo Argus (mock 100% estático)
│   └── src/                           ← Código-fonte TypeScript
│       ├── main.tsx                   ← Ponto de entrada React — monta <App />
│       ├── App.tsx                    ← Router config, ProtectedRoute, AdminRoute, AppLayout
│       ├── contexts/                  ← Provedores de estado global
│       │   ├── AuthContext.tsx        ← JWT, fetch interceptor (monkey-patch window.fetch)
│       │   └── FilialContext.tsx      ← Seleção de filiais, persistência em localStorage
│       ├── pages/                     ← 44 páginas — uma por feature/domínio
│       │   ├── Login.tsx / Register.tsx / ForgotPassword.tsx / ResetPassword.tsx
│       │   ├── DashboardResumo.tsx    ← Painel de resumo fiscal
│       │   ├── RFBApuracao.tsx        ← Gestão de solicitações CBS RFB
│       │   ├── RFBCredentials.tsx     ← Credenciais OAuth2 RFB por empresa
│       │   ├── RFBDebitos.tsx / RFBCreditosCBS.tsx / RFBPagamentosFornecedores.tsx
│       │   ├── GestaoCredIBSCBS.tsx   ← Visão consolidada créditos IBS/CBS
│       │   ├── CGIBSPainel.tsx / CGIBSApuracao.tsx / CGIBSDebitos.tsx / CGIBSCredentials.tsx
│       │   ├── ImportarXMLsEntrada.tsx / ImportarXMLsSaida.tsx / ImportarXMLsCTe.tsx
│       │   ├── ConsultaNFesEntradas.tsx / ConsultaNFeSaidas.tsx / ConsultaCTesEntradas.tsx
│       │   ├── ImportarPagamentosFornecedores.tsx
│       │   ├── MalhaFinaNFeEntradas.tsx / MalhaFinaNFeSaidas.tsx / MalhaFinaCTe.tsx
│       │   ├── ApuracaoCredPerdidos.tsx
│       │   ├── ERPBridgeConfig.tsx / ERPBridgeLogs.tsx / ERPBridgeCredenciais.tsx
│       │   ├── GestaoAmbiente.tsx
│       │   ├── TabelaAliquotas.tsx / TabelaCFOP.tsx / TabelaFornSimples.tsx
│       │   ├── ApelidosFiliais.tsx / Managers.tsx / AdminUsers.tsx / UserActivity.tsx
│       │   ├── LimparDadosApuracao.tsx
│       │   └── ArgusPortal.tsx        ← Portal demo Argus via iframe
│       ├── components/                ← Componentes compartilhados
│       │   ├── AppRail.tsx            ← Barra lateral de módulos (ícones)
│       │   ├── CompanySwitcher.tsx    ← Troca de empresa ativa
│       │   ├── FilialSelector.tsx     ← Seletor de filiais (usa FilialContext)
│       │   ├── FileUpload.tsx         ← Componente de upload reutilizável
│       │   ├── UploadProgress.tsx     ← Barra de progresso de upload
│       │   ├── InsightCard.tsx        ← Card de métrica para dashboards
│       │   ├── ParticipantList.tsx    ← Lista de participantes de NF-e/CT-e
│       │   └── ui/                   ← 46 componentes shadcn/ui (Radix + Tailwind + CVA)
│       │       └── [accordion, alert, avatar, badge, button, card, dialog, form, ...]
│       ├── hooks/                     ← Hooks customizados
│       │   └── use-mobile.tsx         ← Detecção de viewport mobile (breakpoint 768px)
│       └── lib/                       ← Utilitários e configurações
│           ├── navigation.ts          ← FONTE ÚNICA de módulos e abas da navegação
│           ├── utils.ts               ← cn() (Tailwind merge), formatCurrency()
│           ├── formatFilial.ts        ← Formatadores CNPJ, CPF, filial display
│           ├── exportToExcel.ts       ← Helper de exportação Excel (xlsx)
│           ├── storageKeys.ts         ← Constantes de chaves localStorage/sessionStorage
│           └── logger.ts             ← Logger estruturado (adotado inconsistentemente)
│
├── erp-bridge-aws/                    ← Parte 3: Daemon Python (AWS)
│   ├── bridge.py                      ← Script principal (~1200 linhas)
│   │   ├── FBTaxClient               ← Wrapper HTTP para a API FBTax
│   │   ├── OracleBridge              ← Conexão Oracle + consultas fiscais
│   │   ├── SAPBridge                 ← Modo SAP S/4HANA via FCCORP Oracle
│   │   ├── run_daemon()              ← Loop infinito (60s polling)
│   │   └── main()                    ← Entry point CLI (argparse)
│   └── erp-bridge.service            ← systemd unit (ExecStart: python bridge.py --daemon)
│
├── .github/workflows/                 ← CI/CD GitHub Actions
│   ├── deploy-production.yml          ← Deploy prod: push main → build → Coolify
│   ├── deploy-staging.yml             ← Deploy staging
│   └── deploy-cliente-aws.yml         ← Deploy bridge no servidor AWS do cliente
│
├── docs/                              ← Documentação do projeto (este diretório)
├── .planning/                         ← Artefatos GSD (plans, STATE.md)
├── docker-compose.yml                 ← Stack de desenvolvimento local
├── docker-compose.prod.yml            ← Stack de produção (Coolify)
├── CLAUDE.md                          ← Instruções para Claude Code
└── README.md                          ← Visão geral do projeto
```

---

## Diretórios Críticos por Componente

### Backend (`backend/`)

| Diretório | Propósito |
|-----------|-----------|
| `handlers/` | Todos os handlers HTTP — ponto central de lógica de negócio |
| `services/` | Integrações externas (RFB API, email) e goroutines background |
| `middleware/` | TenantContext para RLS multi-tenant (parcialmente implementado) |
| `migrations/` | 113 arquivos SQL — schema completo do banco de dados |
| `vendor/` | Dependências Go vendorizadas (go mod vendor) |

### Frontend (`frontend/src/`)

| Diretório | Propósito |
|-----------|-----------|
| `pages/` | 44 páginas — cada uma corresponde a uma rota da aplicação |
| `contexts/` | Estado global: AuthContext (auth + fetch interceptor) + FilialContext |
| `components/ui/` | 46 componentes base shadcn/ui (Radix + Tailwind) |
| `components/` | Componentes compartilhados específicos do domínio |
| `lib/` | Utilitários, navegação, formatadores, exportação Excel |
| `hooks/` | Hooks customizados (atualmente só use-mobile.tsx) |

### ERP Bridge (`erp-bridge-aws/`)

| Arquivo | Propósito |
|---------|-----------|
| `bridge.py` | Toda a lógica do daemon — Oracle queries, envio à API, tracker SQLite |
| `erp-bridge.service` | Systemd unit para execução como serviço no Linux |
| `tracker.db` | SQLite local (não versionado) — tracker de documentos enviados |
| `config.yaml` | Configuração (não versionado — contém credenciais Oracle e FBTax) |

---

## Pontos de Integração

```
frontend (React SPA)
    ↓ window.fetch (monkey-patched no AuthContext)
    ↓ Authorization: Bearer {token}
    ↓ X-Company-ID: {uuid}
backend (Go API :8081)
    ↓ pool DB (50 conn) → PostgreSQL 15
    ↓ goroutine scheduler (1min tick) → API RFB/CGIBS
    ↓ HMAC-SHA256 webhook ← RFB API callback

erp-bridge (Python daemon :AWS)
    ↓ X-API-Key header (sem JWT)
backend /api/erp-bridge/* ← bridge.py

RFB API (externa)
    ↑ POST /api/rfb/webhook → backend (HMAC validado)
```
