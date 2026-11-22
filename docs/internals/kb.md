# The knowledge base and its site

bud-kb is the private knowledge base: Markdown pages about Jason's projects and systems, written only by the curator and Jason (Suite D9, D20). Working memory (SQLite, `server/memory/`) is where agents write; the curator is the one bridge from it into bud-kb, and the KB site is a read-only view of bud-kb on the tailnet. Decisions: Suite D122 (the layout and the site), D123 (the curator), D124 (staleness and the review queue). `internal/kb` is the format, `internal/kbsite` the site.

## The page format (`internal/kb`)

The KB's tree is in `server/hugo/README.md`. A page is a JSON object (its front matter, which Hugo reads as it is) followed by the Markdown body:

| Key | Meaning |
| --- | --- |
| `title` | The page's title |
| `page_kind` | `project` or `system` (Hugo reserves `kind`); the page lives under `content/projects/` or `content/systems/` |
| `linear` | A project page's Linear project URL. Status lives there, never in the KB (D8) |
| `evergreen` | Long-standing facts: every section gets a refresh check at 12 months instead of being archived (D39) |
| `archived` | Set on pages under `content/archive/` |
| `sections` | Per `##` section, by its anchor: `referenced` (the last day the curator saw it referenced or confirmed), `evergreen`, and `status` (empty, `review` or `refresh`) |
| `facts` | Per curated fact, by `<section>/<name>`: `section`, `value`, `privacy` (the most private tier among its sources), `updated`, and `sources` (the memory records behind it: `record`, `source`, `date`) |

Keys bud doesn't know (Hugo's `weight`, say) are kept when it rewrites a page, and it writes the front matter sorted, so rewriting an unchanged page changes nothing. A section's anchor is `kb.Slug` of its heading, which matches Hugo's default anchors, so the site's heading hook finds the section's status. A curated fact is one bullet ending in `<!-- fact:<section>/<name> -->`; Hugo renders raw HTML as an omitted comment, so the marker never shows, and Jason can write any prose around the bullets. A fact's full key is `<projects|systems>/<page>#<section>/<name>` (`kb.ParseKey`), which names the page file, the section and the fact.

`kb.Repo` runs git in the clone, with `GIT_TERMINAL_PROMPT=0`. `Lock` is an flock on `.git/bud-kb.lock`, which the curator and the site's pull share. `Commit` commits as "Bud curator <curator@bud.invalid>", and `LastHumanEdit` skips that author, so any other commit to a page counts as Jason's edit. `Export` unpacks `content/` and `data/` of a commit from `git archive` and refuses anything that isn't a plain file, so a committed symlink can't pull a file from outside the KB into the site. The review queue is `data/review.json` (`kb.Queue`), whose items have stable ids (`kb.ItemID`) so a rerun finds what it queued.

## The site (`internal/kbsite`, `bud kb-site`)

`server/hugo/site/` is the Hugo config (`hugo.json`) and layouts, in Hugo 0.146's template system, embedded with `all:` (so `_markup/` and `_partials/` come along) and served by `bud.KBSite()`. A build:

1. Reads HEAD. Nothing happens when it's what the live site shows, or a commit that failed less than an hour ago (`--once` builds anyway).
2. Copies the layouts and `Export`s the commit into a temp source dir, then scans `content/` and `data/` for secret patterns (`secretRes`).
3. Runs `hugo --source <src> --destination <site-…>` into a fresh directory under `~/.local/state/bud/kb-site/`.
4. Checks the output: a home page, no secret patterns, and every internal `href`/`src` landing on a file the build made.
5. Points `current` at it with one rename, keeps the build before it and removes older ones, and writes `status.json` (the live commit, and the last failure with its error).

Any failure leaves `current` alone. `Handler` serves `current` with `http.FileServer`, which opens the link afresh on each request, so a swap is atomic for readers; it allows GET and HEAD only, with a strict content security policy and `noindex`. `Watch` pulls the clone with `--ff-only` under the lock, then builds, every `--interval` (a minute). `--listen` must be a loopback or tailnet IP (Suite D72); port 8799.

The layouts: `home.html` lists the review queue (`site.Data.review`) and the project and system pages; `section.html` lists a section's pages with their badges; `page.html` shows the page, its Linear link, an archive banner and a sources table from `facts`; `_markup/render-heading.html` adds a `review` or `refresh` badge to a heading whose section has that status.

## Tests

`internal/kb` tests the format round trip (unknown keys kept, output stable), sections around fenced code, fact bullets in place, in their section and in a new one, cutting and appending sections, keys, `Export` (committed pages only, no symlinks), `LastHumanEdit` and the queue. `internal/kbsite` builds a real git repo with a fake hugo: a commit, an edit and a `git revert` each reach the live site; a commit with a secret or a broken link fails and keeps the last good site; the handler's headers and methods. `TestRealHugo` renders a fixture KB with the real layouts when `hugo` is on PATH, and skips otherwise.
