-- Migration 046: Create managers table for AI report email recipients
-- Managers are linked to companies and receive automated AI reports

CREATE TABLE IF NOT EXISTS managers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    nome_completo VARCHAR(255) NOT NULL,
    cargo VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    ativo BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Index for fast lookups by company
CREATE INDEX IF NOT EXISTS idx_managers_company_id ON managers(company_id);

-- Index for email uniqueness within company (same email can be used in different companies)
CREATE UNIQUE INDEX IF NOT EXISTS idx_managers_company_email ON managers(company_id, email) WHERE ativo = true;

-- Index for active managers filter
CREATE INDEX IF NOT EXISTS idx_managers_ativo ON managers(ativo);

-- Updated_at trigger
CREATE OR REPLACE FUNCTION update_managers_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_managers_updated_at ON managers;
CREATE TRIGGER trigger_managers_updated_at
    BEFORE UPDATE ON managers
    FOR EACH ROW
    EXECUTE FUNCTION update_managers_updated_at();

COMMENT ON TABLE managers IS 'Gestores vinculados a empresas que recebem relatorios IA automatizados';
COMMENT ON COLUMN managers.company_id IS 'Empresa vinculado (FK companies)';
COMMENT ON COLUMN managers.nome_completo IS 'Nome completo do gestor';
COMMENT ON COLUMN managers.cargo IS 'Cargo/funcao (texto livre: CEO, Controller, Contador, etc.)';
COMMENT ON COLUMN managers.email IS 'E-mail para receber relatorios IA';
COMMENT ON COLUMN managers.ativo IS 'Se recebe ou nao e-mails (true = ativo)';
