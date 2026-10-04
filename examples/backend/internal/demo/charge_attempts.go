package demo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type ChargeAttemptState string

const (
	ChargeAttemptReserved   ChargeAttemptState = "reserved"
	ChargeAttemptSubmitting ChargeAttemptState = "submitting"
	ChargeAttemptUnknown    ChargeAttemptState = "unknown"
	ChargeAttemptCreated    ChargeAttemptState = "created"
	ChargeAttemptFailed     ChargeAttemptState = "failed"
)

var ErrChargeAttemptState = errors.New("invalid charge attempt state transition")

type ChargeAttempt struct {
	ID            string
	OrderID       string
	CorrelationID string
	AmountCents   int64
	State         ChargeAttemptState
}

type ChargeCreator interface {
	CreateCharge(context.Context, string, int64) (WooviCharge, error)
}

// ReserveChargeAttempt commits an immutable amount and PSP correlation ID before
// any network request. At most one active PSP attempt may exist for an order.
func (s *PostgresStore) ReserveChargeAttempt(ctx context.Context, orderID string, now time.Time) (ChargeAttempt, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ChargeAttempt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var amount int64
	if err := tx.QueryRow(ctx, `SELECT amount_cents FROM demo_orders WHERE order_id=$1 FOR UPDATE`, orderID).Scan(&amount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ChargeAttempt{}, ErrOrderNotFound
		}
		return ChargeAttempt{}, err
	}
	var existing ChargeAttempt
	err = tx.QueryRow(ctx, `SELECT attempt_id, order_id, correlation_id, amount_cents, state
		FROM psp_charge_attempts WHERE order_id=$1 AND state IN ('reserved','submitting','unknown','created')
		ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, orderID).Scan(
		&existing.ID, &existing.OrderID, &existing.CorrelationID, &existing.AmountCents, &existing.State)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return ChargeAttempt{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ChargeAttempt{}, err
	}
	id, err := randomHex(16)
	if err != nil {
		return ChargeAttempt{}, err
	}
	correlationID, err := randomHex(16)
	if err != nil {
		return ChargeAttempt{}, err
	}
	attempt := ChargeAttempt{ID: id, OrderID: orderID, CorrelationID: correlationID, AmountCents: amount, State: ChargeAttemptReserved}
	if _, err := tx.Exec(ctx, `INSERT INTO psp_charge_attempts
		(attempt_id, order_id, correlation_id, amount_cents, state, created_at, updated_at)
		VALUES ($1,$2,$3,$4,'reserved',$5,$5)`, id, orderID, correlationID, amount, now); err != nil {
		return ChargeAttempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChargeAttempt{}, err
	}
	return attempt, nil
}

// MarkChargeAttemptSubmitting commits the uncertainty boundary before the PSP
// POST. Once this succeeds, automatic POST retries are forbidden.
func (s *PostgresStore) MarkChargeAttemptSubmitting(ctx context.Context, id string, now time.Time) error {
	result, err := s.pool.Exec(ctx, `UPDATE psp_charge_attempts SET state='submitting', request_started_at=$2, updated_at=$2
		WHERE attempt_id=$1 AND state='reserved'`, id, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrChargeAttemptState
	}
	return nil
}

// RecordChargeAttemptUnknown records an ambiguous network outcome. Recovery
// must reconcile this correlation ID; it must not issue a fresh create POST.
func (s *PostgresStore) RecordChargeAttemptUnknown(ctx context.Context, id, safeErrorCode string, now time.Time) error {
	if len(safeErrorCode) > 64 {
		return errors.New("error code too long")
	}
	result, err := s.pool.Exec(ctx, `UPDATE psp_charge_attempts SET state='unknown', last_error_code=$2, updated_at=$3
		WHERE attempt_id=$1 AND state='submitting'`, id, safeErrorCode, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrChargeAttemptState
	}
	return nil
}

func (s *PostgresStore) MarkChargeAttemptCreated(ctx context.Context, id string, charge WooviCharge, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var expectedCorrelation string
	var state ChargeAttemptState
	if err := tx.QueryRow(ctx, `SELECT correlation_id, state FROM psp_charge_attempts WHERE attempt_id=$1 FOR UPDATE`, id).
		Scan(&expectedCorrelation, &state); err != nil {
		return err
	}
	if (state != ChargeAttemptSubmitting && state != ChargeAttemptUnknown) ||
		charge.CorrelationID != expectedCorrelation || charge.Status != "ACTIVE" || charge.BRCode == "" || !charge.ExpiresAt.After(now) {
		return ErrChargeAttemptState
	}
	_, err = tx.Exec(ctx, `UPDATE psp_charge_attempts SET state='created', br_code=$2, expires_at=$3, updated_at=$4 WHERE attempt_id=$1`, id, charge.BRCode, charge.ExpiresAt, now)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (s *PostgresStore) ListChargeAttemptsNeedingReconciliation(ctx context.Context, limit int) ([]ChargeAttempt, error) {
	if limit < 1 || limit > 500 {
		return nil, errors.New("limit must be between 1 and 500")
	}
	rows, err := s.pool.Query(ctx, `SELECT attempt_id, order_id, correlation_id, amount_cents, state
		FROM psp_charge_attempts WHERE state IN ('submitting','unknown') ORDER BY updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attempts []ChargeAttempt
	for rows.Next() {
		var attempt ChargeAttempt
		if err := rows.Scan(&attempt.ID, &attempt.OrderID, &attempt.CorrelationID, &attempt.AmountCents, &attempt.State); err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}
