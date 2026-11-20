#!/bin/sh
# Run synthetic regression probes against an already installed pinned checkout.
set -eu
if [ "$#" -ne 1 ]; then
  echo "usage: $0 <OpenCode 2.3.2 source checkout>" >&2
  exit 2
fi
repo=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
source_dir=$(CDPATH='' cd -- "$1" && pwd)
commit=$(git -C "$source_dir" rev-parse HEAD)
if [ "$commit" != c0ad18b402eb2827fc1ee71c171b1177b3fc02d1 ]; then
  echo "expected OpenCode 2.3.2 commit c0ad18b402eb2827fc1ee71c171b1177b3fc02d1, got $commit" >&2
  exit 1
fi
if ! git -C "$source_dir" diff --quiet HEAD --; then
  echo 'the pinned checkout has tracked changes; use a clean disposable checkout' >&2
  exit 1
fi
command -v bun >/dev/null || { echo 'Bun 1.4.4 is required' >&2; exit 1; }
[ "$(bun --version)" = 1.4.4 ] || { echo 'Bun 1.4.4 is required' >&2; exit 1; }
core=$source_dir/packages/core
[ -d "$source_dir/node_modules" ] || { echo 'install the pinned checkout dependencies first' >&2; exit 1; }
dispatch=
permission=
shell=
plugin=
cleanup() {
  [ -z "$dispatch" ] || rm -f "$dispatch"
  [ -z "$permission" ] || rm -f "$permission"
  [ -z "$shell" ] || rm -f "$shell"
  [ -z "$plugin" ] || rm -f "$plugin"
}
trap cleanup EXIT HUP INT TERM
dispatch=$(mktemp "$core/test/jga348-dispatch-XXXXXX.test.ts")
permission=$(mktemp "$core/test/jga348-permission-XXXXXX.test.ts")
shell=$(mktemp "$core/test/jga348-shell-XXXXXX.test.ts")
plugin=$(mktemp "$core/test/plugin/jga348-plugin-XXXXXX.test.ts")
cat "$core/test/tool-registry.test.ts" "$repo/scripts/testdata/opencode-jga348/dispatch.ts" > "$dispatch"
cat "$core/test/permission.test.ts" "$repo/scripts/testdata/opencode-jga348/permission.ts" > "$permission"
cat "$core/test/tool-shell.test.ts" "$repo/scripts/testdata/opencode-jga348/shell.ts" > "$shell"
cat "$core/test/plugin/promise-tool.test.ts" "$repo/scripts/testdata/opencode-jga348/plugin.ts" > "$plugin"
cd "$core"
env -i PATH="$PATH" bun run script/test.ts "$dispatch" "$permission" "$shell" "$plugin" -t JGA-348
