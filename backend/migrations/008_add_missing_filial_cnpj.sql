-- Ensure filial_cnpj column exists in all relevant tables
-- This is a safety migration to fix the "column does not exist" error

ALTER TABLE reg_c100 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);
ALTER TABLE reg_c500 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);
ALTER TABLE reg_c500 ADD COLUMN IF NOT EXISTS vl_piscofins DECIMAL(18,2);
ALTER TABLE reg_c600 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);
ALTER TABLE reg_c600 ADD COLUMN IF NOT EXISTS vl_piscofins DECIMAL(18,2);
ALTER TABLE reg_d100 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);
ALTER TABLE reg_d100 ADD COLUMN IF NOT EXISTS vl_piscofins DECIMAL(18,2);
ALTER TABLE reg_d500 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);