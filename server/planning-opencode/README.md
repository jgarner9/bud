# server/planning-opencode/

The home server's planning OpenCode (Suite D49, D109): `opencode serve` on the server, which the IDE attaches to for planning chats over the tailnet. It runs the planning role on OpenCode's `plan` agent with the server's tools (Exa search, Linear through the gateway, GitHub's hosted MCP, Context7, memory and the ops MCP; calendar and mail are denied, since they're Hermes's alone). It plans; it doesn't edit Jason's code, which lives on the laptop.

It runs as its own system user, `bud-planning`, not as Jason. Then a planning session can't open the approval socket or read the review secret in Jason's `XDG_RUNTIME_DIR`, and the unit's sandbox (`ProtectHome=yes` and `/run/user` hidden, with `ProtectSystem=strict` leaving only its own home, `/var/lib/bud-planning`, writable) means its file and shell tools see no one's home. That is the planning OpenCode's edit scope (D49), enforced by the OS rather than by permission rules.

| File | What it is |
| --- | --- |
| `bud-planning-opencode.service` | The system unit, `User=bud-planning`. It runs `opencode serve` under `op run`, so provider keys and the fragments' secrets reach it as environment variables. |
| `planning-opencode.env.example` | The server's Basic auth password, `OPENCODE_PASSWORD`. |

## Setting it up

`bud-planning` gets its own bud, OpenCode, synced config and 1Password service account, so nothing it reads or writes is Jason's. Run the steps as root where they say `sudo`, and as `bud-planning` (`sudo -iu bud-planning`) otherwise.

1. Create the user: `sudo useradd --system --user-group --create-home --home-dir /var/lib/bud-planning --shell /bin/bash bud-planning`. Node and `op` must be installed system-wide (`/usr/bin/node`, `/usr/bin/op`), since the user's PATH has only its own `~/.local/bin` beside the system dirs.
2. Make a 1Password service account for it, scoped to the items it needs: the registry's keyed providers (none once providers go through the router, Suite D98), Exa and the GitHub PAT. As `bud-planning`, put `OP_SERVICE_ACCOUNT_TOKEN=...` in `~/.config/bud-server/op-service-account.env`, mode 0600.
3. As `bud-planning`, install bud with `install.sh` (to `~/.local/bin`), set npm's prefix so OpenCode lands there too (`npm config set prefix ~/.local`), then run `bud install --yes --device server-planning` and `bud sync`. `bud sync` pulls the private overlay over SSH, so give `bud-planning` its own read-only deploy key for the overlay repo. The `server-planning` example profile is the harness alone with the `base` and `server` env fragments, so its `opencode.jsonc` is the server profile's, and its overlay replaces it like any other profile.
4. In the overlay's env fragment for this device, set `bud.gateway`, `bud.mcp.memory.url` and the fragments' secrets in `bud.opencode_secrets`, which `bud sync` writes to `~/.local/state/bud/opencode.env` beside the providers' keys:

   ```jsonc
   { "bud": { "opencode_secrets": { "EXA_API_KEY": "op://<vault>/<exa item>/credential", "GITHUB_PERSONAL_ACCESS_TOKEN": "op://<vault>/<github item>/token" } } }
   ```

   `op run` needs that file to exist, so with no secrets at all, drop `op run` from the unit.
5. Make the password: `openssl rand -base64 32`. Put `OPENCODE_PASSWORD=<it>` in `~/.config/bud-server/planning-opencode.env` (mode 0600, owned by `bud-planning`), and the bare password in the laptop's `ai.planning.password_file` (mode 0600).
6. Set `--hostname` in the unit to the server's tailnet IP (`tailscale ip -4`), then install and start it with the commands at the top of `bud-planning-opencode.service`.
7. Check it from the laptop: `curl -s -u opencode:<password> http://<tailnet IP>:8797/api/info` answers with the server's info, and without `-u` it answers 401.

`bud update` and `bud sync` run as `bud-planning`, like the install; restart the unit after either.

## Known gaps

- An OpenCode server with no client attached waits forever on a permission ask (docs/internals/suite.md, "Unattended sessions"). The server fragment's Linear and GitHub writes ask, so a planning session left asking when the IDE detaches waits until it's attached again. Planning sessions are attended through the IDE, so the rules stay as they are for now.
