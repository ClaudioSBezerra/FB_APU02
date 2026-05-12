---
phase: 01-seguranca
reviewed: 2026-05-12T00:00:00Z
depth: standard
files_reviewed: 13
files_reviewed_list:
  - backend/handlers/admin.go
  - backend/handlers/auth.go
  - backend/handlers/cgibs_apuracao.go
  - backend/handlers/config.go
  - backend/handlers/erp_bridge.go
  - backend/handlers/erp_bridge_batch.go
  - backend/handlers/filiais.go
  - backend/handlers/login_ratelimit_test.go
  - backend/handlers/nfe_entradas.go
  - backend/handlers/nfe_saidas.go
  - backend/handlers/rfb_apuracao.go
  - erp-bridge-aws/config.yaml
  - frontend/src/contexts/AuthContext.tsx
findings:
  critical: 4
  warning: 5
  info: 2
  total: 11
status: issues_found
---

# Phase 01: Relatório de Revisão de Código — Segurança

**Revisado:** 2026-05-12
**Profundidade:** standard
**Arquivos Revisados:** 13
**Status:** issues_found

## Resumo

Os quatro planos de segurança foram parcialmente implementados com sucesso: `sanitizeDBErr` está definido e aplicado na maioria dos handlers, `LoginRL` está declarado e o teste confirma sua posição correta, o JWT foi migrado para `sessionStorage`, e `config.yaml` está no `.gitignore`. Porém a revisão identificou quatro blockers — dois vazamentos de erro interno que sobreviveram à implementação do plano SEC-04, um arquivo de configuração que ainda está rastreado pelo git apesar do `.gitignore`, e dados sensíveis remanescentes no `config.yaml`. Há também cinco warnings relevantes de qualidade e segurança.

---

## Critical Issues

### CR-01: `config.yaml` ainda rastreado pelo git — credenciais visíveis no histórico

**Arquivo:** `erp-bridge-aws/config.yaml`
**Issue:** O arquivo está no `.gitignore` (linha 51 do `.gitignore`), mas o comando `git status` confirma que o arquivo existe no working tree limpo — o que significa que ele já foi comitado e permanece no histórico do repositório. O plano 01-01 adicionou o `.gitignore`, mas não removeu o arquivo do índice git com `git rm --cached`. Qualquer clone do repositório terá acesso ao email `claudio.bezerra@ferreiracosta.com.br`, ao `company_id` `95c3d8fa-ff19-45c0-884b-a4f09fd395bd` e aos placeholders que documentam a estrutura de DSN Oracle (host/porta/SID).

**Fix:**
```bash
git rm --cached erp-bridge-aws/config.yaml
git commit -m "chore: remove config.yaml do rastreamento git (dados sensíveis)"
# Considerar rotacionar o company_id e verificar se versões anteriores do arquivo
# continham senhas reais; se sim, fazer rewrite do histórico com git-filter-repo.
```

---

### CR-02: Vazamento de erro interno em `erp_bridge.go` — `execErr.Error()` exposto ao cliente

**Arquivo:** `backend/handlers/erp_bridge.go:403`
**Issue:** O handler `ERPBridgeRunHandler`, no case `PATCH /api/erp-bridge/runs/{id}`, envia diretamente `execErr.Error()` como corpo da resposta HTTP 500. Isso pode expor mensagens de erro do PostgreSQL (nomes de colunas, restrições, valores de parâmetros) para qualquer cliente autenticado que chame esse endpoint. O plano SEC-04 não cobriu este ponto.

```go
if execErr != nil {
    http.Error(w, execErr.Error(), http.StatusInternalServerError)  // BLOCKER
    return
}
```

**Fix:**
```go
if execErr != nil {
    sanitizeDBErr(w, http.StatusInternalServerError, "Erro ao atualizar run", execErr, "[ERPBridgeRun]")
    return
}
```

---

### CR-03: Vazamento de mensagem de erro de serviço externo em `rfb_apuracao.go` — `msg` exposto no `default` case

**Arquivo:** `backend/handlers/rfb_apuracao.go:109-124`
**Issue:** `SolicitarApuracaoHandler` chama `err.Error()` e armazena em `msg`, depois usa `msg` diretamente no `default` case com `http.Error(w, msg, ...)`. Se `services.SolicitarApuracaoParaEmpresa` retornar um erro inesperado (e.g., falha de conexão com o banco, panic recuperado, erro de query PostgreSQL), a mensagem interna é enviada ao cliente. Os cases nomeados (`credenciais RFB`, `RATE_LIMIT`, `TOKEN_ERROR`, `REQUEST_ERROR`) são aceitáveis pois são mensagens controladas pelo próprio serviço, mas o `default` é problemático.

```go
default:
    http.Error(w, msg, http.StatusInternalServerError)  // BLOCKER — msg pode ser erro de DB
```

**Fix:**
```go
default:
    log.Printf("[SolicitarApuracao] Erro inesperado: %v", err)
    http.Error(w, "Erro ao solicitar apuração. Tente novamente.", http.StatusInternalServerError)
```

---

### CR-04: Rate limiter de login não distingue tentativas falhas — todas as requisições consomem cota

**Arquivo:** `backend/handlers/auth.go:601-605` / `backend/handlers/middleware.go:144-163`
**Issue:** `LoginRL.Allow(ip)` é chamado **antes** de verificar a senha, e `Allow()` registra **toda** chamada como uma tentativa (inclusive logins bem-sucedidos). Isso tem dois problemas de segurança opostos:

1. **Bypass trivial via login legítimo:** Um atacante que possua uma conta válida pode consumir suas 5 tentativas fazendo login com a própria conta, resetando a janela a cada 15 minutos para tentar 5 ataques contra outra conta.
2. **Auto-bloqueio de usuário legítimo:** Um usuário que digita a senha errada 5 vezes em 15 minutos fica bloqueado, mesmo que o IP seja compartilhado (NAT corporativo). A janela de 15 minutos sem nenhum mecanismo de desbloqueio é disruptiva.

O padrão correto para brute-force é bloquear por `email` (não só IP) e contar apenas tentativas falhas. A implementação atual usa apenas IP e conta todo acesso.

**Fix (mínimo — sem refatorar a API do rate limiter):**

Mover a verificação de senha antes do registro de tentativa, usando `IsLimited` para verificar e `RecordFailure` só em caso de senha incorreta:

```go
// No LoginHandler, substituir o bloco de rate limit atual por:
ip := GetClientIP(r)
if LoginRL.IsLimited(ip) {
    http.Error(w, "Too many requests", http.StatusTooManyRequests)
    return
}
// ... parse body, buscar usuário, ...
if !CheckPasswordHash(req.Password, hash) {
    LoginRL.RecordFailure(ip)          // só falhas contam
    // ... retornar 401
}
// login bem-sucedido: não registrar tentativa
```

---

## Warnings

### WR-01: `getEncryptionKey` faz fallback silencioso para `JWT_SECRET` em produção

**Arquivo:** `backend/handlers/crypto.go:19-26`
**Issue:** Quando `ENCRYPTION_KEY` não está definida mas `DATABASE_URL` está (ambiente de produção), a função usa `JWT_SECRET` como chave de criptografia. O código faz um "Allow fallback" **sem** logar nenhum aviso, ao contrário de `ValidateJWTSecret` que loga fatal/warning. Se um operador configura apenas `JWT_SECRET` e esquece `ENCRYPTION_KEY`, credenciais RFB e Oracle ficam cifradas com a mesma chave do JWT, o que viola o princípio de separação (comentado na própria função) sem nenhuma notificação.

**Fix:** Adicionar log de aviso obrigatório, como é feito para `JWT_SECRET`:
```go
if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
    log.Println("WARNING: ENCRYPTION_KEY not set — using JWT_SECRET as fallback. " +
        "Set ENCRYPTION_KEY to a separate 32+ byte random value in production.")
    key = jwtSecret
}
```

---

### WR-02: `AuthContext.tsx` ainda armazena `companyId` e preferências via `localStorage` — regressão parcial do SEC-03

**Arquivo:** `frontend/src/contexts/AuthContext.tsx:130, 179`
**Issue:** O plano SEC-03 migrou o JWT para `sessionStorage`. No entanto, o `companyId` preferido do usuário ainda é armazenado em `localStorage` (`pref_company_${userId}`) contendo `{ id, name, cnpj }`. O `company_id` é um UUID interno sem valor direto de ataque, mas o `cnpj` armazenado no `localStorage` é um dado fiscal pessoal que persiste indefinidamente, mesmo após logout. Isso é inconsistente com a postura de segurança do SEC-03 e pode violar LGPD.

**Fix:** Avaliar se a persistência de preferência de empresa é necessária após logout. Se sim, armazenar apenas o `id` (sem `name` e `cnpj`):
```typescript
localStorage.setItem(`pref_company_${user.id}`, id); // só o UUID
```
Se não for necessário após logout, migrar para `sessionStorage`.

---

### WR-03: `ResetDatabaseHandler` não tem verificação de autenticação/autorização

**Arquivo:** `backend/handlers/admin.go:279-355`
**Issue:** `ResetDatabaseHandler` executa `TRUNCATE TABLE import_jobs CASCADE` sem verificar claims do JWT nem role do usuário. A segurança depende inteiramente do middleware `withAuth` aplicado em `main.go`. Não há defesa em profundidade no próprio handler — qualquer bug no roteamento ou configuração de middleware expõe uma operação destrutiva irreversível. Todos os outros handlers destrutivos (`LimparDadosApuracaoHandler`, `ResetCompanyDataHandler`) verificam role explicitamente.

**Fix:**
```go
claims, ok := r.Context().Value(ClaimsKey).(jwt.MapClaims)
if !ok {
    http.Error(w, "Unauthorized", http.StatusUnauthorized)
    return
}
if role, _ := claims["role"].(string); role != "admin" {
    http.Error(w, "Forbidden", http.StatusForbidden)
    return
}
```

---

### WR-04: `UpdatePreferredCompanyHandler` não verifica se o usuário tem acesso à empresa solicitada

**Arquivo:** `backend/handlers/auth.go:291-313`
**Issue:** O handler faz um `SELECT e.id FROM companies WHERE c.id = $1` para obter o `environment_id`, mas não verifica se o `userID` autenticado tem acesso à empresa `req.CompanyID`. Qualquer usuário autenticado pode alterar sua `preferred_company_id` para o ID de qualquer empresa no sistema, desde que conheça o UUID. Embora isso não vaze dados (o `GetEffectiveCompanyID` tem sua própria verificação de acesso), permite que o usuário manipule a preferência para um ID de empresa inacessível, gerando um estado inconsistente no banco.

**Fix:** Adicionar verificação de acesso antes do UPSERT:
```go
var hasAccess bool
db.QueryRow(`SELECT EXISTS(
    SELECT 1 FROM companies c
    LEFT JOIN enterprise_groups eg ON c.group_id = eg.id
    LEFT JOIN user_environments ue ON eg.environment_id = ue.environment_id
    WHERE c.id = $1 AND (c.owner_id = $2 OR ue.user_id = $2)
)`, req.CompanyID, userID).Scan(&hasAccess)
if !hasAccess {
    http.Error(w, "Forbidden", http.StatusForbidden)
    return
}
```

---

### WR-05: Rate limiter em `middleware.go` não tem limpeza periódica de memória — crescimento ilimitado

**Arquivo:** `backend/handlers/middleware.go:122-164`
**Issue:** O `rateLimiter` acumula entradas em `requests map[string][]time.Time` sem nunca remover chaves inativas. Em `Allow()` e `IsLimited()`, a slice de timestamps de uma chave é podada a cada chamada, mas a **chave** em si permanece no mapa para sempre. Com tráfego adversarial (IPs únicos gerados), o mapa cresce indefinidamente, causando consumo de memória crescente sem limite. O `tokenBlacklist` em `auth.go` tem limpeza periódica (goroutine no `init()`), mas os rate limiters não têm.

**Fix:** Adicionar goroutine de limpeza no `init()` ou no `newRateLimiter`, removendo chaves com slice vazia após o pruning:
```go
func (rl *rateLimiter) cleanup() {
    rl.mu.Lock()
    defer rl.mu.Unlock()
    now := time.Now()
    for key, times := range rl.requests {
        valid := times[:0]
        for _, t := range times {
            if now.Sub(t) < rl.window {
                valid = append(valid, t)
            }
        }
        if len(valid) == 0 {
            delete(rl.requests, key)
        } else {
            rl.requests[key] = valid
        }
    }
}
```

---

## Info

### IN-01: `erp-bridge-aws/config.yaml` contém email e `company_id` reais mesmo após sanitização

**Arquivo:** `erp-bridge-aws/config.yaml:16-17`
**Issue:** Após a remoção da senha Oracle (plano 01-01), o arquivo ainda contém o email `claudio.bezerra@ferreiracosta.com.br` e o UUID `95c3d8fa-ff19-45c0-884b-a4f09fd395bd`. O email é dado pessoal (LGPD) e o `company_id` é um identificador interno do sistema. Mesmo que esses dados sejam menos sensíveis que uma senha, não devem permanecer em um arquivo de template versionado.

**Fix:** Substituir por placeholders e documentar em `README` ou arquivo `.env.example`:
```yaml
fbtax:
  email:      "FBTAX_EMAIL_CONFIGURE_VIA_ENV"
  company_id: "FBTAX_COMPANY_ID_CONFIGURE_VIA_ENV"
```

---

### IN-02: `generateRefreshTokenString` ignora o erro de `rand.Read`

**Arquivo:** `backend/handlers/auth.go:144-148`
**Issue:** `rand.Read(b)` pode retornar erro (embora extremamente improvável no kernel moderno), mas o retorno de erro é descartado. O comportamento é seguro na prática pois `crypto/rand` em Linux usa `/dev/urandom` que não retorna erro após boot, mas o padrão idiomático em Go é verificar o erro ou usar `io.ReadFull`.

**Fix:**
```go
func generateRefreshTokenString() (string, error) {
    b := make([]byte, 32)
    if _, err := io.ReadFull(rand.Reader, b); err != nil {
        return "", err
    }
    return hex.EncodeToString(b), nil
}
```
(Alternativamente, manter como está e adicionar um comentário justificando a omissão do erro, que é o padrão adotado em `ForgotPasswordHandler` linha 840.)

---

_Revisado: 2026-05-12_
_Revisor: Claude (gsd-code-reviewer)_
_Profundidade: standard_
