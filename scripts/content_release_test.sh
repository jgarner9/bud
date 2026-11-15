#!/bin/sh
# Runs content-release.sh against throwaway repos with fake gh, bud and
# signer, and checks the releases it makes (Suite D60, D61).
#   sh scripts/content_release_test.sh   (just content-release-test)

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-content-release-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM
mkdir -p "$tmp/bin"

# fake gh: a release is a dir under $tmp/releases; creating one tags the
# target in the repo, as GitHub would. Every call is logged to $tmp/gh.log
cat >"$tmp/bin/gh" <<FAKE
#!/bin/sh
echo "gh \$*" >>"$tmp/gh.log"
[ "\$1" = release ] || exit 1
cmd="\$2" tag="\$3"
shift 3
case "\$cmd" in
create)
	[ -e "$tmp/gh-create-fails" ] && exit 1
	target=HEAD
	for a in "\$@"; do
		[ "\$prev" = --target ] && target="\$a"
		prev="\$a"
	done
	git tag "\$tag" "\$target"
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

# fake bud: pack writes the two files; channel writes its flags as
# channel.json; offered prints $tmp/offered, failing without it. Every call
# is logged to $tmp/bud.log
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
channel) tail -n1 "$tmp/bud.log" >"\$out/channel.json" ;;
offered) cat "$tmp/offered" ;;
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

commit() {
	echo "$1" >"$tmp/repo/content/AGENTS.md"
	git -C "$tmp/repo" add content
	git -C "$tmp/repo" -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m "$1"
}

# repo TAG...: a fresh repo at $tmp/repo, with a bare origin, and two
# commits; each TAG is an earlier release on the first commit, with a dir
reset() {
	rm -rf "$tmp/repo" "$tmp/origin" "$tmp/releases" "$tmp/offered" "$tmp"/gh-*-fails
	mkdir -p "$tmp/repo/content" "$tmp/releases"
	git init -q --bare "$tmp/origin"
	git -C "$tmp/repo" init -q
	git -C "$tmp/repo" remote add origin "$tmp/origin"
	commit c1
	for t in "$@"; do
		git -C "$tmp/repo" tag "$t"
		mkdir -p "$tmp/releases/$t"
		if [ "$t" = content-channel ]; then
			echo published >"$tmp/releases/$t/channel.json"
			echo published >"$tmp/releases/$t/channel.json.sig"
		fi
	done
	commit c2
	: >"$tmp/gh.log"
	: >"$tmp/bud.log"
}

# release NAME WANT ARGS...: runs content-release.sh ARGS in the repo at
# $sha (HEAD by default); WANT is ok or fail
release() {
	name="$1" want="$2"
	shift 2
	if (cd "$tmp/repo" && PATH="$tmp/bin:$PATH" BUD=bud SIGN="$tmp/bin/sign" BUD_CONTENT_DATE=2026.11.15 \
		GITHUB_SHA="${sha:-$(git rev-parse HEAD)}" OUT="$tmp/repo/dist" sh "$here/content-release.sh" "$@" >"$tmp/log" 2>&1); then
		got=ok
	else
		got=fail
	fi
	[ "$got" = "$want" ] || fail "$name: got $got, want $want ($(cat "$tmp/log"))"
}

# has NAME FILE REGEX: FILE holds a line matching REGEX; hasnt the opposite
has() { grep -Eq -- "$3" "$2" || fail "$1: no line matching '$3' in $(basename "$2"): $(cat "$2")"; }
hasnt() { ! grep -Eq -- "$3" "$2" || fail "$1: a line matches '$3' in $(basename "$2"): $(cat "$2")"; }

# every release this script makes is a prerelease, or install.sh's latest breaks
prerelease_only() {
	if grep '^gh release create' "$tmp/gh.log" | grep -qv -- '--prerelease'; then
		fail "$1: a release that isn't a prerelease: $(cat "$tmp/gh.log")"
	fi
}

reset
release "the first release" ok edge
has "the first release" "$tmp/bud.log" '^bud content pack --version 2026\.11\.15\.1 '
has "the first release" "$tmp/bud.log" '^bud content channel --edge 2026\.11\.15\.1 --out'
has "the first release" "$tmp/gh.log" '^gh release create content-2026\.11\.15\.1 .*content-2026\.11\.15\.1\.tar\.gz .*content\.json .*content\.json\.sig$'
has "the first release" "$tmp/gh.log" '^gh release create content-channel .*channel\.json .*channel\.json\.sig$'
has "the first release" "$tmp/releases/content-2026.11.15.1/content.json.sig" '^bud-content$'
has "the first release" "$tmp/releases/content-channel/channel.json.sig" '^bud-channel$'
prerelease_only "the first release"

reset content-2026.11.15.1 content-stable-2026.11.15.1 content-channel
release "a second cut that day" ok edge
has "a second cut that day" "$tmp/bud.log" '^bud content pack --version 2026\.11\.15\.2 '
has "a second cut that day" "$tmp/bud.log" '^bud content channel --edge 2026\.11\.15\.2 --stable 2026\.11\.15\.1 '
has "a second cut that day" "$tmp/gh.log" '^gh release upload content-channel .*channel\.json .*channel\.json\.sig --clobber$'
hasnt "a second cut that day" "$tmp/gh.log" '^gh release create content-channel'
prerelease_only "a second cut that day"

reset content-2026.11.15.9 content-2026.11.14.12 content-channel
release "10 comes after 9" ok edge
has "10 comes after 9" "$tmp/bud.log" '^bud content pack --version 2026\.11\.15\.10 '

reset content-2026.11.15.1 content-channel
release "a re-run, first time" ok edge
: >"$tmp/gh.log"
: >"$tmp/bud.log"
release "a re-run on a released commit" ok edge
hasnt "a re-run on a released commit" "$tmp/bud.log" '^bud content pack'
hasnt "a re-run on a released commit" "$tmp/gh.log" '^gh release create'
has "a re-run on a released commit" "$tmp/bud.log" '^bud content channel --edge 2026\.11\.15\.2 '

# a newer commit is released, then a retried run of an older one comes in
reset content-channel
old="$(git -C "$tmp/repo" rev-parse HEAD~1)"
release "the newer commit" ok edge
: >"$tmp/gh.log"
: >"$tmp/bud.log"
sha="$old" release "a stale retry" ok edge
sha=
hasnt "a stale retry" "$tmp/bud.log" '^bud content pack'
hasnt "a stale retry" "$tmp/gh.log" '^gh release create'
has "a stale retry" "$tmp/bud.log" '^bud content channel --edge 2026\.11\.15\.1 '

# an earlier run stopped after one channel file: the next run rebuilds both
# without reading either
reset content-2026.11.15.1 content-channel
echo half-written >"$tmp/releases/content-channel/channel.json"
release "a channel left half uploaded" ok edge
hasnt "a channel left half uploaded" "$tmp/gh.log" '^gh release download'
has "a channel left half uploaded" "$tmp/releases/content-channel/channel.json" 'channel --edge 2026\.11\.15\.2'

reset content-2026.11.15.1 content-channel
echo 2026.11.15.1 >"$tmp/offered"
release "promoting what edge offers" ok promote 2026.11.15.1
has "promoting what edge offers" "$tmp/bud.log" '^bud content offered --channel edge .*/channel\.json$'
has "promoting what edge offers" "$tmp/bud.log" '^bud content channel --edge 2026\.11\.15\.1 --stable 2026\.11\.15\.1 '
hasnt "promoting what edge offers" "$tmp/bud.log" '^bud content pack'
git -C "$tmp/origin" rev-parse -q --verify refs/tags/content-stable-2026.11.15.1 >/dev/null ||
	fail "promoting what edge offers: content-stable-2026.11.15.1 wasn't pushed"

reset content-2026.11.15.1 content-2026.11.15.2 content-channel
echo 2026.11.15.2 >"$tmp/offered"
release "promoting a version edge no longer offers" fail promote 2026.11.15.1
hasnt "promoting a version edge no longer offers" "$tmp/gh.log" 'upload'

reset content-2026.11.15.1 content-channel
release "promoting when the channel doesn't verify" fail promote 2026.11.15.1
hasnt "promoting when the channel doesn't verify" "$tmp/gh.log" 'upload'

reset content-2026.11.15.1 content-channel
echo 2026.11.15.2 >"$tmp/offered"
release "promoting a version never released" fail promote 2026.11.15.2
hasnt "promoting a version never released" "$tmp/gh.log" 'upload'

reset content-2026.11.15.1 content-2026.11.15.2 content-stable-2026.11.15.1 content-channel
release "resigning" ok resign
has "resigning" "$tmp/bud.log" '^bud content pack --version 2026\.11\.15\.1 '
has "resigning" "$tmp/bud.log" '^bud content pack --version 2026\.11\.15\.2 '
has "resigning" "$tmp/gh.log" '^gh release upload content-2026\.11\.15\.1 .*content\.json\.sig --clobber$'
has "resigning" "$tmp/gh.log" '^gh release upload content-2026\.11\.15\.2 .*content\.json\.sig --clobber$'
has "resigning" "$tmp/bud.log" '^bud content channel --edge 2026\.11\.15\.2 --stable 2026\.11\.15\.1 '
hasnt "resigning" "$tmp/gh.log" '^gh release create'

reset
touch "$tmp/gh-create-fails"
release "a release that won't create" fail edge
hasnt "a release that won't create" "$tmp/bud.log" 'channel'

reset
release "no mode" fail
release "an unknown mode" fail stable
release "promote with no version" fail promote

if [ "$failed" = 0 ]; then echo "content_release_test: ok"; fi
exit "$failed"
