#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
ERP Bridge (Linux/AWS) — Oracle ERP → FBTax Apuração Assistida
Suporta dois modos de importação:
  - oracle_xml   : Oracle por filial, envia XML multipart (legado Totvs/Protheus)
  - sap_s4hana   : Oracle FCCORP único, envia JSON batch via s4i_nfe + s4i_nfe_impostos

Uso:
  python bridge.py                               # últimos N dias (padrão config)
  python bridge.py --data 2026-01-01             # desde data específica
  python bridge.py --data 2026-03-01 --data-fim 2026-03-19
  python bridge.py --mes 2026-03                 # mês inteiro
  python bridge.py --servidor "FC - Recife"      # só um servidor (oracle_xml)
  python bridge.py --daemon                      # modo daemon
"""

import argparse
import io
import json as _json
import logging
import re
import sqlite3
import sys
import time as _time
from datetime import date, datetime, timedelta
from pathlib import Path
from zoneinfo import ZoneInfo

import requests
import yaml

try:
    import oracledb
except ImportError:
    print("ERRO: python-oracledb nao instalado.")
    print("Execute: pip install oracledb requests pyyaml")
    sys.exit(1)

# ─── Caminhos ────────────────────────────────────────────────────────────────

BASE_DIR   = Path(__file__).parent
CONFIG_F   = BASE_DIR / "config.yaml"
TRACKER_DB = BASE_DIR / "tracker.db"
LOG_DIR    = BASE_DIR / "logs"
LOG_DIR.mkdir(exist_ok=True)

# ─── Logging ─────────────────────────────────────────────────────────────────

log_file = LOG_DIR / f"bridge_{datetime.now().strftime('%Y%m%d_%H%M%S')}.log"

_file_handler = logging.FileHandler(log_file, encoding="utf-8")
_file_handler.setLevel(logging.DEBUG)
_file_handler.setFormatter(logging.Formatter("%(asctime)s [%(levelname)s] %(message)s"))

_screen_handler = logging.StreamHandler(sys.stdout)
_screen_handler.setLevel(logging.INFO)
_screen_handler.setFormatter(logging.Formatter("%(asctime)s [%(levelname)s] %(message)s"))

logging.root.setLevel(logging.DEBUG)
logging.root.addHandler(_file_handler)
logging.root.addHandler(_screen_handler)

log = logging.getLogger(__name__)

# ─── Tracker SQLite ───────────────────────────────────────────────────────────

def init_tracker() -> sqlite3.Connection:
    conn = sqlite3.connect(TRACKER_DB)
    conn.execute("""
        CREATE TABLE IF NOT EXISTS enviados (
            servidor   TEXT NOT NULL,
            tipo       TEXT NOT NULL,
            chave      TEXT NOT NULL,
            enviado_em TEXT NOT NULL,
            status     TEXT NOT NULL,
            PRIMARY KEY (servidor, tipo, chave)
        )
    """)
    conn.commit()
    return conn

def ja_enviado(conn, servidor, tipo, chave) -> bool:
    row = conn.execute(
        "SELECT 1 FROM enviados WHERE servidor=? AND tipo=? AND chave=? AND status='ok'",
        (servidor, tipo, str(chave))
    ).fetchone()
    return row is not None

def marcar(conn, servidor, tipo, chave, status):
    conn.execute("""
        INSERT INTO enviados (servidor, tipo, chave, enviado_em, status)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(servidor, tipo, chave)
        DO UPDATE SET enviado_em=excluded.enviado_em, status=excluded.status
    """, (servidor, tipo, str(chave), datetime.now().isoformat(), status))
    conn.commit()

# ── Watermark incremental SAP ─────────────────────────────────────────────────

def get_watermark(dsn: str):
    """Retorna a última data importada com sucesso para o DSN, ou None."""
    conn = sqlite3.connect(TRACKER_DB)
    conn.execute("""
        CREATE TABLE IF NOT EXISTS sap_watermark (
            dsn       TEXT PRIMARY KEY,
            last_date TEXT NOT NULL
        )
    """)
    conn.commit()
    row = conn.execute(
        "SELECT last_date FROM sap_watermark WHERE dsn = ?", (dsn,)
    ).fetchone()
    conn.close()
    if row:
        try:
            return date.fromisoformat(row[0])
        except ValueError:
            return None
    return None

def set_watermark(dsn: str, last_date):
    """Grava o watermark (última data importada) para o DSN."""
    conn = sqlite3.connect(TRACKER_DB)
    conn.execute("""
        CREATE TABLE IF NOT EXISTS sap_watermark (
            dsn       TEXT PRIMARY KEY,
            last_date TEXT NOT NULL
        )
    """)
    conn.execute("""
        INSERT INTO sap_watermark (dsn, last_date) VALUES (?, ?)
        ON CONFLICT(dsn) DO UPDATE SET last_date = excluded.last_date
    """, (dsn, last_date.isoformat()))
    conn.commit()
    conn.close()

# ─── Normalização de XML (modo oracle_xml) ────────────────────────────────────

_DECL_RE     = re.compile(r'<\?xml[^?]*\?>', re.IGNORECASE)
_ENCODING_RE = re.compile(r'encoding\s*=\s*["\'][^"\']*["\']', re.IGNORECASE)

def normalizar_xml(texto: str, adicionar_decl: bool = False) -> bytes:
    texto = texto.strip()
    match = _DECL_RE.match(texto)
    if match:
        nova_decl = _ENCODING_RE.sub('encoding="UTF-8"', match.group())
        texto = nova_decl + texto[match.end():]
    elif adicionar_decl:
        texto = '<?xml version="1.0" encoding="UTF-8"?>' + texto
    return texto.encode("utf-8")

def clob_para_str(valor) -> str:
    if valor is None:
        return ""
    if hasattr(valor, "read"):
        return valor.read()
    return str(valor)

# ─── Fontes de dados (modo oracle_xml legado) ─────────────────────────────────

FONTES = {
    "nfe_saidas": {
        "sql": """
            SELECT nfe,
                   nota_xml
            FROM   sfc_nfe
            WHERE  TRUNC(data_emissao) >= :data_ini
              AND  TRUNC(data_emissao) <  :data_fim
              AND  cstat = '100'
              AND  ultima_operacao = 'S'
            ORDER BY nfe
        """,
        "chave_col":     0,
        "xml_col":       1,
        "adicionar_decl": False,
        "endpoint":      "/api/nfe-saidas/upload",
        "descricao":     "NF-e Saidas (mod 55/65)",
    },
    "nfe_entradas": {
        "sql": """
            SELECT chave_nfe,
                   email_xml_nfe
            FROM   sfc_nfe_imp
            WHERE  TRUNC(data_importacao) >= :data_ini
              AND  TRUNC(data_importacao) <  :data_fim
            ORDER BY chave_nfe
        """,
        "chave_col":     0,
        "xml_col":       1,
        "adicionar_decl": True,
        "endpoint":      "/api/nfe-entradas/upload",
        "descricao":     "NF-e Entradas",
    },
    "cte_entradas": {
        "sql": """
            SELECT CHAVE_CTE,
                   XML_CTE
            FROM   SFC_CTE_IMP
            WHERE  TRUNC(DATA_IMPORTACAO) >= :data_ini
              AND  TRUNC(DATA_IMPORTACAO) <  :data_fim
            ORDER BY CHAVE_CTE
        """,
        "chave_col":     0,
        "xml_col":       1,
        "adicionar_decl": True,
        "endpoint":      "/api/cte-entradas/upload",
        "descricao":     "CT-e Entradas",
    },
}

# ─── Query SAP S4/HANA ────────────────────────────────────────────────────────
# Retorna 1 linha por documento com pivot dos impostos CBS/IBS.
# CBS1/CBS2/CBS3 = CBS | IB1M/IB2M/IB3M = IBS Municipal | IB1S/IB2S/IB3S = IBS Estadual (UF)
# DIRECT=1 → entrada | DIRECT=2 → saída
# modelo derivado da posição 21-22 da chave de 44 dígitos (1-indexed Oracle)

SAP_QUERY = """
SELECT
    nn.DIRECT,
    nn.NFEID                                                                         AS chave,
    SUBSTR(nn.NFEID, 21, 2)                                                          AS modelo,
    nn.SERIES                                                                        AS serie,
    nn.NFENUM                                                                        AS numero,
    TO_CHAR(TRUNC(nn.DOCDAT), 'YYYY-MM-DD')                                         AS data_emissao,
    TO_CHAR(TRUNC(nn.CREDAT), 'YYYY-MM-DD')                                         AS data_autorizacao,
    TO_CHAR(TRUNC(nn.DOCDAT), 'MM/YYYY')                                             AS mes_ano,
    nn.CNPJ_EMIT                                                                     AS emit_cnpj,
    nn.CNPJ_DEST                                                                     AS dest_cnpj,
    nn.CANCELADO                                                                     AS cancelado,
    nn.NFTOT                                                                         AS v_total,
    MAX(CASE WHEN ni.TAXTYP IN ('CBS1','CBS2','CBS3') THEN ni.BASE  ELSE 0 END)     AS v_bc_ibs_cbs,
    SUM(CASE WHEN ni.TAXTYP IN ('IB1S','IB2S','IB3S') THEN ni.TAXVAL ELSE 0 END)   AS v_ibs_uf,
    SUM(CASE WHEN ni.TAXTYP IN ('IB1M','IB2M','IB3M') THEN ni.TAXVAL ELSE 0 END)   AS v_ibs_mun,
    SUM(CASE WHEN ni.TAXTYP IN ('IB1S','IB2S','IB3S','IB1M','IB2M','IB3M') THEN ni.TAXVAL ELSE 0 END) AS v_ibs,
    SUM(CASE WHEN ni.TAXTYP IN ('CBS1','CBS2','CBS3') THEN ni.TAXVAL ELSE 0 END)    AS v_cbs
FROM s4i_nfe nn
LEFT JOIN s4i_nfe_impostos ni
  ON ni.NFEID = nn.NFEID
 AND ni.TAXTYP IN ('CBS1','CBS2','CBS3','IB1M','IB2M','IB3M','IB1S','IB2S','IB3S')
 AND ni.TAXVAL > 0
LEFT JOIN s4i_nfe_it it
  ON it.NFEID = nn.NFEID
 AND it.ITMNUM = ni.ITMNUM
WHERE TRUNC(nn.CREDAT) BETWEEN :data_ini AND :data_fim
  AND LENGTH(nn.NFEID) = 44
  AND (it.cfop IS NULL OR SUBSTR(it.cfop, 1, 4) NOT IN (
    '1151','1152','1153','1154',
    '1408','1409','1658','1659',
    '2151','2152','2153','2154',
    '2408','2409','2658','2659',
    '5151','5152','5153','5154','5155','5156',
    '5408','5409','5658','5659',
    '6151','6152','6153','6154','6155','6156',
    '6408','6409','6658','6659'
  ))
GROUP BY
    nn.DIRECT, nn.NFEID, nn.SERIES, nn.NFENUM,
    nn.DOCDAT, nn.CREDAT, nn.CNPJ_EMIT, nn.CNPJ_DEST, nn.CANCELADO, nn.NFTOT
ORDER BY nn.CREDAT, nn.NFEID
"""

# ─── Query parceiros SAP (FORN + CLIE do período) ────────────────────────────
# Busca fornecedores e clientes cujos CNPJs aparecem nos movimentos do período.
# Usa subquery para evitar ORA-01795 (sem listas Python de CNPJs).

PARCEIROS_QUERY = """
SELECT CGC AS cnpj, MIN(RAZSOC) AS nome
FROM FORN
WHERE LENGTH(CGC) = 14
  AND CGC IN (
    SELECT DISTINCT CNPJ_EMIT FROM s4i_nfe
    WHERE TRUNC(CREDAT) BETWEEN :data_ini AND :data_fim
      AND DIRECT = '1' AND LENGTH(NFEID) = 44
)
GROUP BY CGC
UNION
SELECT CGCCPF AS cnpj, MIN(RAZSOC) AS nome
FROM CLIE
WHERE LENGTH(CGCCPF) = 14
  AND CGCCPF IN (
    SELECT DISTINCT CNPJ_DEST FROM s4i_nfe
    WHERE TRUNC(CREDAT) BETWEEN :data_ini AND :data_fim
      AND DIRECT = '2' AND LENGTH(NFEID) = 44
)
GROUP BY CGCCPF
"""

# ─── Cliente FBTax ────────────────────────────────────────────────────────────

class FBTaxClient:
    def __init__(self, cfg: dict):
        self.base_url   = cfg["url"].rstrip("/")
        self.email      = cfg.get("email", "")
        self.password   = cfg.get("password", "")
        self.api_key    = cfg.get("api_key", "")
        self.company_id = cfg.get("company_id", "")
        self.token      = None
        self.session    = requests.Session()
        self.session.headers.update({"X-Company-ID": self.company_id})

    def login(self):
        resp = self.session.post(
            f"{self.base_url}/api/auth/login",
            json={"email": self.email, "password": self.password},
            timeout=30,
        )
        resp.raise_for_status()
        self.token = resp.json()["token"]
        self.session.headers["Authorization"] = f"Bearer {self.token}"
        log.info("Autenticado no FBTax como %s", self.email)

    def _post_xml(self, endpoint: str, chave: str, xml_bytes: bytes) -> requests.Response:
        url   = f"{self.base_url}{endpoint}"
        files = [("xmls", (f"{chave}.xml", io.BytesIO(xml_bytes), "application/xml"))]
        return self.session.post(url, files=files, timeout=60)

    def enviar(self, endpoint: str, chave: str, xml_bytes: bytes) -> dict:
        resp = self._post_xml(endpoint, chave, xml_bytes)
        if resp.status_code == 401:
            log.warning("Token expirado — renovando sessao...")
            self.login()
            resp = self._post_xml(endpoint, chave, xml_bytes)
        return {"status": resp.status_code, "body": resp.text[:300]}

    def enviar_batch(self, documents: list) -> dict:
        """Envia batch de documentos SAP para /api/erp-bridge/import/batch (auth X-API-Key)."""
        url = f"{self.base_url}/api/erp-bridge/import/batch"
        resp = requests.post(
            url,
            headers={"X-API-Key": self.api_key, "Content-Type": "application/json"},
            json={"documents": documents},
            timeout=120,
        )
        if not resp.ok:
            log.error("enviar_batch HTTP %d: %s", resp.status_code, resp.text[:300])
            resp.raise_for_status()
        return resp.json()

    def sync_parceiros(self, parceiros: list, chunk_size: int = 500) -> dict:
        """Envia lista de {cnpj, nome} em lotes para /api/erp-bridge/parceiros/sync."""
        url = f"{self.base_url}/api/erp-bridge/parceiros/sync"
        total_upserted = 0
        for i in range(0, len(parceiros), chunk_size):
            lote = parceiros[i:i + chunk_size]
            resp = requests.post(
                url,
                headers={"X-API-Key": self.api_key, "Content-Type": "application/json"},
                json={"parceiros": lote},
                timeout=120,
            )
            if not resp.ok:
                log.warning("sync_parceiros HTTP %d: %s", resp.status_code, resp.text[:200])
                continue
            total_upserted += resp.json().get("upserted", 0)
        return {"upserted": total_upserted}

    # ── Métodos de reporte de execução via API ─────────────────────────────────

    def get_bridge_config(self) -> dict | None:
        try:
            resp = self.session.get(f"{self.base_url}/api/erp-bridge/config", timeout=10)
            if resp.status_code == 401:
                self.login()
                resp = self.session.get(f"{self.base_url}/api/erp-bridge/config", timeout=10)
            if resp.ok:
                return resp.json()
        except Exception as exc:
            log.warning("Nao foi possivel obter config bridge: %s", exc)
        return None

    def fetch_credentials(self) -> dict | None:
        """Busca credenciais criptografadas do servidor via api_key."""
        if not self.api_key:
            return None
        try:
            resp = requests.get(
                f"{self.base_url}/api/erp-bridge/credentials",
                headers={"X-API-Key": self.api_key},
                timeout=10,
            )
            if resp.status_code == 200:
                return resp.json()
            log.warning("fetch_credentials HTTP %d", resp.status_code)
        except Exception as exc:
            log.warning("Nao foi possivel buscar credenciais: %s", exc)
        return None

    def registrar_servidores(self, nomes: list) -> None:
        try:
            self.session.post(
                f"{self.base_url}/api/erp-bridge/servidores/registrar",
                json={"nomes": nomes},
                timeout=10,
            )
        except Exception as exc:
            log.warning("Nao foi possivel registrar servidores: %s", exc)

    def reset_tracker_ack(self) -> bool:
        try:
            resp = self.session.patch(
                f"{self.base_url}/api/erp-bridge/config",
                json={"reset_tracker": False},
                timeout=10,
            )
            if resp.status_code == 401:
                self.login()
                resp = self.session.patch(
                    f"{self.base_url}/api/erp-bridge/config",
                    json={"reset_tracker": False},
                    timeout=10,
                )
            return resp.status_code in (200, 204)
        except Exception as exc:
            log.warning("Nao foi possivel confirmar reset_tracker_ack: %s", exc)
        return False

    def create_run(self, data_ini: date, data_fim: date, origem: str = "scheduler") -> str | None:
        try:
            resp = self.session.post(
                f"{self.base_url}/api/erp-bridge/runs",
                json={
                    "data_ini": str(data_ini),
                    "data_fim": str(data_fim - timedelta(days=1)),
                    "origem": origem,
                },
                timeout=10,
            )
            if resp.status_code == 401:
                self.login()
                resp = self.session.post(
                    f"{self.base_url}/api/erp-bridge/runs",
                    json={
                        "data_ini": str(data_ini),
                        "data_fim": str(data_fim - timedelta(days=1)),
                        "origem": origem,
                    },
                    timeout=10,
                )
            if resp.status_code in (200, 201):
                return resp.json().get("id")
        except Exception as exc:
            log.warning("Nao foi possivel criar run na API: %s", exc)
        return None

    def heartbeat(self) -> bool:
        """Envia sinal de vida ao backend e limpa runs presos."""
        if not self.api_key:
            return False
        try:
            resp = self.session.post(
                f"{self.base_url}/api/erp-bridge/heartbeat",
                headers={"X-API-Key": self.api_key},
                timeout=10,
            )
            return resp.ok
        except Exception as exc:
            log.warning("Heartbeat falhou: %s", exc)
        return False

    def get_pending_runs(self) -> list:
        try:
            resp = self.session.get(f"{self.base_url}/api/erp-bridge/pending", timeout=10)
            if resp.status_code == 401:
                self.login()
                resp = self.session.get(f"{self.base_url}/api/erp-bridge/pending", timeout=10)
            if resp.ok:
                return resp.json().get("items", [])
        except Exception as exc:
            log.warning("Nao foi possivel buscar runs pendentes: %s", exc)
        return []

    def start_run(self, run_id: str) -> bool:
        try:
            resp = self.session.patch(
                f"{self.base_url}/api/erp-bridge/runs/{run_id}",
                json={"status": "running"},
                timeout=10,
            )
            if resp.status_code == 401:
                self.login()
                resp = self.session.patch(
                    f"{self.base_url}/api/erp-bridge/runs/{run_id}",
                    json={"status": "running"},
                    timeout=10,
                )
            return resp.status_code in (200, 204)
        except Exception as exc:
            log.warning("Nao foi possivel iniciar run %s: %s", run_id, exc)
        return False

    def is_run_cancelled(self, run_id: str) -> bool:
        try:
            resp = self.session.get(
                f"{self.base_url}/api/erp-bridge/runs/{run_id}",
                timeout=10,
            )
            if resp.status_code == 401:
                self.login()
                resp = self.session.get(f"{self.base_url}/api/erp-bridge/runs/{run_id}", timeout=10)
            if resp.ok:
                return resp.json().get("status") == "cancelled"
        except Exception as exc:
            log.warning("Nao foi possivel verificar status do run %s: %s", run_id, exc)
        return False

    def report_items(self, run_id: str, totais: dict) -> None:
        items = []
        for servidor, tipos in totais.items():
            for tipo, s in tipos.items():
                status = "ok"
                if s.get("erro_conexao"):
                    status = "erro_conexao"
                elif s["erros"] > 0 and s["enviados"] == 0:
                    status = "erro_parcial"
                items.append({
                    "servidor": servidor,
                    "tipo": tipo,
                    "enviados": s["enviados"],
                    "ignorados": s["ignorados"],
                    "erros": s["erros"],
                    "status": status,
                    "erro_msg": s.get("erro_msg"),
                })
        if not items:
            return
        try:
            resp = self.session.post(
                f"{self.base_url}/api/erp-bridge/runs/{run_id}/items",
                json=items,
                timeout=15,
            )
            if resp.status_code == 401:
                self.login()
                self.session.post(
                    f"{self.base_url}/api/erp-bridge/runs/{run_id}/items",
                    json=items,
                    timeout=15,
                )
        except Exception as exc:
            log.warning("Nao foi possivel reportar items na API: %s", exc)

    def finalize_run(self, run_id: str, grand: dict, erro_msg: str | None = None) -> None:
        total_erros = grand["erros"]
        total_env   = grand["enviados"]
        if erro_msg:
            status = "error"
        elif total_erros > 0 and total_env > 0:
            status = "partial"
        elif total_erros > 0:
            status = "error"
        else:
            status = "success"
        try:
            self.session.patch(
                f"{self.base_url}/api/erp-bridge/runs/{run_id}",
                json={
                    "status": status,
                    "total_enviados": total_env,
                    "total_ignorados": grand["ignorados"],
                    "total_erros": total_erros,
                    "erro_msg": erro_msg,
                },
                timeout=10,
            )
        except Exception as exc:
            log.warning("Nao foi possivel finalizar run na API: %s", exc)


# ─── Processamento SAP S4/HANA ────────────────────────────────────────────────

def processar_sap(
    oracle_cfg: dict,
    data_ini: date,
    data_fim: date,
    fbtax: FBTaxClient,
) -> dict:
    """Lê s4i_nfe + s4i_nfe_impostos do FCCORP e envia via /api/erp-bridge/import/batch."""
    NOME = "FCCORP"
    stats = {
        "sap_batch": {
            "enviados": 0,
            "ignorados": 0,
            "erros": 0,
        }
    }

    log.info("=" * 60)
    log.info("SAP S4/HANA — servidor : %s", oracle_cfg.get("dsn", ""))
    log.info("Periodo               : %s -> %s", data_ini, data_fim)

    try:
        conn_ora = oracledb.connect(
            user=oracle_cfg["usuario"],
            password=oracle_cfg["senha"],
            dsn=oracle_cfg["dsn"],
            expire_time=2,  # keepalive TCP a cada 2 min — evita firewall cortar conexão longa
        )
        log.info("Conectado ao Oracle SAP FCCORP (thin mode)")
    except Exception as exc:
        log.error("Falha ao conectar ao FCCORP: %s", exc)
        stats["sap_batch"]["erros"] = 1
        stats["sap_batch"]["erro_msg"] = str(exc)
        stats["sap_batch"]["erro_conexao"] = True
        return stats

    try:
        cur = conn_ora.cursor()
        # data_fim é exclusivo na query (data_ini <= credat <= data_fim - 1 dia)
        data_fim_inc = data_fim - timedelta(days=1)
        cur.execute(SAP_QUERY, data_ini=data_ini, data_fim=data_fim_inc)

        cols = [d[0].lower() for d in cur.description]
        rows = []
        for raw in cur:
            rows.append(dict(zip(cols, raw)))
        cur.close()

        log.info("%d documentos encontrados no FCCORP", len(rows))

        if not rows:
            return stats

        # ── Etapa 1: sincronizar parceiros (FORN/CLIE) antes dos movimentos ──
        # Query separada via subquery — sem listas Python, sem ORA-01795
        try:
            cur_p = conn_ora.cursor()
            cur_p.execute(PARCEIROS_QUERY, data_ini=data_ini, data_fim=data_fim_inc)
            parceiros = [
                {"cnpj": str(row[0]).strip(), "nome": str(row[1]).strip() if row[1] else ""}
                for row in cur_p.fetchall() if row[0]
            ]
            cur_p.close()
            if parceiros:
                result_p = fbtax.sync_parceiros(parceiros)
                log.info("[Parceiros] Sincronizados: %d (upserted=%d)",
                         len(parceiros), result_p.get("upserted", 0))
            else:
                log.info("[Parceiros] Nenhum parceiro encontrado no período.")
        except Exception as exc:
            log.warning("[Parceiros] Erro na sincronização (não bloqueia movimentos): %s", exc)

        # ── Etapa 2: converter movimentos (sem lookup de nomes) ──────────────
        documents = []
        for r in rows:
            def s(v):
                return str(v).strip() if v is not None else ""
            def f(v):
                try:
                    return float(v) if v is not None else 0.0
                except (ValueError, TypeError):
                    return 0.0

            documents.append({
                "direct":           s(r.get("direct")),
                "chave":            s(r.get("chave")),
                "modelo":           s(r.get("modelo")),
                "serie":            s(r.get("serie")),
                "numero":           s(r.get("numero")),
                "data_emissao":     s(r.get("data_emissao")),
                "data_autorizacao": s(r.get("data_autorizacao")),
                "mes_ano":          s(r.get("mes_ano")),
                "emit_cnpj":        s(r.get("emit_cnpj")),
                "dest_cnpj":        s(r.get("dest_cnpj")),
                "cancelado":        s(r.get("cancelado")) or "N",
                "nome_parceiro":    "",  # resolvido via tabela parceiros no backend
                "v_total":          f(r.get("v_total")),
                "v_bc_ibs_cbs":     f(r.get("v_bc_ibs_cbs")),
                "v_ibs_uf":         f(r.get("v_ibs_uf")),
                "v_ibs_mun":        f(r.get("v_ibs_mun")),
                "v_ibs":            f(r.get("v_ibs")),
                "v_cbs":            f(r.get("v_cbs")),
            })

        # ── Sumário antes do envio ──────────────────────────────────────────
        _modelos_nfe = {"55", "62", "65"}
        _modelos_cte = {"57", "66", "67"}
        _cnt = {"nfe_saidas": 0, "nfe_entradas": 0, "cte_entradas": 0, "outros": 0}
        _canceladas = 0
        for d in documents:
            if d.get("cancelado") == "S":
                _canceladas += 1
            direct = d.get("direct", "")
            modelo = d.get("modelo", "")
            if direct == "2":
                _cnt["nfe_saidas"] += 1
            elif direct == "1" and modelo in _modelos_nfe:
                _cnt["nfe_entradas"] += 1
            elif direct == "1" and modelo in _modelos_cte:
                _cnt["cte_entradas"] += 1
            else:
                _cnt["outros"] += 1
        log.info("-" * 60)
        log.info("[SAP] Composição — NF-e Saídas: %d  NF-e Entradas: %d  CT-e Entradas: %d  Outros: %d  Canceladas: %d",
                 _cnt["nfe_saidas"], _cnt["nfe_entradas"], _cnt["cte_entradas"], _cnt["outros"], _canceladas)
        log.info("-" * 60)

        # Envia em lotes de 1000 para não sobrecarregar
        BATCH_SIZE = 1000
        total_inserted = 0
        total_ignored  = 0
        total_errors   = 0
        _prog_ts = _time.monotonic()  # timestamp do último log de progresso

        for i in range(0, len(documents), BATCH_SIZE):
            lote = documents[i:i + BATCH_SIZE]
            processados = min(i + BATCH_SIZE, len(documents))
            try:
                result = fbtax.enviar_batch(lote)
                total_inserted += result.get("inserted", 0)
                total_ignored  += result.get("ignored", 0)
                total_errors   += result.get("errors", 0)
                if result.get("error_details"):
                    for err in result["error_details"][:5]:
                        log.warning("  Detalhe erro: %s", err)
            except Exception as exc:
                log.error("Erro ao enviar lote %d: %s", i // BATCH_SIZE + 1, exc)
                total_errors += len(lote)

            # Log de progresso a cada 2 minutos
            if _time.monotonic() - _prog_ts >= 120:
                pct = processados / len(documents) * 100
                log.info("[Progresso] %d/%d docs (%.0f%%) — env=%d  ign=%d  err=%d",
                         processados, len(documents), pct,
                         total_inserted, total_ignored, total_errors)
                _prog_ts = _time.monotonic()

        stats["sap_batch"]["enviados"]  = total_inserted
        stats["sap_batch"]["ignorados"] = total_ignored
        stats["sap_batch"]["erros"]     = total_errors

        log.info("=" * 60)
        log.info("SAP FCCORP — inseridos=%d  ignorados=%d  erros=%d",
                 total_inserted, total_ignored, total_errors)

        # ── Atualiza watermark para importação incremental ────────────────────
        if total_errors == 0:
            set_watermark(oracle_cfg.get("dsn", "sap"), data_fim_inc)
            log.info("[Watermark] Atualizado para %s", data_fim_inc)

    except Exception as exc:
        log.error("Erro durante processamento SAP: %s", exc)
        stats["sap_batch"]["erros"] = 1
        stats["sap_batch"]["erro_msg"] = str(exc)
    finally:
        conn_ora.close()

    return stats


# ─── Processamento Oracle XML (legado) ────────────────────────────────────────

def processar_servidor(
    srv: dict,
    data_ini: date,
    data_fim: date,
    fbtax: FBTaxClient,
    tracker: sqlite3.Connection,
) -> dict:
    nome  = srv["nome"]
    tipos = srv.get("tipos", list(FONTES.keys()))
    stats = {t: {"enviados": 0, "ignorados": 0, "erros": 0} for t in tipos}

    log.info("=" * 60)
    log.info("Servidor : %s", nome)
    log.info("DSN      : %s", srv["dsn"])
    log.info("Periodo  : %s -> %s", data_ini, data_fim)
    log.info("Tipos    : %s", ", ".join(tipos))

    try:
        conn_ora = oracledb.connect(
            user=srv["usuario"],
            password=srv["senha"],
            dsn=srv["dsn"],
            expire_time=2,  # keepalive TCP a cada 2 min — evita firewall cortar conexão longa
        )
        log.info("Conectado ao Oracle (thin mode)")
    except Exception as exc:
        log.error("Falha ao conectar em %s: %s", nome, exc)
        return stats

    try:
        for tipo in tipos:
            fonte = FONTES.get(tipo)
            if fonte is None:
                log.warning("Tipo desconhecido ignorado: %s", tipo)
                continue

            log.info("-" * 40)
            log.info("Consultando %s...", fonte["descricao"])

            try:
                cur = conn_ora.cursor()
                cur.execute(fonte["sql"], data_ini=data_ini, data_fim=data_fim)
                rows = []
                for raw_row in cur:
                    rows.append((
                        str(raw_row[fonte["chave_col"]]).strip(),
                        clob_para_str(raw_row[fonte["xml_col"]]),
                    ))
                cur.close()
            except Exception as exc:
                log.error("Erro na query %s: %s", tipo, exc)
                continue

            total_rows = len(rows)
            log.info("%d registros encontrados", total_rows)

            for chave, xml_str in rows:
                if not xml_str:
                    log.debug("  XML nulo para %s — ignorado", chave)
                    stats[tipo]["ignorados"] += 1
                    continue

                if ja_enviado(tracker, nome, tipo, chave):
                    stats[tipo]["ignorados"] += 1
                    continue

                try:
                    xml_bytes = normalizar_xml(xml_str, adicionar_decl=fonte["adicionar_decl"])
                except Exception as exc:
                    log.error("  Erro ao normalizar XML %s: %s", chave, exc)
                    stats[tipo]["erros"] += 1
                    marcar(tracker, nome, tipo, chave, "erro_xml")
                    continue

                try:
                    result = fbtax.enviar(fonte["endpoint"], chave, xml_bytes)
                    sc = result["status"]
                    if sc in (200, 201):
                        stats[tipo]["enviados"] += 1
                        marcar(tracker, nome, tipo, chave, "ok")
                        log.debug("  OK  %s", chave)
                    elif sc == 409:
                        stats[tipo]["ignorados"] += 1
                        marcar(tracker, nome, tipo, chave, "ok")
                    else:
                        log.warning("  HTTP %d para %s: %s", sc, chave, result["body"])
                        stats[tipo]["erros"] += 1
                        marcar(tracker, nome, tipo, chave, f"erro_{sc}")
                except Exception as exc:
                    log.error("  Erro ao enviar %s: %s", chave, exc)
                    stats[tipo]["erros"] += 1

                s = stats[tipo]
                print(
                    f"\r  {nome:<20} | {tipo:<14} | "
                    f"env:{s['enviados']:>5,}  ign:{s['ignorados']:>5,}  err:{s['erros']:>3,}  ",
                    end="", flush=True
                )

            s = stats[tipo]
            print(
                f"\r  {nome:<20} | {tipo:<14} | "
                f"env:{s['enviados']:>5,}  ign:{s['ignorados']:>5,}  err:{s['erros']:>3,}  "
            )

    finally:
        conn_ora.close()

    return stats


# ─── Argumentos CLI ───────────────────────────────────────────────────────────

def parse_args():
    p = argparse.ArgumentParser(
        description="ERP Bridge Linux — Oracle ERP -> FBTax Apuracao Assistida"
    )
    p.add_argument("--data",      metavar="YYYY-MM-DD", help="Data inicial")
    p.add_argument("--data-fim",  metavar="YYYY-MM-DD", help="Data final (exclusiva)")
    p.add_argument("--mes",       metavar="YYYY-MM",    help="Mes completo")
    p.add_argument("--servidor",  metavar="NOME",       help="Processa apenas este servidor (oracle_xml)")
    p.add_argument("--dry-run",        action="store_true",  help="Consulta Oracle mas nao envia")
    p.add_argument("--daemon",         action="store_true",  help="Modo daemon")
    p.add_argument("--origin",         metavar="ORIGEM",     default="manual", help="Origem do run")
    p.add_argument("--only-parceiros", action="store_true",  help="Sincroniza apenas parceiros (FORN/CLIE), sem importar movimentos")
    return p.parse_args()


# ─── Execução de um ciclo de importação ──────────────────────────────────────

def executar_importacao(
    cfg: dict,
    fbtax: FBTaxClient,
    data_ini: date,
    data_fim: date,
    origem: str = "manual",
    filtro_servidor: str | None = None,
    filtro_servidores: list | None = None,
    dry_run: bool = False,
    existing_run_id: str | None = None,
) -> int:
    erp_type = cfg.get("erp_type", "oracle_xml")

    log.info("=" * 60)
    log.info("ERP Bridge v2.0 — FBTax Apuracao Assistida")
    log.info("Modo    : %s", erp_type)
    log.info("Periodo : %s ate %s", data_ini, data_fim - timedelta(days=1))
    log.info("Origem  : %s", origem)
    if dry_run:
        log.info("MODO DRY-RUN: apenas consultas, sem envio")
    log.info("=" * 60)

    run_id = existing_run_id
    if run_id is None and not dry_run:
        run_id = fbtax.create_run(data_ini, data_fim, origem=origem)
        if run_id:
            log.info("Run API criado: %s", run_id)
    elif run_id:
        log.info("Usando run existente: %s", run_id)

    grand = {"enviados": 0, "ignorados": 0, "erros": 0}

    # ── Modo SAP S4/HANA ──────────────────────────────────────────────────────
    if erp_type == "sap_s4hana":
        if dry_run:
            log.info("DRY-RUN: pulando envio SAP")
            return 0

        oracle_cfg = cfg.get("oracle", {})
        if not oracle_cfg.get("dsn"):
            log.error("erp_type=sap_s4hana mas 'oracle.dsn' nao configurado em config.yaml")
            return 1

        stats = processar_sap(oracle_cfg, data_ini, data_fim, fbtax)

        for s in stats.values():
            grand["enviados"]  += s["enviados"]
            grand["ignorados"] += s["ignorados"]
            grand["erros"]     += s["erros"]

        if run_id:
            fbtax.report_items(run_id, {"FCCORP": stats})
            fbtax.finalize_run(run_id, grand)
            log.info("Run API finalizado: %s", run_id)

        return 0 if grand["erros"] == 0 else 1

    # ── Modo Oracle XML legado ────────────────────────────────────────────────
    tracker = init_tracker()

    servidores = cfg.get("servidores", [])
    if filtro_servidores:
        servidores = [s for s in servidores if s["nome"] in filtro_servidores]
        if not servidores:
            log.error("Nenhum dos servidores %s encontrado no config.yaml", filtro_servidores)
            tracker.close()
            return 1
    elif filtro_servidor:
        servidores = [s for s in servidores if s["nome"] == filtro_servidor]
        if not servidores:
            log.error("Servidor '%s' nao encontrado no config.yaml", filtro_servidor)
            tracker.close()
            return 1

    totais: dict = {}

    for srv in servidores:
        if dry_run:
            log.info("DRY-RUN: pulando envio para %s", srv["nome"])
            continue

        if run_id and fbtax.is_run_cancelled(run_id):
            log.warning("[Cancelado] Run %s foi cancelado pela UI — interrompendo.", run_id)
            tracker.close()
            return 1

        stats = processar_servidor(srv, data_ini, data_fim, fbtax, tracker)
        totais[srv["nome"]] = stats

        if run_id:
            fbtax.report_items(run_id, {srv["nome"]: stats})

        for tipo, s in stats.items():
            for k in grand:
                grand[k] += s[k]

    tracker.close()

    log.info("=" * 60)
    log.info("RELATORIO FINAL")
    log.info("=" * 60)
    for servidor, stats in totais.items():
        log.info("Servidor: %s", servidor)
        for tipo, s in stats.items():
            log.info("  %-20s  enviados: %4d  ignorados: %4d  erros: %4d",
                     tipo, s["enviados"], s["ignorados"], s["erros"])
    log.info("-" * 60)
    log.info("TOTAL: enviados=%d  ignorados=%d  erros=%d",
             grand["enviados"], grand["ignorados"], grand["erros"])
    log.info("Log: %s", log_file)

    if run_id and not dry_run:
        fbtax.finalize_run(run_id, grand)
        log.info("Run API finalizado: %s", run_id)

    return 0 if grand["erros"] == 0 else 1


# ─── Modo Daemon ──────────────────────────────────────────────────────────────

def run_daemon(cfg: dict, fbtax: FBTaxClient) -> int:
    BRASILIA = ZoneInfo("America/Sao_Paulo")
    log.info("=" * 60)
    log.info("ERP Bridge v2.0 — MODO DAEMON iniciado")
    log.info("erp_type: %s", cfg.get("erp_type", "oracle_xml"))
    log.info("Aguardando horario configurado na UI ou trigger manual...")
    log.info("=" * 60)

    ultimo_run_data: date | None = None

    while True:
        try:
            now = datetime.now(tz=BRASILIA)
            agora_hhmm = now.strftime("%H:%M")
            hoje = now.date()

            # ── 0. Heartbeat — sinaliza ao backend que o daemon está ativo ────
            fbtax.heartbeat()

            # ── 1. Reset tracker ──────────────────────────────────────────────
            bridge_cfg_check = fbtax.get_bridge_config()
            if bridge_cfg_check and bridge_cfg_check.get("reset_tracker"):
                log.info("[Daemon] reset_tracker detectado — limpando tracker.db...")
                try:
                    conn_t = sqlite3.connect(TRACKER_DB)
                    deleted = conn_t.execute("DELETE FROM enviados").rowcount
                    conn_t.commit()
                    conn_t.close()
                    log.info("[Daemon] tracker.db limpo: %d registros removidos.", deleted)
                except Exception as exc:
                    log.error("[Daemon] Erro ao limpar tracker.db: %s", exc)
                fbtax.reset_tracker_ack()

            # ── 1. Runs pendentes criados pela UI ─────────────────────────────
            pending = fbtax.get_pending_runs()
            for run in pending:
                run_id        = run["id"]
                data_ini_s    = run.get("data_ini")
                data_fim_s    = run.get("data_fim")
                filiais_json  = run.get("filiais_filter")
                only_parc     = run.get("only_parceiros", False)

                if not data_ini_s or not data_fim_s:
                    log.warning("[Daemon] Run pendente %s sem datas — ignorado", run_id)
                    continue

                filtro_servidores = None
                if filiais_json:
                    try:
                        filtro_servidores = _json.loads(filiais_json)
                        if not isinstance(filtro_servidores, list) or len(filtro_servidores) == 0:
                            filtro_servidores = None
                    except Exception:
                        filtro_servidores = None

                data_ini_run = date.fromisoformat(data_ini_s[:10])
                data_fim_run = date.fromisoformat(data_fim_s[:10]) + timedelta(days=1)

                fbtax.start_run(run_id)

                # ── Modo apenas parceiros ─────────────────────────────────────
                if only_parc:
                    log.info("[Daemon] Run parceiros %s: %s → %s", run_id, data_ini_s, data_fim_s)
                    oracle_cfg = cfg.get("oracle", {})
                    try:
                        conn_ora = oracledb.connect(
                            user=oracle_cfg["usuario"],
                            password=oracle_cfg["senha"],
                            dsn=oracle_cfg["dsn"],
                            expire_time=2,
                        )
                        data_fim_inc = data_fim_run - timedelta(days=1)
                        cur_p = conn_ora.cursor()
                        cur_p.execute(PARCEIROS_QUERY, data_ini=data_ini_run, data_fim=data_fim_inc)
                        parceiros = [
                            {"cnpj": str(row[0]).strip(), "nome": str(row[1]).strip() if row[1] else ""}
                            for row in cur_p.fetchall() if row[0]
                        ]
                        cur_p.close()
                        conn_ora.close()
                        log.info("[Daemon] Parceiros encontrados: %d", len(parceiros))
                        upserted = 0
                        if parceiros:
                            result_p = fbtax.sync_parceiros(parceiros)
                            upserted = result_p.get("upserted", 0)
                            log.info("[Daemon] Parceiros sincronizados: upserted=%d", upserted)
                        fbtax.finalize_run(run_id, {"enviados": upserted, "ignorados": 0, "erros": 0})
                    except Exception as exc:
                        log.error("[Daemon] Erro no run parceiros %s: %s", run_id, exc)
                        fbtax.finalize_run(run_id, {"enviados": 0, "ignorados": 0, "erros": 1},
                                           erro_msg=str(exc))
                    continue

                # ── Importação normal ─────────────────────────────────────────
                filiais_desc = ", ".join(filtro_servidores) if filtro_servidores else "todas"
                log.info("[Daemon] Run manual %s: %s → %s | filiais: %s",
                         run_id, data_ini_s, data_fim_s, filiais_desc)

                try:
                    executar_importacao(
                        cfg=cfg,
                        fbtax=fbtax,
                        data_ini=data_ini_run,
                        data_fim=data_fim_run,
                        origem="manual",
                        filtro_servidores=filtro_servidores,
                        existing_run_id=run_id,
                    )
                except Exception as exc:
                    log.error("[Daemon] Erro no run manual %s: %s", run_id, exc)
                    fbtax.finalize_run(run_id, {"enviados": 0, "ignorados": 0, "erros": 1},
                                       erro_msg=str(exc))

            # ── 2. Horário agendado ────────────────────────────────────────────
            bridge_cfg = fbtax.get_bridge_config()
            if bridge_cfg and bridge_cfg.get("ativo") and bridge_cfg.get("horario") == agora_hhmm:
                if ultimo_run_data == hoje:
                    _time.sleep(60)
                    continue

                erp_type_curr = cfg.get("erp_type", "oracle_xml")
                dias_retro = bridge_cfg.get("dias_retroativos", 1)

                # SAP: usa watermark incremental se disponível
                if erp_type_curr == "sap_s4hana":
                    dsn = cfg.get("oracle", {}).get("dsn", "sap_default")
                    watermark = get_watermark(dsn)
                    if watermark:
                        data_ini_sched = watermark
                        data_fim_sched = hoje + timedelta(days=1)
                        log.info("[Daemon] SAP incremental desde %s (watermark)", watermark)
                    else:
                        data_ini_sched = hoje - timedelta(days=dias_retro)
                        data_fim_sched = hoje + timedelta(days=1)
                        log.info("[Daemon] SAP sem watermark — retroativo %d dia(s)", dias_retro)
                else:
                    data_ini_sched = hoje - timedelta(days=dias_retro)
                    data_fim_sched = hoje + timedelta(days=1)
                    log.info("[Daemon] Horario %s atingido — retroativo %d dia(s)",
                             agora_hhmm, dias_retro)

                try:
                    executar_importacao(
                        cfg=cfg,
                        fbtax=fbtax,
                        data_ini=data_ini_sched,
                        data_fim=data_fim_sched,
                        origem="scheduler",
                    )
                    ultimo_run_data = hoje
                except Exception as exc:
                    log.error("[Daemon] Erro durante importacao agendada: %s", exc)

        except Exception as exc:
            log.warning("[Daemon] Erro no loop: %s", exc)

        _time.sleep(60)


# ─── Main ─────────────────────────────────────────────────────────────────────

def main() -> int:
    if not CONFIG_F.exists():
        log.error("config.yaml nao encontrado em %s", CONFIG_F)
        return 1

    with open(CONFIG_F, encoding="utf-8") as f:
        cfg = yaml.safe_load(f)

    args = parse_args()
    erp_type = cfg.get("erp_type", "oracle_xml")

    fbtax = FBTaxClient(cfg["fbtax"])

    # Modo daemon
    if args.daemon:
        if fbtax.api_key:
            creds = fbtax.fetch_credentials()
            if creds:
                if creds.get("fbtax_email"):
                    fbtax.email = creds["fbtax_email"]
                if creds.get("fbtax_password"):
                    fbtax.password = creds["fbtax_password"]
                # Sobrescreve erp_type com o valor da API (tem precedência sobre config.yaml)
                if creds.get("erp_type"):
                    cfg["erp_type"] = creds["erp_type"]
                    erp_type = creds["erp_type"]
                # SAP: credenciais Oracle vão para cfg["oracle"]
                if erp_type == "sap_s4hana":
                    if "oracle" not in cfg:
                        cfg["oracle"] = {}
                    if creds.get("oracle_usuario"):
                        cfg["oracle"]["usuario"] = creds["oracle_usuario"]
                    if creds.get("oracle_senha"):
                        cfg["oracle"]["senha"] = creds["oracle_senha"]
                    if creds.get("oracle_dsn"):
                        cfg["oracle"]["dsn"] = creds["oracle_dsn"]
                else:
                    # oracle_xml: propaga para todos os servidores
                    for srv in cfg.get("servidores", []):
                        if creds.get("oracle_usuario"):
                            srv["usuario"] = creds["oracle_usuario"]
                        if creds.get("oracle_senha"):
                            srv["senha"] = creds["oracle_senha"]
                log.info("Credenciais carregadas do servidor FBTax (erp_type=%s).", erp_type)
            else:
                log.warning("api_key configurada mas nao foi possivel buscar credenciais. Usando config.yaml.")
        try:
            fbtax.login()
        except Exception as exc:
            log.error("Falha ao autenticar no FBTax: %s", exc)
            return 1

        # Registra servidores na UI
        if erp_type == "sap_s4hana":
            fbtax.registrar_servidores(["FCCORP"])
        else:
            nomes = [s["nome"] for s in cfg.get("servidores", [])]
            fbtax.registrar_servidores(nomes)

        return run_daemon(cfg, fbtax)

    # Modo normal
    if args.mes:
        ano, mes = map(int, args.mes.split("-"))
        data_ini = date(ano, mes, 1)
        data_fim = date(ano + 1, 1, 1) if mes == 12 else date(ano, mes + 1, 1)
    else:
        dias = cfg.get("dias_padrao", 7)
        data_ini = date.fromisoformat(args.data)     if args.data     else date.today() - timedelta(days=dias)
        data_fim = date.fromisoformat(args.data_fim) if args.data_fim else date.today() + timedelta(days=1)

    if not args.dry_run:
        try:
            fbtax.login()
        except Exception as exc:
            log.error("Falha ao autenticar no FBTax: %s", exc)
            return 1

    # Modo --only-parceiros: sincroniza apenas FORN/CLIE sem importar movimentos
    if args.only_parceiros:
        if erp_type != "sap_s4hana":
            log.error("--only-parceiros disponível apenas para erp_type=sap_s4hana")
            return 1
        oracle_cfg = cfg.get("oracle", {})
        if not oracle_cfg.get("dsn"):
            log.error("oracle.dsn nao configurado em config.yaml")
            return 1
        log.info("=" * 60)
        log.info("Modo: apenas parceiros (FORN/CLIE)")
        log.info("Periodo: %s -> %s", data_ini, data_fim - timedelta(days=1))
        log.info("=" * 60)
        try:
            conn_ora = oracledb.connect(
                user=oracle_cfg["usuario"],
                password=oracle_cfg["senha"],
                dsn=oracle_cfg["dsn"],
                expire_time=2,
            )
            data_fim_inc = data_fim - timedelta(days=1)
            cur_p = conn_ora.cursor()
            cur_p.execute(PARCEIROS_QUERY, data_ini=data_ini, data_fim=data_fim_inc)
            parceiros = [
                {"cnpj": str(row[0]).strip(), "nome": str(row[1]).strip() if row[1] else ""}
                for row in cur_p.fetchall() if row[0]
            ]
            cur_p.close()
            conn_ora.close()
            log.info("Parceiros encontrados: %d", len(parceiros))
            if parceiros and not args.dry_run:
                result_p = fbtax.sync_parceiros(parceiros)
                log.info("Parceiros sincronizados: upserted=%d", result_p.get("upserted", 0))
        except Exception as exc:
            log.error("Erro na sincronização de parceiros: %s", exc)
            return 1
        return 0

    return executar_importacao(
        cfg=cfg,
        fbtax=fbtax,
        data_ini=data_ini,
        data_fim=data_fim,
        origem=args.origin,
        filtro_servidor=args.servidor,
        dry_run=args.dry_run,
    )


if __name__ == "__main__":
    sys.exit(main())
