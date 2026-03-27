-- Migration 086: readiciona colunas de nome de participante
-- forn_nome em nfe_entradas e emit_nome em cte_entradas foram removidas
-- nas migrations 082/083. São necessárias para identificação nas telas
-- de análise (Créditos em Risco). Nullable para compatibilidade com SAP batch.

ALTER TABLE nfe_entradas
  ADD COLUMN IF NOT EXISTS forn_nome TEXT;

ALTER TABLE cte_entradas
  ADD COLUMN IF NOT EXISTS emit_nome TEXT;
