CREATE TABLE IF NOT EXISTS demo_orders (
    order_id TEXT PRIMARY KEY,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    currency TEXT NOT NULL DEFAULT 'BRL' CHECK (currency = 'BRL')
);

INSERT INTO demo_orders (order_id, amount_cents, currency)
VALUES ('demo-order-1', 2599, 'BRL')
ON CONFLICT (order_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS checkout_sessions (
    checkout_id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES demo_orders(order_id),
    token_hash BYTEA NOT NULL CHECK (octet_length(token_hash) = 32),
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'paid', 'expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    br_code TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS checkout_sessions_pending_order_idx
    ON checkout_sessions (order_id, created_at DESC)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS checkout_session_tokens (
    checkout_id TEXT NOT NULL REFERENCES checkout_sessions(checkout_id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL CHECK (octet_length(token_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (checkout_id, token_hash)
);

CREATE INDEX IF NOT EXISTS checkout_session_tokens_expiry_idx
    ON checkout_session_tokens (expires_at);
