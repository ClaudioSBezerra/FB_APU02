# FB_APU02 — Sistema de Apuração Fiscal

## What This Is

Plataforma web de apuração fiscal tributária para o grupo Ferreira Costa. Processa arquivos SPED, NF-e e CT-e, apura créditos e débitos de PIS/COFINS, ICMS, IBS e CBS, integra com RFB e CGIBS, e oferece malha fina de divergências. Multi-tenant com hierarquia Ambiente → Grupo de Empresas → Empresa. Backend Go + Frontend React + PostgreSQL + ERP Bridge Python na AWS.

## Core Value

Apuração fiscal correta e confiável por empresa, sem vazamento de dados entre tenants.

## Requirements

### Validated

- ✓ Upload e processamento de SPED fiscal (EFD-ICMS, EFD-Contribuições) — existente
- ✓ Importação de XML de NF-e (entradas e saídas) e CT-e — existente
- ✓ Apuração de créditos e débitos PIS/COFINS, ICMS, CBS, IBS — existente
- ✓ Malha Fina (divergências NF-e vs SPED por filial/período) — existente
- ✓ Integração RFB (credenciais, dívidas, apuração, créditos CBS) — existente
- ✓ Integração CGIBS (credenciais, débitos, apuração) — existente
- ✓ Multi-tenancy lógico: Ambiente → Grupo de Empresas → Empresa — existente
- ✓ Autenticação JWT com roles (admin, user) — existente
- ✓ Tabelas de referência: CFOP, alíquotas, fornecedores Simples Nacional — existente
- ✓ ERP Bridge — daemon Python na AWS sincronizando dados do ERP — existente
- ✓ Gestão de ambiente (usuários, empresas, managers, filiais) — existente
- ✓ Dashboard de apuração e painel de créditos perdidos — existente
- ✓ Logs de atividade de usuários — existente

### Active

#### Segurança
- [ ] **SEC-01**: Credencial de ERP bridge removida/rotacionada e excluída do histórico git
- [ ] **SEC-02**: Rate limiter `LoginRL` aplicado na rota de login (já implementado, nunca aplicado)
- [ ] **SEC-03**: JWT migrado de `localStorage` para `sessionStorage` (redução de risco XSS)
- [ ] **SEC-04**: Mensagens de erro interno do PostgreSQL sanitizadas antes de retornar ao cliente HTTP

#### Bugs
- [ ] **BUG-01**: Chave de `localStorage` para company ID unificada (3 variantes causando empresa errada)
- [ ] **BUG-02**: `PainelApuracaoCBS.tsx` e `PainelApuracaoIBS.tsx` corrigidos para ler chave correta
- [ ] **BUG-03**: `Managers.tsx` corrigido — lia chave que nunca é escrita (`selectedCompanyId`)

#### Débito Técnico
- [ ] **DEBT-01**: Migrations com números duplicados renumeradas (021 e 061 cada uma com 2 arquivos)
- [ ] **DEBT-02**: Chamadas `db.Exec` sem verificação de erro corrigidas (15+ ocorrências silenciosas)
- [ ] **DEBT-03**: Redis removido do `docker-compose.prod.yml` (alocado mas sem uso no código Go)
- [ ] **DEBT-04**: Ferramentas de debug com senhas hardcoded removidas ou movidas para `.gitignore`
- [ ] **DEBT-05**: Avaliação e aplicação do `middleware/tenant.go` nos handlers críticos

#### Qualidade
- [ ] **QA-01**: Vitest configurado corretamente no frontend (dependência + scripts de test)
- [ ] **QA-02**: Testes unitários para handlers críticos: auth, filiais, NF-e entradas/saídas
- [ ] **QA-03**: Step de testes adicionado ao pipeline CI (GitHub Actions)

### Out of Scope

- Novas funcionalidades fiscais — foco é estabilização do que existe
- Redesign de UI — não é bloqueador atual
- Migração de banco de dados (PostgreSQL) — sem necessidade identificada
- Migração para `httpOnly` cookie (JWT) — sessionStorage já mitiga suficientemente o risco no contexto atual

## Context

- **Codebase mapeado em:** 2026-05-12 (`.planning/codebase/`)
- **Multi-tenancy atual:** Middleware `backend/middleware/tenant.go` implementado e testado, mas **nenhum handler o aplica** — filtragem por empresa é manual em cada query
- **Hierarquia de tenant:** Ambiente → Grupo de Empresas → Empresa (3 níveis)
- **Deployment:** Coolify (produção) + GitHub Actions CI/CD; Traefik como proxy reverso
- **ERP Bridge:** Daemon Python rodando em EC2 na AWS, sincroniza dados de ERP (Totvs/Oracle) via polling
- **Sem relatos de usuários:** Problemas identificados via análise estática — nenhum bug reportado em produção até 2026-05-12
- **Sem prazo:** Estabilização sem deadline definido — prioridade por impacto/risco

## Constraints

- **Stack:** Go + React/TypeScript + PostgreSQL — sem mudança de tecnologia
- **Backward compatibility:** Migrations existentes não podem ser alteradas, apenas adicionadas
- **Deploy:** Alterações de segurança urgentes (SEC-01, SEC-02) devem ser deployadas assim que prontas
- **Sem novas features:** Este milestone é exclusivamente de estabilização

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Multi-tenancy lógico (3 níveis) | Atende hierarquia de negócio: Ambiente > Grupo > Empresa | — Pending |
| Middleware tenant implementado mas não aplicado | Possivelmente aguardando validação — DEBT-05 avaliará impacto de ativar | — Pending |
| JWT em localStorage | Decisão original; mitigação via sessionStorage planejada (SEC-03) | ⚠️ Revisit |
| Redis no compose de produção | Nunca utilizado pelo código Go — DEBT-03 removerá | ⚠️ Revisit |

## Evolution

Este documento evolui a cada transição de fase e milestone.

**Após cada transição de fase** (via `/gsd-transition`):
1. Requisitos invalidados? → Mover para Out of Scope com motivo
2. Requisitos validados? → Mover para Validated com referência da fase
3. Novos requisitos surgiram? → Adicionar em Active
4. Decisões a registrar? → Adicionar em Key Decisions
5. "What This Is" ainda preciso? → Atualizar se divergiu

**Após cada milestone** (via `/gsd-complete-milestone`):
1. Revisão completa de todas as seções
2. Core Value check — ainda é a prioridade certa?
3. Auditoria de Out of Scope — motivos ainda válidos?
4. Atualizar Context com estado atual

---
*Last updated: 2026-05-12 após inicialização*

## Decisões Arquiteturais

### middleware/tenant.go — Diferido para v2 (2026-05-12)

**Contexto:** `backend/middleware/tenant.go` define `WithTenant(db, ctx, fn)` — um helper
que abre uma transação PostgreSQL e chama `set_config('app.tenant_id', ...)` para que
políticas RLS possam filtrar automaticamente por tenant, sem que cada handler precise
passar `company_id` explicitamente.

**Situação atual:** Nenhum handler usa `WithTenant`. Todos os 40+ handlers usam o padrão
`GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))` + `WHERE company_id = $1`
explícito em cada query SQL.

**Decisão: Diferir para v2.**

**Justificativa:**
1. **Escopo do milestone:** Este milestone é exclusivamente de estabilização — sem
   refatorações de arquitetura. Migrar 40+ handlers para `WithTenant` é uma refatoração
   significativa com risco de regressão.
2. **Migração não-trivial:** `WithTenant` requer que todas as queries dentro de uma
   requisição usem o mesmo `*sql.Tx` (não `*sql.DB`). Cada handler precisaria mudar
   sua assinatura e passar `tx` em vez de `db` em todos os `QueryRow`, `Query` e `Exec`.
3. **Risco mitigado:** Os bugs de company ID que motivariam a migração foram corrigidos
   na Phase 2 (BUG-01, BUG-02, BUG-03). O padrão atual funciona corretamente.
4. **Defesa existente:** `GetEffectiveCompanyID` valida que o usuário tem acesso à empresa
   solicitada antes de retornar o ID — é uma verificação explícita e testável.

**Ação v2:** Quando RLS for ativado em produção, migrar os handlers críticos para
`WithTenant` começando pelos handlers de leitura (sem risco de rollback de escrita).
Prioridade: `nfe_entradas.go`, `nfe_saidas.go`, `rfb_apuracao.go`, `cgibs_apuracao.go`.
