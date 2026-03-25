#!/usr/bin/env bash
set -euo pipefail

os="$(uname -s)"
if [ "$os" != "Darwin" ]; then
  echo "error: wecodex install script supports Darwin only (got: $os)" >&2
  exit 1
fi

machine="$(uname -m)"
case "$machine" in
  arm64|aarch64)
    arch="arm64"
    ;;
  x86_64|amd64)
    arch="amd64"
    ;;
  *)
    echo "error: unsupported architecture: $machine" >&2
    exit 1
    ;;
esac

install_dir=""
old_ifs="$IFS"
IFS=:
for entry in $PATH; do
  [ -n "$entry" ] || continue
  if [ -d "$entry" ] && [ -w "$entry" ]; then
    install_dir="$entry"
    break
  fi
done
IFS="$old_ifs"

if [ -z "$install_dir" ]; then
  echo "error: no writable directory found in PATH" >&2
  exit 1
fi

asset="wecodex-darwin-${arch}.tar.gz"
url="https://github.com/xiongzheX/weCodex/releases/latest/download/${asset}"

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

archive_path="$tmpdir/$asset"
curl -fsSL "$url" -o "$archive_path"
tar -xzf "$archive_path" -C "$tmpdir"

if [ ! -f "$tmpdir/wecodex" ]; then
  echo "error: release archive missing wecodex executable" >&2
  exit 1
fi

install -m 0755 "$tmpdir/wecodex" "$install_dir/wecodex"
if [ ! -e "$install_dir/weCodex" ]; then
  ln -s "wecodex" "$install_dir/weCodex"
fi

echo "wecodex installed to $install_dir"
