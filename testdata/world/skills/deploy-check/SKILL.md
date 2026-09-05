---
name: deploy-check
description: The go/no-go deploy guard: verify build, health endpoints, and migrations before release.
---

# Deploy check

Run this before every release. It is deliberately small and strict.

## What to verify

1. The build is green from a clean checkout.
2. Health endpoints return ready after boot, not just alive.
3. Any migration is reversible -- if you cannot name the rollback, stop.

Report a single verdict: **go** or **no-go with the first failing check**.
The release skill treats a missing verdict as no-go.
