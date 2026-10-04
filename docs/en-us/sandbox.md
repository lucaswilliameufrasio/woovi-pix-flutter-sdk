# Test with the Woovi sandbox

[Home](../../README-en-us.md) · [Português (Brasil)](../pt-br/sandbox.md)

Use this guide after trying the [local demo](demo.md).
Commands start at the repository root and use Bash.

> **Testing only:** use a separate account and isolated database. Do not expose
> the server publicly or pay with a real bank. No sandbox calls have been
> validated during development.

## 1. Prepare the account

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

## 2. Start the backend (terminal 1)

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

## 3. Start the app (terminal 2)

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

## 4. Recover without duplicating charges

- After a timeout or `PAYMENT_PROCESSING`, retry with **the same key**.
  The backend recovers through GET; it does not repeat the POST to the PSP.
- Do not delete the database or attempt to bypass ambiguous outcomes.
- Use a new key only after the PSP confirms `EXPIRED`.
  A paid order cannot accept another charge.
- For an independent run, use another isolated database and test order/environment
  without discarding an attempt whose outcome remains uncertain.
- Lookup errors do not mean payment succeeded.

## iOS and physical devices

On the iOS simulator, with the backend on the same Mac, replace the URL with
`http://127.0.0.1:8080` and run `mise exec -- flutter run -d SIMULATOR_ID ...`.
List device IDs with `mise exec -- flutter devices`. iOS requires macOS/Xcode
and has not been validated here. On a physical device, use the host's LAN IP
and appropriate server binding, only on a private test network (HTTP does not
protect the token). Prefer a simulator.

## Optional webhook

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

See also: [backend contract and security](backend.md).
