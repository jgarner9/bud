#!/bin/sh
# Rotates the release signing key (D7, D41, D44) without stranding machines
# that already have bud. Every bud trusts a list of keys, so it runs in two
# phases, a release or more apart:
#
#   scripts/rotate-release-key.sh start [--vault NAME] [--now]
#       makes a new key, keeps it in 1Password, and adds its public half to
#       the keys bud trusts (install.sh's RELEASE_KEYS, updater.ReleaseKeys).
#       Releases are still signed with the old key: ship one (or more) so
#       machines learn the new key as they update.
#
#   scripts/rotate-release-key.sh finish --item TITLE [--old-item TITLE] [--vault NAME]
#       after the grace period (30 days is a good default): signs from now on
#       with the new key (the BUD_RELEASE_KEY Actions secret), drops the old
#       public key from both lists, and archives the old 1Password item.
#       A machine that didn't update during the grace period has to reinstall.
#
# --now does both at once, for a leaked key: every machine has to reinstall.
#
# Needs ssh-keygen, the 1Password CLI (op, signed in), gh (logged in, with
# access to the repo's secrets) and a clean git tree. Run it by hand, never in
# CI. The private key only lives in a temp dir that's removed on exit, and
# goes from 1Password to the secret through a pipe.

set -eu

repo="${BUD_REPO:-$(cd "$(dirname "$0")/.." && pwd)}"
install_sh="$repo/install.sh"
keys_go="$repo/internal/updater/sshsig.go"
vault="${BUD_KEY_VAULT:-Private}"

die() {
	echo "rotate-release-key: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 isn't installed${2:+: $2}"
}

# trusted prints the public keys install.sh trusts, one per line
trusted() {
	awk '/^RELEASE_KEYS="$/ { f = 1; next } f && /^"$/ { exit } f && NF { print }' "$install_sh"
}

# add_key pins a public key in both lists, after the ones there
add_key() {
	key="$1"
	tmp="$install_sh.tmp"
	awk -v key="$key" '/^RELEASE_KEYS="$/ { f = 1 } f && /^"$/ { print key; f = 0 } { print }' "$install_sh" >"$tmp" && mv "$tmp" "$install_sh"
	chmod +x "$install_sh"
	tmp="$keys_go.tmp"
	awk -v key="$key" '/^var ReleaseKeys = \[\]string\{$/ { f = 1 } f && /^\}$/ { printf "\t\"%s\",\n", key; f = 0 } { print }' "$keys_go" >"$tmp" && mv "$tmp" "$keys_go"
}

# drop_key unpins a public key from both lists
drop_key() {
	key="$1"
	tmp="$install_sh.tmp"
	grep -vxF "$key" "$install_sh" >"$tmp" && mv "$tmp" "$install_sh"
	chmod +x "$install_sh"
	tmp="$keys_go.tmp"
	grep -vxF "	\"$key\"," "$keys_go" >"$tmp" && mv "$tmp" "$keys_go"
}

clean_tree() {
	[ -z "$(git -C "$repo" status --porcelain)" ] || die "the git tree has changes; commit or stash them first"
}

commit() {
	git -C "$repo" add install.sh internal/updater/sshsig.go
	git -C "$repo" commit -q -m "$1"
	echo "Committed: $1 (push it and cut a release)"
}

start() {
	now=0
	while [ $# -gt 0 ]; do
		case "$1" in
		--vault) vault="$2"; shift 2 ;;
		--now) now=1; shift ;;
		*) die "start: unknown option $1" ;;
		esac
	done
	need ssh-keygen
	need op "https://developer.1password.com/docs/cli"
	clean_tree
	op whoami >/dev/null 2>&1 || die "op isn't signed in: run op signin"

	date="$(date +%Y-%m-%d)"
	title="bud release key $date"
	tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-rotate.XXXXXX")"
	trap 'rm -rf "$tmp"' EXIT INT TERM
	umask 077
	# unencrypted, because the release workflow signs with it unattended;
	# 1Password is what keeps it safe
	ssh-keygen -q -t ed25519 -N "" -C "bud-release-$date" -f "$tmp/key"
	pub="$(cut -d' ' -f1,2 "$tmp/key.pub")"
	trusted | grep -qxF "$pub" && die "that key is already trusted"

	op document create "$tmp/key" --title "$title" --vault "$vault" --file-name bud-release-key >/dev/null
	# 1Password has it now, so it goes from disk straight away
	rm -rf "$tmp"
	echo "Stored the private key in 1Password: \"$title\" (vault $vault)"
	add_key "$pub"
	echo "Trusting the new key alongside the old: $pub"

	if [ "$now" = 1 ]; then
		echo "--now: switching to it straight away"
		finish --item "$title" --vault "$vault" --no-commit-check
		return
	fi
	commit "chore: trust the next release key ($date)"
	cat <<MSG

Next:
  1. Push and release. Machines that update now learn the new key.
  2. In 30 days or so: scripts/rotate-release-key.sh finish --item "$title" --old-item "<the old key's 1Password title>"
MSG
}

finish() {
	item="" old_item="" check=1
	while [ $# -gt 0 ]; do
		case "$1" in
		--item) item="$2"; shift 2 ;;
		--old-item) old_item="$2"; shift 2 ;;
		--vault) vault="$2"; shift 2 ;;
		--no-commit-check) check=0; shift ;;
		*) die "finish: unknown option $1" ;;
		esac
	done
	[ -n "$item" ] || die "finish needs --item, the new key's 1Password title"
	need ssh-keygen
	need op "https://developer.1password.com/docs/cli"
	need gh "https://cli.github.com"
	[ "$check" = 0 ] || clean_tree
	op whoami >/dev/null 2>&1 || die "op isn't signed in: run op signin"

	# the new key's public half, from the private key, in a private temp dir
	# removed on exit (ssh-keygen won't read a key from a pipe)
	ktmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-rotate.XXXXXX")"
	trap 'rm -rf "$ktmp"' EXIT INT TERM
	(umask 077 && op document get "$item" --vault "$vault" >"$ktmp/key")
	new_pub="$(ssh-keygen -y -f "$ktmp/key" | cut -d' ' -f1,2)"
	rm -f "$ktmp/key"
	trusted | grep -qxF "$new_pub" || die "\"$item\" isn't one of the trusted keys; run start first"

	op document get "$item" --vault "$vault" | gh secret set BUD_RELEASE_KEY --repo "$(git -C "$repo" remote get-url origin)"
	echo "Releases are signed with \"$item\" from now on."

	trusted | while IFS= read -r key; do
		if [ "$key" != "$new_pub" ]; then
			drop_key "$key"
			echo "No longer trusting: $key"
		fi
	done
	if [ -n "$old_item" ]; then
		op item delete "$old_item" --vault "$vault" --archive
		echo "Archived \"$old_item\" in 1Password."
	fi
	commit "chore: sign releases with the new release key, drop the old one"
}

case "${1:-}" in
start) shift; start "$@" ;;
finish) shift; finish "$@" ;;
*) die "usage: rotate-release-key.sh start [--vault NAME] [--now] | finish --item TITLE [--old-item TITLE]" ;;
esac
