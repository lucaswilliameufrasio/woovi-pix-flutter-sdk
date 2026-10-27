package demo

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

const SandboxAPIBaseURL = "https://api.woovi-sandbox.com"

const sandboxTestOrderAmountCents int64 = 2599

// SandboxAuthorizer is only for the isolated example, never merchant production auth.
type SandboxAuthorizer struct{ Token string }

func (a SandboxAuthorizer) AuthorizeCheckoutOrder(ctx context.Context, r *http.Request, orderID string) (AuthorizedMerchantOrder, error) {
	expected := sha256.Sum256([]byte(a.Token))
	provided := sha256.Sum256([]byte(r.Header.Get("X-Sandbox-Session")))
	if len(a.Token) < 32 || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
		return AuthorizedMerchantOrder{}, ErrCheckoutUnauthorized
	}
	if orderID != "sandbox-order-1" && !isGeneratedSandboxOrderID(orderID) {
		return AuthorizedMerchantOrder{}, ErrOrderNotFound
	}
	return AuthorizedMerchantOrder{OrderID: orderID, AmountCents: sandboxTestOrderAmountCents}, nil
}

func isGeneratedSandboxOrderID(orderID string) bool {
	const prefix = "sandbox-order-"
	if !strings.HasPrefix(orderID, prefix) {
		return false
	}
	identifier := strings.TrimPrefix(orderID, prefix)
	if len(identifier) != 32 {
		return false
	}
	_, err := hex.DecodeString(identifier)
	return err == nil
}

// ConfigureSandboxCheckout has no production URL fallback. No network call is
// made until a checkout request or status poll reaches the configured server.
func (s *Server) ConfigureSandboxCheckout(ctx context.Context, token, appID, baseURL string, demoPay bool) error {
	if demoPay || len(token) < 32 || appID == "" || baseURL != SandboxAPIBaseURL {
		return errors.New("sandbox requires a 32+ character session token, AppID, exact sandbox URL and disabled demo PSP")
	}
	store, ok := s.store.(*PostgresStore)
	if !ok {
		return errors.New("sandbox requires PostgreSQL")
	}
	client, err := NewWooviChargeClient(appID, baseURL, nil)
	if err != nil {
		return err
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id,amount_cents) VALUES ('sandbox-order-1',$1) ON CONFLICT DO NOTHING`, sandboxTestOrderAmountCents); err != nil {
		return err
	}
	if err := s.ConfigureMerchantCheckout(SandboxAuthorizer{Token: token}, client); err != nil {
		return err
	}
	s.sandboxLookup = client
	return nil
}

func (s *Server) createSandboxTestOrder(w http.ResponseWriter, r *http.Request) {
	if _, err := s.merchantCheckout.authorizer.AuthorizeCheckoutOrder(r.Context(), r, "sandbox-order-1"); errors.Is(err, ErrCheckoutUnauthorized) {
		writeAPIError(w, unauthorized())
		return
	} else if err != nil {
		writeAPIError(w, unexpectedError())
		return
	}
	store, ok := s.store.(*PostgresStore)
	if !ok {
		writeAPIError(w, unexpectedError())
		return
	}
	identifier, err := randomHex(16)
	if err != nil {
		writeAPIError(w, unexpectedError())
		return
	}
	orderID := "sandbox-order-" + identifier
	if _, err := store.pool.Exec(r.Context(), `INSERT INTO demo_orders(order_id,amount_cents) VALUES ($1,$2)`, orderID, sandboxTestOrderAmountCents); err != nil {
		writeAPIError(w, unexpectedError())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"order_id": orderID, "amount_cents": sandboxTestOrderAmountCents})
}

// refreshSandboxSession observes provider state with GET only, after bearer
// authentication. It never submits a charge or implements order fulfillment.
func (s *Server) refreshSandboxSession(ctx context.Context, c *checkout) error {
	charge, err := s.sandboxLookup.GetCharge(ctx, c.correlationID)
	if err != nil {
		return err
	}
	if charge.CorrelationID != c.correlationID || charge.Value != c.amount {
		return errors.New("sandbox lookup does not match checkout")
	}
	if charge.Status == "ACTIVE" {
		return nil
	}
	if charge.Status != "COMPLETED" && charge.Status != "EXPIRED" {
		return ErrChargeAttemptState
	}
	store := s.merchantCheckout.store
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var attemptID string
	if err := tx.QueryRow(ctx, `SELECT attempt_id FROM psp_charge_attempts WHERE correlation_id=$1 FOR UPDATE`, c.correlationID).Scan(&attemptID); err != nil {
		return err
	}
	if err := applyChargeStatusToAttempt(ctx, tx, attemptID, charge.Status); err != nil {
		return err
	}
	status := Expired
	if charge.Status == "COMPLETED" {
		status = Paid
	}
	if _, err := tx.Exec(ctx, `UPDATE checkout_sessions SET status=$2,br_code=NULL WHERE checkout_id=$1 AND status <> 'paid'`, c.id, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Used by sandbox replay when POST was ambiguous; reconciliation remains GET-only.
func (s *Server) reconcileSandboxAttempt(ctx context.Context, attempt ChargeAttempt) error {
	_, err := ReconcileChargeAttempt(ctx, s.merchantCheckout.store, s.sandboxLookup, attempt, time.Now())
	return err
}
