# Test with the Woovi sandbox

[Home](../../README-en-us.md) · [Português (Brasil)](../pt-br/sandbox.md)

Use this guide after trying the [local demo](demo.md).
The onboarding and run commands use the example's Go CLI.

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

## 2. Configure and run

Prerequisites: `mise`, Docker Compose, and a connected Flutter simulator/device.
On first run, the CLI prompts for the sandbox AppID without echoing it, generates
test tokens, and saves them to `examples/backend/.env`. On macOS/Linux, the file
is created with `0600` permissions and is ignored by Git. Do not share it.

```sh
mise install
cd examples/backend
mise exec -- go run ./cmd/woovi-demo
```

This single command starts local PostgreSQL, the backend, and Flutter. You can
also run onboarding separately with `mise exec -- go run ./cmd/woovi-demo
setup`. The AppID stays in the Go process; the CLI creates a protected temporary
file to pass the session token to Flutter without putting it in shell history.
The token is still embedded in the test app: do not distribute this build or
use this mechanism in production.

For an iOS simulator, choose the target explicitly:

```sh
mise exec -- go run ./cmd/woovi-demo run --platform ios --device-id SIMULATOR_ID
```

To start a fresh round after the previous one is terminal, run
`mise exec -- go run ./cmd/woovi-demo run --new-round`. **Do not use `--new-round`
if the previous outcome is uncertain**; reopen the app with the same config to
recover that checkout using `mise exec -- go run ./cmd/woovi-demo run --resume`,
then enter the order ID and idempotency key shown by the app. `Ctrl+C` stops the
app and backend, then stops PostgreSQL without deleting its volume.

Tap **Pagar com Pix** to create a sandbox charge. Confirm it using the test
account and Woovi's test dashboard; the app polls the backend every 3 seconds.
The CLI only starts local services and does not request a payment from Woovi. It
rejects the simulated PSP configuration and disables the independent worker;
ambiguous outcomes are reconciled through GET.

### Backend-served test console

Open <http://127.0.0.1:8080/sandbox> in a browser. Enter the same
`SANDBOX_SESSION_TOKEN`, current round's order ID, and
`SANDBOX_IDEMPOTENCY_KEY` used by the app. The console recovers the checkout
with the same key and offers:

- **Request successful payment in Woovi sandbox**: the backend looks up the
  charge and calls Woovi's official test-payment endpoint using its
  `transactionID`; the AppID never reaches the browser. The `paid` response
  still depends on the subsequent PSP GET lookup.
- **Local: paid / expired / expired then paid**: synthetic in-memory responses
  for observing app behavior. They do not change Woovi or the PostgreSQL ledger
  and disappear when the backend restarts.

A paid sandbox charge is terminal and cannot be paid again. After the app shows
`paid` (or `expired`), tap **Iniciar nova rodada sandbox**: the backend creates
a new R$ 25.99 test order and the app generates a new idempotency key. The
current round's order and key are shown on screen for recovery after an app
restart; pass them as `SANDBOX_ORDER_ID` and `SANDBOX_IDEMPOTENCY_KEY` if that
round's outcome is still uncertain. Enter the new order in the test console as
well. Do not reuse the key from a paid round.

The console does not simulate signed webhooks or internal PSP states. Use local
HTTP fixtures to validate those contracts. Woovi documents the
[test-payment operation](https://developers.woovi.com/en/docs/test-environment/test-account/test-pay-pix);
no real call to the sandbox endpoint was made during development. Keep the
backend accessible only on the test machine/private network.

### If `UNAUTHORIZED` appears

When creating a checkout, this 401 is returned by the backend before it calls
Woovi: the app's `X-Sandbox-Session` header does not match the token configured
in the Go process (or is shorter than 32 characters). Verify both terminals use
the same token, restart the app after changing the `--dart-define`, and confirm
the request reaches the sandbox backend at the port configured by `DEMO_ADDR`.
An invalid AppID normally appears later as a Woovi integration error, not this
local authorization error.

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
