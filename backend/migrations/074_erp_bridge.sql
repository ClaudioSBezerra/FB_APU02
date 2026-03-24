-- Migration 074: ERP Bridge — tabelas de configuração de agendamento e histórico de execuções

CREATE TABLE IF NOT EXISTS erp_bridge_config (
  company_id        UUID PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
  ativo             BOOLEAN NOT NULL DEFAULT false,
  horario           TIME NOT NULL DEFAULT '02:00:00',
  dias_retroativos  INTEGER NOT NULL DEFAULT 1,
  ultimo_run_em     TIMESTAMP WITH TIME ZONE,
  updated_at        TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS erp_bridge_runs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  company_id      UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
  iniciado_em     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  finalizado_em   TIMESTAMP WITH TIME ZONE,
  status          TEXT NOT NULL DEFAULT 'running',   -- running | success | partial | error
  data_ini        DATE,
  data_fim        DATE,
  total_enviados  INTEGER NOT NULL DEFAULT 0,
  total_ignorados INTEGER NOT NULL DEFAULT 0,
  total_erros     INTEGER NOT NULL DEFAULT 0,
  erro_msg        TEXT,
  origem          TEXT NOT NULL DEFAULT 'manual'     -- manual | scheduler
);

CREATE TABLE IF NOT EXISTS erp_bridge_run_items (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id      UUID NOT NULL REFERENCES erp_bridge_runs(id) ON DELETE CASCADE,
  servidor    TEXT NOT NULL,
  tipo        TEXT NOT NULL,
  enviados    INTEGER NOT NULL DEFAULT 0,
  ignorados   INTEGER NOT NULL DEFAULT 0,
  erros       INTEGER NOT NULL DEFAULT 0,
  status      TEXT NOT NULL DEFAULT 'ok',            -- ok | erro_conexao | erro_query | erro_parcial
  erro_msg    TEXT
);

CREATE INDEX IF NOT EXISTS idx_erp_bridge_runs_company
  ON erp_bridge_runs(company_id, iniciado_em DESC);

CREATE INDEX IF NOT EXISTS idx_erp_bridge_run_items_run
  ON erp_bridge_run_items(run_id);
