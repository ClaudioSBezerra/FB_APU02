-- Optimize aggregation performance by adding indexes on reg_c190
-- This table is heavily used in aggregations (summing values by job_id) and joins (with c100)
-- Adding these indexes significantly reduces processing time for large files

-- Index for filtering by job_id (used in WHERE clauses)
CREATE INDEX IF NOT EXISTS idx_reg_c190_job_id ON reg_c190(job_id);

-- Index for joining with reg_c100 (used in worker aggregation logic)
CREATE INDEX IF NOT EXISTS idx_reg_c190_id_pai_c100 ON reg_c190(id_pai_c100);
