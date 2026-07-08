-- Per-repo fast inner-loop test command: what the worker passes to aider's
-- --auto-test as the after-every-edit convergence signal. Distinct from
-- verify_command (the authoritative, possibly-heavy pre-PR gate) so an owner can
-- give a cheap loop command without making the heavy gate run on every edit.
-- Empty string = the worker detects it from the repo's manifests.
ALTER TABLE repos ADD COLUMN IF NOT EXISTS test_command TEXT NOT NULL DEFAULT '';
