---
name: integration-testing
description: Use when adding, modifying, or reviewing Go or Flutter tests, handlers, repositories, HTTP adapters, workers, or CI test targets. Require real local infrastructure, protocol-level fixtures for external APIs, regression coverage, and repeatable integration tests.
---

# Integration Testing

## Choose the test boundary

- Unit tests exercise isolated logic, invariants, and error branches. Inject clocks and randomness where needed.
- Integration tests exercise real wiring, migrations, SQL, transactions, serialization, authentication, and concurrency.
- Run locally available dependencies for real: PostgreSQL, Valkey, brokers, and the filesystem. Do not replace them with in-memory fakes in integration tests.
- For external APIs that cannot run locally, use a local HTTP fixture and the actual production adapter. Never call development, staging, sandbox, or production APIs as part of the automated suite.
- Flutter widget tests exercise rendering and interactions. Device integration tests complement them; they do not replace backend integration coverage.

## Required coverage

1. Every added or modified handler/route gets an integration test through the real router and real database.
2. Every added or modified repository method gets coverage against the actual database.
3. Verify persisted state directly, not just the HTTP response. Cover rollback, ownership, authorization rejection, deduplication, and side effects.
4. Test HTTP adapters against success, timeout, malformed JSON, unexpected fields, authentication errors, and server errors. Check request paths, headers, and payloads without exposing secrets.
5. Exercise concurrency-sensitive behavior with concurrent callers and assert the intended number of winners or external requests.
6. Cover Flutter views through widget tests: initial/loading/error states, retry, terminal states, callbacks, and disposal.
7. Every bug fix gets a regression test that fails for the reported reason before the implementation changes.

## Isolation and repeatability

- Generate unique identifiers per test/run; avoid fixed values under unique constraints.
- Register cleanup with `t.Cleanup` or the test framework's teardown facilities.
- Use only an isolated test database. Never delete unrelated data or discard uncertain payment attempts to make a test pass.
- Inject time for expiration and retry scenarios. Check timestamp round trips, timezone offsets, and invalid input.
- Respect the suite's existing layout. Use Go build tags only if the repository uses them; do not invent nonexistent test targets.
- If integration tests require `DATABASE_URL`, configure it explicitly. A skipped integration suite is not a passing integration suite.
- Run the full suite with real infrastructure 2–3 times against the same isolated database to expose accumulated-data issues.

## Commands

Discover existing task targets first. For a Go module:

```sh
go test -race -count=1 ./...
# Only when the repository uses this build tag:
go test -race -tags=integration -count=1 ./...
```

Set the test database connection through the approved local test configuration without printing resolved credentials.

For each Flutter package and app:

```sh
flutter test
flutter test --coverage
# Only when device tests exist and a supported device is configured:
flutter test integration_test
```

Use the local `quality-gate` skill before declaring an implementation complete. Coverage must not exclude business logic, authorization, handlers, repositories, or adapters merely to improve the reported percentage.
