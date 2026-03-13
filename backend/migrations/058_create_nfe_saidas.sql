-- Migration 058: Tabela nfe_saidas
-- Armazena cabeçalho de NF-e (mod 55/65) de saída importadas via XML.
-- Nomes de colunas refletem as tags XML para facilitar rastreabilidade e documentação.
-- A chave_nfe é o elo de relacionamento com o SPED (reg_c100) e dados da RFB.

CREATE TABLE IF NOT EXISTS nfe_saidas (
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

    -- Emitente
    emit_cnpj       VARCHAR(14) NOT NULL,        -- <emit><CNPJ>
    emit_nome       VARCHAR(60),                 -- <emit><xNome>
    emit_uf         VARCHAR(2),                  -- <emit><enderEmit><UF>
    emit_municipio  VARCHAR(60),                 -- <emit><enderEmit><xMun>

    -- Destinatário
    dest_cnpj_cpf   VARCHAR(14),                -- <dest><CNPJ> ou <CPF>
    dest_nome       VARCHAR(60),                 -- <dest><xNome>
    dest_uf         VARCHAR(2),                  -- <dest><enderDest><UF>
    dest_c_mun      VARCHAR(7),                  -- <dest><enderDest><cMun> código IBGE município

    -- ICMSTot: valores íntegros das tags XML
    v_bc            NUMERIC(15,2) DEFAULT 0,    -- <vBC>      base de cálculo ICMS
    v_icms          NUMERIC(15,2) DEFAULT 0,    -- <vICMS>    valor ICMS
    v_icms_deson    NUMERIC(15,2) DEFAULT 0,    -- <vICMSDeson>
    v_fcp           NUMERIC(15,2) DEFAULT 0,    -- <vFCP>     Fundo de Combate à Pobreza
    v_bc_st         NUMERIC(15,2) DEFAULT 0,    -- <vBCST>    base ICMS ST
    v_st            NUMERIC(15,2) DEFAULT 0,    -- <vST>      valor ICMS ST
    v_fcp_st        NUMERIC(15,2) DEFAULT 0,    -- <vFCPST>
    v_fcp_st_ret    NUMERIC(15,2) DEFAULT 0,    -- <vFCPSTRet>
    v_prod          NUMERIC(15,2) DEFAULT 0,    -- <vProd>    valor total dos produtos
    v_frete         NUMERIC(15,2) DEFAULT 0,    -- <vFrete>
    v_seg           NUMERIC(15,2) DEFAULT 0,    -- <vSeg>     seguro
    v_desc          NUMERIC(15,2) DEFAULT 0,    -- <vDesc>    desconto
    v_ii            NUMERIC(15,2) DEFAULT 0,    -- <vII>      imposto de importação
    v_ipi           NUMERIC(15,2) DEFAULT 0,    -- <vIPI>
    v_ipi_devol     NUMERIC(15,2) DEFAULT 0,    -- <vIPIDevol>
    v_pis           NUMERIC(15,2) DEFAULT 0,    -- <vPIS>
    v_cofins        NUMERIC(15,2) DEFAULT 0,    -- <vCOFINS>
    v_outro         NUMERIC(15,2) DEFAULT 0,    -- <vOutro>
    v_nf            NUMERIC(15,2) DEFAULT 0,    -- <vNF>      valor total da nota

    -- IBSCBSTot: valores íntegros das tags XML (Reforma Tributária)
    v_bc_ibs_cbs    NUMERIC(15,2),              -- <vBCIBSCBS>  base única IBS+CBS
    v_ibs_uf        NUMERIC(15,2),              -- <gIBS><gIBSUF><vIBSUF>
    v_ibs_mun       NUMERIC(15,2),              -- <gIBS><gIBSMun><vIBSMun>
    v_ibs           NUMERIC(15,2),              -- <gIBS><vIBS>   total IBS
    v_cred_pres_ibs NUMERIC(15,2),              -- <gIBS><vCredPres>
    v_cbs           NUMERIC(15,2),              -- <gCBS><vCBS>
    v_cred_pres_cbs NUMERIC(15,2),              -- <gCBS><vCredPres>

    created_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_nfe_saidas_company_chave UNIQUE (company_id, chave_nfe)
);

CREATE INDEX IF NOT EXISTS idx_nfe_saidas_company_mes   ON nfe_saidas(company_id, mes_ano);
CREATE INDEX IF NOT EXISTS idx_nfe_saidas_company_data  ON nfe_saidas(company_id, data_emissao);
CREATE INDEX IF NOT EXISTS idx_nfe_saidas_emit_cnpj     ON nfe_saidas(company_id, emit_cnpj);
CREATE INDEX IF NOT EXISTS idx_nfe_saidas_dest_c_mun    ON nfe_saidas(company_id, dest_c_mun);
