-- Migration: Update Materialized View to include Origem and better CFOP Types
-- User Requirement: 
-- 1. Identify "R de Entrada do bloco C100/C190" (Revenda) vs "R de Entradas Frete" (D100)
-- 2. Identify "C Entradas Consumo" (C500, D500, C100/C)
-- 3. Identify "A Entradas Ativo" (C100/A)

DROP MATERIALIZED VIEW IF EXISTS mv_mercadorias_agregada;

CREATE MATERIALIZED VIEW mv_mercadorias_agregada AS

-- 1. Mercadorias (C100 + C190)
SELECT 
    j.company_name as filial_nome,
    j.cnpj as filial_cnpj,
    TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(c.dt_e_s, c.dt_doc))::INTEGER as ano,
    CASE WHEN c.ind_oper = '0' THEN 'ENTRADA' ELSE 'SAIDA' END as tipo,
    COALESCE(f.tipo, 'O') as tipo_cfop,
    'C100' as origem,
    SUM(c190.vl_opr) as valor_contabil,
    SUM(c190.vl_icms) as vl_icms_origem
FROM reg_c190 c190
JOIN reg_c100 c ON c.id = c190.id_pai_c100
JOIN import_jobs j ON j.id = c.job_id
LEFT JOIN cfop f ON c190.cfop = f.cfop
GROUP BY 1, 2, 3, 4, 5, 6, 7

UNION ALL

-- 2. Transporte (D100) -> Mapped to 'R' (Frete/Revenda logic) per user request
SELECT 
    j.company_name as filial_nome,
    j.cnpj as filial_cnpj,
    TO_CHAR(COALESCE(d.dt_a_p, d.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(d.dt_a_p, d.dt_doc))::INTEGER as ano,
    CASE WHEN d.ind_oper = '0' THEN 'ENTRADA' ELSE 'SAIDA' END as tipo,
    'R' as tipo_cfop, -- Forced to 'R' for Frete identification
    'D100' as origem,
    SUM(d.vl_doc) as valor_contabil,
    SUM(d.vl_icms) as vl_icms_origem
FROM reg_d100 d
JOIN import_jobs j ON j.id = d.job_id
GROUP BY 1, 2, 3, 4, 5, 6, 7

UNION ALL

-- 3. Energia/Água/Gás (C500) -> Mapped to 'C' (Consumo)
SELECT 
    j.company_name as filial_nome,
    j.cnpj as filial_cnpj,
    TO_CHAR(COALESCE(c5.dt_e_s, c5.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(c5.dt_e_s, c5.dt_doc))::INTEGER as ano,
    'ENTRADA' as tipo,
    'C' as tipo_cfop, -- Forced to 'C' for Consumo
    'C500' as origem,
    SUM(c5.vl_doc) as valor_contabil,
    SUM(c5.vl_icms) as vl_icms_origem
FROM reg_c500 c5
JOIN import_jobs j ON j.id = c5.job_id
GROUP BY 1, 2, 3, 4, 5, 6, 7

UNION ALL

-- 4. Comunicação (D500) -> Mapped to 'C' (Consumo)
SELECT 
    j.company_name as filial_nome,
    j.cnpj as filial_cnpj,
    TO_CHAR(COALESCE(d5.dt_a_p, d5.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(d5.dt_a_p, d5.dt_doc))::INTEGER as ano,
    CASE WHEN d5.ind_oper = '0' THEN 'ENTRADA' ELSE 'SAIDA' END as tipo,
    'C' as tipo_cfop, -- Forced to 'C' for Consumo
    'D500' as origem,
    SUM(d5.vl_doc) as valor_contabil,
    SUM(d5.vl_icms) as vl_icms_origem
FROM reg_d500 d5
JOIN import_jobs j ON j.id = d5.job_id
GROUP BY 1, 2, 3, 4, 5, 6, 7;

CREATE INDEX idx_mv_mercadorias_agregada_filial ON mv_mercadorias_agregada(filial_nome);
CREATE INDEX idx_mv_mercadorias_agregada_cnpj ON mv_mercadorias_agregada(filial_cnpj);
CREATE INDEX idx_mv_mercadorias_agregada_mes ON mv_mercadorias_agregada(mes_ano);
CREATE INDEX idx_mv_mercadorias_agregada_origem ON mv_mercadorias_agregada(origem);
