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
	err = tx.QueryRow(ctx, `SELECT checkout_id, order_id, amount_cents, status, expires_at, br_code
		FROM checkout_sessions WHERE order_id=$1 AND status='pending' AND expires_at>$2
		ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, orderID, now).Scan(
		&c.id, &c.orderID, &c.amount, &status, &c.expiresAt, &c.brCode)
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
	hash := sha256.Sum256([]byte(token))
	_, err = tx.Exec(ctx, `INSERT INTO checkout_sessions
		(checkout_id, order_id, token_hash, amount_cents, status, expires_at, br_code, created_at)
		VALUES ($1,$2,$3,$4,'pending',$5,$6,$7)`, id, orderID, hash[:], amount, expiresAt, brCode, now)
	if err != nil {
		return nil, "", false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO checkout_session_tokens(checkout_id, token_hash, expires_at) VALUES ($1,$2,$3)`, id, hash[:], expiresAt.Add(time.Hour)); err != nil {
		return nil, "", false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, "", false, err
	}
	c = checkout{id: id, tokenHash: hash, orderID: orderID, amount: amount, status: Pending, expiresAt: expiresAt, brCode: brCode}
	created = true
	return &c, token, created, nil
}

func (s *PostgresStore) Status(ctx context.Context, id string, tokenHash [32]byte, now time.Time) (*checkout, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	c, err := scanCheckout(tx.QueryRow(ctx, `SELECT c.checkout_id, t.token_hash, c.order_id, c.amount_cents, c.status, c.expires_at, c.br_code
		FROM checkout_sessions c JOIN checkout_session_tokens t ON t.checkout_id=c.checkout_id
		WHERE c.checkout_id=$1 AND t.token_hash=$2 AND t.expires_at>$3 FOR UPDATE OF c`, id, tokenHash[:], now))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && c.tokenHash != tokenHash) {
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

type rowScanner interface {
	Scan(...any) error
}

func scanCheckout(row rowScanner) (*checkout, error) {
	var c checkout
	var hash []byte
	var status string
	if err := row.Scan(&c.id, &hash, &c.orderID, &c.amount, &status, &c.expiresAt, &c.brCode); err != nil {
		return nil, err
	}
	if len(hash) != sha256.Size {
		return nil, fmt.Errorf("invalid token hash stored for checkout %s", c.id)
	}
	copy(c.tokenHash[:], hash)
	c.status = Status(status)
	return &c, nil
}
