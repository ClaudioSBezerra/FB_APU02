---
phase: 01-seguranca
verified: 2026-05-12T14:30:00Z
status: gaps_found
score: 6/8 must-haves verified
overrides_applied: 0
gaps:
  - truth: "O histórico git não contém mais a senha original (commit de expurgo aplicado)"
    status: failed
    reason: "Dois commits ainda contêm a senha '[REDACTED]' em texto plano no blob de config.yaml: commit 402bc32 ('feat: ERP Bridge v2') e commit 01e5d74 ('Salvar backup do projeto'). O git filter-branch foi executado mas não removeu esses commits — possivelmente porque a reescrita de história foi aplicada num worktree separado e não propagada para o branch main do repositório principal."
    artifacts:
      - path: "erp-bridge-aws/config.yaml"
        issue: "Arquivo atual contém placeholder correto, mas o blob da senha ainda existe em 2 commits do histórico local (402bc32 e 01e5d74)"
    missing:
      - "Re-executar git filter-branch (ou git-filter-repo) no repositório principal para expurgar config.yaml de todos os commits"
      - "Executar git gc --prune=now --aggressive após o filter para remover os blobs órfãos"
      - "Verificar com 'git log -p --all | grep [REDACTED]' — deve retornar 0"

  - truth: "Nenhuma resposta HTTP enviada ao cliente contém strings internas do PostgreSQL (nomes de tabela, query text, constraint names)"
    status: partial
    reason: "Os 9 handlers do escopo do plano estão corrigidos (0 vazamentos). Porém 95 ocorrências de err.Error() permanecem em handlers fora do escopo: environment.go, cfop.go, rfb_creditos.go, forn_simples.go, cgibs_credentials.go, rfb_credentials.go, rfb_debitos_lista.go, cgibs_debitos.go, managers.go. O SUMMARY documenta isso como 'deferred items', mas o success criterion do ROADMAP exige que 'erros internos nunca aparecem na resposta HTTP' — sem qualificador de escopo."
    artifacts:
      - path: "backend/handlers/rfb_credentials.go"
        issue: "6 ocorrências de err.Error() expostos ao cliente"
      - path: "backend/handlers/environment.go"
        issue: "17 ocorrências (estimado pelo SUMMARY)"
      - path: "backend/handlers/rfb_creditos.go"
        issue: "14 ocorrências (estimado pelo SUMMARY)"
      - path: "backend/handlers/cfop.go"
        issue: "5 ocorrências confirmadas"
      - path: "backend/handlers/forn_simples.go"
        issue: "5 ocorrências confirmadas"
      - path: "backend/handlers/managers.go"
        issue: "1 ocorrência confirmada"
    missing:
      - "Aplicar sanitizeDBErr nos handlers out-of-scope listados acima"
      - "Ou criar um plano de fase para cobrir esses 50+ vazamentos restantes"

deferred: []

human_verification:
  - test: "Verificar se a senha foi expurgada do repositório remoto (origin)"
    expected: "git push --force-with-lease origin main foi executado e o repositório remoto não contém mais o histórico com a senha"
    why_human: "Não é possível verificar o estado do repositório remoto sem credenciais de acesso ao GitHub. O SUMMARY documenta explicitamente que 'o repositório remoto ainda contém o histórico antigo com a senha'."
  - test: "Fechar o browser completamente e verificar que a sessão é encerrada"
    expected: "Após fechar e reabrir o browser, a aplicação redireciona para login — sessionStorage foi limpo"
    why_human: "Comportamento de sessionStorage ao fechar browser não é verificável via análise estática de código"
  - test: "Fazer 6 tentativas de login consecutivas e verificar HTTP 429 na 6ª"
    expected: "Tentativas 1-5 retornam HTTP 401; tentativa 6 retorna HTTP 429 sem consultar o banco"
    why_human: "Teste funcional de rate limiting requer servidor em execução"
---

# Phase 01: Segurança — Verification Report

**Phase Goal:** Corrigir as 4 vulnerabilidades de segurança críticas (SEC-01 a SEC-04) antes de qualquer nova feature.
**Verified:** 2026-05-12T14:30:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `erp-bridge-aws/config.yaml` não contém a senha real do Oracle ERP | ✓ VERIFIED | Arquivo contém `"SENHA_ROTACIONADA_CONFIGURE_VIA_ENV"` no campo fbtax.password |
| 2 | `erp-bridge-aws/config.yaml` está listado no `.gitignore` e não será rastreado | ✓ VERIFIED | `.gitignore` linha 51; `git check-ignore -v` confirma |
| 3 | O histórico git não contém mais a senha original (commit de expurgo aplicado) | ✗ FAILED | `git log -p --all` retorna 4 hits de "[REDACTED]"; commits 402bc32 e 01e5d74 ainda têm o blob com a senha em texto plano |
| 4 | A credencial original foi rotacionada no sistema de origem | ? UNCERTAIN | Confirmado pelo usuário no fluxo de execução; não verificável programaticamente |
| 5 | `LoginRL.Allow(ip)` é chamado no início de LoginHandler, antes de qualquer consulta ao banco | ✓ VERIFIED | `auth.go` linhas 601-605: `ip := GetClientIP(r)` + `if !LoginRL.Allow(ip)` são as primeiras instruções do closure, antes de `json.NewDecoder` |
| 6 | Após login bem-sucedido, o JWT está em `sessionStorage['token']`, não em `localStorage['token']` | ✓ VERIFIED | `localStorage.*token` = 0 ocorrências; `sessionStorage.setItem('token'` = 1; `sessionStorage.getItem('token'` = 2 em `AuthContext.tsx` |
| 7 | A função `sanitizeDBErr` está disponível em `config.go` e é usada pelos handlers do plano | ✓ VERIFIED | `func sanitizeDBErr` em `config.go` linha 34; 42 usos confirmados; 9 handlers do plano têm 0 vazamentos de `err.Error()` |
| 8 | Nenhuma resposta HTTP enviada ao cliente contém strings internas do PostgreSQL | ✗ FAILED | 95 ocorrências de `err.Error()` permanecem em handlers fora do escopo do plano (environment.go, rfb_credentials.go, cfop.go, rfb_creditos.go, forn_simples.go, cgibs_credentials.go, rfb_debitos_lista.go, cgibs_debitos.go, managers.go). O success criterion do ROADMAP não tem qualificador de escopo. |

**Score:** 5/8 truths verified (1 uncertain, 2 failed)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `erp-bridge-aws/config.yaml` | Placeholder no campo de senha, sem senha real | ✓ VERIFIED | Contém `"SENHA_ROTACIONADA_CONFIGURE_VIA_ENV"` |
| `.gitignore` | Entrada `erp-bridge-aws/config.yaml` | ✓ VERIFIED | Linha 51 do .gitignore |
| `backend/handlers/auth.go` | `LoginRL.Allow(ip)` como primeiras linhas de LoginHandler | ✓ VERIFIED | Linhas 601-605 confirmam padrão idêntico ao RegisterHandler |
| `backend/handlers/login_ratelimit_test.go` | Teste unitário para rate limiter | ✓ VERIFIED | Arquivo existe em `backend/handlers/` |
| `frontend/src/contexts/AuthContext.tsx` | `sessionStorage.setItem('token'` no bloco de login | ✓ VERIFIED | 1 setItem, 2 getItem, 0 localStorage com token |
| `backend/handlers/config.go` | `func sanitizeDBErr` | ✓ VERIFIED | Linha 34 |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `.gitignore` | `erp-bridge-aws/config.yaml` | entrada de gitignore | ✓ WIRED | `git check-ignore -v` confirma exclusão |
| `backend/main.go` | `handlers.LoginHandler` | `LoginRL` wrapper | ✓ WIRED | `go build` passa; `LoginRL.Allow` em linha 602 do handler |
| `AuthContext.tsx` | `sessionStorage` | `sessionStorage.setItem/getItem('token')` | ✓ WIRED | Sem ocorrências de localStorage com 'token' |
| `backend/handlers/config.go` | handlers do plano | chamadas a `sanitizeDBErr` | ✓ WIRED | 42 ocorrências nos 9 handlers; 0 `err.Error()` nos handlers do escopo |
| git history | config.yaml blob | `git filter-branch` | ✗ NOT_WIRED | Commits 402bc32 e 01e5d74 ainda contêm `password: "[REDACTED]"` |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Backend compila sem erros | `go build ./...` no diretório backend | BUILD OK | ✓ PASS |
| `sanitizeDBErr` usada nos 9 handlers do plano | `grep -rn sanitizeDBErr handlers/ \| wc -l` | 42 ocorrências | ✓ PASS |
| Zero `err.Error()` expostos nos handlers do plano | loop grep nos 9 arquivos alvo | 0 em todos os 9 | ✓ PASS |
| Zero `localStorage` com 'token' no AuthContext | `grep -c "localStorage.*token"` | 0 | ✓ PASS |
| Senha não existe no histórico | `git log -p --all \| grep [REDACTED]` | 4 hits (2 commits com senha real, 2 no SUMMARY) | ✗ FAIL |

### Requirements Coverage

| Requisito | Plano | Descrição | Status | Evidência |
|-----------|-------|-----------|--------|-----------|
| SEC-01 | 01-01 | Credencial removida, gitignore e histórico expurgado | ✗ PARCIAL | Arquivo limpo e gitignore OK; histórico NÃO expurgado — 2 commits ainda contêm a senha |
| SEC-02 | 01-02 | Rate limiter `LoginRL` aplicado na rota `/auth/login` | ✓ SATISFIED | `LoginRL.Allow(ip)` na linha 602 de auth.go, antes de qualquer acesso ao banco |
| SEC-03 | 01-03 | JWT migrado de `localStorage` para `sessionStorage` | ✓ SATISFIED | AuthContext.tsx sem localStorage para 'token'; sessionStorage.setItem/getItem confirmados |
| SEC-04 | 01-04 | Mensagens de erro PostgreSQL sanitizadas | ✗ PARCIAL | 9 handlers do escopo corrigidos (42 usos de sanitizeDBErr); mas 95 ocorrências de err.Error() permanecem em outros handlers |

### Anti-Patterns Found

| Arquivo | Linhas | Pattern | Severidade | Impacto |
|---------|--------|---------|------------|---------|
| `backend/handlers/rfb_credentials.go` | 42, 65, 100, 151, 204, 228, 260, 266 | `err.Error()` exposto ao cliente | ⚠️ Warning | Vaza detalhes internos de queries PostgreSQL |
| `backend/handlers/environment.go` | ~17 ocorrências | `err.Error()` exposto ao cliente | ⚠️ Warning | Handlers de alto uso operacional |
| `backend/handlers/rfb_creditos.go` | ~14 ocorrências | `err.Error()` exposto ao cliente | ⚠️ Warning | Handler de créditos RFB |
| `backend/handlers/cfop.go` | 106, 118, 125, 137, 174 | `err.Error()` exposto ao cliente | ⚠️ Warning | Handler de tabela de referência |
| `backend/handlers/forn_simples.go` | 146, 153, 166, 196 | `err.Error()` exposto ao cliente | ⚠️ Warning | Handler de fornecedores Simples Nacional |
| `backend/handlers/managers.go` | 40 | `err.Error()` exposto ao cliente | ⚠️ Warning | Handler de gestão de managers |
| git history (commits 402bc32, 01e5d74) | — | Senha `[REDACTED]` em blob de config.yaml | 🛑 BLOCKER | Credencial exposta a qualquer pessoa com acesso ao repositório local ou ao remote não atualizado |

### Human Verification Required

#### 1. Estado do repositório remoto (origin)

**Test:** Executar `git log --all --oneline | wc -l` no repositório remoto (GitHub) e verificar se commits com config.yaml ainda existem
**Expected:** Repositório remoto não contém commits com `[REDACTED]` — force-push foi executado
**Why human:** Não é possível verificar o estado do remote sem credenciais. O SUMMARY alerta explicitamente: "o repositório remoto ainda contém o histórico antigo com a senha"

#### 2. Sessão encerra ao fechar browser

**Test:** Fazer login, fechar o browser completamente (não só a aba), reabrir e navegar para a aplicação
**Expected:** Aplicação redireciona para tela de login — sessionStorage foi limpo pelo browser
**Why human:** Comportamento de sessionStorage ao fechar browser não é verificável via análise estática

#### 3. Rate limiter retorna HTTP 429 na 6ª tentativa

**Test:** `for i in $(seq 1 6); do curl -s -o /dev/null -w "Tentativa $i: HTTP %{http_code}\n" -X POST http://localhost:8081/api/auth/login -H "Content-Type: application/json" -d '{"email":"test@test.com","password":"wrong"}'; done`
**Expected:** Tentativas 1-5 retornam 401; tentativa 6 retorna 429
**Why human:** Requer servidor em execução

### Gaps Summary

**Dois gaps bloqueiam o objetivo da fase:**

**Gap 1 — SEC-01 CRÍTICO: senha ainda no histórico git local**

O `git filter-branch` foi executado mas em um worktree separado. O repositório principal (branch `main`) ainda contém os commits `402bc32` e `01e5d74` com `password: "[REDACTED]"` em texto plano no blob de `erp-bridge-aws/config.yaml`. Qualquer `git clone` deste repositório expõe a senha. O repositório remoto (`origin`) também não foi atualizado conforme alertado no próprio SUMMARY.

Correção necessária: re-executar `git filter-branch` ou `git filter-repo` diretamente no branch principal, seguido de `git gc --prune=now --aggressive` e `git push --force-with-lease origin main`.

**Gap 2 — SEC-04 PARCIAL: 95 handlers fora do escopo ainda expõem err.Error()**

O success criterion do ROADMAP para Phase 1 é absoluto: "Erros internos do PostgreSQL nunca aparecem na resposta HTTP". Os 9 handlers do plano estão corrigidos, mas 8 outros handlers (`environment.go`, `rfb_credentials.go`, `cfop.go`, `rfb_creditos.go`, `forn_simples.go`, `cgibs_credentials.go`, `rfb_debitos_lista.go`, `managers.go`) somam aproximadamente 50-95 vazamentos. O SUMMARY documenta como "deferred items" mas o critério de sucesso não tem essa ressalva.

Opções: (a) corrigir os handlers restantes ou (b) criar override documentando que o critério de Phase 1 cobre apenas os 9 handlers críticos do plano, com os demais endereçados em plano subsequente.

---

_Verified: 2026-05-12T14:30:00Z_
_Verifier: Claude (gsd-verifier)_
