#!/bin/sh
set -eu

REPO="ragam20/ssh-manager"
PREFIX=${PREFIX:-"$HOME/.local"}
BIN_DIR="$PREFIX/bin"
BIN_PATH="$BIN_DIR/sshm"
ACTION=${1:-install}

case "$ACTION" in
  install|update|uninstall) ;;
  *) echo "usage: $0 [install|update|uninstall]" >&2; exit 2 ;;
esac

if [ "$ACTION" = uninstall ]; then
  if [ -e "$BIN_PATH" ]; then
    rm -f "$BIN_PATH"
    echo "uninstalled sshm from $BIN_PATH"
  else
    echo "sshm is not installed at $BIN_PATH"
  fi
  exit 0
fi

if [ "$ACTION" = update ] && [ ! -x "$BIN_PATH" ]; then
  echo "sshm is not installed at $BIN_PATH; run install first" >&2
  exit 1
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in linux|darwin) ;; *) echo "unsupported operating system: $os" >&2; exit 1 ;; esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

asset="sshm-$os-$arch"
base="https://github.com/$REPO/releases/latest/download"
tmpdir=$(mktemp -d)
stagedir=
cleanup() {
  [ -z "$stagedir" ] || rm -rf "$stagedir"
  rm -rf "$tmpdir"
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to download sshm" >&2
  exit 1
fi
curl -fsSL "$base/$asset" -o "$tmpdir/$asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmpdir/SHA256SUMS"

expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1; exit }' "$tmpdir/SHA256SUMS")
if [ -z "$expected" ]; then
  echo "checksum for $asset is missing from release" >&2
  exit 1
fi
case "$os" in
  linux) command -v sha256sum >/dev/null 2>&1 || { echo "sha256sum is required to verify downloads" >&2; exit 1; }; actual=$(sha256sum "$tmpdir/$asset" | awk '{print $1}') ;;
  darwin) command -v shasum >/dev/null 2>&1 || { echo "shasum is required to verify downloads" >&2; exit 1; }; actual=$(shasum -a 256 "$tmpdir/$asset" | awk '{print $1}') ;;
esac
if [ "$expected" != "$actual" ]; then
  echo "checksum verification failed for $asset" >&2
  exit 1
fi

chmod 0755 "$tmpdir/$asset"
"$tmpdir/$asset" version >/dev/null
install -d "$BIN_DIR"
if [ -d "$BIN_PATH" ]; then
  echo "cannot replace directory: $BIN_PATH" >&2
  exit 1
fi
# Stage on the destination filesystem so the final rename is atomic.
stagedir=$(mktemp -d "$BIN_DIR/.sshm.XXXXXX")
install -m 0755 "$tmpdir/$asset" "$stagedir/sshm"
mv -f "$stagedir/sshm" "$BIN_PATH"
echo "$ACTION complete: $BIN_PATH ($("$BIN_PATH" version))"
case ":$PATH:" in *":$BIN_DIR:"*) ;; *) echo "warning: $BIN_DIR is not on PATH" ;; esac
