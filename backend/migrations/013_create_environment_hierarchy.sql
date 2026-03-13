-- Tabela de Ambientes (Tenant Principal)
-- Ex: "Ambiente de Testes", "Ambiente de Produção", "Consultoria X"
CREATE TABLE IF NOT EXISTS environments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Tabela de Grupos de Empresas
-- Ex: "Grupo Ferreira Costa", "Grupo Varejo Y"
CREATE TABLE IF NOT EXISTS enterprise_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Tabela de Empresas (Cadastro Formal)
-- Atualmente extraímos empresas dos arquivos SPED (tabela 0000), mas aqui teremos o cadastro "mestre"
CREATE TABLE IF NOT EXISTS companies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES enterprise_groups(id) ON DELETE CASCADE,
    cnpj VARCHAR(14) NOT NULL UNIQUE, -- CNPJ Base ou Completo? Vamos usar 14 digitos
    name VARCHAR(255) NOT NULL,
    trade_name VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Índices para performance
CREATE INDEX IF NOT EXISTS idx_enterprise_groups_env ON enterprise_groups(environment_id);
CREATE INDEX IF NOT EXISTS idx_companies_group ON companies(group_id);

-- Only create CNPJ index if column exists (legacy support)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='companies' AND column_name='cnpj') THEN
        CREATE INDEX IF NOT EXISTS idx_companies_cnpj ON companies(cnpj);
    END IF;
END $$;

-- Alteração futura (comentada por enquanto) será adicionar environment_id ou group_id em import_jobs
-- ALTER TABLE import_jobs ADD COLUMN group_id UUID REFERENCES enterprise_groups(id);
