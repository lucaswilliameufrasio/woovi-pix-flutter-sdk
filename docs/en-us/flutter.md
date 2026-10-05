# Use the Flutter checkout

[Home](../../README-en-us.md) · [Português (Brasil)](../pt-br/flutter.md)

The package has not been published on pub.dev. Use the local dependency in the
[example app](../../example/pubspec.yaml) to try it.
The session comes from the merchant backend; never create Woovi charges directly from the app.

## Initialize once

After receiving a valid `CheckoutSession` from the backend, create resources
outside `build` — for example, in `initState` or after session creation completes:

```dart
final transport = HttpCheckoutTransport(baseUrl: merchantBackendUrl);
final controller = CheckoutController(session: session, transport: transport);
```

Use `PixCheckoutView(controller: controller, onPaid: onPaid)` in the UI.
The callback signals a server-confirmed payment, but **does not authorize
order fulfillment**. Commercial authorization stays on the backend.

## Observe state

`controller.state` is an immutable union carrying the last server-confirmed
payment status:

| Request state | Contents |
| --- | --- |
| `CheckoutReady` | Last confirmed `status`. |
| `CheckoutRefreshing` | Request in progress, previous `status`, and failure count. |
| `CheckoutFailure` | Previous `status`, transport error, and failure count. |
| `CheckoutDisposed` | Disposed controller; ignores late responses. |

The `status`, `transportError`, and `isRefreshing` getters remain available.
Network errors never become payment states. `paid` does not regress; `expired`
can be corrected to `paid` for a late Pix confirmed by the backend.
The controller pauses requests when the app becomes inactive and refreshes on resume.

## Release resources

The controller **does not own the transport**. When the screen/flow ends:

```dart
controller.dispose();
transport.close();
```

- Without an injected `client`, the transport creates and closes its own HTTP client.
- With an injected `client`, `transport.close()` does not close the caller's client.
  Its owner must close it when no consumer needs it anymore.
- `close()` is idempotent and prevents new requests or use of late responses.
- `requestTimeout` defaults to 10 seconds. A timeout does not itself cancel
  the underlying HTTP operation; release resources when abandoning the flow.
- Do not recreate controllers/clients on every rebuild. The view does not dispose them for you.

See also: [backend contract](backend.md) and [local checks](development.md).
