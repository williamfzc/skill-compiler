#!/usr/bin/env bash
# Repo self-check: run before commit or in CI.
#
# 1. compiler builds, vets, and --help is usable
# 2. unit + user-story tests pass (go test; synthetic trees / fake $HOME,
#    never touches the real skill dirs)
# 3. compiler can compile this repo itself with no error
#
# Exits with code 1 if any step fails.

set -uo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$REPO/skillc"
FAIL=0

say()  { printf '%s\n' "$*"; }
ok()   { printf '  OK   %s\n' "$*"; }
bad()  { printf '  FAIL %s\n' "$*"; FAIL=1; }

say "=== 1. build, vet, and --help ==="
if (cd "$REPO" && go build -o "$BIN" ./cmd/skillc) && (cd "$REPO" && go vet ./...); then
  ok "go build + go vet"
else
  bad "build or vet failed"
fi
if "$BIN" --help >/dev/null 2>&1; then
  ok "skillc --help"
else
  bad "skillc --help failed"
fi

say ""
say "=== 2. go test (unit + user-story integration) ==="
if (cd "$REPO" && go test ./...); then
  ok "all go tests pass"
else
  bad "go test has failures"
fi

say ""
say "=== 3. compile this repo itself ==="
if "$BIN" check --only-root "$REPO" >/dev/null; then
  ok "self-compile has no error"
else
  bad "self-compile reported errors"
fi

say ""
if [[ "$FAIL" -eq 0 ]]; then
  say "self-check passed."
else
  say "self-check failed."
fi
exit "$FAIL"
