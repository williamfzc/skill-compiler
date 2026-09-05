---
name: release
description: Cut a release: run the deploy guard, publish release notes, and hand off to on-call.
---

# Release orchestrator

Guides an agent through cutting a release. Follow the steps in order; do not
skip the deploy guard even for "tiny" fixes.

## Steps

1. **Verify the deploy guard passes.** The deploy check owns the go/no-go
   decision -- run [deploy-check](../deploy-check/SKILL.md) and act on its
   verdict before touching anything shared.
2. **Publish notes.** Every release gets notes written from the merged
   changes; see [notes](../notes/SKILL.md) for the format and template.
3. **Follow the runbook for rollout ordering and rollback.** The
   [runbook](runbook.md) lists the exact sequence and the one-line rollback.

After the release, keep the notes skill's archive tidy -- it is the record
on-call reads first when something regresses.
