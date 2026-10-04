package demo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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

func TestPostgresWebhookResolvesChargeAttemptOutOfOrderWithoutCheckout(t *testing.T) {
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
	orderID := "attempt-webhook-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, $2)`, orderID, 7850); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := store.pool.Exec(ctx, `DELETE FROM webhook_events WHERE correlation_id IN (SELECT correlation_id FROM psp_charge_attempts WHERE order_id=$1)`, orderID); err != nil {
			t.Errorf("cleanup webhook events: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM psp_charge_attempts WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup charge attempts: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID); err != nil {
			t.Errorf("cleanup demo order: %v", err)
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
	claimed, err := store.ClaimChargeAttemptsForReconciliation(ctx, "stale-get-worker", 10, now.Add(time.Second), time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].ID != attempt.ID {
		t.Fatalf("claim for delayed reconciliation = %#v, err=%v", claimed, err)
	}
	makeEvent := func(kind, status string) WooviChargeEvent {
		event := WooviChargeEvent{Event: kind}
		event.Charge.CorrelationID = attempt.CorrelationID
		event.Charge.Value = attempt.AmountCents
		event.Charge.Status = status
		if status == "COMPLETED" {
			event.Pix.Status = "CONFIRMED"
		}
		return event
	}
	apply := func(event WooviChargeEvent, key string) WebhookResult {
		t.Helper()
		result, err := store.ApplyChargeEvent(ctx, event, sha256.Sum256([]byte(orderID+key)))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	expired := makeEvent("OPENPIX:CHARGE_EXPIRED", "EXPIRED")
	expiredHash := sha256.Sum256([]byte(orderID + "concurrent-expired"))
	const deliveries = 12
	var wg sync.WaitGroup
	results := make(chan WebhookResult, deliveries)
	errorsFound := make(chan error, deliveries)
	for i := 0; i < deliveries; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := store.ApplyChargeEvent(ctx, expired, expiredHash)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent webhook delivery: %v", err)
	}
	appliedCount, duplicateCount := 0, 0
	for result := range results {
		if result.Applied {
			appliedCount++
		}
		if result.Duplicate {
			duplicateCount++
		}
	}
	if appliedCount != 1 || duplicateCount != deliveries-1 {
		t.Fatalf("concurrent delivery applied=%d duplicate=%d, want 1/%d", appliedCount, duplicateCount, deliveries-1)
	}
	if result := apply(makeEvent("OPENPIX:CHARGE_EXPIRED", "EXPIRED"), "expired-duplicate-payload"); !result.Ignored {
		t.Fatalf("distinct duplicate state should be ignored: %#v", result)
	}
	if result := apply(makeEvent("OPENPIX:CHARGE_COMPLETED", "COMPLETED"), "completed"); !result.Applied {
		t.Fatalf("COMPLETED must supersede EXPIRED: %#v", result)
	}
	if result := apply(makeEvent("OPENPIX:CHARGE_EXPIRED", "EXPIRED"), "late-expired"); !result.Ignored {
		t.Fatalf("late EXPIRED must not downgrade COMPLETED: %#v", result)
	}
	var state, providerStatus string
	if err := store.pool.QueryRow(ctx, `SELECT state, provider_status FROM psp_charge_attempts WHERE attempt_id=$1`, attempt.ID).Scan(&state, &providerStatus); err != nil {
		t.Fatal(err)
	}
	if state != string(ChargeAttemptResolved) || providerStatus != "COMPLETED" {
		t.Fatalf("attempt state/status = %s/%s, want resolved/COMPLETED", state, providerStatus)
	}
	staleActive := WooviCharge{CorrelationID: attempt.CorrelationID, Value: attempt.AmountCents, Status: "ACTIVE", BRCode: "stale-pix", ExpiresAt: now.Add(15 * time.Minute)}
	if err := store.RecordChargeAttemptReconciliation(ctx, attempt.ID, claimed[0].LeaseToken, staleActive, now.Add(2*time.Second)); !errors.Is(err, ErrChargeAttemptState) {
		t.Fatalf("stale GET result must lose after webhook resolves attempt: %v", err)
	}
	if err := store.MarkChargeAttemptCreated(ctx, attempt.ID, staleActive, now.Add(3*time.Second)); !errors.Is(err, ErrChargeAttemptState) {
		t.Fatalf("late POST response must not regress webhook-resolved attempt: %v", err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT state, provider_status FROM psp_charge_attempts WHERE attempt_id=$1`, attempt.ID).Scan(&state, &providerStatus); err != nil {
		t.Fatal(err)
	}
	if state != string(ChargeAttemptResolved) || providerStatus != "COMPLETED" {
		t.Fatalf("stale result changed attempt to %s/%s", state, providerStatus)
	}
	var checkoutCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM checkout_sessions WHERE order_id=$1`, orderID).Scan(&checkoutCount); err != nil {
		t.Fatal(err)
	}
	if checkoutCount != 0 {
		t.Fatalf("webhook must not issue checkout sessions, count=%d", checkoutCount)
	}
}

func TestPostgresChargeCreateCrashWindowsRecoverWithoutDuplicatePost(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set DATABASE_URL to run PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	orderBeforePost := "crash-before-post-" + time.Now().Format("150405.000000000")
	orderAfterAcceptance := "crash-after-accept-" + time.Now().Format("150405.000000000")
	for _, orderID := range []string{orderBeforePost, orderAfterAcceptance} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, $2)`, orderID, 6981); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, orderID := range []string{orderBeforePost, orderAfterAcceptance} {
			if _, err := store.pool.Exec(ctx, `DELETE FROM webhook_events WHERE correlation_id IN (SELECT correlation_id FROM psp_charge_attempts WHERE order_id=$1)`, orderID); err != nil {
				t.Errorf("cleanup webhook events: %v", err)
			}
			if _, err := store.pool.Exec(ctx, `DELETE FROM psp_charge_attempts WHERE order_id=$1`, orderID); err != nil {
				t.Errorf("cleanup charge attempts: %v", err)
			}
			if _, err := store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID); err != nil {
				t.Errorf("cleanup demo order: %v", err)
			}
		}
		store.Close()
	})

	now := time.Now().UTC()
	beforePost, err := store.ReserveChargeAttempt(ctx, orderBeforePost, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkChargeAttemptSubmitting(ctx, beforePost.ID, now); err != nil {
		t.Fatal(err)
	}
	accepted, err := store.ReserveChargeAttempt(ctx, orderAfterAcceptance, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkChargeAttemptSubmitting(ctx, accepted.ID, now); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	charges := make(map[string]int64)
	postCount, getCount := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPost:
			postCount++
			var request struct {
				CorrelationID string `json:"correlationID"`
				Value         int64  `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode fixture create request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			charges[request.CorrelationID] = request.Value
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"charge":{"correlationID":"` + request.CorrelationID + `","status":"ACTIVE","brCode":"fixture-pix","expiresDate":"` + now.Add(15*time.Minute).Format(time.RFC3339) + `"}}`))
		case http.MethodGet:
			getCount++
			correlationID := r.URL.Path[len("/api/v1/charge/"):]
			value, exists := charges[correlationID]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"charge":{"correlationID":"` + correlationID + `","value":` + strconv.FormatInt(value, 10) + `,"status":"ACTIVE","brCode":"fixture-pix","expiresDate":"` + now.Add(15*time.Minute).Format(time.RFC3339) + `"}}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client, err := NewWooviChargeClient("fixture-app-id", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	// Simulate process loss after durable submitting but before sending POST.
	store.Close()
	store, err = OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	beforePost.State = ChargeAttemptSubmitting
	if _, err := ReconcileChargeAttempt(ctx, store, client, beforePost, now.Add(time.Second)); err == nil {
		t.Fatal("lookup for a request never sent must remain unresolved")
	}
	var state string
	if err := store.pool.QueryRow(ctx, `SELECT state FROM psp_charge_attempts WHERE attempt_id=$1`, beforePost.ID).Scan(&state); err != nil || state != string(ChargeAttemptSubmitting) {
		t.Fatalf("pre-POST crash state=%q err=%v", state, err)
	}

	// Woovi accepts the POST, then the database becomes unavailable before the
	// response can be persisted. Recovery is GET-only with the same correlationID.
	createdCharge, err := client.CreateCharge(ctx, accepted.CorrelationID, accepted.AmountCents)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := store.MarkChargeAttemptCreated(ctx, accepted.ID, createdCharge, now.Add(2*time.Second)); err == nil {
		t.Fatal("persisting PSP response through a closed database should fail")
	}
	store, err = OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	accepted.State = ChargeAttemptSubmitting
	recovered, err := ReconcileChargeAttempt(ctx, store, client, accepted, now.Add(3*time.Second))
	if err != nil || recovered.Status != "ACTIVE" {
		t.Fatalf("GET recovery = %#v, err=%v", recovered, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if postCount != 1 || getCount != 2 {
		t.Fatalf("POST count=%d GET count=%d, want 1/2", postCount, getCount)
	}
	if err := store.pool.QueryRow(ctx, `SELECT state FROM psp_charge_attempts WHERE attempt_id=$1`, accepted.ID).Scan(&state); err != nil || state != string(ChargeAttemptResolved) {
		t.Fatalf("recovered attempt state=%q err=%v", state, err)
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

func TestPostgresAmbiguousChargeCanOnlyResolveThroughLookup(t *testing.T) {
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
	orderID := "lookup-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1,$2)`, orderID, 3210); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM psp_charge_attempts WHERE order_id=$1`, orderID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID)
	})
	now := time.Now().UTC()
	attempt, err := store.ReserveChargeAttempt(ctx, orderID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkChargeAttemptSubmitting(ctx, attempt.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordChargeAttemptUnknown(ctx, attempt.ID, "timeout", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	attempt.State = ChargeAttemptUnknown // a restarted worker loads this state from the recovery queue
	lookups := 0
	responseMode := "not-found"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lookups++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/charge/"+attempt.CorrelationID {
			t.Errorf("lookup request %s %s", r.Method, r.URL.Path)
		}
		switch responseMode {
		case "not-found":
			w.WriteHeader(http.StatusNotFound)
		case "mismatch":
			_, _ = w.Write([]byte(`{"charge":{"correlationID":"` + attempt.CorrelationID + `","value":1,"status":"ACTIVE","brCode":"pix-reconciled","expiresDate":"` + now.Add(15*time.Minute).Format(time.RFC3339) + `"}}`))
		default:
			_, _ = w.Write([]byte(`{"charge":{"correlationID":"` + attempt.CorrelationID + `","value":3210,"status":"ACTIVE","brCode":"pix-reconciled","expiresDate":"` + now.Add(15*time.Minute).Format(time.RFC3339) + `"}}`))
		}
	}))
	defer server.Close()
	client, err := NewWooviChargeClient("fixture-app-id", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := ReconcilePendingChargeAttempts(ctx, store, client, 20, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].AttemptID != attempt.ID || outcomes[0].Err == nil || outcomes[0].Charge != nil {
		t.Fatalf("not-found queue outcome=%#v", outcomes)
	}
	var state string
	if err := store.pool.QueryRow(ctx, `SELECT state FROM psp_charge_attempts WHERE attempt_id=$1`, attempt.ID).Scan(&state); err != nil || state != string(ChargeAttemptUnknown) {
		t.Fatalf("404 changed attempt state to %q: %v", state, err)
	}
	responseMode = "mismatch"
	if _, err := ReconcileChargeAttempt(ctx, store, client, attempt, now.Add(4*time.Second)); err == nil {
		t.Fatal("amount mismatch must remain unresolved")
	}
	if err := store.pool.QueryRow(ctx, `SELECT state FROM psp_charge_attempts WHERE attempt_id=$1`, attempt.ID).Scan(&state); err != nil || state != string(ChargeAttemptUnknown) {
		t.Fatalf("mismatched response changed attempt state to %q: %v", state, err)
	}
	responseMode = "active"
	charge, err := ReconcileChargeAttempt(ctx, store, client, attempt, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if lookups != 3 || charge.Status != "ACTIVE" || charge.Value != attempt.AmountCents {
		t.Fatalf("lookup count=%d charge=%#v", lookups, charge)
	}
	var providerStatus string
	if err := store.pool.QueryRow(ctx, `SELECT state, provider_status FROM psp_charge_attempts WHERE attempt_id=$1`, attempt.ID).Scan(&state, &providerStatus); err != nil {
		t.Fatal(err)
	}
	if state != string(ChargeAttemptResolved) || providerStatus != "ACTIVE" {
		t.Fatalf("persisted reconciliation state=%s provider_status=%s", state, providerStatus)
	}
	attempt.State = ChargeAttemptResolved
	if _, err := ReconcileChargeAttempt(ctx, store, client, attempt, now.Add(6*time.Second)); !errors.Is(err, ErrChargeAttemptState) {
		t.Fatalf("resolved attempt must not be reconciled/posted as unknown again: %v", err)
	}
}

func TestPostgresConcurrentChargeReservationsShareOneAttempt(t *testing.T) {
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
	orderID := "reserve-race-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1,$2)`, orderID, 8801); err != nil {
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

	const workers = 12
	type outcome struct {
		attempt ChargeAttempt
		err     error
	}
	results := make(chan outcome, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			attempt, err := store.ReserveChargeAttempt(ctx, orderID, time.Now().UTC())
			results <- outcome{attempt: attempt, err: err}
		}()
	}
	wait.Wait()
	close(results)
	var first ChargeAttempt
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if first.ID == "" {
			first = result.attempt
		}
		if result.attempt.ID != first.ID || result.attempt.CorrelationID != first.CorrelationID {
			t.Fatalf("concurrent reservations diverged: first=%#v next=%#v", first, result.attempt)
		}
	}
	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM psp_charge_attempts WHERE order_id=$1`, orderID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("active PSP attempt rows=%d, want one", count)
	}

	// A durable reserved row is known to be pre-network. After a restart,
	// concurrent callers may all recover it, but only one may cross the
	// submitting boundary and become eligible to issue the single POST.
	recoveryStore, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recoveryStore.Close)
	const resumptions = 12
	transitionResults := make(chan error, resumptions)
	for range resumptions {
		wait.Add(1)
		go func() {
			defer wait.Done()
			transitionResults <- recoveryStore.MarkChargeAttemptSubmitting(ctx, first.ID, time.Now().UTC())
		}()
	}
	wait.Wait()
	close(transitionResults)
	submittingTransitions := 0
	for err := range transitionResults {
		if err == nil {
			submittingTransitions++
		} else if !errors.Is(err, ErrChargeAttemptState) {
			t.Fatalf("reserved resume returned unexpected error: %v", err)
		}
	}
	if submittingTransitions != 1 {
		t.Fatalf("reserved resume crossed submitting boundary %d times, want exactly once", submittingTransitions)
	}
	resumed, err := recoveryStore.ReserveChargeAttempt(ctx, orderID, time.Now().UTC())
	if err != nil || resumed.ID != first.ID || resumed.State != ChargeAttemptSubmitting {
		t.Fatalf("recovered attempt = %#v, err=%v", resumed, err)
	}
	if err := recoveryStore.MarkChargeAttemptSubmitting(ctx, resumed.ID, time.Now().UTC()); !errors.Is(err, ErrChargeAttemptState) {
		t.Fatalf("uncertain submitting attempt must never re-enter POST boundary: %v", err)
	}
}

func TestPostgresReconciliationClaimsLeaseAndBackoffRecover(t *testing.T) {
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
	orderID := "lease-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1,$2)`, orderID, 912); err != nil {
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
	attempt, err := store.ReserveChargeAttempt(ctx, orderID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkChargeAttemptSubmitting(ctx, attempt.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordChargeAttemptUnknown(ctx, attempt.ID, "timeout", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimChargeAttemptsForReconciliation(ctx, "worker-1", 10, now.Add(2*time.Second), 30*time.Second)
	if err != nil || len(first) != 1 {
		t.Fatalf("first lease = %#v err=%v", first, err)
	}
	second, err := store.ClaimChargeAttemptsForReconciliation(ctx, "worker-2", 10, now.Add(3*time.Second), 30*time.Second)
	if err != nil || len(second) != 0 {
		t.Fatalf("concurrent claim should skip leased row: %#v err=%v", second, err)
	}
	if err := store.ScheduleChargeAttemptReconciliationRetry(ctx, attempt.ID, first[0].LeaseToken, "lookup_not_found", now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	tooSoon, err := store.ClaimChargeAttemptsForReconciliation(ctx, "worker-3", 10, now.Add(8*time.Second), time.Second)
	if err != nil || len(tooSoon) != 0 {
		t.Fatalf("claim before backoff elapsed = %#v err=%v", tooSoon, err)
	}
	due, err := store.ClaimChargeAttemptsForReconciliation(ctx, "worker-4", 10, now.Add(10*time.Second), time.Second)
	if err != nil || len(due) != 1 || due[0].ID != attempt.ID {
		t.Fatalf("claim after backoff = %#v err=%v", due, err)
	}
	if err := store.ScheduleChargeAttemptReconciliationRetry(ctx, attempt.ID, first[0].LeaseToken, "stale_worker", now.Add(11*time.Second)); !errors.Is(err, ErrChargeAttemptState) {
		t.Fatalf("stale worker must not update released lease: %v", err)
	}
	if err := store.ScheduleChargeAttemptReconciliationRetry(ctx, attempt.ID, due[0].LeaseToken, "lookup_not_found", now.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := store.pool.QueryRow(ctx, `SELECT reconcile_attempts FROM psp_charge_attempts WHERE attempt_id=$1`, attempt.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("reconcile_attempts=%d, want 2", attempts)
	}
	// An abandoned worker's lease expires and becomes claimable after a crash.
	crashLease, err := store.ClaimChargeAttemptsForReconciliation(ctx, "worker-crashed", 10, now.Add(22*time.Second), time.Second)
	if err != nil || len(crashLease) != 1 {
		t.Fatalf("due attempt not claimed: %#v err=%v", crashLease, err)
	}
	duplicateCrashClaim, err := store.ClaimChargeAttemptsForReconciliation(ctx, "worker-other", 10, now.Add(22*time.Second), time.Second)
	if err != nil || len(duplicateCrashClaim) != 0 {
		t.Fatalf("active lease was claimed twice: %#v err=%v", duplicateCrashClaim, err)
	}
	postCrash, err := store.ClaimChargeAttemptsForReconciliation(ctx, "worker-recovered", 10, now.Add(24*time.Second), time.Second)
	if err != nil || len(postCrash) != 1 {
		t.Fatalf("expired lease was not recoverable: %#v err=%v", postCrash, err)
	}
}
