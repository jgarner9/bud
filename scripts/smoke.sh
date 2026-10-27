#!/bin/sh
# Headless smoke test: syncs the embedded config into a throwaway XDG tree with
# `bud update` (which also restores the plugins pinned in lazy-lock.json), then
# starts Neovim on it and fails on any startup error. Needs go, git, nvim and
# network access to fetch the plugins. Run from the repo root: sh scripts/smoke.sh

set -eu

root="$(mktemp -d "${TMPDIR:-/tmp}/bud-smoke.XXXXXX")"
trap 'rm -rf "$root"' EXIT INT TERM

go build -o "$root/bud" ./cmd/bud

# only bud's XDG dirs move, so Go and git keep using the real HOME
export XDG_CONFIG_HOME="$root/config"
export XDG_DATA_HOME="$root/data"
export XDG_STATE_HOME="$root/state"
export XDG_CACHE_HOME="$root/cache"

echo "smoke: $(nvim --version | head -n 1)"
"$root/bud" update

status=0
NVIM_APPNAME=bud nvim --headless -c "luafile scripts/smoke.lua" 2>"$root/stderr" || status=$?

# headless Neovim reports some startup errors on stderr and still exits 0
if [ "$status" -ne 0 ] || grep -Eq 'Error|E[0-9]+:' "$root/stderr"; then
	cat "$root/stderr" >&2
	echo "smoke: FAILED (exit $status)" >&2
	exit 1
fi
echo "smoke: ok"
