-- Migration 119: Contagens por paymentStatus e detalhe de erro em sap_sync_runs
-- (Story 2.4, AC #1). Nomes de coluna já pré-acordados no comentário da
-- migration 116. Tempo total de execução não vira coluna própria — é
-- calculado a partir de concluido_em - iniciado_em (evita dado redundante).

ALTER TABLE sap_sync_runs
    ADD COLUMN IF NOT EXISTS chaves_pago_total     INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS chaves_pago_parcial    INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS chaves_em_aberto       INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS chaves_nao_localizado  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS erro_detalhe           TEXT;
