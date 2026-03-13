-- Migration 061: adiciona filial_cnpj à mv_operacoes_simples
-- Permite filtrar operações Simples Nacional por filial na UI.

DROP MATERIALIZED VIEW IF EXISTS mv_operacoes_simples;

CREATE MATERIALIZED VIEW mv_operacoes_simples AS

-- C100: notas fiscais de mercadorias (entrada)
SELECT
    j.company_id,
    j.cnpj                                                              AS filial_cnpj,
    p.nome                                                              AS fornecedor_nome,
    p.cnpj                                                              AS fornecedor_cnpj,
    to_char(COALESCE(c.dt_e_s, c.dt_doc)::timestamptz, 'MM/YYYY')      AS mes_ano,
    EXTRACT(YEAR FROM COALESCE(c.dt_e_s, c.dt_doc))::integer            AS ano,
    'C100'                                                              AS origem,
    SUM(c190.vl_opr)                                                    AS total_valor,
    SUM(c190.vl_icms)                                                   AS total_icms
FROM reg_c190 c190
JOIN reg_c100 c  ON c.id = c190.id_pai_c100
JOIN import_jobs j  ON j.id = c.job_id
JOIN participants p  ON p.job_id = c.job_id AND p.cod_part = c.cod_part
JOIN forn_simples fs ON fs.cnpj = REGEXP_REPLACE(p.cnpj, '[^0-9]', '', 'g')
JOIN cfop f          ON c190.cfop = f.cfop
WHERE f.tipo IN ('R', 'C', 'A')
  AND c.ind_oper = '0'
GROUP BY
    j.company_id, j.cnpj, p.nome, p.cnpj,
    to_char(COALESCE(c.dt_e_s, c.dt_doc)::timestamptz, 'MM/YYYY'),
    EXTRACT(YEAR FROM COALESCE(c.dt_e_s, c.dt_doc))::integer

UNION ALL

-- D100: fretes / transportes (entrada)
SELECT
    j.company_id,
    j.cnpj                                                              AS filial_cnpj,
    p.nome                                                              AS fornecedor_nome,
    p.cnpj                                                              AS fornecedor_cnpj,
    to_char(COALESCE(d.dt_a_p, d.dt_doc)::timestamptz, 'MM/YYYY')      AS mes_ano,
    EXTRACT(YEAR FROM COALESCE(d.dt_a_p, d.dt_doc))::integer            AS ano,
    'D100'                                                              AS origem,
    SUM(d.vl_doc)                                                       AS total_valor,
    SUM(d.vl_icms)                                                      AS total_icms
FROM reg_d100 d
JOIN import_jobs j  ON j.id = d.job_id
JOIN participants p  ON p.job_id = d.job_id AND p.cod_part = d.cod_part
JOIN forn_simples fs ON fs.cnpj = REGEXP_REPLACE(p.cnpj, '[^0-9]', '', 'g')
WHERE d.ind_oper = '0'
GROUP BY
    j.company_id, j.cnpj, p.nome, p.cnpj,
    to_char(COALESCE(d.dt_a_p, d.dt_doc)::timestamptz, 'MM/YYYY'),
    EXTRACT(YEAR FROM COALESCE(d.dt_a_p, d.dt_doc))::integer;

-- Índices
CREATE INDEX idx_mv_simples_company ON mv_operacoes_simples(company_id);
CREATE INDEX idx_mv_simples_cnpj    ON mv_operacoes_simples(fornecedor_cnpj);
CREATE INDEX idx_mv_simples_mes     ON mv_operacoes_simples(mes_ano);
CREATE INDEX idx_mv_simples_filial  ON mv_operacoes_simples(filial_cnpj);

CREATE UNIQUE INDEX idx_mv_simples_unique
    ON mv_operacoes_simples(company_id, filial_cnpj, fornecedor_cnpj, fornecedor_nome, mes_ano, origem);
