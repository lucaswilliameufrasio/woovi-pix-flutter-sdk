package demo

import (
	"context"
	"crypto/sha256"
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
	defer store.Close()

	orderID, err := randomHex(12)
	if err != nil {
		t.Fatal(err)
	}
	orderID = "integration-" + orderID
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, $2)`, orderID, 4412); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM checkout_sessions WHERE order_id=$1`, orderID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID)
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
	defer store.Close()
	orderID := "parallel-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, $2)`, orderID, 101); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM checkout_sessions WHERE order_id=$1`, orderID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID)
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
