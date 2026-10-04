package demo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuthorizedMerchantOrder is returned only after the host merchant's existing
// authentication and order-ownership checks have succeeded.
type AuthorizedMerchantOrder struct {
	OrderID     string
	AmountCents int64
}

// MerchantCheckoutAuthorizer adapts the merchant's existing auth/session and
// order repository. Implementations must never trust identity/order ownership
// supplied only by an unverified client header or request body.
type MerchantCheckoutAuthorizer interface {
	AuthorizeCheckoutOrder(context.Context, *http.Request, string) (AuthorizedMerchantOrder, error)
}

type merchantCheckoutService struct {
	store      *PostgresStore
	authorizer MerchantCheckoutAuthorizer
	creator    ChargeCreator
	clockNow   func() time.Time
}

// ReserveChargeAttemptWithIdempotency binds a caller key to a durable attempt.
// Reusing a key with a different order is rejected. A new key after a confirmed
// EXPIRED attempt may create a new reservation; uncertain attempts are reused.
func (s *PostgresStore) ReserveChargeAttemptWithIdempotency(ctx context.Context, orderID, idempotencyKey string, now time.Time) (ChargeAttempt, error) {
	if orderID == "" || len(orderID) > 128 || len(idempotencyKey) < 8 || len(idempotencyKey) > 128 || strings.TrimSpace(idempotencyKey) != idempotencyKey {
		return ChargeAttempt{}, errors.New("invalid order ID or idempotency key")
	}
	fingerprint := sha256.Sum256([]byte(orderID))
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ChargeAttempt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var storedOrderID, attemptID string
	var storedFingerprint []byte
	err = tx.QueryRow(ctx, `SELECT order_id, request_fingerprint, attempt_id FROM psp_charge_idempotency_keys WHERE idempotency_key=$1 FOR UPDATE`, idempotencyKey).
		Scan(&storedOrderID, &storedFingerprint, &attemptID)
	if err == nil {
		if storedOrderID != orderID || len(storedFingerprint) != sha256.Size || string(storedFingerprint) != string(fingerprint[:]) {
			return ChargeAttempt{}, ErrIdempotencyConflict
		}
		var providerStatus string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(provider_status,'') FROM psp_charge_attempts WHERE attempt_id=$1`, attemptID).
			Scan(&providerStatus); err != nil {
			return ChargeAttempt{}, err
		}
		if providerStatus == "EXPIRED" {
			return ChargeAttempt{}, ErrChargeAttemptState
		}
		// Existing-key replay doesn't need the order lock; read the linked attempt
		// in this statement snapshot and return it without acquiring row locks.
		attempt, err := scanChargeAttempt(tx.QueryRow(ctx, `SELECT attempt_id, order_id, correlation_id, amount_cents, state FROM psp_charge_attempts WHERE attempt_id=$1`, attemptID))
		if err != nil {
			return ChargeAttempt{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ChargeAttempt{}, err
		}
		return attempt, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ChargeAttempt{}, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if _, lockErr := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "merchant-checkout:"+orderID); lockErr != nil {
			return ChargeAttempt{}, lockErr
		}
		err = tx.QueryRow(ctx, `SELECT order_id,request_fingerprint,attempt_id FROM psp_charge_idempotency_keys WHERE idempotency_key=$1 FOR UPDATE`, idempotencyKey).
			Scan(&storedOrderID, &storedFingerprint, &attemptID)
		if err == nil {
			if storedOrderID != orderID || len(storedFingerprint) != sha256.Size || string(storedFingerprint) != string(fingerprint[:]) {
				return ChargeAttempt{}, ErrIdempotencyConflict
			}
			attempt, err := scanChargeAttempt(tx.QueryRow(ctx, `SELECT attempt_id,order_id,correlation_id,amount_cents,state FROM psp_charge_attempts WHERE attempt_id=$1`, attemptID))
			if err != nil {
				return ChargeAttempt{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return ChargeAttempt{}, err
			}
			return attempt, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return ChargeAttempt{}, err
		}
	}
	if err == nil {
		return ChargeAttempt{}, ErrChargeInProgress
	}
	// All new keys for an order now hold its transaction-scoped advisory lock.
	// This serializes across backend processes before taking the order row lock.
	var amount int64
	if err := tx.QueryRow(ctx, `SELECT amount_cents FROM demo_orders WHERE order_id=$1 FOR UPDATE`, orderID).Scan(&amount); errors.Is(err, pgx.ErrNoRows) {
		return ChargeAttempt{}, ErrOrderNotFound
	} else if err != nil {
		return ChargeAttempt{}, err
	}
	// Order row serialization ensures a concurrent caller using this new key
	// must observe the durable key after waiting for the lock.
	var attempt ChargeAttempt
	var providerStatus string
	err = tx.QueryRow(ctx, `SELECT attempt_id, order_id, correlation_id, amount_cents, state, COALESCE(provider_status,'')
		FROM psp_charge_attempts WHERE order_id=$1 AND (state IN ('reserved','submitting','unknown','created') OR state='resolved')
		ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, orderID).Scan(
		&attempt.ID, &attempt.OrderID, &attempt.CorrelationID, &attempt.AmountCents, &attempt.State, &providerStatus)
	if err == nil && (attempt.State != ChargeAttemptResolved || providerStatus != "EXPIRED") {
		if attempt.State == ChargeAttemptResolved && providerStatus == "COMPLETED" {
			return ChargeAttempt{}, ErrOrderAlreadyPaid
		}
		if attempt.AmountCents != amount {
			return ChargeAttempt{}, ErrChargeAttemptState
		}
		if attempt.State == ChargeAttemptResolved && providerStatus == "ACTIVE" {
			return ChargeAttempt{}, ErrChargeInProgress
		}
	} else {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
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
		attempt = ChargeAttempt{ID: id, OrderID: orderID, CorrelationID: correlationID, AmountCents: amount, State: ChargeAttemptReserved}
		if _, err := tx.Exec(ctx, `INSERT INTO psp_charge_attempts
			(attempt_id, order_id, correlation_id, amount_cents, state, created_at, updated_at)
			VALUES ($1,$2,$3,$4,'reserved',$5,$5)`, id, orderID, correlationID, amount, now); err != nil {
			return ChargeAttempt{}, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO psp_charge_idempotency_keys(idempotency_key, order_id, request_fingerprint, attempt_id, created_at)
		VALUES ($1,$2,$3,$4,$5)`, idempotencyKey, orderID, fingerprint[:], attempt.ID, now); err != nil {
		return ChargeAttempt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChargeAttempt{}, err
	}
	return attempt, nil
}

func scanChargeAttempt(row rowScanner) (ChargeAttempt, error) {
	var attempt ChargeAttempt
	err := row.Scan(&attempt.ID, &attempt.OrderID, &attempt.CorrelationID, &attempt.AmountCents, &attempt.State)
	return attempt, err
}

// CreateCheckoutForChargeAttempt materializes/recoveries the client-facing
// session only from a durable PSP state. A terminal paid session has no QR.
func (s *PostgresStore) CreateCheckoutForChargeAttempt(ctx context.Context, attemptID string, now time.Time) (*checkout, string, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var attempt ChargeAttempt
	var providerStatus string
	var brCode string
	var chargeExpiresAt sql.NullTime
	err = tx.QueryRow(ctx, `SELECT attempt_id, order_id, correlation_id, amount_cents, state,
		COALESCE(provider_status,''), COALESCE(br_code,''), expires_at
		FROM psp_charge_attempts WHERE attempt_id=$1 FOR UPDATE`, attemptID).
		Scan(&attempt.ID, &attempt.OrderID, &attempt.CorrelationID, &attempt.AmountCents, &attempt.State, &providerStatus, &brCode, &chargeExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, ErrChargeAttemptState
	}
	if err != nil {
		return nil, "", false, err
	}
	if providerStatus == "COMPLETED" && attempt.State != ChargeAttemptResolved {
		return nil, "", false, ErrChargeInProgress
	}
	if providerStatus == "EXPIRED" && attempt.State != ChargeAttemptResolved {
		return nil, "", false, ErrChargeInProgress
	}
	var c checkout
	var token string
	var status string
	err = tx.QueryRow(ctx, `SELECT checkout_id, order_id, correlation_id, amount_cents, status, expires_at, COALESCE(br_code,'')
		FROM checkout_sessions WHERE attempt_id=$1 FOR UPDATE`, attemptID).
		Scan(&c.id, &c.orderID, &c.correlationID, &c.amount, &status, &c.expiresAt, &c.brCode)
	if err == nil {
		if providerStatus == "COMPLETED" && status != string(Paid) {
			status = string(Paid)
			c.brCode = ""
			if _, err := tx.Exec(ctx, `UPDATE checkout_sessions SET status='paid', br_code=NULL WHERE checkout_id=$1`, c.id); err != nil {
				return nil, "", false, err
			}
		} else if providerStatus == "EXPIRED" && status == string(Pending) {
			status = string(Expired)
			if _, err := tx.Exec(ctx, `UPDATE checkout_sessions SET status='expired' WHERE checkout_id=$1`, c.id); err != nil {
				return nil, "", false, err
			}
		}
		c.status = Status(status)
		if c.status == Paid || c.status == Expired {
			c.brCode = ""
			if _, err := tx.Exec(ctx, `UPDATE checkout_sessions SET br_code=NULL WHERE checkout_id=$1`, c.id); err != nil {
				return nil, "", false, err
			}
		}
		token, err = insertSessionToken(ctx, tx, &c, now)
		if err != nil {
			return nil, "", false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, "", false, err
		}
		return &c, token, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, err
	}
	switch {
	case providerStatus == "COMPLETED":
		c.status, c.expiresAt = Paid, now.Add(time.Hour)
	case providerStatus == "EXPIRED":
		c.status = Expired
		if chargeExpiresAt.Valid {
			c.expiresAt = chargeExpiresAt.Time
		} else {
			c.expiresAt = now
		}
	case (attempt.State == ChargeAttemptCreated || attempt.State == ChargeAttemptResolved) && providerStatus == "ACTIVE" && brCode != "" && chargeExpiresAt.Valid:
		c.status, c.expiresAt = Pending, chargeExpiresAt.Time
		if !now.Before(c.expiresAt) {
			c.status = Expired
		}
	default:
		return nil, "", false, ErrChargeInProgress
	}
	id, err := randomHex(16)
	if err != nil {
		return nil, "", false, err
	}
	c.id, c.orderID, c.correlationID, c.amount, c.brCode = id, attempt.OrderID, attempt.CorrelationID, attempt.AmountCents, brCode
	if c.status != Pending {
		c.brCode = ""
	}
	if _, err := tx.Exec(ctx, `INSERT INTO checkout_sessions
		(checkout_id, attempt_id, order_id, correlation_id, token_hash, amount_cents, status, expires_at, br_code, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10)`, id, attemptID, c.orderID, c.correlationID, make([]byte, sha256.Size), c.amount, c.status, c.expiresAt, c.brCode, now); err != nil {
		return nil, "", false, err
	}
	token, err = insertSessionToken(ctx, tx, &c, now)
	if err != nil {
		return nil, "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", false, err
	}
	return &c, token, true, nil
}

func insertSessionToken(ctx context.Context, tx pgx.Tx, c *checkout, now time.Time) (string, error) {
	token, err := randomHex(32)
	if err != nil {
		return "", err
	}
	c.tokenHash = sha256.Sum256([]byte(token))
	if _, err := tx.Exec(ctx, `UPDATE checkout_sessions SET token_hash=$2 WHERE checkout_id=$1`, c.id, c.tokenHash[:]); err != nil {
		return "", err
	}
	expiresAt := now.Add(24 * time.Hour)
	if c.status == Pending && c.expiresAt.Add(time.Hour).Before(expiresAt) {
		expiresAt = c.expiresAt.Add(time.Hour)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO checkout_session_tokens(checkout_id, token_hash, expires_at) VALUES ($1,$2,$3)`, c.id, c.tokenHash[:], expiresAt); err != nil {
		return "", err
	}
	return token, nil
}
