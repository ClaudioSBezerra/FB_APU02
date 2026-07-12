-- Migration 120: índice para os padrões de consulta introduzidos pela Story 2.4
-- (contagem de falhas consecutivas e listagem paginada do histórico), ambos
-- filtrando por (company_id, bukrs) e ordenando por iniciado_em DESC. O índice
-- único existente (company_id, bukrs, request_id) não cobre essa ordenação.

CREATE INDEX IF NOT EXISTS idx_sap_sync_runs_company_bukrs_iniciado_em
    ON sap_sync_runs (company_id, bukrs, iniciado_em DESC);
