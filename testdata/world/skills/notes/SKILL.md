---
name: notes
description: Write and archive release notes from merged changes using the team template.
---

# Release notes

Turn merged changes into notes an on-call engineer can act on at 3am.

## How to write

1. Start from the [minutes template](templates/minutes.md) so every release
   has the same shape: summary, changes, risks, rollback.
2. Lead with user-visible impact; internal refactors get one line.
3. Name the rollback for anything touching data or shared infrastructure.

Notes are short on purpose. The runbook and deploy guard hold the detail;
notes only point at them.
