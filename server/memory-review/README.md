# server/memory-review/

The home server's memory reviewer (Suite D113): `bud memory-review` behind a systemd user unit. Agents stage what they want remembered with `memory_stage`, and nothing is searchable until Jason approves it. This is what asks him: each staged record goes to him on Signal, whole, through the bud-signal plugin's reviewer socket. A 👍 or `yes <code>` approves it, so `memory_search` finds it once it is embedded; a 👎 or `no <code>` deletes it. An unanswered prompt leaves the record staged, and it is asked about again a day later. How it works is in [docs/internals/memory-review.md](https://github.com/jgarner9/bud/blob/main/docs/internals/memory-review.md).

| File | What it is |
| --- | --- |
| `bud-memory-review.service` | The user unit. It runs `bud memory-review` under `op run`, so the reviewer token reaches it as an environment variable. |
| `memory-review.env.example` | The one `op://` reference `op run` resolves, with a placeholder vault path. |

## Setting it up

The memory service (`server/memory/`), Hermes with the bud-signal plugin (`server/hermes/`) and its reviewer secret come first.

1. Give the memory service's reviewer token the actions this needs. In the `BUD_MEMORY_CLIENTS` policy, its entry becomes:

   ```json
   "<reviewer token>": { "tiers": ["P0", "P1", "P2"], "actions": ["search", "staged", "approve", "discard", "retier", "export"] }
   ```

   Restart the memory service so it reads the new policy. The service refuses a policy that gives one token both `stage` and any of `staged`, `approve`, `discard` or `retier`. `retier` and `export` are what `bud memory-review retier <id> <tier>` uses to move an approved record to another tier (Suite D136).
2. Put the env file at `~/.config/bud-server/memory-review.env`, mode 0600, with the reviewer field's `op://` path. The service account's token file is the one the memory service already uses.
3. Install and start the unit with the commands at the top of `bud-memory-review.service`, as the user Hermes and the memory service run as.
4. Check it: stage a record with `memory_stage` (or let Hermes save something). Within a minute a "Save this to Bud memory?" prompt arrives on Signal with the whole record. Answer 👍, and within another minute `journalctl --user -u bud-memory-mcp` says `embedded 1 records` and `memory_search` finds it. The ops `reviews` tool shows the count of records still waiting.

The reviewer token can approve anything in its tiers. Keep it out of every agent's environment: only this unit's `op run` reads it.
