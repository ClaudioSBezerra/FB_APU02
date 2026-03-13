-- Migration 061: adiciona preferred_company_id em user_environments
-- Permite vincular um usuário a uma empresa específica dentro do grupo,
-- sem precisar ser owner_id da empresa.

ALTER TABLE user_environments
  ADD COLUMN IF NOT EXISTS preferred_company_id UUID REFERENCES companies(id) ON DELETE SET NULL;
