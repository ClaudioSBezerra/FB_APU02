-- Add 'used' column to verification_tokens for password reset flow
ALTER TABLE verification_tokens ADD COLUMN IF NOT EXISTS used BOOLEAN DEFAULT false;
