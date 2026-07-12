-- Migration 121: Story 3.2 — última tentativa de busca no SAP por chave, para
-- os casos que NÃO geram linha em pagamentos_fornecedores (EM_ABERTO,
-- NAO_LOCALIZADO — ver comentário em services/sap_payments_processor.go,
-- persistPayments). Sem isso, esses dois status só existiam como contagem
-- agregada por execução (sap_sync_runs, Story 2.4), nunca por chave individual.

CREATE TABLE IF NOT EXISTS sap_resultados_busca (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    chave_dfe           VARCHAR(50) NOT NULL,
    bukrs               VARCHAR(4) NOT NULL,
    payment_status      VARCHAR(20) NOT NULL, -- 'EM_ABERTO' | 'NAO_LOCALIZADO'
    match_type          VARCHAR(20),
    fallback_note       TEXT,
    ultima_tentativa_em TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (company_id, chave_dfe)
);

-- Sem bukrs na chave única (ao contrário de uq_pag_forn_sap_api): o objetivo
-- aqui é "qual foi a ÚLTIMA tentativa para esta chave", não histórico por bukrs.
-- A própria constraint UNIQUE (company_id, chave_dfe) já cria o índice B-tree
-- que o LEFT JOIN da conciliação (srb.company_id/srb.chave_dfe) usa — não é
-- preciso um CREATE INDEX separado (seria redundante).
