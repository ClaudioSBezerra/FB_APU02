-- Migration 099: adiciona coluna cfop (código 4 dígitos) a nfe_entradas
-- Permite exibir o CFOP preponderante por fornecedor e por nota na tela de Créditos em Risco.
-- Popula cruzando com reg_c190 (EFD): CFOP dominante por empresa+forn+período.

ALTER TABLE nfe_entradas
  ADD COLUMN IF NOT EXISTS cfop VARCHAR(4);

-- Popula a partir do EFD: CFOP com maior volume (vl_opr) por empresa+forn+período
UPDATE nfe_entradas ne
SET cfop = sub.cfop
FROM (
  SELECT
    company_id,
    forn_cnpj,
    mes_ano,
    cfop
  FROM (
    SELECT
      j.company_id,
      p.cnpj                                                         AS forn_cnpj,
      TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY')              AS mes_ano,
      c190.cfop,
      ROW_NUMBER() OVER (
        PARTITION BY j.company_id, p.cnpj, TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY')
        ORDER BY SUM(c190.vl_opr) DESC
      ) AS rn
    FROM reg_c190 c190
    JOIN reg_c100 c    ON c.id         = c190.id_pai_c100
    JOIN import_jobs j ON j.id         = c.job_id
    JOIN participants p ON p.job_id    = c.job_id AND p.cod_part = c.cod_part
    WHERE c190.cfop::integer < 5000
      AND c.ind_oper  = '0'
      AND j.status    = 'completed'
    GROUP BY
      j.company_id, p.cnpj,
      TO_CHAR(COALESCE(c.dt_e_s, c.dt_doc), 'MM/YYYY'),
      c190.cfop
  ) ranked
  WHERE rn = 1
) sub
WHERE ne.company_id = sub.company_id
  AND ne.forn_cnpj  = sub.forn_cnpj
  AND ne.mes_ano    = sub.mes_ano
  AND ne.cfop       IS NULL;

CREATE INDEX IF NOT EXISTS idx_nfe_entradas_cfop
  ON nfe_entradas(company_id, cfop);
