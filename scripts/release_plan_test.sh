#!/bin/sh
# Runs release-plan.sh against throwaway repos with a fake gh and checks what
# it decides for each way the release workflow starts (D80).
#   sh scripts/release_plan_test.sh   (just release-plan-test)

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-release-plan-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

# fake gh: a release exists when its tag is listed in $tmp/released
mkdir -p "$tmp/bin"
cat >"$tmp/bin/gh" <<FAKE
#!/bin/sh
[ "\$1 \$2" = "release view" ] && grep -qx "\$3" "$tmp/released" 2>/dev/null
FAKE
chmod +x "$tmp/bin/gh"

failed=0
fail() {
	echo "release_plan_test: $*" >&2
	failed=1
}

# repo VERSION TAG... makes a fresh repo at $tmp/repo with VERSION and tags
repo() {
	rm -rf "$tmp/repo" "$tmp/released"
	mkdir -p "$tmp/repo"
	git -C "$tmp/repo" init -q
	echo "$1" >"$tmp/repo/VERSION"
	git -C "$tmp/repo" add VERSION
	git -C "$tmp/repo" -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m init
	shift
	for t in "$@"; do git -C "$tmp/repo" tag "$t"; done
}

# plan NAME REF WANT: runs the script for REF (refs/heads/... or refs/tags/...)
# and checks its outputs, one per line in order, or "fail" when it should fail
plan() {
	name="$1" ref="$2" want="$3"
	: >"$tmp/out"
	case "$ref" in
	refs/tags/*) type=tag ;;
	*) type=branch ;;
	esac
	if (cd "$tmp/repo" && PATH="$tmp/bin:$PATH" GITHUB_REF="$ref" GITHUB_REF_TYPE="$type" \
		GITHUB_REF_NAME="${ref##*/}" GITHUB_OUTPUT="$tmp/out" sh "$here/release-plan.sh" >"$tmp/log" 2>&1); then
		got="$(cat "$tmp/out")"
	else
		got=fail
	fi
	[ "$got" = "$want" ] || fail "$name: got '$got', want '$want' ($(cat "$tmp/log"))"
}

main=refs/heads/main
nl='
'

repo 0.18.1 v0.18.0 v0.18.0-rc.1
plan "a newer VERSION" "$main" "new=true${nl}tag=v0.18.1"

repo 0.18.1 v0.18.0 v0.18.1
echo v0.18.1 >"$tmp/released"
plan "an already released VERSION" "$main" "skip=true"

repo 0.18.1 v0.18.0 v0.18.1
plan "a tag without a release" "$main" "tag=v0.18.1"

repo 0.17.2 v0.18.0
plan "an older VERSION" "$main" fail

repo 0.10.0 v0.9.0
plan "10 is newer than 9" "$main" "new=true${nl}tag=v0.10.0"

repo 0.1.0
plan "the first release" "$main" "new=true${nl}tag=v0.1.0"

repo "  0.18.1 ${nl}" v0.18.0
plan "whitespace around VERSION" "$main" "new=true${nl}tag=v0.18.1"

repo v0.18.1
plan "a v in VERSION" "$main" fail

repo 0.18.1
plan "a branch other than main" refs/heads/feature fail

repo 0.18.1 v0.18.1
plan "a pushed tag matching VERSION" refs/tags/v0.18.1 "tag=v0.18.1"
plan "a pushed tag not matching VERSION" refs/tags/v0.18.2 fail

[ "$failed" = 0 ] && echo "release_plan_test: ok"
exit "$failed"
