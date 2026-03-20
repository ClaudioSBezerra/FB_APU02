-- Migration 073: Recriar mv_malha_fina_resumo (fallback se 072 foi gravada sem executar)
-- A migration 072 pode ter sido marcada como executada mesmo sem criar a view,
-- devido a um bug no migration runner que gravava schema_migrations mesmo em caso de falha.
-- Esta migration derruba e recria a MV de forma segura.

DROP MATERIALIZED VIEW IF EXISTS mv_malha_fina_resumo CASCADE;

CREATE MATERIALIZED VIEW mv_malha_fina_resumo AS

SELECT
  rd.company_id,
  'nfe-saidas'::TEXT           AS tipo,
  COALESCE(rd.ni_emitente, '') AS ni_emitente,
  rd.data_dfe_emissao::DATE    AS data_emissao,
  COUNT(*)                     AS quantidade,
  COALESCE(SUM(rd.valor_cbs_nao_extinto), 0) AS valor_cbs_nao_extinto
FROM rfb_debitos rd
WHERE rd.modelo_dfe IN ('55','65')
  AND rd.chave_dfe != ''
  AND NOT EXISTS (
    SELECT 1 FROM nfe_saidas ns
    WHERE ns.company_id = rd.company_id AND ns.chave_nfe = rd.chave_dfe
  )
GROUP BY rd.company_id, rd.ni_emitente, rd.data_dfe_emissao::DATE

UNION ALL

SELECT
  rd.company_id,
  'nfe-entradas'::TEXT         AS tipo,
  COALESCE(rd.ni_emitente, '') AS ni_emitente,
  rd.data_dfe_emissao::DATE    AS data_emissao,
  COUNT(*)                     AS quantidade,
  COALESCE(SUM(rd.valor_cbs_nao_extinto), 0) AS valor_cbs_nao_extinto
FROM rfb_debitos rd
WHERE rd.modelo_dfe IN ('55','65')
  AND rd.chave_dfe != ''
  AND NOT EXISTS (
    SELECT 1 FROM nfe_entradas ne
    WHERE ne.company_id = rd.company_id AND ne.chave_nfe = rd.chave_dfe
  )
GROUP BY rd.company_id, rd.ni_emitente, rd.data_dfe_emissao::DATE

UNION ALL

SELECT
  rd.company_id,
  'cte'::TEXT                  AS tipo,
  COALESCE(rd.ni_emitente, '') AS ni_emitente,
  rd.data_dfe_emissao::DATE    AS data_emissao,
  COUNT(*)                     AS quantidade,
  COALESCE(SUM(rd.valor_cbs_nao_extinto), 0) AS valor_cbs_nao_extinto
FROM rfb_debitos rd
WHERE rd.modelo_dfe IN ('57')
  AND rd.chave_dfe != ''
  AND NOT EXISTS (
    SELECT 1 FROM cte_entradas ce
    WHERE ce.company_id = rd.company_id AND ce.chave_cte = rd.chave_dfe
  )
GROUP BY rd.company_id, rd.ni_emitente, rd.data_dfe_emissao::DATE;

-- Índice único — obrigatório para REFRESH CONCURRENTLY (non-blocking)
CREATE UNIQUE INDEX mv_malha_fina_resumo_pk
  ON mv_malha_fina_resumo (company_id, tipo, ni_emitente, data_emissao);

-- Índice para filtros de consulta
CREATE INDEX mv_malha_fina_resumo_company_tipo
  ON mv_malha_fina_resumo (company_id, tipo);
