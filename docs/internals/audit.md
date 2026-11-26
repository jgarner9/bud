# The shared audit log

`audit.jsonl` in bud's state dir (`paths.Audit`, `~/.local/state/bud/audit.jsonl`) is the one record of what Bud's trusted processes did (Suite D140). The gateway, the ops server, the router, mail and memory-review each append to it through `internal/audit`. It is a record only: nothing reads it to decide anything, so a missing or damaged log never changes what a leaf allows.

## What a line holds

One JSON object per line, written in a single `write` on a file opened `O_APPEND`, so lines from different processes never interleave. The file is mode 0600 and its directory 0700. Lines over 32 KB are refused.

| Field | Meaning |
| --- | --- |
| `time` | UTC |
| `source` | `gateway`, `ops`, `router`, `mail`, `memory-review` or `improve` |
| `kind` | `tool_call`, `approval`, `spend` or `mail_action` |
| `tier` | The privacy tier (`P0`, `P1`, `P2`), when the event has one |
| `actor`, `action`, `target` | Who, what and on what, as the leaf names them |
| `decision` | What was decided, such as `allow`, `deny`, `approved` or `declined` |
| `detail` | Names and counts specific to the kind. Never argument values, message bodies, record text or keys |

## Who writes what

| Leaf | Events |
| --- | --- |
| MCP gateway (`gateway`) | A `tool_call` for every call it forwards or refuses: the server and tool, the tier (and the privacy rule that raised it), the client, the decision, the status and the names of the arguments. Its approval-tier calls also write `approval` events |
| ops server (`ops`) | `approval` events for the approval-tier ops tools |
| router (`router`) | A `spend` event for each charged response (the model, dollars, token counts and the month's top-up so far), and `approval` events for the P0 fallback |
| mail (`mail`) | A `mail_action` for each move to the trash, restore and send: the folders and a recipient count, never a sender, subject or body. The send's approval is the gateway's |
| memory-review (`memory-review`) | `approval` events for approving, discarding and re-tiering a memory record |
| `bud improve` (`improve`) | `approval` events for its GitHub actions |

Every `approval` event comes from `approval.Store.Dispatch`, the one path an approval-tier action takes. It writes `requested` when the request is made, then how it ended: `approved`, `approved_failed` (the tool failed after approval), `denied`, `declined` (Jason said no himself), `expired`, `canceled` or `failed`. Its detail has the request id, the runtime, profile and surface, and a hash of the arguments, so a request can be matched to its prompt without the arguments being stored.

A leaf whose log won't open says so at start and runs without it, since nothing decides from the log. A failed write is logged and dropped, and never changes an approval or a call.

## State stays state

Three files are read back to enforce something, so they stay as they are and write an event beside their own write: `approvals.json` (pending approvals), `router-spend.json` (the spend cap) and `mail-actions.jsonl` (restoring moved mail). The audit log is where you look to see what happened; those files are where Bud looks to decide.

## Reading it

`audit.Read(path)` returns the events oldest first and skips lines it can't parse, so a torn or hand-edited line never hides the rest. The log isn't rotated by Bud yet.
