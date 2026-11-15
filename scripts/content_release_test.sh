#!/bin/sh
# Runs content-release.sh against throwaway repos with fake gh, bud and
# signer, and checks the releases it makes (Suite D60, D61).
#   sh scripts/content_release_test.sh   (just content-release-test)

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-content-release-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM
mkdir -p "$tmp/bin"

# fake gh: a release is a dir under $tmp/releases; creating one tags HEAD in
# the repo as GitHub would. Every call is logged to $tmp/gh.log
cat >"$tmp/bin/gh" <<FAKE
#!/bin/sh
echo "gh \$*" >>"$tmp/gh.log"
[ "\$1" = release ] || exit 1
cmd="\$2" tag="\$3"
shift 3
case "\$cmd" in
create)
	[ -e "$tmp/gh-create-fails" ] && exit 1
	git tag "\$tag" HEAD
	mkdir -p "$tmp/releases/\$tag"
	for a in "\$@"; do [ -f "\$a" ] && cp "\$a" "$tmp/releases/\$tag/"; done
	true
	;;
upload)
	for a in "\$@"; do [ -f "\$a" ] && cp "\$a" "$tmp/releases/\$tag/"; done
	true
	;;
download)
	[ -e "$tmp/gh-download-fails" ] && exit 1
	dir=.
	while [ \$# -gt 0 ]; do
		if [ "\$1" = -D ]; then dir="\$2"; fi
		shift
	done
	mkdir -p "\$dir"
	cp "$tmp/releases/\$tag/"* "\$dir/"
	;;
*) exit 1 ;;
esac
FAKE

# fake bud: pack writes the two files; channel writes its arguments as
# channel.json. Both are logged to $tmp/bud.log
cat >"$tmp/bin/bud" <<FAKE
#!/bin/sh
echo "bud \$*" >>"$tmp/bud.log"
shift
cmd="\$1"
shift
out=. version=
while [ \$# -gt 0 ]; do
	case "\$1" in
	--out) out="\$2" ;;
	--version) version="\$2" ;;
	esac
	shift
done
case "\$cmd" in
pack)
	echo bundle >"\$out/content-\$version.tar.gz"
	echo "\$version" >"\$out/content.json"
	;;
channel) echo "channel" >"\$out/channel.json" ;;
esac
FAKE

# fake signer: the signature is the namespace it was asked for
cat >"$tmp/bin/sign" <<'FAKE'
#!/bin/sh
echo "$2" >"$1.sig"
FAKE
chmod +x "$tmp/bin/gh" "$tmp/bin/bud" "$tmp/bin/sign"

failed=0
fail() {
	echo "content_release_test: $*" >&2
	failed=1
}

# repo TAG...: a fresh repo at $tmp/repo with two commits; each TAG names a
# tag on the first commit (an earlier release), and a release dir for it
repo() {
	rm -rf "$tmp/repo" "$tmp/releases" "$tmp/gh.log" "$tmp/bud.log" "$tmp"/gh-*-fails
	mkdir -p "$tmp/repo/content" "$tmp/releases"
	git -C "$tmp/repo" init -q
	for i in 1 2; do
		echo "$i" >"$tmp/repo/content/AGENTS.md"
		git -C "$tmp/repo" add content
		git -C "$tmp/repo" -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m "c$i"
		[ "$i" = 1 ] && for t in "$@"; do
			git -C "$tmp/repo" tag "$t"
			mkdir -p "$tmp/releases/$t"
			[ "$t" = content-channel ] && echo old >"$tmp/releases/$t/channel.json" && echo old >"$tmp/releases/$t/channel.json.sig"
		done
	done
	: >"$tmp/gh.log"
	: >"$tmp/bud.log"
}

# release NAME WANT ARGS...: runs content-release.sh ARGS in the repo; WANT
# is ok or fail
release() {
	name="$1" want="$2"
	shift 2
	if (cd "$tmp/repo" && PATH="$tmp/bin:$PATH" BUD=bud SIGN="$tmp/bin/sign" BUD_CONTENT_DATE=2026.11.15 \
		GITHUB_SHA="$(git rev-parse HEAD)" OUT="$tmp/repo/dist" sh "$here/content-release.sh" "$@" >"$tmp/log" 2>&1); then
		got=ok
	else
		got=fail
	fi
	[ "$got" = "$want" ] || fail "$name: got $got, want $want ($(cat "$tmp/log"))"
}

# has NAME FILE LINE: FILE holds a line matching the regex LINE
has() { grep -Eq -- "$3" "$2" || fail "$1: no line matching '$3' in $(basename "$2"): $(cat "$2")"; }
hasnt() { ! grep -Eq -- "$3" "$2" || fail "$1: a line matches '$3' in $(basename "$2"): $(cat "$2")"; }

# every release this script makes is a prerelease, or install.sh's latest breaks
prerelease_only() {
	if grep '^gh release create' "$tmp/gh.log" | grep -qv -- '--prerelease'; then
		fail "$1: a release that isn't a prerelease: $(cat "$tmp/gh.log")"
	fi
}

repo
release "the first release" ok edge
has "the first release" "$tmp/bud.log" '^bud content pack --version 2026\.11\.15\.1 '
has "the first release" "$tmp/bud.log" '^bud content channel --new --set edge=2026\.11\.15\.1 '
has "the first release" "$tmp/gh.log" '^gh release create content-2026\.11\.15\.1 .*content-2026\.11\.15\.1\.tar\.gz .*content\.json .*content\.json\.sig$'
has "the first release" "$tmp/gh.log" '^gh release create content-channel .*channel\.json .*channel\.json\.sig$'
has "the first release" "$tmp/releases/content-2026.11.15.1/content.json.sig" '^bud-content$'
has "the first release" "$tmp/releases/content-channel/channel.json.sig" '^bud-channel$'
prerelease_only "the first release"

repo content-2026.11.15.1 content-channel
release "a second cut that day" ok edge
has "a second cut that day" "$tmp/bud.log" '^bud content pack --version 2026\.11\.15\.2 '
has "a second cut that day" "$tmp/bud.log" '^bud content channel --from .*/channel\.json --set edge=2026\.11\.15\.2 '
has "a second cut that day" "$tmp/gh.log" '^gh release upload content-channel .*channel\.json .*channel\.json\.sig --clobber$'
hasnt "a second cut that day" "$tmp/gh.log" '^gh release create content-channel'
prerelease_only "a second cut that day"

repo content-2026.11.15.9 content-2026.11.14.12 content-channel
release "10 comes after 9" ok edge
has "10 comes after 9" "$tmp/bud.log" '^bud content pack --version 2026\.11\.15\.10 '

repo content-2026.11.15.1 content-channel
release "a re-run, first time" ok edge
: >"$tmp/gh.log"
: >"$tmp/bud.log"
release "a re-run on a released commit" ok edge
hasnt "a re-run on a released commit" "$tmp/bud.log" '^bud content pack'
hasnt "a re-run on a released commit" "$tmp/gh.log" '^gh release create'
has "a re-run on a released commit" "$tmp/bud.log" '^bud content channel --from .* --set edge=2026\.11\.15\.2 '

repo content-2026.11.15.1 content-channel
touch "$tmp/gh-download-fails"
release "a channel.json that won't download" fail edge
hasnt "a channel.json that won't download" "$tmp/bud.log" '--new'
hasnt "a channel.json that won't download" "$tmp/gh.log" '^gh release (upload|create) content-channel'

repo content-2026.11.15.1 content-channel
release "promoting a release" ok promote 2026.11.15.1
has "promoting a release" "$tmp/bud.log" '^bud content channel --from .* --set stable=2026\.11\.15\.1 '
hasnt "promoting a release" "$tmp/bud.log" '^bud content pack'
has "promoting a release" "$tmp/gh.log" '^gh release upload content-channel '

repo content-2026.11.15.1 content-channel
release "promoting a version never released" fail promote 2026.11.15.2
hasnt "promoting a version never released" "$tmp/gh.log" 'upload'

repo
touch "$tmp/gh-create-fails"
release "a release that won't create" fail edge
hasnt "a release that won't create" "$tmp/bud.log" 'channel'

repo
release "no mode" fail
release "an unknown mode" fail stable
release "promote with no version" fail promote

if [ "$failed" = 0 ]; then echo "content_release_test: ok"; fi
exit "$failed"
