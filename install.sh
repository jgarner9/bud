#!/bin/sh
# Bud installer: fetches the bud release binary for this OS/arch, verifies its
# checksum, puts it in ~/.local/bin, and runs `bud install`.
#
#   curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh
#
# Options (pass after `sh -s --`):
#   --version <tag>   install a specific release, e.g. v0.1.0 (default: latest)
#   --bin-dir <dir>   where to put the binary (default: ~/.local/bin)
#   --no-install      only install the binary, skip `bud install`
#
# The same options can be set with BUD_VERSION, BUD_BIN_DIR and BUD_NO_INSTALL=1.

set -eu

REPO="jgarner9/bud"
VERSION="${BUD_VERSION:-latest}"
BIN_DIR="${BUD_BIN_DIR:-$HOME/.local/bin}"
NO_INSTALL="${BUD_NO_INSTALL:-0}"

say() { printf 'bud: %s\n' "$*"; }
die() { printf 'bud: error: %s\n' "$*" >&2; exit 1; }

usage() {
	cat <<'EOF'
usage: install.sh [--version <tag>] [--bin-dir <dir>] [--no-install]

  --version <tag>   install a specific release, e.g. v0.1.0 (default: latest)
  --bin-dir <dir>   where to put the binary (default: ~/.local/bin)
  --no-install      only install the binary, skip 'bud install'
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
	--bin-dir) [ $# -ge 2 ] || die "--bin-dir needs a value"; BIN_DIR="$2"; shift 2 ;;
	--no-install) NO_INSTALL=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) die "unknown option: $1" ;;
	esac
done

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

if [ "$VERSION" = latest ]; then
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

want="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || die "no checksum listed for $archive"
got="$(sha256 "$tmp/$archive")"
[ "$want" = "$got" ] || die "checksum mismatch for $archive (want $want, got $got)"
say "checksum ok"

tar -xzf "$tmp/$archive" -C "$tmp" bud
mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/bud" "$BIN_DIR/bud"
say "installed $BIN_DIR/bud"

case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*) say "note: $BIN_DIR is not on your PATH; add it to your shell profile" ;;
esac

if [ "$NO_INSTALL" = 1 ]; then
	say "skipping 'bud install' (run it yourself when ready)"
	exit 0
fi

say "running 'bud install'"
"$BIN_DIR/bud" install
