# Tech Spec — Migração para SAP S4/HANA (ERP Bridge v2)
## FB_APU02 · Apuração Assistida IBS/CBS · Ferreira Costa

**Data:** 2026-03-26
**Status:** WIP — aprovação pendente
**Autor:** Claudio Bezerra + Claude

---

## 1. Contexto e Motivação

A Ferreira Costa migrou o ERP de Oracle (por filial) para **SAP S4/HANA** com instância única (`FCCORP`). As tabelas de origem passam a ser `s4i_nfe` e `s4i_nfe_impostos`, que consolidam todos os documentos fiscais de todas as filiais em um único servidor.

### Mudanças estruturais

| Antes (Oracle por filial) | Depois (SAP S4/HANA FCCORP) |
|---|---|
| ~10 servidores Oracle (fcgus, fcimb, ...) | 1 servidor Oracle: FCCORP |
| XML CLOB por documento | Campos estruturados em tabelas |
| Tabela `dfe_xml` (XML bruto) | Removida — não há mais XML |
| DANFE gerado a partir do XML | Removida — não há XML |
| Campos ICMSTot completos | Somente `v_nf` / `v_prest` |
| IBS/CBS via tags XML NF-e 4.0 | Pivot CBS3/IB3M/IB3S dos impostos SAP |

---

## 2. Query Oracle SAP S4/HANA (Base)

### 2a. Query original do cliente (referência)

```sql
select nn.DIRECT, case when nn.direct = '1' then 'Entrada' else 'Saida' end tipo_mov,
 nn.NFEID chave, nn.CNPJ_EMIT emitente, nn.CNPJ_DEST destinatario, nn.NFENUM nrnota,
 trunc(nn.credat) data_autorizacao, trunc(nn.DOCDAT) DataEmissao, nn.SERIES serie,
 nn.NFTOT totalnota, ni.ITMNUM seqitem, ni.TAXTYP tipo_imposto, ni.BASE vlr_base,
 ni.RATE aliquota, ni.TAXVAL vlr_imposto
from s4i_nfe nn, s4i_nfe_impostos ni
where trunc(nn.credat) = '25/03/2026'
and nn.CANCELADO = 'N'
and ni.NFEID = nn.NFEID
and ni.TAXTYP in ('CBS3','IB3M','IB3S')
```

**Observação:** retorna 3 linhas por nota (uma por tipo de imposto). Precisamos agregar.

### 2b. Query agregada (recomendada para importação)

Retorna **1 linha por documento**, com pivot dos impostos:

```sql
SELECT
    nn.DIRECT,
    nn.NFEID                                           AS chave,
    SUBSTR(nn.NFEID, 21, 2)                            AS modelo,
    nn.SERIES                                          AS serie,
    nn.NFENUM                                          AS numero,
    TRUNC(nn.DOCDAT)                                   AS data_emissao,
    TRUNC(nn.CREDAT)                                   AS data_autorizacao,
    TO_CHAR(TRUNC(nn.DOCDAT), 'MM/YYYY')               AS mes_ano,
    nn.CNPJ_EMIT                                       AS emit_cnpj,
    nn.CNPJ_DEST                                       AS dest_cnpj,
    nn.NFTOT                                           AS v_total,
    MAX(CASE WHEN ni.TAXTYP = 'CBS3' THEN ni.BASE  ELSE 0 END) AS v_bc_ibs_cbs,
    SUM(CASE WHEN ni.TAXTYP = 'IB3S' THEN ni.TAXVAL ELSE 0 END) AS v_ibs_uf,
    SUM(CASE WHEN ni.TAXTYP = 'IB3M' THEN ni.TAXVAL ELSE 0 END) AS v_ibs_mun,
    SUM(CASE WHEN ni.TAXTYP IN ('IB3S','IB3M') THEN ni.TAXVAL ELSE 0 END) AS v_ibs,
    SUM(CASE WHEN ni.TAXTYP = 'CBS3' THEN ni.TAXVAL ELSE 0 END) AS v_cbs
FROM s4i_nfe nn
LEFT JOIN s4i_nfe_impostos ni
  ON ni.NFEID = nn.NFEID
 AND ni.TAXTYP IN ('CBS3','IB3M','IB3S')
WHERE TRUNC(nn.CREDAT) BETWEEN :data_ini AND :data_fim
  AND nn.CANCELADO = 'N'
GROUP BY
    nn.DIRECT, nn.NFEID, nn.SERIES, nn.NFENUM,
    nn.DOCDAT, nn.CREDAT, nn.CNPJ_EMIT, nn.CNPJ_DEST, nn.NFTOT
ORDER BY nn.CREDAT, nn.NFEID
```

**Mapeamento de impostos:**

| SAP TAXTYP | Campo PostgreSQL |
|---|---|
| CBS3 | v_bc_ibs_cbs (BASE), v_cbs (TAXVAL) |
| IB3S | v_ibs_uf (TAXVAL) |
| IB3M | v_ibs_mun (TAXVAL) |
| IB3S + IB3M | v_ibs (soma dos dois) |

---

## 3. Lógica de Roteamento

O campo `DIRECT` mais o `modelo` (posição 21-22 da chave de 44 dígitos) determina a tabela de destino:

| DIRECT | Modelo (pos 21-22) | Tabela Destino |
|---|---|---|
| `2` (saída) | 55 (NF-e), 65 (NFC-e), 62, 66, 67 | `nfe_saidas` |
| `1` (entrada) | 55 (NF-e), 62, 65 (NFC-e) | `nfe_entradas` |
| `1` (entrada) | 57 (CT-e), 66, 67 | `cte_entradas` |

---

## 4. Novas Tabelas Simplificadas

### 4a. Colunas REMOVIDAS das tabelas existentes

**`nfe_saidas` — remover:**
- `nat_op`, `emit_nome`, `emit_uf`, `emit_municipio`
- `dest_nome`, `dest_uf`, `dest_c_mun`
- `v_bc`, `v_icms`, `v_icms_deson`, `v_fcp`, `v_bc_st`, `v_st`, `v_fcp_st`, `v_fcp_st_ret`
- `v_prod`, `v_frete`, `v_seg`, `v_desc`, `v_ii`, `v_ipi`, `v_ipi_devol`, `v_pis`, `v_cofins`, `v_outro`
- `v_cred_pres_ibs`, `v_cred_pres_cbs`

**`nfe_entradas` — remover:**
- `nat_op`, `forn_nome`, `forn_uf`, `forn_municipio`
- `dest_nome`, `dest_uf`, `dest_c_mun`
- `v_bc`, `v_icms`, `v_icms_deson`, `v_fcp`, `v_bc_st`, `v_st`, `v_fcp_st`, `v_fcp_st_ret`
- `v_prod`, `v_frete`, `v_seg`, `v_desc`, `v_ii`, `v_ipi`, `v_ipi_devol`, `v_pis`, `v_cofins`, `v_outro`
- `v_cred_pres_ibs`, `v_cred_pres_cbs`

**`cte_entradas` — remover:**
- `nat_op`, `cfop`, `modal`, `emit_nome`, `emit_uf`
- `rem_cnpj_cpf`, `rem_nome`, `rem_uf`
- `dest_nome`, `dest_uf`
- `v_rec`, `v_carga`, `v_bc_icms`, `v_icms`

### 4b. Colunas ADICIONADAS

Todas as 3 tabelas recebem:
- `data_autorizacao DATE` — data de autorização SEFAZ (CREDAT do SAP)

### 4c. Schema final (após migração)

**`nfe_saidas`:**
```
id, company_id, chave_nfe, modelo, serie, numero_nfe,
data_emissao, data_autorizacao (NEW), mes_ano,
emit_cnpj, dest_cnpj_cpf,
v_nf,
v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
created_at
```

**`nfe_entradas`:**
```
id, company_id, chave_nfe, modelo, serie, numero_nfe,
data_emissao, data_autorizacao (NEW), mes_ano,
forn_cnpj, dest_cnpj_cpf,
v_nf,
v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
created_at
```

**`cte_entradas`:**
```
id, company_id, chave_cte, modelo, serie, numero_cte,
data_emissao, data_autorizacao (NEW), mes_ano,
emit_cnpj, dest_cnpj_cpf,
v_prest,
v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun, v_ibs, v_cbs,
created_at
```

---

## 5. Plano de Implementação — Backend

### 5.1 Migrations (PostgreSQL)

#### Migration 080 — `erp_type` na configuração

```sql
-- 080_erp_bridge_erp_type.sql
ALTER TABLE erp_bridge_config
  ADD COLUMN IF NOT EXISTS erp_type TEXT NOT NULL DEFAULT 'oracle_xml';

COMMENT ON COLUMN erp_bridge_config.erp_type IS
  'oracle_xml = por filial com XML (legado) | sap_s4hana = tabela s4i_nfe (novo)';
```

#### Migration 081 — Simplificar `nfe_saidas`

```sql
-- 081_simplify_nfe_saidas.sql
ALTER TABLE nfe_saidas
  ADD COLUMN IF NOT EXISTS data_autorizacao DATE;

-- Remover ICMSTot
ALTER TABLE nfe_saidas
  DROP COLUMN IF EXISTS nat_op,
  DROP COLUMN IF EXISTS emit_nome,
  DROP COLUMN IF EXISTS emit_uf,
  DROP COLUMN IF EXISTS emit_municipio,
  DROP COLUMN IF EXISTS dest_nome,
  DROP COLUMN IF EXISTS dest_uf,
  DROP COLUMN IF EXISTS dest_c_mun,
  DROP COLUMN IF EXISTS v_bc,
  DROP COLUMN IF EXISTS v_icms,
  DROP COLUMN IF EXISTS v_icms_deson,
  DROP COLUMN IF EXISTS v_fcp,
  DROP COLUMN IF EXISTS v_bc_st,
  DROP COLUMN IF EXISTS v_st,
  DROP COLUMN IF EXISTS v_fcp_st,
  DROP COLUMN IF EXISTS v_fcp_st_ret,
  DROP COLUMN IF EXISTS v_prod,
  DROP COLUMN IF EXISTS v_frete,
  DROP COLUMN IF EXISTS v_seg,
  DROP COLUMN IF EXISTS v_desc,
  DROP COLUMN IF EXISTS v_ii,
  DROP COLUMN IF EXISTS v_ipi,
  DROP COLUMN IF EXISTS v_ipi_devol,
  DROP COLUMN IF EXISTS v_pis,
  DROP COLUMN IF EXISTS v_cofins,
  DROP COLUMN IF EXISTS v_outro,
  DROP COLUMN IF EXISTS v_cred_pres_ibs,
  DROP COLUMN IF EXISTS v_cred_pres_cbs;

-- Garantir IBS/CBS NOT NULL DEFAULT 0
ALTER TABLE nfe_saidas
  ALTER COLUMN v_bc_ibs_cbs SET DEFAULT 0,
  ALTER COLUMN v_ibs_uf      SET DEFAULT 0,
  ALTER COLUMN v_ibs_mun     SET DEFAULT 0,
  ALTER COLUMN v_ibs         SET DEFAULT 0,
  ALTER COLUMN v_cbs         SET DEFAULT 0;

UPDATE nfe_saidas SET
  v_bc_ibs_cbs = COALESCE(v_bc_ibs_cbs, 0),
  v_ibs_uf     = COALESCE(v_ibs_uf, 0),
  v_ibs_mun    = COALESCE(v_ibs_mun, 0),
  v_ibs        = COALESCE(v_ibs, 0),
  v_cbs        = COALESCE(v_cbs, 0);

ALTER TABLE nfe_saidas
  ALTER COLUMN v_bc_ibs_cbs SET NOT NULL,
  ALTER COLUMN v_ibs_uf      SET NOT NULL,
  ALTER COLUMN v_ibs_mun     SET NOT NULL,
  ALTER COLUMN v_ibs         SET NOT NULL,
  ALTER COLUMN v_cbs         SET NOT NULL;
```

#### Migration 082 — Simplificar `nfe_entradas`

```sql
-- 082_simplify_nfe_entradas.sql
ALTER TABLE nfe_entradas
  ADD COLUMN IF NOT EXISTS data_autorizacao DATE;

ALTER TABLE nfe_entradas
  DROP COLUMN IF EXISTS nat_op,
  DROP COLUMN IF EXISTS forn_nome,
  DROP COLUMN IF EXISTS forn_uf,
  DROP COLUMN IF EXISTS forn_municipio,
  DROP COLUMN IF EXISTS dest_nome,
  DROP COLUMN IF EXISTS dest_uf,
  DROP COLUMN IF EXISTS dest_c_mun,
  DROP COLUMN IF EXISTS v_bc,
  DROP COLUMN IF EXISTS v_icms,
  DROP COLUMN IF EXISTS v_icms_deson,
  DROP COLUMN IF EXISTS v_fcp,
  DROP COLUMN IF EXISTS v_bc_st,
  DROP COLUMN IF EXISTS v_st,
  DROP COLUMN IF EXISTS v_fcp_st,
  DROP COLUMN IF EXISTS v_fcp_st_ret,
  DROP COLUMN IF EXISTS v_prod,
  DROP COLUMN IF EXISTS v_frete,
  DROP COLUMN IF EXISTS v_seg,
  DROP COLUMN IF EXISTS v_desc,
  DROP COLUMN IF EXISTS v_ii,
  DROP COLUMN IF EXISTS v_ipi,
  DROP COLUMN IF EXISTS v_ipi_devol,
  DROP COLUMN IF EXISTS v_pis,
  DROP COLUMN IF EXISTS v_cofins,
  DROP COLUMN IF EXISTS v_outro,
  DROP COLUMN IF EXISTS v_cred_pres_ibs,
  DROP COLUMN IF EXISTS v_cred_pres_cbs;
```

#### Migration 083 — Simplificar `cte_entradas`

```sql
-- 083_simplify_cte_entradas.sql
ALTER TABLE cte_entradas
  ADD COLUMN IF NOT EXISTS data_autorizacao DATE,
  ADD COLUMN IF NOT EXISTS v_ibs_uf  NUMERIC(15,2) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS v_ibs_mun NUMERIC(15,2) NOT NULL DEFAULT 0;

ALTER TABLE cte_entradas
  DROP COLUMN IF EXISTS nat_op,
  DROP COLUMN IF EXISTS cfop,
  DROP COLUMN IF EXISTS modal,
  DROP COLUMN IF EXISTS emit_nome,
  DROP COLUMN IF EXISTS emit_uf,
  DROP COLUMN IF EXISTS rem_cnpj_cpf,
  DROP COLUMN IF EXISTS rem_nome,
  DROP COLUMN IF EXISTS rem_uf,
  DROP COLUMN IF EXISTS dest_nome,
  DROP COLUMN IF EXISTS dest_uf,
  DROP COLUMN IF EXISTS v_rec,
  DROP COLUMN IF EXISTS v_carga,
  DROP COLUMN IF EXISTS v_bc_icms,
  DROP COLUMN IF EXISTS v_icms;

-- Normalizar IBS/CBS: nullable → NOT NULL DEFAULT 0
UPDATE cte_entradas SET
  v_bc_ibs_cbs = COALESCE(v_bc_ibs_cbs, 0),
  v_ibs        = COALESCE(v_ibs, 0),
  v_cbs        = COALESCE(v_cbs, 0);

ALTER TABLE cte_entradas
  ALTER COLUMN v_bc_ibs_cbs SET NOT NULL,
  ALTER COLUMN v_bc_ibs_cbs SET DEFAULT 0,
  ALTER COLUMN v_ibs         SET NOT NULL,
  ALTER COLUMN v_ibs         SET DEFAULT 0,
  ALTER COLUMN v_cbs         SET NOT NULL,
  ALTER COLUMN v_cbs         SET DEFAULT 0;
```

#### Migration 084 — DROP `dfe_xml`

```sql
-- 084_drop_dfe_xml.sql
DROP TABLE IF EXISTS dfe_xml;
```

---

### 5.2 Novo Endpoint de Importação Batch

**`POST /api/erp-bridge/import/batch`** — autenticado via `X-API-Key`

Recebe JSON com array de documentos já agregados (resultado da query SAP):

```json
{
  "documents": [
    {
      "direct":             "2",
      "chave":              "35260300347820000124550010000001231234567890",
      "modelo":             "55",
      "serie":              "001",
      "numero":             "000000123",
      "data_emissao":       "2026-03-25",
      "data_autorizacao":   "2026-03-25",
      "mes_ano":            "03/2026",
      "emit_cnpj":          "00347820000124",
      "dest_cnpj":          "07526557000100",
      "v_total":            1500.00,
      "v_bc_ibs_cbs":       1500.00,
      "v_ibs_uf":           12.00,
      "v_ibs_mun":          3.00,
      "v_ibs":              15.00,
      "v_cbs":              43.50
    }
  ]
}
```

**Resposta:**
```json
{
  "inserted":  150,
  "ignored":   12,
  "errors":    0,
  "error_details": []
}
```

**Arquivo Go:** `backend/handlers/erp_bridge_batch.go`

```go
// Lógica de roteamento por DIRECT + modelo:
// DIRECT=2 → nfe_saidas (emit=CNPJ_EMIT, v_nf=v_total)
// DIRECT=1 + modelo IN (55,62,65) → nfe_entradas (forn=CNPJ_EMIT, dest=CNPJ_DEST, v_nf=v_total)
// DIRECT=1 + modelo IN (57,66,67) → cte_entradas (emit=CNPJ_EMIT, dest=CNPJ_DEST, v_prest=v_total)
// INSERT ... ON CONFLICT (company_id, chave_nfe) DO NOTHING
```

**Registrar em `main.go`:**
```go
http.HandleFunc("/api/erp-bridge/import/batch", handlers.ERPBridgeBatchImportHandler)
// Sem withAuth — auth via X-API-Key dentro do handler (igual aos outros endpoints do bridge)
```

---

### 5.3 Remover Endpoints de Upload XML

Em `main.go`, remover:
```go
http.HandleFunc("/api/nfe-saidas/upload",  ...)
http.HandleFunc("/api/nfe-entradas/upload", ...)
http.HandleFunc("/api/cte-entradas/upload", ...)
http.HandleFunc("/api/danfe/",             ...)
```

Remover arquivos de handler:
- `backend/handlers/nfe_saidas_upload.go`
- `backend/handlers/nfe_entradas_upload.go`
- `backend/handlers/cte_entradas_upload.go`
- `backend/handlers/danfe.go`

Remover goroutine de retenção `dfe_xml` em `main.go` (linhas ~202-212).

---

### 5.4 Atualizar Handlers de Listagem

Arquivos afetados:
- `backend/handlers/nfe_saidas.go` — remover colunas ICMSTot do SELECT
- `backend/handlers/nfe_entradas.go` — remover colunas ICMSTot + corrigir filtro LIKE→=
- `backend/handlers/cte_entradas.go` — remover colunas prestação + corrigir filtro LIKE→=
- `backend/handlers/malha_fina.go` — verificar se usa colunas removidas
- `backend/handlers/apuracao.go` — verificar se usa colunas removidas

---

## 6. Plano de Implementação — ERP Bridge Python

### 6.1 Arquivo `bridge.py` — SAP mode

Adicionar lógica que detecta `erp_type == 'sap_s4hana'` e usa a query agregada:

```python
SAP_QUERY = """
SELECT
    nn.DIRECT,
    nn.NFEID                                            AS chave,
    SUBSTR(nn.NFEID, 21, 2)                             AS modelo,
    nn.SERIES                                           AS serie,
    nn.NFENUM                                           AS numero,
    TRUNC(nn.DOCDAT)                                    AS data_emissao,
    TRUNC(nn.CREDAT)                                    AS data_autorizacao,
    TO_CHAR(TRUNC(nn.DOCDAT), 'MM/YYYY')                AS mes_ano,
    nn.CNPJ_EMIT                                        AS emit_cnpj,
    nn.CNPJ_DEST                                        AS dest_cnpj,
    nn.NFTOT                                            AS v_total,
    MAX(CASE WHEN ni.TAXTYP = 'CBS3' THEN ni.BASE  ELSE 0 END) AS v_bc_ibs_cbs,
    SUM(CASE WHEN ni.TAXTYP = 'IB3S' THEN ni.TAXVAL ELSE 0 END) AS v_ibs_uf,
    SUM(CASE WHEN ni.TAXTYP = 'IB3M' THEN ni.TAXVAL ELSE 0 END) AS v_ibs_mun,
    SUM(CASE WHEN ni.TAXTYP IN ('IB3S','IB3M') THEN ni.TAXVAL ELSE 0 END) AS v_ibs,
    SUM(CASE WHEN ni.TAXTYP = 'CBS3' THEN ni.TAXVAL ELSE 0 END) AS v_cbs
FROM s4i_nfe nn
LEFT JOIN s4i_nfe_impostos ni
  ON ni.NFEID = nn.NFEID
 AND ni.TAXTYP IN ('CBS3','IB3M','IB3S')
WHERE TRUNC(nn.CREDAT) BETWEEN :data_ini AND :data_fim
  AND nn.CANCELADO = 'N'
GROUP BY
    nn.DIRECT, nn.NFEID, nn.SERIES, nn.NFENUM,
    nn.DOCDAT, nn.CREDAT, nn.CNPJ_EMIT, nn.CNPJ_DEST, nn.NFTOT
ORDER BY nn.CREDAT, nn.NFEID
"""
```

Enviar para `POST /api/erp-bridge/import/batch` com `X-API-Key`.

### 6.2 `config.yaml` — novo campo `erp_type`

```yaml
erp_type: sap_s4hana   # ou oracle_xml (legado)
oracle_dsn: FCCORP     # único servidor
```

---

## 7. Plano de Implementação — Frontend

### 7.1 Remover módulo "Importações" (XML upload)

Remover do AppRail:
```tsx
// Remover da mainItems:
{ id: 'importacoes', icon: FolderInput, label: 'Importações', path: '/apuracao/saida' }
```

Remover páginas:
- `frontend/src/pages/ImportarXMLsSaida.tsx`
- `frontend/src/pages/ImportarXMLsEntrada.tsx`
- `frontend/src/pages/ImportarXMLsCTe.tsx`

Remover rotas correspondentes em `App.tsx` (ou router).

### 7.2 Remover botão DANFE/DACTE

Nas páginas de consulta:
- `ConsultaNFeSaidas.tsx` — remover coluna DANFE e imports relacionados
- `ConsultaNFesEntradas.tsx` — remover coluna DANFE
- `ConsultaCTesEntradas.tsx` — remover coluna DACTE

### 7.3 Atualizar colunas nas tabelas de consulta

**`ConsultaNFeSaidas.tsx`** — remover: DANFE. Manter: Mod | Série/Nº | Data | Emitente | Destinatário | vNF | vIBS | vCBS

**`ConsultaNFesEntradas.tsx`** — remover: DANFE. Manter: Mod | Série/Nº | Data | Fornecedor | Filial (Dest.) | vNF | vIBS | vCBS

**`ConsultaCTesEntradas.tsx`** — remover: DACTE, Modal. Manter: Série/Nº | Data | Transportadora | Destinatário | vPrest | vIBS | vCBS

### 7.4 Configurações → ERP Bridge — adicionar selector `erp_type`

Na página `/config/erp-bridge`, adicionar campo:
```
Tipo de ERP: [Oracle XML (legado)] [SAP S4/HANA]
```
Salvo em `erp_bridge_config.erp_type`.

---

## 8. Arquivos a Modificar / Criar

| Arquivo | Ação |
|---|---|
| `backend/migrations/080_erp_bridge_erp_type.sql` | NOVO |
| `backend/migrations/081_simplify_nfe_saidas.sql` | NOVO |
| `backend/migrations/082_simplify_nfe_entradas.sql` | NOVO |
| `backend/migrations/083_simplify_cte_entradas.sql` | NOVO |
| `backend/migrations/084_drop_dfe_xml.sql` | NOVO |
| `backend/handlers/erp_bridge_batch.go` | NOVO — endpoint batch import |
| `backend/main.go` | +rota batch, -upload, -danfe, -dfe_xml retention |
| `backend/handlers/nfe_saidas.go` | Atualizar SELECT (remover colunas ICMSTot) |
| `backend/handlers/nfe_entradas.go` | Atualizar SELECT + LIKE→= |
| `backend/handlers/cte_entradas.go` | Atualizar SELECT + LIKE→= |
| `backend/handlers/nfe_saidas_upload.go` | REMOVER |
| `backend/handlers/nfe_entradas_upload.go` | REMOVER |
| `backend/handlers/cte_entradas_upload.go` | REMOVER |
| `backend/handlers/danfe.go` | REMOVER |
| `frontend/src/pages/ImportarXMLsSaida.tsx` | REMOVER |
| `frontend/src/pages/ImportarXMLsEntrada.tsx` | REMOVER |
| `frontend/src/pages/ImportarXMLsCTe.tsx` | REMOVER |
| `frontend/src/pages/ConsultaNFeSaidas.tsx` | Remover DANFE button |
| `frontend/src/pages/ConsultaNFesEntradas.tsx` | Remover DANFE button |
| `frontend/src/pages/ConsultaCTesEntradas.tsx` | Remover DACTE button |
| `frontend/src/components/AppRail.tsx` | Remover item Importações |
| `frontend/src/pages/ConfigERPBridge.tsx` | Adicionar selector erp_type |
| `erp-bridge/bridge.py` | Adicionar modo SAP S4/HANA |
| `erp-bridge/config.yaml` | Adicionar erp_type, dsn único FCCORP |

---

## 9. Critérios de Aceitação

1. **Bridge Python** conecta em FCCORP com a query agregada, envia para `/api/erp-bridge/import/batch`
2. **Documentos roteados corretamente**: DIRECT=2→nfe_saidas, DIRECT=1+55→nfe_entradas, DIRECT=1+57→cte_entradas
3. **ON CONFLICT DO NOTHING** garante idempotência (re-executar não duplica)
4. **IBS/CBS** populados com os valores do SAP (CBS3/IB3M/IB3S)
5. **Apuração IBS/CBS** continua funcionando com as tabelas simplificadas
6. **Malha Fina** continua funcionando (não usa colunas removidas)
7. **DANFE/DACTE** removidos sem quebrar nenhuma outra funcionalidade
8. **Importações** removido do AppRail
9. **Deploy** via `docker compose pull && docker compose up -d` no servidor AWS

---

## 10. Ordem de Execução Recomendada

```
Fase 1 — Backend Migrations + Endpoint Batch
  1. Criar migrations 080-084
  2. Criar erp_bridge_batch.go
  3. Atualizar main.go (rotas)
  4. Atualizar handlers de listagem (nfe_saidas, nfe_entradas, cte_entradas)
  5. Remover handlers de upload e danfe
  6. Commit + deploy backend

Fase 2 — ERP Bridge Python
  7. Atualizar bridge.py com modo SAP
  8. Atualizar config.yaml
  9. Testar query SAP em FCCORP
  10. Testar importação completa

Fase 3 — Frontend
  11. Remover páginas de importação XML
  12. Remover botões DANFE/DACTE
  13. Atualizar AppRail (remover Importações)
  14. Adicionar selector erp_type em Configurações
  15. Commit + deploy frontend
```
