package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	assertAPIError(t, response, http.StatusNotFound, "CHECKOUT_NOT_FOUND", false)
	invalidTokenRequest := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+id, nil)
	invalidTokenRequest.Header.Set("Authorization", "Bearer invalid-token")
	invalidTokenResponse := httptest.NewRecorder()
	h.ServeHTTP(invalidTokenResponse, invalidTokenRequest)
	assertAPIError(t, invalidTokenResponse, http.StatusUnauthorized, "UNAUTHORIZED", false)
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

	payReq := httptest.NewRequest(http.MethodPost, "/v1/demo/checkouts/"+id+"/pay", nil)
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
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.Code)
	}
	assertAPIError(t, res, http.StatusUnprocessableEntity, "INVALID_PARAMS", true)
}

func TestAPIErrorHouseContract(t *testing.T) {
	handler := NewServer().Handler()
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		validation bool
	}{
		{name: "malformed json", body: `{"order_id":`, wantStatus: http.StatusBadRequest, wantCode: "MALFORMED_REQUEST"},
		{name: "semantic validation", body: `{"order_id":""}`, wantStatus: http.StatusUnprocessableEntity, wantCode: "INVALID_PARAMS", validation: true},
		{name: "unknown request field", body: `{"order_id":"demo-order-1","amount_cents":1}`, wantStatus: http.StatusUnprocessableEntity, wantCode: "INVALID_PARAMS", validation: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, apiCheckoutSessionsPath, bytes.NewBufferString(test.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertAPIError(t, response, test.wantStatus, test.wantCode, test.validation)
		})
	}

	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/id", nil)
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	assertAPIError(t, unauthorizedResponse, http.StatusUnauthorized, "UNAUTHORIZED", false)

	tooLarge := strings.Repeat(" ", maxBodyBytes+1)
	largeRequest := httptest.NewRequest(http.MethodPost, apiCheckoutSessionsPath, strings.NewReader(tooLarge))
	largeResponse := httptest.NewRecorder()
	handler.ServeHTTP(largeResponse, largeRequest)
	assertAPIError(t, largeResponse, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", false)
}

func TestDemoPaymentRouteIsDisabledByDefault(t *testing.T) {
	handler := NewServerWithStore(NewMemoryStore(), time.Now).Handler()
	request := httptest.NewRequest(http.MethodPost, "/v1/demo/checkouts/does-not-matter/pay", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("demo payment route status = %d, want 404 when disabled", response.Code)
	}
	assertAPIError(t, response, http.StatusNotFound, "RESOURCE_NOT_FOUND", false)
}

type testWebhookVerifier struct {
	valid bool
	raw   []byte
	err   error
}

func (v *testWebhookVerifier) Verify(_ context.Context, raw []byte, _ string) (bool, error) {
	v.raw = append([]byte(nil), raw...)
	return v.valid, v.err
}

func TestWebhookDependencyFailureUsesGatewayErrorContract(t *testing.T) {
	verifier := &testWebhookVerifier{err: errors.New("private upstream details")}
	handler := NewServerWithStoreAndWebhookVerifier(NewMemoryStore(), time.Now, verifier).Handler()
	response := postWebhook(handler, []byte(`{}`))
	assertAPIError(t, response, http.StatusBadGateway, "DEPENDENCY_UNAVAILABLE", false)
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("private upstream details")) {
		t.Fatalf("internal provider error leaked: %s", response.Body.String())
	}
	extra := body["extra"].(map[string]any)
	if extra["provider_code"] != "WEBHOOK_KEY_LOOKUP_FAILED" {
		t.Fatalf("provider code must be classified, got %#v", extra)
	}
}

type failingCreateStore struct {
	CheckoutStore
	err error
}

func (s failingCreateStore) Create(context.Context, string, time.Time) (*checkout, string, bool, error) {
	return nil, "", false, s.err
}

func TestUnexpectedErrorDoesNotExposeInternalDetails(t *testing.T) {
	handler := NewServerWithStore(failingCreateStore{
		CheckoutStore: NewMemoryStore(),
		err:           errors.New("database password and stack details"),
	}, time.Now).Handler()
	request := httptest.NewRequest(http.MethodPost, apiCheckoutSessionsPath, bytes.NewBufferString(`{"order_id":"demo-order-1"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusInternalServerError, "UNEXPECTED_ERROR", false)
	if bytes.Contains(response.Body.Bytes(), []byte("database password")) {
		t.Fatalf("internal error leaked to client: %s", response.Body.String())
	}
}

func TestSignedWebhookAppliesIdempotentlyAndPaidIsTerminal(t *testing.T) {
	store := NewMemoryStore()
	verifier := &testWebhookVerifier{}
	handler := NewServerWithStoreAndWebhookVerifier(store, time.Now, verifier).Handler()
	created := createCheckout(t, handler, "demo-order-1")
	var session map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	id := session["checkout_id"].(string)
	body := []byte(`{"event":"OPENPIX:CHARGE_COMPLETED","charge":{"correlationID":"` + id + `","status":"COMPLETED","value":2599},"pix":{"status":"CONFIRMED"}}`)
	verifier.valid = false
	response := postWebhook(handler, body)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status = %d", response.Code)
	}
	verifier.valid = true
	wrongAmount := bytes.Replace(body, []byte(`"value":2599`), []byte(`"value":1`), 1)
	response = postWebhook(handler, wrongAmount)
	if response.Code != http.StatusAccepted || !bytes.Contains(response.Body.Bytes(), []byte(`"rejected":true`)) {
		t.Fatalf("amount mismatch status = %d, want quarantined 202: %s", response.Code, response.Body.String())
	}
	response = postWebhook(handler, body)
	if response.Code != http.StatusOK || !bytes.Equal(verifier.raw, body) || !bytes.Contains(response.Body.Bytes(), []byte(`"applied":true`)) {
		t.Fatalf("valid webhook not applied using exact raw body: status=%d body=%s", response.Code, response.Body.String())
	}
	response = postWebhook(handler, body)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"duplicate":true`)) {
		t.Fatalf("duplicate webhook not idempotent: status=%d body=%s", response.Code, response.Body.String())
	}
	lateExpired := []byte(`{"event":"OPENPIX:CHARGE_EXPIRED","charge":{"correlationID":"` + id + `","status":"EXPIRED","value":2599}}`)
	response = postWebhook(handler, lateExpired)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"applied":false`)) {
		t.Fatalf("out-of-order expired event changed paid state: status=%d body=%s", response.Code, response.Body.String())
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/v1/checkout-sessions/"+id, nil)
	statusReq.Header.Set("Authorization", "Bearer "+session["access_token"].(string))
	statusRes := httptest.NewRecorder()
	handler.ServeHTTP(statusRes, statusReq)
	if !bytes.Contains(statusRes.Body.Bytes(), []byte(`"status":"paid"`)) {
		t.Fatalf("paid status not retained: %s", statusRes.Body.String())
	}
}

func postWebhook(handler http.Handler, body []byte) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, apiWebhookWooviPath, bytes.NewReader(body))
	req.Header.Set("x-webhook-signature", "test-signature")
	handler.ServeHTTP(response, req)
	return response
}

func createCheckout(t *testing.T, h http.Handler, orderID string) *httptest.ResponseRecorder {
	t.Helper()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/checkout-sessions", bytes.NewBufferString(`{"order_id":"`+orderID+`"}`))
	h.ServeHTTP(res, req)
	return res
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string, withValidation bool) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d, want %d: %s", response.Code, status, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not JSON: %v", err)
	}
	if _, ok := body["message"].(string); !ok || body["message"] == "" {
		t.Fatalf("missing human-readable message: %#v", body)
	}
	if body["error_code"] != code {
		t.Fatalf("error_code=%v, want %s", body["error_code"], code)
	}
	if withValidation {
		extra, ok := body["extra"].(map[string]any)
		if !ok {
			t.Fatalf("422 missing extra.validation_errors: %#v", body)
		}
		errors, ok := extra["validation_errors"].([]any)
		if !ok || len(errors) == 0 {
			t.Fatalf("422 validation_errors empty: %#v", body)
		}
		item, ok := errors[0].(map[string]any)
		if !ok || item["field"] == nil || item["message"] == nil {
			t.Fatalf("invalid validation error shape: %#v", errors)
		}
	}
}
