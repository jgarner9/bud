# The memory reviewer

`bud memory-review` (`internal/memoryreview`) is what approves Bud's working memory (Suite D113). Agents stage what they want remembered through memory-mcp's `memory_stage`, and nothing is searchable until a reviewer approves it (Suite D51, D102). The reviewer token can approve anything in its tiers, so it lives in this one process, which no agent talks to, and Jason decides each record on Signal, through the bud-signal plugin's reviewer socket that the gateway and ops already use (Suite D101, D103). It runs on the home server as the user unit in `server/memory-review/` and listens on nothing.

## A round

Once a minute it lists every staged record with the memory service's `staged` action (the reviewer token's tiers, P0 to P2, 100 at a time in id order) and then:

1. Writes the counts to `memory-review.json` in bud's state dir (`paths.MemoryReviewStatus`), through `atomicfile`, mode 0600: `pending`, `by_privacy`, `asking` (prompts on Jason's phone now), `too_long`, `oldest` (when the oldest was staged) and `updated_at`. No ids or bodies, so the ops `reviews` tool can read it without holding a memory token.
2. Asks about each record that isn't already on his phone, resting or too long, oldest first, while fewer than four prompts wait (`MaxAsking`).

A record is asked about through `approval.Store.Dispatch` with `approval.SocketReview`, so each prompt is a request in the approval store like any other (the ops `approvals` tool counts it), bound to action `memory:approve`, target `memory` and the record's exact JSON (`id`, `source`, `kind`, `privacy`, `created_at`, `body`). The scope is fixed (`memoryreview.Scope`): the reviewer has no session of its own. The prompt is `approval.Summary`'s text: "Save this to Bud memory? A no deletes it.", the body whole, as lines that each open with a bar so no line of it can pass for bud's own words, then the other fields. A record whose prompt would be over `approval.MaxSummary` (6,000 characters, the plugin's `MAX_SUMMARY`; a test keeps the two equal) is never cut and never asked about: it counts as `too_long` and waits for a hand approval (the memory service's `approve`, with the reviewer token).

## The answer

- **Yes** (👍 or `yes <code>`): the executor calls the service's `approve`, and memory-mcp embeds the record within a minute, after which `memory_search` finds it.
- **His no** (👎 or `no <code>`): the service's `discard` deletes the record. Only a staged record can be discarded; one approved meanwhile stays (409, `memory.ErrApproved`). A deleted record can be staged again, and is asked about again.
- **No answer**: the record stays staged and rests for a day (`Retry`) before it is asked about again. The prompt is withdrawn a minute before it expires, so the plugin's own deny at the expiry never races an answer.
- **The plugin stopping** (Hermes restarting): the record stays staged and is asked about again in ten minutes.
- **No reviewer** (the socket or secret missing, a send that failed): the same ten-minute rest, logged.

Telling his no from the other denials is what the reviewer socket's `answered` field is for: the plugin sets `"answered": true` on a choice Jason gave himself, and `approval.Dispatch` returns `approval.ErrDeclined` (an `ErrDenied` too) for such a deny. A timeout or a stopping plugin is a plain `ErrDenied`, so nothing is deleted without his answer.

Which records are waiting, resting or too long is kept in memory only, so a restart asks about every staged record again.

## Re-tiering an approved record (Suite D136)

Records Hermes saved at P1 before D130 can be moved to P0 (or anywhere) with `bud memory-review retier <id> <P0|P1|P2>`, run on the home server under `op run` with the same environment as the unit. It lists the approved records with the service's `export` action to find the one, then asks on Signal under action `memory:retier`, whose target is the new tier: "Move this Bud memory record to P0?", the whole body, and `from`, `to`, `id`, `source`, `kind` and `created_at`. Only his yes calls the service's `retier` action (`POST /retier {id, privacy}`), which updates `records.privacy` and nothing else, since the FTS index and the vector tables don't copy it. His no, a stopping plugin and no answer change nothing. It refuses an id that isn't an approved record in the reviewer's tiers, a record already at that tier and a record too long to show whole (move that one by hand). The reviewer token's policy needs `retier` and `export` added; the service refuses a client that has `retier` and `stage`. Removing an approved record stays deferred: references and per-model vector tables don't cascade. `reviewer_test.go` covers the answers and the refusals, `test_service.py` the service action.

## Tests

`internal/memoryreview`'s tests drive the reviewer against a fake memory service and a fake review: a yes, his no and a stopping plugin; the prompt's arguments and how early it ends; the day's rest; a record too long to show; the four-prompt bound and the status file. `internal/approval` tests the memory prompt's text, `ErrDeclined`, and `MaxSummary` against the plugin. The memory service's `staged` and `discard` are tested in `server/memory/test_service.py`.
