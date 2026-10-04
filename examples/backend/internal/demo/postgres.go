package demo

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type PostgresStore struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	store := &PostgresStore{pool: pool}
	if err := store.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

func (s *PostgresStore) Migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73008601)`); err != nil {
		return fmt.Errorf("lock demo schema migrations: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS demo_schema_migrations
		(name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		var applied bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM demo_schema_migrations WHERE name=$1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		script, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(script)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO demo_schema_migrations(name) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit demo schema migrations: %w", err)
	}
	return nil
}

func (s *PostgresStore) Create(ctx context.Context, orderID string, now time.Time) (*checkout, string, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, "", false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `DELETE FROM checkout_session_tokens WHERE expires_at <= $1`, now); err != nil {
		return nil, "", false, err
	}
	var amount int64
	if err = tx.QueryRow(ctx, `SELECT amount_cents FROM demo_orders WHERE order_id=$1 FOR UPDATE`, orderID).Scan(&amount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", false, ErrOrderNotFound
		}
		return nil, "", false, err
	}
	var c checkout
	var status string
	err = tx.QueryRow(ctx, `SELECT checkout_id, order_id, correlation_id, amount_cents, status, expires_at, br_code
		FROM checkout_sessions WHERE order_id=$1 AND status='pending' AND expires_at>$2
		ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, orderID, now).Scan(
		&c.id, &c.orderID, &c.correlationID, &c.amount, &status, &c.expiresAt, &c.brCode)
	created := false
	if err == nil {
		token, tokenErr := randomHex(32)
		if tokenErr != nil {
			return nil, "", false, tokenErr
		}
		c.tokenHash = sha256.Sum256([]byte(token))
		if _, err = tx.Exec(ctx, `UPDATE checkout_sessions SET token_hash=$2 WHERE checkout_id=$1`, c.id, c.tokenHash[:]); err != nil {
			return nil, "", false, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO checkout_session_tokens(checkout_id, token_hash, expires_at) VALUES ($1,$2,$3)`, c.id, c.tokenHash[:], c.expiresAt.Add(time.Hour)); err != nil {
			return nil, "", false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, "", false, err
		}
		c.status = Status(status)
		return &c, token, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, err
	}
	id, err := randomHex(16)
	if err != nil {
		return nil, "", false, err
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, "", false, err
	}
	expiresAt := now.Add(15 * time.Minute)
	brCode := "000201-DEMO-PIX-" + id
	correlationID := id
	statusValue := string(Pending)
	hash := sha256.Sum256([]byte(token))
	_, err = tx.Exec(ctx, `INSERT INTO checkout_sessions
		(checkout_id, order_id, correlation_id, token_hash, amount_cents, status, expires_at, br_code, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, orderID, correlationID, hash[:], amount, statusValue, expiresAt, brCode, now)
	if err != nil {
		return nil, "", false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO checkout_session_tokens(checkout_id, token_hash, expires_at) VALUES ($1,$2,$3)`, id, hash[:], expiresAt.Add(time.Hour)); err != nil {
		return nil, "", false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, "", false, err
	}
	c = checkout{id: id, tokenHash: hash, orderID: orderID, correlationID: id, amount: amount, status: Pending, expiresAt: expiresAt, brCode: brCode}
	created = true
	return &c, token, created, nil
}

func (s *PostgresStore) Status(ctx context.Context, id string, tokenHash [32]byte, now time.Time) (*checkout, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var grantCheckoutID string
	if err := tx.QueryRow(ctx, `SELECT checkout_id FROM checkout_session_tokens WHERE token_hash=$1 AND expires_at>$2`, tokenHash[:], now).Scan(&grantCheckoutID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCheckoutUnauthorized
	} else if err != nil {
		return nil, err
	}
	if grantCheckoutID != id {
		return nil, ErrCheckoutNotFound
	}
	c, err := scanCheckout(tx.QueryRow(ctx, `SELECT c.checkout_id, t.token_hash, c.order_id, c.correlation_id, c.amount_cents, c.status, c.expires_at, c.br_code
		FROM checkout_sessions c JOIN checkout_session_tokens t ON t.checkout_id=c.checkout_id
		WHERE c.checkout_id=$1 AND t.token_hash=$2 AND t.expires_at>$3 FOR UPDATE OF c`, id, tokenHash[:], now))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCheckoutNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.status == Pending && !now.Before(c.expiresAt) {
		c.status = Expired
		if _, err = tx.Exec(ctx, `UPDATE checkout_sessions SET status='expired' WHERE checkout_id=$1 AND status='pending'`, id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *PostgresStore) SimulatePaid(ctx context.Context, id string, _ time.Time) (*checkout, error) {
	var c checkout
	var status string
	err := s.pool.QueryRow(ctx, `UPDATE checkout_sessions SET status='paid'
		WHERE checkout_id=$1 RETURNING checkout_id, order_id, amount_cents, status, expires_at, br_code`, id).Scan(
		&c.id, &c.orderID, &c.amount, &status, &c.expiresAt, &c.brCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCheckoutNotFound
	}
	if err != nil {
		return nil, err
	}
	c.status = Status(status)
	return &c, nil
}

func (s *PostgresStore) ApplyChargeEvent(ctx context.Context, event WooviChargeEvent, eventHash [32]byte) (WebhookResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return WebhookResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `INSERT INTO webhook_events(event_hash, correlation_id, event_type)
		VALUES ($1,$2,$3) ON CONFLICT (event_hash) DO NOTHING`, eventHash[:], event.Charge.CorrelationID, event.Event)
	if err != nil {
		return WebhookResult{}, err
	}
	if tag.RowsAffected() == 0 {
		if err = tx.Commit(ctx); err != nil {
			return WebhookResult{}, err
		}
		return WebhookResult{Duplicate: true}, nil
	}
	var c checkout
	var status string
	err = tx.QueryRow(ctx, `SELECT checkout_id, order_id, correlation_id, amount_cents, status, expires_at, br_code
		FROM checkout_sessions WHERE correlation_id=$1 FOR UPDATE`, event.Charge.CorrelationID).Scan(
		&c.id, &c.orderID, &c.correlationID, &c.amount, &status, &c.expiresAt, &c.brCode)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return WebhookResult{}, err
		}
		return WebhookResult{Ignored: true}, nil
	}
	if err != nil {
		return WebhookResult{}, err
	}
	c.status = Status(status)
	if isChargeStateEvent(event.Event) && (event.Charge.Value != c.amount ||
		(event.Event == "OPENPIX:CHARGE_COMPLETED" && (event.Charge.Status != "COMPLETED" || event.Pix.Status != "CONFIRMED")) ||
		(event.Event == "OPENPIX:CHARGE_EXPIRED" && event.Charge.Status != "EXPIRED")) {
		if _, err = tx.Exec(ctx, `UPDATE webhook_events SET outcome='rejected_mismatch' WHERE event_hash=$1`, eventHash[:]); err != nil {
			return WebhookResult{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return WebhookResult{}, err
		}
		return WebhookResult{Rejected: true}, nil
	}
	applied := false
	switch event.Event {
	case "OPENPIX:CHARGE_COMPLETED":
		if c.status != Paid {
			if _, err = tx.Exec(ctx, `UPDATE checkout_sessions SET status='paid' WHERE checkout_id=$1`, c.id); err != nil {
				return WebhookResult{}, err
			}
			applied = true
		}
	case "OPENPIX:CHARGE_EXPIRED":
		if c.status == Pending {
			if _, err = tx.Exec(ctx, `UPDATE checkout_sessions SET status='expired' WHERE checkout_id=$1 AND status='pending'`, c.id); err != nil {
				return WebhookResult{}, err
			}
			applied = true
		}
	}
	outcome := "ignored"
	if applied {
		outcome = "applied"
	}
	if _, err = tx.Exec(ctx, `UPDATE webhook_events SET applied=$2, outcome=$3 WHERE event_hash=$1`, eventHash[:], applied, outcome); err != nil {
		return WebhookResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Applied: applied, Ignored: !applied}, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanCheckout(row rowScanner) (*checkout, error) {
	var c checkout
	var hash []byte
	var status string
	if err := row.Scan(&c.id, &hash, &c.orderID, &c.correlationID, &c.amount, &status, &c.expiresAt, &c.brCode); err != nil {
		return nil, err
	}
	if len(hash) != sha256.Size {
		return nil, fmt.Errorf("invalid token hash stored for checkout %s", c.id)
	}
	copy(c.tokenHash[:], hash)
	c.status = Status(status)
	return &c, nil
}
