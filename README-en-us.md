# Woovi Pix Flutter SDK

[Português (Brasil)](README.md)

A Flutter package and example merchant backend for displaying a Pix checkout. The Flutter app talks only to the merchant backend; Woovi credentials and payment confirmation stay server-side.

> **Example, not production:** the default flow uses a simulated payment service provider (PSP). An explicitly enabled sandbox flow creates test charges at Woovi, never at its production API. No sandbox/live calls have been executed during development. Credentials remain server-side. Production authentication and fulfillment must be implemented by the merchant.

## Repository layout

- `packages/woovi_pix_flutter`: reusable Flutter package (controller, HTTP transport, model, and checkout view).
- `example/`: runnable Flutter demonstration app.
- `examples/backend`: Go merchant backend, PostgreSQL migrations, and local PSP simulator.

## Requirements

- Flutter 3.24+ / Dart 3.5+
- Go 1.27.1 (pinned in `.mise.toml` and `examples/backend/go.mod`)
- PostgreSQL 18 (image pinned by digest in `examples/backend/docker-compose.yml`)

## Run the demo

Start the isolated local PostgreSQL service and backend (migrations apply on startup):

```sh
cd examples/backend
mise install
docker compose -p woovi-pix-flutter-sdk-demo up -d postgres
PGPORT="$(docker compose -p woovi-pix-flutter-sdk-demo port postgres 5432 | sed 's/.*://')"
DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable" ENABLE_DEMO_PSP=true mise exec -- go run ./cmd/server
```

In another terminal:

```sh
cd example
mise exec -- flutter pub get
mise exec -- flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

The default URL is for the Android emulator. For the iOS simulator, use `--dart-define=DEMO_BACKEND=http://127.0.0.1:8080`. HTTP is restricted to local development.

The app creates a fictional order through the backend, displays the simulated Pix QR payload, and polls the checkout status. API endpoints are versioned under `/v1`; JSON bodies use `snake_case` except inbound Woovi webhook payloads, whose field names are dictated by Woovi. With `ENABLE_DEMO_PSP=true`, use `POST /v1/demo/checkouts/{id}/pay` to simulate PSP confirmation. This unauthenticated route is disabled by default, must never be enabled outside local demos, and cannot be enabled together with the webhook receiver. The UI's paid callback is only a presentation signal; merchant fulfillment must read its own order state on the server.

## Test the simulated payment

In the app, tap **Pagar com Pix**. The QR is fictional: do not pay with a real bank.
In another terminal, retrieve the checkout for the same order:

```sh
curl -s http://localhost:8080/v1/checkout-sessions \
  -H 'Content-Type: application/json' -d '{"order_id":"demo-order-1"}'
# Copy checkout_id from the response:
curl -X POST http://localhost:8080/v1/demo/checkouts/PASTE_CHECKOUT_ID/pay
```

The app should show payment confirmation and hide the QR/copy-and-paste code.
The token may rotate on replay; previous tokens remain valid until they expire.

## Test with the real Woovi sandbox

### Account and credentials

Register a separate account at <https://app.woovi-sandbox.com/>. Production
credentials do not work in the sandbox. Generate a test AppID in that account's
API/permissions settings. The documented endpoint is `https://api.woovi-sandbox.com`,
without `/api/v1` in the base URL.
Sources: [test environment](https://developers.woovi.com/en/docs/test-environment),
[sandbox configuration](https://developers.woovi.com/en/docs/sdk/node/how-to-configure-for-sandbox).

Do not use a real banking app to pay. Use the test account/simulation feature
in the Woovi sandbox dashboard to complete the charge.
Also prepare the test bank account as described in
[testing Pix without real money](https://developers.woovi.com/en/docs/test-environment/test-account/flow-company-bank-test)
and follow [paying a test Pix charge](https://developers.woovi.com/en/docs/test-environment/test-account/test-pay-pix)
to confirm the charge created by the app.

### Backend (terminal 1, starting at the repository root)

This mode uses a single fixed order, `sandbox-order-1`, worth BRL 25.99, and a
shared test session. This is not production authentication. Use an isolated
database, do not expose this server publicly, and do not reuse the token
outside this example.

```sh
mise install
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-sandbox up -d --wait postgres
PGPORT="$(docker compose -p woovi-pix-flutter-sdk-sandbox port postgres 5432 | sed 's/.*://')"
export DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable"
export ENABLE_WOOVI_SANDBOX_CHECKOUT=true
export WOOVI_API_BASE_URL=https://api.woovi-sandbox.com
export SANDBOX_SESSION_TOKEN="$(openssl rand -hex 32)"
# Bash: read without echoing or putting a literal credential in shell history
read -rsp 'Sandbox AppID: ' WOOVI_APP_ID
export WOOVI_APP_ID
export DEMO_ADDR=127.0.0.1:8080
unset ENABLE_DEMO_PSP ENABLE_WOOVI_WEBHOOK ENABLE_WOOVI_RECONCILIATION
# Record SANDBOX_SESSION_TOKEN for the app terminal (not the AppID).
mise exec -- go run ./cmd/server
```

Configuration rejects a missing PostgreSQL connection, short token, URL other
than the sandbox base, and mixing with the simulated PSP. The simulated
creation route is absent. The independent worker is disabled in this example:
replays reconcile ambiguous outcomes through GET, and authenticated status
queries check the PSP through GET.

### App (terminal 2, starting at the repository root)

```sh
cd example
export SANDBOX_SESSION_TOKEN='PASTE_BACKEND_TEST_TOKEN'
export SANDBOX_IDEMPOTENCY_KEY="$(openssl rand -hex 16)"
# Record and preserve this key even after an error/app restart.
mise exec -- flutter pub get
mise exec -- flutter run \
  --dart-define=WOOVI_SANDBOX=true \
  --dart-define=SANDBOX_SESSION_TOKEN="$SANDBOX_SESSION_TOKEN" \
  --dart-define=SANDBOX_IDEMPOTENCY_KEY="$SANDBOX_IDEMPOTENCY_KEY" \
  --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

Tap **Pagar com Pix**: the app now calls `/v1/merchant/checkout-sessions` and
creates a sandbox charge. Simulate completion in Woovi: the app polls the
backend every 3 seconds; the backend looks up the charge through GET and
confirms `paid` without a QR.
The AppID is never sent to Flutter. The test session token **is embedded in
the example app**: do not distribute this APK or use this mechanism in production.

After a timeout/`PAYMENT_PROCESSING`, retry using **the same key**. The backend
does not repeat the POST to the PSP; it attempts recovery through GET. Do not
delete the database/attempt to bypass ambiguous outcomes. Use a new key only
after the PSP confirms `EXPIRED`; a paid order cannot accept another charge.
For an independent test run, use another isolated database and another test
order/environment without discarding an attempt whose outcome remains uncertain.
Lookup errors do not mean payment succeeded.

### iOS and devices

On the iOS simulator, with the backend on the same Mac, replace the URL with
`http://127.0.0.1:8080` and run `mise exec -- flutter run -d SIMULATOR_ID ...`.
List device IDs with `mise exec -- flutter devices`. iOS requires macOS/Xcode
and has not been validated here. On a physical device, use the host's LAN IP
and appropriate server binding, only on a private test network (HTTP does not
protect the token). Prefer a simulator.

### Optional webhook

GET polling works without a tunnel/webhook. To test the webhook, configure an
accessible HTTPS URL for `/v1/webhooks/woovi` in the sandbox dashboard and set
the following backend variables:

```sh
export ENABLE_WOOVI_WEBHOOK=true
export WOOVI_WEBHOOK_PUBLIC_KEYS_URL=https://api.woovi-sandbox.com/api/v1/webhook/public-keys
```

Restart the backend; never enable `ENABLE_DEMO_PSP` alongside it. Do not expose
the test session through an unrestricted public tunnel; expose only the webhook
path. Webhooks are verified by the backend, and merchant fulfillment remains
outside this example. Automated tests use a local HTTP fixture, not Woovi.

## Merchant backend contract

`POST /v1/checkout-sessions` is the simulated flow and accepts `order_id`. `POST /v1/merchant/checkout-sessions` requires a configured `MerchantCheckoutAuthorizer`, PostgreSQL, and server-side `ChargeCreator`; sandbox mode explicitly configures a test-only adapter. It requires `Idempotency-Key`, derives the amount from the authorizer/store, and never trusts an amount from Flutter. Matching keys recover the same checkout; key reuse across orders conflicts; new attempts require confirmed `EXPIRED`. `COMPLETED` returns `paid` without `pix_copy_paste`. `GET /v1/checkout-sessions/{id}` uses a checkout-scoped bearer token. Errors follow `{ message, error_code, extra? }` with stable codes: malformed JSON 400, semantic validation 422, not-found 404, and generic unexpected errors 500. Network errors are not payment states. Sandbox authentication is not a substitute for merchant authentication/ownership validation.

The backend also contains a `WooviChargeClient` implementing documented charge creation (`POST /api/v1/charge`) and lookup (`GET /api/v1/charge/{id}`, where `id` may be a charge ID or `correlationID`), using `Authorization: <AppID>` and cent values. `SubmitChargeForOrder` orchestrates one durable reservation, the exclusive `reserved` → `submitting` transition, one POST, and persistence/ambiguity handling; concurrent callers and PSP failures are covered by PostgreSQL and local HTTP-fixture tests. The authenticated merchant checkout adapter binds this orchestration to a durable `Idempotency-Key` and creates the session from the authoritative PSP ledger. PostgreSQL retains ambiguous outcomes as `unknown`; such attempts are never POSTed again. Reconciliation claims use expiring PostgreSQL leases with `SKIP LOCKED`; failed GET lookups schedule bounded exponential backoff with jitter. The optional GET-only worker is disabled by default and uses conservative bounded settings. Keep the AppID server-only.

The opt-in `POST /v1/webhooks/woovi` receiver verifies `x-webhook-signature` using Woovi RSA public keys, atomically deduplicates raw events, and applies matching completed/expired charge events by correlation ID and cent value. Unsupported events are ignored; inconsistent signed events are quarantined with HTTP 202. Sandbox can use its own keys endpoint as documented above. This is **not merchant fulfillment**: neither webhook status nor the mobile callback alone authorizes production order delivery. The AppID must remain server-only. PostgreSQL persists checkout/token/event/attempt state; the memory fallback is only for the default simulated flow. Neither mode makes this example production-ready.

Verified docs: [API authentication](https://developers.woovi.com/en/docs/apis/api-getting-started) uses `Authorization: <AppID>` and requires HTTPS; [charge creation](https://developers.woovi.com/en/docs/charge/how-to-create-charge-using-api) uses `POST /api/v1/charge`, cent values, and `correlationID`; [charge completed webhook payload](https://developers.woovi.com/en/docs/webhook/examples/webhook-charge-payload) includes event `OPENPIX:CHARGE_COMPLETED`, `charge.correlationID`, status `COMPLETED`, and `pix.status=CONFIRMED`; [webhook public keys](https://developers.woovi.com/en/docs/webhook/seguranca/webhook-public-keys) specifies `x-webhook-signature = base64(RSA-SHA256(raw body))`, accepts rotating public keys, and recommends caching for one hour with stale-cache fallback; [webhook retries](https://developers.woovi.com/en/docs/webhook/webhook-retry) documents 8 attempts and retries on HTTP >400/unavailability. Charge lookup/status semantics and PSP idempotency guarantees still need verification. No live/sandbox API request has been made.

## Development checks

```sh
cd packages/woovi_pix_flutter && mise exec -- dart format --set-exit-if-changed lib test && mise exec -- flutter analyze && mise exec -- flutter test
cd ../../example && mise exec -- dart format --set-exit-if-changed lib test && mise exec -- flutter analyze && mise exec -- flutter test && mise exec -- flutter test --dart-define=WOOVI_SANDBOX=true && mise exec -- flutter build apk --debug
cd ../examples/backend && mise exec -- gofmt -l . && mise exec -- go test -count=1 ./... && mise exec -- go build ./... && mise exec -- go vet ./...
```

Run backend integration tests with the actual PostgreSQL service by setting the same `DATABASE_URL` before `go test -count=1 ./...`. Without this variable, tests that depend on PostgreSQL are skipped.

See `CHANGELOG.md` and `LICENSE` for project history and terms.
