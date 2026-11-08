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
# it overran, 254 when interrupted (Ctrl-C), and with the command's own status
# otherwise
limited() {
	perl -e '
		use POSIX ":sys_wait_h";
		my $limit = shift;
		my $pid = fork() // die "fork: $!\n";
		if ($pid == 0) {
			setpgrp(0, 0);
			exec(@ARGV) or die "exec $ARGV[0]: $!\n";
		}
		# the group goes with the test: on overrun, on Ctrl-C, and whatever
		# it left running when it finished. TERM first, KILL only for what
		# is still there a second later
		sub stop {
			kill("TERM", -$pid) or return;
			for (1 .. 10) {
				# reap the test itself, or as a zombie it stays in the group
				waitpid($pid, WNOHANG);
				kill(0, -$pid) or return;
				select(undef, undef, undef, 0.1);
			}
			kill("KILL", -$pid);
		}
		my $done = 0;
		$SIG{ALRM} = sub { return if $done; stop(); exit(142) };
		$SIG{INT} = $SIG{TERM} = sub { stop(); exit(254) };
		alarm($limit);
		waitpid($pid, 0);
		$done = 1;
		alarm(0);
		my $status = $? & 127 ? 128 + ($? & 127) : $? >> 8;
		stop();
		exit($status);
	' "$@"
}

if [ $# -eq 0 ]; then set -- tests/lua/*_test.lua; fi

for t in "$@"; do
	limited "$limit" nvim --clean --headless -l "$t"
	status=$?
	# Ctrl-C: the test's group is gone already, so just stop
	if [ "$status" -eq 254 ]; then exit 130; fi
	if [ "$status" -eq 142 ]; then
		echo "lua-test: $t: still running after ${limit}s, stopped it" >&2
		exit 1
	fi
	if [ "$status" -ne 0 ]; then
		echo "lua-test: $t: failed (exit $status)" >&2
		exit 1
	fi
done
