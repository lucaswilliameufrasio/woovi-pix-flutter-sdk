package demo

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxBodyBytes = 4096

type Status string

const (
	Pending Status = "pending"
	Paid    Status = "paid"
	Expired Status = "expired"
)

type checkout struct {
	id            string
	tokenHash     [32]byte
	orderID       string
	correlationID string
	amount        int64
	status        Status
	expiresAt     time.Time
	brCode        string
}

type Server struct {
	store        CheckoutStore
	clockNow     func() time.Time
	webhookAuth  WebhookVerifier
	allowDemoPay bool
}

func NewServer() *Server {
	return NewServerWithOptions(NewMemoryStore(), time.Now, nil, true)
}

func NewServerWithStore(store CheckoutStore, clock func() time.Time) *Server {
	return NewServerWithOptions(store, clock, nil, false)
}

func NewServerWithStoreAndWebhookVerifier(store CheckoutStore, clock func() time.Time, verifier WebhookVerifier) *Server {
	return NewServerWithOptions(store, clock, verifier, false)
}

func NewServerWithOptions(store CheckoutStore, clock func() time.Time, verifier WebhookVerifier, allowDemoPay bool) *Server {
	if clock == nil {
		clock = time.Now
	}
	return &Server{store: store, clockNow: clock, webhookAuth: verifier, allowDemoPay: allowDemoPay}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/checkout-sessions", s.createSession)
	mux.HandleFunc("GET /v1/checkout-sessions/{id}", s.getStatus)
	if s.allowDemoPay {
		mux.HandleFunc("POST /demo/checkouts/{id}/pay", s.simulatePaid)
	}
	if s.webhookAuth != nil {
		mux.HandleFunc("POST /webhooks/woovi", s.receiveWooviWebhook)
	}
	return securityHeaders(mux)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrderID string `json:"order_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil || request.OrderID == "" || len(request.OrderID) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	c, token, created, err := s.store.Create(r.Context(), request.OrderID, s.clockNow())
	if errors.Is(err, ErrOrderNotFound) {
		writeError(w, http.StatusNotFound, "order_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeSessionJSON(w, status, c, token)
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if provided == "" || provided == r.Header.Get("Authorization") {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tokenHash := sha256.Sum256([]byte(provided))
	c, err := s.store.Status(r.Context(), r.PathValue("id"), tokenHash, s.clockNow())
	if errors.Is(err, ErrCheckoutNotFound) {
		writeError(w, http.StatusNotFound, "checkout_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": c.status, "expires_at": c.expiresAt.UTC().Format(time.RFC3339)})
}

func (s *Server) simulatePaid(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.SimulatePaid(r.Context(), r.PathValue("id"), s.clockNow())
	if errors.Is(err, ErrCheckoutNotFound) {
		writeError(w, http.StatusNotFound, "checkout_not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": string(c.status)})
}

func (s *Server) receiveWooviWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}
	valid, err := s.webhookAuth.Verify(r.Context(), rawBody, r.Header.Get("x-webhook-signature"))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "signature_verification_unavailable")
		return
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid_signature")
		return
	}
	var event WooviChargeEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}
	if event.Charge.CorrelationID == "" || len(event.Charge.CorrelationID) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}
	result, err := s.store.ApplyChargeEvent(r.Context(), event, sha256.Sum256(rawBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "webhook_processing_failed")
		return
	}
	status := http.StatusOK
	if result.Rejected {
		// The signed but inconsistent event is durably quarantined; acknowledging
		// prevents provider retry storms. Reconciliation must resolve the mismatch.
		status = http.StatusAccepted
	}
	writeJSON(w, status, map[string]any{"received": true, "duplicate": result.Duplicate, "applied": result.Applied, "rejected": result.Rejected})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func randomHex(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeSessionJSON(w http.ResponseWriter, status int, c *checkout, token string) {
	writeJSON(w, status, map[string]any{
		"checkout_id": c.id, "access_token": token, "status": c.status, "amount_cents": c.amount,
		"currency": "BRL", "expires_at": c.expiresAt.UTC().Format(time.RFC3339), "pix_copy_paste": c.brCode,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
