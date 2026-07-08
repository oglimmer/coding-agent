-- Job provenance + durable logs, so a job can be analysed long after its worker
-- pod (1h TTL) is gone.
--   metadata: config snapshot captured at creation (platform commit, models,
--             review rounds, timeouts, worker image, verify command). JSONB so it
--             stays queryable as the set of fields evolves.
--   logs:     the full worker log, persisted by the watcher when the job ends, so
--             the frontend can show it even once the pod has been TTL-cleaned.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS logs     TEXT  NOT NULL DEFAULT '';
