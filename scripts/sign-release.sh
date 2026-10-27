#!/bin/sh
# Signs a release's checksums.txt with the release key (D7, D41) and checks
# the signature against the public keys install.sh trusts, so a secret that
# matches none of them fails the release instead of every install.
#
#   BUD_RELEASE_KEY="$(cat key)" scripts/sign-release.sh dist/checksums.txt
#
# Writes checksums.txt.sig next to it. The key is an unencrypted OpenSSH
# private key; it only ever lives in a temp file.

set -eu

checksums="${1:?usage: sign-release.sh <checksums.txt>}"
: "${BUD_RELEASE_KEY:?BUD_RELEASE_KEY must hold the release private key}"
here="$(cd "$(dirname "$0")" && pwd)"

# install.sh's RELEASE_KEYS block: the lines between RELEASE_KEYS=" and "
trusted() {
	awk '/^RELEASE_KEYS="$/ { f = 1; next } f && /^"$/ { exit } f && NF { print "bud-release " $0 }' "$here/../install.sh"
}
[ -n "$(trusted)" ] || {
	echo "sign-release: install.sh trusts no RELEASE_KEYS yet; pin the public key first" >&2
	exit 1
}

tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-sign.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM
umask 077
printf '%s\n' "$BUD_RELEASE_KEY" >"$tmp/key"

rm -f "$checksums.sig"
ssh-keygen -q -Y sign -n bud-release -f "$tmp/key" "$checksums" </dev/null

trusted >"$tmp/allowed_signers"
ssh-keygen -Y verify -f "$tmp/allowed_signers" -I bud-release -n bud-release \
	-s "$checksums.sig" <"$checksums" >/dev/null || {
	echo "sign-release: the signing key isn't one of install.sh's RELEASE_KEYS" >&2
	exit 1
}
echo "signed $checksums"
