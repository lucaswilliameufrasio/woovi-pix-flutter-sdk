# Woovi Pix Flutter SDK

A Flutter package and example merchant backend for displaying a Pix checkout. The Flutter app talks only to the merchant backend; Woovi credentials and payment confirmation stay server-side.

> **Example, not production:** the default flow uses a simulated PSP. An explicitly enabled sandbox flow creates test charges at Woovi, never at its production API. No sandbox/live calls have been executed during development. Credentials remain server-side. Production auth and fulfillment must be implemented by the merchant.

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
mise exec -- flutter pub get
mise exec -- flutter run --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

The default URL is for the Android emulator. For the iOS simulator use `--dart-define=DEMO_BACKEND=http://127.0.0.1:8080`. This is local development HTTP only.

The app creates a fictional order through the backend, displays the simulated Pix QR payload and polls the checkout status. API endpoints are versioned under `/v1`; JSON bodies use `snake_case` except inbound Woovi webhook payloads, whose field names are dictated by Woovi. With `ENABLE_DEMO_PSP=true`, use `POST /v1/demo/checkouts/{id}/pay` to simulate PSP confirmation. This unauthenticated route is disabled by default, must never be enabled outside local demos, and cannot be enabled together with the webhook receiver. The UI's paid callback is only a presentation signal; merchant fulfillment must read its own order state on the server.

## Testar o pagamento simulado

No app, toque em **Pagar com Pix**. O QR é fictício: não pague com um banco.
Em outro terminal, recupere o checkout do mesmo pedido:

```sh
curl -s http://localhost:8080/v1/checkout-sessions \
  -H 'Content-Type: application/json' -d '{"order_id":"demo-order-1"}'
# Copie checkout_id da resposta:
curl -X POST http://localhost:8080/v1/demo/checkouts/COLE_O_CHECKOUT_ID/pay
```

O app deve mostrar pagamento confirmado e esconder QR/código. O token pode ser
rotacionado no replay; tokens anteriores permanecem válidos até sua expiração.

## Testar com Woovi sandbox de verdade

### Conta e credenciais

Cadastre uma conta separada em <https://app.woovi-sandbox.com/>. Dados de produção
não funcionam no sandbox. Gere um AppID de teste nas permissões/API dessa conta.
O endpoint documentado é `https://api.woovi-sandbox.com`, sem `/api/v1` na base.
Fontes: [ambiente de teste](https://developers.woovi.com/en/docs/test-environment),
[configuração sandbox](https://developers.woovi.com/en/docs/sdk/node/how-to-configure-for-sandbox).

Não use um app bancário real para pagar. Use a conta de teste/funcionalidade de
simulação do painel Woovi sandbox para concluir a cobrança.
Prepare também a conta bancária de teste conforme
[Testando Pix sem dinheiro real](https://developers.woovi.com/en/docs/test-environment/test-account/flow-company-bank-test)
e siga [Pagando uma Cobrança Pix de Teste](https://developers.woovi.com/en/docs/test-environment/test-account/test-pay-pix)
para confirmar a cobrança criada pelo app.

### Backend (terminal 1, a partir da raiz)

Este modo usa um único pedido fixo `sandbox-order-1`, de R$ 25,99, e uma sessão
de teste compartilhada. Não é autenticação de produção. Use banco isolado, não
exponha esse servidor publicamente e não reutilize o token fora deste exemplo.

```sh
mise install
cd examples/backend
docker compose -p woovi-pix-flutter-sdk-sandbox up -d --wait postgres
PGPORT="$(docker compose -p woovi-pix-flutter-sdk-sandbox port postgres 5432 | sed 's/.*://')"
export DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable"
export ENABLE_WOOVI_SANDBOX_CHECKOUT=true
export WOOVI_API_BASE_URL=https://api.woovi-sandbox.com
export SANDBOX_SESSION_TOKEN="$(openssl rand -hex 32)"
# Bash: leitura sem eco nem credencial literal no histórico
read -rsp 'AppID do sandbox: ' WOOVI_APP_ID
export WOOVI_APP_ID
export DEMO_ADDR=127.0.0.1:8080
unset ENABLE_DEMO_PSP ENABLE_WOOVI_WEBHOOK ENABLE_WOOVI_RECONCILIATION
# Anote SANDBOX_SESSION_TOKEN para usar no terminal do app (não o AppID).
mise exec -- go run ./cmd/server
```

A configuração rejeita PostgreSQL ausente, token curto, URL diferente da base
sandbox e mistura com PSP simulado. A rota simulada de criação fica ausente.
O worker independente fica desabilitado neste exemplo: replays reconciliam
ambiguidades por GET e consultas autenticadas de status verificam o PSP por GET.

### App (terminal 2, a partir da raiz)

```sh
cd example
export SANDBOX_SESSION_TOKEN='COLE_O_TOKEN_DE_TESTE_DO_BACKEND'
export SANDBOX_IDEMPOTENCY_KEY="$(openssl rand -hex 16)"
# Anote e preserve essa chave mesmo após erro/restart do app.
mise exec -- flutter pub get
mise exec -- flutter run \
  --dart-define=WOOVI_SANDBOX=true \
  --dart-define=SANDBOX_SESSION_TOKEN="$SANDBOX_SESSION_TOKEN" \
  --dart-define=SANDBOX_IDEMPOTENCY_KEY="$SANDBOX_IDEMPOTENCY_KEY" \
  --dart-define=DEMO_BACKEND=http://10.0.2.2:8080
```

Toque **Pagar com Pix**: agora chama `/v1/merchant/checkout-sessions` e cria uma
cobrança no sandbox. Simule sua conclusão na Woovi: o app consulta o backend a
cada 3s; o backend consulta a cobrança via GET e confirma `paid` sem QR.
AppID nunca é enviado ao Flutter. O token da sessão de teste **é incorporado ao
app exemplo**: não distribua esse APK, nem use esse mecanismo em produção.

Se houver timeout/`PAYMENT_PROCESSING`, pressione novamente usando **a mesma
chave**. O backend não repete o POST ao PSP; tenta recuperar via GET. Não apague
o banco/tentativa para contornar ambiguidades. Somente após `EXPIRED` confirmado
no PSP use nova chave; um pedido pago não admite outra cobrança. Para uma nova
rodada independente, use outro banco isolado e outro pedido/ambiente de teste,
sem descartar uma tentativa ainda incerta. Erros de consulta não significam pago.

### iOS e dispositivos

No simulador iOS, com backend no mesmo Mac, substitua a URL por
`http://127.0.0.1:8080` e execute `mise exec -- flutter run -d ID_DO_SIMULADOR ...`.
Veja IDs com `mise exec -- flutter devices`. iOS precisa de macOS/Xcode e não
foi validado aqui. No aparelho físico, use IP LAN do host e binding apropriado;
somente em rede privada de teste (HTTP não protege o token). Preferir simulador.

### Webhook opcional

Polling GET funciona sem túnel/webhook. Para testar webhook, configure no painel
sandbox uma URL HTTPS acessível para `/v1/webhooks/woovi` e no backend:

```sh
export ENABLE_WOOVI_WEBHOOK=true
export WOOVI_WEBHOOK_PUBLIC_KEYS_URL=https://api.woovi-sandbox.com/api/v1/webhook/public-keys
```

Reinicie o backend; nunca habilite `ENABLE_DEMO_PSP` junto. Não exponha a sessão
de teste via um túnel público irrestrito; exponha apenas o caminho de webhook.
Webhooks são verificados pelo backend, e fulfillment comercial continua fora
do exemplo. Os testes automatizados usam HTTP fixture local, não a Woovi.

## Contrato do backend merchant

`POST /v1/checkout-sessions` is the simulated flow and accepts `order_id`. `POST /v1/merchant/checkout-sessions` requires a configured `MerchantCheckoutAuthorizer`, PostgreSQL and server-side `ChargeCreator`; sandbox mode configures a test-only adapter explicitly. It requires `Idempotency-Key`, derives amount from the authorizer/store and never trusts an amount from Flutter. Matching keys recover the same checkout; key reuse across orders conflicts; new attempts require confirmed EXPIRED. COMPLETED returns `paid` without `pix_copy_paste`. `GET /v1/checkout-sessions/{id}` uses a checkout-scoped bearer token. Errors follow `{ message, error_code, extra? }` with stable codes: malformed JSON 400, semantic validation 422, not-found 404 and generic unexpected errors 500. Network errors are not payment states. Sandbox authentication is not a substitute for merchant auth/ownership validation.

The backend also contains a `WooviChargeClient` implementing documented charge creation (`POST /api/v1/charge`) and lookup (`GET /api/v1/charge/{id}`, where `id` may be a charge ID or `correlationID`), using `Authorization: <AppID>` and cent values. `SubmitChargeForOrder` orchestrates one durable reservation, the exclusive `reserved` → `submitting` boundary, one POST, and persistence/ambiguity handling; concurrent callers and PSP failures are covered by PostgreSQL + local HTTP-fixture tests. The authenticated merchant checkout adapter binds this orchestration to a durable `Idempotency-Key` and creates the session from the authoritative PSP ledger. PostgreSQL retains ambiguous outcomes as `unknown`; such attempts are never POSTed again. Reconciliation claims use expiring PostgreSQL leases with `SKIP LOCKED`; failed GET lookups schedule bounded exponential backoff with jitter. The optional GET-only worker is disabled by default and uses conservative bounded settings. Keep the AppID server-only.

The opt-in `POST /v1/webhooks/woovi` receiver verifies `x-webhook-signature` using Woovi RSA public keys, atomically deduplicates raw events and applies matching completed/expired charge events by correlation ID and cent value. Unsupported events are ignored; inconsistent signed events are quarantined with HTTP 202. Sandbox can use its own keys endpoint as documented above. This is **not merchant fulfillment**: neither webhook status nor the mobile callback alone authorizes production order delivery. AppID must remain server-only. PostgreSQL persists checkout/token/event/attempt state; the memory fallback is only for the default simulated flow. Neither mode makes this example production-ready.

Verified docs: [API authentication](https://developers.woovi.com/en/docs/apis/api-getting-started) uses `Authorization: <AppID>` and requires HTTPS; [charge creation](https://developers.woovi.com/en/docs/charge/how-to-create-charge-using-api) uses `POST /api/v1/charge`, cent values and `correlationID`; [charge completed webhook payload](https://developers.woovi.com/en/docs/webhook/examples/webhook-charge-payload) includes event `OPENPIX:CHARGE_COMPLETED`, `charge.correlationID`, status `COMPLETED`, and `pix.status=CONFIRMED`; [webhook public keys](https://developers.woovi.com/en/docs/webhook/seguranca/webhook-public-keys) specifies `x-webhook-signature = base64(RSA-SHA256(raw body))`, accepts rotating public keys, and says cache for one hour with stale-cache fallback; [webhook retries](https://developers.woovi.com/en/docs/webhook/webhook-retry) documents 8 attempts and retries on HTTP >400/unavailability. Charge lookup/status semantics and PSP idempotency guarantees still need verification. No live/sandbox API request has been made.

## Development checks

```sh
cd packages/woovi_pix_flutter && mise exec -- dart format --set-exit-if-changed lib test && mise exec -- flutter analyze && mise exec -- flutter test
cd ../../example && mise exec -- dart format --set-exit-if-changed lib test && mise exec -- flutter analyze && mise exec -- flutter test && mise exec -- flutter test --dart-define=WOOVI_SANDBOX=true && mise exec -- flutter build apk --debug
cd ../examples/backend && mise exec -- gofmt -l . && mise exec -- go test -count=1 ./... && mise exec -- go build ./... && mise exec -- go vet ./...
```

Run backend integration tests with the actual PostgreSQL service by setting the same `DATABASE_URL` before `go test -count=1 ./...`.

See `CHANGELOG.md` and `LICENSE` for project status and terms.
