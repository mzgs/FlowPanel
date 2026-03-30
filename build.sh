#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./build.sh <linux|mac|windows> [amd64|arm64]

Examples:
  ./build.sh linux
  ./build.sh mac arm64
  ./build.sh windows amd64

Environment overrides:
  GOARCH
  CGO_ENABLED

Defaults:
  linux/windows -> amd64
  mac -> host GOARCH
EOF
}

if [[ $# -lt 1 || $# -gt 2 ]]; then
  usage
  exit 1
fi

target="$1"

case "$target" in
  linux)
    goos="linux"
    ;;
  mac | macos | darwin)
    goos="darwin"
    ;;
  windows | win)
    goos="windows"
    ;;
  *)
    echo "unsupported target: $target" >&2
    usage
    exit 1
    ;;
esac

default_goarch="$(go env GOARCH)"
if [[ "$goos" == "linux" || "$goos" == "windows" ]]; then
  default_goarch="amd64"
fi

goarch="${2:-${GOARCH:-$default_goarch}}"

case "$goarch" in
  amd64 | arm64)
    ;;
  *)
    echo "unsupported architecture: $goarch" >&2
    usage
    exit 1
    ;;
esac

if [[ ! -d web/dist ]]; then
  echo "missing web/dist; build the frontend first with 'npm run build' in web/panel" >&2
  exit 1
fi

ext=""
if [[ "$goos" == "windows" ]]; then
  ext=".exe"
fi

out_dir="dist"
out_file="$out_dir/flowpanel-${goos}-${goarch}${ext}"

mkdir -p "$out_dir"

echo "Building $out_file"

CGO_ENABLED="${CGO_ENABLED:-0}" \
GOOS="$goos" \
GOARCH="$goarch" \
go build \
  -trimpath \
  -ldflags="-s -w" \
  -o "$out_file" \
  ./cmd/flowpanel

echo "Built $out_file"
