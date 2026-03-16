-- Migration 068: Autovacuum agressivo para tabelas de alto volume
-- Padrão 0.2 = vacuum após 20% da tabela mudar (~720K linhas em 3,6M = 72 dias sem vacuum)
-- Com 0.01 = vacuum após 1% (~36K linhas), mantendo bloat controlado

ALTER TABLE nfe_saidas   SET (autovacuum_vacuum_scale_factor=0.01, autovacuum_analyze_scale_factor=0.005);
ALTER TABLE nfe_entradas SET (autovacuum_vacuum_scale_factor=0.01, autovacuum_analyze_scale_factor=0.005);
ALTER TABLE cte_entradas SET (autovacuum_vacuum_scale_factor=0.01, autovacuum_analyze_scale_factor=0.005);
ALTER TABLE rfb_debitos  SET (autovacuum_vacuum_scale_factor=0.01, autovacuum_analyze_scale_factor=0.005);
ALTER TABLE dfe_xml      SET (autovacuum_vacuum_scale_factor=0.01, autovacuum_analyze_scale_factor=0.005);
