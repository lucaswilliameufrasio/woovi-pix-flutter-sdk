package demo

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxBodyBytes = 4096

const (
	apiCheckoutSessionsPath = "/v1/checkout-sessions"
	apiWebhookWooviPath     = "/v1/webhooks/woovi"
	apiDemoPayPath          = "/v1/demo/checkouts/{id}/pay"
)

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
	store            CheckoutStore
	clockNow         func() time.Time
	webhookAuth      WebhookVerifier
	allowDemoPay     bool
	merchantCheckout *merchantCheckoutService
	sandboxLookup    *WooviChargeClient
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
	if s.sandboxLookup == nil {
		mux.HandleFunc("POST "+apiCheckoutSessionsPath, s.createSession)
	}
	if s.merchantCheckout != nil {
		mux.HandleFunc("POST /v1/merchant/checkout-sessions", s.createMerchantCheckout)
	}
	mux.HandleFunc("GET /v1/checkout-sessions/{id}", s.getStatus)
	if s.allowDemoPay {
		mux.HandleFunc("POST "+apiDemoPayPath, s.simulatePaid)
	}
	if s.webhookAuth != nil {
		mux.HandleFunc("POST "+apiWebhookWooviPath, s.receiveWooviWebhook)
	}
	apiNotFound := func(w http.ResponseWriter, _ *http.Request) { writeAPIError(w, resourceNotFound("resource")) }
	mux.HandleFunc("/v1/", apiNotFound)
	mux.HandleFunc("/demo/", apiNotFound)
	mux.HandleFunc("/webhooks/", apiNotFound)
	mux.HandleFunc("/", apiNotFound)
	return securityHeaders(mux)
}

// ConfigureMerchantCheckout enables the authenticated, PSP-backed endpoint.
// It must be called before Handler; without a trusted merchant authorizer the
// route stays absent. This demo binary intentionally does not configure one.
func (s *Server) ConfigureMerchantCheckout(authorizer MerchantCheckoutAuthorizer, creator ChargeCreator) error {
	store, ok := s.store.(*PostgresStore)
	if !ok || authorizer == nil || creator == nil {
		return errors.New("merchant checkout requires PostgreSQL and trusted authorizer/client")
	}
	s.merchantCheckout = &merchantCheckoutService{store: store, authorizer: authorizer, creator: creator, clockNow: s.clockNow}
	return nil
}

func (s *Server) createMerchantCheckout(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrderID string `json:"order_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, decodeAPIError(err))
		return
	}
	if request.OrderID == "" || len(request.OrderID) > 128 {
		writeAPIError(w, invalidParams("order_id", "deve conter entre 1 e 128 caracteres"))
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if len(idempotencyKey) < 8 || len(idempotencyKey) > 128 || strings.TrimSpace(idempotencyKey) != idempotencyKey {
		writeAPIError(w, invalidParams("Idempotency-Key", "é obrigatório e deve conter entre 8 e 128 caracteres"))
		return
	}
	order, err := s.merchantCheckout.authorizer.AuthorizeCheckoutOrder(r.Context(), r, request.OrderID)
	if errors.Is(err, ErrCheckoutUnauthorized) {
		writeAPIError(w, unauthorized())
		return
	}
	if errors.Is(err, ErrOrderNotFound) {
		writeAPIError(w, resourceNotFound("order"))
		return
	}
	if err != nil || order.OrderID != request.OrderID || order.AmountCents <= 0 {
		writeAPIError(w, unexpectedError())
		return
	}
	attempt, err := s.merchantCheckout.store.ReserveChargeAttemptWithIdempotency(r.Context(), request.OrderID, idempotencyKey, s.clockNow())
	if errors.Is(err, ErrOrderNotFound) {
		writeAPIError(w, resourceNotFound("order"))
		return
	}
	if errors.Is(err, ErrIdempotencyConflict) {
		writeAPIError(w, apiConflict("IDEMPOTENCY_CONFLICT", "A chave de idempotência já foi usada em outra requisição"))
		return
	}
	if errors.Is(err, ErrOrderAlreadyPaid) {
		writeAPIError(w, apiConflict("ORDER_ALREADY_PAID", "O pedido já foi pago"))
		return
	}
	if errors.Is(err, ErrChargeAttemptState) {
		writeAPIError(w, apiConflict("IDEMPOTENCY_KEY_REPLAY", "A chave de idempotência pertence a uma tentativa expirada; use uma nova chave para tentar novamente"))
		return
	}
	if errors.Is(err, ErrChargeInProgress) {
		writeAPIError(w, apiConflict("PAYMENT_PROCESSING", "O pagamento está sendo confirmado; repita a consulta com a mesma chave de idempotência"))
		return
	}
	if err != nil {
		writeAPIError(w, unexpectedError())
		return
	}
	if attempt.AmountCents != order.AmountCents {
		writeAPIError(w, apiConflict("ORDER_AMOUNT_CHANGED", "O valor do pedido mudou; atualize o pedido antes de tentar pagar"))
		return
	}
	if attempt.State == ChargeAttemptReserved {
		// A webhook may resolve the attempt before the POST response is persisted.
		// Always derive the response from the durable row below, never from stale
		// provider response data.
		submitted, _, submitErr := SubmitReservedChargeAttempt(r.Context(), s.merchantCheckout.store, s.merchantCheckout.creator, attempt, s.clockNow())
		if submitErr == nil {
			attempt = submitted
		} else if errors.Is(submitErr, ErrChargeAttemptState) {
			var state ChargeAttemptState
			var providerStatus string
			if readErr := s.merchantCheckout.store.pool.QueryRow(r.Context(), `SELECT state, COALESCE(provider_status,'') FROM psp_charge_attempts WHERE attempt_id=$1`, attempt.ID).Scan(&state, &providerStatus); readErr == nil && state == ChargeAttemptResolved && (providerStatus == "COMPLETED" || providerStatus == "EXPIRED") {
				attempt.State = state
			} else {
				writeAPIError(w, apiConflict("PAYMENT_PROCESSING", "O pagamento está sendo confirmado; repita a consulta com a mesma chave de idempotência"))
				return
			}
		} else {
			writeAPIError(w, dependencyError("PAYMENT_PROCESSING", "O pagamento está sendo confirmado; consulte novamente com a mesma chave de idempotência", "CHARGE_SUBMISSION_UNCERTAIN"))
			return
		}
	}
	if attempt.State == ChargeAttemptSubmitting || attempt.State == ChargeAttemptUnknown {
		if s.sandboxLookup != nil && s.reconcileSandboxAttempt(r.Context(), attempt) == nil {
			attempt.State = ChargeAttemptResolved
		} else {
			writeAPIError(w, apiConflict("PAYMENT_PROCESSING", "O pagamento está sendo confirmado; repita a consulta com a mesma chave de idempotência"))
			return
		}
	}
	c, token, created, sessionErr := s.merchantCheckout.store.CreateCheckoutForChargeAttempt(r.Context(), attempt.ID, s.clockNow())
	if errors.Is(sessionErr, ErrChargeInProgress) {
		writeAPIError(w, apiConflict("PAYMENT_PROCESSING", "O pagamento está sendo confirmado; repita a consulta com a mesma chave de idempotência"))
		return
	}
	if errors.Is(sessionErr, ErrOrderAlreadyPaid) {
		writeAPIError(w, apiConflict("ORDER_ALREADY_PAID", "O pedido já foi pago"))
		return
	}
	if sessionErr != nil {
		writeAPIError(w, unexpectedError())
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeSessionJSON(w, status, c, token)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrderID string `json:"order_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, decodeAPIError(err))
		return
	}
	if request.OrderID == "" || len(request.OrderID) > 128 {
		writeAPIError(w, invalidParams("order_id", "deve conter entre 1 e 128 caracteres"))
		return
	}
	c, token, created, err := s.store.Create(r.Context(), request.OrderID, s.clockNow())
	if errors.Is(err, ErrOrderNotFound) {
		writeAPIError(w, resourceNotFound("order"))
		return
	}
	if err != nil {
		writeAPIError(w, unexpectedError())
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
		writeAPIError(w, unauthorized())
		return
	}
	tokenHash := sha256.Sum256([]byte(provided))
	c, err := s.store.Status(r.Context(), r.PathValue("id"), tokenHash, s.clockNow())
	if errors.Is(err, ErrCheckoutUnauthorized) {
		writeAPIError(w, unauthorized())
		return
	}
	if errors.Is(err, ErrCheckoutNotFound) {
		writeAPIError(w, resourceNotFound("checkout"))
		return
	}
	if err != nil {
		writeAPIError(w, unexpectedError())
		return
	}
	if s.sandboxLookup != nil && c.orderID == "sandbox-order-1" && c.status != Paid {
		if err := s.refreshSandboxSession(r.Context(), c); err != nil {
			writeAPIError(w, dependencyError("PAYMENT_PROCESSING", "Não foi possível consultar o pagamento", "CHARGE_LOOKUP_FAILED"))
			return
		}
		c, err = s.store.Status(r.Context(), r.PathValue("id"), tokenHash, s.clockNow())
		if err != nil {
			writeAPIError(w, unexpectedError())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": c.status, "expires_at": c.expiresAt.UTC().Format(time.RFC3339)})
}

func (s *Server) simulatePaid(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.SimulatePaid(r.Context(), r.PathValue("id"), s.clockNow())
	if errors.Is(err, ErrCheckoutNotFound) {
		writeAPIError(w, resourceNotFound("checkout"))
		return
	}
	if err != nil {
		writeAPIError(w, unexpectedError())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": string(c.status)})
}

func (s *Server) receiveWooviWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIError(w, decodeAPIError(err))
		return
	}
	valid, err := s.webhookAuth.Verify(r.Context(), rawBody, r.Header.Get("x-webhook-signature"))
	if err != nil {
		writeAPIError(w, dependencyError("DEPENDENCY_UNAVAILABLE", "Não foi possível validar o webhook no momento", "WEBHOOK_KEY_LOOKUP_FAILED"))
		return
	}
	if !valid {
		writeAPIError(w, unauthorized())
		return
	}
	var event WooviChargeEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		writeAPIError(w, malformedRequest())
		return
	}
	if event.Charge.CorrelationID == "" || len(event.Charge.CorrelationID) > 128 {
		writeAPIError(w, invalidParams("charge.correlationID", "deve conter entre 1 e 128 caracteres"))
		return
	}
	result, err := s.store.ApplyChargeEvent(r.Context(), event, sha256.Sum256(rawBody))
	if err != nil {
		writeAPIError(w, unexpectedError())
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
		return errMalformedJSON
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

func writeSessionJSON(w http.ResponseWriter, status int, c *checkout, token string) {
	body := map[string]any{
		"checkout_id": c.id, "access_token": token, "status": c.status, "amount_cents": c.amount,
		"currency": "BRL", "expires_at": c.expiresAt.UTC().Format(time.RFC3339),
	}
	if c.brCode != "" {
		body["pix_copy_paste"] = c.brCode
	}
	writeJSON(w, status, body)
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
