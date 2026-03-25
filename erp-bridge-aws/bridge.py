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
  python bridge.py --daemon                      # modo daemon (roda no horário configurado via UI)
"""

import argparse
import io
import logging
import re
import sqlite3
import sys
import time as _time
from datetime import date, datetime, timedelta, timezone
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

    # ── Métodos de reporte de execução via API ─────────────────────────────────

    def get_bridge_config(self) -> dict | None:
        """Busca a configuração de agendamento do bridge na API."""
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

    def registrar_servidores(self, nomes: list) -> None:
        """Registra os servidores configurados na API para popular o dropdown do trigger manual."""
        try:
            self.session.post(
                f"{self.base_url}/api/erp-bridge/servidores/registrar",
                json={"nomes": nomes},
                timeout=10,
            )
        except Exception as exc:
            log.warning("Nao foi possivel registrar servidores: %s", exc)

    def reset_tracker_ack(self) -> bool:
        """Confirma para a API que o tracker.db foi limpo (reset_tracker = false)."""
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
        """Cria um novo registro de execução na API. Retorna o run_id ou None."""
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

    def get_pending_runs(self) -> list:
        """Busca runs com status='pending' criados pela UI para execução manual."""
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
        """Marca um run pendente como 'running' antes de iniciar a execução."""
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
        """Verifica se o run foi cancelado pela UI durante a execução."""
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
        """Envia os totais por servidor/tipo à API."""
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
        """Finaliza o run na API com os totais consolidados."""
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

            # Lê o conteúdo do CLOB/BLOB durante a iteração do cursor (locator ainda válido).
            # fetchall() retorna apenas ponteiros (LOB locators); após cur.close() eles
            # ficam inválidos. Ler inline evita XMLs inválidos e dispensa I/O de disco.
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
    p.add_argument("--servidor",  metavar="NOME",       help="Processa apenas este servidor")
    p.add_argument("--dry-run",   action="store_true",  help="Consulta Oracle mas nao envia")
    p.add_argument("--daemon",    action="store_true",  help="Modo daemon: executa no horario configurado via UI")
    p.add_argument("--origin",    metavar="ORIGEM",     default="manual", help="Origem do run (manual|scheduler)")
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
    """Executa um ciclo completo de importação e reporta via API. Retorna 0 (ok) ou 1 (erros)."""
    log.info("=" * 60)
    log.info("ERP Bridge v1.4 Linux — FBTax Apuracao Assistida")
    log.info("Periodo : %s ate %s", data_ini, data_fim - timedelta(days=1))
    log.info("Origem  : %s", origem)
    if dry_run:
        log.info("MODO DRY-RUN: apenas consultas, sem envio")
    log.info("=" * 60)

    # Usa run existente (criado pela UI) ou abre um novo
    run_id = existing_run_id
    if run_id is None and not dry_run:
        run_id = fbtax.create_run(data_ini, data_fim, origem=origem)
        if run_id:
            log.info("Run API criado: %s", run_id)
    elif run_id:
        log.info("Usando run existente: %s", run_id)

    tracker = init_tracker()

    servidores = cfg["servidores"]
    # filtro_servidores (lista, da UI) tem precedência sobre filtro_servidor (CLI)
    if filtro_servidores:
        servidores = [s for s in servidores if s["nome"] in filtro_servidores]
        if not servidores:
            log.error("Nenhum dos servidores %s encontrado no config.yaml", filtro_servidores)
            return 1
    elif filtro_servidor:
        servidores = [s for s in servidores if s["nome"] == filtro_servidor]
        if not servidores:
            log.error("Servidor '%s' nao encontrado no config.yaml", filtro_servidor)
            return 1

    totais: dict = {}
    grand = {"enviados": 0, "ignorados": 0, "erros": 0}

    for srv in servidores:
        if dry_run:
            log.info("DRY-RUN: pulando envio para %s", srv["nome"])
            continue

        # Verifica cancelamento antes de cada servidor (a UI pode ter abortado)
        if run_id and fbtax.is_run_cancelled(run_id):
            log.warning("[Cancelado] Run %s foi cancelado pela UI — interrompendo.", run_id)
            tracker.close()
            return 1

        stats = processar_servidor(srv, data_ini, data_fim, fbtax, tracker)
        totais[srv["nome"]] = stats

        # Reporta esta filial imediatamente para o acompanhamento em tempo real
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

    # Finaliza o run na API com os totais consolidados
    if run_id and not dry_run:
        fbtax.finalize_run(run_id, grand)
        log.info("Run API finalizado: %s", run_id)

    return 0 if grand["erros"] == 0 else 1


# ─── Modo Daemon ──────────────────────────────────────────────────────────────

def run_daemon(cfg: dict, fbtax: FBTaxClient) -> int:
    """Loop infinito: verifica a cada minuto runs pendentes (manual) e o horário agendado."""
    BRASILIA = ZoneInfo("America/Sao_Paulo")
    log.info("=" * 60)
    log.info("ERP Bridge v1.4 — MODO DAEMON iniciado")
    log.info("Aguardando horario configurado na UI ou trigger manual...")
    log.info("=" * 60)

    ultimo_run_data: date | None = None  # evita rodar agendamento mais de uma vez por dia

    while True:
        try:
            now = datetime.now(tz=BRASILIA)
            agora_hhmm = now.strftime("%H:%M")
            hoje = now.date()

            # ── 0. Verifica se a base foi limpa e o tracker.db deve ser resetado ─
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

            # ── 1. Verifica runs pendentes criados pela UI ─────────────────────
            pending = fbtax.get_pending_runs()
            for run in pending:
                run_id    = run["id"]
                data_ini_s = run.get("data_ini")
                data_fim_s = run.get("data_fim")
                filiais_json = run.get("filiais_filter")  # string JSON ou None

                if not data_ini_s or not data_fim_s:
                    log.warning("[Daemon] Run pendente %s sem datas — ignorado", run_id)
                    continue

                # Parseia o filtro de filiais
                filtro_servidores = None
                if filiais_json:
                    import json as _json
                    try:
                        filtro_servidores = _json.loads(filiais_json)
                        if not isinstance(filtro_servidores, list) or len(filtro_servidores) == 0:
                            filtro_servidores = None
                    except Exception:
                        filtro_servidores = None

                data_ini_run = date.fromisoformat(data_ini_s[:10])
                # data_fim armazenado é inclusivo; a query Oracle usa < data_fim (exclusivo)
                data_fim_run = date.fromisoformat(data_fim_s[:10]) + timedelta(days=1)

                filiais_desc = ", ".join(filtro_servidores) if filtro_servidores else "todas"
                log.info("[Daemon] Run manual %s: %s → %s | filiais: %s",
                         run_id, data_ini_s, data_fim_s, filiais_desc)

                # Marca como 'running' antes de iniciar
                fbtax.start_run(run_id)

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

            # ── 2. Verifica horário agendado ───────────────────────────────────
            bridge_cfg = fbtax.get_bridge_config()
            if bridge_cfg and bridge_cfg.get("ativo") and bridge_cfg.get("horario") == agora_hhmm:
                if ultimo_run_data == hoje:
                    _time.sleep(60)
                    continue

                dias_retro = bridge_cfg.get("dias_retroativos", 1)
                data_ini = hoje - timedelta(days=dias_retro)
                data_fim = hoje + timedelta(days=1)

                log.info("[Daemon] Horario %s atingido — iniciando importacao (%d dia(s) retroativo(s))",
                         agora_hhmm, dias_retro)

                try:
                    executar_importacao(
                        cfg=cfg,
                        fbtax=fbtax,
                        data_ini=data_ini,
                        data_fim=data_fim,
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

    fbtax = FBTaxClient(cfg["fbtax"])

    # Modo daemon — não precisa de datas, as busca da API a cada ciclo
    if args.daemon:
        try:
            fbtax.login()
        except Exception as exc:
            log.error("Falha ao autenticar no FBTax: %s", exc)
            return 1
        nomes = [s["nome"] for s in cfg.get("servidores", [])]
        fbtax.registrar_servidores(nomes)
        return run_daemon(cfg, fbtax)

    # Modo normal (importação pontual)
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
