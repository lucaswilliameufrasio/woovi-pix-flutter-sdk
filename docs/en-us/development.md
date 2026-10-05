# Development and checks

[Home](../../README-en-us.md) · [Português (Brasil)](../pt-br/development.md)

Run each block below in a terminal at the **repository root**.
Install the pinned versions first:

```sh
mise install
```

## CI and reproducible commands

The [Quality](../../.github/workflows/quality.yml) workflow runs the gates on pushes
to `main` and pull requests, using the mise versions and Compose PostgreSQL image.
Actions are pinned to commits, with read-only permissions and no Woovi credentials.
There is no deployment or package/APK publication.

Use the same scripts locally:

```sh
bash scripts/check-flutter.sh package
bash scripts/check-flutter.sh example
```

The example script builds debug APKs in default and sandbox modes, without real tokens.
Both scripts enforce dependency lockfiles. For the backend, prepare the database
as described below and run `bash scripts/check-backend.sh` from the root with
`DATABASE_URL` exported and `golangci-lint` **2.14.0** installed. This gate fails
if the variable is missing or any test is skipped.

## Flutter package

```sh
(
  set -e
  cd packages/woovi_pix_flutter
  mise exec -- flutter pub get
  mise exec -- dart format --output=none --set-exit-if-changed lib test
  mise exec -- flutter analyze
  mise exec -- flutter test
)
```

## Example app

```sh
(
  set -e
  cd example
  mise exec -- flutter pub get
  mise exec -- dart format --output=none --set-exit-if-changed lib test
  mise exec -- flutter analyze
  mise exec -- flutter test
  mise exec -- flutter test --dart-define=WOOVI_SANDBOX=true
  mise exec -- flutter build apk --debug
)
```

Sandbox-mode tests use test configuration without calling Woovi.
An iOS build requires macOS/Xcode and has not been validated in this environment.

## Backend with real PostgreSQL

Do not omit `DATABASE_URL`: without it, integration tests are skipped.
Use an isolated database, not one containing real payments or uncertain attempts.

```sh
(
  set -e
  cd examples/backend
  docker compose -p woovi-pix-flutter-sdk-checks up -d --wait postgres
  PGPORT="$(docker compose -p woovi-pix-flutter-sdk-checks port postgres 5432 | sed 's/.*://')"
  export DATABASE_URL="postgres://woovi_demo:woovi_demo@127.0.0.1:${PGPORT}/woovi_demo?sslmode=disable"
  test -z "$(mise exec -- gofmt -l .)"
  mise exec -- go vet ./...
  mise exec -- go test -race -count=3 ./...
  mise exec -- go build ./...
)
```

`test -z` fails the check if gofmt finds files needing formatting.
Tests use PostgreSQL and a local PSP HTTP fixture, not the external API.
Three runs against the same database help detect isolation and concurrency issues.

For additional lint, with `golangci-lint` installed and compatible with the Go version:

```sh
(
  cd examples/backend
  mise exec -- golangci-lint run ./...
)
```

## Apply formatting

The checks above do not change files. To apply formatting:

```sh
mise exec -- dart format packages/woovi_pix_flutter/lib packages/woovi_pix_flutter/test example/lib example/test
mise exec -- gofmt -w examples/backend
git diff --check
```

## Stop the test database

```sh
(
  cd examples/backend
  docker compose -p woovi-pix-flutter-sdk-checks stop postgres
)
```

Data remains in the volume; do not delete it without confirming it can be discarded.

## Delivery criteria

- Formatting, lint, tests, and builds must pass before committing.
- Skipped integration tests do not count as validation.
- Local checks do not replace iOS or real sandbox validation.
- The CI workflow repeats local gates; check its remote execution after pushing.
- Local conventions and skills are listed in [AGENTS.md](../../AGENTS.md).
