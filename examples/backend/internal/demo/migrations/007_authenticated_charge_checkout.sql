CREATE TABLE IF NOT EXISTS psp_charge_idempotency_keys (
    idempotency_key TEXT PRIMARY KEY CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    order_id TEXT NOT NULL REFERENCES demo_orders(order_id),
    request_fingerprint BYTEA NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    attempt_id TEXT NOT NULL REFERENCES psp_charge_attempts(attempt_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE webhook_events DROP CONSTRAINT IF EXISTS webhook_events_outcome_check;
ALTER TABLE webhook_events ADD CONSTRAINT webhook_events_outcome_check
    CHECK (outcome IN ('applied', 'ignored', 'rejected_mismatch', 'processing'));

ALTER TABLE checkout_sessions ALTER COLUMN br_code DROP NOT NULL;
ALTER TABLE checkout_sessions ADD COLUMN IF NOT EXISTS attempt_id TEXT REFERENCES psp_charge_attempts(attempt_id);
CREATE UNIQUE INDEX IF NOT EXISTS checkout_sessions_attempt_id_idx
    ON checkout_sessions(attempt_id) WHERE attempt_id IS NOT NULL;

DROP INDEX IF EXISTS psp_charge_attempts_active_order_idx;
CREATE UNIQUE INDEX psp_charge_attempts_active_order_idx
    ON psp_charge_attempts(order_id)
    WHERE state IN ('reserved', 'submitting', 'unknown', 'created')
       OR (state = 'resolved' AND provider_status IS DISTINCT FROM 'EXPIRED');
