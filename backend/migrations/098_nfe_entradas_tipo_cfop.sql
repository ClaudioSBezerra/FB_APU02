-- Migration 098: adiciona tipo_cfop a nfe_entradas para filtrar créditos em risco
-- Popula cruzando com mv_compras_fornecedores (EFD/SPED) quando disponível.
-- Notas sem correspondência EFD recebem default 'C' (Consumo — elegíveis para crédito).

ALTER TABLE nfe_entradas
  ADD COLUMN IF NOT EXISTS tipo_cfop VARCHAR(1);

-- Popula a partir do EFD: tipo_cfop dominante (maior total_valor) por empresa+forn+período
UPDATE nfe_entradas ne
SET tipo_cfop = sub.tipo_cfop
FROM (
  SELECT company_id, fornecedor_cnpj, mes_ano, tipo_cfop
  FROM (
    SELECT
      company_id,
      fornecedor_cnpj,
      mes_ano,
      tipo_cfop,
      ROW_NUMBER() OVER (
        PARTITION BY company_id, fornecedor_cnpj, mes_ano
        ORDER BY SUM(total_valor) DESC
      ) AS rn
    FROM mv_compras_fornecedores
    GROUP BY company_id, fornecedor_cnpj, mes_ano, tipo_cfop
  ) ranked
  WHERE rn = 1
) sub
WHERE ne.company_id = sub.company_id
  AND ne.forn_cnpj  = sub.fornecedor_cnpj
  AND ne.mes_ano    = sub.mes_ano
  AND ne.tipo_cfop  IS NULL;

-- Notas sem correspondência EFD → default 'C' (Consumo)
UPDATE nfe_entradas
SET tipo_cfop = 'C'
WHERE tipo_cfop IS NULL;

-- Torna NOT NULL com default para novos registros
ALTER TABLE nfe_entradas
  ALTER COLUMN tipo_cfop SET NOT NULL,
  ALTER COLUMN tipo_cfop SET DEFAULT 'C';

CREATE INDEX IF NOT EXISTS idx_nfe_entradas_tipo_cfop
  ON nfe_entradas(company_id, tipo_cfop);
