#!/bin/sh
# Cuts and promotes content releases (Suite D60, D61; build plan task 1.10).
# The content workflow (.github/workflows/content.yml) runs it.
#
#   content-release.sh edge               cut content-<version> from content/, move edge to it
#   content-release.sh promote <version>  move stable to a version edge has had
#
# It publishes the layout bud sync reads (docs/internals/sync.md):
#   content-<v>/content-<v>.tar.gz, content.json, content.json.sig
#   content-channel/channel.json, channel.json.sig
# Every release it makes is a GitHub prerelease, so none ever becomes the
# repo's latest release and install.sh's releases/latest/download keeps
# finding the code archives.
#
# Versions are YYYY.MM.DD.N, N counting the day's cuts from 1, worked out from
# the content-* tags. A re-run on a commit that already has a content tag
# reuses that version, so a run that failed after the release went up only
# finishes moving the channel. The published channel.json is the start
# point only once its signature verifies (bud content channel --from); with
# no content-channel tag yet, this is the first release (--new).
#
# Needs: git with tags fetched, gh (GH_TOKEN), BUD_RELEASE_KEY for the
# signer, and GITHUB_SHA. BUD (default bud) and SIGN (default
# scripts/sign-release.sh) pick the tools; BUD_CONTENT_DATE (YYYY.MM.DD)
# and OUT (default dist/content) are for tests.
#   just content-release-test covers it

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
bud="${BUD:-bud}"
sign="${SIGN:-$here/sign-release.sh}"
out="${OUT:-dist/content}"
channel_tag=content-channel

fail() {
	echo "content-release: $*" >&2
	exit 1
}

: "${GITHUB_SHA:?GITHUB_SHA must name the commit to release}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-content-release.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM
mkdir -p "$out"

# move_channel CHANNEL VERSION: re-signs channel.json with CHANNEL at VERSION
# and uploads it, creating the channel release on the first run
move_channel() {
	if git rev-parse -q --verify "refs/tags/$channel_tag" >/dev/null; then
		gh release download "$channel_tag" -p channel.json -p channel.json.sig -D "$tmp/published" ||
			fail "can't download the published channel.json from the $channel_tag release; fix or re-upload it, then run again"
		"$bud" content channel --from "$tmp/published/channel.json" --set "$1=$2" --out "$out"
	else
		"$bud" content channel --new --set "$1=$2" --out "$out"
	fi
	sh "$sign" "$out/channel.json" bud-channel

	if git rev-parse -q --verify "refs/tags/$channel_tag" >/dev/null; then
		gh release upload "$channel_tag" "$out/channel.json" "$out/channel.json.sig" --clobber
	else
		gh release create "$channel_tag" --prerelease --target "$GITHUB_SHA" --title "Content channel" \
			--notes "The newest content version on each channel, for bud sync. Rewritten by every content release; never install from here." \
			"$out/channel.json" "$out/channel.json.sig"
	fi
	echo "$1 is at $2"
}

case "${1:-}" in
edge)
	[ $# -eq 1 ] || fail "usage: content-release.sh edge"
	# a re-run on a commit already released finishes that release
	version="$(git tag --points-at "$GITHUB_SHA" -l 'content-[0-9]*' | sed 's/^content-//' | sort -t. -k1,1n -k2,2n -k3,3n -k4,4n | tail -n1)"
	if [ -n "$version" ]; then
		echo "content-$version is already this commit's release; moving edge to it"
	else
		day="${BUD_CONTENT_DATE:-$(date -u +%Y.%m.%d)}"
		n="$(git tag -l "content-$day.*" | sed "s/^content-$day\.//" | grep -E '^[0-9]+$' | sort -n | tail -n1 || true)"
		version="$day.$((${n:-0} + 1))"
		"$bud" content pack --version "$version" --out "$out" content
		sh "$sign" "$out/content.json" bud-content
		gh release create "content-$version" --prerelease --target "$GITHUB_SHA" --title "Content $version" \
			--notes "Content $version, built from $GITHUB_SHA. Devices on edge get it with bud sync; it reaches stable when promoted." \
			"$out/content-$version.tar.gz" "$out/content.json" "$out/content.json.sig"
	fi
	move_channel edge "$version"
	;;
promote)
	[ $# -eq 2 ] || fail "usage: content-release.sh promote <version>"
	version="$2"
	git rev-parse -q --verify "refs/tags/content-$version" >/dev/null ||
		fail "there's no content-$version release to promote; pick one from the content-* releases"
	move_channel stable "$version"
	;;
*)
	fail "usage: content-release.sh edge | promote <version>"
	;;
esac
