package demo

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"time"
)

var (
	ErrOrderNotFound    = errors.New("order not found")
	ErrCheckoutNotFound = errors.New("checkout not found")
)

// CheckoutStore persists the merchant-owned order and checkout session data.
type CheckoutStore interface {
	Create(context.Context, string, time.Time) (*checkout, string, bool, error)
	Status(context.Context, string, [32]byte, time.Time) (*checkout, error)
	SimulatePaid(context.Context, string, time.Time) (*checkout, error)
}

// MemoryStore supports lightweight handler tests and an explicitly ephemeral demo mode.
type MemoryStore struct {
	mu      sync.Mutex
	byID    map[string]*checkout
	byOrder map[string]string
	tokens  map[[32]byte]tokenGrant
	orders  map[string]int64
}

type tokenGrant struct {
	checkoutID string
	expiresAt  time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:    make(map[string]*checkout),
		byOrder: make(map[string]string),
		tokens:  make(map[[32]byte]tokenGrant),
		orders:  map[string]int64{"demo-order-1": 2599},
	}
}

func (s *MemoryStore) Create(_ context.Context, orderID string, now time.Time) (*checkout, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, grant := range s.tokens {
		if !now.Before(grant.expiresAt) {
			delete(s.tokens, hash)
		}
	}
	amount, ok := s.orders[orderID]
	if !ok {
		return nil, "", false, ErrOrderNotFound
	}
	if id := s.byOrder[orderID]; id != "" {
		if c := s.byID[id]; c != nil && c.status == Pending && now.Before(c.expiresAt) {
			token, err := randomHex(32)
			if err != nil {
				return nil, "", false, err
			}
			c.tokenHash = sha256.Sum256([]byte(token))
			s.tokens[c.tokenHash] = tokenGrant{checkoutID: c.id, expiresAt: c.expiresAt.Add(time.Hour)}
			return c.clone(), token, false, nil
		}
	}
	id, err := randomHex(16)
	if err != nil {
		return nil, "", false, err
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, "", false, err
	}
	c := &checkout{id: id, tokenHash: sha256.Sum256([]byte(token)), orderID: orderID, amount: amount,
		status: Pending, expiresAt: now.Add(15 * time.Minute), brCode: "000201-DEMO-PIX-" + id}
	s.byID[id], s.byOrder[orderID] = c, id
	s.tokens[c.tokenHash] = tokenGrant{checkoutID: c.id, expiresAt: c.expiresAt.Add(time.Hour)}
	return c.clone(), token, true, nil
}

func (s *MemoryStore) Status(_ context.Context, id string, tokenHash [32]byte, now time.Time) (*checkout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.byID[id]
	grant, ok := s.tokens[tokenHash]
	if c == nil || !ok || grant.checkoutID != id || !now.Before(grant.expiresAt) {
		return nil, ErrCheckoutNotFound
	}
	if c.status == Pending && !now.Before(c.expiresAt) {
		c.status = Expired
	}
	return c.clone(), nil
}

func (s *MemoryStore) SimulatePaid(_ context.Context, id string, _ time.Time) (*checkout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.byID[id]
	if c == nil {
		return nil, ErrCheckoutNotFound
	}
	// The explicit simulator can model a tardy payment correcting expired to paid.
	c.status = Paid
	return c.clone(), nil
}

func (c *checkout) clone() *checkout {
	copy := *c
	return &copy
}
