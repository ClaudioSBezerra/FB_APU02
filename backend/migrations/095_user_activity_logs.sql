CREATE TABLE user_activity_logs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id)     ON DELETE CASCADE,
    company_id       UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    module           VARCHAR(100) NOT NULL,
    visited_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    duration_seconds INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_user_activity_user_date    ON user_activity_logs(user_id,    visited_at DESC);
CREATE INDEX idx_user_activity_company_date ON user_activity_logs(company_id, visited_at DESC);
CREATE INDEX idx_user_activity_module       ON user_activity_logs(company_id, module);
