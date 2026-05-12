# Requirements: FB_APU02 — Estabilização

**Defined:** 2026-05-12
**Core Value:** Apuração fiscal correta e confiável por empresa, sem vazamento de dados entre tenants.

## v1 Requirements

### Segurança

- [ ] **SEC-01**: Credencial de ERP bridge removida do `config.yaml`, rotacionada e expurgada do histórico git
- [ ] **SEC-02**: Rate limiter `LoginRL` aplicado na rota `/auth/login` (código já existe em `handlers/auth.go`)
- [ ] **SEC-03**: JWT de autenticação migrado de `localStorage` para `sessionStorage`
- [x] **SEC-04**: Mensagens de erro interno do PostgreSQL sanitizadas — cliente recebe mensagem genérica, erro real vai para log

### Bugs

- [x] **BUG-01**: Chave de `localStorage` para company ID unificada em constante única — eliminar as 3 variantes (`companyId`, `company_id`, `selectedCompanyId`)
- [ ] **BUG-02**: `PainelApuracaoCBS.tsx` e `PainelApuracaoIBS.tsx` corrigidos para ler a chave unificada (empresa sempre correta)
- [ ] **BUG-03**: `Managers.tsx` corrigido — lia `selectedCompanyId` que nunca é escrita por nenhum componente

### Débito Técnico

- [ ] **DEBT-01**: Migrations com números duplicados renumeradas — `021` (2 arquivos) e `061` (2 arquivos)
- [ ] **DEBT-02**: Chamadas `db.Exec` sem verificação de erro corrigidas — ao menos 15 ocorrências silenciosas nos handlers
- [ ] **DEBT-03**: Redis removido do `docker-compose.prod.yml` — definido mas sem nenhum uso no código Go (256 MB alocados em vão)
- [ ] **DEBT-04**: Ferramentas de debug com senhas hardcoded removidas ou excluídas do git (`backend/tools/debug_*.go`, `verify_data.go`)
- [ ] **DEBT-05**: Middleware `backend/middleware/tenant.go` avaliado — decisão documentada sobre aplicar ou não nos handlers críticos; se aplicado, handlers migrados

### Qualidade

- [ ] **QA-01**: Vitest configurado corretamente no frontend — dependência instalada, script `test` no `package.json`, rodar `vitest run` sem erros
- [ ] **QA-02**: Testes unitários para handlers críticos do backend: `auth.go`, `filiais.go`, `nfe_entradas.go`, `nfe_saidas.go`
- [ ] **QA-03**: Step de testes adicionado ao pipeline CI — `deploy-production.yml` e `deploy-staging.yml` rodam testes antes do build Docker

## v2 Requirements

### Segurança avançada

- **SEC-V2-01**: JWT migrado para `httpOnly` cookie (elimina risco XSS completamente)
- **SEC-V2-02**: CSP refinada — remover `unsafe-inline` e usar nonces

### Testes

- **QA-V2-01**: Cobertura de testes expandida para todos os 24 handlers do backend
- **QA-V2-02**: Testes de integração E2E com banco de dados real (não soft-fail)
- **QA-V2-03**: Threshold de cobertura configurado no CI (mínimo a definir)

### Multi-tenancy

- **MULTI-01**: Middleware RLS (`middleware/tenant.go`) aplicado em todos os handlers (se DEBT-05 confirmar viabilidade)

## Out of Scope

| Feature | Motivo |
|---------|--------|
| Novas funcionalidades fiscais | Este milestone é exclusivamente de estabilização |
| Redesign de UI | Não é bloqueador — layout atual funciona |
| Migração de banco de dados | Sem necessidade identificada |
| Mobile app | Fora do escopo do projeto |

## Traceability

| Requisito | Fase | Status |
|-----------|------|--------|
| SEC-01 | Fase 1 | Pending |
| SEC-02 | Fase 1 | Pending |
| SEC-03 | Fase 1 | Pending |
| SEC-04 | Fase 1 | Complete |
| BUG-01 | Fase 2 | Complete |
| BUG-02 | Fase 2 | Pending |
| BUG-03 | Fase 2 | Pending |
| DEBT-01 | Fase 3 | Pending |
| DEBT-02 | Fase 3 | Pending |
| DEBT-03 | Fase 3 | Pending |
| DEBT-04 | Fase 3 | Pending |
| DEBT-05 | Fase 3 | Pending |
| QA-01 | Fase 4 | Pending |
| QA-02 | Fase 4 | Pending |
| QA-03 | Fase 4 | Pending |

**Cobertura:**
- Requisitos v1: 15 total
- Mapeados em fases: 15
- Sem fase: 0 ✓

---
*Requirements defined: 2026-05-12*
*Last updated: 2026-05-12 após definição inicial*
