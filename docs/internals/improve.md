# The self-improvement loop

`bud improve` (`internal/improve`) is Bud's self-improvement loop (Suite D47, D125 to D129, JGA-321). Bud notices and proposes, and Jason decides: no runtime edits its own models, permissions or locked skills, and nothing the loop writes reaches a device until Jason merges a pull request himself. It runs on the home server as the daily timer in `server/improve/`.

## A round

`bud improve run` does one day's work under an exclusive lock on the state file, `improve.json` in bud's state dir (`paths.ImproveState`, mode 0600, since overlay proposals hold the overlay's files):

1. **Measure.** It takes a snapshot of the day: this month's router spend per model and the top-up total, read with `router.ReadSpend` from `router-spend.json`, plus the fail and warn counts of `bud doctor --quick`, which runs offline as the ops tool does. One snapshot is kept per UTC day, for 60 days. A ledger that can't be read marks the day `no_spend`.
2. **Scan, once a week** (more than 6.5 days since the last one; Suite D129). It reads `content/env/registry.jsonc` from jgarner9/bud's default branch and `env/registry.jsonc` from the overlay's, merges them, and calls `registry.Compare` against every provider's `GET /models`, as `bud registry refresh` does.
   - Facts that drifted (context, output, price, inputs, caps) become one proposal per file. The file is patched with `registry.PatchFacts`, which keeps comments and layout, and targets the overlay when the model is defined there.
   - Listed models the registry lacks become candidates, each with the roles whose `needs` it meets.
   - Models no longer listed are named.
   - Hermes's latest release is compared with bud-signal's `requires_hermes` pin.

   A newer fact refresh replaces one still waiting for the same file. No model is called; the only requests that leave the house are the model lists and GitHub's API.
3. **Follow.** Each open pull request is read. A merged one records its merge time and commit. One closed without merging drops the change for good. For a merged public change, the signed `channel.json` (`syncer.FetchChannel`) and GitHub's compare API record the first edge (`content-<v>`) and stable (`content-stable-<v>`) versions that hold the merge commit.
4. **Check** (Suite D128). Eight days after a merge, the daily averages of the 7 days before it and the 7 days after it (the merge day in neither) are compared for each criterion. A metric is worse when after > before × (1 + `max_rise`) + `slack`. Each side needs at least 3 days of numbers; after 3 weeks without them the change is marked `unmeasured`. A worse number marks it `regressed` and queues a revert proposal (`Revert`: each file's before and after swapped) for the next digest.
5. **Digest** (Suite D126), when it is due: see below.

Spend in the ledger is month to date, so a day's spend is its change since the day before (or the month's total on the 1st). A day without the day before's snapshot has no spend value. Doctor counts are levels. The server runs stable, so its doctor counts show a public change only after it is promoted.

## Proposals

A proposal (`Proposal`) holds:
- its title and evidence (what was seen);
- the files it changes, with whole before and after contents (`null` before means a new file and `null` after a deleted one);
- the criteria that say it worked;
- its target: `public`, meaning jgarner9/bud, where only paths under `content/` are allowed, or `overlay`.

Paths must be clean and relative, and nothing under `.git` or `.github`. A public proposal is refused when its title, evidence, paths or new contents hold a personal detail. `Leaks` ports `scripts/leak-check.sh`'s shapes (a tailnet name, a real `op://` vault, a home directory, with the same placeholders allowed) and adds `BUD_LEAK_PATTERNS`. Criteria metrics are `spend.total`, `spend.topup`, `spend.model:<id>` (dollars a day), `doctor.fail` and `doctor.warn`. A registry facts refresh checks top-up spend (at most 25% over the week before, plus $0.10 a day) and doctor failures (no more than before).

The fingerprint of a change is a hash of its target and each file's path and contents. A change that is already queued isn't queued twice. One that was dropped, by Jason's no or by closing its PR, is never queued again. Statuses run `new` → `open` → `merged` → `passed`, `regressed` or `unmeasured`; the other endings are `denied`, `closed` and `stale` (a file on the default branch is no longer what the proposal was written against, so its PR wasn't opened).

`bud improve add <file>` queues one written by hand. Agents can't write proposals yet; an ops tool for that is future work.

## The weekly digest

The digest is due from `--digest-day` at `--digest-hour` local time (Sunday 10:00 by default) once a week has passed since the last answer, and it is asked at most once a day. A week with nothing new counts as answered without a prompt. Nothing new means no `new` proposals, and no regression or promotion that an answered digest hasn't already reported.

`Render` writes the digest in this order:
1. The new proposals, numbered, each with its evidence cut to 400 characters, its files and its criteria. Past half the budget, the rest are left for `bud improve list`.
2. What got worse after merging.
3. The changes that held their numbers on edge, followed by the one promote command for the version edge offers now: `gh workflow run content.yml -f mode=promote -f version=<v>`. Promotion stays Jason's explicit step (Suite D127).
4. The proposals that went stale.
5. The landscape scan: up to 8 candidates, the models that are gone, runtimes with a newer release, and providers whose lists couldn't be read.

`Fit` then cuts the text until the prompt fits `approval.MaxSummary`.

It is asked through `approval.Store.Dispatch` with `approval.SocketReview`, like memory-review. The action is `improve:digest`, the target `github`, and the arguments `{"digest": text, "proposals": [ids]}`. The scope is fixed (`improve.Scope`), and the prompt is withdrawn a minute before it expires.
- **Yes:** each shown proposal is opened as a draft pull request. Before that, every file on the default branch is checked against the proposal's before contents, and a mismatch makes it stale. The change goes on a new branch `bud-proposal/<id>`, one commit per file through the contents API, labelled `bud-proposal`. The PR body has the evidence, the criteria and the rollout. Jason approves each change by merging its PR himself. Agent sessions never merge a `bud-proposal` PR.
- **His no** (`approval.ErrDeclined`): the shown proposals become `denied`, and their changes are dropped for good.
- **No answer, or the plugin stopping:** nothing changes, and it is asked again the next day.

Before the prompt can show the digest whole, `internal/approval`'s `titles` map needs an entry for `improve:digest` with `whole: ["digest"]` and `hide: ["proposals"]`. Until it has one, the prompt shows the digest's first 160 characters.

## Rollout

A merged public proposal changes `content/`, so the content workflow cuts an edge release, and edge devices (the laptop) take it at their next sync. The loop records the version. Once the check passes, the digest names the promote command. Recovery is `bud sync --rollback` on a device, or the revert the loop proposes. The overlay has no channels, so a merged overlay proposal reaches every device at its next sync, and its PR says so.

## GitHub

`GitHub` is a small stdlib client for the REST API. It covers the repo's default branch, reading a file at a ref, creating a branch and putting or deleting files on it, opening a draft PR and labelling it, reading a PR, comparing two commits, and reading the latest release. The token comes from `BUD_IMPROVE_GITHUB_TOKEN` through `op run` and needs Contents and Pull requests write on both repos. It is never logged. The overlay's `owner/name` comes from config `overlay.repo` (a GitHub git URL), or from `--overlay`. With none, overlay proposals are skipped.

## Tests

`internal/improve`'s `TestLoopEndToEnd` drives the loop day by day against a fake GitHub, a fake model list and a fake reviewer. It covers:
- a scan that sees a fact change;
- Jason's no, and the change not coming back;
- an unanswered Sunday followed by a yes on Monday;
- the PR's patched registry (comments kept), its draft flag and its label;
- the merge and the edge version;
- a tripled top-up spend that regresses, and the revert in the next Sunday's digest.

Other tests cover:
- validation and the leak shapes;
- dedupe and drops;
- spend across a month change and a missed day;
- the 3-day minimum;
- the digest slot;
- `Fit` against `approval.MaxSummary`;
- the digest's caps;
- a stale proposal.

`internal/cli` tests `bud improve add`, the overlay URL parsing and the Hermes pin, and `internal/mcpops` tests the proposal counts in `reviews`.
