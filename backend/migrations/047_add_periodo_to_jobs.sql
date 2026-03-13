-- Add mes_ano (periodo) to import_jobs for AI report generation
-- Format: MM/YYYY (e.g., "01/2026" for January 2026)
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS mes_ano VARCHAR(7);

-- Index for faster queries by period
CREATE INDEX IF NOT EXISTS idx_import_jobs_mes_ano ON import_jobs(mes_ano);
