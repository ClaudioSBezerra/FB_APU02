-- Migration 125: corrige mv_malha_fina_resumo — blocos "nfe-entradas" e "cte"
-- liam de rfb_debitos (lado de débito/venda) quando deveriam ler de
-- rfb_creditos (lado de crédito/compra, criado na migration 096, bem depois
-- desta MV). Bloco "nfe-saidas" já estava correto e permanece inalterado.

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
  rc.company_id,
  'nfe-entradas'::TEXT         AS tipo,
  COALESCE(rc.ni_emitente, '') AS ni_emitente,
  rc.data_dfe_emissao::DATE    AS data_emissao,
  COUNT(*)                     AS quantidade,
  COALESCE(SUM(rc.valor_cbs_nao_extinto), 0) AS valor_cbs_nao_extinto
FROM rfb_creditos rc
WHERE rc.modelo_dfe IN ('55','65')
  AND rc.chave_dfe != ''
  AND NOT EXISTS (
    SELECT 1 FROM nfe_entradas ne
    WHERE ne.company_id = rc.company_id AND ne.chave_nfe = rc.chave_dfe
  )
GROUP BY rc.company_id, rc.ni_emitente, rc.data_dfe_emissao::DATE

UNION ALL

SELECT
  rc.company_id,
  'cte'::TEXT                  AS tipo,
  COALESCE(rc.ni_emitente, '') AS ni_emitente,
  rc.data_dfe_emissao::DATE    AS data_emissao,
  COUNT(*)                     AS quantidade,
  COALESCE(SUM(rc.valor_cbs_nao_extinto), 0) AS valor_cbs_nao_extinto
FROM rfb_creditos rc
WHERE rc.modelo_dfe IN ('57')
  AND rc.chave_dfe != ''
  AND NOT EXISTS (
    SELECT 1 FROM cte_entradas ce
    WHERE ce.company_id = rc.company_id AND ce.chave_cte = rc.chave_dfe
  )
GROUP BY rc.company_id, rc.ni_emitente, rc.data_dfe_emissao::DATE;

-- Índice único — obrigatório para REFRESH CONCURRENTLY (non-blocking)
CREATE UNIQUE INDEX mv_malha_fina_resumo_pk
  ON mv_malha_fina_resumo (company_id, tipo, ni_emitente, data_emissao);

-- Índice para filtros de consulta
CREATE INDEX mv_malha_fina_resumo_company_tipo
  ON mv_malha_fina_resumo (company_id, tipo);
