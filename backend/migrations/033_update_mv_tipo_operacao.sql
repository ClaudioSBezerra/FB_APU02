-- Migration: Update Materialized View with detailed TIPO_OPERACAO logic
-- Requirements:
-- 1. Detailed breakdown of C100/C190 based on CFOP Type (R, C, T, A, O) and Operation (Entrada/Saida)
-- 2. Specific mapping for C500 (Energia), C600 (Saida Energia), D100 (Frete), D500 (Comunicação)

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
    CASE 
        -- Entradas (ind_oper = '0')
        WHEN c.ind_oper = '0' THEN
            CASE 
                WHEN f.tipo = 'R' THEN 'Entrada_Revenda'
                WHEN f.tipo = 'C' THEN 'Entradas_Consumo'
                WHEN f.tipo = 'T' THEN 'Entradas_Transferencia'
                WHEN f.tipo = 'A' THEN 'Entradas_Imobilizado'
                WHEN f.tipo = 'O' THEN 'Entradas_Outros'
                ELSE 'Entradas_NaoIdent'
            END
        -- Saídas (ind_oper = '1')
        ELSE
            CASE 
                WHEN f.tipo = 'R' THEN 'Saidas_Revenda'
                WHEN f.tipo = 'C' THEN 'Saidas_Consumo'
                WHEN f.tipo = 'T' THEN 'Saidas_Transferencia'
                WHEN f.tipo = 'A' THEN 'Saidas_Imobilizado'
                WHEN f.tipo = 'O' THEN 'Saidas_Outros'
                ELSE 'Saidas_NaoIdent'
            END
    END as tipo_operacao,
    SUM(c190.vl_opr) as valor_contabil,
    SUM(c190.vl_icms) as vl_icms_origem
FROM reg_c190 c190
JOIN reg_c100 c ON c.id = c190.id_pai_c100
JOIN import_jobs j ON j.id = c.job_id
LEFT JOIN cfop f ON c190.cfop = f.cfop
GROUP BY 1, 2, 3, 4, 5, 6, 7, 8

UNION ALL

-- 2. Transporte (D100) -> Entradas_Frete
SELECT 
    j.company_name as filial_nome,
    j.cnpj as filial_cnpj,
    TO_CHAR(COALESCE(d.dt_a_p, d.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(d.dt_a_p, d.dt_doc))::INTEGER as ano,
    CASE WHEN d.ind_oper = '0' THEN 'ENTRADA' ELSE 'SAIDA' END as tipo,
    'R' as tipo_cfop,
    'D100' as origem,
    'Entradas_Frete' as tipo_operacao,
    SUM(d.vl_doc) as valor_contabil,
    SUM(d.vl_icms) as vl_icms_origem
FROM reg_d100 d
JOIN import_jobs j ON j.id = d.job_id
GROUP BY 1, 2, 3, 4, 5, 6, 7, 8

UNION ALL

-- 3. Energia/Água/Gás (C500) -> Entradas_Energia_Agua
SELECT 
    j.company_name as filial_nome,
    j.cnpj as filial_cnpj,
    TO_CHAR(COALESCE(c5.dt_e_s, c5.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(c5.dt_e_s, c5.dt_doc))::INTEGER as ano,
    'ENTRADA' as tipo,
    'C' as tipo_cfop,
    'C500' as origem,
    'Entradas_Energia_Agua' as tipo_operacao,
    SUM(c5.vl_doc) as valor_contabil,
    SUM(c5.vl_icms) as vl_icms_origem
FROM reg_c500 c5
JOIN import_jobs j ON j.id = c5.job_id
GROUP BY 1, 2, 3, 4, 5, 6, 7, 8

UNION ALL

-- 4. Comunicação (D500) -> Entradas_Comunicações
SELECT 
    j.company_name as filial_nome,
    j.cnpj as filial_cnpj,
    TO_CHAR(COALESCE(d5.dt_a_p, d5.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(d5.dt_a_p, d5.dt_doc))::INTEGER as ano,
    CASE WHEN d5.ind_oper = '0' THEN 'ENTRADA' ELSE 'SAIDA' END as tipo,
    'C' as tipo_cfop,
    'D500' as origem,
    'Entradas_Comunicações' as tipo_operacao,
    SUM(d5.vl_doc) as valor_contabil,
    SUM(d5.vl_icms) as vl_icms_origem
FROM reg_d500 d5
JOIN import_jobs j ON j.id = d5.job_id
GROUP BY 1, 2, 3, 4, 5, 6, 7, 8

UNION ALL

-- 5. Consolidação Energia (C600) -> Saidas_Energia_Agua
SELECT 
    j.company_name as filial_nome,
    j.cnpj as filial_cnpj,
    TO_CHAR(c6.dt_doc, 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM c6.dt_doc)::INTEGER as ano,
    'SAIDA' as tipo,
    'O' as tipo_cfop,
    'C600' as origem,
    'Saidas_Energia_Agua' as tipo_operacao,
    SUM(c6.vl_doc) as valor_contabil,
    0 as vl_icms_origem
FROM reg_c600 c6
JOIN import_jobs j ON j.id = c6.job_id
GROUP BY 1, 2, 3, 4, 5, 6, 7, 8;

CREATE INDEX idx_mv_mercadorias_agregada_filial ON mv_mercadorias_agregada(filial_nome);
CREATE INDEX idx_mv_mercadorias_agregada_cnpj ON mv_mercadorias_agregada(filial_cnpj);
CREATE INDEX idx_mv_mercadorias_agregada_mes ON mv_mercadorias_agregada(mes_ano);
CREATE INDEX idx_mv_mercadorias_agregada_tipo_op ON mv_mercadorias_agregada(tipo_operacao);
