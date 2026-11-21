#!/bin/sh
# Headless smoke test: syncs the embedded config into a throwaway XDG tree with
# `bud update` (which also restores the plugins pinned in nvim-pack-lock.json), then
# starts Neovim on it and fails on any startup error. Needs go, git, nvim and
# network access to fetch the plugins. Pass the path to a previous Bud binary
# to seed the XDG tree before checking the candidate.
# Run from the repo root: sh scripts/smoke.sh [previous-bud]

set -eu

root="$(mktemp -d "${TMPDIR:-/tmp}/bud-smoke.XXXXXX")"
trap 'rm -rf "$root"' EXIT INT TERM

go build -o "$root/bud" ./cmd/bud

# only bud's XDG dirs move, so Go and git keep using the real HOME
export XDG_CONFIG_HOME="$root/config"
export XDG_DATA_HOME="$root/data"
export XDG_STATE_HOME="$root/state"
export XDG_CACHE_HOME="$root/cache"

# no languages, so the restore doesn't build parsers or install Mason packages;
# smoke.lua still checks every spec loads, and scripts/lang-check.sh covers the rest
mkdir -p "$XDG_STATE_HOME/bud"
machine_config="$XDG_STATE_HOME/bud/config.json"
printf '%s\n' '{"languages": []}' >"$machine_config"

echo "smoke: $(nvim --version | head -n 1)"
if [ "${1:-}" ]; then
	# The baseline binary must live outside the XDG tree and has no install
	# receipt in it. This runs its config sync without replacing the binary.
	echo "smoke: seeding from $("$1" version)"
	"$1" update --no-doctor
	test -s "$XDG_CONFIG_HOME/bud/.bud-manifest.json"
	mkdir -p "$XDG_CONFIG_HOME/bud/local" "$XDG_CONFIG_HOME/nvim"
	printf '%s\n' '-- user-owned editor config' >"$root/owned.lua"
	cp "$root/owned.lua" "$XDG_CONFIG_HOME/bud/local/upgrade-fixture.lua"
	cp "$root/owned.lua" "$XDG_CONFIG_HOME/nvim/init.lua"
	cp "$machine_config" "$root/machine-before.json"
fi

"$root/bud" update --no-doctor
cmp ide/nvim/init.lua "$XDG_CONFIG_HOME/bud/init.lua"

if [ "${1:-}" ]; then
	cmp "$root/owned.lua" "$XDG_CONFIG_HOME/bud/local/upgrade-fixture.lua"
	cmp "$root/owned.lua" "$XDG_CONFIG_HOME/nvim/init.lua"
	cmp "$root/machine-before.json" "$machine_config"
fi

status=0
NVIM_APPNAME=bud nvim --headless -c "luafile scripts/smoke.lua" 2>"$root/stderr" || status=$?

# headless Neovim reports some startup errors on stderr and still exits 0
if [ "$status" -ne 0 ] || grep -Eq 'Error|E[0-9]+:' "$root/stderr"; then
	cat "$root/stderr" >&2
	echo "smoke: FAILED (exit $status)" >&2
	exit 1
fi
echo "smoke: ok"
