# Technology Stack

**Analysis Date:** 2026-05-12

## Languages

**Primary:**
- Go 1.22 - Backend API server (`backend/`)
- TypeScript 5.2 - Frontend SPA (`frontend/src/`)

**Secondary:**
- Python 3 (zoneinfo, oracledb) - ERP Bridge daemon (`erp-bridge-aws/bridge.py`)
- SQL - Database migrations (`backend/migrations/*.sql`, 104+ files)

## Runtime

**Environment:**
- Go: compiled binary, runs in Alpine Linux container
- Node.js: build-time only (Vite bundler), not present at runtime
- Python 3: runtime on AWS server for ERP bridge daemon

**Package Manager:**
- Go: Go modules with vendored dependencies (`backend/vendor/`, `backend/go.mod`)
- Frontend: npm with lockfile (`frontend/package-lock.json`)
- Python: pip (requirements via inline comment: `pip install oracledb requests pyyaml`)

**Lockfile:**
- `frontend/package-lock.json` — present
- `backend/go.sum` — present

## Frameworks

**Core (Backend):**
- Go standard library `net/http` — HTTP server (no external router framework)
- `database/sql` — Database access layer

**Core (Frontend):**
- React 18.3.1 — UI framework
- React Router DOM 6.22.3 — Client-side routing
- TanStack React Query 5.90 — Server state management
- React Hook Form 7.71 + Zod 4.3.6 — Form validation

**UI Components:**
- Radix UI (full suite, ~25 packages) — Headless accessible components
- Tailwind CSS 3.4 + tailwindcss-animate — Styling
- shadcn/ui pattern (CVA + Radix + Tailwind)
- Lucide React 0.363 — Icons
- Recharts 3.7 — Data visualization charts
- Sonner 2.0 — Toast notifications

**Build/Dev:**
- Vite 5.2 with `@vitejs/plugin-react-swc` — Frontend bundler (SWC compiler)
- PostCSS + Autoprefixer — CSS processing
- Docker multi-stage builds — Container packaging

## Key Dependencies

**Critical (Backend):**
- `github.com/golang-jwt/jwt/v5` v5.3.1 — JWT authentication (access + refresh tokens)
- `github.com/lib/pq` v1.11.2 — PostgreSQL driver
- `golang.org/x/crypto` v0.17.0 — bcrypt password hashing
- `golang.org/x/text` v0.14.0 — Unicode text normalization
- `github.com/joho/godotenv` v1.5.1 — `.env` file loading in development

**Critical (Frontend):**
- `react-router-dom` ^6.22.3 — SPA routing
- `@tanstack/react-query` ^5.90.20 — API data fetching and caching
- `zod` ^4.3.6 — Schema validation
- `xlsx` ^0.18.5 — Excel export functionality
- `date-fns` ^4.1.0 — Date formatting and manipulation

**ERP Bridge (Python):**
- `oracledb` — Oracle Database client (thin mode)
- `requests` — HTTP client for FBTax API calls
- `pyyaml` — YAML config parsing
- `sqlite3` (stdlib) — Local tracker database

## Configuration

**Environment:**
- Backend reads from environment variables via `os.Getenv()`
- Development: `.env` file loaded via `godotenv`
- Production: injected by Coolify/Docker Compose environment blocks
- Key backend vars: `DATABASE_URL`, `JWT_SECRET`, `ENCRYPTION_KEY`, `SMTP_*`, `RFB_*`, `APP_URL`, `ALLOWED_ORIGINS`, `PORT`

**Build:**
- `frontend/vite.config.ts` — Vite dev server (port 3000) + API proxy to `http://localhost:8081`
- `frontend/tsconfig.app.json` — TypeScript strict mode, `@/*` path alias → `./src/*`
- `frontend/tailwind.config.js` — Tailwind configuration
- `backend/Dockerfile` — Multi-stage: `golang:1.22-alpine` builder → `alpine:latest` runner

**Timezone:**
- Container timezone set to `America/Sao_Paulo` via `tzdata` package in Dockerfile

## Platform Requirements

**Development:**
- Go 1.22+
- Node.js (for frontend build)
- PostgreSQL 15
- Redis (configured but not imported in Go source — present in docker-compose only)
- Docker + Docker Compose

**Production:**
- Docker with Docker Compose
- Coolify (PaaS orchestrator, external `coolify` Docker network)
- Traefik (reverse proxy, TLS via Let's Encrypt)
- Domains: `apuracao.fbtax.cloud`, `fctax.fcxlabs.com`
- Timezone: America/Sao_Paulo

---

*Stack analysis: 2026-05-12*
