-- 075: Índices de performance para filtros de filial e queries com mes_ano + ORDER BY data_emissao

-- Índices para o filtro de filial (dest_cnpj_cpf) nas tabelas de entrada
-- Necessários pois o campo não tinha índice e gerava full table scan
CREATE INDEX IF NOT EXISTS idx_nfe_entradas_dest_cnpj
  ON nfe_entradas(company_id, dest_cnpj_cpf);

CREATE INDEX IF NOT EXISTS idx_cte_entradas_dest_cnpj
  ON cte_entradas(company_id, dest_cnpj_cpf);

-- Índices compostos cobrindo o padrão mais comum de query:
-- WHERE company_id = $1 AND mes_ano = $2 ORDER BY data_emissao DESC
CREATE INDEX IF NOT EXISTS idx_nfe_saidas_mes_data
  ON nfe_saidas(company_id, mes_ano, data_emissao DESC);

CREATE INDEX IF NOT EXISTS idx_nfe_entradas_mes_data
  ON nfe_entradas(company_id, mes_ano, data_emissao DESC);

CREATE INDEX IF NOT EXISTS idx_cte_entradas_mes_data
  ON cte_entradas(company_id, mes_ano, data_emissao DESC);
