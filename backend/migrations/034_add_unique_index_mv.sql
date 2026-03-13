-- Migration: Add Unique Index to Materialized View to support CONCURRENT REFRESH
-- This is required to avoid "cannot refresh materialized view concurrently" errors
-- The unique index must cover all columns used in the GROUP BY clauses of the view.

-- Drop index if exists to avoid conflicts during re-runs
DROP INDEX IF EXISTS idx_mv_unique_concurrent;

-- Create Unique Index
-- Columns: filial_nome, filial_cnpj, mes_ano, ano, tipo, tipo_cfop, origem, tipo_operacao
-- These are the grouping keys in 033_update_mv_tipo_operacao.sql
CREATE UNIQUE INDEX idx_mv_unique_concurrent 
ON mv_mercadorias_agregada (filial_nome, filial_cnpj, mes_ano, ano, tipo, tipo_cfop, origem, tipo_operacao);
