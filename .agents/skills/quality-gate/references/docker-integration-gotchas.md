# Running integration tests against real local dependencies via docker-compose

Concrete recipe, distilled from actually doing this (works for any stack —
adjust the run commands to the project's language/tooling).

## 1. Bring up the dependency

```
cd <repo>
docker compose up -d postgres valkey   # or whatever the services are named
docker compose ps                      # confirm "healthy"
```

Use the project's documented isolated test configuration. Do not read or print
resolved secrets from `.env` files. If an example configuration is needed, use
fictional test-only values and check that private configuration is ignored by Git.

## 2. Run migrations

Check the `Makefile` for a `migrate`/`migrate-up` target and run it once against the
fresh container.

## 3. Run the suite

```
DATABASE_URL="postgres://user:pass@127.0.0.1:<port>/<db>" <project test command>
```

Prefer scoping to the test file/package/module you touched first for fast iteration,
then run the full suite before calling the task done.

## 4. Common gotchas

- **Leftover rows from a failed prior run**: if a test seeds rows with a fixed literal
  value (a hardcoded `code`, email, etc.) under a `UNIQUE` constraint, and a previous
  failed run's cleanup never ran the delete (e.g. the process was killed, or cleanup
  itself is scoped to a different key than the unique column), the next run fails with a
  duplicate-key error that looks unrelated to your change. Fix at the source: generate
  unique values per run (prefix + generated uuid), not fixed literals — this also makes
   repeated reruns safe. Clean up only rows owned by the test, in an isolated test
   database; ask before deleting data whose ownership is uncertain.
- **UUID-typed columns fed non-UUID literals**: tests sometimes insert literal
  strings like `'sess'`/`'user'` into columns that are typed `UUID` in Postgres — this
  passes against a mock/fake pool but fails for real with `invalid input syntax for type
  uuid`. Only a real-DB run catches this; it's exactly the class of bug this whole
  workflow exists to catch.
- **Decimal/numeric round-trip equality**: comparing values struct-wise breaks when the
  DB round-trips a value through a `NUMERIC(p,s)` column, because the scanned value's
  internal scale reflects the column's scale even though the value is mathematically
  identical. This bites every decimal library (shopspring `decimal` in Go,
  `rust_decimal`, JS decimal libs). Use the library's value-equality method
  (`a.Equal(b)`-style), not struct equality.
- **Reproduce red before fixing**: when chasing a reported bug, write the integration
  test first and confirm it reproduces the *exact* real error (e.g. the literal Postgres
  error message) before touching implementation code. A test that never failed for the
  right reason proves nothing.

## 5. Cleanup

Leave the containers running if more work is likely in the same session; otherwise
`docker compose down` (add `-v` only if you intend to discard the DB volume — ask first
if that volume might hold data the user cares about).
