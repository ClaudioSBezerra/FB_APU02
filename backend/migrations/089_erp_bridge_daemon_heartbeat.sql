-- Adiciona coluna para rastrear quando o daemon conectou pela última vez
ALTER TABLE erp_bridge_config
    ADD COLUMN IF NOT EXISTS daemon_last_seen TIMESTAMPTZ;
