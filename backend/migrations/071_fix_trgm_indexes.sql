-- Migration 071: Recria índices trgm sem CONCURRENTLY (não pode rodar dentro de transaction block)
-- Fix: migration 067 falhou pois CREATE INDEX CONCURRENTLY não pode rodar em transação

CREATE EXTENSION IF NOT EXISTS pg_trgm;

DROP INDEX IF EXISTS idx_nfe_saidas_dest_nome_trgm;
DROP INDEX IF EXISTS idx_nfe_entradas_dest_nome_trgm;
DROP INDEX IF EXISTS idx_cte_entradas_dest_nome_trgm;

CREATE INDEX IF NOT EXISTS idx_nfe_saidas_dest_nome_trgm
ON nfe_saidas USING GIN (dest_nome gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_nfe_entradas_dest_nome_trgm
ON nfe_entradas USING GIN (dest_nome gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_cte_entradas_dest_nome_trgm
ON cte_entradas USING GIN (dest_nome gin_trgm_ops);
