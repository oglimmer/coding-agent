-- Which coding engine implemented a job. 'aider' is the original worker (aider +
-- DeepSeek); 'claude-code' is the alternative worker running Claude Code against
-- a DeepSeek backend. The engine is chosen per-job at request time; existing rows
-- default to 'aider' so history stays accurate.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS engine TEXT NOT NULL DEFAULT 'aider'
    CHECK (engine IN ('aider', 'claude-code'));
