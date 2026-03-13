CREATE TABLE IF NOT EXISTS forn_simples (
    cnpj VARCHAR(14) PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for faster lookups (though PK already indexes it, explicitly naming it can be useful or just rely on PK)
-- CREATE INDEX idx_forn_simples_cnpj ON forn_simples(cnpj);
