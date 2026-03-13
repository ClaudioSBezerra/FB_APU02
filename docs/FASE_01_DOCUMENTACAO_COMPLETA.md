# FB_APU01 - Documentação Completa - FASE 1

## Resumo Executivo

O **FB_APU01** é uma plataforma SaaS multi-tenant para apuração fiscal brasileira, projetada para processar arquivos SPED EFD Contribuições e calcular tributos conforme a Reforma Tributária (IBS, CBS) e sistema legado (ICMS, PIS, COFINS).

**Versão Atual:** 5.1.0
**Status:** FASE 1 - Functional MVP em Produção
**URL Produção:** https://fbtax.cloud
**Infraestrutura:** VPS Hostinger + Docker Compose + Coolify

---

## 1. Stack Tecnológico

### Backend (Go 1.22+)
```
Linguagem:      Go (Golang)
Framework:      net/http (Standard Library)
Database:       PostgreSQL 15+
Autenticação:   JWT (golang-jwt/jwt/v5)
Password:       bcrypt (golang.org/x/crypto/bcrypt)
Encoder:        ISO-8859-1 para UTF-8 (golang.org/x/text)
```

### Frontend (React 18.3.1)
```
Framework:      React + TypeScript
Build Tool:     Vite 5.2.0
UI:             Tailwind CSS + Shadcn/UI (Radix UI)
State:          React Query (TanStack Query)
Charts:         Recharts 3.7.0
Router:         React Router DOM 6.22.3
Forms:          React Hook Form 7.71.1
Excel Export:   xlsx 0.18.5
```

### Infraestrutura
```
Containerização: Docker + Docker Compose
Proxy:           Traefik (Produção)
Deploy:          GitHub Actions + Coolify
Monitoring:      Prometheus + Grafana (Opcional)
```

---

## 2. Arquitetura do Sistema

### 2.1 Estrutura de Pastas

```
FB_APU01/
├── backend/
│   ├── handlers/          # API Handlers
│   │   ├── auth.go        # Autenticação (login, register, JWT)
│   │   ├── upload.go      # Upload de arquivos SPED
│   │   ├── job.go         # Status e gerenciamento de jobs
│   │   ├── report.go      # Relatórios fiscais
│   │   ├── dashboard.go   # Dashboard de projeção (Reforma Tributária)
│   │   ├── simples_dashboard.go  # Dashboard Simples Nacional
│   │   ├── config.go      # Configurações (alíquotas, CFOPs)
│   │   ├── admin.go       # Administração (usuários, DB)
│   │   ├── environment.go # Gestão de ambientes/grupos/empresas
│   │   ├── hierarchy.go   # Hierarquia de empresas
│   │   ├── cfop.go        # Gestão de CFOPs
│   │   └── forn_simples.go # Fornecedores Simples Nacional
│   ├── worker/
│   │   └── worker.go      # Worker assíncrono de processamento
│   ├── migrations/        # Migrations do PostgreSQL (44 arquivos)
│   ├── main.go            # Entry point do backend
│   ├── Dockerfile         # Build do container Go
│   └── go.mod             # Dependências Go
│
├── frontend/
│   ├── src/
│   │   ├── components/    # Componentes React reutilizáveis
│   │   ├── contexts/      # Contexts (AuthContext)
│   │   ├── hooks/         # Hooks customizados
│   │   ├── lib/           # Bibliotecas (axios, utils)
│   │   └── pages/         # Páginas da aplicação
│   │       ├── Login.tsx
│   │       ├── Register.tsx
│   │       ├── Dashboard.tsx
│   │       ├── Mercadorias.tsx
│   │       ├── ImportarEFD.tsx
│   │       ├── OperacoesSimplesNacional.tsx  # Dashboard Simples
│   │       ├── TabelaAliquotas.tsx
│   │       ├── TabelaCFOP.tsx
│   │       ├── TabelaFornSimples.tsx
│   │       ├── GestaoAmbiente.tsx
│   │       └── AdminUsers.tsx
│   ├── Dockerfile         # Build do container React
│   ├── package.json       # Dependências NPM
│   └── vite.config.ts     # Configuração Vite
│
├── docs/                  # Documentação
│   ├── API_REFERENCE.md
│   ├── ARCHITECTURE.md
│   └── TECHNICAL_SPECS.md
│
├── .github/workflows/
│   └── deploy-production.yml  # CI/CD
│
├── docker-compose.yml      # Ambiente desenvolvimento
├── docker-compose.prod.yml # Ambiente produção
└── .env                    # Variáveis de ambiente
```

### 2.2 Fluxo de Dados

```
┌─────────────┐
│   Usuário   │
└──────┬──────┘
       │ Upload SPED
       ▼
┌──────────────────────────────────────────────────────────────┐
│                     Frontend (React)                          │
│  • Seleção de pasta com webkitdirectory                       │
│  • Upload em chunks via multipart/form-data                   │
│  • Polling de status do job                                   │
└────────────────────────┬─────────────────────────────────────┘
                         │ POST /api/upload
                         ▼
┌──────────────────────────────────────────────────────────────┐
│                    Backend (Go API)                           │
│  • Validação JWT                                             │
│  • Verificação de permissões da empresa                      │
│  • Criação do job (status='pending')                          │
│  • Armazenamento do arquivo em /uploads                      │
└────────────────────────┬─────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────┐
│               Worker Background (Goroutines)                  │
│  • Poll de jobs 'pending' (FOR UPDATE SKIP LOCKED)           │
│  • Parse streaming do arquivo SPED (ISO-8859-1 → UTF-8)      │
│  • Extração de registros: 0000, 0150, C100, C190, C500...    │
│  • Cálculo de impostos projetados (IBS, CBS)                 │
│  • Insert em batches de 1000 linhas                          │
│  • Refresh automático de materialized views                  │
└────────────────────────┬─────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────┐
│                    PostgreSQL 15+                            │
│  • Tabelas raw dos registros SPED                            │
│  • Materialized Views para agregações                        │
│  • Tabelas de configuração (alíquotas, CFOPs)               │
└──────────────────────────────────────────────────────────────┘
```

---

## 3. Banco de Dados

### 3.1 Estrutura de Tabelas

#### Tabelas Principais (Core)

| Tabela | Descrição | Campos Principais |
|--------|-----------|-------------------|
| `users` | Usuários do sistema | id, email, password_hash, full_name, role, trial_ends_at |
| `environments` | Ambientes (Multi-tenancy) | id, name, description |
| `enterprise_groups` | Grupos de Empresas | id, environment_id, name, description |
| `companies` | Empresas (CNPJ) | id, group_id, cnpj, name, trade_name, owner_id |
| `user_environments` | Relação Usuário-Ambiente | user_id, environment_id, role |
| `import_jobs` | Jobs de Importação | id, company_id, filename, status, message, dt_ini, dt_fin, last_line_processed |

#### Tabelas de Registros SPED (Raw Data)

| Tabela | Registro SPED | Descrição |
|--------|---------------|-----------|
| `participants` | 0150 | Cadastro de Participantes (fornecedores/clientes) |
| `reg_c100` | C100 | Nota Fiscal (Código 01, 1B, 04 e 55) |
| `reg_c190` | C190 | Detalhamento da Nota Fiscal (CFOP, valores) |
| `reg_c500` | C500 | Nota Fiscal/Conta de Energia Elétrica |
| `reg_c600` | C600 | Assinatura de Energia Elétrica |
| `reg_d100` | D100 | Conhecimento de Transporte |
| `reg_d500` | D500 | Nota Fiscal de Serviço de Comunicação |

#### Tabelas de Configuração

| Tabela | Descrição |
|--------|-----------|
| `cfop` | Tabela de CFOPs com tipo (R=Revenda, S=Serviço, E=Entrega, C=Consumo, A=Ativo, T=Transferência, O=Outros) |
| `forn_simples` | Fornecedores optantes pelo Simples Nacional |
| `tabela_aliquotas` | Alíquotas por ano para Reforma Tributária (perc_ibs_uf, perc_ibs_mun, perc_cbs, perc_reduc_icms, perc_reduc_piscofins) |

#### Tabelas de Agregação

| Tabela | Descrição |
|--------|-----------|
| `operacoes_comerciais` | Operações comerciais consolidadas (C100 + C190) |
| `energia_agregado` | Serviços de energia elétrica consolidados (C500 + C600) |
| `frete_agregado` | Serviços de frete consolidados (D100) |
| `comunicacoes_agregado` | Serviços de comunicação consolidados (D500) |

#### Materialized Views

| View | Descrição | Refresh |
|------|-----------|---------|
| `mv_mercadorias_agregada` | View agregada de mercadorias por filial/período/CFOP | Após cada job |
| `mv_operacoes_simples` | View para análise de fornecedores Simples Nacional | Após cada job |

### 3.2 Migrations

O sistema possui **44 migrations** que executam automaticamente na inicialização:

**Principais migrations:**
- `001_create_jobs_table.sql` - Tabela de jobs
- `005_revise_sped_tables.sql` - Tabelas SPED
- `013_create_environment_hierarchy.sql` - Multi-tenancy
- `015_create_auth_system.sql` - Autenticação
- `021_create_mv_mercadorias.sql` - Materialized view mercadorias
- `041_create_mv_simples_nacional.sql` - Materialized view Simples
- `035_seed_future_aliquotas.sql` - Alíquotas 2027-2033

**Controle de versão:** A tabela `schema_migrations` rastreia quais migrations já foram executadas.

---

## 4. APIs e Endpoints

### 4.1 Autenticação

| Método | Endpoint | Descrição |
|--------|----------|-----------|
| POST | `/api/auth/register` | Registro de novo usuário |
| POST | `/api/auth/login` | Login (retorna JWT) |
| GET | `/api/auth/me` | Dados do usuário autenticado |
| POST | `/api/auth/forgot-password` | Recuperação de senha |

### 4.2 Upload e Processamento

| Método | Endpoint | Descrição |
|--------|----------|-----------|
| POST | `/api/upload` | Upload de arquivo SPED |
| GET | `/api/jobs` | Lista todos os jobs do usuário |
| GET | `/api/jobs/{id}` | Status de um job específico |
| GET | `/api/jobs/{id}/participants` | Participantes do job |
| GET | `/api/check-duplicity` | Verifica duplicidade de importação |

### 4.3 Relatórios

| Método | Endpoint | Descrição |
|--------|----------|-----------|
| GET | `/api/reports/mercadorias` | Relatório de mercadorias |
| GET | `/api/reports/energia` | Relatório de energia elétrica |
| GET | `/api/reports/transporte` | Relatório de transporte |
| GET | `/api/reports/comunicacoes` | Relatório de comunicações |

### 4.4 Dashboards

| Método | Endpoint | Descrição |
|--------|----------|-----------|
| GET | `/api/dashboard/projection` | Projeção da Reforma Tributária (2027-2033) |
| GET | `/api/dashboard/simples-nacional` | Dashboard Simples Nacional (créditos perdidos) |

### 4.5 Configurações

| Método | Endpoint | Descrição |
|--------|----------|-----------|
| GET | `/api/config/aliquotas` | Lista alíquotas por ano |
| GET/POST | `/api/config/cfop` | Gerencia CFOPs |
| POST | `/api/config/cfop/import` | Importa CFOPs em lote |
| GET/POST/DELETE | `/api/config/forn-simples` | Gerencia fornecedores Simples |
| POST | `/api/config/forn-simples/import` | Importa fornecedores em lote |
| GET/POST/PUT/DELETE | `/api/config/environments` | Gerencia ambientes |
| GET/POST/DELETE | `/api/config/groups` | Gerencia grupos de empresas |
| GET/POST/DELETE | `/api/config/companies` | Gerencia empresas |

### 4.6 Admin

| Método | Endpoint | Descrição | Role |
|--------|----------|-----------|------|
| GET | `/api/admin/users` | Lista usuários | admin |
| POST | `/api/admin/users/create` | Cria usuário | admin |
| POST | `/api/admin/users/promote` | Promove a admin | admin |
| DELETE | `/api/admin/users/delete` | Deleta usuário | admin |
| POST | `/api/admin/reset-db` | Reseta banco de dados | admin |
| POST | `/api/admin/refresh-views` | Refresh manual de views | admin |
| POST | `/api/company/reset-data` | Reseta dados da empresa | user |

### 4.7 Outros

| Método | Endpoint | Descrição |
|--------|----------|-----------|
| GET | `/api/health` | Health check do serviço |
| GET | `/api/user/hierarchy` | Hierarquia do usuário (amb/grupo/empresa) |
| GET | `/api/user/companies` | Lista empresas do usuário |

---

## 5. Worker de Processamento

### 5.1 Arquitetura do Worker

O worker opera em **2 goroutines concorrentes** (configurado para VPS de 2 vCPUs):

```
┌─────────────────────────────────────────────────────────────┐
│                    Worker Pool (2 workers)                   │
│                                                              │
│  ┌────────────────┐  ┌────────────────┐                    │
│  │   Worker #1    │  │   Worker #2    │                    │
│  │                │  │                │                    │
│  │  1. SELECT job │  │  1. SELECT job │                    │
│  │     FOR UPDATE │  │     FOR UPDATE │                    │
│  │     SKIP LOCKED│  │     SKIP LOCKED│                    │
│  │                │  │                │                    │
│  │  2. Parse SPED │  │  2. Parse SPED │                    │
│  │     (streaming)│  │     (streaming)│                    │
│  │                │  │                │                    │
│  │  3. INSERT     │  │  3. INSERT     │                    │
│  │     batch/1000 │  │     batch/1000 │                    │
│  │                │  │                │                    │
│  │  4. REFRESH MV │  │  4. REFRESH MV │                    │
│  └────────────────┘  └────────────────┘                    │
└─────────────────────────────────────────────────────────────┘
```

### 5.2 Características

- **Recovery Automático:** Jobs stuck em 'processing' são resetados para 'pending' no startup
- **Checkpointing:** `last_line_processed` permite retomar de onde parou em caso de crash
- **Batch Processing:** Commits a cada 1000 linhas para evitar locks excessivos
- **Throttling:** Sleep de 200ms entre batches para evitar 504 timeout
- **Concurrent Refresh:** Usa `REFRESH MATERIALIZED VIEW CONCURRENTLY` para não bloquear leituras
- **Validação:** Verifica integridade do arquivo (header |0000|, footer |9999|)

### 5.3 Registros SPED Processados

| Registro | Descrição |
|----------|-----------|
| 0000 | Header do arquivo (dados da empresa, período) |
| 0150 | Cadastro de participantes (fornecedores) |
| C100 | Documento - Nota Fiscal (código 01, 1B, 04 e 55) |
| C190 | Detalhamento consolidado por CFOP |
| C500 | Nota Fiscal/Conta de Energia Elétrica |
| C600 | Assinatura de Energia Elétrica |
| D100 | Conhecimento de Transporte |
| D500 | Nota Fiscal de Serviço de Comunicação |

### 5.4 Cálculos Implementados

Para cada registro, o worker calcula:

```
vl_icms_projetado  = vl_icms × (1 - perc_reduc_icms/100)
vl_ibs_projetado   = vl_doc × (perc_ibs_uf + perc_ibs_mun)/100
vl_cbs_projetado   = vl_doc × perc_cbs/100
```

---

## 6. Dashboards e Funcionalidades

### 6.1 Dashboard Principal (Mercadorias)

**Arquivo:** [Dashboard.tsx](frontend/src/pages/Dashboard.tsx)

- Visualização consolidada de operações comerciais
- Filtros por período (mês/ano)
- Breakdown por tipo (Entrada/Saída)
- Cards com totais de ICMS, IBS e CBS projetados

### 6.2 Dashboard de Projeção (Reforma Tributária)

**Arquivo:** [dashboard.go:21](backend/handlers/dashboard.go#L21) | [Dashboard.tsx](frontend/src/pages/Dashboard.tsx)

**Endpoint:** `/api/dashboard/projection`

Calcula o impacto da transição de ICMS/PIS/COFINS para IBS/CBS entre 2027-2033:

```
Para cada ano:
  1. ICMS Net       = (ICMS Saída - ICMS Entrada) × (1 - Redução)
  2. Base IBS/CBS   = (Valor Saída - Valor Entrada) - ICMS Projetado
  3. IBS Net        = Base Saída × Alíquota - Base Entrada × Alíquota
  4. CBS Net        = Base Saída × Alíquota - Base Entrada × Alíquota
  5. Saldo Total    = ICMS Net + IBS Net + CBS Net
```

**Alíquotas consideradas:**
- IBS UF: Variável por ano
- IBS Município: Variável por ano
- CBS: Variável por ano
- Redução ICMS: 100% em 2033 (extinção progressiva)

### 6.3 Dashboard Simples Nacional

**Arquivo:** [simples_dashboard.go:23](backend/handlers/simples_dashboard.go#L23) | [OperacoesSimplesNacional.tsx](frontend/src/pages/OperacoesSimplesNacional.tsx)

**Endpoint:** `/api/dashboard/simples-nacional`

Analisa **créditos perdidos** com fornecedores optantes pelo Simples Nacional:

```
Fornecedores Simples (CFOP R, C, A + Fretes):
  - Base Cálculo    = Valor da Operação
  - IBS Perdido     = Base × Alíquota IBS (2033)
  - CBS Perdido     = Base × Alíquota CBS (2033)
  - Total Perdido   = IBS + CBS
```

**Ordenação:** Por total perdido (decrescente)

### 6.4 Importação de SPED

**Arquivo:** [ImportarEFD.tsx](frontend/src/pages/ImportarEFD.tsx)

Funcionalidades:
- Upload de arquivos SPED (drag & drop ou seleção)
- Suporte a múltiplos arquivos
- Verificação de duplicidade (período + empresa)
- Barra de progresso em tempo real
- Polling de status a cada 2 segundos
- Detecção automática de CNPJ e período

### 6.5 Gestão de Ambiente

**Arquivo:** [GestaoAmbiente.tsx](frontend/src/pages/GestaoAmbiente.tsx)

Gerencia a hierarquia multi-tenant:
- **Ambientes:** Criação e gerenciamento de ambientes
- **Grupos:** Organização de empresas em grupos
- **Empresas:** Cadastro de empresas (CNPJ, razão social)

### 6.6 Tabelas de Configuração

- **TabelaAliquotas.tsx:** Gerencia alíquotas por ano
- **TabelaCFOP.tsx:** Importação e consulta de CFOPs
- **TabelaFornSimples.tsx:** Cadastro de fornecedores Simples Nacional

---

## 7. Autenticação e Multi-tenancy

### 7.1 Sistema de Autenticação

**JWT (JSON Web Tokens):**
- Algoritmo: HS256
- Expiração: 24 horas
- Secret: Configurável via `JWT_SECRET`

**Middleware:** [auth.go:118](backend/handlers/auth.go#L118)
- Valida token no header `Authorization: Bearer <token>`
- Extrai `user_id` e `role` do token
- Injeta no contexto da requisição

### 7.2 Controle de Acesso

**Roles:**
- `admin`: Acesso total, gestão de usuários e ambientes
- `user`: Acesso restrito aos dados de suas empresas

**Permissões:**
- Verificação de `company_id` em todas as queries
- Header `X-Company-ID` para troca de contexto entre empresas
- Auto-provisioning de environment/group/company no registro

### 7.3 Hierarquia Multi-tenant

```
Environment (Ambiente)
    └── Enterprise Group (Grupo de Empresas)
            └── Company (Empresa)
                    └── Import Jobs (Arquivos SPED)
                            └── SPED Registers (Registros)
```

**Exemplo:**
- Environment: "Cliente Exemplo LTDA"
- Group: "Grupo Varejo"
- Companies: "Loja Matriz", "Filial São Paulo", "Filial Rio"

---

## 8. Ambiente de Desenvolvimento vs Produção

### 8.1 Desenvolvimento

**Arquivo:** [docker-compose.yml](docker-compose.yml)

```yaml
services:
  api:    # Backend Go
    ports: ["8081:8081"]
    environment:
      - DATABASE_URL=postgres://postgres:postgres@db:5432/fiscal_db

  web:    # Frontend React
    ports: ["3000:80"]

  db:     # PostgreSQL 15
    image: postgres:15-alpine

  redis:  # Redis
    image: redis:alpine
```

**Executar:**
```bash
docker-compose up
```

### 8.2 Produção

**Arquivo:** [docker-compose.prod.yml](docker-compose.prod.yml)

```yaml
services:
  api:
    restart: always
    environment:
      - DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@db:5432/${DB_NAME}?sslmode=require
      - JWT_SECRET=${JWT_SECRET}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8081/api/health"]
    deploy:
      resources:
        limits: { cpus: '1.0', memory: 1G }

  web:
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.web.rule=Host(`www.fbtax.cloud`, `fbtax.cloud`)"

  db:
    deploy:
      resources:
        limits: { cpus: '2.0', memory: 2G }
```

**Deploy automático:** [deploy-production.yml](.github/workflows/deploy-production.yml)

### 8.3 Variáveis de Ambiente

| Variável | Descrição | Default |
|----------|-----------|---------|
| `PORT` | Porta do backend | 8081 |
| `DATABASE_URL` | Connection string PostgreSQL | - |
| `JWT_SECRET` | Segredo para assinatura de tokens | - |
| `REDIS_ADDR` | Endereço do Redis | redis:6379 |
| `ENVIRONMENT` | Ambiente (development/production) | development |
| `VITE_API_URL` | URL da API para frontend | http://localhost:8081 |

---

## 9. CI/CD e Deploy

### 9.1 Pipeline GitHub Actions

**Arquivo:** [.github/workflows/deploy-production.yml](.github/workflows/deploy-production.yml)

```
Trigger: Push para branch 'main'
  ↓
Build Backend (Docker)
Build Frontend (Docker)
  ↓
Push para GitHub Container Registry
  ↓
SSH no servidor (Hostinger)
  ↓
docker-compose pull
docker-compose up -d
```

### 9.2 Deploy Manual

```bash
# No servidor
cd /root/fb_apu01
git pull
docker-compose -f docker-compose.prod.yml pull
docker-compose -f docker-compose.prod.yml up -d
```

---

## 10. Funcionalidades Implementadas (FASE 1)

### Core ✅
- [x] Upload e processamento de arquivos SPED EFD Contribuições
- [x] Extração de registros C100, C190, C500, C600, D100, D500
- [x] Cálculo automático de ICMS, PIS, COFINS
- [x] Cálculo projetado de IBS e CBS (Reforma Tributária)
- [x] Detecção automática de filiais (CNPJ nos registros)
- [x] Worker assíncrono com checkpointing

### Dashboards ✅
- [x] Dashboard Principal de Mercadorias
- [x] Dashboard de Projeção Reforma Tributária (2027-2033)
- [x] Dashboard Simples Nacional (créditos perdidos)
- [x] Relatórios de Energia, Transporte e Comunicações

### Multi-tenancy ✅
- [x] Sistema de ambientes/grupos/empresas
- [x] Controle de acesso por empresa
- [x] Auto-provisioning no registro
- [x] Troca de contexto de empresa (X-Company-ID)

### Administração ✅
- [x] Gestão de usuários (admin)
- [x] Gestão de ambientes, grupos e empresas
- [x] Reset de dados por empresa
- [x] Reset completo do banco (admin)
- [x] Refresh manual de materialized views

### Configuração ✅
- [x] Tabela de alíquotas por ano (2024-2033)
- [x] Tabela de CFOPs com importação
- [x] Tabela de fornecedores Simples Nacional
- [x] Verificação de duplicidade de importação

### Performance ✅
- [x] Materialized views para agregações
- [x] Batch processing (1000 linhas)
- [x] Concurrent refresh de views
- [x] Checkpoint para recuperação de falhas
- [x] Throttling para evitar timeout

---

## 11. Próximos Passos (FASE 2)

### Integrações Pendentes
- [ ] Conexão com APIs da RFB (CBS)
- [ ] Conexão com APIs do Comitê Gestor do IBS
- [ ] Integração com ERPs (API REST, mensageria)
- [ ] Captura de extratos de pagamento para split payment

### Funcionalidades Futuras
- [ ] Geração de DARF para pagamento
- [ ] Exportação de eventos DF-e
- [ ] Comparativo de apurações (próprio vs RFB/IBS)
- [ ] Auditoria assistida em tempo real
- [ ] Regras tributárias configuráveis e versionadas

### Melhorias Técnicas
- [ ] Implementar procedures no PostgreSQL
- [ ] Otimizar queries com EXPLAIN ANALYZE
- [ ] Adicionar testes unitários e integração
- [ ] Implementar cache com Redis
- [ ] Logging estruturado

---

## 12. Troubleshooting

### Worker não processa arquivos
```bash
# Verificar logs do container
docker logs fb_apu01-api-prod

# Verificar jobs presos
docker exec -it fb_apu01-db-prod psql -U postgres -d fiscal_db
SELECT id, status, message FROM import_jobs ORDER BY created_at DESC;
```

### Materialized View desatualizada
```sql
-- Manual refresh
REFRESH MATERIALIZED VIEW CONCURRENTLY mv_mercadorias_agregada;
REFRESH MATERIALIZED VIEW CONCURRENTLY mv_operacoes_simples;
```

### Reset de empresa (preservando usuários)
```bash
# Via API (requer auth)
POST /api/company/reset-data
Header: X-Company-ID: <uuid>
```

---

## 13. Links Úteis

| Arquivo | Descrição |
|---------|-----------|
| [main.go:199](backend/main.go#L199) | Entry point e registro de rotas |
| [worker.go:22](backend/worker/worker.go#L22) | Worker de processamento |
| [auth.go:1](backend/handlers/auth.go#L1) | Sistema de autenticação |
| [dashboard.go:21](backend/handlers/dashboard.go#L21) | Dashboard de projeção |
| [simples_dashboard.go:23](backend/handlers/simples_dashboard.go#L23) | Dashboard Simples |
| [upload.go:1](backend/handlers/upload.go#L1) | Handler de upload |
| [report.go:1](backend/handlers/report.go#L1) | Handlers de relatórios |
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | Diagramas de arquitetura |
| [API_REFERENCE.md](docs/API_REFERENCE.md) | Documentação da API |

---

**Documento gerado em:** 2026-02-09
**Versão do Backend:** 5.1.0
**Versão do Frontend:** 0.2.1
