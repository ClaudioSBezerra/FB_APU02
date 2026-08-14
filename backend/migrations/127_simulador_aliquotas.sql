-- Migration 127: Alíquotas editáveis por empresa para o Simulador Projetado (RFB)
--
-- tabela_aliquotas (migration 007) permanece intocada — é a referência oficial
-- da transição EC 132/2023 (2027-2033), usada por outras features (ex: Créditos
-- em Risco) que não devem ser afetadas por ajustes de cenário de uma empresa.
--
-- simulador_aliquotas guarda só os overrides — cada empresa começa sem nenhuma
-- linha (o handler de leitura usa os valores oficiais como fallback via LEFT
-- JOIN) e só ganha uma linha por ano quando o usuário efetivamente edita aquele
-- ano no Simulador Projetado.

CREATE TABLE IF NOT EXISTS simulador_aliquotas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    ano INT NOT NULL,
    perc_ibs_uf NUMERIC(5,2) NOT NULL,
    perc_ibs_mun NUMERIC(5,2) NOT NULL,
    perc_cbs NUMERIC(5,2) NOT NULL,
    perc_reduc_icms NUMERIC(5,2) NOT NULL,
    perc_reduc_piscofins NUMERIC(5,2) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (company_id, ano)
);

CREATE INDEX IF NOT EXISTS idx_simulador_aliquotas_company ON simulador_aliquotas(company_id);
