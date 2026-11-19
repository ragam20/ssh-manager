#!/bin/sh
# Install a prebuilt binary when available, or build from source.
# Usage: ./install.sh            (ke ~/.local/bin)
#            PREFIX=/usr/local ./install.sh   (butuh sudo)
set -e
cd "$(dirname "$0")"
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) arch= ;;
esac
prefix="${PREFIX:-$HOME/.local}"
src=
if [ -n "$arch" ]; then
  candidate="bin/sshm-$os-$arch"
  [ ! -f "$candidate" ] || src=$candidate
fi

if [ -n "$src" ]; then
  install -d "$prefix/bin"
  install -m 0755 "$src" "$prefix/bin/sshm"
  echo "installed at $prefix/bin/sshm"
else
  if ! command -v make >/dev/null 2>&1 || ! command -v go >/dev/null 2>&1; then
    echo "no prebuilt binary is available; installing from source requires Go and Make" >&2
    exit 1
  fi
  PREFIX="$prefix" make install
fi
case ":$PATH:" in *":$prefix/bin:"*) ;; *) echo "warning: $prefix/bin is not on PATH" ;; esac
"$prefix/bin/sshm" version
