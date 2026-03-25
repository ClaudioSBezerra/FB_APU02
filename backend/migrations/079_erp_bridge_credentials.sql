-- Migration 079: ERP Bridge — credenciais criptografadas
-- Remove senhas do config.yaml; armazena fbtax + oracle credentials no banco.
-- O daemon autentica via api_key e busca as credenciais em /api/erp-bridge/credentials.

ALTER TABLE erp_bridge_config
  ADD COLUMN IF NOT EXISTS fbtax_email    TEXT,
  ADD COLUMN IF NOT EXISTS fbtax_password TEXT,   -- AES-256-GCM encrypted
  ADD COLUMN IF NOT EXISTS oracle_usuario TEXT,   -- AES-256-GCM encrypted
  ADD COLUMN IF NOT EXISTS oracle_senha   TEXT,   -- AES-256-GCM encrypted
  ADD COLUMN IF NOT EXISTS api_key        TEXT,   -- AES-256-GCM encrypted (para exibição no painel)
  ADD COLUMN IF NOT EXISTS api_key_hash   TEXT;   -- SHA-256 do api_key em hex (para lookup rápido)

CREATE INDEX IF NOT EXISTS idx_erp_bridge_config_api_key_hash
  ON erp_bridge_config(api_key_hash)
  WHERE api_key_hash IS NOT NULL;
