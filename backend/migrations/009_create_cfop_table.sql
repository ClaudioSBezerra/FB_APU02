-- Criação da tabela CFOP
CREATE TABLE IF NOT EXISTS cfop (
    cfop VARCHAR(4) PRIMARY KEY,
    descricao_cfop VARCHAR(100) NOT NULL,
    tipo VARCHAR(1) NOT NULL
);

COMMENT ON TABLE cfop IS 'Tabela de Código Fiscal de Operações e Prestações';
COMMENT ON COLUMN cfop.cfop IS 'Código CFOP (4 dígitos)';
COMMENT ON COLUMN cfop.descricao_cfop IS 'Descrição do CFOP';
COMMENT ON COLUMN cfop.tipo IS 'Tipo do CFOP (A=Ativo, C=Consumo, R=Revenda, T=Transferencia, O=Outros, S=Saida Legacy)';