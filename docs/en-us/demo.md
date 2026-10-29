# Local demo

[Home](../../README-en-us.md) · [Português (Brasil)](../pt-br/demo.md)

Try the checkout without a Woovi account or payment integration. The backend
creates a fictional BRL 25.99 order; a local demo route simulates confirmation.

> The QR is fictional: **do not pay with a real bank**. The confirmation route
> has no authentication and should only be enabled for this local demo.

## Before you start

- Install [mise](https://mise.jdx.dev/getting-started.html) and Docker Compose.
- Have an Android emulator available. For iOS, use a Mac with Xcode.
- Run commands in Bash from the **repository root**.
- `mise install` installs the Flutter and Go versions from `.mise.toml`. Compose
  uses PostgreSQL 18 with an image pinned by digest.

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

`DEMO_ADDR` sets the listen address as `host:port`; keep `127.0.0.1` so the
backend only accepts connections from this computer. If port `8080` is busy,
change it, for example to `127.0.0.1:9090`, and use `9090` in the app URL and
`curl` commands. Compose publishes PostgreSQL on a dynamic local port; the
commands above discover it and construct `DATABASE_URL`. The database
credentials are public and only for this demo. Migrations run at server startup,
and the Compose volume preserves data between runs. Do not enable
`ENABLE_WOOVI_WEBHOOK` alongside `ENABLE_DEMO_PSP`.

## 2. Open the app

In the second terminal, starting again at the repository root:

```sh
cd example
mise exec -- flutter pub get
mise exec -- flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

Tap **Pagar com Pix**. The app creates or recovers the `demo-order-1` checkout,
displays a fictional QR/copyable code, and polls its status every 3 seconds. The
QR does not represent a real payment.

| Where the app runs | Backend URL |
| --- | --- |
| Android emulator | `http://10.0.2.2:8080` |
| iOS simulator, backend on the same Mac | `http://127.0.0.1:8080` |

For iOS, replace the URL in the command. HTTP is only for local development;
iOS has not been validated here. This guide covers emulators/simulators; a
physical phone cannot reach a backend bound to `127.0.0.1` without additional
network configuration.

If `UNAUTHORIZED` appears, confirm that the app is using this local demo without
`--dart-define=WOOVI_SANDBOX=true`; that define switches the endpoint and
authentication to the sandbox flow. The local demo uses the checkout's bearer
token for status requests. If the token is invalid, start checkout again from
the app. This is not a QR-code failure.

## 3. Confirm the simulated payment

In another terminal, create/retrieve the session for the same order and copy
`checkout_id` from the response:

```sh
curl -sS http://localhost:8080/v1/checkout-sessions \
  -H 'Content-Type: application/json' -d '{"order_id":"demo-order-1"}'
# Use the returned checkout_id:
curl -X POST http://localhost:8080/v1/demo/checkouts/PASTE_CHECKOUT_ID/pay
```

**Expected result:** the app shows simulated payment confirmation and hides the
QR/code. Retrieving a session may rotate its bearer token; earlier tokens remain
valid until expiry. The `/pay` route is unauthenticated: keep the server bound to
`127.0.0.1`, never expose it to a network or the internet. The app callback is
only a UI signal, not authorization to fulfill an order.

## Stop the demo

Stop the app and backend with `Ctrl+C`. To stop PostgreSQL without deleting its
persistent volume:

```sh
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-demo stop postgres
```

Run the command above from the repository root. Do not remove volumes to bypass
uncertain attempts; demo data is local and persists.

Next: [try the Woovi sandbox](sandbox.md) or [run the tests](development.md).
