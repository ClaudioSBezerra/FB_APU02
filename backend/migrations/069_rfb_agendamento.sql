-- Migration 069: Add scheduling columns to rfb_credentials
-- agendamento_ativo: enables/disables automatic daily import
-- horario_agendamento: time of day in Brasília (UTC-3), stored as TIME without timezone
ALTER TABLE rfb_credentials
  ADD COLUMN IF NOT EXISTS agendamento_ativo BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS horario_agendamento TIME DEFAULT '06:00:00';
