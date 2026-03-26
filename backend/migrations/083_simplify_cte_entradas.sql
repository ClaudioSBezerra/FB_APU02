-- Migration 083: simplifica cte_entradas para SAP S4/HANA
-- Remove campos de prestação (exceto v_prest), ICMS, remetente, nomes/UF.
-- Adiciona data_autorizacao, v_ibs_uf, v_ibs_mun (SAP tem granularidade estadual/municipal).
-- Normaliza IBS/CBS para NOT NULL DEFAULT 0.

ALTER TABLE cte_entradas
  ADD COLUMN IF NOT EXISTS data_autorizacao DATE,
  ADD COLUMN IF NOT EXISTS v_ibs_uf  NUMERIC(15,2) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS v_ibs_mun NUMERIC(15,2) NOT NULL DEFAULT 0;

ALTER TABLE cte_entradas
  DROP COLUMN IF EXISTS nat_op,
  DROP COLUMN IF EXISTS cfop,
  DROP COLUMN IF EXISTS modal,
  DROP COLUMN IF EXISTS emit_nome,
  DROP COLUMN IF EXISTS emit_uf,
  DROP COLUMN IF EXISTS rem_cnpj_cpf,
  DROP COLUMN IF EXISTS rem_nome,
  DROP COLUMN IF EXISTS rem_uf,
  DROP COLUMN IF EXISTS dest_nome,
  DROP COLUMN IF EXISTS dest_uf,
  DROP COLUMN IF EXISTS v_rec,
  DROP COLUMN IF EXISTS v_carga,
  DROP COLUMN IF EXISTS v_bc_icms,
  DROP COLUMN IF EXISTS v_icms;

-- Normalizar IBS/CBS: nullable → NOT NULL DEFAULT 0
UPDATE cte_entradas SET
  v_bc_ibs_cbs = COALESCE(v_bc_ibs_cbs, 0),
  v_ibs        = COALESCE(v_ibs, 0),
  v_cbs        = COALESCE(v_cbs, 0);

ALTER TABLE cte_entradas
  ALTER COLUMN v_bc_ibs_cbs SET NOT NULL,
  ALTER COLUMN v_bc_ibs_cbs SET DEFAULT 0,
  ALTER COLUMN v_ibs         SET NOT NULL,
  ALTER COLUMN v_ibs         SET DEFAULT 0,
  ALTER COLUMN v_cbs         SET NOT NULL,
  ALTER COLUMN v_cbs         SET DEFAULT 0;
