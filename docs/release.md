---
type: Decision
title: "CI, releases, and the one-key installer"
description: "Why CI runs scripts/check.sh verbatim, why releases ship static tarballs with checksums and install.sh, why Windows is not built, and why the module path is the repo URL."
tags: [ci, release, distribution, install, decisions]
timestamp: 2026-09-06
---

# CI, releases, and the one-key installer

The repo ships through two GitHub Actions workflows under `.github/workflows/`.

## CI is the self-check, verbatim

[`ci.yml`](../.github/workflows/ci.yml) runs `scripts/check.sh` on every push
to main and every pull request. One grammar locally and in CI: the gate a
committer runs before a commit is the gate CI runs after, so the two cannot
drift. The check needs only Go and bash, and the catalogue fixtures are
vendored under `testdata/`, so no step touches the network.

## Releases are static tarballs + checksums + the installer

[`release.yml`](../.github/workflows/release.yml) triggers on `v*` tags and
publishes to GitHub Releases:

- cross-compiles `cmd/skillc` with `CGO_ENABLED=0 -trimpath -ldflags '-s -w'`
  for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64 -- the same single
  static binary the README promises, just built for four platforms;
- packs one `skillc_<os>_<arch>.tar.gz` per platform (binary + LICENSE +
  README) and emits `checksums.txt` over the tarballs;
- attaches everything to the release, plus `scripts/install.sh` under the
  stable asset name `install.sh`.

That last attachment is the whole trick behind the one-key install:
`https://github.com/williamfzc/skill-compiler/releases/latest/download/install.sh`
is a permanent URL, so no checkout is needed:

```bash
curl -fsSL https://github.com/williamfzc/skill-compiler/releases/latest/download/install.sh | bash
```

The installer ([`scripts/install.sh`](../scripts/install.sh), the canonical
copy) verifies the tarball against the release's own `checksums.txt` before
extracting -- a release that cannot prove its own integrity is not installed
-- and installs into `~/.local/bin` (`SKILLC_INSTALL_DIR` overrides,
`SKILLC_VERSION` pins a tag).

## Deliberately not built

- **Windows**: the compiler targets the POSIX-shaped homes of agent skill
  dirs ([load-roots.md](load-roots.md)), and its path logic assumes `/`.
  Shipping an unchecked platform would be forged success. Revisit when a
  real Windows agent machine demands it.
- **Homebrew / npm / other package managers**: the single static binary plus
  the installer URL covers the need; a package manager is a second channel
  to keep honest. Revisit when users ask for one.

## Module path is the repo URL

The Go module is `github.com/williamfzc/skill-compiler` (not a vanity name),
so outsiders can `go install github.com/williamfzc/skill-compiler/cmd/skillc@latest`
directly from the public repository. The rename from the old `skillscope`
module path was purely mechanical; `internal/` keeps outside code from
importing anything but `cmd/skillc` anyway.
