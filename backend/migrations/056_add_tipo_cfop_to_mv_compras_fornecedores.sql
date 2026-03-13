-- Migration 056: Adiciona tipo_cfop à mv_compras_fornecedores
-- Recria a view com JOIN na tabela cfop para expor o tipo do CFOP (R, C, A, T, O, D)
-- Necessário para filtrar por tipo de operação (ex: apenas Revenda tipo='R')

DROP MATERIALIZED VIEW IF EXISTS mv_compras_fornecedores;

CREATE MATERIALIZED VIEW mv_compras_fornecedores AS
SELECT
    j.company_id,
    p.nome                              AS fornecedor_nome,
    p.cnpj                              AS fornecedor_cnpj,
    p.cod_part,
    j.company_name                      AS filial_nome,
    COALESCE(f.tipo, 'O')               AS tipo_cfop,  -- R=Revenda,C=Consumo,A=Ativo,T=Transferência,O=Operacional,D=Devolução
    TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY')             AS mes_ano,
    EXTRACT(YEAR FROM COALESCE(c.dt_e_s, c.dt_doc))::INTEGER      AS ano,
    SUM(c190.vl_opr)                    AS total_valor,
    SUM(c190.vl_icms)                   AS total_icms
FROM reg_c190 c190
JOIN reg_c100 c    ON c.id           = c190.id_pai_c100
JOIN import_jobs j ON j.id           = c.job_id
JOIN participants p ON p.job_id      = c.job_id AND p.cod_part = c.cod_part
LEFT JOIN cfop f   ON f.cfop         = c190.cfop
WHERE c190.cfop::integer < 5000
  AND c.ind_oper  = '0'
  AND j.status    = 'completed'
GROUP BY
    j.company_id,
    p.nome,
    p.cnpj,
    p.cod_part,
    j.company_name,
    COALESCE(f.tipo, 'O'),
    TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY'),
    EXTRACT(YEAR FROM COALESCE(c.dt_e_s, c.dt_doc))::INTEGER;

-- Índices para performance
CREATE INDEX idx_mv_compras_forn_company  ON mv_compras_fornecedores(company_id);
CREATE INDEX idx_mv_compras_forn_cnpj     ON mv_compras_fornecedores(fornecedor_cnpj);
CREATE INDEX idx_mv_compras_forn_mes      ON mv_compras_fornecedores(mes_ano);
CREATE INDEX idx_mv_compras_forn_nome     ON mv_compras_fornecedores(fornecedor_nome);
CREATE INDEX idx_mv_compras_forn_tipo     ON mv_compras_fornecedores(tipo_cfop);

-- Índice único necessário para REFRESH CONCURRENTLY
CREATE UNIQUE INDEX idx_mv_compras_forn_unique
    ON mv_compras_fornecedores(company_id, fornecedor_cnpj, cod_part, filial_nome, tipo_cfop, mes_ano);
