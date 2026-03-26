-- Adiciona DSN Oracle ao ERP Bridge config (para SAP S/4HANA)
ALTER TABLE erp_bridge_config
  ADD COLUMN IF NOT EXISTS oracle_dsn TEXT;
