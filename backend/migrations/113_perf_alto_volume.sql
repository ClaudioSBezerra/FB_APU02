-- Migration 113: Performance para alto volume (RFB + XMLs compra/venda)
--
-- 1. Índices de listagem/estado para rfb_requests:
--    - listagem do painel ordena por created_at filtrando por company_id
--    - scheduler/badge "travado"/verificação de requests em andamento filtram
--      por status não-terminal — índice parcial fica pequeno e sempre quente
-- 2. Autovacuum agressivo (mesmo padrão da migration 068) para tabelas de alto
--    churn que ficaram fora da 068: rfb_requests (updates de status por request),
--    rfb_creditos (bulk insert por apuração) e parceiros (upsert por documento).

CREATE INDEX IF NOT EXISTS idx_rfb_requests_company_created
    ON rfb_requests(company_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_rfb_requests_em_andamento
    ON rfb_requests(cnpj_base, status)
    WHERE status IN ('pending', 'requested', 'webhook_received', 'downloading', 'reprocessing');

ALTER TABLE rfb_requests SET (autovacuum_vacuum_scale_factor=0.01, autovacuum_analyze_scale_factor=0.005);
ALTER TABLE rfb_creditos SET (autovacuum_vacuum_scale_factor=0.01, autovacuum_analyze_scale_factor=0.005);
ALTER TABLE parceiros    SET (autovacuum_vacuum_scale_factor=0.01, autovacuum_analyze_scale_factor=0.005);
