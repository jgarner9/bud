# KB curator and Hugo view (JGA-311, JGA-312)

Written 2026-11-22 by the thread "Build KB curator and Hugo view". Decisions Suite D122 to D124 were taken on recommendation under Jason's standing rule (2026-11-21); he can reverse any of them.

## What gets built

- `internal/kb`: the bud-kb page format, parsing and writing pages, the review queue file and git helpers for the KB clone.
- `bud kb-site` (`internal/kbsite`, `server/hugo/`): builds the Hugo site from the KB's committed tree, checks it, swaps it in only when it passes, and serves it on a tailnet address (port 8799).
- `bud curator` (`internal/curator`, `server/curator/`): the nightly job that promotes approved working-memory records into pages, queues contradictions and refresh checks, flags and archives stale sections, and commits each change with its evidence.

## The KB layout (D122)

bud-kb holds pages only:

```
content/_index.md
content/projects/<slug>.md     one page per project, links to its Linear project
content/systems/<slug>.md      one page per system (a host, a service, a setup)
content/archive/<kind>/<slug>.md   archived sections, same front matter shape
data/review.json               the review queue (contradictions, refresh checks)
```

A page is Markdown with **JSON front matter** (Hugo reads it natively, and Go reads it with the standard library, D1/D2):

```json
{
  "title": "the home server",
  "kind": "system",
  "linear": "",
  "evergreen": true,
  "sections": {
    "router": { "referenced": "2026-11-22", "evergreen": false, "status": "" }
  },
  "facts": {
    "router/port": { "section": "router", "value": "8795", "privacy": "P1",
      "updated": "2026-11-22", "sources": [{ "record": 12, "source": "hermes", "date": "2026-11-21" }] }
  }
}
```

The body is ordinary Markdown with `## Section` headings. A curated fact is one bullet ending in `<!-- fact:router/port -->`; Hugo drops raw HTML, so the marker never shows. Jason can write any prose around the bullets. A section `status` is empty, `review` (6 months without a reference) or `refresh` (evergreen at 12 months). Linear holds status; a project page only links to it (D8).

The site is a view: navigation by kind, a badge on sections that need review, the sources under each page, a banner on archived pages and the review queue on the home page. It builds from `git archive HEAD`, never the working tree, so only committed content publishes. A build that fails, has a broken internal link or matches a secret pattern keeps the last good site.

## The curator (D123)

- **Model.** It calls the registry's `curator` role directly, on local providers only (`local: true`), never through the router, so a P0 record can't reach a cloud model through a fallback. With Qwen down, extraction stops for the night and the records wait; staleness work, which needs no model, still runs.
- **What it reads.** Approved records and their references, through the memory service's `export` action with a curator token. By default only P1 and P2 records are promoted, because bud-kb's remote is a GitHub repo and P0 must stay in the house; `--tiers P0,P1,P2` turns P0 on once the KB's remote lives at home.
- **Promotion.** The model turns each new record into facts with a key (`<kind>/<page>#<section>/<name>`) and a short value. A fact is promoted when two distinct approved records agree on it, counting each source and day once, so one session repeating itself doesn't count twice. `bud curator promote <record id>` is Jason's doc-commit: it promotes that record's facts on the next run with one occurrence.
- **Changes.** A promoted fact whose key already holds another value is a contradiction. The new value replaces the old only when it has two independent occurrences, all newer than the page's evidence; otherwise it goes to the review queue.
- **State** (seen records, candidates, evidence used) lives in `~/.local/state/bud/curator.json`, outside the KB, so when Jason reverts a curator commit, the next run doesn't redo it from the same evidence.
- **Writes.** KB edits are do-alone (D10), so no approval. One commit per change, by "Bud curator", with the record ids, sources and dates in the message. It pulls with `--ff-only` first and pushes after when the clone has a remote; a failed push is retried next run.

## Staleness and the review queue (D124)

- A section's clock is the newest of: a fact in it confirmed or added, a memory reference to one of its source records, and Jason's own commit to the page.
- 6 months with nothing: `status: review` (a badge on the site). 12 months: the section moves to `content/archive/...` in its own commit, so `git revert` or `bud curator restore <page>#<section>` brings it back. Evergreen sections (or an evergreen page) get `status: refresh` and a refresh item on the queue instead.
- The review queue is `data/review.json` in bud-kb, shown on the site. Jason clears it with `bud curator resolve <id> keep|take|confirm`, or by editing the page himself. No Signal prompts, since the approvals audit found Signal asks too much; a Signal or chat path can come later.

## Rollout (on the home server, Jason)

See the newest revisit file in /mnt/project-files for the steps.
