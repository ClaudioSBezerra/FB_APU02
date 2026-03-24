-- Migration 076: ERP Bridge — trigger manual com filtro de filiais
--
-- Adiciona:
--   - filiais_filter TEXT: JSON array de nomes de servidor (NULL = todas)
--   - status 'pending': run criado pela UI aguardando o daemon Bridge
--   - índice para busca rápida de runs pendentes

ALTER TABLE erp_bridge_runs
  ADD COLUMN IF NOT EXISTS filiais_filter TEXT DEFAULT NULL;

COMMENT ON COLUMN erp_bridge_runs.filiais_filter
  IS 'JSON array de nomes de servidor, ex: ["FC - Recife"]. NULL = todos os servidores.';

-- Índice parcial para busca eficiente de runs pendentes (poucos registros)
CREATE INDEX IF NOT EXISTS idx_erp_bridge_runs_pending
  ON erp_bridge_runs(company_id, iniciado_em ASC)
  WHERE status = 'pending';
