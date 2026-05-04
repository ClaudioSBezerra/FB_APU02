-- Migration 102: tabelas CGIBS — Comitê Gestor do IBS
-- Espelha a estrutura da RFB (rfb_credentials, rfb_requests, rfb_debitos, rfb_resumo)
-- voltada ao IBS. API CGIBS em fase piloto (jan/2026) — estrutura pronta para integração futura.

CREATE TABLE IF NOT EXISTS cgibs_credentials (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id           TEXT NOT NULL UNIQUE,
    cnpj_matriz          VARCHAR(14) NOT NULL,
    client_id            TEXT NOT NULL,
    client_secret        TEXT NOT NULL,
    ambiente             VARCHAR(30) NOT NULL DEFAULT 'piloto',
    ativo                BOOLEAN NOT NULL DEFAULT TRUE,
    agendamento_ativo    BOOLEAN NOT NULL DEFAULT FALSE,
    horario_agendamento  TIME NOT NULL DEFAULT '06:00',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cgibs_requests (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id          TEXT NOT NULL,
    cnpj_base           VARCHAR(14) NOT NULL,
    tiquete             TEXT,
    tiquete_download    TEXT,
    status              VARCHAR(30) NOT NULL DEFAULT 'pending',
    ambiente            VARCHAR(30) NOT NULL DEFAULT 'piloto',
    tipo                VARCHAR(20) NOT NULL DEFAULT 'debito',
    error_code          TEXT,
    error_message       TEXT,
    raw_json            TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cgibs_debitos (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id            UUID REFERENCES cgibs_requests(id) ON DELETE CASCADE,
    company_id            TEXT NOT NULL,
    tipo_apuracao         VARCHAR(20),
    modelo_dfe            VARCHAR(5),
    numero_dfe            TEXT,
    chave_dfe             VARCHAR(44),
    data_dfe_emissao      DATE,
    data_apuracao         VARCHAR(6),
    ni_emitente           VARCHAR(14),
    ni_adquirente         VARCHAR(14),
    valor_ibs_total       NUMERIC(15,2) NOT NULL DEFAULT 0,
    valor_ibs_uf          NUMERIC(15,2) NOT NULL DEFAULT 0,
    valor_ibs_mun         NUMERIC(15,2) NOT NULL DEFAULT 0,
    valor_ibs_extinto     NUMERIC(15,2) NOT NULL DEFAULT 0,
    valor_ibs_nao_extinto NUMERIC(15,2) NOT NULL DEFAULT 0,
    situacao_debito       VARCHAR(30),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cgibs_resumo (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id            UUID REFERENCES cgibs_requests(id) ON DELETE CASCADE,
    company_id            TEXT NOT NULL,
    data_apuracao         VARCHAR(6),
    total_debitos         INTEGER NOT NULL DEFAULT 0,
    valor_ibs_total       NUMERIC(15,2) NOT NULL DEFAULT 0,
    valor_ibs_uf          NUMERIC(15,2) NOT NULL DEFAULT 0,
    valor_ibs_mun         NUMERIC(15,2) NOT NULL DEFAULT 0,
    valor_ibs_extinto     NUMERIC(15,2) NOT NULL DEFAULT 0,
    valor_ibs_nao_extinto NUMERIC(15,2) NOT NULL DEFAULT 0,
    total_corrente        INTEGER NOT NULL DEFAULT 0,
    total_ajuste          INTEGER NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cgibs_requests_company ON cgibs_requests(company_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cgibs_debitos_company  ON cgibs_debitos(company_id, data_apuracao);
CREATE INDEX IF NOT EXISTS idx_cgibs_resumo_company   ON cgibs_resumo(company_id, data_apuracao);
