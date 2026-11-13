# OpenClaw in bud

bud talks to its AI over the Agent Client Protocol (ACP), so the backend is a setting (`ai.backend`). The default is [OpenClaw](https://docs.openclaw.ai): a Gateway you host yourself, on a home server say, with Claude Code and ChatGPT Plus behind it. The editor, OpenClaw's web UI, its apps and `openclaw tui` all talk to the same sessions with the same models.

This is new in 1.1 and still being built. What works today: the AI pane, chat, `bud agent` and `bud chat` run OpenClaw's TUI on the project's Gateway session; the commit message, the generator, issues and work go through OpenClaw; permission questions come up in bud; `bud ai status` and `bud doctor` check it all. To stay on Claude Code directly in the meantime, run `bud config set ai.backend claude`.

## Where the Gateway runs

OpenClaw runs its tools (edits, commands) on the Gateway's machine, not where bud runs. So a Gateway on your home server alone would work on the server's files, not the project on your laptop. Two ways to set it up:

- **A Gateway on the machine you code on** (loopback, `ws://127.0.0.1:18789`). bud's edits and commands act on your project; the home server's Gateway serves the apps and the web UI. This is what bud has been checked against.
- **One Gateway at home, your laptop paired as a node that hosts sessions** (`nodeHost.workerRuns.enabled`). Tools would run on the laptop, but in a session-owned managed workspace (an empty folder or a fresh checkout), not the project you have open, and `openclaw acp` can't ask for a node. So this doesn't suit editing in bud.

So for coding, run a Gateway on the laptop (`gateway.bind loopback`) and point bud at it; keep the home server's for the phone and the web UI. The steps below are the same on either machine.

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

- **One session per project,** made by OpenClaw's ACP bridge with the project as its working directory, saved in `~/.local/state/bud/agent-sessions.json` and reattached on the next start, history and all. The AI pane (`<leader>ac`), `bud agent` and bud's own chat all open it (`agent:main:acp-bridge:<id>`), so the web UI and the apps show the same thread.
- **Chat** (`<leader>ch`, `bud chat`) is one more session, `agent:main:bud:chat`, outside any project.
- **The AI pane** runs `openclaw tui` on the project's session. `<leader>aF` makes it full screen and back, and `ai.agent.chat_layout = "full"` opens it that way.
- **Permission questions** (with `tools.exec.mode` at `ask`) come up as bud's own prompt: `y` allows once, `a` allows that command for the rest of the session, `n` denies.
- **bud's own chat** (`<leader>av`, or `<leader>ac` with `ai.agent.chat_ui = "bud"`) draws the conversation in Neovim instead of a TUI. Its colours are highlight groups you can restyle in your colorscheme or config: `BudChatYou`, `BudChatAgent`, `BudChatTool`, `BudChatToolDone`, `BudChatToolFailed`, `BudChatThinking` and `BudChatPrompt` (for example `:hi BudChatAgent guifg=#89b4fa gui=bold`).
- **`:BudAgent`** shows the project's session (backend, status, session id); `:BudAgent restart` and `:BudAgent stop` do what they say. `<leader>av` is bud's own chat on the same session.
- **The token** goes to `openclaw acp` as `--token-file` and to `openclaw tui` in its environment, never on a command line.

## Other backends

`ai.backend` can also be `claude` (Claude Code directly, as before 1.1), `codex` (`codex-acp`, ChatGPT Plus through Codex's own login), `opencode` (`opencode acp`), or `custom` with `ai.custom.command` for any other ACP agent. `bud ai setup` installs the command for the first three. See [config](config.md).
