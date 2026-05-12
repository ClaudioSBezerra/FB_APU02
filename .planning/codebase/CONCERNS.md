# Codebase Concerns
<!-- Last mapped: 2026-05-12 -->

## Severity Legend

- 🔴 **Critical** — risco imediato de segurança ou bug em produção
- 🟠 **High** — tech debt com impacto funcional
- 🟡 **Medium** — débito técnico relevante
- 🟢 **Low** — melhoria de qualidade

---

## Security

### 🔴 Credencial exposta em arquivo não gitignoreado

**Arquivo:** `erp-bridge-aws/config.yaml`

Contém senha real em texto plano. O arquivo **não está no `.gitignore`**, o que significa que a credencial pode ter sido commitada no histórico Git.

**Ação:** Revogar/rotacionar a credencial imediatamente. Adicionar `erp-bridge-aws/config.yaml` ao `.gitignore`. Usar variáveis de ambiente ou secrets manager.

---

### 🔴 JWT armazenado em `localStorage` (risco XSS)

**Arquivos:** `frontend/src/contexts/AuthContext.tsx`

Access token JWT salvo em `localStorage`. Qualquer script injetado via XSS pode roubar o token.

**Ação:** Migrar para `httpOnly` cookie ou, no mínimo, `sessionStorage`.

---

### 🔴 Rate limiter de login definido mas nunca aplicado

**Arquivo:** `backend/handlers/auth.go`

`LoginRL` (rate limiter) está implementado mas **não é aplicado à rota de login**. O endpoint de login está desprotegido contra brute-force.

**Ação:** Aplicar o middleware `LoginRL` na rota `/auth/login` em `main.go`.

---

### 🟠 Mensagens internas de erro de DB vazando para o cliente HTTP

**Localização:** Handlers em `backend/handlers/`

Mais de 100 chamadas retornam `http.Error(w, err.Error(), ...)` expondo strings internas do PostgreSQL (nomes de tabelas, queries, stack info) para o cliente.

**Ação:** Criar wrapper que loga o erro interno e retorna mensagem genérica ao cliente.

---

### 🟡 CSP permite `unsafe-inline`

**Arquivo:** Configuração do nginx (`frontend/nginx.conf`)

Content Security Policy com `'unsafe-inline'` abre vetor de ataque XSS.

**Ação:** Refatorar para usar nonces ou hashes específicos.

---

## Bugs

### 🔴 Páginas CBS/IBS lendo chave errada de `localStorage`

**Arquivos:**
- `frontend/src/pages/PainelApuracaoCBS.tsx`
- `frontend/src/pages/PainelApuracaoIBS.tsx`

Leem chave de localStorage incorreta → sempre operam com empresa errada silenciosamente.

---

### 🔴 `Managers.tsx` lê chave que nunca é escrita

**Arquivo:** `frontend/src/pages/Managers.tsx`

Lê `'selectedCompanyId'` que **nunca é escrita** por nenhum componente → valor sempre `null` → comportamento incorreto.

---

### 🟠 3 chaves diferentes para `companyId` no `localStorage`

**Chaves encontradas:** `'companyId'`, `'company_id'`, `'selectedCompanyId'`

Diferentes partes do frontend usam chaves distintas para o mesmo dado, causando bugs silenciosos de empresa errada.

**Ação:** Centralizar em uma única constante exportada de `lib/utils.ts` ou `contexts/FilialContext.tsx`.

---

## Tech Debt

### 🟠 Migrations com número duplicado

**Arquivos:**
- `backend/migrations/021_create_mv_mercadorias.sql` e `backend/migrations/021_ensure_admin_user.sql`
- `backend/migrations/061_add_filial_to_mv_operacoes_simples.sql` e `backend/migrations/061_user_environments_preferred_company.sql`

Numeração duplicada pode causar ordem de aplicação não-determinística dependendo da implementação do runner.

**Ação:** Renumerar as duplicatas e garantir que o runner ordene por nome de arquivo.

---

### 🟠 `middleware/tenant.go` implementado mas nunca usado

**Arquivo:** `backend/middleware/tenant.go`

Row-Level Security (RLS) multi-tenant implementado e testado (`tenant_test.go`), mas **nenhum handler aplica este middleware**. O isolamento de dados entre empresas depende de filtros manuais nos handlers.

---

### 🟠 15+ chamadas `db.Exec` sem verificação de erro

**Localização:** Handlers em `backend/handlers/`

Erros de escrita no banco são silenciosamente ignorados — nenhum retorno de erro ao cliente, nenhum log.

**Ação:** Adicionar verificação `if err != nil` após cada `db.Exec`.

---

### 🟡 Redis definido no docker-compose de produção mas sem uso

**Arquivo:** `docker-compose.prod.yml`

Redis alocado com 256MB de RAM mas **zero código Go** o utiliza. Desperdício de recurso.

**Ação:** Remover Redis do compose ou implementar uso (cache de sessão, rate limiting).

---

### 🟡 5 ferramentas de debug com senhas hardcoded commitadas

**Arquivos:** `backend/tools/debug_*.go`, `backend/tools/verify_data.go`

Contêm strings de conexão com senhas locais de desenvolvimento. Não devem existir em produção.

**Ação:** Mover para `.gitignore` ou deletar se não forem mais necessárias.

---

## Test Gaps

### 🟠 Zero testes unitários para os 24 handlers HTTP

Nenhum dos handlers em `backend/handlers/` possui arquivo de teste correspondente. Toda a lógica de negócio (auth, NF-e, RFB, ERP bridge, CGIBS) está sem cobertura.

---

### 🟡 Testes de integração usam soft-fail

O teste de integração não falha (`t.Fatal`) quando o servidor está inacessível — retorna silenciosamente. Nunca bloqueia o CI mesmo quando a infra está quebrada.

---

### 🟡 Pipeline CI sem etapa de testes

**Arquivos:** `.github/workflows/deploy-*.yml`

Deploy vai direto do build Docker para produção sem rodar nenhum teste.

---

## Performance

### 🟡 Materialized Views sem refresh automático agendado

As MVs (`mv_mercadorias`, `mv_malha_fina_resumo`, etc.) são críticas para performance mas dependem de `REFRESH MATERIALIZED VIEW` manual ou via trigger. Sem refresh automático, dados podem ficar desatualizados.

---

## Summary

| Severidade | Quantidade |
|-----------|-----------|
| 🔴 Critical | 4 |
| 🟠 High | 7 |
| 🟡 Medium | 5 |
| 🟢 Low | 0 |

**Prioridade máxima:** Credencial exposta em `config.yaml` e rate limiter de login não aplicado.
