# Guia de Desenvolvimento — FB_APU02

---

## Pré-requisitos

| Ferramenta | Versão mínima | Propósito |
|------------|--------------|-----------|
| Go | 1.22+ | Backend API |
| Node.js | 18+ | Build do frontend (não presente em produção) |
| PostgreSQL | 15 | Banco de dados |
| Docker + Docker Compose | Qualquer recente | Ambiente completo |
| Git | — | Controle de versão |

---

## Configuração do Ambiente Local

### 1. Clonar o repositório

```bash
git clone https://github.com/ferreiracosta/FB_APU02.git
cd FB_APU02
```

### 2. Configurar variáveis de ambiente do Backend

```bash
cp backend/.env.example backend/.env
# Editar backend/.env com as configurações locais
```

Variáveis essenciais em `backend/.env`:

```ini
DATABASE_URL=postgres://fbtax:password@localhost:5432/fbtax_cloud?sslmode=disable
JWT_SECRET=minha-chave-secreta-de-pelo-menos-32-bytes-local
PORT=8081
ALLOWED_ORIGINS=http://localhost:3000
```

### 3. Subir o banco de dados

```bash
# Apenas o PostgreSQL via Docker:
docker compose up -d postgres

# Ou a stack completa:
docker compose up -d
```

### 4. Instalar dependências do Frontend

```bash
cd frontend && npm install
```

---

## Rodando em Desenvolvimento

### Backend Go

```bash
cd backend
go run main.go
# Servidor na porta 8081
# Migrations executadas automaticamente ao conectar ao DB
```

### Frontend React

```bash
cd frontend
npm run dev
# Servidor na porta 3000
# Proxy /api/* → http://localhost:8081 (configurado no vite.config.ts)
```

### ERP Bridge Python (opcional)

```bash
cd erp-bridge-aws
pip install oracledb requests pyyaml
cp config.yaml.example config.yaml
# Editar config.yaml com credenciais Oracle e FBTax

# Importação manual de período:
python bridge.py --data 2026-06-01 --data-fim 2026-06-30

# Modo daemon (não recomendado em dev sem Oracle disponível):
python bridge.py --daemon
```

---

## Comandos Úteis

### Backend

```bash
# Rodar com hot-reload (usando air):
# Instalar: go install github.com/cosmtrek/air@latest
air

# Build:
cd backend && go build -o ../bin/server .

# Verificar compilação:
go build ./...

# Testes Go:
go test ./...

# Formatar código:
gofmt -w .

# Verificar imports:
goimports -w .
```

### Frontend

```bash
cd frontend

# Desenvolvimento:
npm run dev

# Build de produção:
npm run build

# Preview do build:
npm run preview

# Type checking:
npx tsc -p tsconfig.app.json --noEmit

# Linting:
npm run lint

# Testes:
npm run test
```

### Banco de Dados

```bash
# Conectar ao PostgreSQL local:
psql -U fbtax -d fbtax_cloud

# Ver migrations executadas:
SELECT filename, executed_at FROM schema_migrations ORDER BY executed_at;

# Verificar tabelas:
\dt

# Refresh manual de materialized view:
REFRESH MATERIALIZED VIEW CONCURRENTLY mv_mercadorias;
```

---

## Adicionando Novas Features

### Novo Handler Backend

1. Criar ou abrir o arquivo no domínio correspondente em `backend/handlers/`
2. Seguir o padrão factory:

```go
func NovaFuncionalidadeHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        userID := GetUserIDFromContext(r)
        companyID := GetEffectiveCompanyID(db, userID, r.Header.Get("X-Company-ID"))
        
        var req struct {
            Campo string `json:"campo"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            jsonErr(w, http.StatusBadRequest, "Dados inválidos")
            return
        }
        
        // ... lógica ...
        
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
    }
}
```

3. Registrar a rota em `backend/main.go`:

```go
http.HandleFunc("/api/nova-funcionalidade", withAuth(handlers.NovaFuncionalidadeHandler(db), ""))
```

### Nova Migration

```bash
# Nomear com o próximo número sequencial disponível:
# Ex: 114_descricao_breve.sql
touch backend/migrations/114_descricao_breve.sql
```

**Regras:**
- Nunca alterar migrations já executadas em produção
- Usar `IF NOT EXISTS` e `IF EXISTS` para idempotência
- Testar com o banco limpo antes de fazer merge

### Nova Página Frontend

1. Criar `frontend/src/pages/NomePagina.tsx` com `export default function NomePagina()`:

```tsx
export default function NomePagina() {
    // useAuth() e useFiliais() estão disponíveis se necessário
    // fetch('/api/...') — headers injetados automaticamente pelo AuthContext
    
    return <div>...</div>
}
```

2. Adicionar rota em `frontend/src/App.tsx`:

```tsx
import NomePagina from './pages/NomePagina'
// ...
<Route path="/modulo/pagina" element={<NomePagina />} />
```

3. Se for uma nova aba de módulo existente, adicionar em `frontend/src/lib/navigation.ts`:

```typescript
modulo: {
    label: 'Módulo',
    tabs: [
        // ...tabs existentes...
        { label: 'Nova Aba', path: '/modulo/pagina' },
    ],
}
```

4. Se for um novo módulo inteiro:

```typescript
// Em modules:
novoModulo: {
    label: 'Novo Módulo',
    tabs: [{ label: 'Principal', path: '/novo-modulo' }],
},

// Em getActiveModule():
if (pathname.startsWith('/novo-modulo')) return 'novoModulo'
```

---

## Estrutura de Commits

O projeto usa commits convencionais:

```
feat(domínio): descrição breve da feature
fix(domínio): descrição do bug corrigido
docs(task-id): descrição da documentação
sec(area): correção de segurança
perf(area): melhoria de performance
refactor(area): refatoração sem mudança de comportamento
```

---

## Convenções Obrigatórias

### Backend Go

1. **Todos os handlers** retornam `http.HandlerFunc` (factory pattern)
2. **Multi-tenancy:** Sempre usar `GetEffectiveCompanyID` — nunca assumir `company_id` sem verificar
3. **Errors:** Usar `jsonErr(w, status, msg)` para novos handlers; `sanitizeDBErr` para erros de DB
4. **Migrations:** Nunca alterar arquivo `.sql` já commitado — criar novo arquivo
5. **Logging:** `log.Printf("[HandlerName] description: %v", err)` antes de retornar erro

### Frontend TypeScript

1. **Headers:** Não adicionar `Authorization` ou `X-Company-ID` manualmente — o interceptor cuida disso
2. **Tipos:** TypeScript strict mode — definir tipos para todas as props e retornos de API
3. **Formulários simples:** `useState` + fetch manual; **formulários complexos:** React Hook Form + Zod
4. **Toast:** `toast.success()` / `toast.error()` via Sonner — não `alert()`
5. **Exportações:** Usar o helper `exportToExcel()` de `@/lib/exportToExcel`
6. **Storage:** Usar constantes de `@/lib/storageKeys` — nunca string literals

---

## Debugging

### Backend

```bash
# Ver logs em tempo real (Docker):
docker logs -f fb_apu02_backend

# Adicionar logs temporários:
log.Printf("[DEBUG] companyID=%s, userID=%s", companyID, userID)
```

### Frontend

```bash
# Network: inspecionar as chamadas /api/* no DevTools > Network
# AuthContext: verificar tokenRef e companyIdRef em closure do fetch interceptor
# Estado: React DevTools (extensão do browser)
```

### Banco de Dados

```sql
-- Verificar solicitações RFB travadas:
SELECT id, status, error_code, created_at FROM rfb_requests 
WHERE status NOT IN ('completed', 'error') 
ORDER BY created_at;

-- Ver migrations recentes:
SELECT * FROM schema_migrations ORDER BY executed_at DESC LIMIT 10;

-- Contar documentos por empresa:
SELECT company_id, COUNT(*) FROM nfe_entradas GROUP BY company_id;
```

---

## Testes

```bash
# Backend — Go tests (poucos no momento):
cd backend && go test ./...

# Frontend — Vitest:
cd frontend && npm run test

# Type check frontend:
cd frontend && npx tsc -p tsconfig.app.json --noEmit
```

> **Nota:** A cobertura de testes é baixa. Priorizar testes de integração para handlers críticos (RFB, auth, multi-tenancy).
