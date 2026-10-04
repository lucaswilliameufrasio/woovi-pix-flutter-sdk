package demo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckoutSessionIsScopedAndIdempotentInDemo(t *testing.T) {
	s := NewServer()
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	s.clockNow = func() time.Time { return now }
	h := s.Handler()

	first := createCheckout(t, h, "demo-order-1")
	if first.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", first.Code, first.Body.String())
	}
	var issued map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	id, token := issued["checkout_id"].(string), issued["access_token"].(string)
	if issued["amount_cents"] != float64(2599) {
		t.Fatalf("server did not choose amount: %#v", issued)
	}
	second := createCheckout(t, h, "demo-order-1")
	if second.Code != http.StatusOK {
		t.Fatalf("repeat create status = %d, want idempotent recovery", second.Code)
	}
	var recovered map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &recovered); err != nil {
		t.Fatal(err)
	}
	newToken, ok := recovered["access_token"].(string)
	if recovered["checkout_id"] != id || !ok || newToken == token {
		t.Fatal("idempotent recovery must keep checkout and issue a new bearer token")
	}

	wrongCheckout := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/not-the-id", nil)
	wrongCheckout.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, wrongCheckout)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-checkout status = %d", response.Code)
	}
	oldTokenReq := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+id, nil)
	oldTokenReq.Header.Set("Authorization", "Bearer "+token)
	oldTokenRes := httptest.NewRecorder()
	h.ServeHTTP(oldTokenRes, oldTokenReq)
	if oldTokenRes.Code != http.StatusOK {
		t.Fatalf("parallel session bearer should remain valid: %d", oldTokenRes.Code)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+id, nil)
	statusReq.Header.Set("Authorization", "Bearer "+newToken)
	statusRes := httptest.NewRecorder()
	h.ServeHTTP(statusRes, statusReq)
	if statusRes.Code != http.StatusOK {
		t.Fatalf("status request = %d: %s", statusRes.Code, statusRes.Body.String())
	}

	payReq := httptest.NewRequest(http.MethodPost, "/demo/checkouts/"+id+"/pay", nil)
	payRes := httptest.NewRecorder()
	h.ServeHTTP(payRes, payReq)
	if payRes.Code != http.StatusOK {
		t.Fatalf("simulated pay = %d", payRes.Code)
	}
	statusReq = httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+id, nil)
	statusReq.Header.Set("Authorization", "Bearer "+newToken)
	statusRes = httptest.NewRecorder()
	h.ServeHTTP(statusRes, statusReq)
	if !bytes.Contains(statusRes.Body.Bytes(), []byte(`"status":"paid"`)) {
		t.Fatalf("paid state not reported: %s", statusRes.Body.String())
	}
}

func TestCreateRejectsClientControlledAmounts(t *testing.T) {
	s := NewServer()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/checkout-sessions", bytes.NewBufferString(`{"order_id":"demo-order-1","amount_cents":1}`))
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.Code)
	}
}

func createCheckout(t *testing.T, h http.Handler, orderID string) *httptest.ResponseRecorder {
	t.Helper()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/checkout-sessions", bytes.NewBufferString(`{"order_id":"`+orderID+`"}`))
	h.ServeHTTP(res, req)
	return res
}
