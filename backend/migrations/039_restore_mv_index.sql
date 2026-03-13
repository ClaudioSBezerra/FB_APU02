-- Migration: Restore Unique Index for Concurrent Refresh (Post-038)
-- Required because 038 dropped the view (and thus the index)

CREATE UNIQUE INDEX IF NOT EXISTS idx_mv_mercadorias_agregada_v3
ON mv_mercadorias_agregada (company_id, filial_nome, filial_cnpj, mes_ano, ano, tipo, tipo_cfop, origem, tipo_operacao);
