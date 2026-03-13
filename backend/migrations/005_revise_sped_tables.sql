DROP TABLE IF EXISTS reg_0200;

CREATE TABLE IF NOT EXISTS reg_0140 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    cod_est VARCHAR(60),
    nome VARCHAR(100),
    cnpj VARCHAR(14),
    uf VARCHAR(2),
    ie VARCHAR(14),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reg_c010 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    cnpj VARCHAR(14),
    ind_escri VARCHAR(1),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reg_c500 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    cod_part VARCHAR(60),
    cod_mod VARCHAR(2),
    ser VARCHAR(4),
    num_doc VARCHAR(9),
    dt_doc DATE,
    dt_e_s DATE,
    vl_doc DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_pis DECIMAL(18,2),
    vl_cofins DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reg_c600 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    cod_mod VARCHAR(2),
    cod_mun VARCHAR(7),
    ser VARCHAR(4),
    sub VARCHAR(3),
    cod_cons VARCHAR(2),
    qtd_cons DECIMAL(18,4),
    dt_doc DATE,
    vl_doc DECIMAL(18,2),
    vl_pis DECIMAL(18,2),
    vl_cofins DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reg_c100 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    ind_oper VARCHAR(1),
    ind_emit VARCHAR(1),
    cod_part VARCHAR(60),
    cod_mod VARCHAR(2),
    cod_sit VARCHAR(2),
    ser VARCHAR(4),
    num_doc VARCHAR(9),
    chv_nfe VARCHAR(44),
    dt_doc DATE,
    dt_e_s DATE,
    vl_doc DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_pis DECIMAL(18,2),
    vl_cofins DECIMAL(18,2),
    vl_piscofins DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS company_name VARCHAR(255);
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS cnpj VARCHAR(14);
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS dt_ini DATE;
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS dt_fin DATE;

CREATE TABLE IF NOT EXISTS reg_d100 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    ind_oper VARCHAR(1),
    ind_emit VARCHAR(1),
    cod_part VARCHAR(60),
    cod_mod VARCHAR(2),
    cod_sit VARCHAR(2),
    ser VARCHAR(4),
    num_doc VARCHAR(9),
    chv_cte VARCHAR(44),
    dt_doc DATE,
    dt_a_p DATE,
    vl_doc DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_pis DECIMAL(18,2),
    vl_cofins DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reg_c100 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    ind_oper VARCHAR(1),
    ind_emit VARCHAR(1),
    cod_part VARCHAR(60),
    cod_mod VARCHAR(2),
    cod_sit VARCHAR(2),
    ser VARCHAR(4),
    num_doc VARCHAR(9),
    chv_nfe VARCHAR(44),
    dt_doc DATE,
    dt_e_s DATE,
    vl_doc DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_pis DECIMAL(18,2),
    vl_cofins DECIMAL(18,2),
    vl_piscofins DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS company_name VARCHAR(255);
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS cnpj VARCHAR(14);
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS dt_ini DATE;
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS dt_fin DATE;


CREATE INDEX IF NOT EXISTS idx_c500_job_id ON reg_c500(job_id);
CREATE INDEX IF NOT EXISTS idx_c600_job_id ON reg_c600(job_id);
CREATE INDEX IF NOT EXISTS idx_d100_job_id ON reg_d100(job_id);