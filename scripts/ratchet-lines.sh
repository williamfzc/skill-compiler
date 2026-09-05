#!/usr/bin/env bash
# Line-budget ratchet for hand-written Go sources.
#
# MAX_LINES was chosen from the observed distribution at install time
# (2026-09-05): every non-test module except internal/cli/cli.go fits under
# 320 lines, so the ratchet freezes the stock (cli.go, recorded in the
# baseline) and blocks new growth past it. Test files are exempt:
# characterization and end-to-end suites run long by nature.
#
# The baseline may only shrink. Removing a line happens in the same commit as
# the fix that pays the debt. Adding a line requires owner approval: a new
# violation is either fixed or, if a module must legitimately grow past the
# budget, approved by an owner who adds the line in a reviewed commit.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MAX_LINES=320
BASELINE="$REPO/scripts/ratchet-lines.baseline.txt"

violations="$(find "$REPO" -name '*.go' -not -path '*/testdata/*' -not -name '*_test.go' \
  -exec wc -l {} + \
  | awk -v max="$MAX_LINES" '$1 > max && $2 != "total" {print $2}' \
  | sed -e "s|^$REPO/||" -e '/^$/d' | sort -u)"

known="$(grep -v '^#' "$BASELINE" 2>/dev/null | sed -e '/^$/d' | sort -u)"
new="$(comm -13 <(printf '%s\n' "$known") <(printf '%s\n' "$violations"))"

if [[ -n "$new" ]]; then
  printf 'ERROR: Go sources over %s lines not registered in the baseline:\n\n' "$MAX_LINES"
  printf '%s\n' "$new"
  printf '\nThe baseline (%s) may only shrink.\n' "${BASELINE#"$REPO"/}"
  printf 'Fix the violation; a new baseline line needs owner approval.\n'
  exit 1
fi
