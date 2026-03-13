-- Migration 059: Tabela nfe_entradas
-- Armazena cabeçalho de NF-e (mod 55/65) de entrada importadas via XML.
-- Nomes de colunas refletem as tags XML para facilitar rastreabilidade.
-- IBS/CBS são NOT NULL com DEFAULT 0: fornecedores sem as tags ficam com zero.

CREATE TABLE IF NOT EXISTS nfe_entradas (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,

    -- Identificação da NF-e
    chave_nfe       VARCHAR(44) NOT NULL,       -- chave de acesso 44 dígitos
    modelo          SMALLINT NOT NULL,           -- 55 (NF-e) ou 65 (NFC-e)
    serie           VARCHAR(3),                  -- <serie>
    numero_nfe      VARCHAR(9),                  -- <nNF>
    data_emissao    DATE NOT NULL,               -- derivado de <dhEmi>
    mes_ano         VARCHAR(7) NOT NULL,         -- MM/YYYY (padrão do projeto)
    nat_op          VARCHAR(60),                 -- <natOp> natureza da operação

    -- Fornecedor (emitente da nota de entrada)
    forn_cnpj       VARCHAR(14) NOT NULL,        -- <emit><CNPJ>
    forn_nome       VARCHAR(60),                 -- <emit><xNome>
    forn_uf         VARCHAR(2),                  -- <emit><enderEmit><UF>
    forn_municipio  VARCHAR(60),                 -- <emit><enderEmit><xMun>

    -- Destinatário (a empresa que recebeu)
    dest_cnpj_cpf   VARCHAR(14),                -- <dest><CNPJ> ou <CPF>
    dest_nome       VARCHAR(60),                 -- <dest><xNome>
    dest_uf         VARCHAR(2),                  -- <dest><enderDest><UF>
    dest_c_mun      VARCHAR(7),                  -- <dest><enderDest><cMun> código IBGE

    -- ICMSTot: valores íntegros das tags XML
    v_bc            NUMERIC(15,2) DEFAULT 0,    -- <vBC>
    v_icms          NUMERIC(15,2) DEFAULT 0,    -- <vICMS>
    v_icms_deson    NUMERIC(15,2) DEFAULT 0,    -- <vICMSDeson>
    v_fcp           NUMERIC(15,2) DEFAULT 0,    -- <vFCP>
    v_bc_st         NUMERIC(15,2) DEFAULT 0,    -- <vBCST>
    v_st            NUMERIC(15,2) DEFAULT 0,    -- <vST>
    v_fcp_st        NUMERIC(15,2) DEFAULT 0,    -- <vFCPST>
    v_fcp_st_ret    NUMERIC(15,2) DEFAULT 0,    -- <vFCPSTRet>
    v_prod          NUMERIC(15,2) DEFAULT 0,    -- <vProd>
    v_frete         NUMERIC(15,2) DEFAULT 0,    -- <vFrete>
    v_seg           NUMERIC(15,2) DEFAULT 0,    -- <vSeg>
    v_desc          NUMERIC(15,2) DEFAULT 0,    -- <vDesc>
    v_ii            NUMERIC(15,2) DEFAULT 0,    -- <vII>
    v_ipi           NUMERIC(15,2) DEFAULT 0,    -- <vIPI>
    v_ipi_devol     NUMERIC(15,2) DEFAULT 0,    -- <vIPIDevol>
    v_pis           NUMERIC(15,2) DEFAULT 0,    -- <vPIS>
    v_cofins        NUMERIC(15,2) DEFAULT 0,    -- <vCOFINS>
    v_outro         NUMERIC(15,2) DEFAULT 0,    -- <vOutro>
    v_nf            NUMERIC(15,2) DEFAULT 0,    -- <vNF>

    -- IBSCBSTot: NOT NULL DEFAULT 0 — fornecedores sem as tags ficam com zero
    v_bc_ibs_cbs    NUMERIC(15,2) NOT NULL DEFAULT 0,   -- <vBCIBSCBS>
    v_ibs_uf        NUMERIC(15,2) NOT NULL DEFAULT 0,   -- <gIBS><gIBSUF><vIBSUF>
    v_ibs_mun       NUMERIC(15,2) NOT NULL DEFAULT 0,   -- <gIBS><gIBSMun><vIBSMun>
    v_ibs           NUMERIC(15,2) NOT NULL DEFAULT 0,   -- <gIBS><vIBS>
    v_cred_pres_ibs NUMERIC(15,2) NOT NULL DEFAULT 0,   -- <gIBS><vCredPres>
    v_cbs           NUMERIC(15,2) NOT NULL DEFAULT 0,   -- <gCBS><vCBS>
    v_cred_pres_cbs NUMERIC(15,2) NOT NULL DEFAULT 0,   -- <gCBS><vCredPres>

    created_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_nfe_entradas_company_chave UNIQUE (company_id, chave_nfe)
);

CREATE INDEX IF NOT EXISTS idx_nfe_entradas_company_mes   ON nfe_entradas(company_id, mes_ano);
CREATE INDEX IF NOT EXISTS idx_nfe_entradas_company_data  ON nfe_entradas(company_id, data_emissao);
CREATE INDEX IF NOT EXISTS idx_nfe_entradas_forn_cnpj     ON nfe_entradas(company_id, forn_cnpj);
