# Testing Patterns
<!-- Last mapped: 2026-05-12 -->

## Overview

Cobertura de testes muito baixa — 3 arquivos de teste no total para um projeto de médio porte. Não há step de testes no CI; o deploy ocorre diretamente após o build Docker.

## Test Files

| Arquivo | Tipo | Cobertura |
|---------|------|-----------|
| `backend/middleware/tenant_test.go` | Unit (Go) | Middleware multi-tenant |
| `frontend/src/lib/utils.test.ts` | Unit (Vitest) | Utilitários frontend |

> Nota: existe referência a `tests/integration_test.go` mas com padrão soft-fail — retorna sem `t.Fatal` se o servidor não estiver acessível.

## Backend (Go)

**Framework:** `testing` padrão da stdlib Go (sem framework externo)

**Convenção de nomes:** `Test{Função}_{Cenário}` (ex: `TestExtractTenant_ValidHeader`)

**Mocking:** Não há framework de mock. Dependências são passadas por parâmetro ou via interface.

**Padrão observado (tenant_test.go):**
```go
func TestExtractTenant_ValidHeader(t *testing.T) {
    req := httptest.NewRequest("GET", "/", nil)
    req.Header.Set("X-Tenant-ID", "empresa-abc")
    // ...
}
```

**Testes de integração:** Padrão soft-fail — se o servidor não responder, o teste loga aviso e retorna sem falhar. Isso significa que os testes de integração nunca bloqueiam o CI mesmo quando a infra não está disponível.

## Frontend (TypeScript)

**Framework:** Vitest (referenciado, mas não há `vitest.config.ts` nem scripts de test configurados no `package.json`)

**Arquivo de teste:** `frontend/src/lib/utils.test.ts` — testa utilitários de `utils.ts`

**Estado:** Vitest não está devidamente configurado como dependência de desenvolvimento; os testes não rodam no CI.

## CI Pipeline

**Localização:** `.github/workflows/deploy-production.yml`, `deploy-staging.yml`, `deploy-cliente-aws.yml`

**Etapas de teste no CI:** Nenhuma. O pipeline faz build Docker e faz deploy diretamente.

## Coverage

**Thresholds:** Não definidos.

**Ferramentas de coverage:** Nenhuma configurada.

## Gaps e Riscos

- Sem testes para handlers HTTP do backend (auth, NF-e, RFB, ERP bridge, etc.)
- Sem testes para contexts React (`AuthContext`, `FilialContext`)
- Pipeline CI não roda nenhum teste antes do deploy
- Testes de integração usam soft-fail — nunca falham mesmo com server down
- Vitest não está configurado corretamente no frontend

## Recomendações para Novos Testes

**Backend:** Usar `net/http/httptest` para handlers; `database/sql` com DB de teste para integração.

**Frontend:** Configurar Vitest + Testing Library para componentes críticos (Login, FileUpload, FilialSelector).
