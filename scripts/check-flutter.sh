#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

case "${1:-}" in
  package)
    cd packages/woovi_pix_flutter
    mise exec -- flutter pub get --enforce-lockfile
    mise exec -- dart format --output=none --set-exit-if-changed lib test
    mise exec -- flutter analyze
    mise exec -- flutter test
    ;;
  example)
    cd example
    mise exec -- flutter pub get --enforce-lockfile
    mise exec -- dart format --output=none --set-exit-if-changed lib test
    mise exec -- flutter analyze
    mise exec -- flutter test
    mise exec -- flutter test --dart-define=WOOVI_SANDBOX=true
    mise exec -- flutter build apk --debug
    mise exec -- flutter build apk --debug --dart-define=WOOVI_SANDBOX=true
    ;;
  *)
    echo 'Usage: bash scripts/check-flutter.sh package|example' >&2
    exit 2
    ;;
esac
