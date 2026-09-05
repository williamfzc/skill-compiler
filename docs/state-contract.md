# The state graph contract

`skillc build --out state.json` emits one JSON file. This document is the
contract for that file: what every field means, and copy-paste `jq` recipes that
let an agent do find / explain / health itself.

The design line: skillscope produces deterministic facts; the agent does the
judging. So there is deliberately **no** `search`, `explain`, or `status`
command -- each is one `jq` line over this file, and an agent runs `jq` more
flexibly than any flag set we could freeze. The commands that do exist
(`build` / `check` / `diff` / `roots`) are the parts an agent cannot trivially
reproduce: scanning the disk, ranking diagnostics, and set-subtracting two
snapshots correctly.

## Top level

| Field | Meaning |
|---|---|
| `schema` | State format version. `diff` refuses two files whose `schema` differs. |
| `generated_at` | ISO timestamp of the compile. |
| `scan_seconds` | Wall-clock compile time. |
| `roots` | The load roots that were scanned. |
| `skills` | The nodes, an **object keyed by realpath**. |
| `files` | The file-level graph nodes: every markdown file inside a skill, keyed by realpath. |
| `file_edges` | Per-file references: doc-to-doc and doc-to-skill links, including within one skill. |
| `edges` | `ref` and `contains` relationships (the skill-node projection of `file_edges`). |
| `broken_refs` | References that did not resolve (one row per physical file). |
| `external_refs` | References resolving to an existing file outside any skill. |
| `identities` | `multi_mounted` and `same_content` groupings. |
| `name_collisions` | Same name within one root (the loader can hit only one). |
| `diagnostics` | Correctness findings, pre-sorted by severity. |
| `summary` | Scalar counts for a one-line health read. |

## `skills` -- the nodes

`skills` is an object; each key is the skill directory's realpath, each value:

| Field | Meaning |
|---|---|
| `id` | Realpath of the skill dir (equals the key). |
| `name` | Directory basename. |
| `name_field` | The `name:` in frontmatter (may differ from `name`). |
| `description` | The `description:` frontmatter value. |
| `desc_chars` | Length of `description` (its resident-context cost). |
| `has_frontmatter` | Whether a frontmatter block was found. |
| `frontmatter_keys` | Sorted keys present in frontmatter. |
| `disable_model_invocation` | `disable-model-invocation: true` -> not auto-triggered. |
| `skill_md_bytes` | Size of `SKILL.md`. |
| `content_hash` | First 16 hex of the `SKILL.md` sha256 (identity of content). |
| `is_symlink` / `symlink_target` / `symlink_style` / `symlink_ok` | Symlink facts. |
| `broken_symlink` | Present and `true` only for dangling-symlink nodes. |
| `provenance` | Every mount: list of `{root, root_kind, rel}`. `len > 1` = multi-mounted. |
| `refs_in_count` | How many skills reference this one (its blast radius). |
| `refs_out` | Node ids this skill references. |
| `contains` | Node ids nested under this skill. |
| `contained_by` | Node ids this skill is nested under. |

`refs_out` / `contains` / `contained_by` hold **node ids** (realpaths); resolve
them back through `.skills[id].name` (recipe R2).

## `files` / `file_edges` -- the file-level graph

Every markdown file inside a skill is a node; every reference it writes is an
edge, including links between two docs of the same skill.

- **`files{}`**: keyed by realpath: `{id, owner (skill id), ext, bytes,
  out_count, in_count}`. An **orphan** is a node with `in_count == 0 &&
  out_count == 0`; a file other docs point at is load-bearing.
- **`file_edges[]`**: `{from, to, from_skill, to_skill, raw, quoted}`. `to` is
  the resolved target's realpath; `to_skill` is `""` when the target resolves
  outside any skill. `quoted: true` means the reference was written in inline
  code -- it counts only because it resolves.
- **Severity rule**: a broken link written in `SKILL.md` is an error; one
  written in any other doc of the skill is a warn (the same call rustdoc makes
  with `broken_intra_doc_links`). Only errors fail `check`.

## Other collections

- **`edges[]`**: `{kind: "ref"|"contains", from, to, ...}`. For `ref` also
  `from_file`, `raw` (the link as written), `target_is_skill_md`.
- **`broken_refs[]`**: `{from, from_file, raw, resolved, real_from}`. `from` is
  the owning skill's realpath, `real_from` the file that wrote the link. One row
  per physical file, so a skill copied into `.trae`/`.agents`/`.claude` yields
  three rows for one logical problem (recipe R3 collapses them).
- **`name_collisions[]`**: `{root, name, skills[], rels[]}`.
- **`identities.multi_mounted`**: `{node_id: [mount_path, ...]}` -- one realpath
  reached via several roots (a copy is really a symlink; safe).
- **`identities.same_content`**: `{"name@hash": [node_id, ...]}` -- distinct
  realpaths with identical content (edit one, the others drift; a real risk).
- **`diagnostics[]`**: `{severity, code, skill, where, detail}`, pre-sorted
  error-first. Codes: `NO_FRONTMATTER`, `NO_DESCRIPTION`, `BROKEN_REF`,
  `DANGLING_SYMLINK`, `NAME_COLLISION` (error); `NO_NAME`, `NAME_MISMATCH`,
  `LONG_DESCRIPTION` (warn).

## Recipes

All assume `S=state.json`.

### R1 -- find a skill by keyword (this replaces a `search` command)

```bash
jq -r '.skills[]
  | select((.name + " " + .description) | ascii_downcase | test("lark"))
  | "\(.name)\t\(.description)"' "$S"
```

### R2 -- explain one skill: blast radius + relationships (replaces `explain`)

```bash
jq -r '.skills as $s | $s[] | select(.name=="lark-doc")
  | "referenced_by: \(.refs_in_count)",
    "references:  " + ([.refs_out[]     | $s[.].name] | join(", ")),
    "contains:    " + ([.contains[]     | $s[.].name] | join(", ")),
    "contained_by:" + ([.contained_by[] | $s[.].name] | join(", ")),
    "mounted_at:  " + ([.provenance[] | "[\(.root_kind)] \(.rel)"] | join("; "))
' "$S"
```

Change one skill? `refs_in_count` is how many others feel it. The most-referenced
skills are the load-bearing ones:

```bash
jq -r '.skills[] | select(.refs_in_count>0) | "\(.refs_in_count)\t\(.name)"' "$S" \
  | sort -rn | head
```

### R3 -- real broken refs, cross-root copies collapsed

`broken_refs` has one row per physical file; collapse copies to the logical count
by keying on the path after `.../skills/` plus the raw target:

```bash
jq -r '[.broken_refs[] | {k:(.real_from|sub("^.*/skills/";"")), raw}]
  | unique | .[] | "\(.k)  ->  \(.raw)"' "$S"
```

### R4 -- content copies that will drift when you edit one

```bash
jq -r '.identities.same_content | to_entries[]
  | "\(.key):\n  " + (.value | join("\n  "))' "$S"
```

### R5 -- one-line health (replaces `status`)

```bash
jq -r '.summary
  | "skills=\(.skill_count) broken=\(.broken_ref_count) "
    + "collisions=\(.name_collision_count) dangling=\(.broken_symlink_count) "
    + "resident_desc=\(.resident_desc_chars)ch (~\(.resident_desc_chars/3|floor)tok)"' "$S"
```

### R6 -- errors only, grouped by code

```bash
jq -r '.diagnostics[] | select(.severity=="error") | "\(.code)\t\(.where)"' "$S" \
  | sort | uniq -c | sort -rn
```

### R7 -- orphan docs (Foam's orphans: no inbound, no outbound links)

```bash
jq -r '.files[] | select(.in_count==0 and .out_count==0)
  | "\(.owner|sub("^.*/skills/";""))  \(.id|sub("^.*/skills/";""))"' "$S"
```

### R8 -- what breaks if I move this file (per-file blast radius)

```bash
jq -r --arg f "references/index.md" '
  .file_edges[] | select(.from | endswith("/" + $f)) | "  -> \(.to)"
' "$S"
jq -r --arg f "references/index.md" '
  .file_edges[] | select(.to | endswith("/" + $f)) | "  <- \(.from)"
' "$S"
```

### R9 -- load-bearing docs (most-referenced files)

```bash
jq -r '.files[] | select(.in_count>0) | "\(.in_count)\t\(.id)"' "$S" | sort -rn | head
```

## Working with an agent

A useful loop: the agent runs `build` once, then answers your questions with
recipes over the file -- no rescanning between questions. For gating a change,
`check` (exit 1 on error) and `diff` (exit 1 on regression, exit 2 on schema
mismatch) are the two it should shell out to; everything descriptive is `jq`.
