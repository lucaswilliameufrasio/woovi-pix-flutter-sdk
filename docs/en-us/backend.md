# Backend contract and security

[Home](../../README-en-us.md) · [Português (Brasil)](../pt-br/backend.md)

Flutter displays the checkout. The merchant backend controls authorization,
order amount, and payment confirmation. Fulfillment is the merchant's
responsibility and is not implemented in this example.

## Example routes

| Method and path | Purpose | Protection |
| --- | --- | --- |
| `POST /v1/checkout-sessions` | Create/retrieve the simulated checkout using `order_id`. | Simulated flow only. |
| `POST /v1/merchant/checkout-sessions` | Create/retrieve a merchant charge checkout. | Merchant authorization and `Idempotency-Key`. |
| `GET /v1/checkout-sessions/{id}` | Look up checkout status. | Checkout-scoped bearer token. |
| `POST /v1/demo/checkouts/{id}/pay` | Simulate PSP confirmation. | Unauthenticated; requires `ENABLE_DEMO_PSP=true`, local only. |
| `POST /v1/webhooks/woovi` | Receive Woovi notifications. | Explicit enablement and RSA signature. |

Routes use `/v1`. JSON bodies use `snake_case` except Woovi webhook events,
which follow the provider's field names.
Sandbox mode does not expose the simulated creation route.

## Authorization and idempotency

The merchant flow requires `MerchantCheckoutAuthorizer`, PostgreSQL, and a
server-side `ChargeCreator`. The authorizer/store determines the order and
amount; **do not trust an amount supplied by Flutter**.
Sandbox explicitly configures a test-only authentication adapter.

| Situation | Behavior |
| --- | --- |
| Same `Idempotency-Key` and order | Recovers the same checkout. |
| Same key for another order | Returns a conflict. |
| New attempt | Requires PSP-confirmed `EXPIRED`. |
| Already paid order | Cannot accept another charge. |
| PSP returns `COMPLETED` | Session is `paid`, without `pix_copy_paste`. |
| Timeout or ambiguous response | Recovery through GET, without repeating the POST to the PSP. |

Sandbox authentication does not replace production authentication or order
ownership validation. Network errors are not payment states.

## HTTP errors

Format: `{ message, error_code, extra? }`. `error_code` is stable;
`message` is human-readable.

| HTTP | Meaning |
| --- | --- |
| 400 | Malformed JSON. |
| 422 | Semantic validation failure. |
| 404 | Resource not found. |
| 500 | Unexpected error, with a generic response. |

## Charge creation and reconciliation

`WooviChargeClient` uses `Authorization: <AppID>` and cent values:

- Creation: `POST /api/v1/charge`.
- Lookup: `GET /api/v1/charge/{id}`, using a charge ID or `correlationID`.

`SubmitChargeForOrder` coordinates a durable reservation, the exclusive
`reserved` → `submitting` transition, one POST, and result persistence.
The merchant adapter binds the operation to the idempotency key and creates
the session from the authoritative PSP ledger.

- Ambiguous outcomes remain `unknown` in PostgreSQL and are never POSTed again.
- Reconciliation uses expiring leases and `SKIP LOCKED`.
- GET failures use bounded exponential backoff with jitter.
- The independent reconciliation worker is GET-only, uses conservative limits,
  and is disabled by default.
- Concurrent callers and PSP failures are tested with real PostgreSQL and a
  local HTTP fixture, without external calls.

## Webhook verification

The receiver verifies `x-webhook-signature` with Woovi RSA public keys:

1. Verifies the signature over the raw body.
2. Atomically deduplicates events.
3. Checks the correlation ID and cent amount.
4. Applies matching completed/expired charge events.

Unsupported events are ignored. Inconsistent signed events are quarantined
with HTTP 202. Sandbox can use its own keys endpoint;
see [webhook configuration](sandbox.md#optional-webhook).

## What this does not guarantee

- The AppID is server-only; neither the mobile callback nor a webhook alone
  authorizes production order fulfillment.
- PostgreSQL persists checkout, token, event, and attempt state. In-memory
  storage is only a fallback for the default simulation.
- Charge lookup/status semantics and PSP idempotency guarantees still need
  external verification.
- No production or sandbox API calls have been made during development.
  Local tests do not make the example production-ready.

## Woovi references consulted

| Documentation | Described contract |
| --- | --- |
| [Authentication](https://developers.woovi.com/en/docs/apis/api-getting-started) | AppID in `Authorization`; HTTPS required. |
| [Charge creation](https://developers.woovi.com/en/docs/charge/how-to-create-charge-using-api) | `POST /api/v1/charge`, cents, and `correlationID`. |
| [Completed charge](https://developers.woovi.com/en/docs/webhook/examples/webhook-charge-payload) | `OPENPIX:CHARGE_COMPLETED`, `charge.correlationID`, `COMPLETED`, `pix.status=CONFIRMED`. |
| [Public keys](https://developers.woovi.com/en/docs/webhook/seguranca/webhook-public-keys) | `base64(RSA-SHA256(raw body))`, rotation, and a one-hour cache with stale-cache fallback. |
| [Webhook retries](https://developers.woovi.com/en/docs/webhook/webhook-retry) | 8 attempts; retries on HTTP >400 or unavailability. |
