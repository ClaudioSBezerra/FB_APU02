-- Migration 092: deduplicar rfb_debitos e prevenir duplicatas futuras
--
-- Causa: INSERT sem ON CONFLICT — cada nova consulta à RFB reinseria as
-- mesmas chaves com um novo request_id, gerando duplicatas na Malha Fina.

-- 1. Remover duplicatas mantendo o registro mais recente por (company_id, chave_dfe)
DELETE FROM rfb_debitos
WHERE chave_dfe IS NOT NULL AND chave_dfe != ''
  AND id NOT IN (
    SELECT DISTINCT ON (company_id, chave_dfe) id
    FROM rfb_debitos
    WHERE chave_dfe IS NOT NULL AND chave_dfe != ''
    ORDER BY company_id, chave_dfe, created_at DESC
  );

-- 2. Índice único parcial para prevenir duplicatas futuras
CREATE UNIQUE INDEX IF NOT EXISTS uq_rfb_debitos_company_chave
  ON rfb_debitos (company_id, chave_dfe)
  WHERE chave_dfe IS NOT NULL AND chave_dfe != '';
