# Self-improvement proposals and the weekly review (JGA-321)

Built 2026-11-22 by the thread "Build self-improvement proposals and weekly review". It turns the D47 loop (architecture section 8) into code: `bud improve`, a daily timer on the home server. Decisions D125 to D129 were taken on Claude's recommendation under Jason's standing rule; each can be reversed.

## How it works

1. **Observe (daily).** `bud improve run` snapshots the numbers checks are made against: this month's router spend per model and the top-up total (`router-spend.json`), and the server's `bud doctor --quick` fail and warn counts. Snapshots are kept 60 days.
2. **Scan (weekly).** Every provider's `GET /models` is compared with the registry (`registry.Compare`, as `bud registry refresh` does). Facts that changed (context, output, price, inputs, caps) become a registry proposal with the patched file (`registry.PatchFacts`), against the overlay's `env/registry.jsonc` when the model is defined there and `content/env/registry.jsonc` otherwise. Models the registry lacks are listed as candidates, each with the roles whose needs it meets; models no longer listed are named. Hermes's latest release is compared with bud-signal's pin. The scan calls no model, so nothing leaves the house but the list requests.
3. **Propose.** A proposal is an entry in `~/.local/state/bud/improve.json`: what was seen (evidence), the files it changes (whole before and after contents), how to tell it worked (criteria on the measured numbers), and its target, `public` (paths under `content/` in jgarner9/bud) or `overlay` (the private overlay). A public proposal holding a personal detail (a tailnet name, a real `op://` vault, a home path, any `BUD_LEAK_PATTERNS` match) is refused. The same change is never proposed twice (a fingerprint of target and files).
4. **Review (weekly, one prompt).** Sunday morning, if anything is new, one Signal prompt through the bud-signal reviewer socket (action `improve:digest`) shows the digest: each new proposal's title, evidence and criteria, regressions, versions waiting for promotion, and the landscape scan. A yes opens each proposal as a draft pull request labelled `bud-proposal`; his no drops them for good; no answer asks again the next morning. Jason then reviews and merges each PR himself on GitHub.
5. **Roll out.** A merged public proposal reaches the edge channel through the content release workflow, as any change to `content/` does. Once its check passes, the digest names the edge version with the command that promotes it (`gh workflow run content.yml -f mode=promote -f version=<v>`). Promotion stays Jason's explicit step.
6. **Check.** Seven days after a merge, the seven days before and after are compared for each criterion. A regression makes a revert proposal (the after and before swapped), which comes in the next digest like any other.

## Decisions

- **D125.** Proposals live in a Bud-owned queue on the home server, written by `bud improve` and by nothing else that applies them. Each carries evidence, whole-file before and after contents, criteria and a target (public `content/` or the overlay). Public proposals are refused when they hold a personal detail. Rejected: agents writing PRs directly (no single record, no leak gate) and diffs instead of whole files (an applier is more code and the PR shows the diff anyway).
- **D126.** Review is at most one Signal prompt a week, through the existing approval API. A yes opens draft PRs; Jason approves each change by merging its PR. Claude threads never merge a `bud-proposal` PR. Rejected: one prompt per proposal (too many asks, Jason's 2026-11-22 feedback) and merging on a Signal yes (one yes would approve several changes he hasn't read).
- **D127.** Public proposals ship edge first through the content workflow, and promotion to stable is Jason's run of its `promote` mode, which the digest names once the check passes. The overlay has no channels, so a merged overlay proposal reaches every device at its next sync; its PR says so.
- **D128.** Checks compare seven days before and after the merge, from daily snapshots of router spend (per model and top-up, dollars per day) and the server's doctor counts. A regression becomes a revert proposal, never an automatic revert.
- **D129.** The weekly scan is deterministic, with no model: provider model lists against the registry (fact drift becomes a proposal, new models become candidates mapped to roles by their needs), and Hermes's latest release against the pin.

## Not built yet

- Proposals from agents (Hermes noticing repeated corrections or failures and drafting a skill edit). The queue takes hand-written proposals through `bud improve add`; an ops tool for agents is a follow-up.
- Quality numbers (thumbs up or down per role, eval sets per role, JGA-295's token use). Checks use spend and doctor until those exist.
- Edge-first rollout for overlay changes.
