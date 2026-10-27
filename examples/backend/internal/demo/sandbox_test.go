package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSandboxConfigurationFailsClosed(t *testing.T) {
	s := NewServer()
	for _, base := range []string{"", "https://api.woovi.com", SandboxAPIBaseURL + "/", "http://localhost:8080"} {
		if err := s.ConfigureSandboxCheckout(context.Background(), strings.Repeat("t", 32), "fixture", base, false); err == nil {
			t.Fatalf("accepted unsafe URL %q", base)
		}
	}
	if err := s.ConfigureSandboxCheckout(context.Background(), strings.Repeat("t", 32), "fixture", SandboxAPIBaseURL, true); err == nil {
		t.Fatal("accepted demo PSP")
	}
	if err := s.ConfigureSandboxCheckout(context.Background(), strings.Repeat("t", 32), "fixture", SandboxAPIBaseURL, false); err == nil {
		t.Fatal("accepted memory store")
	}
}

func TestSandboxAuthorizer(t *testing.T) {
	a := SandboxAuthorizer{Token: strings.Repeat("t", 32)}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if _, err := a.AuthorizeCheckoutOrder(context.Background(), r, "sandbox-order-1"); err != ErrCheckoutUnauthorized {
		t.Fatalf("missing auth: %v", err)
	}
	r.Header.Set("X-Sandbox-Session", a.Token)
	if _, err := a.AuthorizeCheckoutOrder(context.Background(), r, "other"); err != ErrOrderNotFound {
		t.Fatalf("other order: %v", err)
	}
	order, err := a.AuthorizeCheckoutOrder(context.Background(), r, "sandbox-order-1")
	if err != nil || order.AmountCents != 2599 {
		t.Fatalf("authorized order=%#v err=%v", order, err)
	}
	generatedID := "sandbox-order-0123456789abcdef0123456789abcdef"
	if _, err := a.AuthorizeCheckoutOrder(context.Background(), r, generatedID); err != nil {
		t.Fatalf("generated test order was not authorized: %v", err)
	}
	for _, invalidOrderID := range []string{"sandbox-order-random", "sandbox-order-0123", "another-order-0123456789abcdef0123456789abcdef"} {
		if _, err := a.AuthorizeCheckoutOrder(context.Background(), r, invalidOrderID); err != ErrOrderNotFound {
			t.Fatalf("invalid sandbox order %q: %v", invalidOrderID, err)
		}
	}
}

func TestPostgresSandboxReplayAndPolling(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set DATABASE_URL")
	}
	ctx := context.Background()
	store, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	generatedOrderID := ""
	// Sandbox has a fixed order: clean only that test order, never other data.
	cleanup := func() {
		for _, query := range []string{
			`DELETE FROM webhook_events WHERE correlation_id IN (SELECT correlation_id FROM psp_charge_attempts WHERE order_id='sandbox-order-1')`,
			`DELETE FROM checkout_sessions WHERE order_id='sandbox-order-1'`,
			`DELETE FROM psp_charge_idempotency_keys WHERE order_id='sandbox-order-1'`,
			`DELETE FROM psp_charge_attempts WHERE order_id='sandbox-order-1'`,
			`DELETE FROM demo_orders WHERE order_id='sandbox-order-1'`,
		} {
			if _, err := store.pool.Exec(ctx, query); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
		if generatedOrderID != "" {
			if _, err := store.pool.Exec(ctx, `DELETE FROM demo_orders WHERE order_id=$1`, generatedOrderID); err != nil {
				t.Errorf("cleanup generated sandbox order: %v", err)
			}
		}
	}
	cleanup()
	defer cleanup()
	s := NewServerWithOptions(store, time.Now, nil, false)
	token := strings.Repeat("t", 32)
	for _, config := range []struct{ token, appID string }{{"short", "fixture"}, {token, ""}} {
		if err := s.ConfigureSandboxCheckout(ctx, config.token, config.appID, SandboxAPIBaseURL, false); err == nil {
			t.Fatal("accepted missing sandbox credentials")
		}
	}
	if err := s.ConfigureSandboxCheckout(ctx, token, "fixture", SandboxAPIBaseURL, false); err != nil {
		t.Fatal(err)
	}
	var posts, gets, testPayments atomic.Int32
	var correlation atomic.Value
	var completed atomic.Bool
	var lookupFailure atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "fixture" {
			t.Error("missing server-side AppID")
		}
		if r.Method == http.MethodPost {
			posts.Add(1)
			var body struct {
				CorrelationID string `json:"correlationID"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			correlation.Store(body.CorrelationID)
			w.WriteHeader(http.StatusInternalServerError) // accepted but ambiguous
			return
		}
		if r.URL.Path == "/openpix/testing" {
			if r.URL.Query().Get("transactionID") != "sandbox-transaction" {
				t.Errorf("sandbox test payment transactionID = %q", r.URL.Query().Get("transactionID"))
			}
			testPayments.Add(1)
			completed.Store(true)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		gets.Add(1)
		if lookupFailure.Load() == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if lookupFailure.Load() == 2 {
			_, _ = w.Write([]byte(`not-json`))
			return
		}
		status := "ACTIVE"
		if completed.Load() {
			status = "COMPLETED"
		}
		w.Header().Set("Content-Type", "application/json")
		value := 2599
		if lookupFailure.Load() == 3 {
			value = 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"charge": map[string]any{
			"correlationID": correlation.Load(), "value": value, "status": status,
			"transactionID": "sandbox-transaction",
			"brCode":        "sandbox-fixture", "expiresDate": time.Now().Add(time.Hour).Format(time.RFC3339),
		}})
	}))
	defer fixture.Close()
	client, err := NewWooviChargeClient("fixture", fixture.URL, fixture.Client())
	if err != nil {
		t.Fatal(err)
	}
	// Protocol fake only at PSP boundary; real config/store/router/client remain.
	s.sandboxLookup, s.merchantCheckout.creator = client, client
	handler := s.Handler()
	consoleRequest := httptest.NewRequest(http.MethodGet, "/sandbox", nil)
	consoleResponse := httptest.NewRecorder()
	handler.ServeHTTP(consoleResponse, consoleRequest)
	if consoleResponse.Code != http.StatusOK || !strings.Contains(consoleResponse.Body.String(), "Local: expirado e depois pago") || consoleResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("sandbox console response=%d body_contains_scenarios=%t headers=%v", consoleResponse.Code, strings.Contains(consoleResponse.Body.String(), "Local: expirado e depois pago"), consoleResponse.Header())
	}
	newOrderRequest := httptest.NewRequest(http.MethodPost, "/v1/sandbox/test-orders", nil)
	newOrderResponse := httptest.NewRecorder()
	handler.ServeHTTP(newOrderResponse, newOrderRequest)
	if newOrderResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized sandbox order creation=%d", newOrderResponse.Code)
	}
	newOrderRequest = httptest.NewRequest(http.MethodPost, "/v1/sandbox/test-orders", nil)
	newOrderRequest.Header.Set("X-Sandbox-Session", token)
	newOrderResponse = httptest.NewRecorder()
	handler.ServeHTTP(newOrderResponse, newOrderRequest)
	if newOrderResponse.Code != http.StatusCreated {
		t.Fatalf("sandbox order creation=%d body=%s", newOrderResponse.Code, newOrderResponse.Body.String())
	}
	var generatedOrder struct {
		OrderID     string `json:"order_id"`
		AmountCents int64  `json:"amount_cents"`
	}
	if err := json.Unmarshal(newOrderResponse.Body.Bytes(), &generatedOrder); err != nil {
		t.Fatal(err)
	}
	generatedOrderID = generatedOrder.OrderID
	if !isGeneratedSandboxOrderID(generatedOrderID) || generatedOrder.AmountCents != sandboxTestOrderAmountCents {
		t.Fatalf("generated sandbox order=%#v", generatedOrder)
	}
	var storedAmount int64
	if err := store.pool.QueryRow(ctx, `SELECT amount_cents FROM demo_orders WHERE order_id=$1`, generatedOrderID).Scan(&storedAmount); err != nil || storedAmount != sandboxTestOrderAmountCents {
		t.Fatalf("generated order was not persisted: amount=%d err=%v", storedAmount, err)
	}
	demoRequest := httptest.NewRequest(http.MethodPost, "/v1/checkout-sessions", strings.NewReader(`{"order_id":"demo-order-1"}`))
	demoResponse := httptest.NewRecorder()
	handler.ServeHTTP(demoResponse, demoRequest)
	if demoResponse.Code != http.StatusNotFound {
		t.Fatalf("sandbox exposed simulated checkout: %d", demoResponse.Code)
	}
	checkout := func(auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/merchant/checkout-sessions", strings.NewReader(`{"order_id":"sandbox-order-1"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Sandbox-Session", auth)
		r.Header.Set("Idempotency-Key", "sandbox-fixture-key")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := checkout("wrong"); w.Code != http.StatusUnauthorized || posts.Load() != 0 {
		t.Fatalf("unauthorized=%d posts=%d", w.Code, posts.Load())
	}
	if w := checkout(token); w.Code < 400 {
		t.Fatalf("expected ambiguity: %s", w.Body.String())
	}
	w := checkout(token)
	if w.Code != http.StatusCreated {
		t.Fatalf("reconciled checkout=%d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	payRequest := httptest.NewRequest(http.MethodPost, "/v1/sandbox/test-pay?checkout_id="+body["checkout_id"].(string), nil)
	payRequest.Header.Set("Authorization", "Bearer "+body["access_token"].(string))
	payResponse := httptest.NewRecorder()
	handler.ServeHTTP(payResponse, payRequest)
	if payResponse.Code != http.StatusAccepted || testPayments.Load() != 1 {
		t.Fatalf("sandbox test payment=%d body=%s calls=%d", payResponse.Code, payResponse.Body.String(), testPayments.Load())
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+body["checkout_id"].(string), nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || gets.Load() != 2 {
		t.Fatalf("unauthorized polling=%d gets=%d", w.Code, gets.Load())
	}
	r.Header.Set("Authorization", "Bearer "+body["access_token"].(string))
	for failure := int32(1); failure <= 3; failure++ {
		lookupFailure.Store(failure)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code < 500 {
			t.Fatalf("lookup failure %d became success: %s", failure, w.Body.String())
		}
		var state string
		if err := store.pool.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE checkout_id=$1`, body["checkout_id"]).Scan(&state); err != nil || state != "pending" {
			t.Fatalf("lookup failure mutated checkout: %s err=%v", state, err)
		}
	}
	lookupFailure.Store(0)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"paid"`) {
		t.Fatalf("poll=%d %s", w.Code, w.Body.String())
	}
	if posts.Load() != 1 || gets.Load() != 6 {
		t.Fatalf("posts=%d gets=%d", posts.Load(), gets.Load())
	}
	unauthorizedScenario := httptest.NewRequest(http.MethodPost, "/v1/sandbox/test-scenarios/expired?checkout_id="+body["checkout_id"].(string), nil)
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedScenario)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized local scenario=%d body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}
	for _, test := range []struct {
		scenario string
		want     []string
	}{
		{scenario: "expired", want: []string{`"status":"expired"`}},
		{scenario: "late-paid", want: []string{`"status":"expired"`, `"status":"paid"`}},
	} {
		request := httptest.NewRequest(http.MethodPost, "/v1/sandbox/test-scenarios/"+test.scenario+"?checkout_id="+body["checkout_id"].(string), nil)
		request.Header.Set("Authorization", "Bearer "+body["access_token"].(string))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"source":"local_test_simulation"`) {
			t.Fatalf("scenario %s setup=%d body=%s", test.scenario, response.Code, response.Body.String())
		}
		for _, wantStatus := range test.want {
			statusRequest := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+body["checkout_id"].(string), nil)
			statusRequest.Header.Set("Authorization", "Bearer "+body["access_token"].(string))
			statusResponse := httptest.NewRecorder()
			handler.ServeHTTP(statusResponse, statusRequest)
			if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), wantStatus) {
				t.Fatalf("scenario %s expected %s, got %d %s", test.scenario, wantStatus, statusResponse.Code, statusResponse.Body.String())
			}
		}
	}
	var persistedStatus string
	if err := store.pool.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE checkout_id=$1`, body["checkout_id"]).Scan(&persistedStatus); err != nil || persistedStatus != "paid" {
		t.Fatalf("local test scenario changed persisted payment: status=%s err=%v", persistedStatus, err)
	}
}
