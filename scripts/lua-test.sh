#!/bin/sh
# Runs the headless Lua tests (tests/lua/*_test.lua, or the files given), each in
# its own `nvim --clean --headless -l` with a time limit, and stops at the first
# failure. A test still running after the limit is killed and fails with its name,
# so a hang never stalls `just check` or CI. macOS has no `timeout`, so perl
# does the timing: it runs each test in a process group of its own and kills
# the whole group, so nothing the test started is left holding the output open.
# Run from the repo root: sh scripts/lua-test.sh [test ...]

set -u

limit=60

# limited runs "$@" for at most $1 seconds, exiting 142 (SIGALRM's status) when
# it overran and with the command's own status otherwise
limited() {
	perl -e '
		my $limit = shift;
		my $pid = fork() // die "fork: $!\n";
		if ($pid == 0) {
			setpgrp(0, 0);
			exec(@ARGV) or die "exec $ARGV[0]: $!\n";
		}
		$SIG{ALRM} = sub { kill("TERM", -$pid); sleep(1); kill("KILL", -$pid); exit(142) };
		alarm($limit);
		waitpid($pid, 0);
		exit($? & 127 ? 128 + ($? & 127) : $? >> 8);
	' "$@"
}

if [ $# -eq 0 ]; then set -- tests/lua/*_test.lua; fi

for t in "$@"; do
	limited "$limit" nvim --clean --headless -l "$t"
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
