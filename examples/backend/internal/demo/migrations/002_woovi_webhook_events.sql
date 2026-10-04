ALTER TABLE checkout_sessions ADD COLUMN IF NOT EXISTS correlation_id TEXT;

UPDATE checkout_sessions SET correlation_id = checkout_id WHERE correlation_id IS NULL;

ALTER TABLE checkout_sessions ALTER COLUMN correlation_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS checkout_sessions_correlation_id_idx
    ON checkout_sessions (correlation_id);

CREATE TABLE IF NOT EXISTS webhook_events (
    event_hash BYTEA PRIMARY KEY CHECK (octet_length(event_hash) = 32),
    correlation_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    applied BOOLEAN NOT NULL DEFAULT FALSE,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
