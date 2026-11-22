# The knowledge base and its site

bud-kb is the private knowledge base: Markdown pages about Jason's projects and systems, written only by the curator and Jason (Suite D9, D20). Working memory (SQLite, `server/memory/`) is where agents write; the curator is the one bridge from it into bud-kb, and the KB site is a read-only view of bud-kb on the tailnet. Decisions: Suite D122 (the layout and the site), D123 (the curator), D124 (staleness and the review queue). `internal/kb` is the format, `internal/curator` the curator, `internal/kbsite` the site.

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

## The curator (`internal/curator`, `bud curator`)

`bud curator run` is the nightly job (`server/curator/`). It takes the clone's lock, refuses a working tree with uncommitted changes, pulls with `--ff-only`, then:

1. **Reads memory.** `memory.Client.Export` returns every approved record within the curator token's tiers, plus `record_references`. The curator keeps only records in `--tiers` (P1 and P2 by default, Suite D123), even if the token's policy is wider.
2. **Extracts.** Each record it hasn't read goes to the `Extractor` once, with the KB's existing pages and facts as `key = value` lines so the model reuses keys. `ChatExtractor` posts to `/chat/completions` on each of `LocalModels(reg)` in turn: the registry's `curator` role, keeping members on `local: true` providers without a key, never the router. A model that can't be reached (connection error, 502, 503) moves on to the next; when none answers, `ErrNoModel` stops extraction for the night and the rest wait. Any other failure counts against the record, which is skipped after three. `parseFacts` drops a fact whose key doesn't parse, whose value is empty or over 200 characters, or which `kbsite.HasSecret` flags.
3. **Promotes.** Every value seen for a key is a `Candidate` with its `Occurrence`s (record, source, day, tier). A candidate acts when it has at least two independent occurrences (distinct source and day, `independent`), or one of its records is in `Promote` (`bud curator promote <id>`), and at least one occurrence it hasn't acted on before (`State.Used`). Then: no fact yet adds the bullet, the section if needed and the front matter; the same value confirms it (sources merged, `updated` and the section's `referenced` set to today); another value replaces it only when every occurrence is newer than the fact's sources, its `updated` day and Jason's last commit to the page (`supported`), and otherwise opens a `contradiction` on the queue. A fact whose bullet Jason deleted starts over.
4. **Staleness** (Suite D124). A section's clock is the newest of its `referenced`, its facts' `updated`, the last reference to any record behind its facts, and Jason's last commit to the page (or the page's last commit, when nothing else applies). Under 6 months clears a status; 6 months sets `review`; 12 months archives the section (`moveSection` into `content/archive/<dir>/<slug>.md`, facts and front matter with it, in a commit whose message names `bud curator restore`), or for an evergreen page or section sets `refresh` and queues a refresh item. Before that, `pruneQueue` drops contradictions whose fact Jason has since changed or removed.
5. **Pushes**, when the clone has an origin. A failed push leaves the commits for the next run.

Every change is its own commit by the curator, with the evidence (record, source, day, tier) in the message. The state file (`paths.CuratorState`, mode 0600) holds `seen`, `failed`, `candidates`, `used` and `promote`, and is saved after each record and each commit. Because `used` lives outside the KB, `git revert` of a curator commit sticks: the fact comes back only when a record outside the used evidence states it.

`Resolve` answers an item: `keep` or `confirm` sets the section's `referenced` to today and clears its status, and `take` writes the proposed value and sources over the page's, keeping the old fact's tier. `Restore` moves an archived section back, removing the archive page when it's left empty. Both commit and push under the lock.

## Tests

`internal/kb` tests the format round trip (unknown keys kept, output stable), sections around fenced code, fact bullets in place, in their section and in a new one, cutting and appending sections, keys, `Export` (committed pages only, no symlinks), `LastHumanEdit` and the queue. `internal/kbsite` builds a real git repo with a fake hugo: a commit, an edit and a `git revert` each reach the live site; a commit with a secret or a broken link fails and keeps the last good site; the handler's headers and methods. `TestRealHugo` renders a fixture KB with the real layouts and `TestRealHugoError` checks Hugo's own message comes through, when `hugo` is on PATH; both skip otherwise, and CI has no hugo. `internal/curator` runs against a real git clone with a fake memory service, a fake model and a fake clock: promotion at two independent occurrences and not one, manual promotion, tiers, confirmation, a contradiction queued and taken, a supported change, a revert that stays reverted, the model down, a record that keeps failing, review, archive and restore, an evergreen refresh confirmed, Jason's edit settling a contradiction, and a dirty clone refused. `model_test.go` covers `LocalModels`, `parseFacts` and the fallthrough between endpoints.
