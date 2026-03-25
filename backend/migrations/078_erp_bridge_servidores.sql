-- Migration 078: ERP Bridge — tabela de servidores configurados
-- O daemon registra seus servidores ao iniciar, garantindo que o dropdown
-- do trigger manual sempre mostre todos os servidores configurados,
-- mesmo que nunca tenham importado dados.

CREATE TABLE IF NOT EXISTS erp_bridge_servidores (
  company_id  UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
  nome        TEXT NOT NULL,
  updated_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  PRIMARY KEY (company_id, nome)
);
