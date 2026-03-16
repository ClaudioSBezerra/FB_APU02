-- Migration 067: Índices trigramas GIN para busca ILIKE '%nome%'
-- Habilita buscas por nome de cliente/fornecedor com índice (sem seq scan)

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_nfe_saidas_dest_nome_trgm
ON nfe_saidas USING GIN (dest_nome gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_nfe_entradas_dest_nome_trgm
ON nfe_entradas USING GIN (dest_nome gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cte_entradas_dest_nome_trgm
ON cte_entradas USING GIN (dest_nome gin_trgm_ops);
