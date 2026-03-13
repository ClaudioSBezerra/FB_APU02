-- Migration to delete user gilson.costa@hotmail.com and company 145c5e10-334b-4dc7-8255-ee2e8c83661b
-- This is a one-time cleanup operation requested by the user.

-- Delete the user (should cascade to user_companies if it exists)
DELETE FROM users WHERE email = 'gilson.costa@hotmail.com';
DELETE FROM users WHERE email = 'gilson.costa@hostmail.com'; -- Typo variant just in case

-- Delete the company (should cascade to related data if configured, otherwise might fail if FKs prevent it)
-- We will try to delete dependent data first to be safe if cascades aren't perfect.

-- Delete import_jobs for this company
DELETE FROM import_jobs WHERE company_id = '145c5e10-334b-4dc7-8255-ee2e8c83661b';

-- Delete the company
DELETE FROM companies WHERE id = '145c5e10-334b-4dc7-8255-ee2e8c83661b';
