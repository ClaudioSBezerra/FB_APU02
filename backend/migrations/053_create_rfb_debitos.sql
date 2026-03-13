-- Migration: Create rfb_debitos table for normalized CBS debit data
CREATE TABLE IF NOT EXISTS rfb_debitos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES rfb_requests(id) ON DELETE CASCADE,
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    tipo_apuracao VARCHAR(30) NOT NULL,
    modelo_dfe VARCHAR(10),
    numero_dfe VARCHAR(50),
    chave_dfe VARCHAR(50),
    data_dfe_emissao TIMESTAMP WITH TIME ZONE,
    data_apuracao VARCHAR(6),
    ni_emitente VARCHAR(14),
    ni_adquirente VARCHAR(14),
    valor_cbs_total DECIMAL(18,2),
    valor_cbs_extinto DECIMAL(18,2),
    valor_cbs_nao_extinto DECIMAL(18,2),
    situacao_debito VARCHAR(50),
    formas_extincao JSONB,
    eventos JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_rfb_debitos_request ON rfb_debitos(request_id);
CREATE INDEX IF NOT EXISTS idx_rfb_debitos_company ON rfb_debitos(company_id);
CREATE INDEX IF NOT EXISTS idx_rfb_debitos_apuracao ON rfb_debitos(company_id, data_apuracao);
