-- Migration 091: adiciona coluna only_parceiros em erp_bridge_runs
-- Quando TRUE, o daemon sincroniza apenas FORN/CLIE sem importar movimentos.

ALTER TABLE erp_bridge_runs
  ADD COLUMN IF NOT EXISTS only_parceiros BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN erp_bridge_runs.only_parceiros
  IS 'Quando TRUE, sincroniza apenas parceiros (FORN/CLIE) sem importar movimentos fiscais';
