-- The coding model(s) a job ran on, chosen per-job at request time. Nullable:
-- NULL means "the engine's deployment default was used" (older rows, and jobs
-- created before per-job model selection existed). model is the aider architect
-- model or the claude-code primary model; editor_model is aider's editor model
-- and stays NULL for the claude-code engine, which has no editor split.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS model TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS editor_model TEXT;
