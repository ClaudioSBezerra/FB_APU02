# Stack Tecnológica — FBTax Apuração Assistida

**Projeto:** FB_APU02
**Arquitetura:** Monolito (API + SPA servidos pelo mesmo container)

---

## 1. Visão Geral da Arquitetura

```
┌─────────────────────────────────────────────────────────────────┐
│                        Coolify / Docker                         │
│                                                                 │
│  ┌─────────────────────────────────────────────────────┐        │
│  │              Container: fb_apu02                    │        │
│  │                                                     │        │
│  │   Go 1.22 Binary                                    │        │
│  │   ├── HTTP Server (net/http, port 8081)             │        │
│  │   ├── API REST (/api/*)                             │        │
│  │   ├── Static SPA (/ → React build)                 │        │
│  │   ├── RFB Scheduler (goroutine, cron)               │        │
│  │   └── Webhook Receiver (/api/rfb/webhook)           │        │
│  │                                                     │        │
│  └─────────────────────────────────────────────────────┘        │
│                            │                                    │
│                ┌───────────┴───────────┐                        │
│                ▼                       ▼                        │
│  ┌─────────────────────┐  ┌───────────────────┐                 │
│  │  PostgreSQL 15      │  │  Redis (opcional) │                 │
│  │  (volume persistido)│  │  (sessões/cache)  │                 │
│  └─────────────────────┘  └───────────────────┘                 │
│                                                                 │
│  Traefik (Reverse Proxy) — HTTPS, Let's Encrypt                 │
└─────────────────────────────────────────────────────────────────┘
                            │
                            │ HTTPS
                            ▼
                    ┌───────────────┐
                    │  Receita      │
                    │  Federal API  │
                    │  (OAuth2)     │
                    └───────────────┘
```

---

## 2. Backend

| Componente | Tecnologia | Versão |
|-----------|-----------|--------|
| Linguagem | Go | 1.22.0 |
| HTTP Server | `net/http` (stdlib) | — |
| ORM / DB Driver | `lib/pq` (PostgreSQL) | v1.11.2 |
| Auth JWT | `golang-jwt/jwt/v5` | v5.3.1 |
| Criptografia | `golang.org/x/crypto` (bcrypt) | v0.17.0 |
| Email SMTP | stdlib `net/smtp` + template | — |
| Env vars | `joho/godotenv` | v1.5.1 |
| Build | CGO_ENABLED=0, static binary | — |

### Padrões Backend
- **Roteamento:** `net/http` ServeMux com exact-match paths
- **Multi-tenancy:** `company_id` em todas as queries + header `X-Company-ID`
- **Concorrência:** Goroutines para scheduler e processamento de webhooks
- **Migrações:** Runner próprio sequencial via `schema_migrations` table (73 migrations)
- **Views Materializadas:** `mv_malha_fina_resumo` com `REFRESH CONCURRENTLY`

---

## 3. Frontend

| Componente | Tecnologia | Versão |
|-----------|-----------|--------|
| Framework | React | 18.3.1 |
| Roteamento | React Router | 6.22.3 |
| State/Cache | TanStack Query (React Query) | v5.90.20 |
| Build tool | Vite + SWC | 5.2.0 / 3.5.0 |
| Linguagem | TypeScript | 5.2.2 |
| Estilização | Tailwind CSS | 3.4.3 |
| UI Components | Radix UI + shadcn/ui | — |
| Ícones | Lucide React | 0.363.0 |
| Formulários | React Hook Form + Zod | 7.71.1 / 4.3.6 |
| Gráficos | Recharts | 3.7.0 |
| Notificações | Sonner | 2.0.7 |
| Excel I/O | xlsx | 0.18.5 |
| Datas | date-fns | 4.1.0 |

### Padrões Frontend
- **SPA:** React Router com fallback para `index.html`
- **Auth:** Context API com interceptor global de fetch (injeta Bearer + X-Company-ID)
- **Dados:** TanStack Query com `keepPreviousData` para UX fluida
- **UI:** shadcn/ui (componentes Radix UI estilizados com Tailwind)
- **Persistência local:** `localStorage` para token, empresa selecionada, filiais

---

## 4. Banco de Dados

| Aspecto | Detalhe |
|---------|---------|
| SGBD | PostgreSQL 15 |
| Driver | `lib/pq` (Go) |
| Migrações | Sequenciais via `schema_migrations` |
| Views | Materialized Views para relatórios pesados |
| Indexes | B-tree + GiST (trigram pg_trgm) |
| Tenancy | `company_id` UUID em todas as tabelas de dados |
| Retenção | `dfe_xml` auto-delete após 5 anos (fiscal) |

### Principais Tabelas

```
Hierarquia:       environments → groups → companies → users
                               → user_environments (RBAC)

Documentos:       nfe_saidas | nfe_entradas | cte_entradas
                  dfe_xml (XMLs brutos, retidos 5 anos)

RFB:              rfb_credentials | rfb_requests | rfb_debitos | rfb_resumo

Configuração:     cfop | aliquotas | filial_apelidos | forn_simples
                  managers | schema_migrations

Views:            mv_malha_fina_resumo (MATERIALIZED, atualizada pós-RFB)
                  mv_mercadorias_agregada | mv_compras_fornecedores
                  mv_simples_nacional
```

---

## 5. Infraestrutura e Deploy

| Componente | Tecnologia | Detalhe |
|-----------|-----------|---------|
| Container | Docker (multi-stage) | Alpine Linux runtime |
| Orquestração | Docker Compose | Via Coolify |
| Reverse Proxy | Traefik | Gerenciado pelo Coolify |
| SSL/TLS | Let's Encrypt | Auto-renovação via Traefik |
| Plataforma CI/CD | Coolify | Self-hosted PaaS |
| Repositório | GitHub | Push → auto-deploy |
| Base OS | Alpine Linux | Imagem mínima (~20MB runtime) |

### Multi-stage Docker Build
```
Stage 1: node:18-alpine    → npm run build (React/Vite)
Stage 2: golang:1.22-alpine → go build -ldflags="-w -s" (binary estático)
Stage 3: alpine:3.19        → runtime final (~50MB total)
```

---

## 6. Integrações Externas

| Serviço | Protocolo | Uso |
|---------|-----------|-----|
| **RFB API** (`api.receitafederal.gov.br`) | HTTPS / OAuth2 | Solicitar e baixar apuração CBS |
| **RFB Webhook** (inbound) | HTTPS POST (HMAC) | Receber tiquete de download |
| **SMTP Hostinger** (`smtp.hostinger.com:465`) | SSL | Emails transacionais (reset de senha) |
| **Oracle ERP** (via ERP Bridge) | JDBC/cx_Oracle | Extração de XMLs fiscais |
| **Redis** (opcional) | TCP | Cache de sessões |

---

## 7. ERP Bridge (Ferramenta Auxiliar)

| Aspecto | Detalhe |
|---------|---------|
| Linguagem | Python 3.x |
| Oracle Driver | `cx_Oracle` + Oracle Instant Client 23.0 |
| HTTP Client | `requests` |
| Rastreamento | SQLite (`tracker.db`) |
| Config | `config.yaml` |
| Execução | `executar.bat` (Windows) |

---

## 8. Desenvolvimento Local

```bash
# Backend
cd backend && go run .

# Frontend
cd frontend && npm run dev   # Vite dev server em :5173

# Banco (Docker)
docker compose up db -d
```

**Variáveis mínimas para dev:**
```
DATABASE_URL=postgres://user:pass@localhost:5432/fb_apu02
JWT_SECRET=dev-secret-minimo-32-caracteres-aqui
PORT=8081
```
