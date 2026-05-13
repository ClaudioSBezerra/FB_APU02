---
phase: 03-debito-tecnico
verified: 2026-05-12T00:00:00Z
status: gaps_found
score: 4/5
overrides_applied: 0
gaps:
  - truth: "Nenhum arquivo rastreado pelo git contem senha hardcoded de ambiente de desenvolvimento"
    status: partial
    reason: >
      Clause 4a (backend/tools/ removido do git) foi atendida. Clause 4b (nenhum tracked file
      com credencial hardcoded) nao foi atendida: main.go linha 68 tem
      postgres://postgres:postgres hardcoded como fallback dev; .env.FB_APU01 esta
      rastreado e contem DB_PASSWORD=postgres; backend/fb_apu02 (binario compilado)
      esta rastreado e contem a mesma string via compilacao de main.go.
      O SUMMARY-04 reconheceu explicitamente os tres como "fora do escopo" e "pre-existentes",
      mas o ROADMAP SC-4 exige que NENHUM arquivo rastreado tenha isso.
    artifacts:
      - path: "backend/main.go"
        issue: "Linha 68: postgres://postgres:postgres hardcoded como fallback de conexao dev"
      - path: ".env.FB_APU01"
        issue: "Arquivo .env rastreado com DB_PASSWORD=postgres; deveria estar no .gitignore"
      - path: "backend/fb_apu02"
        issue: "Binario compilado rastreado; contem string postgres://postgres:postgres; nao deve estar no git"
    missing:
      - "Adicionar backend/fb_apu02 ao .gitignore e remover do rastreamento git via git rm --cached"
      - "Adicionar .env.FB_APU01 ao .gitignore e remover do rastreamento git via git rm --cached"
      - "Avaliar se main.go linha 68 precisa ser mantida ou substituida por erro explicito quando DATABASE_URL nao esta definida em producao"
---

# Fase 3: Debito Tecnico — Relatorio de Verificacao

**Objetivo da Fase:** O codebase nao tem migrations com numeracao ambigua, nao silencia falhas de escrita no banco, nao aloca recursos inutilizados em producao e documentou a decisao sobre o middleware de tenant.

**Verificado em:** 2026-05-12
**Status:** GAPS FOUND
**Re-verificacao:** Nao — verificacao inicial

---

## Conquista do Objetivo

### Verdades Observaveis

| # | Verdade (ROADMAP SC) | Status | Evidencia |
|---|----------------------|--------|-----------|
| 1 | Nao existem dois arquivos de migration com o mesmo numero prefixo; a ordem de aplicacao e deterministicamente alfabetica | VERIFICADO | `021_create_mv_mercadorias.sql` e `021b_ensure_admin_user.sql` tem prefixos distintos ("021_" vs "021b_"). `filepath.Glob` retorna resultados em ordem lexicografica — 021_ < 021b_ deterministica. Idem para 061/061b. |
| 2 | Toda chamada `db.Exec` relevante nos handlers verifica o erro retornado; falhas de escrita geram log e resposta HTTP de erro | VERIFICADO | Nenhuma linha `db.Exec`/`tx.Exec` sem variavel de retorno em nenhum handler (grep confirma). auth.go:749 usa `_, _ =` intencionalmente (ON CONFLICT DO NOTHING idempotente — excecao documentada). 21 chamadas corrigidas em 5 arquivos. |
| 3 | `docker-compose.prod.yml` nao define servico Redis; o container nao sobe em producao | VERIFICADO | `grep -ci "redis" docker-compose.prod.yml` retorna 0. |
| 4 | Os arquivos `backend/tools/debug_*.go` e `verify_data.go` nao existem no repositorio (ou estao no `.gitignore`) e nenhum arquivo rastreado pelo git contem senha hardcoded de ambiente de desenvolvimento | FALHOU (parcial) | Clause 4a PASSOU: `git ls-files backend/tools/` retorna 0 linhas; `.gitignore` tem entrada `backend/tools/`. Clause 4b FALHOU: tres arquivos rastreados contem credenciais hardcoded (ver Gaps). |
| 5 | Ha uma decisao documentada em PROJECT.md sobre `middleware/tenant.go`: aplicado (com handlers migrados) ou diferido para v2 com justificativa | VERIFICADO | `PROJECT.md` linha 109+: secao "middleware/tenant.go — Diferido para v2 (2026-05-12)" com contexto tecnico, justificativa de 3 razoes e plano de acao para v2. |

**Pontuacao:** 4/5 verdades verificadas

---

## Artefatos Requeridos

| Artefato | Esperado | Status | Detalhes |
|----------|----------|--------|----------|
| `backend/migrations/021b_ensure_admin_user.sql` | Migration de admin user com numero nao-conflitante | VERIFICADO | Arquivo presente; usa `IF EXISTS` para idempotencia |
| `backend/migrations/061b_user_environments_preferred_company.sql` | Migration de preferred company com numero nao-conflitante | VERIFICADO | Arquivo presente; usa `ADD COLUMN IF NOT EXISTS` para idempotencia |
| `backend/handlers/erp_bridge.go` | Handler com todos db.Exec checando erro | VERIFICADO | Todas as 12 chamadas corrigidas — mix de `sanitizeDBErr` (critico) e `log.Printf` (best-effort) |
| `backend/handlers/admin.go` | Admin handler com REFRESH MATERIALIZED VIEW checando erro | VERIFICADO | Chamadas corrigidas em linhas 185, 191, 197, 440, 442 |
| `.gitignore` | Entrada para `backend/tools/` | VERIFICADO | Linha presente: `# Debug tools (local only — contain dev credentials)\nbackend/tools/` |
| `backend/fb_apu02` (nao rastreado) | Binario nao deve estar no git | FALHOU | `git ls-files backend/fb_apu02` retorna o arquivo — binario compilado ainda rastreado |
| `.env.FB_APU01` (nao rastreado) | Arquivo .env nao deve estar no git | FALHOU | `git ls-files .env.FB_APU01` retorna o arquivo — .env rastreado com DB_PASSWORD=postgres |

---

## Verificacao de Vinculos (Key Links)

| De | Para | Via | Status | Detalhes |
|----|------|-----|--------|----------|
| `erp_bridge.go` db.Exec | tratamento de erro | `execErr != nil` / `sanitizeDBErr` | VERIFICADO | Linha 421 checa `execErr`; linha 558 usa `sanitizeDBErr + return` |
| `admin.go` REFRESH VIEW | log de erro | `log.Printf` | VERIFICADO | Goroutines usam `log.Printf` (sem acesso a `w`); linhas 185, 191, 197 |
| `.gitignore` | `backend/tools/` | entrada direta | VERIFICADO | Linha presente com comentario explicativo |

---

## Build Check

| Verificacao | Comando | Resultado | Status |
|-------------|---------|-----------|--------|
| Compilacao Go | `go build ./...` | Exit code 0, sem saida | PASSOU |

---

## Anti-Padroes Encontrados

| Arquivo | Linha | Padrao | Severidade | Impacto |
|---------|-------|--------|------------|---------|
| `backend/main.go` | 68 | `postgres://postgres:postgres@localhost:5432/fiscal_db` hardcoded como fallback | AVISO | Expoe credencial dev no repositorio; nao afeta producao (DATABASE_URL sobrescreve), mas viola SC-4 |
| `.env.FB_APU01` | 5 | `DB_PASSWORD=postgres` em arquivo .env rastreado pelo git | BLOQUEADOR | Arquivo .env nao deve ser rastreado pelo git (mesmo com senha "trivial") |
| `backend/fb_apu02` | — | Binario compilado rastreado pelo git | BLOQUEADOR | Binarios nao devem estar no git; este contem strings de credencial compiladas de main.go |

Nenhum marcador TBD/FIXME/XXX encontrado nos arquivos modificados nesta fase.

---

## Verificacao Humana Necessaria

Nenhuma — todas as verificacoes foram realizadas programaticamente.

---

## Resumo dos Gaps

**1 gap bloqueia o objetivo completo da fase.**

**SC-4 parcialmente atendida:** A parte principal (remover `backend/tools/` do git) foi executada corretamente com `git rm --cached` e `.gitignore` atualizado. Porem, o ROADMAP SC-4 tem uma segunda clausula — "nenhum arquivo rastreado pelo git contem senha hardcoded de ambiente de desenvolvimento" — que continua violada por tres arquivos pre-existentes que o PLAN-04 reconheceu mas nao corrigiu.

Os tres itens restantes sao:

1. **`backend/main.go` linha 68** — fallback de conexao dev (`postgres://postgres:postgres`). O SUMMARY-04 classificou como "intencional como fallback local". Em producao DATABASE_URL e sempre definida, entao o risco e baixo. Pode ser substituido por um erro explicito (`log.Fatal("DATABASE_URL nao definida")`) para eliminar o gap sem perder funcionalidade de dev.

2. **`.env.FB_APU01`** — arquivo .env com credenciais simples rastreado desde o commit inicial (bb93982). Deve ser adicionado ao `.gitignore` e removido do rastreamento via `git rm --cached`.

3. **`backend/fb_apu02`** — binario compilado do backend rastreado no git. Deve ser adicionado ao `.gitignore` e removido via `git rm --cached`.

Os itens 2 e 3 sao corridas rapidas (2 comandos git + 1 linha no .gitignore cada). O item 1 requer decisao sobre se um erro explicito e preferivel ao fallback silencioso.

**Gaps estruturados no frontmatter para `/gsd-plan-phase --gaps`.**

---

_Verificado em: 2026-05-12_
_Verificador: Claude (gsd-verifier)_
