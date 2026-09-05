#!/usr/bin/env bash
# Module header gate: every maintained source file opens with a comment
# header stating what it is and where it stands in the whole.
#
# Installed 2026-09-05 with zero stock: every existing Go and shell file
# already carries a header, so this is a hard gate, not a ratchet. Exempt:
# testdata (fixture content, not maintained sources).

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
missing=""

while IFS= read -r file; do
  rel="${file#"$REPO"/}"
  case "$rel" in
    *.go)  marker='//' ;;
    *.sh)  marker='#'  ;;
  esac
  if ! head -3 "$file" | grep -q "^$marker"; then
    missing="${missing}${rel}"$'\n'
  fi
done < <(find "$REPO" \( -name '*.go' -o -name '*.sh' \) \
  -not -path '*/testdata/*' -not -path '*/.git/*' | sort)

if [[ -n "$missing" ]]; then
  printf 'ERROR: source files missing a module header (a comment in the\n'
  printf 'first three lines stating what the file is and where it stands):\n\n'
  printf '%s' "$missing"
  exit 1
fi
