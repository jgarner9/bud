#!/bin/sh
# Runs leak-check.sh against throwaway repos and checks what it flags.
#   sh scripts/leak_check_test.sh   (just leak-test)

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-leak-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

failed=0
fail() {
	echo "leak_check_test: $*" >&2
	failed=1
}

# check NAME WANT LINE: a fresh repo whose one file holds LINE, then
# leak-check.sh, which should "pass" or "fail"; BUD_LEAK_PATTERNS passes through
check() {
	name="$1" want="$2" line="$3"
	rm -rf "$tmp/repo"
	mkdir -p "$tmp/repo/scripts"
	cp "$here/leak-check.sh" "$tmp/repo/scripts/"
	git -C "$tmp/repo" init -q
	printf '%s\n' "$line" >"$tmp/repo/notes.md"
	git -C "$tmp/repo" add -A
	status=0
	(cd "$tmp/repo" && sh scripts/leak-check.sh) >"$tmp/out" 2>&1 || status=$?
	if [ "$want" = pass ] && [ "$status" -ne 0 ]; then
		fail "$name: should pass, got: $(cat "$tmp/out")"
	elif [ "$want" = fail ] && [ "$status" -eq 0 ]; then
		fail "$name: should fail"
	fi
}

check "a real tailnet" fail 'reach it at home.tail4a2b9.ts.net'
check "a placeholder tailnet" pass 'such as wss://gateway.example.ts.net:18789 or home.your-tailnet.ts.net'
check "the bare suffix" pass 'a .ts.net name'
check "a real vault" fail 'key: op://Personal/github/token'
check "the placeholder vault" pass 'an op://vault/item/field reference'
check "a real home" fail 'see /Users/jasongarner/notes'
check "a placeholder home" pass '/home/you/.config and /Users/j/bud'
check "an allowed line" pass 'home.tail4a2b9.ts.net  <!-- leak-check: allow -->'
check "an untracked file is ignored" pass 'nothing here'
echo 'home.tail4a2b9.ts.net' >"$tmp/repo/untracked.md"
(cd "$tmp/repo" && sh scripts/leak-check.sh) >/dev/null 2>&1 || fail "an untracked file was checked"

BUD_LEAK_PATTERNS='
mybox[0-9]*
garner-mbp'
export BUD_LEAK_PATTERNS
check "a personal pattern" fail 'ssh mybox2 for the logs'
check "no personal pattern" pass 'ssh the server for the logs'
check "the report hides the pattern's match" fail 'ssh garner-mbp'
if grep -q 'garner-mbp' "$tmp/out"; then fail "the report printed a personal match"; fi
check "a shape match is hidden too" fail 'reach mybox7.tail4a2b9.ts.net'
if grep -q 'mybox7' "$tmp/out"; then fail "the report printed a shape match that a personal pattern covers"; fi

check "the scripts get the personal patterns" pass 'nothing here'
echo '# ssh mybox3' >>"$tmp/repo/scripts/leak-check.sh"
git -C "$tmp/repo" add -A
(cd "$tmp/repo" && sh scripts/leak-check.sh) >/dev/null 2>&1 && fail "a personal pattern in leak-check.sh itself passed"

BUD_LEAK_PATTERNS='good
['
check "an invalid pattern fails" fail 'nothing here'
if grep -q '\[' "$tmp/out"; then fail "the invalid pattern was printed"; fi
unset BUD_LEAK_PATTERNS

check "a file named like a grep option" fail 'reach it at home.tail4a2b9.ts.net'
echo 'x' >"$tmp/repo/-q"
git -C "$tmp/repo" add -- -q
(cd "$tmp/repo" && sh scripts/leak-check.sh) >/dev/null 2>&1 && fail "a tracked file named -q hid a real tailnet"

if [ "$failed" -ne 0 ]; then exit 1; fi
echo "leak_check_test: ok"
