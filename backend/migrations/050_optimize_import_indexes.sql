-- Performance indexes for aggregation queries (WHERE job_id = $1)
-- Critical for scaling to 30-40 files per company
CREATE INDEX IF NOT EXISTS idx_reg_c100_job_id ON reg_c100(job_id);
CREATE INDEX IF NOT EXISTS idx_operacoes_comerciais_job_id ON operacoes_comerciais(job_id);
CREATE INDEX IF NOT EXISTS idx_energia_agregado_job_id ON energia_agregado(job_id);
CREATE INDEX IF NOT EXISTS idx_frete_agregado_job_id ON frete_agregado(job_id);
CREATE INDEX IF NOT EXISTS idx_comunicacoes_agregado_job_id ON comunicacoes_agregado(job_id);
