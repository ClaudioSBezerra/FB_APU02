# Roadmap do Projeto - Rumo à Produção (Hostinger VPS + Coolify)

Este documento detalha os passos macro (Épicos) para levar o **FB_APU01** do ambiente local para o servidor de produção na Hostinger VPS, utilizando o **Coolify** para gerenciamento.

---

## 🏁 Épico 1: Finalização e Estabilização do MVP (Local)
**Objetivo:** Garantir que o fluxo "Upload -> Processamento -> Visualização" funcione perfeitamente na máquina local.

- [x] Correção de Bugs Críticos (Build Backend).
- [x] Teste de Carga SPED (Leitura e Importação).
- [x] Visualização de Participantes.

## ☁️ Épico 2: Preparação da Infraestrutura (Hostinger VPS)
**Objetivo:** Configurar o servidor VPS (Recomendado: KVM 2 ou superior, Ubuntu 22.04/24.04).

- [x] **Contratação Hostinger**: VPS adquirida.
- [x] **Configuração Inicial**: Resetar senha root ou configurar chave SSH.
- [x] **Acesso SSH**: Validar conexão.
- [x] **Instalação do Coolify**: O painel de controle da nossa infraestrutura.

## 🚀 Épico 3: Pipeline de Deploy Contínuo (CD)
**Objetivo:** Automatizar a atualização do sistema via Git.

- [x] **Conexão GitHub -> Coolify**: Adicionar repositório.
- [x] **Configuração de Serviços**:
  - [x] Banco de Dados (Postgres).
  - [x] Redis.
  - [x] Aplicação (Docker Compose).
- [x] **Variáveis de Ambiente**: Configurar segredos de produção.
- [x] **Deploy em Produção**: Acessível em `http://fbtax.cloud`.

## 📊 Épico 4: Monitoramento e Observabilidade
**Objetivo:** Manter a saúde do sistema.

- [x] **Painel Coolify**: Monitoramento de recursos ativo.
- [x] **Health Checks**: Endpoint `/api/health` validado.
- [ ] **Backups**: Configurar rotina automática no Coolify.

---

## FASE 2 - Fundacao Multi-Tenant e Motor de Apuracao IBS/CBS

> Documentacao completa em `_bmad-output/planning-artifacts/`

### Epico 6: Multi-Tenancy Nativo

**Objetivo:** Implementar isolamento de dados robusto com RLS no PostgreSQL, TenantContext automatico e planos comerciais por tenant.

- [ ] **Tenant Context + JWT Expandido**: Middleware automatico com tenant_id no JWT
- [ ] **Row-Level Security (RLS)**: Policies PostgreSQL para todas as tabelas de dados
- [ ] **Desnormalizacao environment_id**: Coluna direta nas tabelas para RLS performatico
- [ ] **Refatoracao de Handlers**: Migrar handlers para WithTenant() e TenantContext
- [ ] **Planos Comerciais**: Controle de limites e features por tenant (trial/starter/pro/enterprise)
- [ ] **Audit Log**: Registro de acesso cross-tenant e operacoes sensiveis

**Documentacao:**

- [PRD Multi-Tenancy](../_bmad-output/planning-artifacts/prd-multi-tenancy.md)
- [Architecture Decision Record](../_bmad-output/planning-artifacts/architecture-multi-tenancy.md)
- [Epic Breakdown (Stories)](../_bmad-output/planning-artifacts/epics-multi-tenancy.md)

### Epico 7: Captura de Documentos Fiscais

**Objetivo:** Sistema de captura automatizada de XMLs fiscais (NF-e, NFC-e, NFS-e, CT-e) de multiplas fontes.

- [ ] **Canonical Model**: Modelo unificado para 4 tipos de documentos fiscais
- [ ] **Connectors**: File system, Database APIs (Oracle, PostgreSQL, SAP), Blob storage
- [ ] **Normalizacao NFS-e**: Tratamento dos schemas municipais heterogeneos

### Epico 8: Motor de Apuracao IBS/CBS

**Objetivo:** Implementar motor de regras para apuracao dos novos tributos da Reforma Tributaria.

- [ ] **Rule Engine**: Motor de regras desacoplado do CRUD
- [ ] **Conciliacao de Pagamentos**: NF-e/CT-e -> Pagamento -> Credito IBS/CBS
- [ ] **Validacao de Creditos**: Elegibilidade, risk scoring, workflow de aprovacao

### Epico 9: Migracao do Sistema Lovable

**Objetivo:** Migrar funcionalidades avancadas do Lovable para infraestrutura proprietaria.

- [ ] **Analise do Codigo Lovable**: Mapear componentes e fluxos
- [ ] **Migracao do Frontend**: Dashboards, cadastros, relatorios fiscais
- [ ] **Expansao do Backend (Go)**: Novos endpoints e otimizacao de queries