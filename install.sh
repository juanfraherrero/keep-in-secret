#!/bin/sh
# Installs the latest kis release:
#   curl -fsSL https://raw.githubusercontent.com/juanfraherrero/keep-in-secret/main/install.sh | sh
# KIS_VERSION=v0.1.0 pins a release; KIS_INSTALL_DIR picks the directory.
set -eu

repo=juanfraherrero/keep-in-secret

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "kis: unsupported OS $(uname -s); on Windows download the zip from https://github.com/$repo/releases" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "kis: unsupported CPU $(uname -m); build from source: go install github.com/$repo/cmd/kis@latest" >&2; exit 1 ;;
esac

if [ -n "${KIS_VERSION:-}" ]; then
  base="https://github.com/$repo/releases/download/$KIS_VERSION"
else
  base="https://github.com/$repo/releases/latest/download"
fi
archive="kis_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
else
  got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
fi
if [ -z "$want" ] || [ "$want" != "$got" ]; then
  echo "kis: checksum mismatch for $archive, not installing" >&2
  exit 1
fi
tar -xzf "$tmp/$archive" -C "$tmp" kis

dir=${KIS_INSTALL_DIR:-}
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
fi
mkdir -p "$dir"
mv "$tmp/kis" "$dir/kis"
ln -sf kis "$dir/git-kis"

echo "installed $("$dir/kis" version) to $dir/kis (git kis works too)"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "add $dir to your PATH, e.g.: echo 'export PATH=\"$dir:\$PATH\"' >> ~/.profile" ;;
esac
