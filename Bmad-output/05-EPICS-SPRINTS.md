# Epics e Sprints — FBTax Apuração Assistida

**Projetos:** FC_APU01 (protótipo/MVP) → FC_APU02 (produção)
**Período:** 2024 Q4 → 2025 Q1

---

## Visão Geral

O desenvolvimento foi dividido em dois repositórios/projetos:

| Projeto | Descrição |
|---------|-----------|
| **FC_APU01** | Protótipo inicial — validação de conceito, integração RFB, importação básica |
| **FC_APU02** | Refatoração completa para produção — multi-tenancy, segurança enterprise, Malha Fina |

---

## FC_APU01 — MVP / Protótipo

### Epic 1 — Fundação e Infraestrutura

**Objetivo:** Criar a base técnica do sistema (Go + React + PostgreSQL + Docker)

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 1 | E1-S1 | Setup do repositório: Go 1.22, Vite/React 18, TypeScript, Tailwind |
| Sprint 1 | E1-S2 | Dockerfile multi-stage (node → golang → alpine) |
| Sprint 1 | E1-S3 | Docker Compose com PostgreSQL 15 |
| Sprint 1 | E1-S4 | Migration runner sequencial (schema_migrations table) |
| Sprint 1 | E1-S5 | Configuração Coolify + Traefik + Let's Encrypt |
| Sprint 2 | E1-S6 | CI/CD via GitHub push → auto-deploy Coolify |
| Sprint 2 | E1-S7 | Variáveis de ambiente: JWT_SECRET, DATABASE_URL, PORT |

---

### Epic 2 — Autenticação e Multi-tenancy

**Objetivo:** Sistema de login seguro com isolamento por empresa

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 2 | E2-S1 | Tabelas: environments, groups, companies, users, user_environments |
| Sprint 2 | E2-S2 | Registro e login com bcrypt (custo 14) + JWT (30min) |
| Sprint 2 | E2-S3 | Middleware AuthMiddleware — validação JWT em todas as rotas |
| Sprint 2 | E2-S4 | Refresh token (sync.Map) + endpoint POST /api/auth/refresh |
| Sprint 2 | E2-S5 | Token blacklist no logout |
| Sprint 3 | E2-S6 | Header X-Company-ID + GetEffectiveCompanyID() |
| Sprint 3 | E2-S7 | RBAC: role admin vs user |
| Sprint 3 | E2-S8 | Rate limiting por IP (login: 5/15min, registro: 10/hora) |
| Sprint 3 | E2-S9 | Reset de senha por e-mail (SMTP Hostinger + token hex) |

---

### Epic 3 — Importação de Documentos Fiscais

**Objetivo:** Upload e parsing de XMLs NF-e/CT-e

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 3 | E3-S1 | Tabela nfe_saidas + parser XML NF-e (mod. 55 e 65) |
| Sprint 3 | E3-S2 | Endpoint POST /api/nfe-saidas/upload (multipart) |
| Sprint 4 | E3-S3 | Tabela nfe_entradas + parser NF-e entrada |
| Sprint 4 | E3-S4 | Tabela cte_entradas + parser CT-e |
| Sprint 4 | E3-S5 | Controle de duplicatas por chave de acesso (unique constraint) |
| Sprint 4 | E3-S6 | Tabela dfe_xml — armazenamento de XML bruto com TTL 5 anos |
| Sprint 4 | E3-S7 | Bulk upload com relatório de sucesso/falha/duplicata |

---

### Epic 4 — Apuração IBS/CBS

**Objetivo:** Calcular débitos e créditos CBS/IBS por documento

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 5 | E4-S1 | Tabela aliquotas por período |
| Sprint 5 | E4-S2 | Tabela cfop com classificação de operações tributáveis |
| Sprint 5 | E4-S3 | Cálculo de débitos CBS por NF-e saída |
| Sprint 5 | E4-S4 | Cálculo de créditos IBS/CBS por NF-e entrada e CT-e |
| Sprint 5 | E4-S5 | Tabela forn_simples — fornecedores Simples Nacional (sem crédito) |
| Sprint 6 | E4-S6 | Painel de apuração com totais por período |
| Sprint 6 | E4-S7 | Importação em lote de fornecedores Simples (CSV/XLSX) |

---

### Epic 5 — Integração RFB (Protótipo)

**Objetivo:** Conexão inicial com API da Receita Federal CBS v1

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 6 | E5-S1 | OAuth2 client_credentials — GetToken() para API RFB |
| Sprint 6 | E5-S2 | Endpoint POST /api/rfb/apuracao/solicitar |
| Sprint 6 | E5-S3 | Tabela rfb_credentials com client_id/secret criptografados (AES-256-GCM) |
| Sprint 7 | E5-S4 | Webhook receiver POST /api/rfb/webhook + validação HMAC-SHA256 |
| Sprint 7 | E5-S5 | Download e parsing do JSON de débitos RFB |
| Sprint 7 | E5-S6 | Tabela rfb_debitos + rfb_resumo |
| Sprint 7 | E5-S7 | Tabela rfb_requests com status workflow (pending → completed/error) |

---

## FC_APU02 — Produção

### Epic 6 — Refatoração Multi-tenancy Enterprise

**Objetivo:** Escalar para múltiplos clientes com isolamento garantido

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 1 | E6-S1 | Hierarquia completa: environments → groups → companies |
| Sprint 1 | E6-S2 | company_id UUID em todas as tabelas de dados |
| Sprint 1 | E6-S3 | Company Switcher no frontend sem logout |
| Sprint 1 | E6-S4 | Preferência de empresa persistida por usuário no banco |
| Sprint 2 | E6-S5 | Seletor de filiais — filtrar documentos por CNPJ |
| Sprint 2 | E6-S6 | Tabela filial_apelidos — nomes amigáveis para CNPJs |
| Sprint 2 | E6-S7 | AppRail com navegação real (React Router) |

---

### Epic 7 — Segurança Enterprise

**Objetivo:** Hardening de segurança para produção enterprise

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 2 | E7-S1 | SecurityMiddleware — headers HTTP (HSTS, CSP, X-Frame-Options, etc.) |
| Sprint 2 | E7-S2 | CORS whitelist configurável via ALLOWED_ORIGINS |
| Sprint 2 | E7-S3 | ENCRYPTION_KEY separado do JWT_SECRET para credenciais RFB |
| Sprint 3 | E7-S4 | Rate limiting: forgot-password (3/hora) |
| Sprint 3 | E7-S5 | Mascaramento de client_secret RFB (exibição: últimos 4 chars) |
| Sprint 3 | E7-S6 | Goroutine de limpeza de tokens expirados (blacklist + refresh) |
| Sprint 3 | E7-S7 | Goroutine de deleção automática de dfe_xml após 5 anos |

---

### Epic 8 — Malha Fina

**Objetivo:** Identificar documentos que a RFB vê mas a empresa não importou

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 4 | E8-S1 | View materializada mv_malha_fina_resumo (NF-e Saídas, NF-e Entradas, CT-e) |
| Sprint 4 | E8-S2 | Unique index (company_id, tipo, ni_emitente, data_emissao::DATE) |
| Sprint 4 | E8-S3 | Endpoint GET /api/malha-fina/resumo com filtros |
| Sprint 4 | E8-S4 | Endpoint POST /api/malha-fina/refresh (REFRESH CONCURRENTLY) |
| Sprint 5 | E8-S5 | Painel MalhaFinaPanel — tabela com filtro por tipo e CNPJ emitente |
| Sprint 5 | E8-S6 | MalhaFinaResumoGeral — consolidado multi-tipo |
| Sprint 5 | E8-S7 | Filtro CNPJ Emitente com apelido de filial |
| Sprint 5 | E8-S8 | Drill-down: chave completa + número da chave por registro |
| Sprint 5 | E8-S9 | Auto-refresh da view após download bem-sucedido da RFB |

---

### Epic 9 — Integração RFB Robusta

**Objetivo:** Tornar a integração RFB confiável para uso em produção

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 5 | E9-S1 | Scheduler automático com horário configurável por empresa |
| Sprint 5 | E9-S2 | Banner "Importação automática ativa" no painel RFB |
| Sprint 6 | E9-S3 | Token cache (rfbTokenCache sync.Mutex) — garantir mesmo token entre solicitação e download |
| Sprint 6 | E9-S4 | Ambiente: producao / producao_restrita configurável por empresa |
| Sprint 6 | E9-S5 | Reprocessamento manual de solicitações com erro |
| Sprint 6 | E9-S6 | Download Manual para registros com tiquete_download |
| Sprint 6 | E9-S7 | Limpeza de erros em lote (DELETE /api/rfb/apuracao/clear-errors) |
| Sprint 6 | E9-S8 | Status workflow completo: pending → requested → webhook_received → downloading → completed/error |
| Sprint 6 | E9-S9 | Histórico de solicitações com resumo inline (período, débitos, CBS) |

---

### Epic 10 — Créditos em Risco

**Objetivo:** Alertar sobre documentos que podem gerar negação de crédito

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 7 | E10-S1 | Cruzamento NF-e Entradas × cadastro de filiais/CNPJs da empresa |
| Sprint 7 | E10-S2 | Endpoint GET /api/creditos-perdidos |
| Sprint 7 | E10-S3 | Painel CreditosPerdidos com lista de emitentes não cadastrados |
| Sprint 7 | E10-S4 | Correção: excluir filiais cadastradas como emitentes da contagem |

---

### Epic 11 — Configurações e Administração

**Objetivo:** Interface de administração completa

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 7 | E11-S1 | Gerenciamento de usuários (admin): criar, listar, alterar role |
| Sprint 7 | E11-S2 | Configuração de alíquotas CBS/IBS por período |
| Sprint 7 | E11-S3 | Configuração da tabela CFOP |
| Sprint 7 | E11-S4 | Configuração de fornecedores Simples Nacional |
| Sprint 8 | E11-S5 | Configuração de apelidos de filiais |
| Sprint 8 | E11-S6 | Configuração de gestores por área |
| Sprint 8 | E11-S7 | Credenciais RFB por empresa (CRUD com criptografia) |
| Sprint 8 | E11-S8 | Limpeza de dados (admin): NF-e, CT-e, débitos RFB |

---

### Epic 12 — ERP Bridge (Conector Oracle)

**Objetivo:** Ferramenta Python para extrair XMLs do Oracle ERP e enviar à API

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 8 | E12-S1 | Conexão Oracle via cx_Oracle + Instant Client 23.0 |
| Sprint 8 | E12-S2 | Query NF-e Saídas do Oracle ERP |
| Sprint 8 | E12-S3 | Query NF-e Entradas e CT-e Entradas |
| Sprint 8 | E12-S4 | tracker.db (SQLite) — controle de chaves já enviadas |
| Sprint 9 | E12-S5 | Retomada após interrupção (continua da última chave) |
| Sprint 9 | E12-S6 | Renovação automática de token JWT durante execução longa |
| Sprint 9 | E12-S7 | config.yaml com suporte a múltiplos servidores Oracle |
| Sprint 9 | E12-S8 | executar.bat para execução no Windows |
| Sprint 9 | E12-S9 | Logging detalhado: progresso por servidor, estatísticas finais |

---

### Epic 13 — Performance e Views Materializadas

**Objetivo:** Performance para volumes de centenas de milhares de documentos

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 9 | E13-S1 | mv_malha_fina_resumo — VIEW MATERIALIZADA com índices |
| Sprint 9 | E13-S2 | mv_mercadorias_agregada — agregação de mercadorias |
| Sprint 9 | E13-S3 | mv_compras_fornecedores — agregação de compras por fornecedor |
| Sprint 9 | E13-S4 | mv_simples_nacional — view para créditos Simples |
| Sprint 10 | E13-S5 | Índices B-tree + GiST (trigram pg_trgm) para busca |
| Sprint 10 | E13-S6 | TanStack Query com keepPreviousData para UX fluida |
| Sprint 10 | E13-S7 | Paginação server-side nas listagens grandes |

---

### Epic 14 — Documentação e Entrega

**Objetivo:** Documentar o sistema para entrega ao cliente e operação AWS

| Sprint | Story | Descrição |
|--------|-------|-----------|
| Sprint 10 | E14-S1 | Escopo de Negócio (01-ESCOPO-NEGOCIO.md) |
| Sprint 10 | E14-S2 | Stack Tecnológica (02-STACK-TECNOLOGICA.md) |
| Sprint 10 | E14-S3 | Segurança (03-SEGURANCA.md) |
| Sprint 10 | E14-S4 | Guia de Instalação AWS/Coolify (04-INSTALACAO-AWS.md) |
| Sprint 10 | E14-S5 | Epics e Sprints (05-EPICS-SPRINTS.md) |

---

## Resumo de Versões Entregues

| Versão | Data | Principais Features |
|--------|------|---------------------|
| 1.0.0 | FC_APU01 | MVP: importação XMLs, apuração CBS, integração RFB básica |
| 1.0.1 | FC_APU02 | Multi-tenancy enterprise, segurança, Company Switcher |
| 1.0.2 | FC_APU02 | Malha Fina v1, view materializada, filtros |
| 1.0.3 | FC_APU02 | Malha Fina v2 (chave completa, filtro CNPJ, ícone Telescope) |
| 1.0.4 | FC_APU02 | Créditos em risco, token cache RFB, Download Manual, fix GROUP BY |

---

## Débito Técnico Identificado

| Item | Prioridade | Descrição |
|------|-----------|-----------|
| Redis | Média | Cache de sessões e rate limiting persistente (hoje em memória) |
| Testes automatizados | Alta | Cobertura de testes unitários e integração (Go + React) |
| Observabilidade | Média | Structured logging + métricas (Prometheus/Grafana) |
| Paginação cursor-based | Baixa | Para rfb_debitos com +368k registros |
| Webhook retry | Baixa | Fila de reprocessamento para falhas de webhook |
