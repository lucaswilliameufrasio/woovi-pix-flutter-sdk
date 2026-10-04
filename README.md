# Woovi Pix Flutter SDK

A Flutter package and example merchant backend for displaying a Pix checkout. The Flutter app talks only to the merchant backend; Woovi credentials and payment confirmation stay server-side.

> **Demo only:** this repository currently uses a local simulated PSP. It does not create real Woovi charges. The first tracer bullet backend uses an in-memory store and is not production-ready; PostgreSQL persistence, webhook verification, reconciliation and deployment are not yet implemented.

## Repository layout

- `packages/woovi_pix_flutter`: reusable Flutter package (controller, transport, model and checkout view).
- `example/`: runnable Flutter demonstration app.
- `examples/backend`: Go merchant backend and local PSP simulator (in-memory tracer bullet; PostgreSQL is pending).

## Requirements

- Flutter 3.24+ / Dart 3.5+
- Go 1.23+

## Run the demo

Start the backend:

```sh
cd examples/backend
go run ./cmd/server
```

In another terminal:

```sh
cd example
flutter pub get
flutter run
```

The app creates a fictional order through the backend, displays the simulated Pix QR payload and polls the checkout status. Use `POST /demo/checkouts/{id}/pay` to simulate PSP confirmation. The UI's paid callback is only a presentation signal; merchant fulfillment must read its own order state on the server.

## Merchant backend contract

`POST /v1/checkout-sessions` accepts an `order_id`; the server owns the amount and recovers a pending checkout idempotently within the process. The returned short-lived opaque token is scoped to that checkout. `GET /v1/checkout-sessions/{id}` requires it as a bearer token and can only read status. The demo uses `pending`, `paid`, and `expired`; a network error is not a payment state.

The example PSP simulator is not the Woovi API. Before adapting the backend to Woovi, verify current API authentication, charge lookup, webhook signature/validation, retry semantics, and idempotency guarantees against the official documentation and a supervised test account. Do not put an AppID or API key in a Flutter app. This in-memory demo is single-process only; restart loses checkout/payment state. It intentionally does not claim PostgreSQL-backed durability or production-grade idempotency.

## Development checks

```sh
cd packages/woovi_pix_flutter && flutter pub get && dart format --set-exit-if-changed . && flutter analyze && flutter test
cd ../../examples/backend && gofmt -w . && go test ./... && go build ./...
```

See `CHANGELOG.md` and `LICENSE` for project status and terms.
