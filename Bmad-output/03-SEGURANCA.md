# Segurança — FBTax Apuração Assistida

**Projeto:** FB_APU02

---

## 1. Autenticação

### JWT (JSON Web Tokens)
- Biblioteca: `golang-jwt/jwt/v5`
- Segredo: `JWT_SECRET` (env var, mínimo 32 bytes, obrigatório em produção)
- Expiração: **30 minutos** (access token)
- Refresh token: Armazenado em memória server-side com expiração
- Validação: Middleware `AuthMiddleware` em todas as rotas protegidas

### Refresh Token
- Armazenado em `refreshTokenStore` (sync.Map em memória)
- Limpeza automática a cada hora (goroutine)
- Endpoint: `POST /api/auth/refresh`

### Blacklist de Tokens
- Tokens invalidados no logout são inseridos em `tokenBlacklist` (sync.Map)
- Verificação a cada request autenticado
- Limpeza automática de tokens expirados (goroutine horária)

### Proteção de Rotas
```go
// Todas as rotas exceto auth e webhook exigem JWT válido
withAuth(handler, "")        // qualquer usuário autenticado
withAuth(handler, "admin")   // somente administradores
```

Única rota pública (sem JWT): `POST /api/rfb/webhook` (validada por HMAC separadamente)

---

## 2. Gerenciamento de Senhas

| Mecanismo | Detalhe |
|-----------|---------|
| Hash | **bcrypt** com custo 14 (`golang.org/x/crypto/bcrypt`) |
| Reset | Token hex aleatório, armazenado em `verification_tokens` |
| Validade | Token expirado após uso (`used = true`) |
| Exposição | Senhas nunca retornadas na API |
| Client Secret RFB | Mascarado na exibição (somente últimos 4 chars) |

---

## 3. Criptografia de Credenciais RFB

As credenciais OAuth2 da RFB (client_id, client_secret) são sensíveis:

- Chave separada: `ENCRYPTION_KEY` (env var, diferente do JWT_SECRET)
- Fallback: `JWT_SECRET` em desenvolvimento (com aviso no log)
- **Produção:** Falha fatal se `ENCRYPTION_KEY` não estiver definida
- Algoritmo: AES-256-GCM (padrão Go crypto)

---

## 4. Rate Limiting

Implementado em memória (IP-based, usa `X-Forwarded-For` com rightmost IP anti-spoofing):

| Endpoint | Limite | Janela |
|----------|--------|--------|
| `POST /api/auth/login` | 5 tentativas | 15 minutos |
| `POST /api/auth/register` | 10 tentativas | 1 hora |
| `POST /api/auth/forgot-password` | 3 tentativas | 1 hora |

---

## 5. Webhook HMAC (RFB)

O webhook público da RFB é validado por assinatura HMAC-SHA256:

```
Header: X-RFB-Signature: sha256={hex_digest}
Segredo: RFB_WEBHOOK_SECRET (env var)
Algoritmo: HMAC-SHA256 sobre o corpo da requisição
```

- Se `RFB_WEBHOOK_SECRET` não estiver configurado: **warning no log, validação ignorada** (aceitável em desenvolvimento)
- Em produção: configurar o segredo para garantir que apenas a RFB acione o webhook

---

## 6. Headers de Segurança HTTP

`SecurityMiddleware` adicionado a todas as respostas:

```
X-Frame-Options: DENY
X-Content-Type-Options: nosniff
X-XSS-Protection: 1; mode=block
Referrer-Policy: strict-origin-when-cross-origin
Strict-Transport-Security: max-age=31536000; includeSubDomains
Permissions-Policy: geolocation=(), microphone=(), camera=()
Content-Security-Policy:
  default-src 'self';
  script-src 'self' 'unsafe-inline';
  style-src 'self' 'unsafe-inline' https://fonts.googleapis.com;
  font-src 'self' https://fonts.gstatic.com;
  img-src 'self' data: https:;
  connect-src 'self';
  frame-ancestors 'none'
```

---

## 7. CORS

Whitelist configurável via `ALLOWED_ORIGINS`:

- Padrão: `https://apuracao.fbtax.cloud`, `https://fbtax.cloud`, `http://localhost:3000`, `http://localhost:5173`
- Origin só incluída em `Access-Control-Allow-Origin` se estiver na whitelist
- Preflight OPTIONS tratados automaticamente
- Credentials: `Access-Control-Allow-Credentials: true`

---

## 8. Multi-tenancy / Isolamento de Dados

- Toda query de dados filtra por `company_id` (UUID)
- `company_id` derivado do JWT claims + header `X-Company-ID`
- Usuários só acessam empresas às quais pertencem
- `GetEffectiveCompanyID()` valida que o `company_id` solicitado pertence ao usuário

```
Hierarquia de acesso:
  environments → groups → companies → user_environments (role: admin|user)
```

---

## 9. Proteção Frontend

- Token JWT armazenado em `localStorage` (padrão SPA)
- Token incluído em **todos os requests via interceptor global** (`fetch` override no AuthContext)
- Expiração detectada: usuário redirecionado para login automaticamente
- Senha nunca armazenada localmente

---

## 10. TLS / HTTPS

- Gerenciado pelo **Traefik** (via Coolify)
- Certificado Let's Encrypt com renovação automática
- HSTS habilitado (`Strict-Transport-Security: max-age=31536000`)
- Backend escuta apenas em porta interna (8081), não exposta diretamente

---

## 11. Retenção e Privacidade de Dados

- Tabela `dfe_xml` (XMLs brutos): **deleção automática após 5 anos** (obrigação fiscal)
- Goroutine de limpeza executa a cada 7 dias
- Senhas nunca logadas
- Client Secret RFB mascarado em logs e respostas da API

---

## 12. Checklist de Segurança para Produção

- [ ] `JWT_SECRET` definido (32+ chars, aleatório)
- [ ] `ENCRYPTION_KEY` definido (separado do JWT_SECRET)
- [ ] `RFB_WEBHOOK_SECRET` configurado e sincronizado com a RFB
- [ ] `ALLOWED_ORIGINS` contém apenas domínios autorizados
- [ ] `COOKIE_SECURE=true` (garante cookie apenas em HTTPS)
- [ ] `DATABASE_URL` com usuário de mínimos privilégios
- [ ] PostgreSQL não exposto na internet (apenas container interno)
- [ ] Backup automático do volume PostgreSQL configurado
- [ ] Logs sem secrets (verificar variáveis de ambiente)
- [ ] Traefik com Let's Encrypt ativo
- [ ] SMTP com autenticação TLS
