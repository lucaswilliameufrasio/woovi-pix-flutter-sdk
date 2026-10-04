package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWooviChargeClientUsesDocumentedServerRequest(t *testing.T) {
	const appID = "not-a-real-app-id"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/charge" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != appID {
			t.Errorf("Authorization header was not AppID value")
		}
		var body struct {
			CorrelationID string `json:"correlationID"`
			Value         int64  `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.CorrelationID != "order-42" || body.Value != 2599 {
			t.Errorf("request body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"correlationID":"order-42","brCode":"top-level-brcode","charge":{"correlationID":"order-42","status":"ACTIVE","brCode":"000201-test","expiresDate":"2030-01-01T00:00:00.123Z"}}`))
	}))
	defer server.Close()

	client, err := NewWooviChargeClient(appID, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	charge, err := client.CreateCharge(context.Background(), "order-42", 2599)
	if err != nil {
		t.Fatal(err)
	}
	if charge.CorrelationID != "order-42" || charge.Status != "ACTIVE" || charge.BRCode != "000201-test" ||
		!charge.ExpiresAt.Equal(time.Date(2030, 1, 1, 0, 0, 0, 123_000_000, time.UTC)) {
		t.Fatalf("parsed charge = %#v", charge)
	}
}

func TestWooviChargeClientNeverRetriesAndRejectsInsecureRemoteURL(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client, err := NewWooviChargeClient("not-a-real-app-id", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateCharge(context.Background(), "order-42", 2599); err == nil {
		t.Fatal("expected provider failure")
	}
	if requests != 1 {
		t.Fatalf("charge request was retried %d times", requests)
	}
	if _, err := NewWooviChargeClient("not-a-real-app-id", "http://192.0.2.5", nil); err == nil {
		t.Fatal("non-loopback HTTP endpoint must be rejected")
	}
}

func TestWooviChargeClientDoesNotForwardAppIDOnRedirect(t *testing.T) {
	const appID = "not-a-real-app-id"
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		if r.Header.Get("Authorization") != "" {
			t.Error("AppID Authorization header was forwarded to redirect target")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, nil, target.URL+"/capture", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client, err := NewWooviChargeClient(appID, redirect.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateCharge(context.Background(), "order-42", 2599); err == nil {
		t.Fatal("redirect response must be treated as an uncertain outcome")
	}
	if targetRequests != 0 {
		t.Fatalf("redirect target received %d requests", targetRequests)
	}
}
