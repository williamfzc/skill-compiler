# skillscope user stories

Settle "who uses it, why, and what it deliberately won't do" first, then let the
compiler grow on top. This document is the source of truth for requirements;
`README.md` covers **what** the compiler checks, this covers **for whom** and
**how far is far enough**.

## Roles

- **Skill author**: writes/edits a single skill and, after editing, wants to know
  whether they broke it.
- **Environment maintainer**: manages the hundreds of skills one machine loads
  (many agents, many plugins, symlinks everywhere) and wants to know whether the
  whole tree is healthy right now.
- **Consumer tool**: the optimization actions -- folding, slimming, dedup,
  reindexing. They never rescan the disk; they eat this state graph. This layer
  is deferred, but the base must leave a clean interface for it.

## Core stories

### S1 -- turn "what this machine loads" into deterministic fact

> As an environment maintainer, I want the tool to **discover every skill the
> agent actually loads** and compile it into one graph, so any discussion rests
> on the same facts instead of everyone running their own `find` and disagreeing.

Why it matters: on one machine, skills are scattered across `~/.trae/skills`,
`~/.agents/skills`, dozens of historical versions in the plugin cache, and behind
layers of symlinks. "Which skills exist" -- the most basic question -- cannot be
answered by eye. Without this deterministic state, every later judgment is a
tower built on sand.

Acceptance:
- One `build` command emits a JSON state graph; the same machine state compiles
  to the same result twice.
- It reflects the set the agent **actually loads**: the plugin cache keeps only
  the version dir with the newest mtime, so unloadable zombie versions are not
  counted.
- Symlinks / multiple mounts dedup by realpath into one node; `provenance`
  records every arrival path -- the same thing is not double counted, yet each
  mount path stays discoverable.

### S2 -- edit a skill, immediately know whether you broke it

> As a skill author, after editing `SKILL.md` I want one command to tell me
> whether it **can still be loaded and triggered correctly**, instead of finding
> out only when some agent silently skips my skill at runtime.

Why it matters: skill failures are mostly **silent** -- a missing description
raises no error, it just never triggers; a broken reference just makes the agent
degrade quietly. These must be caught before commit.

Acceptance:
- `check` reports four kinds of load-time correctness, each with a diagnostic
  code, location, and human-readable reason: `NO_FRONTMATTER` / `NO_DESCRIPTION`
  / `BROKEN_REF` / `DANGLING_SYMLINK` (error), and `NAME_MISMATCH` /
  `LONG_DESCRIPTION` etc. (warn).
- Exit code 1 when any error is present, so it drops straight into CI /
  pre-commit.
- Reference resolution fits real-world writing: when a file-relative path does
  not resolve, retry skill-root-relative then repo-root-relative; a backtick
  path counts when it resolves and stays silent when it does not.
- **Severity follows the writing file**: a broken link in `SKILL.md` is an
  error; the same fault in a reference doc is a warn -- editing a reference
  doc cannot turn the whole gate red over a stale link.
- **The file-level graph tells the author what their docs are worth**: every
  markdown file inside a skill carries `in_count` / `out_count` in `files`,
  so orphans (unreferenced, unreferencing docs) and the blast radius of a
  rename are one `jq` away (recipes R7-R9 in the state contract).

### S3 -- judge whether this change made the tree worse

> As an environment maintainer, after a round of cleanup / upgrades I want to know
> **whether I broke it or it was already broken** -- a single `check` full of
> historical debt cannot tell them apart.

Why it matters: a tree that already carries dozens of broken refs cannot, from a
single scan, separate "new debt" from "old debt". The precondition for safe
cleanup is separating regressions from pre-existing state.

Acceptance:
- `diff --before --after` does set subtraction and counts **only what `after` has
  and `before` lacks** as a regression: `NEW_BROKEN_REF` / `BECAME_DANGLING` /
  `SKILL_DISAPPEARED` / `NEW_NAME_COLLISION`.
- Problems present in both go into "notes", not falsely reported as newly
  introduced.

### S4 -- leave a clean consumer entry point for the optimization layer

> As a consumer tool, I want to read this state graph directly and get nodes,
> edges, identity relations, and diagnostics, **without rescanning the disk
> myself**, and without stuffing "how to organize" rules into the compiler.

Why it matters: judgment belongs to the agent, execution belongs to scripts. The
moment a skill name, domain word, or classification table appears in the
compiler, it breaks on the next machine. The compiler only emits deterministic
facts; "which group, how to index" is the consumer's on-the-spot judgment built
on those facts.

Acceptance:
- The state graph contains `skills` / `edges` (ref, contains) / `broken_refs` /
  `external_refs` / `identities` (multi_mounted, same_content) /
  `name_collisions` / `diagnostics`.
- The compiler source contains no skill name, domain word, or classification
  rule.

## Deliberately out of scope (for now)

Lay the base first; these are built on top of it and come later:

- **Folding / slimming / dedup / reindexing** -- any "how to organize" action.
  The compiler only observes, it does not organize.
- **Scanning skills inside project repos** -- those do not occupy the agent's
  resident context; scan only the agent load roots.
- **Rewriting any skill file** -- read only, judge only, report only; never write
  into the user's skills.
- **Guessing category / grouping by name prefix** -- breaks on the next machine;
  it is the consumer's on-the-spot judgment, not part of the compiler.

## Boundary in one sentence

skillscope is a **read-only skill compiler and health checker**: it compiles the
skills this machine loads and their references into a deterministic state graph,
judges load-time correctness, and can judge regressions between two states. It
does not organize, rewrite, or guess categories -- all of that is built on the
facts it emits.
