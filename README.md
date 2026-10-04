# Woovi Pix Flutter SDK

A Flutter package and example merchant backend for displaying a Pix checkout. The Flutter app talks only to the merchant backend; Woovi credentials and payment confirmation stay server-side.

> **Demo only:** this repository uses a local simulated PSP and does not create real Woovi charges. The optional webhook receiver and isolated Woovi adapter/reconciliation primitives are not wired to checkout and have not been called against live or sandbox Woovi. The backend supports PostgreSQL persistence; if `DATABASE_URL` is omitted it falls back to ephemeral memory.

## Repository layout

- `packages/woovi_pix_flutter`: reusable Flutter package (controller, transport, model and checkout view).
- `example/`: runnable Flutter demonstration app.
- `examples/backend`: Go merchant backend, PostgreSQL migrations and local PSP simulator.

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
flutter pub get
flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

The default URL is for the Android emulator. For the iOS simulator use `--dart-define=DEMO_BACKEND=http://127.0.0.1:8080`. This is local development HTTP only.

The app creates a fictional order through the backend, displays the simulated Pix QR payload and polls the checkout status. API endpoints are versioned under `/v1`; JSON bodies use `snake_case` except inbound Woovi webhook payloads, whose field names are dictated by Woovi. With `ENABLE_DEMO_PSP=true`, use `POST /v1/demo/checkouts/{id}/pay` to simulate PSP confirmation. This unauthenticated route is disabled by default, must never be enabled outside local demos, and cannot be enabled together with the webhook receiver. The UI's paid callback is only a presentation signal; merchant fulfillment must read its own order state on the server.

## Merchant backend contract

`POST /v1/checkout-sessions` accepts an `order_id`; the server owns the amount and recovers a pending checkout idempotently within the process. The returned short-lived opaque token is scoped to that checkout. `GET /v1/checkout-sessions/{id}` requires it as a bearer token and can only read status. Errors follow `{ message, error_code, extra? }`, with stable `SNAKE_UPPER` codes; malformed JSON is HTTP 400, semantic validation is HTTP 422 with `extra.validation_errors[]`, not-found is 404, and unexpected failures return a generic 500 without internal details. The demo uses `pending`, `paid`, and `expired`; a network error is not a payment state.

The backend also contains a `WooviChargeClient` implementing documented charge creation (`POST /api/v1/charge`) and lookup (`GET /api/v1/charge/{id}`, where `id` may be a charge ID or `correlationID`), using `Authorization: <AppID>` and cent values. `SubmitChargeForOrder` orchestrates one durable reservation, the exclusive `reserved` → `submitting` boundary, one POST, and persistence/ambiguity handling; concurrent callers and PSP failures are covered by PostgreSQL + local HTTP-fixture tests. This orchestration remains deliberately **not wired to checkout creation or a scheduled background worker**, and the app makes no real or sandbox calls. PostgreSQL has a durable charge-attempt reservation/state ledger: reserve immutable amount/correlation first, commit `submitting` before the network boundary, retain ambiguous outcomes as `unknown`, and expose those for reconciliation; a persisted uncertain attempt cannot be marked submitting and POSTed again. Reconciliation claims use expiring PostgreSQL leases with `SKIP LOCKED`; failed GET lookups clear the lease and schedule capped exponential backoff, while crashes are recoverable after lease expiration. `ReconcilePendingChargeAttempts` is still an explicitly invoked bounded drainer, not an automatically scheduled worker. It performs only GETs and isolates per-attempt failures; consistent observations are persisted while 404/network/mismatch cases remain unresolved. Keep the AppID server-only.

The opt-in `POST /v1/webhooks/woovi` receiver (enable with `ENABLE_WOOVI_WEBHOOK=true`) verifies `x-webhook-signature` against all RSA keys from Woovi's public key endpoint, then atomically deduplicates raw event bodies and applies matching completed/expired charge events by correlation ID and cent value. The inbound webhook payload retains Woovi's provider-defined field names. The receiver's own success/error response follows our API contract. The event schema is based on Woovi's documented `OPENPIX:CHARGE_COMPLETED` payload; unsupported events are recorded and ignored. Signed but inconsistent amount/status events are quarantined in the event ledger and answered with HTTP 202 to avoid retries; operator reconciliation is still needed. Woovi documents 8 retries with exponential intervals when the endpoint returns HTTP >400 or is unavailable. This route is **not connected to real charge creation or merchant order fulfillment**; the demo only creates simulated PSP charges. Do not treat webhook status or the mobile callback alone as production order fulfillment. Do not put an AppID or API key in a Flutter app. PostgreSQL persists demo orders, checkout sessions, bearer-token hashes and event fingerprints; the in-memory fallback is single-process and loses state on restart. Neither store makes this demo production-ready.

Verified docs: [API authentication](https://developers.woovi.com/en/docs/apis/api-getting-started) uses `Authorization: <AppID>` and requires HTTPS; [charge creation](https://developers.woovi.com/en/docs/charge/how-to-create-charge-using-api) uses `POST /api/v1/charge`, cent values and `correlationID`; [charge completed webhook payload](https://developers.woovi.com/en/docs/webhook/examples/webhook-charge-payload) includes event `OPENPIX:CHARGE_COMPLETED`, `charge.correlationID`, status `COMPLETED`, and `pix.status=CONFIRMED`; [webhook public keys](https://developers.woovi.com/en/docs/webhook/seguranca/webhook-public-keys) specifies `x-webhook-signature = base64(RSA-SHA256(raw body))`, accepts rotating public keys, and says cache for one hour with stale-cache fallback; [webhook retries](https://developers.woovi.com/en/docs/webhook/webhook-retry) documents 8 attempts and retries on HTTP >400/unavailability. Charge lookup/status semantics and PSP idempotency guarantees still need verification. No live/sandbox API request has been made.

## Development checks

```sh
cd packages/woovi_pix_flutter && flutter pub get && dart format --set-exit-if-changed . && flutter analyze && flutter test
cd ../../example && flutter pub get && dart format --set-exit-if-changed . && flutter analyze && flutter test && flutter build apk --debug
cd ../examples/backend && gofmt -l . && go test -count=1 ./... && go build ./... && golangci-lint run ./...
```

Run backend integration tests with the actual PostgreSQL service by setting the same `DATABASE_URL` before `go test -count=1 ./...`.

See `CHANGELOG.md` and `LICENSE` for project status and terms.
