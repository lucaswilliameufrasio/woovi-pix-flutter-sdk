package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestPostgresReconciliationWorkerRunsBoundedGETOnlyCycles(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set DATABASE_URL to run PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	orderID := "worker-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1,$2)`, orderID, 5623); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := store.pool.Exec(ctx, `DELETE FROM psp_charge_attempts WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup attempts: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup order: %v", err)
		}
	})
	now := time.Now().UTC()
	attempt, err := store.ReserveChargeAttempt(ctx, orderID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkChargeAttemptSubmitting(ctx, attempt.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordChargeAttemptUnknown(ctx, attempt.ID, "test_timeout", now); err != nil {
		t.Fatal(err)
	}
	var getCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/charge/"+attempt.CorrelationID {
			t.Errorf("worker unexpectedly used %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		getCount.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"charge": map[string]any{
			"correlationID": attempt.CorrelationID,
			"value":         attempt.AmountCents,
			"status":        "ACTIVE",
			"brCode":        "worker-fixture-pix",
			"expiresDate":   now.Add(15 * time.Minute).Format(time.RFC3339),
		}})
	}))
	defer server.Close()
	client, err := NewWooviChargeClient("fixture-only-app-id", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	cycleResults := make(chan ReconciliationCycle, 1)
	worker, err := NewChargeReconciliationWorker(store, client, 10*time.Millisecond, 1, DefaultReconciliationRetryPolicy(), time.Now, func(cycle ReconciliationCycle) {
		select {
		case cycleResults <- cycle:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() { workerDone <- worker.Run(workerCtx) }()
	select {
	case cycle := <-cycleResults:
		if cycle.Claimed != 1 || cycle.Resolved != 1 || cycle.Failed != 0 || cycle.Err != nil {
			t.Fatalf("worker cycle = %#v", cycle)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconciliation worker did not run a cycle")
	}
	stopWorker()
	if err := <-workerDone; err != nil {
		t.Fatalf("worker shutdown: %v", err)
	}
	if got := getCount.Load(); got != 1 {
		t.Fatalf("worker issued %d GET requests, want 1", got)
	}
	var state, providerStatus string
	if err := store.pool.QueryRow(ctx, `SELECT state, provider_status FROM psp_charge_attempts WHERE attempt_id=$1`, attempt.ID).Scan(&state, &providerStatus); err != nil {
		t.Fatal(err)
	}
	if state != string(ChargeAttemptResolved) || providerStatus != "ACTIVE" {
		t.Fatalf("worker persisted %s/%s", state, providerStatus)
	}
}
