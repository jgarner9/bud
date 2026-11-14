# OpenClaw in bud

[OpenClaw](https://docs.openclaw.ai) is one of bud's [backends](backends.md), and this page is the worked example of setting one up. It is a Gateway you host yourself, with its own models behind it, reached through `openclaw acp`. The editor, OpenClaw's web UI, its apps and `openclaw tui` can all show the same sessions. Turn it on with `bud config set ai.backend openclaw`; the default is still `claude`.

## Where the Gateway runs

OpenClaw runs its tools (edits and commands) on the Gateway's machine, not on the machine bud runs on. A Gateway on another computer therefore edits that computer's files, not the project you have open. This decides how to set it up:

- **A Gateway on the machine you code on** (loopback, `ws://127.0.0.1:18789`). bud's edits and commands act on your project. This is the recommended setup, and the one bud has been checked against.
- **A shared Gateway on a server.** Fine for a phone app or the web UI, but a bud session pointed at it works on the server's files. OpenClaw can pair your machine as a node that hosts sessions, but those run in a managed workspace rather than your open project, and `openclaw acp` cannot ask for a node, so that does not suit editing in bud.

So for coding, run a Gateway on your own machine and point bud at it. Keep a server's Gateway for the apps and the web UI if you want one.

## Install and sign in

```sh
npm install -g openclaw      # Node 24.16 or newer; bud ai setup offers this too
openclaw onboard             # sets up the Gateway, its token and the models
```

Models sign in on the Gateway's machine, with `openclaw onboard` or OpenClaw's own configuration; bud never sees those logins.

## The Gateway token

bud reaches a Gateway with its token. Keep it in a file readable only by you, and name the file in `ai.openclaw.token_file`:

```sh
mkdir -p ~/.config/openclaw
umask 077 && printf '%s\n' "$TOKEN" > ~/.config/openclaw/gateway-token   # the token your Gateway is configured with
bud config set ai.openclaw.token_file ~/.config/openclaw/gateway-token
```

The file must hold the token alone on one line, since the TUI reads only the first line. bud passes the file to `openclaw acp` as `--token-file`, so the token stays out of `config.json` and process listings. For the AI pane, which runs `openclaw tui`, bud reads the file and hands the token over in `OPENCLAW_GATEWAY_TOKEN`, because the TUI takes no token file. If the file is missing, `bud ai status` reports the backend as not ready with a fix line; if other users can read it, that is only a warning (fix: `chmod 600`).

## Pointing bud at it

With a Gateway on the same machine, bud needs no URL. With none set, `openclaw acp` uses the CLI's own `gateway.remote.url`, else a Gateway on this machine. For a Gateway somewhere else:

```sh
bud config set ai.openclaw.url wss://gateway.example.ts.net:18789
```

OpenClaw accepts plain `ws://` only to loopback, private and Tailscale addresses (and `localhost`, `.local` and `.ts.net` names), so use `wss://` anywhere else. `bud ai setup` asks for the URL and the token file and saves them.

## Checking it

```sh
bud ai status
```

For a Gateway that is up, it looks like this:

```text
Backend: openclaw, ready
Runs: openclaw acp --url wss://gateway.example.ts.net:18789 --token-file /home/you/.config/openclaw/gateway-token
Command: <openclaw's version> (/usr/local/bin/openclaw)
Gateway: gateway.example.ts.net:18789, answering
```

With no `ai.openclaw.url`, `bud ai status` asks the CLI's own Gateway to report its health, which also proves the token. With a URL set, "answering" only means the port accepted a connection: the token and the WebSocket are not tested, since a health check would put the token on a command line. A wrong token shows up when a session starts instead (see below). Each problem is printed with a `fix:` line. `bud doctor` shows the same under "ai backend".

## Asking before commands

Whether OpenClaw asks before running a command is the Gateway's `tools.exec.mode`, and it applies to every surface, not only bud. The default, `full`, never asks. With `ask`, each command not on the allowlist comes up in bud as a question (`y` allow once, `a` allow that command for the session, `n` deny):

```sh
openclaw config set tools.exec.mode ask
```

File edits do not ask on OpenClaw: they are written, and bud shows what changed afterwards. Its ACP bridge sends no diff to review first.

## How bud uses it

- One session per project, made with the project as its working directory, saved in `~/.local/state/bud/agent-sessions.json` and reattached on the next start with its history.
- The AI pane (`<leader>ac`), `bud agent` and bud's own chat open that session. Chat mode (`<leader>ch`, `bud chat`) is a separate session outside any project.
- `ai.agent.chat_ui = "bud"` swaps the pane's `openclaw tui` for bud's own chat; `<leader>av` opens it either way.
- `:BudAgent` shows the session and why it stopped; `:BudAgent restart` starts it again, and `:BudAgent option` sets the session options OpenClaw offers.

## When it doesn't connect

`:BudAgent` shows OpenClaw's own words a few seconds after a project opens.

| It says | Fix |
| --- | --- |
| `openclaw isn't installed` | `bud ai setup`, or `npm install -g openclaw`. |
| `ACP bridge failed: connect ECONNREFUSED ...` | No Gateway at that address: start it (`openclaw gateway`, or its service) and check `ai.openclaw.url`. |
| `ACP bridge failed: unauthorized: gateway token mismatch` | The token file does not hold that Gateway's token: use the token your Gateway is configured with. |
