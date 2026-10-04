package demo

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"
)

const SandboxAPIBaseURL = "https://api.woovi-sandbox.com"

// SandboxAuthorizer is only for the isolated example, never merchant production auth.
type SandboxAuthorizer struct{ Token string }

func (a SandboxAuthorizer) AuthorizeCheckoutOrder(ctx context.Context, r *http.Request, orderID string) (AuthorizedMerchantOrder, error) {
	expected := sha256.Sum256([]byte(a.Token))
	provided := sha256.Sum256([]byte(r.Header.Get("X-Sandbox-Session")))
	if len(a.Token) < 32 || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
		return AuthorizedMerchantOrder{}, ErrCheckoutUnauthorized
	}
	if orderID != "sandbox-order-1" {
		return AuthorizedMerchantOrder{}, ErrOrderNotFound
	}
	return AuthorizedMerchantOrder{OrderID: orderID, AmountCents: 2599}, nil
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
	if _, err := store.pool.Exec(ctx, `INSERT INTO demo_orders(order_id,amount_cents) VALUES ('sandbox-order-1',2599) ON CONFLICT DO NOTHING`); err != nil {
		return err
	}
	if err := s.ConfigureMerchantCheckout(SandboxAuthorizer{Token: token}, client); err != nil {
		return err
	}
	s.sandboxLookup = client
	return nil
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
