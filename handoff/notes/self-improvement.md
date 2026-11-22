# Handoff: self-improvement proposals and weekly review (JGA-321)

Thread "Build self-improvement proposals and weekly review". Resumed 2026-11-22 16:22Z: tests, the timer unit, the docs and the ops count are done, and PR #109 is open (Copilot round pending). Remaining: the improve:digest prompt title (revisit item 19) and the home server rollout (revisit item 21).

## Done

- **Design and decisions.** `/mnt/project-files/architecture/jga-321-self-improvement-design.md` covers the whole loop and Suite D125 to D129, taken on recommendation (the coordinator reserved those numbers; the curator thread has D122 to D124). They aren't in the architecture HTML yet.
- **`internal/improve`** (compiles, vet clean, its test passes):
  - `proposal.go` holds the proposal type, its validation (public proposals may change only `content/`, there are no git or CI paths, and the leak gate runs) and a fingerprint, so a change Jason turned down never comes back. It also has the state file (`paths.ImproveState`, `~/.local/state/bud/improve.json`, mode 0600) and `Revert`.
  - `leaks.go` is a Go port of `scripts/leak-check.sh`'s patterns plus `BUD_LEAK_PATTERNS`.
  - `measure.go` takes daily snapshots of router spend and doctor counts, and compares the 7 days before a merge with the 7 days after.
  - `github.go` is a stdlib REST client. It reads files, opens a draft PR on a new branch with the `bud-proposal` label, reads a PR, compares commits and reads the latest release.
  - `scan.go` is the weekly scan. It runs `registry.Compare` on every provider and turns fact drift into a proposal through `PatchFacts`, aimed at the overlay's registry when the model is defined there. Unknown models become candidates mapped to roles by their needs. It also compares Hermes's latest release with bud-signal's pin.
  - `loop.go` runs a daily `Round`: measure, scan weekly, follow the PRs (merged or closed), track edge and stable through `channel.json` and `compare`, check after 8 days, and propose a revert on a regression. When the digest is due (Sunday 10:00 local by default), it sends one Signal prompt through `approval.Store.Dispatch` and `SocketReview`. A yes opens the PRs, his no drops the proposals, and no answer means it asks again the next day. A quiet week asks nothing.
  - `digest.go` renders the digest text and the PR body.
  - `loop_test.go` is the end-to-end fixture JGA-321 asks for. It covers a scan, a no, no re-proposal, an unanswered digest and then a yes, the PR's contents and label, the merge, the edge rollout, a regression, and the revert in the next digest.
- **`internal/cli/improve.go`** adds `bud improve run|list|show|add`. `run` needs `BUD_IMPROVE_GITHUB_TOKEN` (through op run) and the reviewer socket.
- **`internal/paths`** gains `ImproveState()`.

## Not done (next steps, in order)

1. **Digest prompt title.** `internal/approval/socket.go`'s `titles` map needs an entry for `improve:digest` with `whole: ["digest"]` and `hide: ["proposals"]`. Without it the prompt shows only 160 characters. I asked the "Build approvals and privacy routing changes" thread to add it (or to say my PR may), since that area is theirs, and the auto-mode classifier refused my own edit there. Check their answer or their merged PRs before you add it.
2. **Tests.** There are none yet for leaks, Validate, `daily`'s month rollover, `nextSlot`, `Fit`, a stale proposal (the file changed on main), overlay proposals, or the CLI (`githubRepo`, `add`).
3. **Server unit.** Add `server/improve/` with `bud-improve.service` (Type=oneshot, TimeoutStartSec=2h, op run with `improve.env`), `bud-improve.timer` (daily at about 09:30) and `improve.env.example` (`BUD_IMPROVE_GITHUB_TOKEN=op://vault/item/token`, optionally `BUD_LEAK_PATTERNS`), plus a README.
4. **Docs.** Add `docs/internals/improve.md`, a row in AGENTS.md's "Where the detail is" table, `docs/commands.md` entries for `bud improve`, and a `site/_data/nav.yml` line only if a user doc is added. `TestDocsCoverEveryKey` doesn't apply, since no config keys were added.
5. **Ops count.** Optionally add a `proposals` count by status to the ops `reviews` tool.
6. **Then** run `just check`, open the PR (attribution lines from the session context, assignee jgarner9, review request), go through the Copilot round, leave the fix-summary comment and merge. After that, add the Suite D125 to D129 rows to the architecture doc, write a project memory note, and add the rollout steps below to the newest revisit file (`/mnt/project-files/revisit-20261122T081850-a7a8.md` at the time of writing).

## Rollout steps for the home server (for the revisit file)

- Create a fine-grained GitHub PAT with contents and pull-requests write on jgarner9/bud and the overlay, store it in 1Password, and put its `op://` reference in `~/.config/bud-server/improve.env`.
- Create the `bud-proposal` label in both repos. Without it the PR still opens, just unlabelled.
- Install and enable `bud-improve.timer`.
- Rule for Claude threads: never merge a `bud-proposal` PR. Jason merges those himself. Save this to project memory.

## Gotchas

- The overlay has no channels, so a merged overlay proposal reaches every device at its next sync. The PR body says so.
- Spend numbers are month-to-date in the router ledger. `daily()` turns them into per-day deltas and needs consecutive days, or the 1st of a month.
- Doctor counts are measured on the server, which runs stable, so a public change shows up there only after promotion.
- The digest runs as a blocking oneshot for up to an hour while the prompt waits. The state lock is released while it waits.
