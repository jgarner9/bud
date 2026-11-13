# OpenClaw in bud

bud talks to its AI over the Agent Client Protocol (ACP), so the backend is a setting (`ai.backend`). The default is [OpenClaw](https://docs.openclaw.ai): a Gateway you host yourself, on a home server say, with Claude Code and ChatGPT Plus behind it. The editor, OpenClaw's web UI, its apps and `openclaw tui` all talk to the same sessions with the same models.

This is new in 1.1 and still being built. What works today: the AI pane, chat, `bud agent` and `bud chat` run OpenClaw's TUI on the project's Gateway session; the commit message, the generator, issues and work go through OpenClaw; permission questions come up in bud; `bud ai status` and `bud doctor` check it all. To stay on Claude Code directly in the meantime, run `bud config set ai.backend claude`.

## On the server (once)

OpenClaw needs Node 24.16 or newer.

```sh
npm install -g openclaw
openclaw onboard
```

The onboarding wizard sets up the Gateway, its token and the models. For the two subscriptions:

- **ChatGPT Plus:** pick OpenAI Codex sign-in (`openclaw onboard --auth-choice openai-codex`). It uses your ChatGPT subscription, not API billing.
- **Claude Code:** sign in to `claude` on the server, then set the Claude model's runtime to Claude Code's CLI (`agentRuntime: { id: "claude-cli" }` on that model in `~/.openclaw/openclaw.json`). Anthropic has changed its terms for third-party tools using a Claude subscription before, so check them first; an API key works either way.

Run the Gateway as a service, reachable over Tailscale:

```sh
openclaw gateway install
openclaw config set gateway.bind tailnet
```

Should OpenClaw ask before running a command? That's the Gateway's `tools.exec.mode`, and it applies to every surface. The default, `full`, never asks. `ask` makes every command not on the allowlist come up as a question in bud (and in the apps), the way Claude Code's permission prompt did:

```sh
openclaw config set tools.exec.mode ask
```

File edits never ask on OpenClaw: they're written, and bud shows what changed afterwards.

## On each machine

```sh
npm install -g openclaw                        # or bud ai setup, which offers to
install -m 600 /dev/null ~/.config/openclaw/gateway-token
$EDITOR ~/.config/openclaw/gateway-token       # paste the Gateway token (openclaw gateway auth-token on the server prints it)
bud config set ai.openclaw.url wss://HOME.YOUR-TAILNET.ts.net:18789
bud config set ai.openclaw.token_file ~/.config/openclaw/gateway-token
bud ai status                                  # installed, Gateway answering?
```

`bud ai setup` does the install and asks for the URL and the token file. OpenClaw only accepts plain `ws://` to loopback, private and Tailscale addresses, so use `wss://` anywhere else. With no URL set, bud uses the `openclaw` CLI's own (`gateway.remote.url`), or a Gateway on the same machine.

## How bud uses it

- **One Gateway session per project,** `agent:main:bud:<project>-<hash>`, kept across restarts. The AI pane (`<leader>ac`), `bud agent` and bud's own calls all use it, so the web UI and the apps show the same thread. `ai.openclaw.agent` picks another OpenClaw agent than `main`.
- **Chat** (`<leader>ch`, `bud chat`) is one more session, `agent:main:bud:chat`, outside any project.
- **The AI pane** runs `openclaw tui` on the project's session. `<leader>aF` makes it full screen and back, and `ai.agent.chat_layout = "full"` opens it that way.
- **Permission questions** (with `tools.exec.mode` at `ask`) come up as bud's own prompt: `y` allows once, `a` allows that command for the rest of the session, `n` denies.
- **The token** goes to `openclaw acp` as `--token-file` and to `openclaw tui` in its environment, never on a command line.

## Other backends

`ai.backend` can also be `claude` (Claude Code directly, as before 1.1), `codex` (`codex-acp`, ChatGPT Plus through Codex's own login), `opencode` (`opencode acp`), or `custom` with `ai.custom.command` for any other ACP agent. `bud ai setup` installs the command for the first three. See [config](config.md).
