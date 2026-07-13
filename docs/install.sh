#!/bin/sh
# emday installer — https://madnh.github.io/emday
#
# Usage:
#   curl -fsSL https://madnh.github.io/emday/install.sh | sh
#
# Environment overrides:
#   EMDAY_VERSION=0.1.0            install a specific version (default: latest)
#   EMDAY_INSTALL_DIR=/opt/bin     install location (default: /usr/local/bin)
#
# The script downloads the release archive for this OS/arch from GitHub,
# verifies its sha256 against the release's checksums.txt, and installs the
# single binary. Nothing else is touched. Windows: download the zip from
# https://github.com/madnh/emday/releases instead.
set -eu

REPO="madnh/emday"
INSTALL_DIR="${EMDAY_INSTALL_DIR:-/usr/local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *)
    echo "unsupported OS: $os — download manually from https://github.com/$REPO/releases" >&2
    exit 1
    ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *)
    echo "unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

version="${EMDAY_VERSION:-}"
if [ -z "$version" ]; then
  version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' | head -1)
  if [ -z "$version" ]; then
    echo "could not determine the latest version — pass EMDAY_VERSION=x.y.z" >&2
    exit 1
  fi
fi
version="${version#v}"

asset="emday_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/v${version}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading emday v${version} (${os}/${arch})..."
curl -fsSL -o "$tmp/$asset" "$base/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

cd "$tmp"
if command -v sha256sum >/dev/null 2>&1; then
  grep " $asset\$" checksums.txt | sha256sum -c - >/dev/null
  echo "Checksum OK."
elif command -v shasum >/dev/null 2>&1; then
  grep " $asset\$" checksums.txt | shasum -a 256 -c - >/dev/null
  echo "Checksum OK."
else
  echo "warning: no sha256sum/shasum found — skipping checksum verification" >&2
fi

tar -xzf "$asset" emday

if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 emday "$INSTALL_DIR/emday"
else
  echo "Installing to $INSTALL_DIR (requires sudo)..."
  sudo install -m 0755 emday "$INSTALL_DIR/emday"
fi

echo ""
echo "Installed: $("$INSTALL_DIR/emday" version)"
echo "Next steps:"
echo "  sudo emday init        # create the config directory"
echo "  emday docs             # built-in documentation"
