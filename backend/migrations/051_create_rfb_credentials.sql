-- Migration: Create rfb_credentials table for Receita Federal API credentials
CREATE TABLE IF NOT EXISTS rfb_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    cnpj_matriz VARCHAR(14) NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    client_secret VARCHAR(255) NOT NULL,
    ativo BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- One credential per company
CREATE UNIQUE INDEX IF NOT EXISTS idx_rfb_credentials_company_id ON rfb_credentials(company_id);
