-- Migration 082: simplifica nfe_entradas para SAP S4/HANA
-- Remove ICMSTot completo, adiciona data_autorizacao.

ALTER TABLE nfe_entradas
  ADD COLUMN IF NOT EXISTS data_autorizacao DATE;

ALTER TABLE nfe_entradas
  DROP COLUMN IF EXISTS nat_op,
  DROP COLUMN IF EXISTS forn_nome,
  DROP COLUMN IF EXISTS forn_uf,
  DROP COLUMN IF EXISTS forn_municipio,
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
