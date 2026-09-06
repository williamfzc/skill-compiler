# skill-compiler

A skill compiler. It compiles every skill this machine's agents load, plus their
reference relationships, into a deterministic state (one graph) and checks
correctness.

This is the solid base. Optimization actions -- folding, slimming, dedup -- are
built on top of this state; they come later. Get the ground right first.

For who it is built for and how far is far enough, see the
[user stories](docs/user-stories.md); below covers **what** the compiler checks.

## Why the rewrite

The previous version rested on a **wrong premise**: it assumed the loader "scans
one level only", that a parent dir with a `SKILL.md` truncates its subdirs, and
built an `indexed / routed / unreachable` reachability model on top.

That does not hold in practice. `metis-case-writer` has its own `SKILL.md`, yet
the 9 `SKILL.md` files under its `roles/` are still each loaded as independent
skills. **The loader is recursive**: every `SKILL.md` at any depth under a root
is a loadable skill. Once the premise is wrong, the whole model is meaningless.

So it was rebuilt from scratch. The compiler does not guess "which skill should
be discovered"; it observes four deterministic facts.

## What the compiler checks

What actually decides whether a skill can be used correctly is four kinds of
deterministic correctness:

| Check | Codes | Level |
|---|---|---|
| Loadable: is frontmatter / description present | `NO_FRONTMATTER` `NO_DESCRIPTION` | error |
| Name unique: same name in one root, the loader hits only one | `NAME_COLLISION` | error |
| Reference intact: does the file a `SKILL.md` points to exist | `BROKEN_REF` | error |
| Symlink alive: if the skill dir is a symlink, does the target exist | `DANGLING_SYMLINK` | error |
| name field mismatches dir name / description too long | `NAME_MISMATCH` `LONG_DESCRIPTION` | warn |

Observe and check correctness only; it does not judge "how to organize".

## Compiled output (the state graph)

`build` emits a JSON that is the single source of truth for every later tool.
Its full field contract, with `jq` recipes, is in
[docs/state-contract.md](docs/state-contract.md):

```
roots            discovered load roots (agent dirs + plugin cache, newest version only)
skills           nodes: one per SKILL.md, deduped by realpath, provenance keeps every mount
edges            edges: ref (doc references) / contains (directory nesting)
broken_refs      references that do not resolve
name_collisions  same name within one root
identities       multi_mounted (one thing mounted in many places) / same_content (identical independent copies)
diagnostics      correctness diagnostics (error / warn)
```

## Usage

```bash
go build -o skillc ./cmd/skillc      # or: go install ./cmd/skillc

./skillc roots                                # list discovered load roots only
./skillc build --out state.json               # compile into the state graph
./skillc check                                # compile + diagnostics (exit 1 on error)
./skillc check --json                         # machine readable
./skillc query --skill <name>                 # all relationships of one skill
./skillc diff --before a.json --after b.json  # compare, flag regressions
./skillc viz --out graph.html                 # interactive network view, open in a browser
./skillc viz --format mermaid                 # or emit Mermaid / DOT text
```

For an agent picking up skillc for the first time: `./skillc --skill` prints
skillc's own skill page -- a SKILL.md-style document (name, description, when
to use it) followed by situation-based recipes: "did my edit break loading",
"did this round make the tree worse", "what does this machine load and how
healthy is it", "where is a skill and what depends on it", and the fold-safety
check, each with copy-paste commands. `./skillc --help` is the compact product
reference and routes there. Looking a concrete skill up (mounts, references,
blast radius) is `./skillc query --skill <name>`.

It compiles to a single static binary: copy it to any machine (same
GOOS/GOARCH) and run it, no interpreter or package install needed.

The command set stays small on purpose: `build` / `check` / `diff` do the parts
an agent cannot trivially reproduce (scan the disk, rank diagnostics, subtract
two snapshots correctly). Descriptive questions -- find a skill, show its blast
radius, print a health line -- are one `jq` over `state.json`, so they live as
recipes in [docs/state-contract.md](docs/state-contract.md) rather than as
frozen flags. (`query` is kept as a convenience for the common single-skill
lookup.)

Common flags:

- `--extra-root <dir>` append one load root (repeatable)
- `--only-root <dir>` compile only these roots, skip discovery (for tests and targeted checks)

`check` exits 1 when errors are present, so it drops straight into CI or
pre-commit.

## Where load roots come from

The compiler reflects the dirs the agent **actually loads**, not every skill on
the filesystem:

- **agent dirs**: `~/.trae/skills`, `~/.agents/skills`, `~/.claude/skills`,
  `~/.zcode/skills`, etc. -- the list mirrors the community-maintained agent
  table in [vercel-labs/skills](https://github.com/vercel-labs/skills) (pinned
  revision), with env-relocated homes (`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, ...)
  honored too. Provenance and divergences:
  [docs/load-roots.md](docs/load-roots.md).
- **plugin cache**: `~/.trae/plugins/cache`, `~/.claude/plugins/cache`,
  `~/.zcode/cli/plugins/cache`. A plugin
  often has dozens of historical versions coexisting; the agent loads only the
  newest -- the compiler **keeps only the version dir with the newest mtime**,
  otherwise a pile of unloadable zombie versions would appear out of nowhere.
- deduped by realpath; when one batch of skills is symlinked into several roots
  it merges into one node, and `provenance` records every arrival path.

Skills inside project repos are not scanned -- those do not occupy the agent's
resident context; that is part of "later".

## Judging whether a change broke things

A single `check` cannot tell a newly introduced problem from a pre-existing one.
To judge regressions, compare two states:

```bash
./skillc build --out before.json
# ... make changes ...
./skillc build --out after.json
./skillc diff --before before.json --after after.json
```

`diff` does set subtraction and **does not guess**: only what `after` has and
`before` lacks is a regression. Flags: `NEW_BROKEN_REF`, `BECAME_DANGLING`,
`SKILL_DISAPPEARED`, `NEW_NAME_COLLISION`. Problems present in both go to
"notes". Exit code: 1 on any regression, 0 otherwise.

Both files are its own `build --out` output, so `diff` first checks their
`schema` fields match; snapshots from two different skillc versions cannot be
compared and it refuses with exit code 2 rather than silently miscomputing.

## Reference-resolution trade-offs

Extraction is AST-based (goldmark, the CommonMark standard), so fenced code
blocks and inline code are excluded by the parser, not by regex guesswork.
Two tiers of references come out of that:

- **Markdown links and bare relative paths in prose** are references; an
  unresolvable one is a broken ref -- an error when written in `SKILL.md`, a
  warn when written in any other doc of the skill (the same call rustdoc makes
  with its broken-intra-doc-links lint). Only errors fail `check`.
- **A path in inline code counts when it resolves** (rustdoc treats backticks
  as links too): the agent really will read `qa/look-mechanics.md`. An
  unresolvable one stays silent -- an example filename like `TODO.md` is not a
  fault. Resolution order: file-relative first, then skill-root-relative,
  then repo-root-relative. A ref to an existing file outside the skill root
  is recorded as `external_refs`: still checked for existence, but not
  counted as broken.

Every markdown file inside a skill is also compiled as a graph node with
per-file out-edges (`files` / `file_edges` in the state), so doc-to-doc links
inside one skill are visible -- see
[docs/ref-graph.md](docs/ref-graph.md) for the design and
[docs/state-contract.md](docs/state-contract.md) for recipes (orphans,
blast radius). `viz` renders broken refs the same way in every format
(HTML, Mermaid, DOT): a red dashed edge to a ghost endpoint labeled with the
text as written, so a dead link is visible on the graph, not just as a count.

## Design principle

**Judgment belongs to the agent, execution belongs to scripts.**

The compiler contains no skill name, domain word, or classification table. It
only discovers roots, extracts references, hashes content, checks correctness,
and compares states. Which skills belong in a group and how to index them is the
consumer's on-the-spot judgment on this state -- guessing category by name prefix
breaks on the next machine.

## Layout

The engine is a small layered package; each module carries one thesis and
dependencies point one direction (foundation -> analysis -> outside):

```
cmd/skillc/main.go        thin entry point (forwards to internal/cli)
internal/
  paths/        foundation: path expansion, reads, pruning, symlink-aware walk
  frontmatter/  parse SKILL.md frontmatter
  refparse/     goldmark-based reference extraction from markdown
  roots/        discover load roots (agent dirs + newest plugin version)
  collect/      walk roots into nodes, dedup by realpath, owner index
  refgraph/     file-level reference graph (files, file_edges, broken/external)
  edges/        node-level projection: ref edges between skills + nesting
  analysis/     refs-in counts, identity groups, name collisions
  diagnostics/  turn facts into severity-ranked diagnostics
  state/        compile everything into one state graph + summary
  cli/          argument parsing and human/JSON rendering
```

## Development

```bash
./scripts/check.sh      # self-check: build + vet + tests + self-compile
go test ./...           # unit (synthetic trees) + user-story (fake $HOME) tests
go build ./...          # just compile
```

## License

Apache License 2.0

The vendored test corpus under `testdata/skills-sh/` retains its upstream MIT
licenses (see each vendored `LICENSE` and `testdata/skills-sh/SOURCES.md`).
