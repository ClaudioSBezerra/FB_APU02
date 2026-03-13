-- Migration 025: Add performance indexes for auth and context queries
-- Created to fix 504 timeouts during login for users with complex associations

-- Index for finding companies by owner (Strategy A in Login)
CREATE INDEX IF NOT EXISTS idx_companies_owner_id ON companies(owner_id);

-- Indexes for finding context via user_environments (Strategy B in Login)
CREATE INDEX IF NOT EXISTS idx_user_environments_user_id ON user_environments(user_id);
CREATE INDEX IF NOT EXISTS idx_enterprise_groups_env_id ON enterprise_groups(environment_id);
CREATE INDEX IF NOT EXISTS idx_companies_group_id ON companies(group_id);

-- Additional useful indexes
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
