#!/bin/sh
# Works out what the release workflow does (D80), from VERSION and the ref
# that started it, and writes it to $GITHUB_OUTPUT:
#   tag=vX.Y.Z  the release to build
#   new=true    the tag doesn't exist yet: test, then push it
#   skip=true   VERSION's release already exists; nothing to do
# Fails when VERSION isn't X.Y.Z, when a pushed tag isn't VERSION's, when a
# run isn't on main, or when VERSION is older than the newest release.
# Needs: GITHUB_REF, GITHUB_REF_TYPE, GITHUB_REF_NAME, GITHUB_OUTPUT, and gh
# (with GH_TOKEN) to ask whether a tag already has a release.
#   just release-plan-test covers it

set -eu

out() { echo "$1" >>"$GITHUB_OUTPUT"; }
fail() {
	echo "::error::$*"
	exit 1
}

version="$(tr -d '[:space:]' <VERSION)"
echo "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || fail "VERSION is '$version', not X.Y.Z"
tag="v$version"

if [ "$GITHUB_REF_TYPE" = tag ]; then
	[ "$GITHUB_REF_NAME" = "$tag" ] || fail "pushed $GITHUB_REF_NAME, but VERSION says $version; change VERSION instead"
elif [ "$GITHUB_REF" != refs/heads/main ]; then
	fail "releases come from main, not $GITHUB_REF"
elif git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
	if gh release view "$tag" >/dev/null 2>&1; then
		echo "$tag is already released"
		out skip=true
		exit 0
	fi
	# a run that failed after pushing the tag: build what the tag points at
	echo "$tag has no release yet; building it"
	git checkout -q "$tag"
else
	newest="$(git tag -l 'v*' --sort=-v:refname | grep -v -- - | head -n1 || true)"
	if [ -n "$newest" ] && [ "$(printf '%s\n%s\n' "${newest#v}" "$version" | sort -V | tail -n1)" != "$version" ]; then
		fail "VERSION $version is older than the newest release, $newest"
	fi
	out new=true
fi

echo "releasing $tag"
out "tag=$tag"
