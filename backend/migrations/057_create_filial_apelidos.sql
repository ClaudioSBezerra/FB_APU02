CREATE TABLE IF NOT EXISTS filial_apelidos (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id  UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    cnpj        VARCHAR(14) NOT NULL,
    apelido     VARCHAR(20) NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_filial_apelidos_company_cnpj ON filial_apelidos(company_id, cnpj);
CREATE INDEX IF NOT EXISTS idx_filial_apelidos_company ON filial_apelidos(company_id);
