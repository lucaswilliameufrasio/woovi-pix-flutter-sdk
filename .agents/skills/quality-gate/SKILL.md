---
name: quality-gate
description: Before declaring any coding task done, all tests (unit + integration, against a real local DB when the project has one), lint, format, and build must pass for every project touched. Use whenever finishing an implementation, before summarizing results as done, or when asked to verify everything passes.
---

# Quality gate

A task is not done until build, format, lint, and the full test suite (unit + integration)
are all green — for every repo touched, not just the files edited. "The specific test I
added passes" is not the bar; "the project is green" is.

## The checklist, every time

For each repository touched in the task — **the whole repo, not just the files you
edited**:

1. **Build**: the project's build command (`go build ./...`, `cargo build --workspace`,
   `pnpm build`, ...). Must produce no errors.
2. **Format**: the project's formatter (`gofmt -l .`, `cargo fmt --check`, biome/prettier)
   run against the **whole repo**, not scoped to touched files. Anything it prints must
   be fixed before commit, even if you didn't touch that file.
3. **Lint**: run the project's full lint target (`golangci-lint run ./...`,
   `cargo clippy --workspace --all-targets -- -D warnings`, eslint, ...) — zero issues,
   repo-wide, before commit.
4. **Unit tests**: the project's unit test command, with result caching disabled
   (`go test -count=1 ./...`; cargo tests run fresh by default) — a cached `ok` from
   before your change proves nothing. All packages green.
5. **Integration tests**: if the project has an integration suite (build tags, a
   `docker-compose.yml`, a `make test-integration` target), **bring up the real
   dependencies (Postgres, Valkey, broker...) and run it for real** — never skip
   integration coverage by only running unit/mocked tests, and never trust a cached
   result. Run it 2-3 times in a row before trusting it — a local dev DB that's been
   through many prior test runs accumulates data, and that's exactly what surfaces
   ordering/pagination/limit assumptions that don't hold at scale (see
   `references/docker-integration-gotchas.md` for concrete gotchas hit this way).

Only after all five are green, **freshly re-run right before the commit** (not "were
green a few tool calls ago"), may the task be reported as complete or committed.

## Handling failures found along the way

Run the check yourself before claiming it's done — do not rely on the user to catch what
you skipped. If you say "all green" and the user then runs it and finds errors, that's a
process failure, not a matter of taste.

- **Failures in code you changed**: fix them. They block completion.
- **Pre-existing failures anywhere in the repo** (gofmt, lint, flaky/order-dependent
  tests) also block completion once you're asked to verify before commit — this user's
  bar is the whole repo green, not just your diff. Fix them if they're mechanical and
  behavior-preserving (unused-parameter renames, gofmt alignment, a flaky assertion that
  assumes an unbounded/first-page result). Ask first only for changes with actual
  behavioral or architectural weight — don't ask permission for a one-line `_` rename.
- **Never** report "all tests pass" when you only ran the subset you added, or when you're
  quoting a cached/earlier run. Re-run the whole suite for every touched repo, fresh,
  immediately before saying it's done.

## Multi-repo tasks

When a fix spans repos (e.g. a BFF + a backing microservice), the gate applies to **each**
repo independently. Don't consider the task done because the repo you started in is green
while a sibling repo you also edited wasn't rechecked.
