#!/bin/sh
# Cuts and promotes content releases (Suite D60, D61; build plan task 1.10).
# The content workflow (.github/workflows/content.yml) runs it.
#
#   content-release.sh edge               cut content-<version> from content/, then republish the channel
#   content-release.sh promote <version>  move stable to the version edge offers now
#   content-release.sh resign             rebuild and re-sign what the channel offers, then republish it
#
# It publishes the layout bud sync reads (docs/internals/sync.md):
#   content-<v>/content-<v>.tar.gz, content.json, content.json.sig
#   content-channel/channel.json, channel.json.sig
# Every release it makes is a GitHub prerelease, so none ever becomes the
# repo's latest release and install.sh's releases/latest/download keeps
# finding the code archives.
#
# The tags are the record. content-<v> is a release, content-stable-<v> a
# promotion, and channel.json is rebuilt from them on every run (edge at the
# newest release, stable at the newest promotion) and signed afresh. Before
# either channel asset is uploaded, a single-asset recovery archive containing
# the exact signed pair is persisted in the channel release. A retry verifies
# and reuses that pair when it matches the tags, never trusting a mismatched
# channel.json or signature downloaded separately. resign also rebuilds and
# re-signs the bundles the channel names, from their tagged commits, before an
# old release key is dropped.
#
# Versions are YYYY.MM.DD.N, N counting the day's cuts from 1. A run on a
# commit that already has a content tag cuts nothing new, and neither does a
# stale run on a commit the newest release's commit isn't an ancestor of, so
# a retried old run can never publish old content as a newer version.
#
# Needs: git with tags fetched and push access to tags, gh (GH_TOKEN),
# BUD_RELEASE_KEY for the signer, and GITHUB_SHA. BUD (default bud) and SIGN
# (default scripts/sign-release.sh) pick the tools; BUD_CONTENT_DATE
# (YYYY.MM.DD) and OUT (default dist/content) are for tests.
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

# newest PREFIX: the highest version among the tags PREFIX<version>
newest() {
	git tag -l "$1"'[0-9]*' | sed "s/^$1//" | grep -E '^[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.[0-9]+$' |
		sort -t. -k1,1n -k2,2n -k3,3n -k4,4n | tail -n1 || true
}

# recover_channel EDGE STABLE DIR: extracts the persisted signed pair only
# when it verifies and matches the versions recorded by the release tags.
recover_channel() {
	[ -n "$1" ] || return 1
	git rev-parse -q --verify "refs/tags/$channel_tag" >/dev/null || return 1
	gh release download "$channel_tag" -p channel-recovery.tar.gz -D "$tmp/recovery" >/dev/null 2>&1 || return 1
	recovery="$tmp/recovery/channel-recovery.tar.gz"
	entries="$(tar -tzf "$recovery" | LC_ALL=C sort)" || return 1
	[ "$entries" = "$(printf 'channel.json\nchannel.json.sig')" ] || return 1
	mkdir -p "$3"
	tar -xOzf "$recovery" channel.json >"$3/channel.json" || return 1
	tar -xOzf "$recovery" channel.json.sig >"$3/channel.json.sig" || return 1
	recovered_edge="$("$bud" content offered --channel edge "$3/channel.json" 2>/dev/null)" || return 1
	recovered_stable="$("$bud" content offered --channel stable "$3/channel.json" 2>/dev/null)" || recovered_stable=
	[ "$recovered_edge" = "$1" ] && [ "$recovered_stable" = "$2" ]
}

# publish_channel [force]: rebuilds channel.json from the tags, signs it,
# persists the signed pair in one recovery archive, then uploads both assets.
publish_channel() {
	edge="$(newest content-)"
	stable="$(newest content-stable-)"
	[ -n "$edge" ] || fail "there's no content release yet, so there's no channel to publish"

	reused=0
	if [ "${1:-}" != force ] && recover_channel "$edge" "$stable" "$tmp/recovered"; then
		cp "$tmp/recovered/channel.json" "$out/channel.json"
		cp "$tmp/recovered/channel.json.sig" "$out/channel.json.sig"
		reused=1
	fi

	if [ "$reused" = 0 ]; then
		if [ -n "$stable" ]; then
			"$bud" content channel --edge "$edge" --stable "$stable" --out "$out"
		else
			"$bud" content channel --edge "$edge" --out "$out"
		fi
		sh "$sign" "$out/channel.json" bud-channel
		"$bud" content offered --channel edge "$out/channel.json" >/dev/null
		if [ -n "$stable" ]; then
			"$bud" content offered --channel stable "$out/channel.json" >/dev/null
		fi
	fi
	tar -czf "$tmp/channel-recovery.tar.gz" -C "$out" channel.json channel.json.sig

	if git rev-parse -q --verify "refs/tags/$channel_tag" >/dev/null; then
		gh release upload "$channel_tag" "$tmp/channel-recovery.tar.gz" --clobber
	else
		gh release create "$channel_tag" --prerelease --target "$GITHUB_SHA" --title "Content channel" \
			--notes "The newest content version on each channel, for bud sync. Rebuilt by every content run; never install from here." \
			"$tmp/channel-recovery.tar.gz"
		# gh made the tag on GitHub; keep the local tag in sync for this run.
		git rev-parse -q --verify "refs/tags/$channel_tag" >/dev/null || git tag "$channel_tag" "$GITHUB_SHA"
	fi
	gh release upload "$channel_tag" "$out/channel.json" "$out/channel.json.sig" --clobber
	echo "channel: edge $edge, stable ${stable:-nothing yet}"
}

# pack_at VERSION REF DIR: packs content/ as it is at REF into DIR, signed;
# the pack is deterministic, so a rebuild gives the bytes first published
pack_at() {
	mkdir -p "$tmp/src-$1" "$3"
	git archive "$2" content | tar -x -C "$tmp/src-$1"
	"$bud" content pack --version "$1" --out "$3" "$tmp/src-$1/content"
	sh "$sign" "$3/content.json" bud-content
}

case "${1:-}" in
edge)
	[ $# -eq 1 ] || fail "usage: content-release.sh edge"
	latest="$(newest content-)"
	if [ -n "$(git tag --points-at "$GITHUB_SHA" -l 'content-[0-9]*')" ]; then
		echo "this commit is already released; republishing the channel"
	elif [ -n "$latest" ] && ! git merge-base --is-ancestor "content-$latest" "$GITHUB_SHA"; then
		echo "content-$latest is from a commit this one doesn't include, so this run is stale; republishing the channel only"
	else
		day="${BUD_CONTENT_DATE:-$(date -u +%Y.%m.%d)}"
		n="$(git tag -l "content-$day.*" | sed "s/^content-$day\.//" | grep -E '^[0-9]+$' | sort -n | tail -n1 || true)"
		version="$day.$((${n:-0} + 1))"
		pack_at "$version" "$GITHUB_SHA" "$out"
		gh release create "content-$version" --prerelease --target "$GITHUB_SHA" --title "Content $version" \
			--notes "Content $version, built from $GITHUB_SHA. Devices on edge get it with bud sync; it reaches stable when promoted." \
			"$out/content-$version.tar.gz" "$out/content.json" "$out/content.json.sig"
		# gh made the tag on GitHub; the same one here, for the channel below
		git rev-parse -q --verify "refs/tags/content-$version" >/dev/null || git tag "content-$version" "$GITHUB_SHA"
	fi
	publish_channel
	;;
promote)
	[ $# -eq 2 ] || fail "usage: content-release.sh promote <version>"
	version="$2"
	git rev-parse -q --verify "refs/tags/content-$version" >/dev/null ||
		fail "there's no content-$version release to promote; pick one from the content-* releases"
	# only what edge devices were actually offered, read from the signed channel
	edge="$(newest content-)"
	stable="$(newest content-stable-)"
	if recover_channel "$edge" "$stable" "$tmp/published"; then
		gh release upload "$channel_tag" "$tmp/recovery/channel-recovery.tar.gz" --clobber
		gh release upload "$channel_tag" "$tmp/published/channel.json" "$tmp/published/channel.json.sig" --clobber
	elif ! gh release download "$channel_tag" -p channel.json -p channel.json.sig -D "$tmp/published"; then
		fail "can't download the published channel.json; run the workflow with resign first, then promote"
	fi
	offered="$("$bud" content offered --channel edge "$tmp/published/channel.json")" ||
		fail "the published channel.json doesn't verify; run the workflow with resign first, then promote"
	[ "$offered" = "$version" ] ||
		fail "edge offers $offered, not $version: stable only takes the version edge offers now"
	if ! git rev-parse -q --verify "refs/tags/content-stable-$version" >/dev/null; then
		git tag "content-stable-$version" "content-$version^{commit}"
		git push origin "refs/tags/content-stable-$version"
	fi
	publish_channel
	;;
resign)
	[ $# -eq 1 ] || fail "usage: content-release.sh resign"
	for version in $(printf '%s\n%s\n' "$(newest content-)" "$(newest content-stable-)" | sort -u); do
		[ -n "$version" ] || continue
		pack_at "$version" "content-$version" "$out/$version"
		gh release upload "content-$version" "$out/$version/content-$version.tar.gz" \
			"$out/$version/content.json" "$out/$version/content.json.sig" --clobber
		echo "re-signed content-$version"
	done
	publish_channel force
	;;
*)
	fail "usage: content-release.sh edge | promote <version> | resign"
	;;
esac
