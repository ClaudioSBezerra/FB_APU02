-- Migration 028: Create reg_d500 table for Comunicacoes (Telecom)
-- Based on SPED EFD ICMS/IPI Layout

CREATE TABLE IF NOT EXISTS reg_d500 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    filial_cnpj VARCHAR(14),
    ind_oper VARCHAR(1), -- 0: Entrada, 1: Saída
    ind_emit VARCHAR(1), -- 0: Própria, 1: Terceiros
    cod_part VARCHAR(60),
    cod_mod VARCHAR(2),
    cod_sit VARCHAR(2),
    ser VARCHAR(4),
    sub VARCHAR(3),
    num_doc VARCHAR(9),
    dt_doc DATE,
    dt_a_p DATE,
    vl_doc DECIMAL(18,2),
    vl_desc DECIMAL(18,2),
    vl_serv DECIMAL(18,2),
    vl_serv_nt DECIMAL(18,2),
    vl_terc DECIMAL(18,2),
    vl_da DECIMAL(18,2),
    vl_bc_icms DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    cod_inf VARCHAR(6),
    vl_pis DECIMAL(18,2),
    vl_cofins DECIMAL(18,2),
    cod_grp_tensao VARCHAR(2),
    vl_piscofins DECIMAL(18,2), -- Helper column for aggregations
    
    -- Projected Fields for Tax Reform (2027-2033)
    vl_icms_projetado DECIMAL(18,2),
    vl_ibs_projetado DECIMAL(18,2),
    vl_cbs_projetado DECIMAL(18,2),

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_d500_job_id ON reg_d500(job_id);
CREATE INDEX IF NOT EXISTS idx_d500_dt_doc ON reg_d500(dt_doc);
CREATE INDEX IF NOT EXISTS idx_d500_filial_cnpj ON reg_d500(filial_cnpj);
