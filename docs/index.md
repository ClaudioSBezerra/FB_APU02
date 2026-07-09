# Índice de Documentação — FB_APU02

**Projeto:** FB_APU02 — Sistema de Apuração Fiscal  
**Versão:** 2.0.4  
**Tipo:** Monorepo multi-parte (Backend Go + Frontend React + ERP Bridge Python)  
**Gerado em:** 2026-07-09 (Deep Scan)  
**Diretório:** `docs/`

> Este é o ponto de entrada principal para IA (BMAD, Claude Code) e desenvolvedores que precisam de contexto sobre o projeto. Para qualquer trabalho novo neste repositório, comece lendo este índice.

---

## Visão Rápida

| Campo | Valor |
|-------|-------|
| **Domínio** | Apuração fiscal — IBS, CBS, PIS/COFINS, ICMS (Reforma Tributária) |
| **Cliente** | Grupo Ferreira Costa |
| **Produção** | `fctax.fcxlabs.com` / `apuracao.fbtax.cloud` |
| **Backend** | Go 1.22, `net/http` stdlib, `database/sql` + `lib/pq`, PostgreSQL 15 |
| **Frontend** | React 18.3, TypeScript 5.2, Vite 5.2, Tailwind CSS, Radix UI (shadcn/ui) |
| **ERP Bridge** | Python 3 daemon, `oracledb` thin mode, AWS |
| **Padrão back** | Handler Factory Pattern — sem framework de roteamento externo |
| **Multi-tenant** | `company_id` UUID em todas as tabelas fiscais |
| **Auth** | JWT HS256 (30min) + Refresh token HttpOnly (7 dias) |
| **Deploy** | Docker + Docker Compose + Coolify + Traefik + GitHub Actions |

---

## Navegação por Parte do Sistema

### Backend Go (`backend/`)

| Documento | Conteúdo |
|-----------|---------|
| [Arquitetura Backend](./architecture-backend.md) | Stack, padrões, autenticação, segurança, multi-tenancy, serviços background |
| [Contratos de API](./api-contracts-backend.md) | Todos os endpoints REST com payloads e respostas (88+ endpoints) |
| [Modelos de Dados](./data-models-backend.md) | Schema PostgreSQL — 30+ tabelas com colunas, tipos e relações |

### Frontend React (`frontend/`)

| Documento | Conteúdo |
|-----------|---------|
| [Arquitetura Frontend](./architecture-frontend.md) | Stack, roteamento, AuthContext, FilialContext, fetch interceptor, design system |
| [Inventário de Componentes](./component-inventory-frontend.md) | 44 páginas, 9 componentes compartilhados, 46 componentes UI, contextos |

### ERP Bridge Python (`erp-bridge-aws/`)

| Documento | Conteúdo |
|-----------|---------|
| [Arquitetura ERP Bridge](./architecture-erp-bridge.md) | Daemon Python, modos oracle_xml/sap_s4hana, Oracle queries, systemd |

### Integração entre Partes

| Documento | Conteúdo |
|-----------|---------|
| [Arquitetura de Integração](./integration-architecture.md) | Como as 3 partes se comunicam — protocolos, auth, formatos, fluxos |

### Desenvolvimento e Deploy

| Documento | Conteúdo |
|-----------|---------|
| [Guia de Desenvolvimento](./development-guide.md) | Setup local, comandos, convenções, adicionando features |
| [Guia de Deploy](./deployment-guide.md) | CI/CD, Docker, Coolify, ERP Bridge systemd, SSL |
| [Visão Geral do Projeto](./project-overview.md) | Resumo executivo, arquitetura de alto nível, módulos funcionais |
| [Análise da Árvore de Fontes](./source-tree-analysis.md) | Estrutura completa de diretórios anotada |

---

## Documentação Existente (Pré-BMAD)

| Documento | Conteúdo |
|-----------|---------|
| [README.md](../README.md) | Visão geral e instruções de início rápido |
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Documento de arquitetura anterior |
| [API_REFERENCE.md](./API_REFERENCE.md) | Referência de API anterior |
| [COOLIFY_DEPLOY_GUIDE.md](./COOLIFY_DEPLOY_GUIDE.md) | Guia específico de deploy no Coolify |
| [TECHNICAL_SPECS.md](./TECHNICAL_SPECS.md) | Especificações técnicas detalhadas |
| [WORKFLOW_ALM.md](./WORKFLOW_ALM.md) | Workflow de ALM do projeto |
| [ROADMAP.md](./ROADMAP.md) | Roadmap do produto |
| [diagrama_banco_importacao.md](./diagrama_banco_importacao.md) | Diagrama do banco de dados |

---

## Quick Reference para IA

### Padrões que TODA IA deve conhecer neste projeto

**1. Handler Factory (Go):**
```go
func MeuHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        userID := GetUserIDFromContext(r)
        companyID := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
        // ... lógica com WHERE company_id = companyID ...
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
    }
}
// Registrar em main.go:
http.HandleFunc("/api/meu-endpoint", withAuth(handlers.MeuHandler(db), ""))
```

**2. Fetch no Frontend (não adicionar headers — injetados automaticamente):**
```typescript
// CORRETO: sem headers de auth
const res = await fetch('/api/minha-rota')
const data = await res.json()

// ERRADO: não adicionar manualmente
const res = await fetch('/api/minha-rota', {
    headers: { 'Authorization': `Bearer ${token}` }  // ← NÃO FAZER
})
```

**3. Nova Aba de Navegação:**
```typescript
// Sempre editar navigation.ts para adicionar tabs — nunca hardcoded em componentes
// Arquivo: frontend/src/lib/navigation.ts
modulo: {
    label: 'Módulo',
    tabs: [
        { label: 'Nova Aba', path: '/modulo/nova-aba' },
    ],
}
```

**4. Nova Migration:**
```bash
# Nomear com o próximo número disponível APÓS o 113 atual
# backend/migrations/114_descricao.sql
# NUNCA alterar migrations existentes
```

**5. Resposta de Erro:**
```go
// Novos handlers: usar jsonErr
jsonErr(w, http.StatusBadRequest, "Mensagem de erro")

// Para erros de DB: usar sanitizeDBErr
sanitizeDBErr(w, http.StatusInternalServerError, "Erro interno", err, "[MeuHandler]")
```

---

## Módulos Funcionais

| ID Módulo | Label | Rotas Ativas |
|----------|-------|-------------|
| `painel` | Painel | `/painel/resumo-fiscal`, `/apuracao/creditos-perdidos` |
| `notas` | Notas Importadas | `/apuracao/*/notas` |
| `importacoes` | Importações | `/importacoes/*` |
| `cgibs` | CGIBS - Apuração Assistida IBS | `/cgibs/*` |
| `rfb` | Receita Federal - Apuração Assistida | `/rfb/*` |
| `malha` | Malha Fina | `/malha-fina/*` |
| `argus` | Portal Fiscal CBS/IBS (Demo) | `/argus` |
| `config` | Configurações | `/config/*`, `/rfb/credenciais`, `/cgibs/credenciais` |

**Resolução do módulo ativo:** `getActiveModule(pathname)` em `frontend/src/lib/navigation.ts`

---

## Fluxos Críticos de Negócio

### Apuração CBS (RFB)

```
1. Usuário clica "Solicitar" em /rfb/apuracao
2. POST /api/rfb/apuracao/solicitar { competencia, cnpj_base }
3. Backend → OAuth2 GetToken → POST para API RFB → tiquete
4. INSERT rfb_requests status='requested'
5. [aguarda minutos/horas] → RFB chama POST /api/rfb/webhook (HMAC validado)
6. Backend → goroutine download → processamento
7. UPDATE rfb_requests status='completed' + INSERT rfb_debitos + rfb_resumo
8. Usuário vê resultados em /rfb/debitos e /rfb/gestao-creditos
```

### Importação ERP Bridge (SAP)

```
1. Daemon bridge.py roda no AWS (systemd, loop 60s)
2. Poll GET /api/erp-bridge/pending → runs enfileirados pela UI
3. Conecta Oracle FCCORP → SAP_QUERY (JOIN s4i_nfe + impostos)
4. POST /api/erp-bridge/parceiros/sync (lotes 500)
5. POST /api/erp-bridge/import/batch (lotes 1000 documentos JSON)
6. Backend: INSERT nfe_saidas/nfe_entradas/cte_entradas ON CONFLICT DO NOTHING
7. Bridge atualiza watermark SQLite
8. PATCH /api/erp-bridge/runs/{id} status='success'
```

---

## Estado das Funcionalidades

| Funcionalidade | Status | Notas |
|---------------|--------|-------|
| Apuração CBS (RFB) | ✅ Produção | Webhook callback + scheduler automático |
| Apuração IBS (CGIBS) | ✅ Produção | Estrutura espelha RFB |
| Malha Fina NF-e/CT-e | ✅ Produção | Divergências por documento |
| ERP Bridge SAP S/4HANA | ✅ Produção | Daemon AWS, modo sap_s4hana |
| ERP Bridge Oracle XML | ✅ Produção | Modo oracle_xml (Totvs/Protheus) |
| Pagamentos Fornecedores CSV | ✅ Produção | Conciliação CBS |
| Pagamentos CBS | 🚧 Pendente | Tab desabilitada em /rfb |
| Portal Argus | 🎭 Demo | Mock estático via iframe |
| Gestão Eventos Cred/Deb | 🚧 Pendente | Tab desabilitada |
| Créditos IBS (CGIBS) | 🚧 Pendente | Tab desabilitada |

---

## Informações de Segurança

| Aspecto | Detalhe |
|---------|---------|
| JWT expirado em | 30 minutos |
| Refresh token | 7 dias, HttpOnly cookie — perdido em restart do container |
| Senhas | bcrypt custo 14 |
| Rate limiting | Em memória (não distribuído) |
| Multi-tenant | WHERE company_id explícito (sem RLS automático) |
| CORS | Allowlist de origens — não wildcard |
| Webhook RFB | HMAC-SHA256 com JWT_SECRET |
| ERP Bridge | X-API-Key (SHA256 hash no banco) |
| Credentials RFB/CGIBS | Criptografadas com ENCRYPTION_KEY |

---

## Como Usar Esta Documentação com BMAD

Para criar um PRD brownfield ou especificação de nova feature, referencie:

```markdown
# Ao descrever o contexto técnico para o BMAD:
Documentação do projeto em: docs/index.md
Arquitetura backend: docs/architecture-backend.md
Contratos API: docs/api-contracts-backend.md
Schema banco: docs/data-models-backend.md
Componentes frontend: docs/component-inventory-frontend.md
Integração: docs/integration-architecture.md
```

O agente BMAD (PRD writer, architect, dev) deve carregar `docs/index.md` como contexto primário e navegar para os documentos específicos conforme necessário.
