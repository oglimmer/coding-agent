-- Whether the worker squash-merges the PR itself once it is approved and checks
-- are green, or stops at an approved-but-open PR and leaves the final merge to a
-- human. Chosen per-job at request time. Existing rows default to true so history
-- reflects the original always-auto-merge behaviour.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS auto_merge BOOLEAN NOT NULL DEFAULT true;
