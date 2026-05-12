-- Ensure admin user exists and has a known password (123456)
-- This fixes login issues for claudio_bezerra@hotmail.com

DO $$
BEGIN
    -- Check if user exists
    IF EXISTS (SELECT 1 FROM users WHERE email = 'claudio_bezerra@hotmail.com') THEN
        -- Update existing user
        UPDATE users 
        SET password_hash = '$2a$14$Opb3Wt02JbSQbMLm.OQF8ObYr4UZh5h7S8KzCj1PfwLyjes6vFluC', -- 123456
            role = 'admin',
            is_verified = true,
            full_name = 'Claudio Bezerra (Admin)'
        WHERE email = 'claudio_bezerra@hotmail.com';
    ELSE
        -- Insert new user
        INSERT INTO users (email, password_hash, full_name, role, is_verified, trial_ends_at)
        VALUES (
            'claudio_bezerra@hotmail.com',
            '$2a$14$Opb3Wt02JbSQbMLm.OQF8ObYr4UZh5h7S8KzCj1PfwLyjes6vFluC', -- 123456
            'Claudio Bezerra (Admin)',
            'admin',
            true,
            NOW() + INTERVAL '365 days'
        );
    END IF;
END $$;
