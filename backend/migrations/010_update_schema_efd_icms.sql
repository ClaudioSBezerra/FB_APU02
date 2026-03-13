-- Migration 010: Update schema for EFD ICMS and create aggregation tables

-- Create reg_c190 table (Child of reg_c100)
CREATE TABLE IF NOT EXISTS reg_c190 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    id_pai_c100 UUID NOT NULL REFERENCES reg_c100(id) ON DELETE CASCADE,
    cfop VARCHAR(4),
    vl_opr DECIMAL(18,2),
    vl_bc_icms DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_bc_icms_st DECIMAL(18,2),
    vl_icms_st DECIMAL(18,2),
    vl_red_bc DECIMAL(18,2),
    vl_ipi DECIMAL(18,2),
    cod_obs VARCHAR(6),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create operacoes_comerciais table (Aggregation)
CREATE TABLE IF NOT EXISTS operacoes_comerciais (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    filial_cnpj VARCHAR(14),
    cod_part VARCHAR(60),
    mes_ano VARCHAR(7), -- MM/YYYY
    ind_oper CHAR(1),
    vl_doc DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_icms_projetado DECIMAL(18,2),
    vl_piscofins DECIMAL(18,2),
    vl_ibs_projetado DECIMAL(18,2),
    vl_cbs_projetado DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create energia_agregado table (Aggregation for C500/C600)
CREATE TABLE IF NOT EXISTS energia_agregado (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    filial_cnpj VARCHAR(14),
    cod_part VARCHAR(60),
    mes_ano VARCHAR(7),
    ind_oper CHAR(1), -- 0=Entrada (C500), 1=Saida (C600)
    vl_doc DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_icms_projetado DECIMAL(18,2),
    vl_piscofins DECIMAL(18,2),
    vl_ibs_projetado DECIMAL(18,2),
    vl_cbs_projetado DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create frete_agregado table (Aggregation for D100)
CREATE TABLE IF NOT EXISTS frete_agregado (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    filial_cnpj VARCHAR(14),
    cod_part VARCHAR(60),
    mes_ano VARCHAR(7),
    ind_oper CHAR(1),
    vl_doc DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_icms_projetado DECIMAL(18,2),
    vl_ibs_projetado DECIMAL(18,2),
    vl_cbs_projetado DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create comunicacoes_agregado table (Aggregation for D500)
CREATE TABLE IF NOT EXISTS comunicacoes_agregado (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    filial_cnpj VARCHAR(14),
    cod_part VARCHAR(60),
    mes_ano VARCHAR(7),
    ind_oper CHAR(1),
    vl_doc DECIMAL(18,2),
    vl_icms DECIMAL(18,2),
    vl_icms_projetado DECIMAL(18,2),
    vl_ibs_projetado DECIMAL(18,2),
    vl_cbs_projetado DECIMAL(18,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);