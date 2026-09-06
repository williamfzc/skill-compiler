---
type: Decision
title: "The file-level reference graph and its extraction semantics"
description: "Why extraction is goldmark-based, why quoted paths count when they resolve, why severity is split by writing file, and what was deliberately not adopted from prior art."
tags: [refgraph, refparse, extraction, prior-art, decisions]
timestamp: 2026-09-06
---

# The file-level reference graph

`skillc build` compiles two projections of one extraction pass:

- **file-level** (`internal/refgraph`): every markdown file inside a skill is
  a node (`files`) with `in_count`/`out_count`; every reference it writes is a
  `file_edges` row -- including doc-to-doc links inside one skill, which carry
  no node-level relationship but do carry documentation weight.
- **skill-node-level** (`internal/edges`): the projection consumers query for
  relationships between skills (`edges`, `refs_in_count`).

Layering: `refparse` (pure parsing) -> `refgraph` (filesystem pass, broken /
external facts) -> `edges` (node projection) -> `analysis` -> `state`.

## Extraction semantics

- **Parsing is AST-based** ([goldmark](https://github.com/yuin/goldmark), the
  CommonMark standard for Go). Code exclusion belongs to the parser, not to
  regex fence-tracking -- the lesson of
  [lychee](https://github.com/lycheeverse/lychee), whose pulldown-cmark core
  never treats code as links. The previous hand-rolled fence-depth machinery
  needed four dedicated tests to pin its corners; the parser removes the
  class of bug. Frontmatter is stripped before parsing (it is structured
  data, not prose).
- **Two-tier backtick rule**: a path-shaped inline-code span counts as a
  reference when it resolves, and stays silent when it does not. Rustdoc is
  the precedent: [intra-doc links](https://doc.rust-lang.org/rustdoc/write-documentation/linking-to-items-by-name.html)
  are written in backticks. On real corpora the resolvable tier is genuine
  (`qa/look-mechanics.md`, `root-cause-tracing.md`) and the unresolvable tier
  is examples (`TODO.md`, `analyze_form.py`) -- resolution itself is the
  filter. Plain prose keeps the stricter rule (unresolved = broken ref).
- **Severity split by writing file**: a broken link in `SKILL.md` is an
  error, one in any other doc is a warn -- rustdoc reports broken
  intra-doc links as a warning lint, and an author editing a reference doc
  should not turn the whole gate red over a stale link. Only errors fail
  `check`; `diff` reports regressions regardless of severity.
- **Scope order** (rustdoc's scope-based resolution, filesystem edition):
  file-relative, then skill-root-relative, then repo-root-relative.
  References resolving outside any skill are facts (`external_refs`), not
  faults -- matching rustdoc's silence on cross-crate failures.

## Vocabulary

Adopted from [Foam](https://github.com/foambubble/foam): **orphans** (files
with no inbound and no outbound links) and **placeholders** (dangling links,
our file-level broken refs). Foam's "a dangling link becomes a ghost node"
was deliberately **not** adopted: we list facts, we do not invent nodes.

## Rendering

`skillc viz` (`internal/viz`) draws the graph for humans in three formats --
HTML canvas, Mermaid, Graphviz DOT -- and draws broken refs the same way in
all of them: a red dashed edge from the writing file to a **ghost endpoint**
labeled with the text as written. The ghost is the *drawing* of a
`broken_refs` row, not a node in the state: the Foam decision above still
holds for the data model, and the renderers add nothing the compiler did not
observe. The HTML overview rings any skill whose files write broken refs and
carries per-skill counts in the legend; the per-skill detail view places the
ghosts next to their writing files.

## Deliberately not adopted

- **Suppression comments** (`<!-- lychee-disable-next -->` style escape
  hatches): nothing on real machines has needed one; the two-tier rule plus
  severity split already keep signal high. Revisit only when a real corpus
  proves a false positive that no rule can express.
- **Manifests** (mdBook's `SUMMARY.md`): skillc infers the graph instead of
  requiring authors to declare it. A compiler that needs a manifest to be
  correct is a compiler that is wrong the day the manifest drifts.

## Competitive landscape

[agnix](https://github.com/agent-sh/agnix) lints agent config files
(CLAUDE.md, SKILL.md, hooks, MCP configs) against ~450 per-file rules with
auto-fix -- it validates *how a file is written*; skillc compiles *what a
machine loads and how the pieces relate* (recursive discovery, plugin-cache
newest-version selection, reference graph, snapshot diff). The two are
complementary layers, not competitors. Smaller tools (`cclint`,
`skill-lint`, `lintai`) are security scanners over individual files; none
compile a machine-level state graph.
