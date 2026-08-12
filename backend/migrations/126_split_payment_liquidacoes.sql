-- Migration 126: Cronograma de liquidação financeira para vendas/compras parceladas
-- (Split Payment CBS/IBS, Etapa 2 — cartão/voucher, LC 214/2025, data ainda não
-- definida por ato conjunto RFB/CGIBS).
--
-- rfb_debitos.eventos e rfb_creditos.eventos já guardam (raw JSON) as extinções
-- reais reportadas pela RFB via Apuração Assistida quando existirem. O que falta
-- é o cronograma ESPERADO de liquidação, conhecido desde a venda/compra (dado do
-- ERP: forma de pagamento, nº de parcelas, datas previstas) — disponível muito
-- antes de qualquer retorno da RFB, e usado depois para reconciliar contra o
-- que a Apuração Assistida efetivamente confirmar.
--
-- Ligação por chave_dfe (não por FK a rfb_debitos/rfb_creditos.id): o cronograma
-- nasce no momento da venda/compra, antes de existir qualquer request/download
-- da RFB para aquele documento — mesmo padrão de acoplamento fraco via chave já
-- usado entre nfe_saidas/nfe_entradas e rfb_debitos/rfb_creditos.

CREATE TABLE IF NOT EXISTS rfb_debitos_liquidacoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    chave_dfe VARCHAR(50) NOT NULL,
    numero_parcela SMALLINT NOT NULL,
    total_parcelas SMALLINT NOT NULL,
    arranjo_pagamento VARCHAR(30) NOT NULL,
    valor_parcela NUMERIC(15,2) NOT NULL,
    valor_cbs_proporcional NUMERIC(15,2),
    data_prevista_liquidacao DATE NOT NULL,
    data_liquidacao_confirmada DATE,
    status VARCHAR(20) NOT NULL DEFAULT 'previsto',
    origem VARCHAR(20) NOT NULL DEFAULT 'erp',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (company_id, chave_dfe, numero_parcela)
);

CREATE INDEX IF NOT EXISTS idx_rfb_debitos_liquidacoes_chave
    ON rfb_debitos_liquidacoes(company_id, chave_dfe);
CREATE INDEX IF NOT EXISTS idx_rfb_debitos_liquidacoes_pendentes
    ON rfb_debitos_liquidacoes(company_id, data_prevista_liquidacao)
    WHERE status = 'previsto';

CREATE TABLE IF NOT EXISTS rfb_creditos_liquidacoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    chave_dfe VARCHAR(50) NOT NULL,
    numero_parcela SMALLINT NOT NULL,
    total_parcelas SMALLINT NOT NULL,
    arranjo_pagamento VARCHAR(30) NOT NULL,
    valor_parcela NUMERIC(15,2) NOT NULL,
    valor_cbs_proporcional NUMERIC(15,2),
    data_prevista_liquidacao DATE NOT NULL,
    data_liquidacao_confirmada DATE,
    status VARCHAR(20) NOT NULL DEFAULT 'previsto',
    origem VARCHAR(20) NOT NULL DEFAULT 'erp',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (company_id, chave_dfe, numero_parcela)
);

CREATE INDEX IF NOT EXISTS idx_rfb_creditos_liquidacoes_chave
    ON rfb_creditos_liquidacoes(company_id, chave_dfe);
CREATE INDEX IF NOT EXISTS idx_rfb_creditos_liquidacoes_pendentes
    ON rfb_creditos_liquidacoes(company_id, data_prevista_liquidacao)
    WHERE status = 'previsto';

-- status: 'previsto' (só o cronograma do ERP, ainda sem confirmação da RFB),
--         'liquidado' (operadora liquidou a parcela — data_liquidacao_confirmada preenchida),
--         'reconciliado' (evento correspondente já apareceu em rfb_debitos/creditos.eventos)
-- origem: 'erp' (nasceu do dado de venda/compra) ou 'rfb' (nasceu de um evento
--         reportado pela Apuração Assistida sem cronograma prévio do ERP)
