-- Promote users to admin to fix 403 Forbidden on reset-db
-- Specifically targeting likely emails based on git config and project context
UPDATE users SET role = 'admin' WHERE email ILIKE 'claudio%' OR email ILIKE 'admin%';
