package demo

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPostgresCheckoutLifecycle(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("set DATABASE_URL to run PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	orderID, err := randomHex(12)
	if err != nil {
		t.Fatal(err)
	}
	orderID = "integration-" + orderID
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, $2)`, orderID, 4412); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := store.pool.Exec(ctx, `DELETE FROM webhook_events WHERE correlation_id IN (SELECT correlation_id FROM checkout_sessions WHERE order_id=$1)`, orderID); err != nil {
			t.Errorf("cleanup webhook events: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM checkout_sessions WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup checkout sessions: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup demo order: %v", err)
		}
	})

	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	first, token, created, err := store.Create(ctx, orderID, now)
	if err != nil || !created {
		t.Fatalf("first create = created:%v err:%v", created, err)
	}
	second, rotated, created, err := store.Create(ctx, orderID, now.Add(time.Second))
	if err != nil || created || second.id != first.id || rotated == token {
		t.Fatalf("idempotent recovery = checkout:%v created:%v err:%v", second, created, err)
	}
	if _, err := store.Status(ctx, first.id, first.tokenHash, now); err != nil {
		t.Fatalf("parallel checkout-session bearer must remain valid: %v", err)
	}
	currentHash := second.tokenHash
	status, err := store.Status(ctx, second.id, currentHash, now)
	if err != nil || status.amount != 4412 || status.status != Pending {
		t.Fatalf("status = %#v err=%v", status, err)
	}
	expired, err := store.Status(ctx, second.id, currentHash, now.Add(16*time.Minute))
	if err != nil || expired.status != Expired {
		t.Fatalf("expired status = %#v err=%v", expired, err)
	}
	completed := WooviChargeEvent{Event: "OPENPIX:CHARGE_COMPLETED"}
	completed.Charge.CorrelationID = second.correlationID
	completed.Charge.Status = "COMPLETED"
	completed.Charge.Value = second.amount
	completed.Pix.Status = "CONFIRMED"
	mismatched := completed
	mismatched.Charge.Value--
	mismatchHash := sha256.Sum256([]byte("mismatched-event-" + orderID))
	result, err := store.ApplyChargeEvent(ctx, mismatched, mismatchHash)
	if err != nil || !result.Rejected {
		t.Fatalf("mismatched signed event should be quarantined: result=%#v err=%v", result, err)
	}
	var outcome string
	if err := store.pool.QueryRow(ctx, `SELECT outcome FROM webhook_events WHERE event_hash=$1`, mismatchHash[:]).Scan(&outcome); err != nil || outcome != "rejected_mismatch" {
		t.Fatalf("quarantined event outcome = %q err=%v", outcome, err)
	}
	completedHash := sha256.Sum256([]byte("completed-event-" + orderID))
	result, err = store.ApplyChargeEvent(ctx, completed, completedHash)
	if err != nil || !result.Applied {
		t.Fatalf("tardy completed event should correct expired to paid: result=%#v err=%v", result, err)
	}
	result, err = store.ApplyChargeEvent(ctx, completed, completedHash)
	if err != nil || !result.Duplicate {
		t.Fatalf("duplicate completed event should be deduplicated: result=%#v err=%v", result, err)
	}
	expiredEvent := WooviChargeEvent{Event: "OPENPIX:CHARGE_EXPIRED"}
	expiredEvent.Charge.CorrelationID = second.correlationID
	expiredEvent.Charge.Status = "EXPIRED"
	expiredEvent.Charge.Value = second.amount
	result, err = store.ApplyChargeEvent(ctx, expiredEvent, sha256.Sum256([]byte("late-expired-event-"+orderID)))
	if err != nil || !result.Ignored {
		t.Fatalf("out-of-order expired event must not revert paid: result=%#v err=%v", result, err)
	}
	paid, err := store.SimulatePaid(ctx, second.id, now.Add(17*time.Minute))
	if err != nil || paid.status != Paid {
		t.Fatalf("simulated paid = %#v err=%v", paid, err)
	}
}

func TestPostgresConcurrentSessionCreationReusesCheckout(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("set DATABASE_URL to run PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	orderID := "parallel-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, $2)`, orderID, 101); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := store.pool.Exec(ctx, `DELETE FROM webhook_events WHERE correlation_id IN (SELECT correlation_id FROM checkout_sessions WHERE order_id=$1)`, orderID); err != nil {
			t.Errorf("cleanup webhook events: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM checkout_sessions WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup checkout sessions: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup demo order: %v", err)
		}
	})

	const parallel = 8
	type result struct {
		id    string
		token string
		err   error
	}
	results := make(chan result, parallel)
	var wait sync.WaitGroup
	for range parallel {
		wait.Add(1)
		go func() {
			defer wait.Done()
			c, token, _, err := store.Create(ctx, orderID, time.Now())
			if err != nil {
				results <- result{err: err}
				return
			}
			results <- result{id: c.id, token: token}
		}()
	}
	wait.Wait()
	close(results)
	var checkoutID string
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if checkoutID == "" {
			checkoutID = result.id
		}
		if result.id != checkoutID {
			t.Fatalf("parallel create generated multiple checkouts: %s and %s", checkoutID, result.id)
		}
		if _, err := store.Status(ctx, checkoutID, sha256.Sum256([]byte(result.token)), time.Now()); err != nil {
			t.Fatalf("parallel session token invalid: %v", err)
		}
	}
}

func TestPostgresChargeAttemptReservationAndAmbiguousRecovery(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("set DATABASE_URL to run PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	orderID := "attempt-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, $2)`, orderID, 7599); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := store.pool.Exec(ctx, `DELETE FROM psp_charge_attempts WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup charge attempts: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup demo order: %v", err)
		}
	})

	now := time.Now().UTC()
	reserved, err := store.ReserveChargeAttempt(ctx, orderID, now)
	if err != nil || reserved.State != ChargeAttemptReserved || reserved.AmountCents != 7599 {
		t.Fatalf("reservation=%#v err=%v", reserved, err)
	}
	recovered, err := store.ReserveChargeAttempt(ctx, orderID, now.Add(time.Second))
	if err != nil || recovered.ID != reserved.ID || recovered.CorrelationID != reserved.CorrelationID {
		t.Fatalf("reservation recovery=%#v err=%v", recovered, err)
	}
	if err := store.MarkChargeAttemptSubmitting(ctx, reserved.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordChargeAttemptUnknown(ctx, reserved.ID, "timeout", now.Add(12*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkChargeAttemptSubmitting(ctx, reserved.ID, now.Add(13*time.Second)); !errors.Is(err, ErrChargeAttemptState) {
		t.Fatalf("ambiguous attempt must not be POSTed again: %v", err)
	}
	toReconcile, err := store.ListChargeAttemptsNeedingReconciliation(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, attempt := range toReconcile {
		if attempt.ID == reserved.ID && attempt.CorrelationID == reserved.CorrelationID && attempt.State == ChargeAttemptUnknown {
			found = true
		}
	}
	if !found {
		t.Fatal("unknown attempt missing from reconciliation queue")
	}
	charge := WooviCharge{CorrelationID: reserved.CorrelationID, Status: "ACTIVE", Value: reserved.AmountCents, BRCode: "pix-fixture", ExpiresAt: now.Add(15 * time.Minute)}
	if err := store.MarkChargeAttemptCreated(ctx, reserved.ID, charge, now.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	var state, brCode string
	if err := store.pool.QueryRow(ctx, `SELECT state, br_code FROM psp_charge_attempts WHERE attempt_id=$1`, reserved.ID).Scan(&state, &brCode); err != nil {
		t.Fatal(err)
	}
	if state != string(ChargeAttemptCreated) || brCode != charge.BRCode {
		t.Fatalf("persisted PSP outcome state=%s br_code=%s", state, brCode)
	}
}
