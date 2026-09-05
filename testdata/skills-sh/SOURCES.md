---
type: TestData
title: skills.sh vendored corpus sources
description: Provenance, licenses, and pinned revisions for the real-world skill fixtures used by catalogue_test.go.
resource: https://skills.sh
tags: [testdata, fixtures, provenance]
---

# Vendored skills.sh corpus

`catalogue_test.go` characterizes the compiler against real, well-known skills
from the skills.sh registry. Files here are copied verbatim from upstream
(only the skill directories and licenses; repo-level scaffolding is dropped).
Do not edit the vendored files by hand -- refresh them from upstream and update
the pinned facts in `catalogue_test.go` in the same change.

## Contents

| Dir | Upstream repo | Pinned commit | License | Notes |
|---|---|---|---|---|
| `obra-superpowers/` | https://github.com/obra/superpowers | `b36e0829c6d0` | MIT (see `LICENSE`) | `skills/` only: 14 flat skills from the "superpowers" family |
| `vercel-labs/` | https://github.com/vercel-labs/skills | `435076e78988` | MIT (see `LICENSE`) | `skills/find-skills` only: the #1 all-time skills.sh skill (~3.2M installs) |

## What the corpus exercises

- A real **flat skill suite** with sibling `skills/<a>/SKILL.md` layouts and
  cross-skill markdown links (superpowers): ref edges, blast-radius counts,
  frontmatter quality across 14 shipped skills.
- A suite whose `writing-skills` skill references the Anthropic
  document-creation bundles (`REFERENCE.md`, `DOCX-JS.md`, ...) **inside code
  samples** (fenced ````-blocks that demonstrate a generated SKILL.md), not in
  prose. Those links resolve nowhere on disk, but code is example content: the
  compiler reads references only from real prose, so they correctly produce
  **no** broken refs. This corpus pins that code-sample paths are never
  reported as faults.
- A real **clean single skill** (`find-skills`) with no faults at all.

## Refresh procedure

1. Download the pinned tarball:
   `curl -sL https://codeload.github.com/<owner>/<repo>/tar.gz/<sha> -o /tmp/x.tgz`
2. Copy the skill directory tree and the `LICENSE` into the matching dir above.
3. Run `go test ./... -run TestCatalogue -v`; update the pinned counts in
   `catalogue_test.go` only where the upstream change deliberately moved the
   facts (new skill, fixed/removed reference), with the upstream commit noted
   in the test change.
