# Arquitetura de Integração — FB_APU02

**Tipo de repositório:** Monorepo multi-parte  
**Partes integradas:** Backend Go API ↔ Frontend React ↔ ERP Bridge Python ↔ APIs Externas

---

## Diagrama Geral

```
┌────────────────────────────────────────────────────────────────────────┐
│                         Usuário (Browser)                               │
└──────────────────────────┬─────────────────────────────────────────────┘
                           │ HTTPS/TLS (Let's Encrypt)
                    ┌──────▼──────┐
                    │   Traefik   │ ← reverse proxy (Docker label routing)
                    └──────┬──────┘
              ┌────────────┼────────────┐
              │            │            │
     ┌────────▼────────┐   │   ┌────────▼────────────┐
     │  Frontend       │   │   │  Backend Go         │
     │  Nginx:80       │   │   │  :8081              │
     │  React SPA      │   │   │                     │
     └─────────────────┘   │   └────────┬────────────┘
                           │            │ pool 50 conn
                           │     ┌──────▼──────┐
                           │     │ PostgreSQL  │
                           │     │    :5432    │
                           │     └─────────────┘
                    ┌──────▼──────┐
                    │  RFB API    │ ← api.receitafederal.gov.br
                    │  CGIBS API  │ ← portal.cgibs.gov.br
                    └──────┬──────┘
                           │ POST /api/rfb/webhook (HMAC-SHA256)
                    ┌──────▼──────┐
                    │  Backend    │ ← webhook callback
                    └─────────────┘

                    ┌─────────────────────────────────────────┐
                    │  ERP Bridge Python (AWS)                │
                    │  Daemon systemd (polling 60s)           │
                    └──┬──────────────────────────────────────┘
                       │
         ┌─────────────┼──────────────────┐
         │             │                  │
  ┌──────▼──────┐      │     ┌────────────▼──────────────┐
  │ Oracle ERP  │      │     │       Backend API          │
  │ (FCCORP)    │      │     │  X-API-Key endpoints       │
  │ Thin mode   │      │     │  /api/erp-bridge/import/   │
  └─────────────┘      │     │  /api/erp-bridge/parceiros │
                       │     └───────────────────────────-┘
                       │
               ┌───────▼───────┐
               │  tracker.db   │
               │  (SQLite)     │
               └───────────────┘
```

---

## Pontos de Integração Detalhados

### 1. Frontend → Backend (Principal)

| Aspecto | Detalhe |
|---------|---------|
| **Protocolo** | HTTP/HTTPS REST + JSON |
| **Autenticação** | `Authorization: Bearer {JWT}` + `X-Company-ID: {uuid}` |
| **Injeção automática** | `window.fetch` monkey-patched no `AuthContext.tsx` |
| **Auto-logout** | HTTP 401 em `/api/*` (exceto `/api/auth/`) → logout + redirect `/login` |
| **Dev proxy** | Vite redireciona `/api/*` para `http://localhost:8081` |
| **Prod** | Traefik roteia por domínio — frontend e backend em containers separados |

**Fluxo de troca de empresa:**
```
switchCompany(id, name, cnpj)
    → sessionStorage + localStorage atualizados
    → PATCH /api/user/preferred-company (fire-and-forget)
    → window.location.reload() ← garante dados consistentes para nova empresa
```

### 2. Backend → RFB API (Receita Federal)

| Aspecto | Detalhe |
|---------|---------|
| **Protocolo** | HTTPS REST + JSON |
| **Autenticação** | OAuth2 `client_credentials` (por empresa) |
| **Token cache** | Em memória por `client_id` (com margem de 5min antes do expiry) |
| **Rate limit** | Detectado via HTTP 429 → persistido em `rfb_requests.error_message` como `retry_until=RFC3339` |
| **Ambientes** | `producao_restrita` → path prefix `prr-rtc`; demais → `rtc` |
| **Timeout HTTP** | 30 segundos |
| **Webhook retorno** | RFB chama `POST /api/rfb/webhook` com resultado da apuração |

**Fluxo de apuração CBS:**
```
SolicitarApuracaoHandler (usuário ou scheduler)
    → OAuth2 GetToken (cacheado)
    → POST /api/rfb/* → tiquete
    → INSERT rfb_requests (status='requested')
    ↓ (assíncrono — pode levar minutos/horas)
POST /api/rfb/webhook ← RFB callback
    → Validar HMAC-SHA256 (JWT_SECRET)
    → goroutine: download + processamento
    → UPDATE rfb_requests (status='completed')
    → INSERT rfb_debitos + rfb_resumo
```

### 3. Backend → CGIBS API

| Aspecto | Detalhe |
|---------|---------|
| **Protocolo** | HTTPS REST + JSON |
| **Autenticação** | OAuth2 `client_credentials` por empresa |
| **Estrutura** | Espelha exatamente o fluxo RFB (requests, webhook, débitos, resumo) |
| **Tabelas** | `cgibs_credentials`, `cgibs_requests`, `cgibs_debitos`, `cgibs_resumo` |

### 4. ERP Bridge → Backend (Import Batch)

| Aspecto | Detalhe |
|---------|---------|
| **Protocolo** | HTTPS REST + JSON ou multipart/form-data |
| **Autenticação** | `X-API-Key` header (sem JWT) → `api_key_hash` SHA256 no banco |
| **Modo oracle_xml** | Multipart XML — 1 arquivo por request para cada documento |
| **Modo sap_s4hana** | JSON batch — lotes de até 1000 documentos por request |
| **Idempotência** | Backend: `INSERT ... ON CONFLICT (company_id, chave_nfe) DO NOTHING` |
| **Heartbeat** | `POST /api/erp-bridge/heartbeat` a cada 60s — sinaliza daemon ativo |

**Fluxo de importação SAP:**
```
ERP Bridge daemon (60s polling)
    → GET /api/erp-bridge/pending  ← runs enfileirados pela UI
    → Para cada run:
        → Oracle FCCORP: SAP_QUERY (JOIN s4i_nfe + s4i_nfe_impostos)
        → POST /api/erp-bridge/parceiros/sync (lotes 500 parceiros)
        → POST /api/erp-bridge/import/batch (lotes 1000 documentos)
        → PATCH /api/erp-bridge/runs/{id} (finalizar run)
        → SQLite: set_watermark(dsn, last_date)
```

### 5. RFB Webhook → Backend

| Aspecto | Detalhe |
|---------|---------|
| **Direção** | RFB chama o backend (callback externo) |
| **Endpoint** | `POST /api/rfb/webhook` (público — sem JWT) |
| **Autenticação** | HMAC-SHA256 do payload com `JWT_SECRET` |
| **URL registrada** | `RFB_WEBHOOK_URL` env var (ex: `https://fctax.fcxlabs.com/api/rfb/webhook`) |
| **Validação SSL** | RFB valida se a URL de callback está acessível e com SSL válido antes de aceitar solicitações |

---

## Compartilhamento de Banco de Dados

O PostgreSQL é o único estado compartilhado entre o Frontend (via Backend) e o ERP Bridge (via Backend). **Não há acesso direto ao PostgreSQL pelo ERP Bridge** — tudo passa pela API.

```
Frontend → Backend API → PostgreSQL ← Backend API ← ERP Bridge
                                  ↑
                           Único estado
                           compartilhado
```

---

## Fluxo de Autenticação Completo

```
1. Usuário → POST /api/auth/login {email, password}
2. Backend → bcrypt verify → gera JWT (30min) + refresh token (7 dias)
3. Backend → retorna { token: "..." } + Set-Cookie: refresh_token (HttpOnly)
4. Frontend → salva token em sessionStorage
5. AuthContext → monkey-patch window.fetch com Bearer + X-Company-ID
6. Qualquer fetch('/api/...') → automático: headers injetados

Renovação automática:
7. JWT expira → 401 recebido → logout automático
8. Refresh: POST /api/auth/refresh (cookie enviado automaticamente)
   → novo JWT retornado → tokenRef.current atualizado
```

---

## Dados em Trânsito — Formatos

| Integração | Formato | Observação |
|-----------|---------|------------|
| Frontend ↔ Backend (geral) | JSON (`Content-Type: application/json`) | |
| ERP Bridge → Backend (XML mode) | `multipart/form-data` com campo `xmls` | 1 arquivo XML por request |
| ERP Bridge → Backend (SAP mode) | JSON `{ documents: [...] }` | Lotes de até 1000 |
| ERP Bridge → Backend (parceiros) | JSON `{ parceiros: [...] }` | Lotes de até 500 |
| Backend → RFB/CGIBS | JSON com OAuth2 Bearer | |
| RFB → Backend (webhook) | JSON com HMAC-SHA256 | |

---

## Variáveis de Ambiente Críticas para Integração

| Variável | Componente | Propósito |
|----------|-----------|-----------|
| `DATABASE_URL` | Backend | Conexão PostgreSQL |
| `JWT_SECRET` | Backend | Assinar JWT + validar webhook RFB |
| `RFB_WEBHOOK_URL` | Backend | URL registrada na RFB para callbacks |
| `ALLOWED_ORIGINS` | Backend | CORS — deve incluir domínio do frontend |
| `fbtax.url` | ERP Bridge (`config.yaml`) | URL base da API |
| `fbtax.api_key` | ERP Bridge | X-API-Key para endpoints erp-bridge/* |
| `fbtax.company_id` | ERP Bridge | UUID da empresa no FBTax |

---

## Resiliência e Pontos de Falha

| Ponto de falha | Impacto | Mitigação |
|---------------|---------|-----------|
| SSL expirado no backend | RFB rejeita solicitações de apuração | certbot auto-renovação + nginx |
| Rate limit RFB (HTTP 429) | Solicitações bloqueadas até `retry_until` | Persistido em banco; respeitado após restart |
| Restart do container backend | Perda dos refresh tokens (sync.Map) | Usuários precisam fazer login novamente |
| Oracle connection timeout | Sessão idle cortada por firewall | `expire_time=2min` no oracledb + reconexão por iteração |
| Run longo cancelado pela UI | Bridge verifica `GET /api/erp-bridge/runs/{id}` | Interrompe processamento |
