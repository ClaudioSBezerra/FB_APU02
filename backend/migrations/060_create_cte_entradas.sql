-- Migration 060: Tabela cte_entradas
-- Armazena cabeçalho de CT-e (mod 57) de entrada importados via XML.
-- Nomes de colunas refletem as tags XML para facilitar rastreabilidade.
-- IBS/CBS são NULLABLE: transportadoras que não implementaram a Reforma ficam com NULL.

CREATE TABLE IF NOT EXISTS cte_entradas (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,

    -- Identificação do CT-e
    chave_cte       VARCHAR(44) NOT NULL,       -- chave de acesso 44 dígitos
    modelo          SMALLINT NOT NULL,           -- sempre 57 (CT-e)
    serie           VARCHAR(3),                  -- <serie>
    numero_cte      VARCHAR(9),                  -- <nCT>
    data_emissao    DATE NOT NULL,               -- derivado de <dhEmi>
    mes_ano         VARCHAR(7) NOT NULL,         -- MM/YYYY (padrão do projeto)
    nat_op          VARCHAR(60),                 -- <natOp> natureza da operação
    cfop            VARCHAR(4),                  -- <CFOP>
    modal           VARCHAR(2),                  -- <modal>: 01=Rodoviário 02=Aéreo 03=Aquaviário 04=Ferroviário

    -- Emitente (transportadora)
    emit_cnpj       VARCHAR(14) NOT NULL,        -- <emit><CNPJ>
    emit_nome       VARCHAR(60),                 -- <emit><xNome>
    emit_uf         VARCHAR(2),                  -- <emit><enderEmit><UF>

    -- Remetente (origem da carga)
    rem_cnpj_cpf    VARCHAR(14),                 -- <rem><CNPJ> ou <rem><CPF>
    rem_nome        VARCHAR(60),                 -- <rem><xNome>
    rem_uf          VARCHAR(2),                  -- <rem><enderReme><UF>

    -- Destinatário (destino da carga)
    dest_cnpj_cpf   VARCHAR(14),                 -- <dest><CNPJ> ou <dest><CPF>
    dest_nome       VARCHAR(60),                 -- <dest><xNome>
    dest_uf         VARCHAR(2),                  -- <dest><enderDest><UF>

    -- vPrest: valores da prestação do serviço de transporte
    v_prest         NUMERIC(15,2) DEFAULT 0,    -- <vPrest><vTPrest> total da prestação
    v_rec           NUMERIC(15,2) DEFAULT 0,    -- <vPrest><vRec> valor a receber

    -- Carga
    v_carga         NUMERIC(15,2) DEFAULT 0,    -- <infCTeNorm><infCarga><vCarga>

    -- ICMS da prestação
    v_bc_icms       NUMERIC(15,2) DEFAULT 0,    -- base de cálculo do ICMS
    v_icms          NUMERIC(15,2) DEFAULT 0,    -- valor do ICMS

    -- IBSCBSTot: NULLABLE — transportadoras sem as tags ficam com NULL
    v_bc_ibs_cbs    NUMERIC(15,2),              -- <vBCIBSCBS>
    v_ibs           NUMERIC(15,2),              -- <gIBS><vIBS>
    v_cbs           NUMERIC(15,2),              -- <gCBS><vCBS>

    created_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_cte_entradas_company_chave UNIQUE (company_id, chave_cte)
);

CREATE INDEX IF NOT EXISTS idx_cte_entradas_company_mes  ON cte_entradas(company_id, mes_ano);
CREATE INDEX IF NOT EXISTS idx_cte_entradas_company_data ON cte_entradas(company_id, data_emissao);
CREATE INDEX IF NOT EXISTS idx_cte_entradas_emit_cnpj    ON cte_entradas(company_id, emit_cnpj);
