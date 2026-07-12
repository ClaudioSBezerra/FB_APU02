-- Migration 115: Tabela de credenciais SAP S/4HANA (OAuth2 client_credentials)
-- Suporte à carga automática de pagamentos SAP (PRD FR-5/FR-6, epics.md Epic 1).
-- client_secret é armazenado já criptografado (AES-256-GCM, ver backend/handlers/crypto.go)
-- e nunca é retornado em texto plano por nenhuma rota de leitura.

CREATE TABLE IF NOT EXISTS sap_credentials (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id   UUID NOT NULL UNIQUE REFERENCES companies(id) ON DELETE CASCADE,
    client_id    TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    base_url     TEXT,
    bukrs_list   TEXT[] NOT NULL DEFAULT '{}',
    ativo        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
