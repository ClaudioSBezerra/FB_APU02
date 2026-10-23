-- Migration 133: Certificado digital A1 por empresa (NFS-e) + schema de captura
-- (nfse_entradas/nfse_saidas).
--
-- Primeira peça da automação de captura de NFS-e (tomadas/prestadas) — hoje 100%
-- manual, sem automação no SAP (spec-nfse-captura-plano.md). Esta migration só cria
-- a estrutura de armazenamento: cliente mTLS e captura real via Portal Nacional ADN
-- ficam para a sub-spec seguinte (issue #6).
--
-- certificado_pfx (arquivo .pfx em base64) e certificado_senha são SEMPRE
-- criptografados (AES-256-GCM, backend/crypto — EncryptField/DecryptField) antes de
-- persistir, nunca gravados em texto plano. Nenhuma rota de leitura devolve esses
-- dois campos — ver backend/handlers/nfse_credentials.go.

CREATE TABLE IF NOT EXISTS nfse_certificados (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id        UUID NOT NULL UNIQUE REFERENCES companies(id) ON DELETE CASCADE,
    certificado_pfx   TEXT NOT NULL,              -- .pfx em base64, criptografado (AES-256-GCM)
    certificado_senha TEXT NOT NULL,               -- senha do .pfx, criptografada (AES-256-GCM)
    nome_arquivo      TEXT,                        -- nome original do arquivo enviado (metadado, não sensível)
    validade          DATE,                        -- informada pelo usuário no upload (ver Design Notes da spec — não extraída do .pfx)
    status            TEXT NOT NULL DEFAULT 'ativo' CHECK (status IN ('ativo','vencido','revogado')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- nfse_entradas / nfse_saidas: mesmo padrão de colunas de documento fiscal usado em
-- nfe_entradas/nfe_saidas (migrations 058/059), com nomenclatura base_X/X por tributo
-- (migration 103) replicada para ISS/PIS/COFINS/INSS/IRRF/CSLL. Ficam vazias até a
-- sub-spec do cliente ADN popular dados reais — sem consumidor ainda, não quebram
-- nenhum handler existente.

CREATE TABLE IF NOT EXISTS nfse_entradas (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id           UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,

    identificador_nfse   TEXT NOT NULL,            -- identificador único da NFS-e no Portal Nacional ADN
    cnpj_prestador       VARCHAR(14),
    cnpj_tomador         VARCHAR(14),
    data_emissao         DATE,
    mes_ano              VARCHAR(7),                -- MM/YYYY (padrão do projeto)
    valor_servicos       NUMERIC(15,2) DEFAULT 0,

    base_iss             NUMERIC(15,2) NOT NULL DEFAULT 0,
    iss                  NUMERIC(15,2) NOT NULL DEFAULT 0,
    aliquota_iss         NUMERIC(15,2) NOT NULL DEFAULT 0,
    iss_retido           NUMERIC(15,2) NOT NULL DEFAULT 0,
    base_pis             NUMERIC(15,2) NOT NULL DEFAULT 0,
    pis                  NUMERIC(15,2) NOT NULL DEFAULT 0,
    base_cofins          NUMERIC(15,2) NOT NULL DEFAULT 0,
    cofins               NUMERIC(15,2) NOT NULL DEFAULT 0,
    inss                 NUMERIC(15,2) NOT NULL DEFAULT 0,
    irrf                 NUMERIC(15,2) NOT NULL DEFAULT 0,
    csll                 NUMERIC(15,2) NOT NULL DEFAULT 0,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_nfse_entradas_company_identificador UNIQUE (company_id, identificador_nfse)
);

CREATE INDEX IF NOT EXISTS idx_nfse_entradas_company_mes  ON nfse_entradas(company_id, mes_ano);
CREATE INDEX IF NOT EXISTS idx_nfse_entradas_company_data ON nfse_entradas(company_id, data_emissao);
CREATE INDEX IF NOT EXISTS idx_nfse_entradas_prestador    ON nfse_entradas(company_id, cnpj_prestador);

CREATE TABLE IF NOT EXISTS nfse_saidas (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id           UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,

    identificador_nfse   TEXT NOT NULL,
    cnpj_prestador       VARCHAR(14),
    cnpj_tomador         VARCHAR(14),
    data_emissao         DATE,
    mes_ano              VARCHAR(7),
    valor_servicos       NUMERIC(15,2) DEFAULT 0,

    base_iss             NUMERIC(15,2) NOT NULL DEFAULT 0,
    iss                  NUMERIC(15,2) NOT NULL DEFAULT 0,
    aliquota_iss         NUMERIC(15,2) NOT NULL DEFAULT 0,
    iss_retido           NUMERIC(15,2) NOT NULL DEFAULT 0,
    base_pis             NUMERIC(15,2) NOT NULL DEFAULT 0,
    pis                  NUMERIC(15,2) NOT NULL DEFAULT 0,
    base_cofins          NUMERIC(15,2) NOT NULL DEFAULT 0,
    cofins               NUMERIC(15,2) NOT NULL DEFAULT 0,
    inss                 NUMERIC(15,2) NOT NULL DEFAULT 0,
    irrf                 NUMERIC(15,2) NOT NULL DEFAULT 0,
    csll                 NUMERIC(15,2) NOT NULL DEFAULT 0,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_nfse_saidas_company_identificador UNIQUE (company_id, identificador_nfse)
);

CREATE INDEX IF NOT EXISTS idx_nfse_saidas_company_mes  ON nfse_saidas(company_id, mes_ano);
CREATE INDEX IF NOT EXISTS idx_nfse_saidas_company_data ON nfse_saidas(company_id, data_emissao);
CREATE INDEX IF NOT EXISTS idx_nfse_saidas_prestador    ON nfse_saidas(company_id, cnpj_prestador);
