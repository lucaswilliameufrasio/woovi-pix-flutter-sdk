# Woovi Pix Flutter SDK

[Português (Brasil)](README.md)

**Pix checkout for Flutter, with server-side payment confirmation.**
The package displays a QR code, lets users copy the Pix code, and tracks payment status.

```text
Flutter app → Merchant backend → Woovi
```

Woovi credentials stay on the backend — never in the app.

> **Example project, not production-ready.** Start with simulated payments,
> without real money. The merchant must implement authentication, order ownership
> validation, and server-side fulfillment.

## What's included

| Directory | Contents |
| --- | --- |
| [`packages/woovi_pix_flutter`](packages/woovi_pix_flutter) | Reusable package: checkout view, controller, and HTTP transport. |
| [`example`](example) | Flutter app for trying the checkout. |
| [`examples/backend`](examples/backend) | Go backend, PostgreSQL, and payment simulator. |

## Start with the local demo

You do not need a Woovi account to try the simulated flow.

1. Install the tools and **start the backend**.
2. Open the app in an emulator and tap **Pagar com Pix**.
3. Simulate confirmation and watch the UI update.

**[Open the demo guide →](docs/en-us/demo.md)**

You will need Docker Compose and mise. The project pins Flutter **3.47.5**,
Go **1.27.1**, and PostgreSQL **18**. The package declares Flutter 3.24+ / Dart 3.5+
support; local checks use the pinned version.

## Guides by goal

| I want to… | Guide |
| --- | --- |
| Run and confirm a fictional payment | [Local demo](docs/en-us/demo.md) |
| Integrate the UI and manage SDK resources | [Flutter checkout](docs/en-us/flutter.md) |
| Create a charge in a Woovi test account | [Sandbox](docs/en-us/sandbox.md) |
| Understand the API, idempotency, and security | [Backend contract](docs/en-us/backend.md) |
| Run formatting, lint, tests, and builds | [Development](docs/en-us/development.md) |

## Important limitations

- **Do not pay the demo QR with a real bank.**
- The Flutter callback does not authorize order fulfillment.
- The real sandbox and iOS have not been validated during development.
- The package has not been published on pub.dev; integrations use this repository's code.

[History](CHANGELOG.md) · [License](LICENSE)
