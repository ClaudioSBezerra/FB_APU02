-- Migration 118: Suporte a pagamentos de origem SAP em pagamentos_fornecedores
-- (Story 2.3, AC #1/#2/#3).

ALTER TABLE pagamentos_fornecedores
    ADD COLUMN IF NOT EXISTS origem             VARCHAR(10) NOT NULL DEFAULT 'csv', -- 'csv' | 'sap_api'
    ADD COLUMN IF NOT EXISTS payment_status      VARCHAR(20),                       -- espelha paymentStatus do SAP
    ADD COLUMN IF NOT EXISTS match_type          VARCHAR(20),                       -- CHAVE | FALLBACK
    ADD COLUMN IF NOT EXISTS bukrs               VARCHAR(4),                        -- código de empresa SAP
    ADD COLUMN IF NOT EXISTS settlement_mode     VARCHAR(50),
    ADD COLUMN IF NOT EXISTS fallback_ambiguous  BOOLEAN NOT NULL DEFAULT FALSE;

-- Fecha o enum de origem — evita que um typo futuro crie uma linha fora dos
-- dois índices únicos parciais abaixo (ambos escopados por origem), o que
-- deixaria essa linha sem nenhuma proteção de deduplicação.
ALTER TABLE pagamentos_fornecedores
    ADD CONSTRAINT pagamentos_fornecedores_origem_check
    CHECK (origem IN ('csv', 'sap_api'));

-- Pagamentos de origem SAP não têm um batch de import CSV associado.
ALTER TABLE pagamentos_fornecedores
    ALTER COLUMN import_id DROP NOT NULL;

-- tipo_doc VARCHAR(10) (migration 110) é curto demais para o dfeType "DESCONHECIDO"
-- (12 caracteres) que o SAP retorna para documentos fora de NF-e/CT-e — sem
-- alargar, o INSERT de pagamentos de origem SAP para esses casos falharia com
-- "value too long". A conciliação (rfb_pagamentos_fornecedores.go) só compara
-- tipo_doc a 'NFE'/'CTE' literalmente, então alargar a coluna é seguro e não
-- muda nenhum comportamento existente.
ALTER TABLE pagamentos_fornecedores
    ALTER COLUMN tipo_doc TYPE VARCHAR(20);

-- uq_pag_forn (migration 110) era uma constraint UNIQUE de tabela inteira,
-- sem distinguir origem. Isso faz colidir pagamentos SAP legítimos mas
-- "coincidentes" em (chave_doc, data_pagamento, valor_pagamento) — o cenário
-- de "pagamentos por conta" (parcelas de mesmo valor no mesmo dia, decisão de
-- negócio já registrada no PRD desta iniciativa) é exatamente esse caso: duas
-- parcelas distintas (num_doc_pagamento diferente) mas com mesmo valor/data
-- seriam incorretamente barradas pela constraint antiga.
--
-- Solução: substituir a constraint única geral por dois ÍNDICES únicos
-- PARCIAIS, um por origem. Postgres CONSTRAINTs (ADD CONSTRAINT ... UNIQUE)
-- não suportam predicado WHERE — por isso a troca por índices parciais, e por
-- isso backend/handlers/pagamentos_fornecedores.go precisou trocar
-- `ON CONFLICT ON CONSTRAINT uq_pag_forn` por
-- `ON CONFLICT (...) WHERE origem = 'csv'` (ver Story 2.3, Completion Notes —
-- desvio do plano original, que assumia não precisar tocar nesse handler).
-- IF EXISTS: se esta migration falhar em um passo posterior e for reexecutada
-- (o runner só marca uma migration como aplicada após sucesso completo), o
-- DROP não pode falhar por já ter sido executado na tentativa anterior.
ALTER TABLE pagamentos_fornecedores DROP CONSTRAINT IF EXISTS uq_pag_forn;

CREATE UNIQUE INDEX IF NOT EXISTS uq_pag_forn_csv
    ON pagamentos_fornecedores (company_id, chave_doc, data_pagamento, valor_pagamento)
    WHERE origem = 'csv';

-- Chave de dedup nova, exclusiva para linhas de origem SAP. Pagamentos de
-- origem SAP sempre gravam num_doc_pagamento e bukrs preenchidos (nunca NULL),
-- então este índice parcial funciona como dedup real e robusta para esse
-- universo de linhas, incluindo "pagamentos por conta" (distinguidos pelo
-- num_doc_pagamento/clearing document, que é sempre diferente entre parcelas).
CREATE UNIQUE INDEX IF NOT EXISTS uq_pag_forn_sap_api
    ON pagamentos_fornecedores (company_id, chave_doc, num_doc_pagamento, bukrs)
    WHERE origem = 'sap_api';
