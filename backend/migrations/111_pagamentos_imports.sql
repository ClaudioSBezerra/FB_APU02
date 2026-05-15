-- Migration 111: Histórico de batches de importação de pagamentos a fornecedores

CREATE TABLE IF NOT EXISTS pagamentos_imports (
    id            UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id    UUID          NOT NULL REFERENCES companies(id),
    mes_ano       VARCHAR(7),
    filename      VARCHAR(255),
    total_linhas  INT           NOT NULL DEFAULT 0,
    importados    INT           NOT NULL DEFAULT 0,
    duplicados    INT           NOT NULL DEFAULT 0,
    erros         INT           NOT NULL DEFAULT 0,
    erro_detalhe  TEXT,
    importado_por UUID          REFERENCES users(id),
    importado_em  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_pag_imports_company
    ON pagamentos_imports (company_id, importado_em DESC);
