-- Migration: Ensure MASTER hierarchy and Link Admin User
-- Created: 2026-02-02
-- Description: Garante que a estrutura MASTER existe e vincula claudio_bezerra@hotmail.com a ela.

DO $$
DECLARE
    v_user_id UUID;
    v_env_id UUID;
    v_group_id UUID;
    v_company_id UUID;
BEGIN
    -- 1. Buscar ID do Usuário Admin
    SELECT id INTO v_user_id FROM users WHERE email = 'claudio_bezerra@hotmail.com';

    -- Se o usuário não existir, não fazemos nada (ou poderíamos criar, mas melhor não arriscar senha)
    IF v_user_id IS NOT NULL THEN
        
        -- 2. Garantir Ambiente MASTER
        SELECT id INTO v_env_id FROM environments WHERE name = 'MASTER';
        IF v_env_id IS NULL THEN
            INSERT INTO environments (name, description) VALUES ('MASTER', 'Ambiente Principal de Administração') RETURNING id INTO v_env_id;
            RAISE NOTICE 'Ambiente MASTER criado: %', v_env_id;
        ELSE
            RAISE NOTICE 'Ambiente MASTER já existe: %', v_env_id;
        END IF;

        -- 3. Garantir Grupo MASTER
        SELECT id INTO v_group_id FROM enterprise_groups WHERE name = 'MASTER' AND environment_id = v_env_id;
        IF v_group_id IS NULL THEN
            INSERT INTO enterprise_groups (environment_id, name, description) VALUES (v_env_id, 'MASTER', 'Grupo Corporativo') RETURNING id INTO v_group_id;
            RAISE NOTICE 'Grupo MASTER criado: %', v_group_id;
        ELSE
            RAISE NOTICE 'Grupo MASTER já existe: %', v_group_id;
        END IF;

        -- 4. Garantir Empresa MASTER
        SELECT id INTO v_company_id FROM companies WHERE name = 'MASTER' AND group_id = v_group_id;
        IF v_company_id IS NULL THEN
            INSERT INTO companies (group_id, name, trade_name) VALUES (v_group_id, 'MASTER', 'MASTER Corporation') RETURNING id INTO v_company_id;
            RAISE NOTICE 'Empresa MASTER criada: %', v_company_id;
        ELSE
             RAISE NOTICE 'Empresa MASTER já existe: %', v_company_id;
        END IF;

        -- 5. Garantir Vínculo (User <-> Environment)
        IF NOT EXISTS (SELECT 1 FROM user_environments WHERE user_id = v_user_id AND environment_id = v_env_id) THEN
            INSERT INTO user_environments (user_id, environment_id, role) VALUES (v_user_id, v_env_id, 'admin');
            RAISE NOTICE 'Vínculo User-Ambiente criado para %', v_user_id;
        ELSE
            RAISE NOTICE 'Vínculo User-Ambiente já existe.';
        END IF;

    ELSE
        RAISE NOTICE 'Usuário claudio_bezerra@hotmail.com não encontrado. Nenhuma ação realizada.';
    END IF;
END $$;
