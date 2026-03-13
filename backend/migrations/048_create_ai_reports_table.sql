-- Migration 048: Create ai_reports table for storing AI-generated executive summaries
-- These reports are generated automatically after SPED imports and manually via UI

CREATE TABLE IF NOT EXISTS ai_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    job_id UUID REFERENCES import_jobs(id) ON DELETE SET NULL,
    periodo VARCHAR(7) NOT NULL, -- Format: MM/YYYY
    titulo TEXT NOT NULL,
    resumo TEXT NOT NULL,
    dados_brutos JSONB,
    gerado_automaticamente BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Index for fast lookups by company
CREATE INDEX IF NOT EXISTS idx_ai_reports_company_id ON ai_reports(company_id);

-- Index for filtering by company and period (most common query)
CREATE INDEX IF NOT EXISTS idx_ai_reports_company_period ON ai_reports(company_id, periodo);

-- Index for filtering by automatic/manual
CREATE INDEX IF NOT EXISTS idx_ai_reports_automatic ON ai_reports(gerado_automaticamente);

-- Updated_at trigger
CREATE OR REPLACE FUNCTION update_ai_reports_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_ai_reports_updated_at ON ai_reports;
CREATE TRIGGER trigger_ai_reports_updated_at
    BEFORE UPDATE ON ai_reports
    FOR EACH ROW
    EXECUTE FUNCTION update_ai_reports_updated_at();

COMMENT ON TABLE ai_reports IS 'Relatorios executivos gerados por IA (Claude) para analise fiscal de empresas';
COMMENT ON COLUMN ai_reports.company_id IS 'Empresa vinculada (FK companies)';
COMMENT ON COLUMN ai_reports.job_id IS 'Job de importacao que originou o relatorio (FK import_jobs)';
COMMENT ON COLUMN ai_reports.periodo IS 'Periodo analisado no formato MM/YYYY';
COMMENT ON COLUMN ai_reports.titulo IS 'Titulo do relatorio gerado pela IA';
COMMENT ON COLUMN ai_reports.resumo IS 'Conteudo do relatorio em Markdown gerado pela IA';
COMMENT ON COLUMN ai_reports.dados_brutos IS 'Dados fiscais agregados em JSON usados para geracao do relatorio';
COMMENT ON COLUMN ai_reports.gerado_automaticamente IS 'Se foi gerado automaticamente apos importacao (true) ou manualmente pelo usuario (false)';
