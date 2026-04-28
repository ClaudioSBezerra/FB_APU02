-- Índice composto para suportar paginação ordenada na Malha Fina sem varredura total.
-- Permite que o planner use index scan em (company_id, data_dfe_emissao DESC) e
-- interrompa após N linhas, evitando sort de 200k+ registros.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rfb_debitos_company_date_sort
    ON rfb_debitos (company_id, data_dfe_emissao DESC NULLS LAST)
    WHERE chave_dfe IS NOT NULL AND chave_dfe != '';
