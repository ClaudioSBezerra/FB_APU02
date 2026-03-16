-- Migration 066: Índices covering para queries de lista paginada
-- INCLUDE: colunas mais projetadas → elimina heap fetch nas queries comuns
-- Nota: Em produção com tabelas grandes, prefira CREATE INDEX CONCURRENTLY (não pode rodar via migration runner)

CREATE INDEX IF NOT EXISTS idx_nfe_saidas_list_cover
ON nfe_saidas(company_id, data_emissao DESC, numero_nfe DESC)
INCLUDE (chave_nfe, modelo, serie, mes_ano, emit_cnpj, emit_nome, emit_uf,
         dest_cnpj_cpf, dest_nome, dest_uf, v_nf);

CREATE INDEX IF NOT EXISTS idx_nfe_entradas_list_cover
ON nfe_entradas(company_id, data_emissao DESC, numero_nfe DESC)
INCLUDE (chave_nfe, modelo, serie, mes_ano, forn_cnpj, forn_nome, forn_uf,
         dest_cnpj_cpf, dest_nome, dest_uf, v_nf);

CREATE INDEX IF NOT EXISTS idx_cte_entradas_list_cover
ON cte_entradas(company_id, data_emissao DESC, numero_cte DESC)
INCLUDE (chave_cte, modelo, serie, mes_ano, emit_cnpj, emit_nome, emit_uf,
         dest_cnpj_cpf, dest_nome, dest_uf, v_prest);
