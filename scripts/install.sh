#!/usr/bin/env bash
# One-key installer for skillc: fetches a release tarball from GitHub,
# verifies it against that release's checksums.txt, and installs the binary
# into a user bin dir.
#
# Canonical copy: scripts/install.sh in the repo; the release workflow also
# attaches it to every release, so
#   curl -fsSL https://github.com/williamfzc/skill-compiler/releases/latest/download/install.sh | bash
# works without a checkout. Env: SKILLC_INSTALL_DIR (default ~/.local/bin),
# SKILLC_VERSION (default: latest release, e.g. SKILLC_VERSION=v0.1.0).

set -euo pipefail

REPO="williamfzc/skill-compiler"
INSTALL_DIR="${SKILLC_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${SKILLC_VERSION:-latest}"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) echo "error: $(uname -s) is not supported (releases cover macOS and Linux)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64)   arch=arm64 ;;
  x86_64|amd64)    arch=amd64 ;;
  *) echo "error: $(uname -m) is not a supported architecture" >&2; exit 1 ;;
esac

base="https://github.com/$REPO/releases"
if [[ "$VERSION" == "latest" ]]; then
  base="$base/latest/download"
else
  base="$base/download/$VERSION"
fi
asset="skillc_${os}_${arch}.tar.gz"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "fetching $asset ..."
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

want="$(awk -v a="$asset" '$2 == a { print $1 }' "$tmp/checksums.txt")"
if [[ -z "$want" ]]; then
  echo "error: checksums.txt has no entry for $asset" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum < "$tmp/$asset" | awk '{print $1}')"
else
  got="$(shasum -a 256 < "$tmp/$asset" | awk '{print $1}')"
fi
if [[ "$got" != "$want" ]]; then
  echo "error: checksum mismatch for $asset (want $want, got $got)" >&2
  exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$INSTALL_DIR"
mv "$tmp/skillc" "$INSTALL_DIR/skillc"
chmod +x "$INSTALL_DIR/skillc"

echo "installed skillc -> $INSTALL_DIR/skillc"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: $INSTALL_DIR is not on your PATH; add it to use bare 'skillc'" >&2 ;;
esac
echo "try: $INSTALL_DIR/skillc --help"
