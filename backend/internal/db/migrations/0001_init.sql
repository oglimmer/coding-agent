-- Initial schema for the coding-agent platform.
-- gen_random_uuid() comes from pgcrypto.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    oidc_sub      TEXT UNIQUE,
    email         TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    token_version INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS repos (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner       TEXT NOT NULL,
    name        TEXT NOT NULL,
    base_branch TEXT NOT NULL DEFAULT 'main',
    added_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner, name)
);

CREATE TABLE IF NOT EXISTS jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id       UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feature       TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'checking'
                  CHECK (status IN ('checking', 'rejected', 'running', 'success', 'failed')),
    k8s_job_name  TEXT NOT NULL DEFAULT '',
    branch        TEXT NOT NULL DEFAULT '',
    pr_url        TEXT NOT NULL DEFAULT '',
    reason        TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_jobs_user_id ON jobs (user_id);
CREATE INDEX IF NOT EXISTS idx_jobs_repo_id ON jobs (repo_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at DESC);
