-- Migration 090: consolidar nomes de parceiros na tabela parceiros
-- Migra os dados das colunas forn_nome/dest_nome/emit_nome para a tabela parceiros
-- e depois remove as colunas redundantes das tabelas de documentos.

-- 1. Migrar nomes existentes para parceiros (preserva dados)
INSERT INTO parceiros (company_id, cnpj, nome)
SELECT company_id, forn_cnpj, forn_nome
FROM nfe_entradas
WHERE forn_nome IS NOT NULL AND forn_nome != ''
  AND forn_cnpj IS NOT NULL AND forn_cnpj != ''
ON CONFLICT (company_id, cnpj)
DO UPDATE SET nome = EXCLUDED.nome
WHERE parceiros.nome = '' OR parceiros.nome IS NULL;

INSERT INTO parceiros (company_id, cnpj, nome)
SELECT company_id, dest_cnpj_cpf, dest_nome
FROM nfe_saidas
WHERE dest_nome IS NOT NULL AND dest_nome != ''
  AND dest_cnpj_cpf IS NOT NULL AND dest_cnpj_cpf != ''
ON CONFLICT (company_id, cnpj)
DO UPDATE SET nome = EXCLUDED.nome
WHERE parceiros.nome = '' OR parceiros.nome IS NULL;

INSERT INTO parceiros (company_id, cnpj, nome)
SELECT company_id, emit_cnpj, emit_nome
FROM cte_entradas
WHERE emit_nome IS NOT NULL AND emit_nome != ''
  AND emit_cnpj IS NOT NULL AND emit_cnpj != ''
ON CONFLICT (company_id, cnpj)
DO UPDATE SET nome = EXCLUDED.nome
WHERE parceiros.nome = '' OR parceiros.nome IS NULL;

-- 2. Remover colunas redundantes
ALTER TABLE nfe_entradas DROP COLUMN IF EXISTS forn_nome;
ALTER TABLE nfe_saidas   DROP COLUMN IF EXISTS dest_nome;
ALTER TABLE cte_entradas DROP COLUMN IF EXISTS emit_nome;
