# Handoff: approvals and privacy routing (JGA-377 to JGA-382)

2026-11-22, thread "Build approvals and privacy routing changes". Design: `/mnt/project-files/architecture/autonomy-and-privacy-design.md` (Suite D114 to D121). Order agreed with Jason: 377, 378, 380, 381, 379, then 382 last (it's blocked by 380 and 381, so nothing private reaches the cloud early). Leave the Gemma classifier (D120) off. Keep PR #104's "deny without an answer" meaning "nobody answered".

## Status (2026-11-22 18:40 UTC)

All six merged: #106, #108, #110, #112, #113, #114 (39bdd7a). Jason chose to hold P1 on the home server: the overlay puts qwen-35b first in roles.manager.pool until memory records carry a tier (revisit item 24).

### Earlier (17:10 UTC)

- Merged: #106 (JGA-377), #108 (378), #110 (380), #112 (381), #113 (379).
- Open: #114 (JGA-382), branch `claude/project-thread-70vf6j-gateway-trust`, waiting on Copilot, then merge.
- The home server rollout and the decisions left: items 22 to 25 of `/mnt/project-files/revisit-20261122T081850-a7a8.md`.
- Follow-ups not filed yet: memory records carry a tier (P0 records left out for P1 sessions); subagents through an always-P0 router path; router rules don't read `tool_calls` arguments.

## Gotchas

- The workflow: merge your own PRs after the Copilot round, leave a fix-summary comment, and poll for Copilot (a review with no comments doesn't wake the thread). Review requests to jgarner9 fail because he's the PR author; that's expected.
- Cloud sessions may push only `claude/project-thread-70vf6j` (and this handoff branch, at Jason's request).
- `just leak-check` rejects `op://` refs that don't use the placeholder `op://vault/item/field`.
- Use `GOCACHE=$TMPDIR/gocache` in the sandbox. stylua, selene and nvim are installed, so `just check` runs in full (about 3 minutes).
- The ask list is a speed bump, not a wall: a script can still push. The real guard for writes is the gateway.

## The home server rollout (once merged)

- After #106: `bud update`, `bud sync`, `hermes gateway restart`; check that `ls` or `curl` in a Signal chat no longer asks, and that `git push` still does.
- After 378: the same, then `bud doctor` should show "gateway approvals" OK; a Linear or calendar write should ask once on Signal.
