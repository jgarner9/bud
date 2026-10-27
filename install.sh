#!/bin/sh
# Bud installer: fetches the bud release binary for this OS/arch, verifies the
# signature on checksums.txt and then the archive's checksum (D7), puts it in
# ~/.local/bin, and asks before running `bud install`.
#
#   curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh
#
# Options (pass after `sh -s --`):
#   --version <tag>      install a specific release, e.g. v0.1.0 (default: latest)
#   --bin-dir <dir>      where to put the binary (default: ~/.local/bin)
#   --profile <name>     full or minimal (default: full)
#   --yes                run `bud install` without asking
#   --no-install         only install the binary, skip `bud install`
#
# The same options can be set with BUD_VERSION, BUD_BIN_DIR, BUD_PROFILE,
# BUD_YES=1 and BUD_NO_INSTALL=1. BUD_RELEASE_URL downloads from somewhere
# other than GitHub (a mirror, or the tests); the signature still has to match.

set -eu

REPO="jgarner9/bud"

# The release key's public half (D7, D41). Empty until the key exists: until
# then only the checksum is checked (D43). Keep it in step with
# internal/updater.ReleaseKey
RELEASE_KEY=""
VERSION="${BUD_VERSION:-latest}"
BIN_DIR="${BUD_BIN_DIR:-$HOME/.local/bin}"
PROFILE="${BUD_PROFILE:-full}"
YES="${BUD_YES:-0}"
NO_INSTALL="${BUD_NO_INSTALL:-0}"

say() { printf 'bud: %s\n' "$*"; }
die() { printf 'bud: error: %s\n' "$*" >&2; exit 1; }

usage() {
	cat <<'EOF'
usage: install.sh [--version <tag>] [--bin-dir <dir>] [--profile <name>] [--yes] [--no-install]

  --version <tag>      install a specific release, e.g. v0.1.0 (default: latest)
  --bin-dir <dir>      where to put the binary (default: ~/.local/bin)
  --profile <name>     full or minimal (default: full)
  --yes                run 'bud install' without asking
  --no-install         only install the binary, skip 'bud install'
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
	--bin-dir) [ $# -ge 2 ] || die "--bin-dir needs a value"; BIN_DIR="$2"; shift 2 ;;
	--profile) [ $# -ge 2 ] || die "--profile needs a value"; PROFILE="$2"; shift 2 ;;
	--yes | -y) YES=1; shift ;;
	--no-install) NO_INSTALL=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) die "unknown option: $1" ;;
	esac
done

case "$PROFILE" in
full | minimal) ;;
*) die "unknown profile: $PROFILE (use full or minimal)" ;;
esac

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported OS: $(uname -s) (bud supports Linux and macOS)" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
else
	die "curl or wget is required"
fi

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	die "sha256sum or shasum is required to verify the download"
fi

if [ -n "${BUD_RELEASE_URL:-}" ]; then
	base="$BUD_RELEASE_URL"
elif [ "$VERSION" = latest ]; then
	base="https://github.com/$REPO/releases/latest/download"
else
	base="https://github.com/$REPO/releases/download/$VERSION"
fi
archive="bud_${os}_${arch}.tar.gz"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/bud.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

say "downloading $archive ($VERSION)"
fetch "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "download failed: $base/checksums.txt"

if [ -n "$RELEASE_KEY" ]; then
	command -v ssh-keygen >/dev/null 2>&1 || die "ssh-keygen (OpenSSH) is required to verify the release signature"
	fetch "$base/checksums.txt.sig" "$tmp/checksums.txt.sig" || die "download failed: $base/checksums.txt.sig"
	printf 'bud-release %s\n' "$RELEASE_KEY" >"$tmp/allowed_signers"
	ssh-keygen -Y verify -f "$tmp/allowed_signers" -I bud-release -n bud-release \
		-s "$tmp/checksums.txt.sig" <"$tmp/checksums.txt" >/dev/null 2>&1 ||
		die "checksums.txt isn't signed by bud's release key; not installing"
	say "signature ok"
fi

want="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || die "no checksum listed for $archive"
got="$(sha256 "$tmp/$archive")"
[ "$want" = "$got" ] || die "checksum mismatch for $archive (want $want, got $got)"
say "checksum ok"

tar -xzf "$tmp/$archive" -C "$tmp" bud
mkdir -p "$BIN_DIR"
BIN_DIR="$(cd "$BIN_DIR" && pwd)"
install -m 0755 "$tmp/bud" "$BIN_DIR/bud"
say "installed $BIN_DIR/bud"

# record that this script put the binary there: bud uninstall only removes a
# binary it finds here, and self-update only replaces one (D20)
case "${XDG_STATE_HOME:-}" in
/*) state_dir="$XDG_STATE_HOME/bud" ;;
*) state_dir="$HOME/.local/state/bud" ;;
esac
mkdir -p "$state_dir"
json_path="$(printf '%s' "$BIN_DIR/bud" | sed 's/\\/\\\\/g; s/"/\\"/g')"
printf '{\n  "method": "install.sh",\n  "path": "%s"\n}\n' "$json_path" >"$state_dir/install.json"

case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*) say "note: $BIN_DIR is not on your PATH; add it to your shell profile" ;;
esac

if [ "$NO_INSTALL" = 1 ]; then
	say "skipping 'bud install' (run it yourself when ready)"
	exit 0
fi

# under `curl | sh` stdin is this script, so the question goes to the terminal directly
if [ "$YES" != 1 ]; then
	if ! (: </dev/tty) 2>/dev/null; then
		say "no terminal to ask on, skipping 'bud install' (rerun with --yes, or run it yourself)"
		exit 0
	fi
	printf "bud: run 'bud install' now? It checks system packages and sets up the Neovim config [Y/n] " >/dev/tty
	read -r answer </dev/tty || answer=n
	case "$answer" in
	"" | y | Y | yes | YES) ;;
	*)
		say "skipping 'bud install' (run it yourself when ready)"
		exit 0
		;;
	esac
fi

say "running 'bud install'"
if [ "$YES" = 1 ]; then
	"$BIN_DIR/bud" --profile "$PROFILE" install --yes
else
	# bud install asks its own questions, and under `curl | sh` its stdin would be this script
	"$BIN_DIR/bud" --profile "$PROFILE" install </dev/tty
fi
