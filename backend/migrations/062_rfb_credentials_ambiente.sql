-- Migration 062: Add ambiente column to rfb_credentials
-- Supports 'producao' (rtc) and 'producao_restrita' (prr-rtc / beta access)
ALTER TABLE rfb_credentials
  ADD COLUMN IF NOT EXISTS ambiente VARCHAR(50) NOT NULL DEFAULT 'producao';
