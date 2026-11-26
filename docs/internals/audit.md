# The shared audit log

`audit.jsonl` in bud's state dir (`paths.Audit`, `~/.local/state/bud/audit.jsonl`) is the one record of what Bud's trusted processes did (Suite D140). The gateway, the ops server, the router, mail and memory-review each append to it through `internal/audit`. It is a record only: nothing reads it to decide anything, so a missing or damaged log never changes what a leaf allows.

## What a line holds

One JSON object per line, written in a single `write` on a file opened `O_APPEND`, so lines from different processes never interleave. The file is mode 0600 and its directory 0700. Lines over 32 KB are refused.

| Field | Meaning |
| --- | --- |
| `time` | UTC |
| `source` | `gateway`, `ops`, `router`, `mail` or `memory-review` |
| `kind` | `tool_call`, `approval`, `spend` or `mail_action` |
| `tier` | The privacy tier (`P0`, `P1`, `P2`), when the event has one |
| `actor`, `action`, `target` | Who, what and on what, as the leaf names them |
| `decision` | What was decided, such as `allow`, `deny`, `approved` or `declined` |
| `detail` | Names and counts specific to the kind. Never argument values, message bodies, record text or keys |

## State stays state

Three files are read back to enforce something, so they stay as they are and write an event beside their own write: `approvals.json` (pending approvals), `router-spend.json` (the spend cap) and `mail-actions.jsonl` (restoring moved mail). The audit log is where you look to see what happened; those files are where Bud looks to decide.

## Reading it

`audit.Read(path)` returns the events oldest first and skips lines it can't parse, so a torn or hand-edited line never hides the rest. The log isn't rotated by Bud yet.
