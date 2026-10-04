# Woovi Pix Flutter SDK

A Flutter package and example merchant backend for displaying a Pix checkout. The Flutter app talks only to the merchant backend; Woovi credentials and payment confirmation stay server-side.

> **Demo only:** this repository uses a local simulated PSP. It does not create real Woovi charges. The backend supports PostgreSQL persistence; if `DATABASE_URL` is omitted it falls back to ephemeral memory. Webhook verification, real PSP reconciliation and deployment are not implemented.

## Repository layout

- `packages/woovi_pix_flutter`: reusable Flutter package (controller, transport, model and checkout view).
- `example/`: runnable Flutter demonstration app.
- `examples/backend`: Go merchant backend, PostgreSQL migrations and local PSP simulator.

## Requirements

- Flutter 3.24+ / Dart 3.5+
- Go 1.23+

## Run the demo

Start the isolated local PostgreSQL service and backend (migrations apply on startup):

```sh
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-demo up -d postgres
PGPORT="$(docker compose -p woovi-pix-flutter-sdk-demo port postgres 5432 | sed 's/.*://')"
DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable" go run ./cmd/server
```

In another terminal:

```sh
cd example
flutter pub get
flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

The default URL is for the Android emulator. For the iOS simulator use `--dart-define=DEMO_BACKEND=http://127.0.0.1:8080`. This is local development HTTP only.

The app creates a fictional order through the backend, displays the simulated Pix QR payload and polls the checkout status. Use `POST /demo/checkouts/{id}/pay` to simulate PSP confirmation. The UI's paid callback is only a presentation signal; merchant fulfillment must read its own order state on the server.

## Merchant backend contract

`POST /v1/checkout-sessions` accepts an `order_id`; the server owns the amount and recovers a pending checkout idempotently within the process. The returned short-lived opaque token is scoped to that checkout. `GET /v1/checkout-sessions/{id}` requires it as a bearer token and can only read status. The demo uses `pending`, `paid`, and `expired`; a network error is not a payment state.

The example PSP simulator is not the Woovi API. Before adapting the backend to Woovi, verify current API authentication, charge lookup, webhook signature/validation, retry semantics, and idempotency guarantees against the official documentation and a supervised test account. Do not put an AppID or API key in a Flutter app. PostgreSQL persists demo orders, checkout sessions and expiring bearer-token hashes; the in-memory fallback is single-process and loses state on restart. Neither store makes this demo production-ready.

## Development checks

```sh
cd packages/woovi_pix_flutter && flutter pub get && dart format --set-exit-if-changed . && flutter analyze && flutter test
cd ../../example && flutter pub get && dart format --set-exit-if-changed . && flutter analyze && flutter test && flutter build apk --debug
cd ../examples/backend && gofmt -l . && go test -count=1 ./... && go build ./... && golangci-lint run ./...
```

Run backend integration tests with the actual PostgreSQL service by setting the same `DATABASE_URL` before `go test -count=1 ./...`.

See `CHANGELOG.md` and `LICENSE` for project status and terms.
