-- Migration 064: Change raw_json from JSONB to TEXT
-- JSONB has a practical ~268 MB limit. PostgreSQL TEXT supports up to ~1 GB
-- via TOAST, allowing storage of large RFB responses (~298 MB observed).
ALTER TABLE rfb_requests
  ALTER COLUMN raw_json TYPE TEXT USING raw_json::TEXT;
