ALTER TABLE webhook_events ADD COLUMN IF NOT EXISTS outcome TEXT NOT NULL DEFAULT 'ignored'
    CHECK (outcome IN ('applied', 'ignored', 'rejected_mismatch'));
