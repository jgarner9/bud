# R5b: the home-server planning OpenCode and the IDE's local/home switch (JGA-293)

Status: draft, 2026-11-21 overnight, on Claude's recommendations while Jason was asleep. The system-user question was already settled in project memory (own system user), so building follows that; the card in the thread lets Jason reverse it. Built 2026-11-21: slice 1 PR #92, slice 2 PR #93. bud-planning's home is /var/lib/bud-planning (not /home), and the doctor check for `ask` rules was dropped: the Linear/GitHub write rules still ask, since planning is attended.

## Why this comes first

JGA-293 (the IDE switch between local coding and home-server planning) depends on a planning OpenCode running on the home server. JGA-290 is marked Done in Linear, but `server/` has no planning OpenCode unit and nothing installs one: Hermes, the router, the gateway and the MCP servers are there, the planning server isn't. So R5's second half starts with that server.

## The planning server

- **What it is.** `opencode serve` on the home server, the planning role on the `plan` agent (Suite D49), reached by the IDE over the tailnet. It plans; it doesn't edit Jason's code, since the code lives on the laptop.
- **Who it runs as (the open question, r6b Q1).** Recommended: its own system user, `bud-planning`, with a system unit. Then it can't open the approval socket or read the reviewer secret in Jason's `XDG_RUNTIME_DIR` (0700), and its file tools see only its own home. The alternative is a user unit as Jason's user, like every other server unit: simpler, but a planning session's shell could then reach the socket and secret, so r6b's boundary would need another layer.
- **What already exists.** The `server` profile's `harness` component and `content/env/server.jsonc` already render the planning OpenCode's `opencode.jsonc` on the home server (Exa websearch, Linear through the gateway, GitHub's hosted MCP with a PAT, Context7, memory, ops; calendar and mail denied). Provider keys go in `~/.local/state/bud/opencode.env` for `op run`. What's missing is a unit that runs `opencode serve` with that config, and the fragment secrets (`EXA_API_KEY`, `GITHUB_PERSONAL_ACCESS_TOKEN`) reaching it (docs/internals/sync.md says that comes with this install).
- **Its config and secrets under its own user.** `bud-planning` runs its own `bud install --device server-planning` (a new example profile: `harness` only, env `base` + `server`, so the same rules render) and `bud sync`, so its config, ledger and content live in its own home. It needs its own 1Password service-account token file (0600, owned by `bud-planning`) for `op run`, scoped to the provider, Exa and GitHub items only. Jason creates that token. When the overlay registry points providers at the router (Suite D98), most provider keys drop out.
- **The ask gap (r6a).** server.jsonc has `ask` rules (Linear and GitHub writes), and an OpenCode server with no client attached waits forever on an ask. With the IDE attached, Jason answers it; unattended, nobody does. Recommended: leave the rules as they are, since planning sessions are always attended through the IDE, and note it for the R6a follow-up rather than changing the tiers here.
- **Listening.** `opencode serve --hostname <tailnet IP> --port 8797`, Basic auth on (username `opencode`). Port 8797 is the next free one after mail-mcp's 8796.
- **The open gap (r6a).** An OpenCode server with no client attached waits forever on a permission ask. With edit and bash outside its home denied rather than asked, and MCP approvals held by the gateway (which times out after an hour, D40), nothing it does should ask. A doctor check confirms the rendered config has no `ask` rules.

## The IDE switch (JGA-293)

Recommended: two surfaces that can't share state, rather than one plugin pointed at two hosts.

- **Local coding stays opencode.nvim** on the laptop's own OpenCode, as built in JGA-350.
- **Home planning is `opencode --server <url>`**, OpenCode's own TUI attached to the home server, in bud's chat pane (`<leader>aP`, and `:BudPlan`). Before starting it, bud checks the server's health with the password; when the home server or the tailnet is down, the pane says "home planning unavailable: <why>" and local coding carries on. Closing the pane only detaches; the server keeps running (opencode.nvim's `auto_kill` never applies, since bud didn't spawn it).
- **Why not switch opencode.nvim between hosts.** The plugin keeps one server connection, its sessions, pending permissions and context in module state set up once. Re-pointing it mid-session means resetting that state from outside, which depends on internals the pin could change, and a missed reset mixes sessions or sends laptop paths to a server that can't see them, which is what JGA-293 says must not happen. Two processes can't mix by construction. The cost is that planning uses OpenCode's TUI rather than the Neovim panel; if that grates in use, a follow-up can add a second opencode.nvim-backed panel upstream (JGA-293's "prepare for an upstream contribution").
- **Status.** The statusline shows `plan:home` while the planning pane runs, and `plan:off` when the health check failed, beside the existing chat status.
- **Config keys.** `ai.planning.url` (for example `http://100.x.y.z:8797`, from the overlay's IDE settings or `bud config set`), `ai.planning.password_file` (default `~/.config/bud/planning-password`, must be 0600). Unset URL means the key says how to set it.

## Slices, one PR each

1. `server/planning-opencode/`: the system unit (`User=bud-planning`, `op run` over its own `opencode.env` plus an env file for `EXA_API_KEY`/`GITHUB_PERSONAL_ACCESS_TOKEN` references, `opencode serve --hostname <tailnet IP> --port 8797`, `OPENCODE_PASSWORD` from a 0600 file), README (useradd, service-account token, one-time install as that user, password file, tailnet IP), the `server-planning` example profile, and routing the fragment secrets into `opencode.env`.
2. The IDE side: `ai.planning.*` keys (docs/config.md rows), the health check, `<leader>aP` / `:BudPlan` with the unavailable state, the statusline item, Lua tests with a fake `opencode` and a fake health server.

## Open for Jason

1. Own system user for the planning OpenCode: taken from project memory; the card lets Jason reverse it.
3. A 1Password service-account token for `bud-planning`, scoped to the provider, Exa and GitHub items.
2. JGA-290 is marked Done but the planning OpenCode part isn't built; reopen it or let this slice close it.
