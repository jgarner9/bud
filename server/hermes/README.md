# Hermes, the 24/7 manager

Hermes Agent (Nous Research) runs on the home server as Bud's manager (Suite D48). Bud doesn't ship a unit for it: Hermes installs its own systemd unit for its gateway, and a second unit would fight it for the bot connections. What Bud owns is the config: `bud sync` on a `server` device renders `~/.hermes/config.yaml` from the model registry and the env fragments, with the D48 safety settings, and the tiers `bud hermes-gate` reads before every tool call. How that works is in [docs/internals/sync.md](../../docs/internals/sync.md#hermes).

Setting it up, at the machine:

1. Install Hermes at the pinned version (v0.23.2), and Docker, with your user in the `docker` group: the agent's terminal and file tools run in Hermes's sandbox container. `docker pull nousresearch/hermes-sandbox:desktop` saves the first command a wait.
2. Put `OP_SERVICE_ACCOUNT_TOKEN` (the Bud vault's service account) in `~/.hermes/.env`, so Hermes can resolve the `op://` references in its config with `op read` at start. Any provider key the registry names as `{env:VAR}`, such as `OPENCODE_GO_API_KEY`, goes there too, or as an `op://` reference under `bud.hermes.secrets.onepassword.env` in your overlay's `env/personal.jsonc`, with `EXA_API_KEY` for Hermes's web tools and `GITHUB_PERSONAL_ACCESS_TOKEN` for GitHub. GitHub's MCP server is GitHub's hosted one (`api.githubcopilot.com/mcp/`), so there is nothing to install for it; the token is a fine-grained PAT, limited to the repos Bud works on, with read and write on Contents and Pull requests and read on Actions, Issues and Metadata. A PAT doesn't need a Copilot licence.
3. Point the registry at the real Qwen box in your overlay's `env/registry.jsonc` (the public file's hosts are `.invalid`).
4. Run `bud sync`. It copies the synced skills into `~/.hermes/skills` and locks them; its notes name any it couldn't copy or lock. A skill Hermes already had under the same name is left alone and named, so move it aside first if you want bud's.
5. Check the gate refuses the never tier: `echo '{"hook_event_name": "pre_tool_call", "tool_name": "terminal", "tool_input": {"command": "op read x"}}' | ~/.local/bin/bud hermes-gate ~/.local/state/bud/hermes-tiers.json` prints a block.
6. `hermes doctor`, then `hermes gateway install`, `loginctl enable-linger "$USER"` and `hermes gateway start`.

After a Hermes version bump, run `bud sync` again and `hermes gateway restart`.
