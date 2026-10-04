# Local demo

[Home](../../README-en-us.md) · [Português (Brasil)](../pt-br/demo.md)

Try the checkout without a Woovi account or real money.
The backend creates a fictional order; a local command simulates payment confirmation.

> The QR is fictional: **do not pay with a real bank**. The confirmation route
> has no authentication and should only be enabled for this local demo.

## Before you start

- Install [mise](https://mise.jdx.dev/getting-started.html) and Docker with Compose.
- Have an Android emulator available. For iOS, use a Mac with Xcode.
- Run commands from the **repository root**, in Bash.
- `mise install` installs the Flutter and Go versions from `.mise.toml`.
  Compose uses PostgreSQL 18 with an image pinned by digest.

## 1. Start the backend

In the first terminal:

```sh
mise install
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-demo up -d --wait postgres
PGPORT="$(docker compose -p woovi-pix-flutter-sdk-demo port postgres 5432 | sed 's/.*://')"
export DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable"
export ENABLE_DEMO_PSP=true
export DEMO_ADDR=127.0.0.1:8080
mise exec -- go run ./cmd/server
```

The credentials above are public and only for the local demo database.
Migrations apply when the backend starts.
Do not enable `ENABLE_WOOVI_WEBHOOK` alongside `ENABLE_DEMO_PSP`.

## 2. Open the app

In the second terminal, starting again at the repository root:

```sh
cd example
mise exec -- flutter pub get
mise exec -- flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

Tap **Pagar com Pix**. The app should display a QR and copyable code
while polling the checkout status.

| Where the app runs | Backend URL |
| --- | --- |
| Android emulator | `http://10.0.2.2:8080` |
| iOS simulator, backend on the same Mac | `http://127.0.0.1:8080` |

For iOS, replace the URL in the command. HTTP is only for local development;
iOS has not been validated here.

## 3. Confirm the simulated payment

In another terminal, retrieve the checkout for the same order:

```sh
curl -s http://localhost:8080/v1/checkout-sessions \
  -H 'Content-Type: application/json' -d '{"order_id":"demo-order-1"}'
# Copy checkout_id from the response:
curl -X POST http://localhost:8080/v1/demo/checkouts/PASTE_CHECKOUT_ID/pay
```

**Expected result:** the app shows payment confirmation and hides the QR/code.
Retrieval may rotate the token; earlier tokens remain valid until they expire.
The callback is only a UI signal, not an authorization to fulfill the order.

## Stop the demo

Stop the app and backend with `Ctrl+C`. To stop the database without deleting data:

```sh
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-demo stop postgres
```

Start the command at the repository root. Do not remove volumes to bypass uncertain attempts.

Next: [try the Woovi sandbox](sandbox.md) or [run the tests](development.md).
