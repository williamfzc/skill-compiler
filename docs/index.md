---
type: Index
title: skillscope docs
description: Repo-specific design docs for skillscope - who it serves, what it refuses, and the decisions the compiler rests on.
tags: [index, navigation]
updated: 2026-09-02
---

# skillscope docs

Design docs for skillscope. How the code is written lives in `../cmd/skillc/` and
`../internal/`; this tree records **why** it is written that way -- the facts,
boundaries, and trade-offs settled up front. This is the ground the compiler
grows on.

One concept per file, path is identity, one `index.md` per directory. Only
decisions with lasting value go here; transient actions do not enter the tree.

## Documents

| Document | Contents |
|---|---|
| [User stories](user-stories.md) | Who uses skillscope, what it solves, what it deliberately refuses |
| [Load roots](load-roots.md) | Where the scanned-directory list comes from (upstream mirror, divergences, sync policy) |
| [Reference graph](ref-graph.md) | File-level graph design: extraction semantics, severity split, prior-art decisions |
| [State graph contract](state-contract.md) | Every field of `state.json`, plus `jq` recipes so an agent does find / explain / health itself |

## Where to start

- To learn who the tool is built for -> [User stories](user-stories.md)
- To learn what the compiler checks and what it emits -> [../README.md](../README.md)
- To consume the state graph (as an agent or tool) -> [State graph contract](state-contract.md)
