-- Remove CNPJ column from companies table as it is now stored in branches/filiais
ALTER TABLE companies DROP COLUMN IF EXISTS cnpj;
