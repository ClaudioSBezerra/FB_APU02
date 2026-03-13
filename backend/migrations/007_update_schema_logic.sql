-- Create table for Comunicacoes (Telecom)
CREATE TABLE IF NOT EXISTS reg_d500 (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id UUID REFERENCES import_jobs(id) ON DELETE CASCADE,
  filial_cnpj VARCHAR(14),
  ind_oper VARCHAR(1),
  ind_emit VARCHAR(1),
  cod_part VARCHAR(60),
  cod_mod VARCHAR(2),
  cod_sit VARCHAR(2),
  ser VARCHAR(4),
  sub VARCHAR(3),
  num_doc VARCHAR(9),
  dt_doc DATE,
  dt_a_p DATE,
  vl_doc DECIMAL(18,2),
  vl_icms DECIMAL(18,2),
  vl_pis DECIMAL(18,2),
  vl_cofins DECIMAL(18,2),
  vl_piscofins DECIMAL(18,2),
  vl_icms_projetado DECIMAL(18,2),
  vl_ibs_projetado DECIMAL(18,2),
  vl_cbs_projetado DECIMAL(18,2)
);

-- Add projected columns to existing tables if they don't exist
ALTER TABLE reg_c100 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);
ALTER TABLE reg_c100 ADD COLUMN IF NOT EXISTS vl_icms_projetado DECIMAL(18,2);
ALTER TABLE reg_c100 ADD COLUMN IF NOT EXISTS vl_ibs_projetado DECIMAL(18,2);
ALTER TABLE reg_c100 ADD COLUMN IF NOT EXISTS vl_cbs_projetado DECIMAL(18,2);

ALTER TABLE reg_c500 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);
ALTER TABLE reg_c500 ADD COLUMN IF NOT EXISTS vl_icms_projetado DECIMAL(18,2);
ALTER TABLE reg_c500 ADD COLUMN IF NOT EXISTS vl_ibs_projetado DECIMAL(18,2);
ALTER TABLE reg_c500 ADD COLUMN IF NOT EXISTS vl_cbs_projetado DECIMAL(18,2);

ALTER TABLE reg_c600 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);
ALTER TABLE reg_c600 ADD COLUMN IF NOT EXISTS vl_icms_projetado DECIMAL(18,2);
ALTER TABLE reg_c600 ADD COLUMN IF NOT EXISTS vl_ibs_projetado DECIMAL(18,2);
ALTER TABLE reg_c600 ADD COLUMN IF NOT EXISTS vl_cbs_projetado DECIMAL(18,2);

ALTER TABLE reg_d100 ADD COLUMN IF NOT EXISTS filial_cnpj VARCHAR(14);
ALTER TABLE reg_d100 ADD COLUMN IF NOT EXISTS vl_icms_projetado DECIMAL(18,2);
ALTER TABLE reg_d100 ADD COLUMN IF NOT EXISTS vl_ibs_projetado DECIMAL(18,2);
ALTER TABLE reg_d100 ADD COLUMN IF NOT EXISTS vl_cbs_projetado DECIMAL(18,2);

-- Ensure tabela_aliquotas exists
CREATE TABLE IF NOT EXISTS tabela_aliquotas (
  ano INT PRIMARY KEY,
  perc_ibs_uf DECIMAL(5,2),
  perc_ibs_mun DECIMAL(5,2),
  perc_cbs DECIMAL(5,2),
  perc_reduc_icms DECIMAL(5,2),
  perc_reduc_piscofins DECIMAL(5,2)
);

-- Insert default data if empty
INSERT INTO tabela_aliquotas (ano, perc_ibs_uf, perc_ibs_mun, perc_cbs, perc_reduc_icms, perc_reduc_piscofins)
VALUES 
(2027, 0.05, 0.05, 8.80, 0.00, 100.00),
(2028, 0.05, 0.05, 8.80, 0.00, 100.00),
(2029, 5.20, 5.00, 8.80, 20.00, 100.00),
(2030, 10.40, 5.00, 8.80, 40.00, 100.00),
(2031, 15.60, 5.00, 8.80, 60.00, 100.00),
(2032, 20.80, 5.00, 8.80, 80.00, 100.00),
(2033, 26.00, 5.00, 8.80, 100.00, 100.00)
ON CONFLICT (ano) DO NOTHING;