# Signal and approvals by reaction

How Hermes, the manager, reaches Jason on Signal and how a reaction answers an approval (Suite D30, D42). The at-the-machine setup is in [server/signal-cli/README.md](../../server/signal-cli/README.md).

## The pieces

- **signal-cli** runs as `bud-signal-cli.service` on the home server, registered on the spare number as a dedicated bot account. Its daemon serves JSON-RPC at `/api/v1/rpc` and an SSE event stream at `/api/v1/events` on `127.0.0.1:8793` (`hermes.SignalURL`; the home server's 879x ports, clear of SearXNG's usual 8080). Only Hermes, on the same machine, talks to it. The agent's terminal runs in Docker and can't reach the host's loopback.
- **Hermes's Signal adapter** (bundled with Hermes) reads the event stream and sends replies. bud renders `platforms.signal` in `~/.hermes/config.yaml`: `enabled: true`, `extra.http_url` set to the URL above, and `unauthorized_dm_behavior: ignore`, so a stranger gets no reply and no pairing code. The bot number, the allowlist (Jason's number alone) and the home channel are personal, so they come from `~/.hermes/.env` or the overlay's `op://` references as `SIGNAL_ACCOUNT`, `SIGNAL_ALLOWED_USERS` and `SIGNAL_HOME_CHANNEL`, never from this repo. Groups stay off while `SIGNAL_GROUP_ALLOWED_USERS` is unset. `platforms.signal` and `plugins` are on the adapter's `owned` list, so a fragment can't turn pairing back on or enable other plugin code.
- **bud-signal**, a Hermes plugin in `server/hermes/plugins/bud-signal/`, embedded in the binary (`bud.HermesPlugin()`). On a `server` device, `bud sync` installs it in `~/.hermes/plugins/bud-signal/` under the ledger (kind `hermes-plugin`, layer `binary`), and the config enables it (`plugins.enabled: [bud-signal]`). A plugin dir bud didn't write, one reached through a link, or one where a file of bud's was edited by hand or swapped for something that isn't a plain file, is left alone and named in the notes: none of its files are written or deleted (their ledger entries are kept), and the plugin isn't enabled, so Hermes never runs plugin code that isn't bud's. Files Hermes adds itself, such as `__pycache__/`, don't count. After a sync that changed it, `hermes gateway restart` loads it.

## How an approval is answered

Every approval Bud's Hermes asks for comes from the tier gate (`bud hermes-gate`, see [sync.md](sync.md#hermes)), through Hermes's own approval gate. When the turn that raised it came from Signal, the gateway shows it in that chat. Hermes's Signal adapter has no buttons, so out of the box it posts text and waits for a typed `/approve`, and it drops an incoming reaction as an empty message.

bud-signal registers the `signal` platform again (last registration wins), through the bundled plugin's own `register` with only the adapter swapped, so setup, cron delivery and the auth env names stay Hermes's. Its `BudSignalAdapter` subclasses `SignalAdapter`:

1. It overrides `_send_exec_approval_prompt`, the hook a button platform overrides. The gateway then renders the prompt the way it does for buttons: it passes the request id, marks the request deny-only until the whole command has been shown, and leaves the timeout notice to Hermes. The adapter gives the prompt a four-digit code no other waiting prompt has, adds the lines saying how to answer, sends it, and keeps the message's sent timestamp, which is what a Signal reaction points at.
2. It looks at each incoming event before the bundled handler does. A reaction to a kept prompt, or a direct message that is only `yes <code>` or `no <code>`, answers that prompt: 👍 (any skin tone) or `yes` approves the one call (`once`, never the session or always scopes), and 👎 or `no` denies it. The answer goes to `tools.approval.resolve_gateway_approval_card` with that prompt's request id, so it can't answer another request, and with the sender as the actor. Hermes checks there that the sender is the person whose message raised it (`approvals.approvers` is `[]`) and refuses an approval on a deny-only request. On success the bot reacts ✅ or ❌ on the prompt.
3. Only a sender `SIGNAL_ALLOWED_USERS` names may answer, and the gateway's own authorization check must say yes. With the list unset or `*`, nothing answers, and the adapter logs a warning at start. Group messages, sync messages (the bot account's own devices), removed reactions and a reaction whose target author isn't the bot's number (one naming its target by UUID alone included) never answer anything. Anything that isn't an answer goes on to the bundled handler unchanged, so a code that matches nothing gets "No approval is waiting for code N." instead of reaching the agent, and a late answer is told the approval timed out.

An approval nobody answers is denied after `approvals.timeout`, an hour (D42). Typed `/approve` and `/deny` keep working. Prompts are kept in memory only, so a gateway restart forgets them, and the requests they answer go with it.

The plugin calls the Signal adapter's private methods, so `plugin.yaml` sets `requires_hermes: "==0.23.2"`: on another Hermes it doesn't load, the bundled adapter is used, and approvals take a typed `/approve` until the plugin is checked against the new release and the pin raised.

## Not built yet

The approval service (D28, JGA-348) that sends every approval, whatever surface raised it, to the Tauri app first and to Signal after 5 minutes. Until then an approval shows where its turn came from: on Signal for Signal turns, in the CLI for CLI turns. Cron turns can't ask (`approvals.cron_mode: deny`). A doctor check for the daemon and the plugin's load state isn't there either.

## Tests

`just hermes-plugin-test` runs the plugin's Python tests (standard library `unittest`, no Hermes needed): `test_approvals.py` covers parsing and the prompt registry, and `test_adapter.py` drives the adapter against stand-ins for the Hermes parts it calls, shaped after v0.23.2's. They don't replace the end-to-end check on the home server in the README's last step.
