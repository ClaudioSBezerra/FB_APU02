-- Migration 134: NFS-e — cursor de NSU do Portal Nacional ADN, XML original e cancelamento.
--
-- spec-nfse-adn-parser-real.md. Puramente aditiva (migration 133 continua intacta):
--
-- nfse_certificados.ultimo_nsu: maior NSU já processado da fila de distribuição do ADN
--   (GET /contribuintes/DFe/{NSU}?lote=true). Lido no início de cada ciclo e atualizado na
--   MESMA transação que grava cada lote — interrupção no meio nunca perde nem duplica.
--
-- nfse_entradas / nfse_saidas:
--   xml_gzip_b64      — ArquivoXml exatamente como recebido do ADN (gzip + base64; nunca o XML
--                       descomprimido).
--   nsu               — NSU do documento na fila do ADN.
--   numero_nfse       — infNFSe/nNFSe.
--   cancelada         — marcada por evento de cancelamento (CANCELAMENTO,
--                       CANCELAMENTO_POR_SUBSTITUICAO, CANCELAMENTO_DEFERIDO_ANALISE_FISCAL,
--                       CANCELAMENTO_POR_OFICIO); nunca desmarcada por reenvio da nota.
--   data_cancelamento — DataHoraGeracao do evento de cancelamento.

ALTER TABLE nfse_certificados ADD COLUMN IF NOT EXISTS ultimo_nsu BIGINT NOT NULL DEFAULT 0;

ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS xml_gzip_b64      TEXT;
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS nsu               BIGINT;
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS numero_nfse       VARCHAR(20);
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS cancelada         BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE nfse_entradas ADD COLUMN IF NOT EXISTS data_cancelamento TIMESTAMPTZ;

ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS xml_gzip_b64      TEXT;
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS nsu               BIGINT;
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS numero_nfse       VARCHAR(20);
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS cancelada         BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE nfse_saidas ADD COLUMN IF NOT EXISTS data_cancelamento TIMESTAMPTZ;

-- nfse_certificados.cnpj_titular: CNPJ extraído do Subject do certificado no upload. Quando um
--   novo upload traz um certificado de OUTRO CNPJ, ultimo_nsu volta a 0 (a fila do ADN é por
--   titular — o cursor do CNPJ anterior não vale para o novo; ver nfse_credentials.go).
ALTER TABLE nfse_certificados ADD COLUMN IF NOT EXISTS cnpj_titular VARCHAR(14);

-- nfse_adn_falhas: quarentena de documentos do ADN que não puderam ser gravados (ArquivoXml
--   inválido — base64/gzip/tamanho/XML —, TipoDocumento/TipoEvento desconhecido, inclusive enum
--   numérico, ou erro de banco ao gravar). Gravada na MESMA transação que avança o cursor
--   ultimo_nsu: o cursor nunca passa por um documento sem que ele esteja gravado OU aqui.
--   arquivo_xml é o ArquivoXml exatamente como recebido (gzip + base64), para reprocessamento.
CREATE TABLE IF NOT EXISTS nfse_adn_falhas (
    id             BIGSERIAL PRIMARY KEY,
    company_id     UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    nsu            BIGINT,
    chave_acesso   TEXT,
    tipo_documento TEXT,
    tipo_evento    TEXT,
    motivo         TEXT NOT NULL,
    arquivo_xml    TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_nfse_adn_falhas_company_nsu UNIQUE (company_id, nsu)
);

CREATE INDEX IF NOT EXISTS idx_nfse_adn_falhas_company_created ON nfse_adn_falhas(company_id, created_at);

-- nfse_cancelamentos_pendentes: evento de cancelamento recebido ANTES da nota (ou de nota ainda
--   não gravada). Ao gravar a nota com a mesma chave, o cancelamento é aplicado e a linha
--   removida — a ordem de chegada na fila nunca perde um cancelamento.
CREATE TABLE IF NOT EXISTS nfse_cancelamentos_pendentes (
    company_id        UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    chave_acesso      TEXT NOT NULL,
    data_cancelamento TIMESTAMPTZ,
    tipo_evento       TEXT,
    nsu               BIGINT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (company_id, chave_acesso)
);
