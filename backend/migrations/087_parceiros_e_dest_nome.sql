-- Migration 087: tabela parceiros + dest_nome em nfe_saidas
-- Permite armazenar e consultar nomes de clientes/fornecedores/transportadoras
-- identificados via SAP NOME_PARCEIRO (forn.razsoc / clie.razsoc).

-- Nome do cliente destinatário nas saídas
ALTER TABLE nfe_saidas
  ADD COLUMN IF NOT EXISTS dest_nome TEXT;

-- Lookup CNPJ → nome compartilhado entre todas as tabelas de documentos.
-- Populado via UPSERT a cada importação SAP. Permite popular retroativamente
-- registros importados antes da inclusão do campo nome_parceiro no bridge.
CREATE TABLE IF NOT EXISTS parceiros (
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    cnpj       TEXT NOT NULL,
    nome       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (company_id, cnpj)
);

CREATE INDEX IF NOT EXISTS idx_parceiros_company ON parceiros(company_id);
