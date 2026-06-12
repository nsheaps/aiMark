#!/bin/sh
# aimark installer — https://aimark.dev/install.sh
#
#   curl -fsSL https://aimark.dev/install.sh | sh
#
# Downloads the latest aimark release binary for this OS/arch from GitHub
# Releases, verifies the checksum, and installs to ~/.local/bin (or
# AIMARK_INSTALL_DIR).
set -eu

REPO="nsheaps/aiMark"
INSTALL_DIR="${AIMARK_INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin | linux) ;;
  *) echo "aimark install: unsupported OS: $os (windows: download the zip from GitHub Releases)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "aimark install: unsupported architecture: $arch" >&2; exit 1 ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
  grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
[ -n "$tag" ] || { echo "aimark install: could not resolve latest release" >&2; exit 1; }
version=${tag#v}

base="https://github.com/$REPO/releases/download/$tag"
archive="aimark_${version}_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading aimark $tag ($os/$arch)..."
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

(cd "$tmp" && grep " $archive\$" checksums.txt | sha256sum -c - >/dev/null 2>&1) ||
  (cd "$tmp" && grep " $archive\$" checksums.txt | shasum -a 256 -c - >/dev/null) ||
  { echo "aimark install: checksum verification failed" >&2; exit 1; }

tar -xzf "$tmp/$archive" -C "$tmp" aimark
mkdir -p "$INSTALL_DIR"
install -m 755 "$tmp/aimark" "$INSTALL_DIR/aimark"

echo "Installed aimark $tag to $INSTALL_DIR/aimark"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "NOTE: $INSTALL_DIR is not on your PATH — add it to your shell profile." ;;
esac
"$INSTALL_DIR/aimark" version
