---
stepsCompleted: [1, 2, 3]
inputDocuments:
  - docs/FASE_01_DOCUMENTACAO_COMPLETA.md
  - docs/ARCHITECTURE.md
  - _bmad-output/analysis/brainstorming-session-2026-02-10.md
workflowType: 'prd'
---

# Product Requirements Document - Multi-Tenancy Nativo FBTax Cloud

**Author:** Claudio Bezerra
**Date:** 2026-02-12
**Version:** 1.0
**Phase:** FASE 2 - Fundação Multi-Tenant

---

## 1. Visao do Produto

### 1.1 Problema

O FBTax Cloud (v5.1.0) implementa isolamento de dados por tenant a nivel de aplicacao (WHERE clauses manuais nos handlers Go). Isso apresenta riscos criticos:

- **Vazamento de dados entre tenants:** Um bug em qualquer query SQL pode expor dados de um cliente para outro
- **Escalabilidade limitada:** Cada novo handler exige implementacao manual de filtros por tenant
- **Sem defesa em profundidade:** Nao existe protecao no banco de dados caso a camada de aplicacao falhe
- **Impossibilidade de auditoria:** Nao ha log de tentativas de acesso cross-tenant
- **Bloqueio comercial:** Clientes medios e grandes exigem isolamento certificavel para compliance (SOX, LGPD)

### 1.2 Solucao Proposta

Implementar multi-tenancy nativo com:

1. **Tenant Context automatico** no middleware (elimina filtros manuais)
2. **Row-Level Security (RLS)** no PostgreSQL (defesa no banco)
3. **Desnormalizacao de `environment_id`** como `tenant_id` nas tabelas de dados
4. **Planos comerciais por tenant** (controle de features e limites)

### 1.3 Usuarios-Alvo

| Perfil | Necessidade | Exemplo |
|--------|-------------|---------|
| Escritorio contabil pequeno | Ambiente isolado por cliente | 1-5 empresas, 1 usuario |
| Empresa media | Multi-empresa com grupos | 5-50 empresas, 5-20 usuarios |
| Grande empresa | Isolamento total, compliance, auditoria | 50+ empresas, multi-grupo |
| Plataforma admin | Visao global, gestao de tenants | Equipe FBTax |

---

## 2. Requisitos Funcionais

### FR-001: Tenant Context Automatico

O sistema deve resolver automaticamente o contexto do tenant (environment) em cada requisicao HTTP autenticada, sem intervencao do handler.

**Criterios de aceitacao:**

- JWT inclui `tenant_id` (environment_id) e `company_id` alem de `user_id` e `role`
- Middleware `TenantMiddleware` extrai e valida contexto antes de chegar ao handler
- Header `X-Company-ID` permite trocar empresa ativa dentro do mesmo tenant
- Tentativa de acessar empresa fora do tenant retorna HTTP 403
- Contexto disponivel via `GetTenantContext(r.Context())` em qualquer handler

### FR-002: Row-Level Security no PostgreSQL

O banco de dados deve impedir acesso a dados de outros tenants independentemente da camada de aplicacao.

**Criterios de aceitacao:**

- Todas as tabelas com dados de tenant possuem RLS habilitado
- Policies filtram por `current_setting('app.tenant_id')`
- Middleware Go executa `SET LOCAL app.tenant_id` em cada transacao
- Usuario de aplicacao no PostgreSQL NAO e superuser (RLS se aplica)
- Admin (superuser) pode acessar todos os dados para suporte

### FR-003: Desnormalizacao do Tenant ID

Tabelas de dados devem conter `environment_id` como coluna direta para queries performaticas.

**Criterios de aceitacao:**

- Tabelas `companies`, `import_jobs`, `nfe_data` e dados fiscais possuem coluna `environment_id`
- Dados existentes migrados corretamente via `enterprise_groups.environment_id`
- Indices criados em `environment_id` para todas as tabelas com RLS
- Trigger automatico preenche `environment_id` em INSERTs (derivando de `company_id` -> `group_id` -> `environment_id`)

### FR-004: Planos Comerciais por Tenant

O sistema deve controlar features e limites por tenant baseado em plano contratado.

**Criterios de aceitacao:**

- Tabela `tenant_plans` armazena plano ativo por tenant
- Tipos de plano: `trial`, `starter`, `professional`, `enterprise`
- Limites por plano: max_companies, max_users, max_nfes_month
- Features por plano: lista de modulos habilitados (JSON)
- Middleware verifica limites antes de operacoes de criacao
- API de consulta de plano disponivel para frontend exibir limites

### FR-005: Audit Log de Acesso

O sistema deve registrar tentativas de acesso cross-tenant e operacoes sensiveisveis.

**Criterios de aceitacao:**

- Tabela `audit_log` registra: user_id, tenant_id, action, resource, timestamp, ip_address
- Tentativas de acesso negado (403) sao registradas automaticamente
- Operacoes CRUD em dados sensiveis sao registradas
- Admin pode consultar audit log filtrado por tenant/user/periodo
- Retencao minima de 12 meses

---

## 3. Requisitos Nao-Funcionais

### NFR-001: Performance

- RLS policies nao devem adicionar mais que 5ms de latencia por query
- Indices em `environment_id` devem garantir seq scan zero em tabelas com RLS
- Connection pooling deve ser compativel com `SET LOCAL` (transacoes por request)

### NFR-002: Seguranca

- Isolamento de dados deve ser verificavel por teste automatizado
- Nenhum endpoint deve retornar dados de outro tenant em nenhuma circunstancia
- Credenciais de banco nao devem usar superuser em producao
- Compliance com LGPD para isolamento de dados pessoais entre tenants

### NFR-003: Compatibilidade

- Migracao deve ser retrocompativel (sistema funciona durante cada fase)
- JWT antigo (sem tenant_id) deve ser aceito temporariamente com fallback para resolucao via DB
- APIs existentes mantem contrato (sem breaking changes)

### NFR-004: Observabilidade

- Logs devem incluir `tenant_id` em toda requisicao
- Metricas de uso por tenant (requests, storage, NF-es processadas)
- Alertas para tentativas de acesso cross-tenant

---

## 4. Escopo e Limites

### Dentro do Escopo (Fase 2)

- Tenant context middleware
- RLS no PostgreSQL para todas as tabelas de dados
- Desnormalizacao de environment_id
- Planos comerciais basicos (trial, starter, professional, enterprise)
- Audit log de acesso
- Migracao de dados existentes

### Fora do Escopo (Futuro)

- Schema-per-tenant para grandes clientes (schema isolation)
- Billing automatico e cobranca
- Feature flags granulares por tenant
- Self-service de criacao de tenant (onboarding automatico)
- API publica com rate limiting por tenant
- Marketplace de integracoes

---

## 5. Metricas de Sucesso

| Metrica | Baseline (v5.1) | Meta (Fase 2) |
|---------|-----------------|---------------|
| Vazamentos cross-tenant em testes | Nao testado | 0 (zero) |
| Queries sem filtro de tenant | ~30% dos handlers | 0% (RLS como safety net) |
| Latencia adicional por RLS | N/A | < 5ms p99 |
| Codigo de isolamento por handler | ~10 linhas | 0 linhas (automatico) |
| Tentativas cross-tenant logadas | Nao logado | 100% |

---

## 6. Riscos e Mitigacoes

| Risco | Probabilidade | Impacto | Mitigacao |
|-------|--------------|---------|-----------|
| RLS causa degradacao de performance | Media | Alto | Indices em tenant_id + benchmarks antes/depois |
| SET LOCAL incompativel com connection pool | Media | Alto | Usar transacoes explicitas (BEGIN/SET LOCAL/COMMIT) |
| Migracao de dados corrompe environment_id | Baixa | Critico | Backup pre-migracao + validacao pos-migracao |
| JWT legado sem tenant_id causa erros | Alta (transicao) | Medio | Fallback temporario com resolucao via DB |
| Materialized Views ignoram RLS | Alta | Alto | Adicionar environment_id nas MVs e filtrar explicitamente |

---

## 7. Dependencias

- **PostgreSQL 15+** com suporte completo a RLS (ja em uso)
- **Go 1.22+** com `database/sql` suportando transacoes contextuais (ja em uso)
- **Migracao 013-022** ja criaram a hierarquia environments/groups/companies
- **Nenhuma dependencia externa** nova necessaria
