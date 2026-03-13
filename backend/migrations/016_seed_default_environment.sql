-- Seed Default Environment and Enterprise Group
-- This ensures we have a standard environment for new trial users to attach to.

DO $$
DECLARE
    v_env_id UUID;
BEGIN
    -- 1. Ensure Default Environment exists
    SELECT id INTO v_env_id FROM environments WHERE name = 'Ambiente de Testes';
    
    IF v_env_id IS NULL THEN
        INSERT INTO environments (name, description)
        VALUES ('Ambiente de Testes', 'Ambiente padrão para usuários em período de avaliação (Trial)')
        RETURNING id INTO v_env_id;
    END IF;

    -- 2. Ensure Default Enterprise Group exists linked to that environment
    IF NOT EXISTS (SELECT 1 FROM enterprise_groups WHERE name = 'Grupo de Empresas Testes' AND environment_id = v_env_id) THEN
        INSERT INTO enterprise_groups (environment_id, name, description)
        VALUES (v_env_id, 'Grupo de Empresas Testes', 'Grupo empresarial padrão para empresas de teste');
    END IF;
END $$;
