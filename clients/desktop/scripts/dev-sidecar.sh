#!/bin/sh
# Tauri copies external binaries into target/debug only when it starts. Build
# the current Go server before every desktop dev session so frontend work is
# never tested against yesterday's sidecar.
set -eu

desktop_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
repo_dir=$(CDPATH= cd -- "$desktop_dir/../.." && pwd)
target=$(rustc -vV | sed -n 's/^host: //p')

case "$target" in
	x86_64-apple-darwin) goos=darwin; goarch=amd64 ;;
	aarch64-apple-darwin) goos=darwin; goarch=arm64 ;;
	x86_64-pc-windows-msvc) goos=windows; goarch=amd64 ;;
	aarch64-pc-windows-msvc) goos=windows; goarch=arm64 ;;
	*) echo "Unsupported Tauri target: $target" >&2; exit 1 ;;
esac

suffix=""
[ "$goos" = windows ] && suffix=.exe
binary="$desktop_dir/src-tauri/binaries/pixeltui-$target$suffix"

cd "$repo_dir"
CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -ldflags='-s -w' -o "$binary" ./tui

cd "$desktop_dir"
exec pnpm dev
