#!/bin/sh
# Runs rotate-release-key.sh start and finish against a throwaway copy of the
# repo, with fake op and gh, and checks both key lists at each step.
#   sh scripts/rotate_test.sh   (just rotate-test)

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-rotate-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

repo="$tmp/repo"
mkdir -p "$repo/internal/updater" "$tmp/bin" "$tmp/vault"
cp "$root/install.sh" "$repo/install.sh"
cp "$root/internal/updater/sshsig.go" "$repo/internal/updater/sshsig.go"
git -C "$repo" init -q
git -C "$repo" remote add origin https://example.com/bud.git
git -C "$repo" -c user.name=t -c user.email=t@t add .
git -C "$repo" -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m init

# fake op: documents live in $tmp/vault, by title
cat >"$tmp/bin/op" <<FAKE
#!/bin/sh
case "\$1 \$2" in
"whoami "*) exit 0 ;;
"document create") cp "\$3" "$tmp/vault/\$5" ;;
"document get") cat "$tmp/vault/\$3" ;;
"item delete") echo "\$3" >>"$tmp/archived" ;;
esac
FAKE
# fake gh: keeps what it was given as the secret
cat >"$tmp/bin/gh" <<FAKE
#!/bin/sh
cat >"$tmp/secret"
FAKE
chmod +x "$tmp/bin/op" "$tmp/bin/gh"

fail() {
	echo "rotate_test: $*" >&2
	exit 1
}
keys() {
	awk '/^RELEASE_KEYS="$/ { f = 1; next } f && /^"$/ { exit } f && NF' "$repo/install.sh" | wc -l | tr -d ' '
}
go_keys() {
	awk '/^var ReleaseKeys = \[\]string\{$/ { f = 1; next } f && /^\}$/ { exit } f' "$repo/internal/updater/sshsig.go" | wc -l | tr -d ' '
}

old="$(awk '/^RELEASE_KEYS="$/ { f = 1; next } f && NF { print; exit }' "$repo/install.sh")"
run() {
	env PATH="$tmp/bin:$PATH" BUD_REPO="$repo" GIT_CONFIG_COUNT=3 \
		GIT_CONFIG_KEY_0=user.name GIT_CONFIG_VALUE_0=t \
		GIT_CONFIG_KEY_1=user.email GIT_CONFIG_VALUE_1=t@t \
		GIT_CONFIG_KEY_2=commit.gpgsign GIT_CONFIG_VALUE_2=false \
		sh "$here/rotate-release-key.sh" "$@" >/dev/null
}

run start
if [ "$(keys)" != 2 ] || [ "$(go_keys)" != 2 ]; then
	fail "after start: $(keys) keys in install.sh, $(go_keys) in Go, want 2 each"
fi
[ -x "$repo/install.sh" ] || fail "install.sh lost its execute bit"
[ -z "$(git -C "$repo" status --porcelain)" ] || fail "start left changes uncommitted"
title="$(ls "$tmp/vault")"
[ -n "$title" ] || fail "no key stored in 1Password"
ls "$tmp"/bud-rotate.* >/dev/null 2>&1 && fail "the private key was left in a temp dir"

run finish --item "$title" --old-item "bud release key old"
if [ "$(keys)" != 1 ] || [ "$(go_keys)" != 1 ]; then
	fail "after finish: $(keys) and $(go_keys) keys, want 1 each"
fi
grep -qxF "$old" "$repo/install.sh" && fail "the old key is still trusted"
cmp -s "$tmp/secret" "$tmp/vault/$title" || fail "the secret isn't the new private key"
grep -qx "bud release key old" "$tmp/archived" || fail "the old 1Password item wasn't archived"
new="$(ssh-keygen -y -f "$tmp/vault/$title" | cut -d' ' -f1,2)"
grep -qxF "$new" "$repo/install.sh" || fail "install.sh doesn't trust the new key"
grep -qF "\"$new\"," "$repo/internal/updater/sshsig.go" || fail "the Go list doesn't trust the new key"

echo "rotate-test: ok"
