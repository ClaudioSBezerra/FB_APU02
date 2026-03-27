-- Migration 088: adiciona coluna cancelado nas tabelas de documentos fiscais
-- Permite importar notas canceladas e identificá-las na Malha Fina

ALTER TABLE nfe_saidas   ADD COLUMN IF NOT EXISTS cancelado TEXT NOT NULL DEFAULT 'N';
ALTER TABLE nfe_entradas ADD COLUMN IF NOT EXISTS cancelado TEXT NOT NULL DEFAULT 'N';
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS cancelado TEXT NOT NULL DEFAULT 'N';
