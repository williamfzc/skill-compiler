# Simulated skill world (`world_test.go`)

A small, hand-written multi-agent world for end-to-end scenarios. The markdown
is guidance an agent would actually follow; the compiler turns that guidance
into edges, and the tests pin what a consumer must be able to prove and do.

## Canonical root: `skills/`

| Skill | Role | Guidance it gives an agent |
|---|---|---|
| `release` | orchestrator | run the deploy guard first, publish notes, follow the runbook -> links `deploy-check`, `notes`, `runbook.md` |
| `deploy-check` | the go/no-go guard | strict pre-release verification; referenced by `release` |
| `notes` | release notes hub | templates for changelogs; referenced by release, workflows, and the nested plan (**refs_in = 3**, the load-bearing hub) |
| `workflows` | suite root | delegates planning to its nested skill and releasing to `release` |
| `workflows/sub` (declares `name: plan`) | nested skill | plans before coding; links back to `../../notes` |
| `risky` | deliberately broken draft | points at `postmortem.md` and `safety/checklist.md` that do not exist (the 2 errors `check` must surface), and at `tickets/ticket-escalate/SKILL.md` (a real cross-boundary reference); also gives the `NAME_MISMATCH` warn (dir `sub`, name `plan`) |
| `tickets/ticket-*` (8 flat skills) | the flat-set clutter | eight small ticket-lifecycle skills (triage/assign/escalate/close/refund/merge/prioritize/tag), each advertised at the root level on load. `ticket-escalate` is the one with an external inbound reference (from `risky`). |

## The fold problem this models

Eight small skills sitting flat at the root each pay resident-context cost to
advertise themselves; they are one cohesive workflow. The fold consumer groups
them by adding a `tickets/SKILL.md` parent (loading the suite turns one parent
entry, not eight) and `check` verifies the move: `ticket-escalate`'s external
inbound edge must keep resolving. Moving the set without updating callers is
caught as a new broken reference. Baseline: 14 skills, 13 top-level entries
(only `sub` is nested); after the fold: 15 skills, **6** top-level entries.

## Other roots

- `agents/agent-1/skills/release/` — an independent byte-copy of release
  (fold/dedup scenario: same_content group across roots).
- `agents/agent-2/skills/workflows/` — an independent copy of the suite with
  its nested plan (same_content groups for `workflows` and `plan`).

Scenarios covered in `world_test.go`: observed baseline facts, error
discoverability (state JSON + `check --json` exit code), the cross-root copy
story (same_content groups, removable by pointing consumers at the canonical
root), the flat-set fold story (8 advertisements collapse under one parent,
external references stay intact, unsafe moves are caught), and the regression
story (a new broken reference is caught by `diff`, pre-existing draft debt
stays in notes, repairing the reference returns to baseline).
