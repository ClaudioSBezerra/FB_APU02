-- Migration 101: adiciona colunas cfop e tipo_cfop a nfe_saidas
-- Permite rastrear o tipo de operação das notas de saída importadas via ERP Bridge.
-- O preenchimento efetivo virá do reprocessamento via bridge.py (reset_tracker).

ALTER TABLE nfe_saidas
  ADD COLUMN IF NOT EXISTS cfop      VARCHAR(4),
  ADD COLUMN IF NOT EXISTS tipo_cfop VARCHAR(1);

-- Backfill tipo_cfop a partir da tabela cfop (para registros que já tenham cfop preenchido)
UPDATE nfe_saidas ns
SET tipo_cfop = c.tipo
FROM cfop c
WHERE c.cfop = ns.cfop
  AND ns.tipo_cfop IS NULL;

CREATE INDEX IF NOT EXISTS idx_nfe_saidas_tipo_cfop ON nfe_saidas(company_id, tipo_cfop);
CREATE INDEX IF NOT EXISTS idx_nfe_saidas_cfop      ON nfe_saidas(company_id, cfop);
