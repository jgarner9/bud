#!/bin/sh
# Runs the headless Lua tests (tests/lua/*_test.lua, or the files given), each in
# its own `nvim --clean --headless -l` with a time limit, and stops at the first
# failure. A test still running after the limit is killed and fails with its name,
# so a hang never stalls `just check` or CI. macOS has no `timeout`, so perl's
# alarm, which survives the exec, does the killing. Run from the repo root:
# sh scripts/lua-test.sh [test ...]

set -u

limit=60

if [ $# -eq 0 ]; then set -- tests/lua/*_test.lua; fi

for t in "$@"; do
	perl -e 'alarm shift; exec @ARGV or die "exec $ARGV[0]: $!\n"' "$limit" nvim --clean --headless -l "$t"
	status=$?
	if [ "$status" -eq 142 ]; then
		echo "lua-test: $t: still running after ${limit}s, stopped it" >&2
		exit 1
	fi
	if [ "$status" -ne 0 ]; then
		echo "lua-test: $t: failed (exit $status)" >&2
		exit 1
	fi
done
