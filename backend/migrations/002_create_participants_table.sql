CREATE TABLE IF NOT EXISTS participants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    cod_part VARCHAR(255),
    nome VARCHAR(255),
    cod_pais VARCHAR(50),
    cnpj VARCHAR(20),
    cpf VARCHAR(20),
    ie VARCHAR(50),
    cod_mun VARCHAR(50),
    suframa VARCHAR(50),
    endereco TEXT,
    numero VARCHAR(50),
    complemento TEXT,
    bairro VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_participants_job_id ON participants(job_id);