-- Migration 136: mv_malha_fina_resumo ganha os blocos NFS-e ("nfse-saidas" e
-- "nfse-entradas") — spec-nfse-apuracao-ibscbs.md (issue #14).
--
-- Os 3 blocos existentes ("nfe-saidas", "nfe-entradas", "cte") são cópia literal da
-- migration 125. Blocos novos: linhas da base RFB com chave de 50 dígitos (chave de
-- acesso nacional da NFS-e — o processador RFB não usa modelo_dfe específico para
-- NFS-e) cuja chave NÃO existe em nfse_saidas/nfse_entradas.identificador_nfse da
-- empresa. Somente leitura de nfse_*: NFS-e nunca é gravada em rfb_*/cgibs_*.
-- Atualizada pelos mesmos REFRESH CONCURRENTLY já existentes (malha_fina.go,
-- services/mv_refresh.go, admin/limpeza) — a MV é uma só.

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
GROUP BY rc.company_id, rc.ni_emitente, rc.data_dfe_emissao::DATE

UNION ALL

SELECT
  rd.company_id,
  'nfse-saidas'::TEXT          AS tipo,
  COALESCE(rd.ni_emitente, '') AS ni_emitente,
  rd.data_dfe_emissao::DATE    AS data_emissao,
  COUNT(*)                     AS quantidade,
  COALESCE(SUM(rd.valor_cbs_nao_extinto), 0) AS valor_cbs_nao_extinto
FROM rfb_debitos rd
WHERE length(rd.chave_dfe) = 50
  AND NOT EXISTS (
    SELECT 1 FROM nfse_saidas nss
    WHERE nss.company_id = rd.company_id AND nss.identificador_nfse = rd.chave_dfe
  )
GROUP BY rd.company_id, rd.ni_emitente, rd.data_dfe_emissao::DATE

UNION ALL

SELECT
  rc.company_id,
  'nfse-entradas'::TEXT        AS tipo,
  COALESCE(rc.ni_emitente, '') AS ni_emitente,
  rc.data_dfe_emissao::DATE    AS data_emissao,
  COUNT(*)                     AS quantidade,
  COALESCE(SUM(rc.valor_cbs_nao_extinto), 0) AS valor_cbs_nao_extinto
FROM rfb_creditos rc
WHERE length(rc.chave_dfe) = 50
  AND NOT EXISTS (
    SELECT 1 FROM nfse_entradas nse
    WHERE nse.company_id = rc.company_id AND nse.identificador_nfse = rc.chave_dfe
  )
GROUP BY rc.company_id, rc.ni_emitente, rc.data_dfe_emissao::DATE;


-- Índice único — obrigatório para REFRESH CONCURRENTLY (non-blocking)
CREATE UNIQUE INDEX mv_malha_fina_resumo_pk
  ON mv_malha_fina_resumo (company_id, tipo, ni_emitente, data_emissao);

-- Índice para filtros de consulta
CREATE INDEX mv_malha_fina_resumo_company_tipo
  ON mv_malha_fina_resumo (company_id, tipo);
