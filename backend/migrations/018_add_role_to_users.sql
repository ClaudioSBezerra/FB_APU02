-- Add role column to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(50) DEFAULT 'user';

-- Update specific user to admin if exists
UPDATE users SET role = 'admin' WHERE email = 'claudio_bezerra@hotmail.com';
