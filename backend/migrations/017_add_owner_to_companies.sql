-- Add owner_id to companies table to support shared environment isolation
-- In the "Test Environment", multiple users share the same Group, so we need to know who owns which company.

ALTER TABLE companies ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_companies_owner ON companies(owner_id);
