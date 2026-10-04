ALTER TABLE psp_charge_attempts DROP CONSTRAINT IF EXISTS psp_charge_attempts_state_check;

ALTER TABLE psp_charge_attempts ADD CONSTRAINT psp_charge_attempts_state_check
    CHECK (state IN ('reserved', 'submitting', 'unknown', 'created', 'resolved', 'failed'));

ALTER TABLE psp_charge_attempts ADD COLUMN IF NOT EXISTS provider_status TEXT;

DROP INDEX IF EXISTS psp_charge_attempts_active_order_idx;

CREATE UNIQUE INDEX psp_charge_attempts_active_order_idx
    ON psp_charge_attempts(order_id)
    WHERE state IN ('reserved', 'submitting', 'unknown', 'created', 'resolved');
