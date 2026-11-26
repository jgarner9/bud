# server/improve/

The home server's self-improvement loop (Suite D47, D125 to D129): `bud improve run` behind a daily systemd timer. Each round measures router spend and the server's doctor counts, scans every provider's model list against the registry once a week, follows the pull requests it opened, and checks merged changes a week after the merge. Once a week, when there is something new, it asks Jason one Signal prompt with the digest: a 👍 or `yes <code>` opens each new proposal as a draft pull request labelled `bud-proposal`, a 👎 or `no <code>` drops them for good, and no answer asks again the next morning. Nothing reaches a device until Jason merges a pull request himself. How it works is in [docs/internals/improve.md](https://github.com/jgarner9/bud/blob/main/docs/internals/improve.md).

| File | What it is |
| --- | --- |
| `bud-improve.service` | The oneshot unit. It runs `bud improve run` under `op run`, so the GitHub token reaches it as an environment variable. |
| `bud-improve.timer` | Starts it daily at 10:30, catching up a missed day. |
| `improve.env.example` | The `op://` references `op run` resolves, with placeholder vault paths. |

## Setting it up

Hermes with the bud-signal plugin and its reviewer secret (`server/hermes/`) and the router (`server/router/`, for its spend ledger) come first. The machine config's `overlay.repo` names the overlay; `--overlay owner/name` in the unit overrides it.

1. Make a fine-grained GitHub token for jgarner9/bud and the overlay with Contents and Pull requests read and write, and put it in the Bud vault.
2. Add a `bud-proposal` label to both repos. Without it the pull requests still open, unlabelled.
3. Put the env file at `~/.config/bud-server/improve.env`, mode 0600, with the token's `op://` path.
4. Install and start the timer with the commands at the top of `bud-improve.service`, as the user Hermes runs as.
5. Check it: `systemctl --user start bud-improve` runs a round now; `journalctl --user -u bud-improve` shows what it measured and queued, and `bud improve list` and `bud improve show` print the queue and the digest it would ask. The ops `reviews` tool counts the proposals by status.

Agent sessions never merge a `bud-proposal` pull request: merging one is Jason's approval of that change.
