-- Migration: Create rfb_requests table for tracking RFB API requests
CREATE TABLE IF NOT EXISTS rfb_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    cnpj_base VARCHAR(8) NOT NULL,
    tiquete VARCHAR(255),
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    ambiente VARCHAR(20) NOT NULL DEFAULT 'producao',
    error_code VARCHAR(50),
    error_message TEXT,
    raw_json JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_rfb_requests_company ON rfb_requests(company_id);
CREATE INDEX IF NOT EXISTS idx_rfb_requests_tiquete ON rfb_requests(tiquete);
