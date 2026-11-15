#!/bin/sh
# Fails when a tracked file names something personal (Suite D21, D59): this
# repo is public, and personal details belong in the private overlay.
#
#   sh scripts/leak-check.sh            # check the repo it's run from
#
# Two kinds of check:
#
# 1. Shapes, always on. A Tailscale name (*.ts.net) other than the
#    placeholders example, your-tailnet, tailnet and tail; an op:// reference
#    whose vault isn't the placeholder "vault" or "example"; and a home
#    directory (/Users/x, /home/x) for anyone but the placeholder users below.
# 2. Your own strings, when BUD_LEAK_PATTERNS is set: one extended regex per
#    line (real hostnames, the tailnet, your username, and so on). CI reads it
#    from a secret, since the list itself is personal.
#
# A line that has to name one anyway carries "leak-check: allow" and is
# skipped, so every exception is visible where it's made.

set -eu

placeholder_users='a|b|j|me|you|user|runner|example|name|someone'
placeholder_tailnets='example|your-tailnet|tailnet|tail'
placeholder_vaults='vault|example'

root="$(git rev-parse --show-toplevel)"
cd "$root"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud-leak.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

# every tracked text file, minus this script and its test, which name the shapes
git ls-files -z | xargs -0 grep -IlE '.' 2>/dev/null |
	grep -vxE 'scripts/leak-check\.sh|scripts/leak_check_test\.sh' >"$tmp/files" || true

# scan prints "file:line: what: match" for each match of $1 not excused by
# $2; with $4 set the match itself is left out of the report
scan() {
	pattern="$1" allowed="$2" what="$3" hide="${4:-}"
	while IFS= read -r f; do
		grep -noE -- "$pattern" "$f" 2>/dev/null | while IFS= read -r hit; do
			line="${hit%%:*}"
			match="${hit#*:}"
			if [ -n "$allowed" ] && printf '%s\n' "$match" | grep -qE -- "$allowed"; then
				continue
			fi
			if sed -n "${line}p" "$f" | grep -q 'leak-check: allow'; then
				continue
			fi
			if [ -n "$hide" ]; then match='(not shown)'; fi
			printf '%s:%s: %s: %s\n' "$f" "$line" "$what" "$match"
		done
	done <"$tmp/files"
}

{
	scan '[A-Za-z0-9.-]*\.ts\.net' "^\\.ts\\.net\$|(^|\\.)($placeholder_tailnets)\\.ts\\.net\$" 'a Tailscale name'
	scan 'op://[^/[:space:]"`)<>]+' "^op://($placeholder_vaults)\$" 'a 1Password reference'
	scan '/(Users|home)/[A-Za-z0-9._-]+' "^/(Users|home)/($placeholder_users)\$" 'a home directory'

	if [ -n "${BUD_LEAK_PATTERNS:-}" ]; then
		printf '%s\n' "$BUD_LEAK_PATTERNS" | grep -v '^[[:space:]]*$' >"$tmp/patterns" || true
		while IFS= read -r p; do
			# the pattern is personal, so the report shows only where it matched
			scan "$p" '' 'one of your BUD_LEAK_PATTERNS' hide
		done <"$tmp/patterns"
	fi
} >"$tmp/hits"

if [ -s "$tmp/hits" ]; then
	sort -u "$tmp/hits" >&2
	n="$(sort -u "$tmp/hits" | wc -l | tr -d ' ')"
	echo "leak-check: $n personal detail(s) in a public file. Move them to the private overlay, or use a placeholder (example.ts.net, op://vault/item/field, /home/you); if a line must name one, add \"leak-check: allow\" to it" >&2
	exit 1
fi

if [ -z "${BUD_LEAK_PATTERNS:-}" ]; then
	echo "leak-check: ok (shapes only; set BUD_LEAK_PATTERNS to check your own strings too)"
else
	echo "leak-check: ok"
fi
