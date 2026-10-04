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
	var posts, gets atomic.Int32
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
			"brCode": "sandbox-fixture", "expiresDate": time.Now().Add(time.Hour).Format(time.RFC3339),
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
	completed.Store(true)
	r := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+body["checkout_id"].(string), nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || gets.Load() != 1 {
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
	if posts.Load() != 1 || gets.Load() != 5 {
		t.Fatalf("posts=%d gets=%d", posts.Load(), gets.Load())
	}
}
