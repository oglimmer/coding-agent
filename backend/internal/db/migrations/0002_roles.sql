-- Introduce a three-value role model (viewer < user < admin) to replace the
-- is_admin boolean. New accounts default to 'viewer' and must be promoted by an
-- admin before they can create jobs. This migration is idempotent.

ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'viewer';

-- Backfill from the legacy is_admin flag while it still exists: existing admins
-- stay admins; everyone who already had an account keeps write access as a
-- 'user' (they could already create jobs). Runs only until is_admin is dropped.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'users' AND column_name = 'is_admin'
    ) THEN
        UPDATE users SET role = CASE WHEN is_admin THEN 'admin' ELSE 'user' END;
    END IF;
END $$;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('viewer', 'user', 'admin'));

ALTER TABLE users DROP COLUMN IF EXISTS is_admin;
