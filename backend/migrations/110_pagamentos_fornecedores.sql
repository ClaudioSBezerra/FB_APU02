-- Migration 110: Tabela de pagamentos a fornecedores
-- Rastreamento de pagamentos que a empresa faz a seus fornecedores
-- Requisito futuro: cruzamento com rfb_creditos.situacao_credito (reforma tributária IBS/CBS)

CREATE TABLE IF NOT EXISTS pagamentos_fornecedores (
    id                BIGSERIAL PRIMARY KEY,
    company_id        UUID          NOT NULL REFERENCES companies(id),
    chave_doc         VARCHAR(100)  NOT NULL,
    tipo_doc          VARCHAR(10)   NOT NULL,
    forn_cnpj         VARCHAR(14)   NOT NULL,
    forn_nome         VARCHAR(255),
    data_pagamento    DATE          NOT NULL,
    valor_pagamento   NUMERIC(15,2) NOT NULL CHECK (valor_pagamento > 0),
    num_doc_pagamento VARCHAR(100),
    descricao         TEXT,
    mes_ano           VARCHAR(7)    NOT NULL,
    import_id         UUID          NOT NULL,
    importado_em      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    importado_por     UUID          REFERENCES users(id),
    CONSTRAINT uq_pag_forn UNIQUE (company_id, chave_doc, data_pagamento, valor_pagamento)
);

CREATE INDEX IF NOT EXISTS idx_pag_forn_company_mes
    ON pagamentos_fornecedores (company_id, mes_ano);

CREATE INDEX IF NOT EXISTS idx_pag_forn_chave
    ON pagamentos_fornecedores (company_id, chave_doc);

CREATE INDEX IF NOT EXISTS idx_pag_forn_cnpj
    ON pagamentos_fornecedores (company_id, forn_cnpj);

CREATE INDEX IF NOT EXISTS idx_pag_forn_import
    ON pagamentos_fornecedores (import_id);
