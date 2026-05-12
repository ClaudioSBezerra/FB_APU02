# Roadmap: FB_APU02 — Estabilizacao

## Overview

Milestone de estabilizacao do sistema de apuracao fiscal Ferreira Costa. Sem novas funcionalidades — foco exclusivo em corrigir riscos de seguranca criticos, bugs silenciosos de empresa errada, debito tecnico acumulado, e construir uma base minima de testes e CI confiavel.

## Phases

- [x] **Phase 1: Seguranca** - Remover credenciais expostas, ativar rate limiter, migrar JWT para sessionStorage e sanitizar erros do banco (completed 2026-05-12)
- [ ] **Phase 2: Bugs** - Unificar a chave de company ID no localStorage e corrigir os tres componentes que leem chave errada
- [ ] **Phase 3: Debito Tecnico** - Renumerar migrations duplicadas, corrigir erros silenciosos de DB, remover Redis ocioso e ferramentas de debug, avaliar tenant middleware
- [ ] **Phase 4: Qualidade** - Configurar Vitest, escrever testes unitarios nos handlers criticos e incluir etapa de testes no CI

## Phase Details

### Phase 1: Seguranca
**Goal**: O sistema nao expoe credenciais, nao aceita brute-force no login, armazena JWT com menor superficie de risco e nunca vaza mensagens internas do PostgreSQL para o browser
**Depends on**: Nothing (first phase)
**Requirements**: SEC-01, SEC-02, SEC-03, SEC-04
**Success Criteria** (what must be TRUE):
  1. `erp-bridge-aws/config.yaml` nao contem senha real e esta no `.gitignore`; credencial original rotacionada e expurgada do historico git
  2. Um agente automatizado fazendo mais de 5 tentativas de login em 15 minutos recebe HTTP 429 antes de atingir a 6a tentativa
  3. Apos login bem-sucedido, o JWT esta em `sessionStorage` e nao em `localStorage`; fechar o browser encerra a sessao
  4. Erros internos do PostgreSQL (nomes de tabela, query text) nunca aparecem na resposta HTTP; o browser recebe apenas uma mensagem generica
**Plans**: 7 planos (4 originais + 3 gap closure)
Plans:
- [x] 01-01-PLAN.md — Credencial ERP bridge: remover senha de config.yaml, gitignore e expurgar historico git (SEC-01)
- [x] 01-02-PLAN.md — Rate limiter de login: aplicar LoginRL na rota /api/auth/login (SEC-02)
- [x] 01-03-PLAN.md — JWT sessionStorage: migrar token de localStorage para sessionStorage no AuthContext (SEC-03)
- [x] 01-04-PLAN.md — Sanitizacao de erros DB: helper sanitizeDBErr + correcao de todos os vazamentos de err.Error() (SEC-04)
- [x] 01-05-PLAN.md — [GAP] SEC-01: Re-expurgar historico git (commits 402bc32 e 01e5d74 ainda contem config.yaml)
- [x] 01-06-PLAN.md — [GAP] CR-02+CR-03+WR-03: Fix vazamentos em erp_bridge.go e rfb_apuracao.go + auth check em admin.go
- [x] 01-07-PLAN.md — [GAP] SEC-04 extensao: sanitizeDBErr nos ~12 handlers fora do escopo original

### Phase 2: Bugs
**Goal**: Qualquer pagina do frontend opera sempre com a empresa correta — sem empresa errada silenciosa causada por chaves discrepantes de localStorage
**Depends on**: Phase 1
**Requirements**: BUG-01, BUG-02, BUG-03
**Success Criteria** (what must be TRUE):
  1. Existe uma unica constante exportada para a chave de company ID usada em todo o frontend; busca por `'company_id'` e `'selectedCompanyId'` como strings literais retorna zero ocorrencias relevantes
  2. `PainelApuracaoCBS` e `PainelApuracaoIBS` exibem dados da empresa selecionada pelo usuario, nao de outra empresa
  3. A tela de Managers carrega e exibe dados corretamente sem depender de uma chave de localStorage que nunca e escrita
**Plans**: 2 planos
Plans:
- [ ] 02-01-PLAN.md — BUG-01: Criar storageKeys.ts + corrigir 9 paginas que bypassam o interceptor (Wave 1)
- [ ] 02-02-PLAN.md — BUG-02 + BUG-03: Refatorar PainelApuracaoCBS, PainelApuracaoIBS e Managers para useAuth() (Wave 2)
**UI hint**: yes

### Phase 3: Debito Tecnico
**Goal**: O codebase nao tem migrations com numeracao ambigua, nao silencia falhas de escrita no banco, nao aloca recursos inutilizados em producao e documentou a decisao sobre o middleware de tenant
**Depends on**: Phase 2
**Requirements**: DEBT-01, DEBT-02, DEBT-03, DEBT-04, DEBT-05
**Success Criteria** (what must be TRUE):
  1. Nao existem dois arquivos de migration com o mesmo numero prefixo; a ordem de aplicacao e deterministicamente alfabetica
  2. Toda chamada `db.Exec` relevante nos handlers verifica o erro retornado; falhas de escrita geram log e resposta HTTP de erro
  3. `docker-compose.prod.yml` nao define servico Redis; o container nao sobe em producao
  4. Os arquivos `backend/tools/debug_*.go` e `verify_data.go` nao existem no repositorio (ou estao no `.gitignore`) e nenhum arquivo rastreado pelo git contem senha hardcoded de ambiente de desenvolvimento
  5. Ha uma decisao documentada em PROJECT.md sobre `middleware/tenant.go`: aplicado (com handlers migrados) ou diferido para v2 com justificativa
**Plans**: TBD

### Phase 4: Qualidade
**Goal**: O projeto tem testes executaveis, os handlers criticos tem cobertura unitaria e o pipeline de CI impede deploy sem testes passando
**Depends on**: Phase 3
**Requirements**: QA-01, QA-02, QA-03
**Success Criteria** (what must be TRUE):
  1. `npm test` no diretorio `frontend/` executa o Vitest e conclui sem erros de configuracao ou dependencias faltando
  2. Os handlers `auth.go`, `filiais.go`, `nfe_entradas.go` e `nfe_saidas.go` tem arquivos `*_test.go` com testes unitarios que passam com `go test ./...`
  3. Os workflows `deploy-production.yml` e `deploy-staging.yml` executam os testes (Go e frontend) antes do build Docker; um teste falhando bloqueia o deploy
**Plans**: TBD

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Seguranca | 7/7 | Complete   | 2026-05-12 |
| 2. Bugs | 0/2 | Not started | - |
| 3. Debito Tecnico | 0/? | Not started | - |
| 4. Qualidade | 0/? | Not started | - |
