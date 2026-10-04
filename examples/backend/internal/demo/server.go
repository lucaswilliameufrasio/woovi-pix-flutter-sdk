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
	"sync"
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
	id          string
	accessToken string
	tokenHash   [32]byte
	orderID     string
	amount      int64
	status      Status
	expiresAt   time.Time
	brCode      string
}

type Server struct {
	mu       sync.Mutex
	byID     map[string]*checkout
	byOrder  map[string]string
	byToken  map[[32]byte]string
	orders   map[string]int64
	clockNow func() time.Time
}

func NewServer() *Server {
	return &Server{
		byID: map[string]*checkout{}, byOrder: map[string]string{}, byToken: map[[32]byte]string{},
		orders: map[string]int64{"demo-order-1": 2599}, clockNow: time.Now,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/checkout-sessions", s.createSession)
	mux.HandleFunc("GET /v1/checkout-sessions/{id}", s.getStatus)
	mux.HandleFunc("POST /demo/checkouts/{id}/pay", s.simulatePaid)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	amount, exists := s.orders[request.OrderID]
	if !exists {
		writeError(w, http.StatusNotFound, "order_not_found")
		return
	}
	if id := s.byOrder[request.OrderID]; id != "" {
		if old := s.byID[id]; old != nil && old.status == Pending && s.clockNow().Before(old.expiresAt) {
			writeJSON(w, http.StatusOK, map[string]any{
				"checkout_id": old.id, "access_token": old.accessToken, "status": old.status,
				"amount_cents": old.amount, "currency": "BRL",
				"expires_at": old.expiresAt.UTC().Format(time.RFC3339), "pix_copy_paste": old.brCode,
			})
			return
		}
	}
	id, err := randomHex(16)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	token, err := randomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	c := &checkout{id: id, accessToken: token, tokenHash: sha256.Sum256([]byte(token)), orderID: request.OrderID, amount: amount, status: Pending,
		expiresAt: s.clockNow().Add(15 * time.Minute), brCode: "000201-DEMO-PIX-" + id}
	s.byID[id], s.byOrder[request.OrderID], s.byToken[c.tokenHash] = c, id, id
	writeJSON(w, http.StatusCreated, map[string]any{
		"checkout_id": c.id, "access_token": token, "status": c.status, "amount_cents": c.amount,
		"currency": "BRL", "expires_at": c.expiresAt.UTC().Format(time.RFC3339), "pix_copy_paste": c.brCode,
	})
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if provided == "" || provided == r.Header.Get("Authorization") {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tokenHash := sha256.Sum256([]byte(provided))
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byToken[tokenHash]
	c := s.byID[id]
	if !ok || c == nil || c.id != r.PathValue("id") || c.tokenHash != tokenHash {
		writeError(w, http.StatusNotFound, "checkout_not_found")
		return
	}
	s.expire(c)
	writeJSON(w, http.StatusOK, map[string]any{"status": c.status, "expires_at": c.expiresAt.UTC().Format(time.RFC3339)})
}

func (s *Server) simulatePaid(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.byID[r.PathValue("id")]
	if c == nil {
		writeError(w, http.StatusNotFound, "checkout_not_found")
		return
	}
	// Idempotent simulator action. A tardy payment remains a backend policy question;
	// this deliberately simple demo does not pretend to implement refunds/reconciliation.
	if c.status == Pending {
		c.status = Paid
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": string(c.status)})
}

func (s *Server) expire(c *checkout) {
	if c.status == Pending && !s.clockNow().Before(c.expiresAt) {
		c.status = Expired
	}
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
