#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
ERP Bridge (Linux/AWS) — Oracle ERP (Totvs/Protheus) → FBTax Apuração Assistida
Versão Linux: usa oracledb thin mode (sem Oracle Client instalado).
DSNs configurados como host:port/service diretamente no config.yaml.

Uso:
  python bridge.py                               # últimos N dias (padrão config)
  python bridge.py --data 2026-01-01             # desde data específica
  python bridge.py --data 2026-03-01 --data-fim 2026-03-19
  python bridge.py --mes 2026-03                 # mês inteiro
  python bridge.py --servidor "FC - Recife"      # só um servidor
"""

import argparse
import io
import logging
import re
import sqlite3
import sys
from datetime import date, datetime, timedelta
from pathlib import Path

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

# ─── Normalização de XML ──────────────────────────────────────────────────────

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

# ─── Definição das fontes de dados ───────────────────────────────────────────

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

# ─── Cliente FBTax ────────────────────────────────────────────────────────────

class FBTaxClient:
    def __init__(self, cfg: dict):
        self.base_url   = cfg["url"].rstrip("/")
        self.email      = cfg["email"]
        self.password   = cfg["password"]
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

# ─── Processamento de um servidor Oracle ─────────────────────────────────────

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
                rows = cur.fetchall()
                cur.close()
            except Exception as exc:
                log.error("Erro na query %s: %s", tipo, exc)
                continue

            total_rows = len(rows)
            log.info("%d registros encontrados", total_rows)

            for row in rows:
                chave   = str(row[fonte["chave_col"]]).strip()
                xml_raw = row[fonte["xml_col"]]

                if not xml_raw:
                    log.debug("  XML nulo para %s — ignorado", chave)
                    stats[tipo]["ignorados"] += 1
                    continue

                if ja_enviado(tracker, nome, tipo, chave):
                    stats[tipo]["ignorados"] += 1
                    continue

                xml_str = clob_para_str(xml_raw)

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
    p.add_argument("--servidor",  metavar="NOME",       help="Processa apenas este servidor")
    p.add_argument("--dry-run",   action="store_true",  help="Consulta Oracle mas nao envia")
    return p.parse_args()

# ─── Main ─────────────────────────────────────────────────────────────────────

def main() -> int:
    if not CONFIG_F.exists():
        log.error("config.yaml nao encontrado em %s", CONFIG_F)
        return 1

    with open(CONFIG_F, encoding="utf-8") as f:
        cfg = yaml.safe_load(f)

    args = parse_args()

    if args.mes:
        ano, mes = map(int, args.mes.split("-"))
        data_ini = date(ano, mes, 1)
        data_fim = date(ano + 1, 1, 1) if mes == 12 else date(ano, mes + 1, 1)
    else:
        dias = cfg.get("dias_padrao", 7)
        data_ini = date.fromisoformat(args.data)     if args.data     else date.today() - timedelta(days=dias)
        data_fim = date.fromisoformat(args.data_fim) if args.data_fim else date.today() + timedelta(days=1)

    log.info("=" * 60)
    log.info("ERP Bridge v1.2 Linux — FBTax Apuracao Assistida")
    log.info("Periodo : %s ate %s", data_ini, data_fim - timedelta(days=1))
    if args.dry_run:
        log.info("MODO DRY-RUN: apenas consultas, sem envio")
    log.info("=" * 60)

    fbtax = FBTaxClient(cfg["fbtax"])
    if not args.dry_run:
        try:
            fbtax.login()
        except Exception as exc:
            log.error("Falha ao autenticar no FBTax: %s", exc)
            return 1

    tracker = init_tracker()

    servidores = cfg["servidores"]
    if args.servidor:
        servidores = [s for s in servidores if s["nome"] == args.servidor]
        if not servidores:
            log.error("Servidor '%s' nao encontrado no config.yaml", args.servidor)
            return 1

    totais: dict = {}
    for srv in servidores:
        if args.dry_run:
            log.info("DRY-RUN: pulando envio para %s", srv["nome"])
            continue
        totais[srv["nome"]] = processar_servidor(srv, data_ini, data_fim, fbtax, tracker)

    tracker.close()

    log.info("=" * 60)
    log.info("RELATORIO FINAL")
    log.info("=" * 60)
    grand = {"enviados": 0, "ignorados": 0, "erros": 0}
    for servidor, stats in totais.items():
        log.info("Servidor: %s", servidor)
        for tipo, s in stats.items():
            log.info("  %-20s  enviados: %4d  ignorados: %4d  erros: %4d",
                     tipo, s["enviados"], s["ignorados"], s["erros"])
            for k in grand:
                grand[k] += s[k]
    log.info("-" * 60)
    log.info("TOTAL: enviados=%d  ignorados=%d  erros=%d",
             grand["enviados"], grand["ignorados"], grand["erros"])
    log.info("Log: %s", log_file)

    return 0 if grand["erros"] == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
