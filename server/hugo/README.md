# The KB site

`bud kb-site` renders bud-kb, the private knowledge base, with Hugo and serves it on the tailnet (Suite D53, D122). The site is a view of the KB, never a second copy: it shows the pages, navigation by kind, the sources behind each curated fact, a badge on sections waiting on review, a banner on archived pages and the review queue on the home page. `site/` is the Hugo config and layouts, embedded in the bud binary; the pages come only from bud-kb. How it works is in [docs/internals/kb.md](../../docs/internals/kb.md).

## What it guarantees

- **Only commits publish.** Each build reads `content/` and `data/` from `git archive HEAD`, never the working tree, so an uncommitted edit (the curator's or yours) never shows.
- **A bad commit keeps the last good site.** The new build goes into a fresh directory and is swapped in with one rename only when Hugo succeeds, every internal link resolves and nothing in the pages or the output looks like a secret (private keys, GitHub, OpenAI, 1Password service-account, Slack, AWS or Linear tokens). A failed commit is logged and retried an hour later or on the next commit; `status.json` in `~/.local/state/bud/kb-site/` says what's live and what failed.
- **Tailnet only.** `--listen` must be a loopback or tailnet IP, like every other server here (Suite D72), and the pages say `noindex`. The handler serves GET and HEAD only, with a strict content security policy.

## Setup on the home server

1. Install Hugo 0.146 or newer (the layouts use its template system). The standard edition is enough; nothing here needs the extended one. On Debian or Ubuntu, take the `.deb` from Hugo's GitHub releases rather than the distro package, which can be older.
2. Clone bud-kb to `~/.local/share/bud/kb` with a deploy key that can push, since the curator pushes its commits from the same clone. To keep P0 facts in the house, give the clone a remote on the home server instead (see `server/curator/README.md`).
3. Build once by hand to check it: `bud kb-site --once`.
4. Copy `kb-site.env.example` to `~/.config/bud-server/kb-site.env`, set `BUD_KB_LISTEN` to the tailnet IP and port 8799, and install `bud-kb-site.service` as the comment at its top says.

## The KB's layout

```
content/_index.md                  the home page's intro
content/projects/<slug>.md         one page per project, linking its Linear project for status (D8)
content/systems/<slug>.md          one page per system: a host, a service, a setup
content/archive/<dir>/<slug>.md    sections the curator archived, kept for good
data/review.json                   the review queue
```

Each page is Markdown with JSON front matter; the fields are in [docs/internals/kb.md](../../docs/internals/kb.md). Pages hold reference documentation only, never tasks: those stay in Linear.
