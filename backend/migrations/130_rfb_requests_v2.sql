-- Migration 130: suporte à API RFB CBS v2 em rfb_requests.
-- api_versao: versão da API usada NA SOLICITAÇÃO ('v1'/'v2'); download e reprocesso
--   seguem a versão da solicitação, não a config atual (mesmo princípio do ambiente).
--   Linhas antigas ficam 'v1'.
-- url_assinada / url_assinada_expira_em: URL pré-assinada entregue pelo webhook v2
--   (ou por GET .../v2/situacao/{tiquete}); zerada após o download concluir.
ALTER TABLE rfb_requests ADD COLUMN IF NOT EXISTS api_versao VARCHAR(4) NOT NULL DEFAULT 'v1';
ALTER TABLE rfb_requests ADD COLUMN IF NOT EXISTS url_assinada TEXT;
ALTER TABLE rfb_requests ADD COLUMN IF NOT EXISTS url_assinada_expira_em TIMESTAMPTZ;
-- CHECK idempotente (DROP + ADD): funciona mesmo se a coluna já existia sem CHECK (banco de dev)
--   ou se a migration for reaplicada.
ALTER TABLE rfb_requests DROP CONSTRAINT IF EXISTS rfb_requests_api_versao_chk;
ALTER TABLE rfb_requests ADD CONSTRAINT rfb_requests_api_versao_chk CHECK (api_versao IN ('v1', 'v2'));
