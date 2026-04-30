-- Índice para filtrar créditos por situação (A_APROPRIAR, APROPRIADO, COMPENSADO)
CREATE INDEX IF NOT EXISTS idx_rfb_creditos_situacao
    ON rfb_creditos(company_id, situacao_credito, data_dfe_emissao DESC NULLS LAST);

CREATE INDEX IF NOT EXISTS idx_rfb_creditos_periodo
    ON rfb_creditos(company_id, data_apuracao, situacao_credito);
