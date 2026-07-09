# Arquitetura — ERP Bridge Python (Daemon)

**Parte:** `erp-bridge-aws/`  
**Tipo:** Daemon Python — extração Oracle ERP → API FBTax  
**Deploy:** Servidor AWS interno do cliente (systemd service)

---

## Sumário Executivo

O ERP Bridge é um daemon Python que roda no servidor AWS com acesso à rede interna Oracle (ERP da Ferreira Costa). Ele extrai documentos fiscais (NF-e, CT-e) do Oracle e os envia para a API FBTax.

Suporta dois modos de operação:
- **`oracle_xml`**: modo legado Totvs/Protheus — múltiplos servidores Oracle por filial, extrai XMLs individualmente
- **`sap_s4hana`**: modo SAP S/4HANA via Oracle FCCORP — query pivô única, envia batches JSON

---

## Stack

| Componente | Tecnologia |
|-----------|-----------|
| Linguagem | Python 3.9+ |
| Oracle client | `oracledb` (thin mode, sem Oracle Client) |
| HTTP client | `requests` (Session reutilizável) |
| Config | `pyyaml` (`yaml.safe_load`) |
| Tracker local | `sqlite3` (stdlib) |
| Timezone | `zoneinfo` (stdlib) — `America/Sao_Paulo` |
| Serviço | systemd unit (`erp-bridge.service`) |

---

## Modos de Operação

### Modo `oracle_xml` (Totvs/Protheus)

```
Múltiplos servidores Oracle (1 por filial)
    │ oracledb thin mode
    ▼
Query SQL → tabelas sfc_nfe, sfc_nfe_imp, SFC_CTE_IMP
    │
    ▼ XML CLOB
Normalização XML (encoding UTF-8, declaração XML)
    │
    ▼ multipart/form-data (1 arquivo por request)
POST /api/nfe-saidas/upload
POST /api/nfe-entradas/upload
POST /api/cte-entradas/upload
    │
    ▼ SQLite tracker.db
Registra status por (servidor, tipo, chave)
```

**Tabelas Oracle consultadas:**

| Tabela | Tipo | Filtros |
|--------|------|---------|
| `sfc_nfe` | NF-e Saídas | `cstat='100'`, `ultima_operacao='S'`, `data_emissao BETWEEN :ini :fim` |
| `sfc_nfe_imp` | NF-e Entradas | `data_importacao BETWEEN :ini :fim` |
| `SFC_CTE_IMP` | CT-e Entradas | `DATA_IMPORTACAO BETWEEN :ini :fim` |

### Modo `sap_s4hana` (SAP S/4HANA via FCCORP)

```
Servidor Oracle FCCORP único
    │ oracledb thin mode
    ▼
SAP_QUERY → s4i_nfe + s4i_nfe_impostos + s4i_nfe_it
(query pivô: agrega 60+ TAXTYP em CBS, IBS_UF, IBS_MUN, ICMS, PIS, COFINS, IPI por documento)
    │
    ▼ Lista de documentos + parceiros
Sync parceiros → POST /api/erp-bridge/parceiros/sync (lotes 500)
Envio documentos → POST /api/erp-bridge/import/batch (lotes 1000)
    │
    ▼ SQLite sap_watermark
Atualiza watermark (data da última importação bem-sucedida)
```

**Campos da SAP_QUERY por documento:**

| Campo | Fonte | Descrição |
|-------|-------|-----------|
| `chave` | `s4i_nfe.NFEID` (44 dígitos) | Chave de acesso |
| `direct` | `s4i_nfe.DIRECT` | 1=entrada, 2=saída |
| `modelo` | `SUBSTR(NFEID, 21, 2)` | 55=NF-e, 65=NFC-e, 57=CT-e |
| `v_cbs` | `TAXTYP IN (CBS1,CBS2,CBS3)` | Valor CBS |
| `v_ibs_uf` | `TAXTYP IN (IB1S,IB2S,IB3S)` | IBS UF |
| `v_ibs_mun` | `TAXTYP IN (IB1M,IB2M,IB3M)` | IBS Município |
| `icms` | 17 TAXTYP | ICMS |
| `pis` / `cofins` | 18 / 20 TAXTYP | PIS / COFINS |
| `cfop` | Subquery frequência em `s4i_nfe_it` | CFOP dominante |

---

## Loop Principal do Daemon

```python
def run_daemon(cfg, fbtax):
    while True:
        # 0. Heartbeat → POST /api/erp-bridge/heartbeat
        fbtax.heartbeat()
        
        # 1. Verificar reset_tracker
        if cfg_api.get('reset_tracker'):
            sqlite_clear(conn)
            fbtax.reset_tracker_ack()
        
        # 2. Processar runs manuais enfileirados pela UI
        pending = fbtax.get_pending_runs()
        for run in pending:
            process_run(run)
        
        # 3. Agendamento automático (horário configurado na UI)
        if agora == horario and nao_rodou_hoje:
            process_scheduled()
        
        time.sleep(60)  # polling a cada 60 segundos
```

---

## Autenticação Dupla

| Tipo | Header | Usado em |
|------|--------|---------|
| JWT Bearer | `Authorization: Bearer {token}` | `GET /api/erp-bridge/config`, `GET /api/erp-bridge/pending`, `POST/PATCH /api/erp-bridge/runs/*` |
| API Key | `X-API-Key: {key}` | `POST /api/erp-bridge/import/batch`, `POST /api/erp-bridge/parceiros/sync`, `POST /api/erp-bridge/heartbeat`, `GET /api/erp-bridge/credentials` |

O JWT é renovado automaticamente quando recebe 401 (chama `login()` novamente).

---

## Endpoints da API Chamados

| Método | Path | Auth | Propósito |
|--------|------|------|-----------|
| `POST` | `/api/auth/login` | — | Obter JWT Bearer |
| `GET` | `/api/erp-bridge/credentials` | X-API-Key | Credenciais remotas (sobrescrevem config.yaml) |
| `POST` | `/api/erp-bridge/heartbeat` | X-API-Key | Sinalizar daemon ativo |
| `GET` | `/api/erp-bridge/config` | Bearer | Verificar flag reset_tracker + horário/ativo |
| `PATCH` | `/api/erp-bridge/config` | Bearer | Confirmar reset do tracker |
| `GET` | `/api/erp-bridge/pending` | Bearer | Buscar runs enfileirados pela UI |
| `POST` | `/api/erp-bridge/runs` | Bearer | Criar novo run |
| `PATCH` | `/api/erp-bridge/runs/{id}` | Bearer | Atualizar status/progress do run |
| `GET` | `/api/erp-bridge/runs/{id}` | Bearer | Verificar cancelamento |
| `POST` | `/api/erp-bridge/runs/{id}/items` | Bearer | Reportar itens por servidor |
| `POST` | `/api/erp-bridge/servidores/registrar` | Bearer | Registrar servidores (oracle_xml) |
| `POST` | `/api/nfe-saidas/upload` | Bearer | Upload XML NF-e saída (oracle_xml) |
| `POST` | `/api/nfe-entradas/upload` | Bearer | Upload XML NF-e entrada (oracle_xml) |
| `POST` | `/api/cte-entradas/upload` | Bearer | Upload XML CT-e entrada (oracle_xml) |
| `POST` | `/api/erp-bridge/import/batch` | X-API-Key | Batch documentos JSON (sap_s4hana) |
| `POST` | `/api/erp-bridge/parceiros/sync` | X-API-Key | Sync fornecedores/clientes |

---

## Tracker SQLite (`tracker.db`)

### Tabela `enviados` (oracle_xml)

```sql
CREATE TABLE enviados (
    servidor TEXT,
    tipo TEXT,       -- 'nfe_saidas' | 'nfe_entradas' | 'cte_entradas'
    chave TEXT,      -- chave de acesso (44 dígitos)
    enviado_em TEXT,
    status TEXT,     -- 'ok' | 'erro_xml' | 'erro_{http_code}'
    PRIMARY KEY (servidor, tipo, chave)
)
```

### Tabela `sap_watermark` (sap_s4hana)

```sql
CREATE TABLE sap_watermark (
    dsn TEXT PRIMARY KEY,
    last_date TEXT    -- 'YYYY-MM-DD' — data da última importação bem-sucedida
)
```

---

## Configuração (`config.yaml`)

```yaml
# config.yaml (não versionado — contém credenciais)
erp_type: sap_s4hana   # ou oracle_xml
dias_padrao: 7         # dias retroativos no modo CLI

fbtax:
  url: https://fctax.fcxlabs.com
  email: user@domain.com
  password: "..."
  api_key: "..."         # X-API-Key para endpoints erp-bridge/*
  company_id: "uuid"    # ID da empresa no FBTax

# Para modo sap_s4hana:
oracle:
  usuario: FCCORP_USER
  senha: "..."
  dsn: "host:1521/service"

# Para modo oracle_xml:
servidores:
  - nome: "FC - Recife"
    dsn: "host:1521/service"
    usuario: "..."
    senha: "..."
    tipos: [nfe_saidas, nfe_entradas, cte_entradas]
```

---

## Argumentos CLI

```bash
python bridge.py --daemon            # Loop infinito (systemd)
python bridge.py --data 2026-06-01   # Importação manual de uma data
python bridge.py --mes 2026-06       # Mês completo
python bridge.py --data-fim 2026-06-30  # Com data fim
python bridge.py --servidor "FC - Recife"  # Filtrar por servidor (oracle_xml)
python bridge.py --dry-run           # Consulta Oracle sem enviar à API
python bridge.py --only-parceiros    # Apenas sync de parceiros (sap_s4hana)
```

---

## Operação como Serviço (systemd)

```ini
# erp-bridge.service
[Unit]
Description=FBTax ERP Bridge — Oracle ERP → FBTax Apuração Assistida
After=network-online.target

[Service]
Type=simple
User=claudio
WorkingDirectory=/opt/apps/fbtax/erp-bridge
ExecStart=/opt/apps/fbtax/erp-bridge/venv/bin/python bridge.py --daemon
Restart=on-failure
RestartSec=30
StandardOutput=journal
StandardError=journal
```

```bash
# Gerenciamento
systemctl start erp-bridge
systemctl status erp-bridge
journalctl -u erp-bridge -f

# Logs em arquivo
ls /opt/apps/fbtax/erp-bridge/logs/bridge_*.log
```

---

## Tratamento de Erros e Resiliência

| Situação | Comportamento |
|----------|--------------|
| Oracle connection timeout | Timeout keepalive 2 min (TCP expire_time), reconecta na próxima iteração |
| HTTP 409 (duplicado) | Marcado como `ok` no tracker — não é erro |
| HTTP 401 (JWT expirado) | Renova JWT via `login()` automaticamente |
| Run cancelado pela UI | Verifica `GET /api/erp-bridge/runs/{id}` e interrompe processamento |
| Erro total de batch | Run finalizado com `status='error'`; watermark não atualizado (reprocessa na próxima) |

---

## Limitações e Observações

| Item | Detalhe |
|------|---------|
| `config.yaml` não versionado | Contém credenciais Oracle + FBTax — armazenado apenas no servidor |
| Credenciais remotas | Buscadas via `GET /api/erp-bridge/credentials` — sobrescrevem config.yaml em memória |
| Sem múltiplos daemons | Um daemon por instalação — não projetado para execução paralela |
| Duração de importação | Sem limite de tempo — runs longos não são automaticamente cancelados pelo daemon |
| Logs | Gerado novo arquivo de log por execução em `logs/bridge_YYYYMMDD_HHMMSS.log` |
