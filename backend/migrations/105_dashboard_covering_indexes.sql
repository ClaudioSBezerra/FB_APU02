-- Covering indexes para queries de dashboard (COUNT + SUM por company_id + mes_ano).
-- Eliminam heap fetch ao incluir colunas de valor no próprio índice,
-- permitindo index-only scans nos SELECTs do DashboardResumoHandler.

CREATE INDEX IF NOT EXISTS idx_nfe_entradas_dashboard_cover
  ON nfe_entradas (company_id, mes_ano)
  INCLUDE (v_nf, v_ibs, v_cbs, v_bc_ibs_cbs, forn_cnpj);

CREATE INDEX IF NOT EXISTS idx_cte_entradas_dashboard_cover
  ON cte_entradas (company_id, mes_ano)
  INCLUDE (v_prest, v_ibs, v_cbs, v_bc_ibs_cbs, emit_cnpj);

CREATE INDEX IF NOT EXISTS idx_nfe_saidas_dashboard_cover
  ON nfe_saidas (company_id, mes_ano)
  INCLUDE (v_nf, v_ibs, v_cbs, v_bc_ibs_cbs, v_ibs_uf, v_ibs_mun);

-- Índice em forn_simples.cnpj para o LEFT JOIN do dashboard
-- (forn_simples não tinha índice explícito na PK além do padrão serial)
CREATE INDEX IF NOT EXISTS idx_forn_simples_cnpj
  ON forn_simples (cnpj);
