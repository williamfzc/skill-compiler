# skill-compiler

[![ci](https://github.com/williamfzc/skill-compiler/actions/workflows/ci.yml/badge.svg)](
https://github.com/williamfzc/skill-compiler/actions/workflows/ci.yml)

A skill compiler. It compiles every skill this machine's agents load, plus their
reference relationships, into a deterministic state (one graph) and checks
correctness.

## What it is for

Compiling is a means. The point is what the one state graph lets you answer:

- **I edited a skill -- did I break it?** Skill failures are silent: a missing
  description never triggers, a broken reference just degrades. `check` catches
  them before commit and drops into CI / pre-commit with exit code 1.

  ```bash
  ./skillc check
  ```

- **Did this round of cleanup make things worse, or was it already broken?**
  A bare check cannot tell new debt from old. `diff` flags only what the newer
  state has and the older lacks -- never the reverse.

  ```bash
  ./skillc diff --before before.json --after after.json
  ```

- **Can I move, fold, or delete this skill safely?** One command shows every
  mount of a skill, what references it, and what sits inside it, so the blast
  radius is read off the graph instead of guessed.

  ```bash
  ./skillc query --skill <name>
  ```

- **What does this machine actually load, and at what context cost?** Agent
  dirs, plugin caches with dozens of zombie versions, layers of symlinks --
  compiled into one deterministic tree. The build prints a health line; the
  resident-description cost per turn is one field away.

  ```bash
  ./skillc build --out state.json
  jq .summary.resident_desc_chars state.json
  ```

- **Every agent installed the same skill into its own directory?** One thing
  symlinked into several roots folds back into one node with every arrival
  path recorded (`identities.multi_mounted`); independent installs with
  byte-identical content are grouped as fold candidates (`same_content`);
  same-name skills that a loader would silently resolve to one of them are
  flagged (`name_collisions`).

  ```bash
  ./skillc build --out state.json
  jq '.identities.same_content, .identities.multi_mounted, .name_collisions' state.json
  ```

  Pretty-printed recipes: [docs/state-contract.md](docs/state-contract.md).

- **What does the web between skills look like?** `viz` renders the reference
  network -- cross-skill references, doc-level links, broken references as red
  dashed edges -- for humans, not for `jq`.

  ```bash
  ./skillc viz --out graph.html
  ```

The full scenarios and the reasoning behind each boundary live in
[docs/user-stories.md](docs/user-stories.md).

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

## Install

One key (macOS / Linux; installs into `~/.local/bin`):

```bash
curl -fsSL https://github.com/williamfzc/skill-compiler/releases/latest/download/install.sh | bash
```

The script downloads the release tarball for your platform, verifies it against
the release's checksums, and drops the single `skillc` binary into
`~/.local/bin`. Override the destination with `SKILLC_INSTALL_DIR`, or pin a
version with `SKILLC_VERSION=v0.1.0`. Review before running: the same script
lives at [scripts/install.sh](scripts/install.sh) in this repo.

From source instead (any platform Go builds for):

```bash
go install github.com/williamfzc/skill-compiler/cmd/skillc@latest
```

## Usage

```bash
./skillc roots                                # list discovered load roots only
./skillc build --out state.json               # compile into the state graph
./skillc check                                # compile + diagnostics (exit 1 on error)
./skillc check --json                         # machine readable
./skillc query --skill <name>                 # all relationships of one skill
./skillc diff --before a.json --after b.json  # compare, flag regressions
./skillc viz --out graph.html                 # interactive network view, open in a browser
```

`./skillc --skill` prints skillc's own skill page -- situation-based recipes
("did my edit break loading", "did this round make the tree worse", "where is
a skill and what depends on it", ...) with copy-paste commands; `--help` is
the compact reference and routes there. `check` exits 1 when errors are
present, so it drops straight into CI or pre-commit.

Common flags:

- `--extra-root <dir>` append one load root (repeatable)
- `--only-root <dir>` compile only these roots, skip discovery (for tests and targeted checks)

## Where load roots come from

The compiler reflects the dirs the agent **actually loads**: agent skill dirs
(the list mirrors the community-maintained table in
[vercel-labs/skills](https://github.com/vercel-labs/skills), env-relocated
homes honored) and plugin caches -- where only the newest version dir
survives, so unloadable zombie versions never appear. Discovery is recursive,
everything dedups by realpath, and `provenance` records every arrival path.
The premise, the full divergence list, and the sync policy:
[docs/load-roots.md](docs/load-roots.md).

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
`schema` fields match and refuses with exit code 2 rather than silently
miscomputing across skillc versions.

## How references are resolved

Extraction is AST-based (goldmark, the CommonMark standard), so fenced code
and inline code are excluded by the parser, not regex guesswork. Markdown
links and bare relative paths in prose are references; an unresolvable one is
a broken ref -- an error when written in `SKILL.md`, a warn in any other doc
of the skill. A path in backticks counts when it resolves and stays silent
when it does not -- an example filename is not a fault. Resolution tries
file-relative, then skill-root-relative, then repo-root-relative; a ref to an
existing file outside any skill is `external_refs`: checked for existence,
not counted as broken. The full semantics, severity reasoning, and prior art:
[docs/ref-graph.md](docs/ref-graph.md).

Every markdown file inside a skill is also a graph node with per-file
out-edges (`files` / `file_edges`), so doc-to-doc links inside one skill are
visible -- orphans and rename blast radius are one `jq` away
([docs/state-contract.md](docs/state-contract.md)).

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
