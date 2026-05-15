-- Migration 109: cobertura completa de todas as colunas usadas pelo CteEntradasListHandler
-- Garante que colunas adicionadas em migrations anteriores (083, 086, 088, 104, 107, 108)
-- existam, independente do estado do banco.

-- Colunas adicionadas pela 083 (SAP granularidade IBS)
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS data_autorizacao DATE;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS v_ibs_uf  NUMERIC(15,2) NOT NULL DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS v_ibs_mun NUMERIC(15,2) NOT NULL DEFAULT 0;

-- Coluna re-adicionada pela 086 (nome do emitente)
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS emit_nome TEXT;

-- Coluna adicionada pela 088 (flag cancelamento)
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS cancelado TEXT NOT NULL DEFAULT 'N';

-- Colunas adicionadas pela 104 (ICMS, PIS, COFINS detalhados)
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS base_icms     NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS icms          NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS icms_st       NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS ipi           NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS base_pis      NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS pis           NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS base_cofins   NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS cofins        NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS base_partilha NUMERIC(15,2) DEFAULT 0;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS icms_partilha NUMERIC(15,2) DEFAULT 0;

-- Colunas re-adicionadas pelas 107/108 (dados ricos do XML)
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS emit_uf      TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS rem_cnpj_cpf TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS rem_nome     TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS rem_uf       TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS dest_nome    TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS dest_uf      TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS nat_op       TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS cfop         TEXT;
ALTER TABLE cte_entradas ADD COLUMN IF NOT EXISTS modal        TEXT;
