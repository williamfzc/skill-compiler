#!/usr/bin/env bash
# Protected surface: foundation files whose change is a decision, not a
# commit. The gate fails while the working tree touches one, so a "small
# tweak" to a contract needs a named approval rather than a passing diff.
#
# Escape hatch (owner only): ALLOW_FOUNDATION=1 for this one run, and a
# "foundation:" commit prefix recording the approval.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

PROTECTED=(
  "AGENTS.md"                      # the working constitution
  "docs/state-contract.md"         # the state contract; change is a design decision
  "docs/user-stories.md"           # acceptance source; change re-scopes the product
  "scripts/check.sh"               # the standing self-check
  "scripts/ratchet-lines.sh"       # the ratchet gate
  "scripts/protected-surface.sh"   # this gate
  "scripts/check-headers.sh"       # the module header gate
  "testdata/skills-sh/SOURCES.md"  # pinned upstream provenance
)

if [[ "${ALLOW_FOUNDATION:-0}" == "1" ]]; then
  exit 0
fi

changed="$(cd "$REPO" && git status --porcelain -- "${PROTECTED[@]}")"

if [[ -n "$changed" ]]; then
  printf 'ERROR: protected foundation files have uncommitted changes:\n\n'
  printf '%s\n' "$changed"
  printf '\nFoundation changes need owner approval: ALLOW_FOUNDATION=1 for\n'
  printf 'this run and a "foundation:" commit prefix recording the decision.\n'
  exit 1
fi
