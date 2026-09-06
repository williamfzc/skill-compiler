---
type: Reference
title: "Load roots: where the list of scanned directories comes from"
description: "Provenance and sync policy for the agent-dir table in internal/roots, mirroring vercel-labs/skills, plus the deliberate divergences."
tags: [roots, discovery, provenance, upstream]
timestamp: 2026-09-05
---

# Load roots

`internal/roots/roots.go` owns the fact "which directories an agent loads
skills from". That fact is a maintained table, not something the compiler can
derive: it reflects each agent loader's conventions, which change upstream.

## The loader is recursive

The premise discovery rests on: an agent loader does not stop at the first
`SKILL.md` -- every `SKILL.md` at any depth under a root is independently
loadable (one skill directory may carry a `roles/` tree whose members are
skills of their own). The compiler therefore scans roots recursively and
records nesting as `contains` / `contained_by` edges instead of using it to
prune. A reachability model built on any other premise describes a loader
that does not exist.

## Upstream source of truth

The agent directory list **mirrors** [`vercel-labs/skills`](https://github.com/vercel-labs/skills)
(MIT), `src/agents.ts` @ `435076e` (2026-08-18, v1.5.23). Upstream keeps the
per-agent table current with community PRs and a validation script; hand-maintaining
a second table here would drift silently, so this repo treats upstream as the
source and syncs against a pinned revision.

Sync policy: when the tool under-reports on a machine, check whether the missing
agent is already upstream; if so, port its `globalSkillsDir` entries and bump the
pinned revision in the comment on `AgentRootCandidates`.

## Deliberate divergences from upstream

- **Plugin caches** (`~/.trae/plugins/cache`, `~/.claude/plugins/cache`,
  `~/.zcode/cli/plugins/cache`) are not an upstream concept -- upstream only
  installs skills, it does not observe what a machine loads. Newest-version
  selection and the recursive `skills/` detection are this repo's addition.
- **`~/.trae/skills/.system` and `~/.aipaas/skills`** were observed on real
  machines but are absent upstream.
- **Relocated homes** (`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `VIBE_HOME`,
  `HERMES_HOME`, `AUTOHAND_HOME`, `GROK_HOME`): when the env var is set and
  non-blank, the loader uses that home, so `envAgentHomes` adds its
  `<home>/skills` as an extra candidate. Defaults stay in the static list and
  realpath dedup collapses the overlap.
- **XDG configHome** is pinned to `~/.config` here (upstream honors
  `$XDG_CONFIG_HOME`). Revisit if a machine actually relocates it.

## Why not a dependency

Upstream is TypeScript and skillc ships as a single static Go binary; only the
data (the path table) is wanted, and data is ported, not imported. The pinned
revision in the code comment is the link that keeps the mirror honest.
