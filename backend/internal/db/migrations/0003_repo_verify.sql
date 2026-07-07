-- Per-repo verify command: the authoritative build/lint/test gate the worker runs
-- locally BEFORE opening a PR. Catching a lint error or build break here means a
-- cheap local fix round instead of a failed CI check that blocks merge and burns
-- review rounds. Empty string = fall back to the worker's model-guessed test
-- command (the prior behaviour), so existing repos are unaffected.
ALTER TABLE repos ADD COLUMN IF NOT EXISTS verify_command TEXT NOT NULL DEFAULT '';
