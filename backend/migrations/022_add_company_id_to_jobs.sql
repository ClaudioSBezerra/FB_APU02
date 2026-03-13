-- Add company_id to import_jobs to link data to specific company entity
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES companies(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_import_jobs_company ON import_jobs(company_id);

-- Try to backfill based on CNPJ match (best effort) if CNPJ exists
DO $$
BEGIN
    -- Only attempt backfill if BOTH tables have the cnpj column
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='import_jobs' AND column_name='cnpj') 
       AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='companies' AND column_name='cnpj') THEN
        
        UPDATE import_jobs 
        SET company_id = c.id
        FROM companies c
        WHERE import_jobs.cnpj = c.cnpj AND import_jobs.company_id IS NULL;
    END IF;
END $$;
