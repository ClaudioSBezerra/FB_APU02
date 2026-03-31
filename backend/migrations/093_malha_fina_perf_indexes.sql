-- Migration 093: índices compostos para acelerar NOT EXISTS da Malha Fina
--
-- O handler malhaFinaList faz NOT EXISTS (SELECT 1 FROM nfe_saidas t WHERE
-- t.company_id = $1 AND t.chave_nfe = rd.chave_dfe ...) para cada linha
-- de rfb_debitos — sem índice em (company_id, chave_nfe) causa seq scan.

CREATE INDEX IF NOT EXISTS idx_nfe_saidas_company_chave
    ON nfe_saidas (company_id, chave_nfe);

CREATE INDEX IF NOT EXISTS idx_nfe_entradas_company_chave
    ON nfe_entradas (company_id, chave_nfe);

CREATE INDEX IF NOT EXISTS idx_cte_entradas_company_chave
    ON cte_entradas (company_id, chave_cte);

-- Índice para a query principal de rfb_debitos (company_id + modelo_dfe)
CREATE INDEX IF NOT EXISTS idx_rfb_debitos_company_modelo
    ON rfb_debitos (company_id, modelo_dfe)
    WHERE chave_dfe IS NOT NULL AND chave_dfe != '';
