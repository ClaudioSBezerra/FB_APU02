-- Migration 084: remove tabela dfe_xml
-- SAP S4/HANA não fornece XML — DANFE e DACTE foram removidos do sistema.

DROP TABLE IF EXISTS dfe_xml;
