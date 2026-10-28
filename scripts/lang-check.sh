#!/bin/sh
# Language check: syncs the embedded config into a throwaway XDG tree with every
# language enabled, so `bud update` builds every parser and installs every Mason
# package, then runs scripts/lang_check.lua, which opens a sample project per
# language and checks its parsers, servers, linters and formatters. Needs go,
# git, nvim, the tree-sitter CLI, a C compiler, node and python3, and network
# access. Run from the repo root: sh scripts/lang-check.sh [language ...]

set -eu

root="$(mktemp -d "${TMPDIR:-/tmp}/bud-lang-check.XXXXXX")"
trap 'rm -rf "$root"' EXIT INT TERM

go build -o "$root/bud" ./cmd/bud

# only bud's XDG dirs move, so Go and git keep using the real HOME
export XDG_CONFIG_HOME="$root/config"
export XDG_DATA_HOME="$root/data"
export XDG_STATE_HOME="$root/state"
export XDG_CACHE_HOME="$root/cache"

echo "lang-check: $(nvim --version | head -n 1)"
"$root/bud" update
echo "lang-check: Mason packages take $(du -sh "$XDG_DATA_HOME/bud/mason/packages" | cut -f1)"

# the sample projects go under $root too, so the trap cleans them up
BUD_LANG_CHECK_DIR="$root/projects" BUD_LANG_CHECK_ONLY="$*" \
	NVIM_APPNAME=bud nvim --headless \
	-c "lua local ok, err = pcall(dofile, 'scripts/lang_check.lua') if not ok then io.stderr:write(err .. '\\n') vim.cmd('cquit 1') end"
