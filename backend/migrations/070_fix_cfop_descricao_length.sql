-- Migration 070: Aumenta campo descricao_cfop de VARCHAR(100) para VARCHAR(255)
-- Fix: migration 026_seed_cfops falhava com "value too long for type character varying(100)"
ALTER TABLE cfop ALTER COLUMN descricao_cfop TYPE VARCHAR(255);
