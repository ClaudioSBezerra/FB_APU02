-- Migration 080: adiciona erp_type em erp_bridge_config
-- oracle_xml = importação por filial via XML (legado)
-- sap_s4hana = importação via tabelas s4i_nfe + s4i_nfe_impostos (SAP S4/HANA)

ALTER TABLE erp_bridge_config
  ADD COLUMN IF NOT EXISTS erp_type TEXT NOT NULL DEFAULT 'oracle_xml';

COMMENT ON COLUMN erp_bridge_config.erp_type IS
  'oracle_xml = por filial com XML (legado) | sap_s4hana = tabela s4i_nfe (novo)';
