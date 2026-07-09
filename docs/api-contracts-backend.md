# Contratos de API — Backend Go

**Base URL:** `https://fctax.fcxlabs.com` (produção) / `http://localhost:8081` (dev)  
**Content-Type:** `application/json` (todos os endpoints, salvo multipart indicado)  
**Autenticação padrão:** `Authorization: Bearer {JWT}` + `X-Company-ID: {uuid}`

---

## Convenções

- **Erro padrão:** `{ "error": "mensagem descritiva" }` com status HTTP adequado
- **Multi-tenant:** Todos os endpoints de dados fiscais são implicitamente filtrados por `company_id` (via header `X-Company-ID`)
- **Paginação:** Parâmetros `limit` e `offset` onde aplicável
- **Datas:** Formato `YYYY-MM-DD` ou `MM/YYYY` (competência)

---

## Autenticação

### POST `/api/auth/register`
**Auth:** Público  
**Body:**
```json
{
  "email": "user@domain.com",
  "password": "MinhaSenh@123",
  "full_name": "Nome Completo",
  "company_name": "Empresa LTDA",
  "cnpj": "12345678000190"
}
```
**Resposta 201:** `{ "message": "Usuário criado com sucesso" }`

### POST `/api/auth/login`
**Auth:** Público  
**Body:** `{ "email": "...", "password": "..." }`  
**Resposta 200:** `{ "token": "eyJ..." }` + `Set-Cookie: refresh_token` (HttpOnly, 7 dias)

### POST `/api/auth/logout`
**Auth:** Bearer  
**Resposta 200:** `{ "message": "Logout realizado" }` + limpa refresh cookie

### POST `/api/auth/refresh`
**Auth:** Público (usa cookie refresh_token)  
**Resposta 200:** `{ "token": "eyJ..." }` (novo JWT + rotaciona refresh token)

### GET `/api/auth/me`
**Auth:** Bearer  
**Resposta 200:**
```json
{
  "id": "uuid",
  "email": "user@domain.com",
  "full_name": "Nome",
  "role": "admin",
  "trial_ends_at": "2026-12-31T00:00:00Z"
}
```

### POST `/api/auth/forgot-password`
**Auth:** Público (rate limit: 3/hora por IP)  
**Body:** `{ "email": "..." }`  
**Resposta 200:** `{ "message": "E-mail enviado se o endereço existir" }`

### POST `/api/auth/reset-password`
**Auth:** Público  
**Body:** `{ "token": "uuid-do-email", "password": "NovaSenh@123" }`

### POST `/api/auth/change-password`
**Auth:** Bearer (rate limit: 5/15min por userID)  
**Body:** `{ "current_password": "...", "new_password": "..." }`

---

## Usuário e Hierarquia

### GET `/api/user/hierarchy`
**Resposta 200:**
```json
{
  "environments": [
    {
      "id": "uuid", "name": "Ambiente Produção",
      "groups": [
        {
          "id": "uuid", "name": "Grupo Ferreira Costa",
          "companies": [
            { "id": "uuid", "name": "Ferreira Costa Recife", "cnpj": "12345678000190" }
          ]
        }
      ]
    }
  ]
}
```

### GET `/api/user/companies`
**Resposta 200:** `[{ "id": "uuid", "name": "...", "cnpj": "..." }]`

### POST `/api/user/preferred-company`
**Body:** `{ "company_id": "uuid" }`

---

## Apuração RFB (CBS)

### POST `/api/rfb/apuracao/solicitar`
**Body:**
```json
{
  "competencia": "06/2026",
  "cnpj_base": "12345678"
}
```
**Resposta 201:** `{ "id": "uuid", "tiquete": "...", "status": "requested" }`

### GET `/api/rfb/apuracao/status`
**Query:** `?competencia=06/2026`  
**Resposta 200:**
```json
[{
  "id": "uuid",
  "status": "completed",
  "competencia": "06/2026",
  "cnpj_base": "12345678",
  "tiquete": "...",
  "erro_msg": null,
  "resumo": {
    "total_debitos": 150,
    "valor_cbs_total": 12500.50,
    "valor_cbs_extinto": 8000.00,
    "valor_cbs_nao_extinto": 4500.50
  }
}]
```

### GET `/api/rfb/apuracao/{id}`
**Resposta 200:** Detalhes completos da solicitação + lista paginada de débitos

### POST `/api/rfb/apuracao/abort`
**Body:** `{ "id": "uuid" }`  
**Comportamento:** Muda status para `error` com `error_code='ABORTED'` para requests em `pending`, `requested`, `webhook_received`, `downloading`, `reprocessing`

### POST `/api/rfb/apuracao/resolicitar`
**Body:** `{ "competencia": "06/2026" }`  
**Comportamento:** Nova solicitação para o período, arquivando a anterior

### GET `/api/rfb/debitos`
**Query:** `?competencia=06/2026&cnpj_filial=12345678000190&modelo=55&limit=100&offset=0`  
**Resposta 200:**
```json
{
  "items": [{
    "chave_dfe": "35260612345678000190550010000001001234567890",
    "data_dfe_emissao": "2026-06-15",
    "ni_emitente": "12345678000190",
    "ni_adquirente": "98765432000150",
    "valor_cbs_total": 125.50,
    "valor_cbs_extinto": 80.00,
    "valor_cbs_nao_extinto": 45.50,
    "situacao_debito": "EXTINTO_PARCIAL",
    "modelo_dfe": 55
  }],
  "total": 1250
}
```

### GET `/api/rfb/creditos/lista`
**Query:** `?competencia=06/2026&limit=100&offset=0`

---

## Apuração CGIBS (IBS)

Estrutura idêntica ao RFB — substitua `/api/rfb/` por `/api/cgibs/`.

### POST `/api/cgibs/apuracao/solicitar`
### GET `/api/cgibs/apuracao/status`
### GET `/api/cgibs/debitos`

---

## Credenciais

### GET `/api/rfb/credentials`
**Resposta 200:**
```json
{
  "company_id": "uuid",
  "cnpj_matriz": "12345678",
  "client_id": "***",
  "ativo": true,
  "agendamento_ativo": true,
  "horario_agendamento": "06:00",
  "ambiente": "producao"
}
```

### POST `/api/rfb/credentials`
**Body:**
```json
{
  "cnpj_matriz": "12345678",
  "client_id": "...",
  "client_secret": "...",
  "ambiente": "producao"
}
```

### PUT `/api/rfb/credentials/agendamento`
**Body:** `{ "ativo": true, "horario": "06:00" }`

---

## NF-e e CT-e

### POST `/api/nfe-entradas/upload`
**Auth:** Bearer | **Content-Type:** `multipart/form-data`  
**Campo:** `xmls` — um ou mais arquivos XML de NF-e  
**Resposta 200:** `{ "inserted": 45, "updated": 5, "errors": 0 }`

### GET `/api/nfe-entradas`
**Query:** `?mes_ano=06/2026&emit_cnpj=12345678000190&cancelado=false&limit=100&offset=0`  
**Resposta 200:**
```json
{
  "items": [{
    "id": "uuid",
    "chave_nfe": "35260612345678000190550010000001001234567890",
    "forn_cnpj": "12345678000190",
    "forn_nome": "Fornecedor LTDA",
    "data_emissao": "2026-06-15",
    "v_nf": 1250.00,
    "v_pis": 25.00,
    "v_cofins": 115.00,
    "v_cbs": 140.00,
    "cancelado": false
  }],
  "total": 5280
}
```

### GET `/api/nfe-entradas/filiais`
**Resposta 200:** `[{ "cnpj": "12345678000190", "nome": "FC - Recife", "apelido": "Recife" }]`

### GET `/api/nfe-entradas/competencias`
**Resposta 200:** `[{ "mes_ano": "06/2026", "total": 1250 }]`

*Padrão idêntico para `/api/nfe-saidas/*` e `/api/cte-entradas/*`*

---

## Malha Fina

### GET `/api/malha-fina/nfe-entradas`
**Query:** `?competencia=06/2026&cnpj_filial=...`  
**Resposta 200:** Documentos presentes na RFB mas ausentes ou divergentes na base local

### GET `/api/malha-fina/nfe-entradas/resumo`
**Resposta 200:** `{ "total": 150, "valor_divergente": 45000.00, "competencias": [...] }`

*Padrão idêntico para `/api/malha-fina/nfe-saidas/*` e `/api/malha-fina/cte/*`*

---

## Pagamentos a Fornecedores

### POST `/api/pagamentos-fornecedores/import`
**Content-Type:** `multipart/form-data` | **Campo:** `file` — CSV de pagamentos  
**CSV esperado:** `cnpj_fornecedor,nome,valor,data_pagamento,nota_fiscal`

### GET `/api/pagamentos-fornecedores`
**Query:** `?mes_ano=06/2026&cnpj_fornecedor=...&limit=100&offset=0`

---

## ERP Bridge

### GET `/api/erp-bridge/config`
**Resposta 200:**
```json
{
  "ativo": true,
  "horario": "02:00",
  "dias_retroativos": 7,
  "reset_tracker": false,
  "api_key": "***"
}
```

### GET `/api/erp-bridge/runs`
**Query:** `?limit=20&offset=0`  
**Resposta 200:** Histórico de execuções com status e totais

### GET `/api/erp-bridge/runs/{id}`
**Resposta 200:** Detalhe da execução com itens por servidor

### POST `/api/erp-bridge/trigger`
**Body:** `{ "data_ini": "2026-06-01", "data_fim": "2026-06-30" }`  
**Comportamento:** Enfileira run em `erp_bridge_pending` para o daemon processar

### POST `/api/erp-bridge/import/batch`
**Auth:** `X-API-Key` (sem JWT) | **Content-Type:** `application/json`  
**Body:**
```json
{
  "documents": [{
    "direct": 2,
    "chave": "35260612345678000190550010000001001234567890",
    "modelo": "55",
    "serie": "001",
    "numero": "100001",
    "data_emissao": "2026-06-15",
    "mes_ano": "06/2026",
    "emit_cnpj": "12345678000190",
    "dest_cnpj": "98765432000150",
    "cancelado": false,
    "v_total": 1250.00,
    "v_cbs": 140.00,
    "v_ibs_uf": 45.00,
    "v_ibs_mun": 15.00,
    "cfop": "5102"
  }]
}
```
**Resposta 200:** `{ "inserted": 980, "ignored": 20, "errors": 0 }`

### POST `/api/erp-bridge/parceiros/sync`
**Auth:** `X-API-Key`  
**Body:** `{ "parceiros": [{ "cnpj": "12345678000190", "nome": "Empresa LTDA" }] }`

---

## ERP Bridge (Bridge Internal — autenticação X-API-Key)

### POST `/api/erp-bridge/heartbeat`
**Auth:** `X-API-Key` | **Body:** vazio  
**Resposta 200:** `{ "ok": true }`

### GET `/api/erp-bridge/credentials`
**Auth:** `X-API-Key`  
**Resposta 200:** Credenciais criptografadas para o daemon (fbtax_email, fbtax_password, etc.)

---

## Webhook RFB

### POST `/api/rfb/webhook`
**Auth:** HMAC-SHA256 do payload com `JWT_SECRET` (header `X-RFB-Signature`)  
**Comportamento:** Dispara download + processamento em goroutine assíncrona  
**Resposta 200:** `{ "ok": true }` (imediatamente, antes do processamento completar)

---

## Admin (role: admin obrigatório)

### GET `/api/admin/users`
**Resposta 200:** Lista de todos os usuários com roles e environments

### POST `/api/admin/users/create`
**Body:** `{ "email": "...", "full_name": "...", "role": "user", "environment_id": "uuid" }`

### POST `/api/admin/limpeza-base`
**Body:** `{ "tabelas": ["nfe_entradas", "nfe_saidas", "cte_entradas"], "competencia": "06/2026" }`  
**Comportamento:** Remove dados das tabelas selecionadas para a empresa ativa

### POST `/api/admin/refresh-views`
**Comportamento:** Executa `REFRESH MATERIALIZED VIEW CONCURRENTLY` nas views relevantes

---

## Health Check

### GET `/api/health`
**Auth:** Público  
**Resposta 200:**
```json
{
  "status": "ok",
  "db": "connected",
  "pool": {
    "open": 5,
    "idle": 3,
    "wait": 0
  }
}
```
