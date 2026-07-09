# Arquivo — Planejamento FASE 2 (Multi-Tenancy) — fev/2026

Artefatos do ciclo de planejamento BMAD da era **FB_APU01** (fev–mai/2026),
arquivados em 2026-07-09 ao iniciar novo ciclo de planejamento da fase 2
da apuração assistida no **FB_APU02**.

## Por que foi arquivado

O escopo original (Multi-Tenancy nativo com RLS + Motor IBS/CBS) foi definido
antes da evolução do projeto: apuração assistida RFB/CGIBS, ERP Bridge SAP,
malha fina e conciliação de pagamentos foram entregues por outros caminhos
(GSD workflow, milestone v1.0 de estabilização).

## Estado na data do arquivamento

- **Story 1-1** (TenantContext struct + helpers): ✅ entregue — `backend/middleware/tenant.go` existe com testes, mas **nenhum handler o utiliza**
- **Stories 1-2 a 5-4** (19 stories): não iniciadas
- **RLS**: nenhuma migration criada
- Conteúdo ainda útil como referência: os 11 ADRs em `architecture-multi-tenancy.md` (RLS, JWT, desnormalização) continuam tecnicamente válidos caso multi-tenancy robusto volte ao roadmap

## Conteúdo

| Arquivo | Descrição |
|---------|-----------|
| `prd-multi-tenancy.md` | PRD Multi-Tenancy (Proposed) |
| `architecture-multi-tenancy.md` | 11 ADRs — RLS, JWT, desnormalização, planos |
| `epics-multi-tenancy.md` | 5 epics, 20 stories com acceptance criteria BDD |
| `prd-ai-reports.md` | PRD Relatórios com IA (nunca iniciado) |
| `bmm-workflow-status.yaml` | Status do workflow BMM na época |
| `sprint-status.yaml` | Sprint status (epic-1 in-progress, story 1-1 done) |
| `tech-spec-wip.md` | Tech spec em andamento (incompleta) |
| `1-1-criar-struct-tenantcontext-e-helpers.md` | Única story implementada |
| `index.md` | Índice original dos artefatos |
