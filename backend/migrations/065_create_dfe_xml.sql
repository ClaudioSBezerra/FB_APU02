-- Migration 065: Tabela dfe_xml — armazena XML bruto de NF-e, NFC-e e CT-e
-- Permite gerar DANFE/DACTE sem depender de serviços externos com captcha.
-- Chave única por empresa+chave para evitar duplicatas.

CREATE TABLE IF NOT EXISTS dfe_xml (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id  UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    chave       VARCHAR(44) NOT NULL,
    tipo        VARCHAR(5)  NOT NULL DEFAULT 'nfe',   -- nfe, nfce, cte
    modelo      SMALLINT,                              -- 55, 65, 57
    xml_raw     TEXT        NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_dfe_xml_company_chave UNIQUE (company_id, chave)
);

CREATE INDEX IF NOT EXISTS idx_dfe_xml_company_chave ON dfe_xml(company_id, chave);
