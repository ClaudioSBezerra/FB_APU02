-- Migration 081: simplifica nfe_saidas para SAP S4/HANA
-- Remove ICMSTot completo (desnecessário no SAP), adiciona data_autorizacao.
-- Normaliza IBS/CBS para NOT NULL DEFAULT 0.

ALTER TABLE nfe_saidas
  ADD COLUMN IF NOT EXISTS data_autorizacao DATE;

ALTER TABLE nfe_saidas
  DROP COLUMN IF EXISTS nat_op,
  DROP COLUMN IF EXISTS emit_nome,
  DROP COLUMN IF EXISTS emit_uf,
  DROP COLUMN IF EXISTS emit_municipio,
  DROP COLUMN IF EXISTS dest_nome,
  DROP COLUMN IF EXISTS dest_uf,
  DROP COLUMN IF EXISTS dest_c_mun,
  DROP COLUMN IF EXISTS v_bc,
  DROP COLUMN IF EXISTS v_icms,
  DROP COLUMN IF EXISTS v_icms_deson,
  DROP COLUMN IF EXISTS v_fcp,
  DROP COLUMN IF EXISTS v_bc_st,
  DROP COLUMN IF EXISTS v_st,
  DROP COLUMN IF EXISTS v_fcp_st,
  DROP COLUMN IF EXISTS v_fcp_st_ret,
  DROP COLUMN IF EXISTS v_prod,
  DROP COLUMN IF EXISTS v_frete,
  DROP COLUMN IF EXISTS v_seg,
  DROP COLUMN IF EXISTS v_desc,
  DROP COLUMN IF EXISTS v_ii,
  DROP COLUMN IF EXISTS v_ipi,
  DROP COLUMN IF EXISTS v_ipi_devol,
  DROP COLUMN IF EXISTS v_pis,
  DROP COLUMN IF EXISTS v_cofins,
  DROP COLUMN IF EXISTS v_outro,
  DROP COLUMN IF EXISTS v_cred_pres_ibs,
  DROP COLUMN IF EXISTS v_cred_pres_cbs;

-- Normalizar IBS/CBS: nullable → NOT NULL DEFAULT 0
UPDATE nfe_saidas SET
  v_bc_ibs_cbs = COALESCE(v_bc_ibs_cbs, 0),
  v_ibs_uf     = COALESCE(v_ibs_uf, 0),
  v_ibs_mun    = COALESCE(v_ibs_mun, 0),
  v_ibs        = COALESCE(v_ibs, 0),
  v_cbs        = COALESCE(v_cbs, 0);

ALTER TABLE nfe_saidas
  ALTER COLUMN v_bc_ibs_cbs SET DEFAULT 0,
  ALTER COLUMN v_ibs_uf      SET DEFAULT 0,
  ALTER COLUMN v_ibs_mun     SET DEFAULT 0,
  ALTER COLUMN v_ibs         SET DEFAULT 0,
  ALTER COLUMN v_cbs         SET DEFAULT 0;

ALTER TABLE nfe_saidas
  ALTER COLUMN v_bc_ibs_cbs SET NOT NULL,
  ALTER COLUMN v_ibs_uf      SET NOT NULL,
  ALTER COLUMN v_ibs_mun     SET NOT NULL,
  ALTER COLUMN v_ibs         SET NOT NULL,
  ALTER COLUMN v_cbs         SET NOT NULL;
