# Handoff: KB curator and Hugo view (JGA-311, JGA-312)

Thread "Build KB curator and Hugo view", stopped 2026-11-22 08:32 UTC when usage ran low. Design and decisions: `/mnt/project-files/architecture/kb-curator-design.md` (Suite D122 to D124, allocated by the coordinator; D125+ belong to other threads).

## Done

- Branch `claude/project-thread-xiqxs2`, draft PR https://github.com/jgarner9/bud/pull/107 (commit f7adf78). JGA-312's code:
  - `internal/kb`: page format (JSON front matter + Markdown), sections, fact bullets with `<!-- fact:key -->` markers, fact keys, review queue (`data/review.json`), git helpers (`Repo.Lock` flock, `Commit` as "Bud curator", `LastHumanEdit`, `Export` of committed `content/` and `data/`).
  - `internal/kbsite` + `internal/cli/kbsite.go`: `bud kb-site` builds HEAD with Hugo, checks secrets and internal links, swaps a `current` symlink only on success, serves on 127.0.0.1:8799 or a tailnet IP, pulls every minute.
  - `server/hugo/`: layouts for Hugo 0.146+ (`site/`, embedded via `all:server/hugo/site` in `embed.go`, served by `bud.KBSite()`), unit, env example, README.
  - `internal/paths`: `KB()`, `KBSite()`, `CuratorState()`.
  - Docs: `docs/internals/kb.md`, `docs/commands.md` row, AGENTS.md table row, `server/README.md` (dir row, port 8799).
- Tests pass: `go test ./internal/kb/ ./internal/kbsite/ ./internal/cli/ ./internal/paths/ .`, vet and gofmt clean, leak-check ok.

## Not done

1. **Render the real layouts.** Hugo isn't in the cloud sandbox (downloading it was refused), so `TestRealHugo` (`internal/kbsite/site_test.go`) has never run. Run it with hugo on PATH and fix any template errors. Likely spots: `home.html`'s `site.GetPage` path built from `.page`, `index` on `.Page.Params.sections` in `_markup/render-heading.html`, and whether the archive section lists nested pages.
2. **Then the PR round:** mark #107 ready, request Copilot review, fix, leave the fix-summary comment, merge (Jason's standing auto-merge rule).
3. **The curator (JGA-311), not started in code.** Plan is in the design doc. Pieces:
   - `memory.Client.Export` (the service's `export` action already exists: approved records plus `record_references`).
   - `internal/curator`: state file outside the KB (`paths.CuratorState`), extraction through the registry's `curator` role on `local: true` providers only (never the router), promotion at two distinct (source, day) occurrences or `bud curator promote <id>`, contradictions to the queue, supported changes rewritten, staleness (6 months review, 12 months archive to `content/archive/...` or refresh for evergreen), one commit per change with evidence, pull before and push after. Default tiers P1,P2 because bud-kb's remote is GitHub.
   - `bud curator run|promote|resolve <id> keep|take|confirm`, a systemd timer in `server/curator/`, a curator token in the "Bud Memory" clients policy with `export` only.
   - Tests with a fake clock and fake model: promotion, contradiction, refresh, archive, rerun idempotence, revert not redone.
   - Add the curator half to `docs/internals/kb.md`.
4. **Rollout steps** in the newest revisit file in /mnt/project-files (not added yet): install Hugo 0.146+ on the home server, clone bud-kb to `~/.local/share/bud/kb` with a push-capable deploy key, `bud kb-site --once`, then the unit with `BUD_KB_LISTEN` set to the tailnet IP:8799. Curator steps once it exists.
5. **Linear:** JGA-311 and JGA-312 are still Backlog; move JGA-312 to In Progress/Done as the PR goes.

## Gotchas

- `go:embed` of a directory drops `_`-prefixed names, hence `all:server/hugo/site`.
- Hugo 0.146 template system: layouts at `layouts/` root, `_partials/`, `_markup/`, `home.html` (not `index.html`).
- The branch can't be force-pushed. After #107 squash-merges, restart the branch from main (merge the old head with `-s ours` if the push is refused) before the curator PR.
- Jason's question for later (recorded on recommendation in D123): P0 facts stay out of the KB until bud-kb's remote lives on the home server.
- Don't touch `internal/approval` or the router: another thread owns them.
