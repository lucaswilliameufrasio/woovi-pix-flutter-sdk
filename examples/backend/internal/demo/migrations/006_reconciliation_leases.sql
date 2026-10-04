ALTER TABLE psp_charge_attempts
    ADD COLUMN IF NOT EXISTS reconcile_attempts INTEGER NOT NULL DEFAULT 0 CHECK (reconcile_attempts >= 0),
    ADD COLUMN IF NOT EXISTS next_reconcile_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS reconcile_lease_token TEXT,
    ADD COLUMN IF NOT EXISTS reconcile_lease_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_reconcile_error TEXT;

CREATE INDEX IF NOT EXISTS psp_charge_attempts_due_reconciliation_idx
    ON psp_charge_attempts(next_reconcile_at, updated_at)
    WHERE state IN ('submitting', 'unknown');
