-- Migration 129: prepara rfb_debitos/rfb_resumo/rfb_creditos/rfb_creditos_resumo
-- para o schema v2 da RFB (apuracao-cbs/v2), num deploy único junto com o parser
-- dual v1/v2 e os pontos de leitura traduzidos (ver spec-rfb-cbs-v2-schema.md).
--
-- 1) Widen data_apuracao: rfb_debitos/rfb_resumo eram VARCHAR(6) (só cabia
--    "AAAAMM"); o formato canônico passa a ser "mm/aaaa" (10 chars no pior caso,
--    ex. "01/2026"). rfb_creditos/rfb_creditos_resumo já eram VARCHAR(10) desde a
--    migration 096, mas ainda continham valores "AAAAMM" gravados pelo parser v1 —
--    também precisam do backfill abaixo.
ALTER TABLE rfb_debitos ALTER COLUMN data_apuracao TYPE VARCHAR(10);
ALTER TABLE rfb_resumo  ALTER COLUMN data_apuracao TYPE VARCHAR(10);

-- 2) Backfill idempotente "AAAAMM" → "mm/aaaa" nas 4 tabelas. O predicado
--    `~ '^[0-9]{6}$'` garante que só linhas ainda no formato antigo são
--    convertidas — reexecutar a migration (ou rodar contra uma tabela já
--    convertida) é um no-op, nenhuma linha "mm/aaaa" bate no regex de 6 dígitos.
UPDATE rfb_debitos
SET data_apuracao = SUBSTRING(data_apuracao FROM 5 FOR 2) || '/' || SUBSTRING(data_apuracao FROM 1 FOR 4)
WHERE data_apuracao ~ '^[0-9]{6}$';

UPDATE rfb_resumo
SET data_apuracao = SUBSTRING(data_apuracao FROM 5 FOR 2) || '/' || SUBSTRING(data_apuracao FROM 1 FOR 4)
WHERE data_apuracao ~ '^[0-9]{6}$';

UPDATE rfb_creditos
SET data_apuracao = SUBSTRING(data_apuracao FROM 5 FOR 2) || '/' || SUBSTRING(data_apuracao FROM 1 FOR 4)
WHERE data_apuracao ~ '^[0-9]{6}$';

UPDATE rfb_creditos_resumo
SET data_apuracao = SUBSTRING(data_apuracao FROM 5 FOR 2) || '/' || SUBSTRING(data_apuracao FROM 1 FOR 4)
WHERE data_apuracao ~ '^[0-9]{6}$';

-- 3) Colunas novas em rfb_debitos para os campos do payload v2 sem contraparte
--    direta no schema v1. NULL para linhas inseridas pelo caminho v1 (o parser só
--    preenche essas colunas quando o item veio do shape v2).
ALTER TABLE rfb_debitos ADD COLUMN IF NOT EXISTS origem INTEGER;
ALTER TABLE rfb_debitos ADD COLUMN IF NOT EXISTS documento INTEGER;
ALTER TABLE rfb_debitos ADD COLUMN IF NOT EXISTS data_registro TIMESTAMP WITH TIME ZONE;
ALTER TABLE rfb_debitos ADD COLUMN IF NOT EXISTS data_atualizacao TIMESTAMP WITH TIME ZONE;
ALTER TABLE rfb_debitos ADD COLUMN IF NOT EXISTS valor_cbs_excedente DECIMAL(18,2);
ALTER TABLE rfb_debitos ADD COLUMN IF NOT EXISTS valor_cbs_inexigivel DECIMAL(18,2);
ALTER TABLE rfb_debitos ADD COLUMN IF NOT EXISTS valor_cbs_suspenso DECIMAL(18,2);
ALTER TABLE rfb_debitos ADD COLUMN IF NOT EXISTS valor_cbs_saldo_devedor DECIMAL(18,2);

-- 4) Colunas novas em rfb_creditos — mesma ideia, mas com a árvore de
--    apropriação/utilização do bloco "cbs" de créditos v2 (mais rica que a de
--    débitos). Todos NUMERIC(15,2) para casar com os tipos monetários já usados
--    em rfb_creditos (migration 096).
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS origem INTEGER;
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS documento INTEGER;
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS data_registro DATE;
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS data_atualizacao DATE;
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_excedentes NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_apropriacao_inapropriavel NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_apropriacao_suspenso NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_apropriacao_prescrito NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_apropriacao_a_apropriar NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_apropriacao_apropriado NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_utilizacao_inutilizavel NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_utilizacao_utilizado NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_utilizacao_restabelecido NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_nao_utilizado_saldo_credor NUMERIC(15,2);
ALTER TABLE rfb_creditos ADD COLUMN IF NOT EXISTS valor_cbs_nao_utilizado_pedido_ressarcimento NUMERIC(15,2);
