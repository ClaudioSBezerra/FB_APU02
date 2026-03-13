-- Add expected_lines column for frontend-provided line count (avoids re-counting file)
ALTER TABLE import_jobs ADD COLUMN IF NOT EXISTS expected_lines INT DEFAULT 0;
