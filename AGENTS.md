# AGENTS.md

Before you write a single line in my repository, read this.
It is not a style guide. It is the shape of mind I want you to wear while you work here.
Everything here is in English, because the work is in English.

---

## Before you act

Add nothing that is not needed now.
The future will make its own demands; it does not need your sympathy.
Before creating a file, a layer, a flag, or an abstraction, ask whether something
that already exists can carry the weight. If it can, let it.

Before adding or moving code, find the existing boundaries.
Name the module you are changing, the layer it belongs to, and the responsibility
it owns. If you cannot name those three things, you are not ready to change it.

Before writing any fact, search for it.
A fact — an interface, a default, a decision, a responsibility — lives in exactly
one place. Everything else links to it. Two notes that disagree are not two
opinions; they are one bug. When you cannot reconcile them in the moment, name
the conflict openly. Silence is how contradictions survive.

---

## While you write

Write so the codebase reads like a well-indexed book.
Every source file opens with a short header: what it is, and where it stands in
the whole. A file whose role cannot be stated in two lines does not yet know
what it is.

Keep modules small enough to have a thesis.
A module exports a capability, not a drawer of helpers. Its public surface should
be narrower than its implementation, and its name should explain why it exists.
When two reasons to change appear in one module, split them before the module
turns into territory.

Dependencies move in one direction.
Higher layers may depend on lower layers; lower layers do not reach upward.
Peers do not reach sideways through private rooms. If two peers need to share
something, move the shared truth down into a lower layer with a clear owner.

Draw the layers before crossing them.
The outside layer speaks to users, networks, files, processes, and tools.
The application layer coordinates use cases and policy.
The domain layer holds the language, invariants, and decisions of the product.
The foundation layer holds small, stable utilities with no product opinion.
Each layer supports the one above it. None of them should secretly steer the one
below it.

When old structure is wrong but the task is narrower, contain the damage first.
Put a small boundary around the change, name the debt, and refactor only when
the work cannot be made honest without it.

Verify behavior at the boundary you changed.
Prefer tests that prove the contract between layers over tests that mirror the
implementation.

One language, one voice. Code, identifiers, comments, commits, docs — all English.

---

## When you finish

Leave the docs truer than you found them.
I keep a `docs/` tree alongside the code, maintained as an Open Knowledge Format
bundle: Markdown files with YAML frontmatter, nested by concern, linked with
ordinary Markdown links, mapped by per-directory `index.md` files, and, where
history matters, recorded in `log.md`.

Every durable concept gets one document.
Use the file path as the concept identity. Give each document frontmatter with
at least `type`, and add `title`, `description`, `resource`, `tags`, and
`timestamp` when they help the next reader or agent. The body carries the
knowledge; the frontmatter carries the small set of fields worth querying.

Do not document everything.
Document what has durable value and will be reused by a future reader, agent, or
maintainer: decisions, domain concepts, public contracts, operational runbooks,
architecture boundaries, and hard-won lessons. Leave temporary motion out of the
permanent graph.

Keep the graph navigable.
Indexes explain what lives under a directory and where to go next. Links point
to the canonical source of a concept instead of restating it. A document that
only repeats another document should usually be deleted or turned into an index.

Before you call the work done, check the shape you leave behind.
The changed module has a clear owner.
The dependency direction still runs one way.
The lasting knowledge is in `docs/` as OKF-shaped Markdown, with links to the
source of truth.

Refresh it whenever a unit of work closes — before the commit, after the commit,
at the end of the session. Drift is decay.

Let your commits read as narrative. I follow Angular Conventional Commits as
the grammar of that narrative — not for tooling, but for the discipline of
naming each move. A commit that cannot be summarized in one honest line is
probably two commits.

---

## What I refuse

Abstractions without a second caller.
Junk-drawer modules with no thesis.
Layer violations hidden behind convenience.
Two-way dependencies, even when both sides look small.
Parallel structures that never deprecate their predecessor.
Code that moves while its header or its doc stays behind.
The same truth stated twice instead of linked once.

These are not style preferences. They are failures of the principles above.
When you see them — mine or yours — name them.
Name nearby violations when they affect the work, hide a future cost, or make
the next change harder. Do not turn every task into a cleanup campaign.

---

## How I want you to work with me

Tell me when I violate these principles. Do not sink quietly into my mistakes.
When a live instruction of mine contradicts this text, follow the instruction,
but flag the contradiction so this text can grow.
When you are uncertain, prefer to do less. Do not fill uncertainty with motion.

---

When in doubt, choose the version with fewer things in it.
When certain, check once more.

---

## In this repository

The one language rule is not optional here: the Go sources, their tests, the
CLI output, `docs/`, and this file are all English.

- `cmd/skillc/main.go` — the entry point; the engine is `internal/`, one
  package per thesis: observe the facts about installed skills, never decide
  how to tidy them.
- `skillc_test.go` — unit behavior on synthetic trees via `--only-root`.
- `user_stories_test.go` — end-to-end acceptance of `docs/user-stories.md`,
  driven through a fake `$HOME` so the real root-discovery path runs.
- `catalogue_test.go` — characterization over real, well-known skills from the
  skills.sh registry, vendored verbatim under `testdata/skills-sh/` (MIT
  licensed; sources and pinned revisions in `testdata/skills-sh/SOURCES.md`).
- `world_test.go` — end-to-end scenarios over a hand-written simulated
  multi-agent world under `testdata/world/` (see its `README.md`): guidance
  edges, error discoverability, the fold story (same_content copies), and the
  before/after regression gate.
  `testdata` is pruned while scanning — fixture trees are test content, never
  skills an agent loads.
- `docs/` — repo design docs. Start at [`docs/index.md`](docs/index.md); the
  requirements live in [`docs/user-stories.md`](docs/user-stories.md).
- `scripts/check.sh` — the full self-check (build + vet + tests + self-compile).
  Run it before opening a change.

The compiler carries no skill names, domain words, or classification tables.
That boundary is enforced by a test, not trusted; see
`TestS4CompilerHasNoHardcodedSkillKnowledge` in `user_stories_test.go`.
