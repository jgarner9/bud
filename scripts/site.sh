#!/bin/sh
# Assembles the docs site's Jekyll source: site/ (the landing page, layout and
# styles) plus the user docs, docs/*.md, copied in as they are. The pages are
# the repo's own Markdown, so there is no second copy to keep in step.
# docs/internals/ is for working on bud and stays on GitHub.
# Fails when a page in docs/ is missing from the sidebar (site/_data/nav.yml).
# Run from the repo root: sh scripts/site.sh <out-dir>
# (.github/workflows/pages.yml builds <out-dir> with GitHub Pages' Jekyll.)

set -eu

out="${1:?usage: sh scripts/site.sh <out-dir>}"

missing=0
for page in docs/*.md; do
	name="$(basename "$page" .md)"
	if ! grep -q "file: $name," site/_data/nav.yml; then
		echo "site: docs/$name.md is not in site/_data/nav.yml: add it to the sidebar" >&2
		missing=1
	fi
done
[ "$missing" -eq 0 ] || exit 1

rm -rf "$out"
mkdir -p "$out/docs"
cp -R site/. "$out/"
cp docs/*.md "$out/docs/"
echo "site: assembled in $out"
