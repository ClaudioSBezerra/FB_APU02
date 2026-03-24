-- Migration 077: ERP Bridge — flag reset_tracker
-- Quando LimparDadosApuracao é executado, sinaliza ao daemon Bridge
-- que deve limpar o tracker.db local para permitir reimportação completa.

ALTER TABLE erp_bridge_config
  ADD COLUMN IF NOT EXISTS reset_tracker BOOLEAN NOT NULL DEFAULT FALSE;
