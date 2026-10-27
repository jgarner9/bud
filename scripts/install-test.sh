#!/bin/sh
# Tests install.sh's checks against a fake release on disk, signed with a
# throwaway key (D41): with no key pinned, the checksum alone; with one, a
# good signature, someone else's, a missing one, and a changed archive.
# Needs curl (for file:// URLs) and ssh-keygen.

set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-install-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; *) arch=arm64 ;; esac
archive="bud_${os}_${arch}.tar.gz"

ssh-keygen -q -t ed25519 -N '' -C test -f "$tmp/key"
ssh-keygen -q -t ed25519 -N '' -C other -f "$tmp/other"

# release builds a fake release in $1: archive, checksums.txt and, with a key
# file as $2, its signature
release() {
	mkdir -p "$1/src"
	printf '#!/bin/sh\necho bud-test\n' >"$1/src/bud"
	chmod +x "$1/src/bud"
	tar -czf "$1/$archive" -C "$1/src" bud
	(cd "$1" && { sha256sum "$archive" 2>/dev/null || shasum -a 256 "$archive"; } >checksums.txt)
	if [ -n "${2:-}" ]; then
		ssh-keygen -q -Y sign -n bud-release -f "$2" "$1/checksums.txt"
	fi
}

# script writes install.sh with key pinned as $1 (empty for none), in place
# of whatever the real one pins
script() {
	sed "s|^RELEASE_KEY=\".*\"\$|RELEASE_KEY=\"$1\"|" "$root/install.sh" >"$tmp/install.sh"
}

pass=0
fail=0
# run NAME WANT RELEASE_DIR: WANT is ok or fail
run() {
	home="$tmp/home-$1"
	if BUD_RELEASE_URL="file://$3" BUD_BIN_DIR="$home/bin" XDG_STATE_HOME="$home/state" \
		sh "$tmp/install.sh" --no-install >"$tmp/out" 2>&1 && [ "$("$home/bin/bud")" = bud-test ]; then
		got=ok
	else
		got=fail
	fi
	if [ "$got" = "$2" ]; then
		pass=$((pass + 1))
	else
		fail=$((fail + 1))
		echo "install-test: $1: want $2, got $got" >&2
		cat "$tmp/out" >&2
	fi
}

pub="$(cut -d' ' -f1-2 "$tmp/key.pub")"

release "$tmp/unsigned"
release "$tmp/signed" "$tmp/key"
release "$tmp/other-signed" "$tmp/other"
release "$tmp/tampered" "$tmp/key"
printf 'junk' >>"$tmp/tampered/$archive"

script ""
run no-key-unsigned ok "$tmp/unsigned"
run no-key-tampered fail "$tmp/tampered"

script "$pub"
run signed ok "$tmp/signed"
run unsigned fail "$tmp/unsigned"
run other-key fail "$tmp/other-signed"
run tampered fail "$tmp/tampered"

# sign-release.sh: signs with the key install.sh pins, refuses another
cp "$tmp/unsigned/checksums.txt" "$tmp/sums"
mkdir -p "$tmp/repo/scripts"
cp "$tmp/install.sh" "$tmp/repo/install.sh"
cp "$root/scripts/sign-release.sh" "$tmp/repo/scripts/"
if BUD_RELEASE_KEY="$(cat "$tmp/key")" sh "$tmp/repo/scripts/sign-release.sh" "$tmp/sums" >/dev/null &&
	! BUD_RELEASE_KEY="$(cat "$tmp/other")" sh "$tmp/repo/scripts/sign-release.sh" "$tmp/sums" 2>/dev/null; then
	pass=$((pass + 1))
else
	fail=$((fail + 1))
	echo "install-test: sign-release.sh should sign with the pinned key and refuse another" >&2
fi

echo "install-test: $pass passed, $fail failed"
[ "$fail" = 0 ]
