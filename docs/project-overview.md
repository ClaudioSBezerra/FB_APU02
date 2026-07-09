# Visão Geral do Projeto — FB_APU02

## Identificação

| Campo | Valor |
|-------|-------|
| **Nome** | FB_APU02 — Sistema de Apuração Fiscal |
| **Versão** | 2.0.4 |
| **Tipo** | Monorepo multi-parte (Backend + Frontend + ERP Bridge) |
| **Domínio** | Fiscal tributário — IBS, CBS, PIS/COFINS, ICMS |
| **Cliente** | Grupo Ferreira Costa |
| **Ambiente de produção** | `fctax.fcxlabs.com` / `apuracao.fbtax.cloud` |

---

## Propósito

Plataforma web de apuração fiscal tributária para o grupo Ferreira Costa. Processa arquivos SPED, NF-e e CT-e, apura créditos e débitos de **PIS/COFINS, ICMS, IBS e CBS**, integra com **RFB (Receita Federal)** e **CGIBS**, e oferece malha fina de divergências.

**Valor central:** Apuração fiscal correta e confiável por empresa, sem vazamento de dados entre tenants (multi-tenant via `company_id`).

---

## Stack Tecnológica

| Componente | Tecnologia | Propósito |
|------------|-----------|-----------|
| **Backend API** | Go 1.22, `net/http`, `database/sql` | REST API — auth, fiscal, integrações |
| **Frontend SPA** | React 18.3, TypeScript 5.2, Vite 5.2 | Interface web |
| **Banco de dados** | PostgreSQL 15 | Persistência de dados fiscais |
| **ERP Bridge** | Python 3 (`oracledb`, `requests`) | Daemon de importação Oracle ERP |
| **Deploy** | Docker + Docker Compose + Coolify + Traefik | Produção |
| **CI/CD** | GitHub Actions | Deploy automático por branch |

---

## Arquitetura de Alto Nível

```
┌─────────────────────────────────────────────────────────────┐
│                    Usuário (Browser)                         │
└────────────────────────┬────────────────────────────────────┘
                         │ HTTPS
                    ┌────▼────┐
                    │ Traefik │ ← Let's Encrypt TLS
                    └────┬────┘
           ┌─────────────┼─────────────┐
           │             │             │
    ┌──────▼──────┐      │    ┌────────▼────────┐
    │  Frontend   │      │    │    Backend Go   │
    │ React/Nginx │      │    │  API :8081      │
    │    :80      │      │    │                 │
    └─────────────┘      │    └────────┬────────┘
                         │            │
                    ┌────▼────┐  ┌────▼─────┐
                    │   RFB   │  │ PostgreSQL│
                    │   API   │  │    :5432  │
                    └─────────┘  └──────────┘
                                      ▲
                              ┌───────┴───────┐
                              │  ERP Bridge   │
                              │  Python Daemon│
                              │   (AWS)       │
                              └───────┬───────┘
                                      │
                              ┌───────▼───────┐
                              │  Oracle ERP   │
                              │   (FCCORP)    │
                              └───────────────┘
```

---

## Módulos Funcionais

| Módulo | Descrição | Status |
|--------|-----------|--------|
| **Painel** | Resumo fiscal consolidado + créditos em risco | Ativo |
| **Notas Importadas** | Consulta NF-e Entrada/Saída + CT-e Entrada | Ativo |
| **Importações** | Upload XMLs fiscais + importação via ERP Bridge | Ativo |
| **CGIBS** | Apuração IBS via CGIBS (Câmara IBS) | Ativo |
| **Receita Federal** | Apuração CBS + créditos + débitos + pagamentos | Ativo |
| **Malha Fina** | Divergências RFB × base local por documento | Ativo |
| **Portal Argus (Demo)** | Portal fiscal CBS/IBS — dados mock para apresentação | Demo |
| **Configurações** | Alíquotas, CFOP, fornecedores, ambientes, usuários | Ativo |

---

## Multi-tenancy

A hierarquia de tenants é:

```
Ambiente (Environment)
└── Grupo de Empresas (enterprise_groups)
    └── Empresa (companies)  ← company_id em todas as tabelas de dados
```

Cada requisição autentica o `company_id` via:
1. Header `X-Company-ID` (override explícito)
2. `preferred_company_id` do usuário owner
3. `preferred_company_id` do usuário membro

---

## Autenticação

- **JWT HS256** (30 minutos) transportado como `Authorization: Bearer {token}`
- **Refresh token** (7 dias) em cookie `HttpOnly; SameSite=Strict`
- **ERP Bridge**: Autenticado via `X-API-Key` (SHA256 hash no banco) — sem JWT
- **Webhook RFB**: Validado via HMAC-SHA256 do payload com `JWT_SECRET`

---

## Implantação

| Ambiente | URL | Branch |
|----------|-----|--------|
| Produção | `fctax.fcxlabs.com` / `apuracao.fbtax.cloud` | `main` |
| ERP Bridge | AWS (servidor interno cliente) | manual / systemd |

---

## Documentação Disponível

- [Índice Master](./index.md)
- [Arquitetura Backend Go](./architecture-backend.md)
- [Arquitetura Frontend React](./architecture-frontend.md)
- [Arquitetura ERP Bridge Python](./architecture-erp-bridge.md)
- [Contratos de API](./api-contracts-backend.md)
- [Modelos de Dados](./data-models-backend.md)
- [Inventário de Componentes Frontend](./component-inventory-frontend.md)
- [Arquitetura de Integração](./integration-architecture.md)
- [Guia de Desenvolvimento](./development-guide.md)
- [Guia de Deploy](./deployment-guide.md)
- [Análise da Árvore de Fontes](./source-tree-analysis.md)
