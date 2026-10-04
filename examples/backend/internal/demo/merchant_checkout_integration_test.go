package demo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testMerchantCheckoutAuthorizer struct {
	orders map[string]int64
}

func (a testMerchantCheckoutAuthorizer) AuthorizeCheckoutOrder(_ context.Context, r *http.Request, orderID string) (AuthorizedMerchantOrder, error) {
	if r.Header.Get("X-Merchant-Session") != "trusted-fixture-session" {
		return AuthorizedMerchantOrder{}, ErrCheckoutUnauthorized
	}
	amount, ok := a.orders[orderID]
	if !ok {
		return AuthorizedMerchantOrder{}, ErrOrderNotFound
	}
	return AuthorizedMerchantOrder{OrderID: orderID, AmountCents: amount}, nil
}

func TestPostgresMerchantCheckoutIdempotencyAndExpiredRetry(t *testing.T) {
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
	suffix := time.Now().Format("150405.000000000")
	orderOne, orderTwo, paidOrder := "merchant-checkout-a-"+suffix, "merchant-checkout-b-"+suffix, "merchant-checkout-paid-"+suffix
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, 2599), ($2, 4800), ($3, 8703)`, orderOne, orderTwo, paidOrder); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, orderID := range []string{orderOne, orderTwo, paidOrder} {
			if _, err := store.pool.Exec(ctx, `DELETE FROM webhook_events WHERE correlation_id IN (SELECT correlation_id FROM psp_charge_attempts WHERE order_id=$1)`, orderID); err != nil {
				t.Errorf("cleanup events: %v", err)
			}
			if _, err := store.pool.Exec(ctx, `DELETE FROM checkout_sessions WHERE order_id=$1`, orderID); err != nil {
				t.Errorf("cleanup sessions: %v", err)
			}
			if _, err := store.pool.Exec(ctx, `DELETE FROM psp_charge_idempotency_keys WHERE order_id=$1`, orderID); err != nil {
				t.Errorf("cleanup idempotency keys: %v", err)
			}
			if _, err := store.pool.Exec(ctx, `DELETE FROM psp_charge_attempts WHERE order_id=$1`, orderID); err != nil {
				t.Errorf("cleanup attempts: %v", err)
			}
			if _, err := store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID); err != nil {
				t.Errorf("cleanup orders: %v", err)
			}
		}
	})

	now := time.Now().UTC()
	var mu sync.Mutex
	postCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/charge" {
			t.Errorf("PSP request = %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			CorrelationID string `json:"correlationID"`
			Value         int64  `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode PSP request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		postCount++
		mu.Unlock()
		if request.Value == 8703 {
			event := WooviChargeEvent{Event: "OPENPIX:CHARGE_COMPLETED"}
			event.Charge.CorrelationID, event.Charge.Value, event.Charge.Status = request.CorrelationID, request.Value, "COMPLETED"
			event.Pix.Status = "CONFIRMED"
			result, err := store.ApplyChargeEvent(r.Context(), event, sha256.Sum256([]byte("early-complete-"+request.CorrelationID)))
			if err != nil || (!result.Applied && !result.Ignored) {
				t.Errorf("apply completed webhook before POST response: result=%#v err=%v", result, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"charge": map[string]any{
			"correlationID": request.CorrelationID,
			"status":        "ACTIVE",
			"brCode":        "merchant-fixture-pix",
			"expiresDate":   now.Add(15 * time.Minute).Format(time.RFC3339),
		}})
	}))
	defer server.Close()
	client, err := NewWooviChargeClient("fixture-only-app-id", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	hosted := NewServerWithOptions(store, func() time.Time { return now }, nil, false)
	if err := hosted.ConfigureMerchantCheckout(testMerchantCheckoutAuthorizer{orders: map[string]int64{orderOne: 2599, orderTwo: 4800, paidOrder: 8703}}, client); err != nil {
		t.Fatal(err)
	}
	handler := hosted.Handler()

	unauthorized := merchantCheckoutRequest(handler, orderOne, "idem-key-order-0001", false)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	first := merchantCheckoutRequest(handler, orderOne, "idem-key-order-0001", true)
	if first.Code != http.StatusCreated {
		t.Fatalf("first checkout status=%d body=%s", first.Code, first.Body.String())
	}
	var firstBody map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	checkoutID := firstBody["checkout_id"]
	if firstBody["status"] != "pending" || firstBody["pix_copy_paste"] != "merchant-fixture-pix" {
		t.Fatalf("first checkout response = %#v", firstBody)
	}

	replay := merchantCheckoutRequest(handler, orderOne, "idem-key-order-0001", true)
	if replay.Code != http.StatusOK {
		t.Fatalf("idempotent replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayBody map[string]any
	if err := json.Unmarshal(replay.Body.Bytes(), &replayBody); err != nil {
		t.Fatal(err)
	}
	if replayBody["checkout_id"] != checkoutID || replayBody["access_token"] == firstBody["access_token"] {
		t.Fatalf("idempotency replay did not recover session with rotated token: %#v", replayBody)
	}

	conflict := merchantCheckoutRequest(handler, orderTwo, "idem-key-order-0001", true)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("key reuse across orders status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	var correlationID string
	if err := store.pool.QueryRow(ctx, `SELECT correlation_id FROM psp_charge_attempts WHERE order_id=$1`, orderOne).Scan(&correlationID); err != nil {
		t.Fatal(err)
	}
	expired := WooviChargeEvent{Event: "OPENPIX:CHARGE_EXPIRED"}
	expired.Charge.CorrelationID, expired.Charge.Value, expired.Charge.Status = correlationID, 2599, "EXPIRED"
	if result, err := store.ApplyChargeEvent(ctx, expired, sha256.Sum256([]byte("expired-"+correlationID))); err != nil || !result.Applied {
		t.Fatalf("expire original attempt result=%#v err=%v", result, err)
	}

	retry := merchantCheckoutRequest(handler, orderOne, "idem-key-order-0002", true)
	if retry.Code != http.StatusCreated {
		t.Fatalf("new idempotency key after confirmed expiry status=%d body=%s", retry.Code, retry.Body.String())
	}
	var retryBody map[string]any
	if err := json.Unmarshal(retry.Body.Bytes(), &retryBody); err != nil {
		t.Fatal(err)
	}
	if retryBody["status"] != "pending" || retryBody["checkout_id"] == checkoutID {
		t.Fatalf("expired order retry response = %#v", retryBody)
	}
	paid := merchantCheckoutRequest(handler, paidOrder, "idem-key-paid-0001", true)
	if paid.Code != http.StatusCreated {
		t.Fatalf("early-completed order status=%d body=%s", paid.Code, paid.Body.String())
	}
	var paidBody map[string]any
	if err := json.Unmarshal(paid.Body.Bytes(), &paidBody); err != nil {
		t.Fatal(err)
	}
	if paidBody["status"] != "paid" || paidBody["pix_copy_paste"] != nil {
		t.Fatalf("completed-before-session response must be paid without QR: %#v", paidBody)
	}
	paidReplay := merchantCheckoutRequest(handler, paidOrder, "idem-key-paid-0001", true)
	if paidReplay.Code != http.StatusOK {
		t.Fatalf("paid idempotent replay status=%d body=%s", paidReplay.Code, paidReplay.Body.String())
	}
	var paidReplayBody map[string]any
	if err := json.Unmarshal(paidReplay.Body.Bytes(), &paidReplayBody); err != nil {
		t.Fatal(err)
	}
	if paidReplayBody["status"] != "paid" || paidReplayBody["pix_copy_paste"] != nil || paidReplayBody["checkout_id"] != paidBody["checkout_id"] {
		t.Fatalf("paid replay must preserve no-QR paid session: %#v", paidReplayBody)
	}
	var paidCorrelationID string
	if err := store.pool.QueryRow(ctx, `SELECT correlation_id FROM psp_charge_attempts WHERE order_id=$1`, paidOrder).Scan(&paidCorrelationID); err != nil {
		t.Fatal(err)
	}
	completed := WooviChargeEvent{Event: "OPENPIX:CHARGE_COMPLETED"}
	completed.Charge.CorrelationID, completed.Charge.Value, completed.Charge.Status = paidCorrelationID, 8703, "COMPLETED"
	completed.Pix.Status = "CONFIRMED"
	if result, err := store.ApplyChargeEvent(ctx, completed, sha256.Sum256([]byte("completed-again-"+paidCorrelationID))); err != nil || !result.Ignored {
		t.Fatalf("completed webhook for no-QR paid session result=%#v err=%v", result, err)
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+paidBody["checkout_id"].(string), nil)
	statusRequest.Header.Set("Authorization", "Bearer "+paidReplayBody["access_token"].(string))
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"status":"paid"`) {
		t.Fatalf("no-QR paid status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}
	paidRetry := merchantCheckoutRequest(handler, paidOrder, "idem-key-paid-0002", true)
	if paidRetry.Code != http.StatusConflict || !strings.Contains(paidRetry.Body.String(), "ORDER_ALREADY_PAID") {
		t.Fatalf("new key for paid order status=%d body=%s", paidRetry.Code, paidRetry.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if postCount != 3 {
		t.Fatalf("provider POST count=%d, want initial + expiry retry + early-completed order", postCount)
	}
}

func TestPostgresMerchantCheckoutConcurrentSameKeyPostsOnce(t *testing.T) {
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
	orderID := "merchant-concurrent-" + time.Now().Format("150405.000000000")
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id, amount_cents) VALUES ($1, 3100)`, orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM webhook_events WHERE correlation_id IN (SELECT correlation_id FROM psp_charge_attempts WHERE order_id=$1)`, orderID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM checkout_sessions WHERE order_id=$1`, orderID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM psp_charge_idempotency_keys WHERE order_id=$1`, orderID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM psp_charge_attempts WHERE order_id=$1`, orderID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, orderID)
	})
	var postCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&postCount, 1)
		var request struct {
			CorrelationID string `json:"correlationID"`
			Value         int64  `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode charge request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"charge": map[string]any{
			"correlationID": request.CorrelationID,
			"status":        "ACTIVE",
			"value":         request.Value,
			"brCode":        "concurrent-fixture-pix",
			"expiresDate":   time.Now().Add(10 * time.Minute).Format(time.RFC3339),
		}})
	}))
	defer server.Close()
	client, err := NewWooviChargeClient("fixture-only", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	hosted := NewServerWithOptions(store, time.Now, nil, false)
	if err := hosted.ConfigureMerchantCheckout(testMerchantCheckoutAuthorizer{orders: map[string]int64{orderID: 3100}}, client); err != nil {
		t.Fatal(err)
	}
	handler := hosted.Handler()
	otherStore, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherStore.Close)
	otherHost := NewServerWithOptions(otherStore, time.Now, nil, false)
	if err := otherHost.ConfigureMerchantCheckout(testMerchantCheckoutAuthorizer{orders: map[string]int64{orderID: 3100}}, client); err != nil {
		t.Fatal(err)
	}
	handlers := []http.Handler{handler, otherHost.Handler()}
	const callers = 12
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- merchantCheckoutRequest(handlers[i%len(handlers)], orderID, "idem-concurrent-0001", true)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	created, replayed, conflicts := 0, 0, 0
	checkoutIDs := map[string]bool{}
	for response := range results {
		switch response.Code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			replayed++
		case http.StatusConflict:
			conflicts++
			if !strings.Contains(response.Body.String(), "PAYMENT_PROCESSING") && !strings.Contains(response.Body.String(), "IDEMPOTENCY_CONFLICT") {
				t.Fatalf("unexpected conflict: %s", response.Body.String())
			}
			continue
		case http.StatusInternalServerError:
			t.Fatalf("internal transaction failure under checkout concurrency: %s", response.Body.String())
		default:
			t.Fatalf("concurrent checkout status=%d body=%s", response.Code, response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		checkoutIDs[body["checkout_id"].(string)] = true
	}
	if created != 1 || created+replayed+conflicts != callers || len(checkoutIDs) != 1 || atomic.LoadInt32(&postCount) != 1 {
		t.Fatalf("created=%d replayed=%d conflicts=%d checkouts=%d provider_posts=%d", created, replayed, conflicts, len(checkoutIDs), postCount)
	}
}

func merchantCheckoutRequest(handler http.Handler, orderID, idempotencyKey string, authenticated bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/merchant/checkout-sessions", strings.NewReader(`{"order_id":"`+orderID+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	if authenticated {
		request.Header.Set("X-Merchant-Session", "trusted-fixture-session")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
