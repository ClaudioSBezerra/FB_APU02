-- Permite bloquear usuários sem excluí-los (mantém histórico/vínculos)
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_blocked BOOLEAN NOT NULL DEFAULT false;
