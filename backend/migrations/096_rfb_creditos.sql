-- Adiciona coluna tipo em rfb_requests para distinguir débitos de créditos
ALTER TABLE rfb_requests ADD COLUMN IF NOT EXISTS tipo VARCHAR(20) NOT NULL DEFAULT 'debito';

-- Índice para filtrar por tipo rapidamente
CREATE INDEX IF NOT EXISTS idx_rfb_requests_tipo ON rfb_requests(company_id, tipo, created_at DESC);

-- Tabela de créditos CBS (espelha rfb_debitos para notas de entrada)
CREATE TABLE IF NOT EXISTS rfb_creditos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES rfb_requests(id) ON DELETE CASCADE,
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    tipo_apuracao VARCHAR(50),
    modelo_dfe VARCHAR(10),
    numero_dfe VARCHAR(20),
    chave_dfe VARCHAR(50),
    data_dfe_emissao DATE,
    data_apuracao VARCHAR(10),
    ni_emitente VARCHAR(20),
    ni_adquirente VARCHAR(20),
    valor_cbs_total NUMERIC(15,2) DEFAULT 0,
    valor_cbs_extinto NUMERIC(15,2) DEFAULT 0,
    valor_cbs_nao_extinto NUMERIC(15,2) DEFAULT 0,
    situacao_credito VARCHAR(100),
    formas_extincao TEXT,
    eventos TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rfb_creditos_request ON rfb_creditos(request_id);
CREATE INDEX IF NOT EXISTS idx_rfb_creditos_company_date ON rfb_creditos(company_id, data_dfe_emissao DESC NULLS LAST);
CREATE UNIQUE INDEX IF NOT EXISTS idx_rfb_creditos_chave
    ON rfb_creditos(company_id, chave_dfe)
    WHERE chave_dfe IS NOT NULL AND chave_dfe != '';

-- Tabela de resumo de créditos
CREATE TABLE IF NOT EXISTS rfb_creditos_resumo (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES rfb_requests(id) ON DELETE CASCADE,
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    data_apuracao VARCHAR(10),
    total_creditos INTEGER DEFAULT 0,
    valor_cbs_total NUMERIC(15,2) DEFAULT 0,
    valor_cbs_extinto NUMERIC(15,2) DEFAULT 0,
    valor_cbs_nao_extinto NUMERIC(15,2) DEFAULT 0,
    total_corrente INTEGER DEFAULT 0,
    total_ajuste INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (company_id, data_apuracao)
);
