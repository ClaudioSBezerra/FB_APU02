-- Migration 055: View materializada de compras por fornecedor (todos os fornecedores, CFOP < 5000)
-- Diferença da mv_operacoes_simples: sem filtro por Simples Nacional (forn_simples)
-- Agrega compras de TODOS os fornecedores, de todas as filiais, com CFOP de entrada (< 5000)

DROP MATERIALIZED VIEW IF EXISTS mv_compras_fornecedores;

CREATE MATERIALIZED VIEW mv_compras_fornecedores AS
SELECT
    j.company_id,
    p.nome            AS fornecedor_nome,
    p.cnpj            AS fornecedor_cnpj,
    p.cod_part,
    j.company_name    AS filial_nome,
    TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY')             AS mes_ano,
    EXTRACT(YEAR FROM COALESCE(c.dt_e_s, c.dt_doc))::INTEGER      AS ano,
    SUM(c190.vl_opr)  AS total_valor,
    SUM(c190.vl_icms) AS total_icms
FROM reg_c190 c190
JOIN reg_c100 c   ON c.id         = c190.id_pai_c100
JOIN import_jobs j ON j.id        = c.job_id
JOIN participants p ON p.job_id   = c.job_id AND p.cod_part = c.cod_part
WHERE c190.cfop::integer < 5000
  AND c.ind_oper = '0'
  AND j.status   = 'completed'
GROUP BY
    j.company_id,
    p.nome,
    p.cnpj,
    p.cod_part,
    j.company_name,
    TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY'),
    EXTRACT(YEAR FROM COALESCE(c.dt_e_s, c.dt_doc))::INTEGER;

-- Índices para performance nas consultas filtradas por empresa
CREATE INDEX idx_mv_compras_forn_company  ON mv_compras_fornecedores(company_id);
CREATE INDEX idx_mv_compras_forn_cnpj     ON mv_compras_fornecedores(fornecedor_cnpj);
CREATE INDEX idx_mv_compras_forn_mes      ON mv_compras_fornecedores(mes_ano);
CREATE INDEX idx_mv_compras_forn_nome     ON mv_compras_fornecedores(fornecedor_nome);

-- Índice único necessário para REFRESH CONCURRENTLY
CREATE UNIQUE INDEX idx_mv_compras_forn_unique
    ON mv_compras_fornecedores(company_id, fornecedor_cnpj, cod_part, filial_nome, mes_ano);
