-- Migration 116: Tabela de execuções de sincronização de pagamentos SAP
-- Suporte ao disparo automático pós-apuração RFB (PRD FR-1/FR-7/FR-10, epics.md Epic 2, Story 2.1).
-- Uma linha por tentativa de sincronização por (company_id, bukrs) — mesmo padrão de erp_bridge_runs.
-- Campos de contagem detalhada por paymentStatus (chaves_pago_total, chaves_pago_parcial,
-- chaves_em_aberto, chaves_nao_localizado, erro_detalhe) ficam para a Story 2.4 — não incluídos aqui.

CREATE TABLE IF NOT EXISTS sap_sync_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    bukrs           VARCHAR(4) NOT NULL,
    request_id      UUID REFERENCES rfb_requests(id) ON DELETE SET NULL,
    chaves_enviadas TEXT[] NOT NULL DEFAULT '{}',
    status          VARCHAR(20) NOT NULL DEFAULT 'em_andamento', -- em_andamento | concluido | falha
    iniciado_em     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    concluido_em    TIMESTAMPTZ,
    -- Garante idempotência no banco (AC #5): duas goroutines concorrentes para o
    -- mesmo lote nunca criam duas linhas — a segunda cai no ON CONFLICT DO NOTHING.
    UNIQUE (company_id, bukrs, request_id)
);

CREATE INDEX IF NOT EXISTS idx_sap_sync_runs_company_bukrs_request
    ON sap_sync_runs(company_id, bukrs, request_id);
