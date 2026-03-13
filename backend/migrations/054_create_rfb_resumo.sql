-- Migration: Create rfb_resumo table for dashboard summary
CREATE TABLE IF NOT EXISTS rfb_resumo (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES rfb_requests(id) ON DELETE CASCADE,
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    data_apuracao VARCHAR(6),
    total_debitos INT DEFAULT 0,
    valor_cbs_total DECIMAL(18,2) DEFAULT 0,
    valor_cbs_extinto DECIMAL(18,2) DEFAULT 0,
    valor_cbs_nao_extinto DECIMAL(18,2) DEFAULT 0,
    total_corrente INT DEFAULT 0,
    total_ajuste INT DEFAULT 0,
    total_extemporaneo INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_rfb_resumo_company_periodo ON rfb_resumo(company_id, data_apuracao);
