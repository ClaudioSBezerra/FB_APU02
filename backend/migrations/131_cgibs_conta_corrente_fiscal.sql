-- Migration 131: CGIBS — conta corrente fiscal (substitui o modelo período/tiquete da 102).
-- Tabelas antigas (cgibs_requests/cgibs_debitos/cgibs_resumo) permanecem intactas nesta
-- migration — ainda são lidas por cgibs_apuracao.go/limpeza_total.go; dropar só quando
-- esses arquivos forem reescritos (spec-cgibs-solicitacao-ui.md).
-- Modelo derivado do MOC API Apuração Assistida IBS v1.10 (CGIBS, set/2026), seções 5.1-5.6
-- e ANEXO I (estrutura conta corrente fiscal / extrato_cc).

ALTER TABLE cgibs_credentials ADD COLUMN IF NOT EXISTS webhook_url TEXT;
ALTER TABLE cgibs_credentials ADD COLUMN IF NOT EXISTS token_contrib TEXT;
ALTER TABLE cgibs_credentials ADD COLUMN IF NOT EXISTS habilitado BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE cgibs_credentials ADD COLUMN IF NOT EXISTS data_habilitacao TIMESTAMP WITH TIME ZONE;
ALTER TABLE cgibs_credentials DROP CONSTRAINT IF EXISTS cgibs_credentials_habilitado_chk;
ALTER TABLE cgibs_credentials ADD CONSTRAINT cgibs_credentials_habilitado_chk
    CHECK (NOT habilitado OR data_habilitacao IS NOT NULL);
COMMENT ON COLUMN cgibs_credentials.ativo IS 'Credencial configurada e habilitada para uso pelo admin (liga/desliga local) — não confundir com habilitado.';
COMMENT ON COLUMN cgibs_credentials.habilitado IS 'CGIBS confirmou o registro via API de Habilitação do Contribuinte (MOC 5.1) — só true após resposta Habilitado=sucesso.';

-- Nova Solicitação (5.5) + notificações de arquivo via webhook (5.2), manual e diferencial
CREATE TABLE IF NOT EXISTS cgibs_solicitacoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    cnpj_base VARCHAR(8) NOT NULL,
    id_solicitacao_externo BIGINT,
    tipo_solicitacao VARCHAR(20) NOT NULL DEFAULT 'diferencial'
        CHECK (tipo_solicitacao IN ('manual','diferencial')),
    situacao_solicitacao VARCHAR(20) NOT NULL DEFAULT 'solicitada'
        CHECK (situacao_solicitacao IN ('solicitada','gerada','enviada','cancelada','expirada')),
    data_solicitacao TIMESTAMP WITH TIME ZONE,
    data_transacao_ini DATE,
    data_transacao_fim DATE,
    qtd_operacoes INTEGER DEFAULT 0 CHECK (qtd_operacoes >= 0),
    qtd_arq_vinculados INTEGER DEFAULT 0 CHECK (qtd_arq_vinculados >= 0),
    data_validade_solicitacao DATE,
    resultado TEXT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (data_transacao_ini IS NULL OR data_transacao_fim IS NULL OR data_transacao_ini <= data_transacao_fim)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_solicitacoes_externo ON cgibs_solicitacoes(company_id, id_solicitacao_externo) WHERE id_solicitacao_externo IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cgibs_solicitacoes_company ON cgibs_solicitacoes(company_id, created_at DESC);

-- Arquivos vinculados a uma solicitação (Arquivos[] do webhook / Obter Arquivo, 5.2/5.3)
CREATE TABLE IF NOT EXISTS cgibs_arquivos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    solicitacao_id UUID NOT NULL REFERENCES cgibs_solicitacoes(id) ON DELETE CASCADE,
    numero_sequencial BIGINT NOT NULL CHECK (numero_sequencial > 0),
    nome_arquivo TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','baixado','erro')),
    baixado_em TIMESTAMP WITH TIME ZONE,
    raw_json TEXT,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (status <> 'baixado' OR baixado_em IS NOT NULL),
    CHECK (status <> 'erro' OR error_message IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_arquivos_seq ON cgibs_arquivos(solicitacao_id, numero_sequencial);
CREATE INDEX IF NOT EXISTS idx_cgibs_arquivos_company ON cgibs_arquivos(company_id);

-- Operações = conta corrente fiscal por documento fiscal (ANEXO I, nível 2)
CREATE TABLE IF NOT EXISTS cgibs_operacoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    operacao_id_externo BIGINT,
    chave_acesso VARCHAR(44) NOT NULL CHECK (length(chave_acesso) = 44),
    dth_emissao TIMESTAMP WITH TIME ZONE,
    dth_autorizacao TIMESTAMP WITH TIME ZONE,
    cnpj_fornecedor VARCHAR(8), -- CNPJ8/raiz, conforme MOC ANEXO I (não é o CNPJ completo de 14)
    cnpj_adquirente VARCHAR(8), -- idem; opcional (MOC: "pode não existir ou ter um registro")
    extrato_hash VARCHAR(40),
    ultimo_arquivo_id UUID REFERENCES cgibs_arquivos(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (cnpj_fornecedor IS NOT NULL OR cnpj_adquirente IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_operacoes_chave ON cgibs_operacoes(company_id, chave_acesso);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_operacoes_externo ON cgibs_operacoes(company_id, operacao_id_externo) WHERE operacao_id_externo IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cgibs_operacoes_fornecedor ON cgibs_operacoes(company_id, cnpj_fornecedor);
CREATE INDEX IF NOT EXISTS idx_cgibs_operacoes_adquirente ON cgibs_operacoes(company_id, cnpj_adquirente);

-- Lançamentos = extrato_cc incremental (ANEXO I, nível 3-4) — 7 valores por lançamento
CREATE TABLE IF NOT EXISTS cgibs_lancamentos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    operacao_id UUID NOT NULL REFERENCES cgibs_operacoes(id) ON DELETE CASCADE,
    lancamento_id_externo BIGINT NOT NULL,
    dth_lancto TIMESTAMP WITH TIME ZONE NOT NULL,
    mov_codigo INTEGER, -- tabela de correspondência ainda não existe (MOC ANEXO I, campo MOV)
    mov_descricao TEXT, -- texto livre enquanto a tabela oficial não existir
    recurso_financeiro_disponivel NUMERIC(15,2) NOT NULL DEFAULT 0,
    recurso_financeiro_a_transferir NUMERIC(15,2) NOT NULL DEFAULT 0,
    credito_a_apropriar NUMERIC(15,2) NOT NULL DEFAULT 0,
    credito_nao_utilizado NUMERIC(15,2) NOT NULL DEFAULT 0,
    credito_utilizado NUMERIC(15,2) NOT NULL DEFAULT 0,
    debito_em_aberto NUMERIC(15,2) NOT NULL DEFAULT 0,
    debito_extinto NUMERIC(15,2) NOT NULL DEFAULT 0,
    arquivo_id UUID REFERENCES cgibs_arquivos(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cgibs_lancamentos_externo ON cgibs_lancamentos(operacao_id, lancamento_id_externo);
CREATE INDEX IF NOT EXISTS idx_cgibs_lancamentos_data ON cgibs_lancamentos(operacao_id, dth_lancto DESC);
CREATE INDEX IF NOT EXISTS idx_cgibs_lancamentos_company ON cgibs_lancamentos(company_id);
