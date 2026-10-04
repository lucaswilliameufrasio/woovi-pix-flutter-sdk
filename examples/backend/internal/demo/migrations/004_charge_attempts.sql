CREATE TABLE IF NOT EXISTS psp_charge_attempts (
    attempt_id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES demo_orders(order_id),
    correlation_id TEXT NOT NULL UNIQUE,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    state TEXT NOT NULL CHECK (state IN ('reserved', 'submitting', 'unknown', 'created', 'failed')),
    br_code TEXT,
    expires_at TIMESTAMPTZ,
    request_started_at TIMESTAMPTZ,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS psp_charge_attempts_active_order_idx
    ON psp_charge_attempts(order_id)
    WHERE state IN ('reserved', 'submitting', 'unknown', 'created');

CREATE INDEX IF NOT EXISTS psp_charge_attempts_recovery_idx
    ON psp_charge_attempts(state, updated_at)
    WHERE state IN ('submitting', 'unknown');
