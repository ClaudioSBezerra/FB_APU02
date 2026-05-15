-- Migration 107: restaura colunas de nome/UF/modal para importação via XML
-- As migrations 081-083 removeram essas colunas por serem desnecessárias no fluxo SAP S4/HANA.
-- Para importação direta de arquivos XML (NF-e e CT-e), precisamos armazenar os dados ricos
-- presentes nos XMLs: nomes de participantes, UFs, modal de transporte, etc.

-- nfe_entradas: adiciona forn_uf, dest_nome, dest_uf
-- (forn_nome já existe desde migration 086)
ALTER TABLE nfe_entradas
  ADD COLUMN IF NOT EXISTS forn_uf   TEXT,
  ADD COLUMN IF NOT EXISTS dest_nome TEXT,
  ADD COLUMN IF NOT EXISTS dest_uf   TEXT;

-- nfe_saidas: adiciona emit_nome, emit_uf, dest_uf
-- (dest_nome já existe desde migration 087)
ALTER TABLE nfe_saidas
  ADD COLUMN IF NOT EXISTS emit_nome TEXT,
  ADD COLUMN IF NOT EXISTS emit_uf   TEXT,
  ADD COLUMN IF NOT EXISTS dest_uf   TEXT;

-- cte_entradas: adiciona emit_uf, rem_cnpj_cpf, rem_nome, rem_uf, dest_nome, dest_uf, nat_op, cfop, modal
-- (emit_nome já existe desde migration 086)
ALTER TABLE cte_entradas
  ADD COLUMN IF NOT EXISTS emit_uf      TEXT,
  ADD COLUMN IF NOT EXISTS rem_cnpj_cpf TEXT,
  ADD COLUMN IF NOT EXISTS rem_nome     TEXT,
  ADD COLUMN IF NOT EXISTS rem_uf       TEXT,
  ADD COLUMN IF NOT EXISTS dest_nome    TEXT,
  ADD COLUMN IF NOT EXISTS dest_uf      TEXT,
  ADD COLUMN IF NOT EXISTS nat_op       TEXT,
  ADD COLUMN IF NOT EXISTS cfop         TEXT,
  ADD COLUMN IF NOT EXISTS modal        TEXT;
