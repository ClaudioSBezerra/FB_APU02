-- Migration: Fix Simples Nacional View Column Name and JOIN
-- Purpose: 
-- 1. Rename valor_contabil to total_valor to match handler expectations
-- 2. Ensure proper CNPJ sanitization in JOIN condition
-- 3. Fix missing company_id filtering

DROP MATERIALIZED VIEW IF EXISTS mv_operacoes_simples;

CREATE MATERIALIZED VIEW mv_operacoes_simples AS

-- 1. Mercadorias (C100 + C190)
SELECT 
    j.company_id,
    p.nome as fornecedor_nome,
    p.cnpj as fornecedor_cnpj,
    TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(c.dt_e_s, c.dt_doc))::INTEGER as ano,
    'C100' as origem,
    SUM(c190.vl_opr) as total_valor,        -- Changed: valor_contabil -> total_valor
    SUM(c190.vl_icms) as total_icms          -- Changed: vl_icms_origem -> total_icms
FROM reg_c190 c190
JOIN reg_c100 c ON c.id = c190.id_pai_c100
JOIN import_jobs j ON j.id = c.job_id
JOIN participants p ON p.job_id = c.job_id AND p.cod_part = c.cod_part
JOIN forn_simples fs ON fs.cnpj = REGEXP_REPLACE(p.cnpj, '[^0-9]', '', 'g')
JOIN cfop f ON c190.cfop = f.cfop
WHERE f.tipo IN ('R', 'C', 'A') -- Revenda, Consumo, Ativo
AND c.ind_oper = '0' -- Apenas Entradas
GROUP BY 1, 2, 3, 4, 5, 6

UNION ALL

-- 2. Fretes (D100)
SELECT 
    j.company_id,
    p.nome as fornecedor_nome,
    p.cnpj as fornecedor_cnpj,
    TO_CHAR(COALESCE(d.dt_a_p, d.dt_doc), 'MM/YYYY') as mes_ano,
    EXTRACT(YEAR FROM COALESCE(d.dt_a_p, d.dt_doc))::INTEGER as ano,
    'D100' as origem,
    SUM(d.vl_doc) as total_valor,            -- Changed: valor_contabil -> total_valor
    SUM(d.vl_icms) as total_icms              -- Changed: vl_icms_origem -> total_icms
FROM reg_d100 d
JOIN import_jobs j ON j.id = d.job_id
JOIN participants p ON p.job_id = d.job_id AND p.cod_part = d.cod_part
JOIN forn_simples fs ON fs.cnpj = REGEXP_REPLACE(p.cnpj, '[^0-9]', '', 'g')
WHERE d.ind_oper = '0' -- Apenas Entradas
GROUP BY 1, 2, 3, 4, 5, 6;

-- Create Indexes for Performance
CREATE INDEX idx_mv_simples_company ON mv_operacoes_simples(company_id);
CREATE INDEX idx_mv_simples_cnpj ON mv_operacoes_simples(fornecedor_cnpj);
CREATE INDEX idx_mv_simples_mes ON mv_operacoes_simples(mes_ano);

-- Unique Index for Concurrent Refresh
CREATE UNIQUE INDEX idx_mv_simples_unique 
ON mv_operacoes_simples (company_id, fornecedor_cnpj, fornecedor_nome, mes_ano, origem);

-- Refresh the view with new data
REFRESH MATERIALIZED VIEW CONCURRENTLY mv_operacoes_simples;