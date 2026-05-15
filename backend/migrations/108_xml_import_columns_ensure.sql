-- Migration 108: garante colunas de importação XML (retry seguro da 107)
-- Usa IF NOT EXISTS em cada coluna individualmente para ser idempotente.
-- Necessário porque a migration 107 pode ter sido marcada como executada
-- antes de concluir no servidor de produção.

ALTER TABLE nfe_entradas ADD COLUMN IF NOT EXISTS forn_uf   TEXT;
ALTER TABLE nfe_entradas ADD COLUMN IF NOT EXISTS dest_nome TEXT;
ALTER TABLE nfe_entradas ADD COLUMN IF NOT EXISTS dest_uf   TEXT;

ALTER TABLE nfe_saidas ADD COLUMN IF NOT EXISTS emit_nome TEXT;
ALTER TABLE nfe_saidas ADD COLUMN IF NOT EXISTS emit_uf   TEXT;
ALTER TABLE nfe_saidas ADD COLUMN IF NOT EXISTS dest_uf   TEXT;

ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS emit_uf      TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS rem_cnpj_cpf TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS rem_nome     TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS rem_uf       TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS dest_nome    TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS dest_uf      TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS nat_op       TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS cfop         TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS modal        TEXT;
