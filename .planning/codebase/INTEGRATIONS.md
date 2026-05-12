# External Integrations

**Analysis Date:** 2026-05-12

## APIs & External Services

**Brazilian Tax Authority (Receita Federal do Brasil):**
- RFB CBS API - Retrieve CBS (Contribuição sobre Bens e Serviços) tax assessments
  - SDK/Client: Custom `RFBClient` in `backend/services/rfb.go`
  - Auth: OAuth2 client_credentials flow — `client_id` + `client_secret` stored encrypted per company in `rfb_credentials` table
  - Base URL: `https://api.receitafederal.gov.br` (configurable via `RFB_API_URL`)
  - Token URL: `https://api.receitafederal.gov.br/token` (configurable via `RFB_TOKEN_URL`)
  - Token caching: per `client_id`, with 5-minute safety margin before expiry
  - Rate limiting: HTTP 429 handled with `Retry-After` header, persisted to DB on restart
  - Environments: `producao` (path prefix `rtc`) and `producao_restrita` (path prefix `prr-rtc`)
  - Webhook: RFB pushes async results to `POST /api/rfb/webhook` — public endpoint (no JWT)

**CGIBS (Comitê Gestor do IBS):**
- CGIBS IBS API - Retrieve IBS (Imposto sobre Bens e Serviços) tax assessments
  - SDK/Client: Custom handlers in `backend/handlers/cgibs_apuracao.go`, `backend/handlers/cgibs_credentials.go`
  - Auth: OAuth2 client_credentials — `client_id` + `client_secret` stored encrypted per company in `cgibs_credentials` table
  - Environments: `piloto` (default) and production
  - Scheduling: configurable per-company scheduled assessments

**Z.AI GLM API (referenced, not yet implemented):**
- AI-generated executive reports — referenced in `coolify-env-template.txt` as `ZAI_API_KEY`
- Current state: `backend/services/email.go` has `SendAIReportEmail()` which sends AI narrative via SMTP; no HTTP call to Z.AI found in current Go source
- Planned integration for generating `narrativaMarkdown` content

## Data Storage

**Databases:**
- PostgreSQL 15 (primary database)
  - Connection: `DATABASE_URL` env var (format: `postgres://user:pass@host:5432/dbname?sslmode=...`)
  - Client: `database/sql` + `github.com/lib/pq` driver (no ORM)
  - Pool: max 50 open connections, 15 idle, 15-minute lifetime
  - Auto-migration: on startup, runs `backend/migrations/*.sql` files in filename order, tracked in `schema_migrations` table
  - 104+ migration files covering all schema versions

- SQLite 3 (ERP bridge local tracker)
  - File: `erp-bridge-aws/tracker.db`
  - Purpose: tracks which fiscal documents have been sent to avoid duplicates
  - Client: Python stdlib `sqlite3`

**File Storage:**
- Local filesystem: uploaded XML files stored in `/root/uploads` (Docker volume `api_uploads`)
- No external object storage (S3, GCS, etc.) detected

**Caching:**
- Redis: configured in `docker-compose.yml` and `docker-compose.prod.yml` (image: `redis:alpine` / `redis:7-alpine`)
- Redis address: `REDIS_ADDR` env var (default `redis:6379`)
- Note: Redis is in docker-compose but no Redis client library appears in `backend/go.mod` or Go source — it is present in the infrastructure but not actively used by the current Go backend code

## Authentication & Identity

**Auth Provider:**
- Custom JWT-based authentication (no external provider)
  - Implementation: `backend/handlers/auth.go`
  - Access tokens: HS256 JWT signed with `JWT_SECRET`
  - Refresh tokens: in-memory `sync.Map` store (`refreshTokenStore`), 30-day expiry
  - Token blacklist: in-memory `sync.Map` (`tokenBlacklist`) for logout invalidation
  - Password hashing: bcrypt via `golang.org/x/crypto/bcrypt`
  - Reset tokens: crypto-random hex tokens stored in DB with expiry

**Multi-tenancy:**
- Environment → Group → Company hierarchy
- `X-Company-ID` header carries active company context per request
- Users may belong to multiple companies; preferred company stored in localStorage (frontend)
- Role system: `admin` role for privileged operations

**ERP Bridge Auth:**
- X-API-Key header authentication for batch import endpoints (`/api/erp-bridge/import/batch`, `/api/erp-bridge/heartbeat`)
- API key generated per company via `POST /api/erp-bridge/config/generate-api-key`
- Key stored hashed (SHA-256) in `erp_bridge_config` table

**Credential Encryption:**
- RFB and CGIBS OAuth2 secrets encrypted at rest using AES-256-GCM
- Key derivation: SHA-256 of `ENCRYPTION_KEY` env var (falls back to `JWT_SECRET` with warning)
- Implementation: `backend/handlers/crypto.go`

## Monitoring & Observability

**Error Tracking:**
- None (no Sentry, Rollbar, or equivalent detected)

**Metrics (production only):**
- Prometheus — `docker-compose.prod.yml` includes `prom/prometheus:latest` on port 9090
- Grafana — `docker-compose.prod.yml` includes `grafana/grafana:latest` on port 3001
- Config files expected at `monitoring/prometheus.yml` and `monitoring/grafana/`

**Logs:**
- Go backend: `log` stdlib package to stdout — captured by Docker/Coolify
- ERP Bridge Python: file-based logging to `erp-bridge-aws/logs/bridge_YYYYMMDD_HHMMSS.log` + stdout

**Health Check:**
- `GET /api/health` — returns JSON with DB connection status, pool stats, version, and features

## CI/CD & Deployment

**Hosting:**
- Coolify (self-hosted PaaS) — manages container lifecycle
- Traefik — reverse proxy handling TLS termination and routing
- TLS: Let's Encrypt via Traefik `certresolver=letsencrypt`

**CI Pipeline:**
- None detected (no GitHub Actions, GitLab CI, or similar config files found)

**Deployment:**
- `docker-compose.yml` — development/staging (no port exposure on db/redis, uses `fb_net` + external `coolify` network)
- `docker-compose.prod.yml` — production reference (includes Prometheus, Grafana, backup container)
- `scripts/deploy_production.sh` — manual deployment script

## Webhooks & Callbacks

**Incoming:**
- `POST /api/rfb/webhook` — Receives async RFB CBS assessment completion notifications
  - Authentication: none (public endpoint) — validates content internally
  - Handler: `backend/handlers/rfb_apuracao.go` → `RFBWebhookHandler`

**Outgoing:**
- RFB webhook registration: when requesting an assessment, the RFB API is given `RFB_WEBHOOK_URL` (default `https://fbtax.cloud/api/rfb/webhook`) to call back upon completion
- SMTP email: password reset and AI report emails sent via `backend/services/email.go`

## Email (SMTP)

**Provider:**
- Hostinger SMTP (default host `smtp.hostinger.com`, port 465, implicit TLS)
- Configurable via `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM`
- From address default: `contato@fortesbezerra.com.br`

**Email Types:**
- Password reset: `SendPasswordResetEmail()` in `backend/services/email.go`
- AI executive report: `SendAIReportEmail()` in `backend/services/email.go` — HTML with fiscal comparison data

## ERP Integration (External Database)

**Oracle Database (via ERP Bridge daemon):**
- Client: `python-oracledb` thin mode
- Modes:
  - `sap_s4hana`: connects to single Oracle instance (`FCCORP`), reads `s4i_nfe` + `s4i_nfe_impostos` tables, sends JSON batch to `POST /api/erp-bridge/import/batch`
  - `oracle_xml`: connects to per-branch Oracle instances, sends XML multipart (legacy Totvs/Protheus)
- Config: `erp-bridge-aws/config.yaml` (contains DSN, credentials — sensitive file)
- Runs as systemd service (`erp-bridge-aws/erp-bridge.service`) on AWS Linux server
- Heartbeat: `POST /api/erp-bridge/heartbeat` — daemon reports liveness to backend every N minutes

## Environment Configuration

**Required env vars (backend):**
- `DATABASE_URL` — PostgreSQL connection string
- `JWT_SECRET` — JWT signing key (must be set in production)
- `ENCRYPTION_KEY` — AES-256 key for RFB/CGIBS credential encryption (falls back to JWT_SECRET with warning)
- `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` — email sending
- `APP_URL` — base URL for password reset links (default `https://apuracao.fbtax.cloud`)
- `PORT` — HTTP listen port (default `8081`)

**Optional env vars (backend):**
- `RFB_API_URL`, `RFB_TOKEN_URL`, `RFB_WEBHOOK_URL` — override RFB API endpoints
- `ALLOWED_ORIGINS` — comma-separated CORS whitelist (defaults to hardcoded list)
- `COOKIE_SECURE`, `APP_MODULE`, `REDIS_ADDR` — additional config

**Frontend build vars:**
- `VITE_APP_MODULE` — module identifier injected at build time (default `apuracao`)
- `VITE_API_TARGET` — API proxy target for dev server (default `http://localhost:8081`)

**Secrets location:**
- Production secrets injected via Coolify environment variable management (not committed to repo)
- Template: `coolify-env-template.txt` — documents all required vars without values
- Dev: `.env` file (git-ignored, loaded via godotenv)

---

*Integration audit: 2026-05-12*
