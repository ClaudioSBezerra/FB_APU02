-- Migration 014: Make CNPJ optional in companies table
-- Reason: User requested that CNPJ is not mandatory, only company name.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='companies' AND column_name='cnpj') THEN
        ALTER TABLE companies ALTER COLUMN cnpj DROP NOT NULL;
        ALTER TABLE companies DROP CONSTRAINT IF EXISTS companies_cnpj_key;
    END IF;
END $$;
