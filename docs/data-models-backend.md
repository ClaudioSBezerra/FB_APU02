# Modelos de Dados — Backend (PostgreSQL)

**Banco:** PostgreSQL 15  
**ORM:** Nenhum — SQL puro via `database/sql` + `lib/pq`  
**Migrations:** 113 arquivos SQL em `backend/migrations/`, auto-executados na inicialização  
**Controle:** Tabela `schema_migrations (filename PK, executed_at)`

---

## Hierarquia Multi-tenant

```
environments (Ambiente)
    └── enterprise_groups (Grupo de Empresas)
            └── companies (Empresa)  ← company_id em todas as tabelas de dados
```

Todos os dados fiscais têm `company_id UUID FK → companies(id)` e são filtrados via `WHERE company_id = $1`.

---

## Tabelas Principais

### `environments` — Ambientes (Tenant Principal)

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `name` | VARCHAR(255) | Nome do ambiente |
| `description` | TEXT | Descrição opcional |
| `created_at` | TIMESTAMPTZ | Data de criação |

**Migration:** `013_create_environment_hierarchy.sql`

---

### `enterprise_groups` — Grupos de Empresas

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `environment_id` | UUID FK → environments | Ambiente pai |
| `name` | VARCHAR(255) | Nome do grupo |
| `created_at` | TIMESTAMPTZ | Data de criação |

---

### `companies` — Empresas (Unidade de Apuração)

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `group_id` | UUID FK → enterprise_groups | Grupo pai |
| `owner_id` | UUID FK → users | Usuário proprietário |
| `name` | VARCHAR(255) | Razão social |
| `trade_name` | VARCHAR(255) | Nome fantasia |
| `cnpj` | VARCHAR(14) | CNPJ completo |
| `created_at` | TIMESTAMPTZ | Data de criação |

---

### `users` — Usuários

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `email` | VARCHAR UNIQUE | E-mail (login) |
| `password_hash` | VARCHAR | bcrypt hash (custo 14) |
| `full_name` | VARCHAR | Nome completo |
| `is_verified` | BOOL | E-mail verificado |
| `role` | VARCHAR(50) | `admin` ou `user` |
| `trial_ends_at` | TIMESTAMPTZ | Validade do trial |
| `created_at` | TIMESTAMPTZ | Data de criação |

**Migrations:** `015_create_auth_system.sql`, `018_add_role_to_users.sql`

---

### `user_environments` — Relacionamento Usuário ↔ Ambiente

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `user_id` | UUID FK → users | Usuário |
| `environment_id` | UUID FK → environments | Ambiente |
| `role` | VARCHAR | Role no ambiente |
| `preferred_company_id` | UUID nullable | Empresa preferida |
| `created_at` | TIMESTAMPTZ | Data de vinculação |

**PK:** `(user_id, environment_id)`  
**Migrations:** `015_create_auth_system.sql`, `061b_user_environments_preferred_company.sql`

---

### `verification_tokens` — Tokens de Verificação

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `user_id` | UUID FK → users | Usuário |
| `token` | VARCHAR UNIQUE | Token hex gerado |
| `type` | VARCHAR | `email_verification` ou `password_reset` |
| `expires_at` | TIMESTAMPTZ | Expiração |
| `used` | BOOL | Já utilizado |

---

## Credenciais de APIs Externas

### `rfb_credentials` — Credenciais OAuth2 RFB por Empresa

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `company_id` | UUID UNIQUE FK | Empresa (1:1) |
| `cnpj_matriz` | VARCHAR(8) | CNPJ base (8 dígitos) |
| `client_id` | TEXT | OAuth2 client_id (criptografado) |
| `client_secret` | TEXT | OAuth2 client_secret (criptografado) |
| `ativo` | BOOL | Credencial ativa |
| `agendamento_ativo` | BOOL | Agendamento automático ativo |
| `horario_agendamento` | TIME | Horário BRT do agendamento diário |
| `ambiente` | VARCHAR | `producao`, `producao_restrita`, `homologacao` |
| `updated_at` | TIMESTAMPTZ | Última atualização |

**Migrations:** `051_create_rfb_credentials.sql`, `062_rfb_credentials_ambiente.sql`, `069_rfb_agendamento.sql`

---

### `cgibs_credentials` — Credenciais OAuth2 CGIBS por Empresa

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `company_id` | TEXT UNIQUE | Empresa (1:1) |
| `cnpj_matriz` | VARCHAR | CNPJ base |
| `client_id` | TEXT | OAuth2 client_id (criptografado) |
| `client_secret` | TEXT | OAuth2 client_secret (criptografado) |
| `ambiente` | VARCHAR(30) | `piloto` (padrão), `producao` |
| `ativo` | BOOL | Credencial ativa |
| `agendamento_ativo` | BOOL | Agendamento ativo |
| `horario_agendamento` | TIME | Horário BRT |

**Migration:** `102_cgibs_tables.sql`

---

## Solicitações de Apuração

### `rfb_requests` — Solicitações RFB CBS

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `company_id` | UUID FK → companies | Empresa |
| `cnpj_base` | VARCHAR(8) | CNPJ base (8 dígitos) |
| `tiquete` | VARCHAR | Ticket da solicitação RFB |
| `tiquete_download` | VARCHAR | Ticket de download do resultado |
| `status` | VARCHAR | Ver estados abaixo |
| `ambiente` | VARCHAR | `producao`, `producao_restrita`, etc. |
| `tipo` | VARCHAR | `debito` ou `credito` |
| `competencia` | VARCHAR | Período (ex: `06/2026`) |
| `error_code` | VARCHAR | Código de erro (ex: `RATE_LIMIT_429`) |
| `error_message` | TEXT | Mensagem detalhada ou `retry_until=RFC3339|...` |
| `raw_json` | JSONB | Resposta bruta da API RFB (limpo periodicamente) |
| `created_at` | TIMESTAMPTZ | Data de criação |
| `updated_at` | TIMESTAMPTZ | Última atualização |

**Estados do `status`:**

```
pending → requested → webhook_received → downloading → reprocessing → completed
                                                                    ↘ error
```

**Migrations:** `052_create_rfb_requests.sql`, `063_*`, `064_*`, `096_rfb_creditos.sql`

---

### `cgibs_requests` — Solicitações CGIBS IBS

Mesma estrutura de `rfb_requests`. **Migration:** `102_cgibs_tables.sql`

---

## Resultados de Apuração

### `rfb_debitos` — Débitos CBS por Documento

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `request_id` | UUID FK → rfb_requests | Solicitação de origem |
| `company_id` | UUID FK → companies | Empresa |
| `tipo_apuracao` | VARCHAR | Tipo de apuração |
| `modelo_dfe` | INT | Modelo do documento (55=NF-e, 57=CT-e, etc.) |
| `numero_dfe` | VARCHAR | Número do documento |
| `chave_dfe` | VARCHAR(50) | Chave de acesso (44 dígitos) |
| `data_dfe_emissao` | DATE | Data de emissão |
| `data_apuracao` | VARCHAR(6) | Período `YYYYMM` |
| `ni_emitente` | VARCHAR | CNPJ/CPF do emitente |
| `ni_adquirente` | VARCHAR | CNPJ/CPF do adquirente |
| `valor_cbs_total` | DECIMAL(18,2) | CBS total apurado |
| `valor_cbs_extinto` | DECIMAL(18,2) | CBS extinto (pago) |
| `valor_cbs_nao_extinto` | DECIMAL(18,2) | CBS a pagar |
| `situacao_debito` | VARCHAR | `EXTINTO`, `EXTINTO_PARCIAL`, `NAO_EXTINTO` |
| `formas_extincao` | JSONB | Detalhes das formas de extinção |
| `eventos` | JSONB | Eventos do documento |

**Deduplicação:** UNIQUE por `(company_id, chave_dfe, data_apuracao, tipo_apuracao)`.  
**Migrations:** `053_create_rfb_debitos.sql`, `092_rfb_debitos_dedup.sql`

---

### `rfb_resumo` — Resumo de Apuração CBS

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `request_id` | UUID FK → rfb_requests | Solicitação de origem |
| `company_id` | UUID FK → companies | Empresa |
| `data_apuracao` | VARCHAR | Período `YYYYMM` |
| `total_debitos` | INT | Total de documentos |
| `valor_cbs_total` | FLOAT | CBS total |
| `valor_cbs_extinto` | FLOAT | CBS extinto |
| `valor_cbs_nao_extinto` | FLOAT | CBS a pagar |
| `total_corrente` | INT | Documentos correntes |
| `total_ajuste` | INT | Documentos de ajuste |
| `total_extemporaneo` | INT | Documentos extemporâneos |

**Migration:** `054_create_rfb_resumo.sql`

---

### `rfb_creditos` — Créditos CBS

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `request_id` | UUID FK → rfb_requests | Solicitação de créditos |
| `company_id` | UUID FK → companies | Empresa |
| `chave_dfe` | VARCHAR | Chave do documento |
| `data_dfe_emissao` | DATE | Data de emissão |
| `ni_emitente` | VARCHAR | CNPJ emitente |
| `ni_adquirente` | VARCHAR | CNPJ adquirente |
| `valor_cbs_total` | NUMERIC(15,2) | CBS total |
| `valor_cbs_extinto` | NUMERIC(15,2) | CBS extinto |
| `valor_cbs_nao_extinto` | NUMERIC(15,2) | CBS restante |
| `situacao_credito` | VARCHAR | Situação do crédito |

**Migration:** `096_rfb_creditos.sql`

---

### `cgibs_debitos` / `cgibs_resumo` — Débitos IBS

Estrutura análoga a `rfb_debitos` / `rfb_resumo`. **Migration:** `102_cgibs_tables.sql`

---

## Documentos Fiscais

### `nfe_saidas` — NF-e Saídas

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `company_id` | UUID FK | Empresa |
| `chave_nfe` | VARCHAR(44) | Chave de acesso — UNIQUE por empresa |
| `modelo` | SMALLINT | 55=NF-e, 65=NFC-e |
| `serie` | VARCHAR | Série |
| `numero_nfe` | VARCHAR | Número |
| `data_emissao` | DATE | Data de emissão |
| `mes_ano` | VARCHAR(7) | `MM/YYYY` |
| `emit_cnpj` | VARCHAR(14) | CNPJ emitente |
| `emit_nome` | VARCHAR | Nome emitente |
| `dest_cnpj_cpf` | VARCHAR(14) | CNPJ/CPF destinatário |
| `dest_nome` | VARCHAR | Nome destinatário |
| `v_prod` | NUMERIC(15,2) | Valor dos produtos |
| `v_nf` | NUMERIC(15,2) | Valor total da NF |
| `v_icms` | NUMERIC(15,2) | ICMS |
| `v_pis` | NUMERIC(15,2) | PIS |
| `v_cofins` | NUMERIC(15,2) | COFINS |
| `v_bc_ibs_cbs` | NUMERIC(15,2) | Base de cálculo IBS/CBS |
| `v_ibs_uf` | NUMERIC(15,2) | IBS Estadual |
| `v_ibs_mun` | NUMERIC(15,2) | IBS Municipal |
| `v_ibs` | NUMERIC(15,2) | IBS total |
| `v_cbs` | NUMERIC(15,2) | CBS total |
| `cancelado` | BOOL | NF-e cancelada |
| `created_at` | TIMESTAMPTZ | Data de importação |

**UNIQUE:** `(company_id, chave_nfe)`  
**Migrations:** `058_create_nfe_saidas.sql` + `081`, `088`, `101`, `103`, `107-108`

---

### `nfe_entradas` — NF-e Entradas

Estrutura análoga a `nfe_saidas` com colunas de fornecedor no lugar de destinatário.  
Adicional: `cfop VARCHAR(4)`, `tipo_cfop VARCHAR(1)`.  
**UNIQUE:** `(company_id, chave_nfe)`  
**Migrations:** `059_create_nfe_entradas.sql` + subsequentes

---

### `cte_entradas` — CT-e Entradas

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `chave_cte` | VARCHAR(44) | Chave CT-e |
| `modal` | VARCHAR(2) | Modal de transporte |
| `cfop` | VARCHAR(4) | CFOP |
| `v_tprest` | NUMERIC(15,2) | Valor total da prestação |
| + demais colunas fiscais análogas | | |

**UNIQUE:** `(company_id, chave_cte)`  
**Migrations:** `060_create_cte_entradas.sql` + subsequentes

---

## ERP Bridge

### `erp_bridge_config` — Configuração por Empresa

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `company_id` | UUID PK FK → companies | Empresa (1:1) |
| `ativo` | BOOL | Daemon ativo |
| `horario` | TIME | Horário de execução |
| `dias_retroativos` | INT | Dias retroativos no oracle_xml |
| `ultimo_run_em` | TIMESTAMPTZ | Último run executado |
| `api_key_hash` | VARCHAR | SHA256 hex da X-API-Key |
| `reset_tracker` | BOOL | Flag para limpar tracker SQLite |

**Migration:** `074_erp_bridge.sql`

---

### `erp_bridge_runs` — Histórico de Execuções

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `company_id` | UUID FK | Empresa |
| `iniciado_em` | TIMESTAMPTZ | Início da execução |
| `finalizado_em` | TIMESTAMPTZ | Fim da execução |
| `status` | VARCHAR | `running`, `success`, `partial`, `error` |
| `data_ini` | DATE | Data inicial do período |
| `data_fim` | DATE | Data final do período |
| `total_enviados` | INT | Total de documentos enviados |
| `total_ignorados` | INT | Total ignorados (duplicados) |
| `total_erros` | INT | Total com erro |
| `origem` | VARCHAR | `manual` ou `scheduler` |

---

### `erp_bridge_run_items` — Itens por Servidor em cada Run

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `run_id` | UUID FK → erp_bridge_runs | Run de origem |
| `servidor` | TEXT | Nome do servidor/filial |
| `tipo` | TEXT | `nfe_saidas`, `nfe_entradas`, `cte_entradas` |
| `enviados` | INT | Documentos enviados |
| `ignorados` | INT | Documentos duplicados ignorados |
| `erros` | INT | Documentos com erro |
| `status` | VARCHAR | `ok`, `erro_conexao`, `erro_query`, `erro_parcial` |
| `erro_msg` | TEXT | Mensagem de erro detalhada |

---

## Configurações de Referência

### `tabela_aliquotas` — Alíquotas IBS/CBS por Ano

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `ano` | INT | Ano fiscal |
| `perc_ibs_uf` | FLOAT | Alíquota IBS Estadual (%) |
| `perc_ibs_mun` | FLOAT | Alíquota IBS Municipal (%) |
| `perc_cbs` | FLOAT | Alíquota CBS (%) |
| `perc_reduc_icms` | FLOAT | Redução ICMS pela Reforma (%) |
| `perc_reduc_piscofins` | FLOAT | Redução PIS/COFINS pela Reforma (%) |

---

### `user_activity_logs` — Log de Atividade por Módulo

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `id` | UUID PK | Identificador único |
| `user_id` | UUID FK → users | Usuário |
| `company_id` | UUID FK → companies | Empresa ativa no momento |
| `module` | VARCHAR(100) | Módulo acessado |
| `visited_at` | TIMESTAMPTZ | Timestamp do acesso |
| `duration_seconds` | INT | Tempo de permanência |

**Migration:** `095_user_activity_logs.sql`

---

## Materialized Views

### `mv_mercadorias` — Mercadorias SPED EFD

Agregação de mercadorias do SPED EFD com tributos (ICMS, PIS/COFINS, IBS/CBS) para análise.  
**Refresh:** Manual via `POST /api/admin/refresh-views` ou automático após importações.  
**Migrations:** `021_create_mv_mercadorias.sql` + subsequentes

---

## Convenções de Schema

1. **UUIDs** como PKs em todas as tabelas principais (extensão `pgcrypto` via `gen_random_uuid()`)
2. **`created_at`** e **`updated_at`** em tabelas mutáveis
3. **`company_id`** como FK em toda tabela de dados fiscal
4. **UNIQUE** em chaves fiscais para garantir idempotência nas importações
5. **Gaps na numeração** são intencionais (migrations removidas/desabilitadas)
6. **Sufixo `b`** para correções: `021b_ensure_admin_user.sql`
7. **Arquivo `.disabled`** para migrations desativadas (não executadas)
