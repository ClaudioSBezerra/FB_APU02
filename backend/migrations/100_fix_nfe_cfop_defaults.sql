-- Migration 100: garante DEFAULT e NOT NULL em tipo_cfop / cfop de nfe_entradas
-- Corrige caso as migrations 098/099 tenham falhado parcialmente (colunas criadas mas
-- DEFAULT/NOT NULL não aplicados por erro no UPDATE de cross-ref com EFD).

-- Zera NULLs antes de tornar NOT NULL
UPDATE nfe_entradas SET tipo_cfop = 'C' WHERE tipo_cfop IS NULL OR tipo_cfop = '';

-- Aplica NOT NULL e DEFAULT (idempotente — não falha se já existir)
ALTER TABLE nfe_entradas
  ALTER COLUMN tipo_cfop SET NOT NULL,
  ALTER COLUMN tipo_cfop SET DEFAULT 'C';

-- cfop permanece nullable (nem toda nota tem CFOP disponível)
ALTER TABLE nfe_entradas
  ALTER COLUMN cfop DROP NOT NULL;

-- Índices (idempotentes)
CREATE INDEX IF NOT EXISTS idx_nfe_entradas_tipo_cfop ON nfe_entradas(company_id, tipo_cfop);
CREATE INDEX IF NOT EXISTS idx_nfe_entradas_cfop      ON nfe_entradas(company_id, cfop);
