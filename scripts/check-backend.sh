#!/usr/bin/env bash
set -euo pipefail

# Fail before running tests rather than accepting skipped PostgreSQL coverage.
: "${DATABASE_URL:?Set DATABASE_URL to an isolated local test database}"
cd "$(dirname "${BASH_SOURCE[0]}")/../examples/backend"

test -z "$(mise exec -- gofmt -l .)"
mise exec -- go mod tidy -diff
mise exec -- go mod verify
mise exec -- go vet ./...
mise exec -- golangci-lint run ./...

test_log="$(mktemp)"
trap 'rm -f "$test_log"' EXIT
mise exec -- go test -race -count=3 -v ./... | tee "$test_log"
if grep -q -- '--- SKIP:' "$test_log"; then
  echo 'Skipped tests are not permitted in the backend quality gate.' >&2
  exit 1
fi

mise exec -- go build ./...
