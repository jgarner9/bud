# The private overlay

Bud's repo is public, so everything personal lives in a small private git repo that `bud sync` layers on top: your device profiles, your env (tailnet hosts, the Qwen box and server URLs, personal MCP servers), and your personal skills, agents, commands and persona (Suite D21, D59). It's optional. With no overlay, Bud runs on the public content alone.

## Layout

```
overlay.json             {"format": 1}
profiles/<device>.json   your real device profiles (see Device profiles)
env/registry.jsonc       your providers' real endpoints, merged over the public registry
env/<name>.jsonc         personal config fragments; every secret an op:// reference
content/AGENTS.md        your persona and the "who you are" instructions
content/skills/<id>/     personal skills, each with a SKILL.md
content/agents/<id>.md   personal agents
content/commands/<id>.md personal commands
content/plugins/<name>/  personal plugins
```

## How it layers

- **Skills, plugins, agents and commands** are replaced by ID. An overlay skill called `learn` replaces the public `learn` in full, so none of the public skill's files are left beside yours.
- **Env fragments and `AGENTS.md`** come from both layers, public first and the overlay last. They're merged when a runtime's config is rendered, so the overlay's settings win there. `env/registry.jsonc` merges by ID, as [the model registry](registry.md) describes.
- **Profiles** in the overlay's `profiles/` replace the public examples with the same name.
- **Every item remembers which layer it came from**, so `bud doctor` lists what you take from each layer, marks each overlay skill, agent, command or plugin that replaces a public one, and names the files it rendered.

## Your MCP addresses

The public fragments list the MCP servers each profile uses, but not the addresses only your tailnet has: the MCP gateway and Bud memory. Put them in an env fragment such as `env/personal.jsonc`, and add `personal` to each profile's `env` list after the public fragments:

```jsonc
{
  "bud": {
    // the home server's tailnet IP (tailscale ip -4) and the gateway's port; the
    // gateway answers only its IP address, so a MagicDNS name is refused here
    "gateway": "http://100.64.0.1:8790",
    "mcp": {
      "memory": { "url": "http://100.64.0.1:8791/mcp" },
    },
  },
}
```

Until a fragment sets them, `bud sync` leaves Linear, Exa (on a dev machine) and Bud memory out and says so. The same block can add servers of your own or turn a public one off with `"disabled": true`; the format is in [bud sync's internals](https://github.com/jgarner9/bud/blob/main/docs/internals/sync.md#tools-and-mcp-servers).

## How it's fetched

The overlay is a plain git repo over SSH, and Bud stores no token for it. On a dev machine, git uses the credentials already there; with 1Password's SSH agent, the key never sits on disk. On the home server, a read-only deploy key for the overlay repo is kept in the Bud vault (Suite D7).

The clone lives in `~/.local/share/bud/overlay`. A sync only fast-forwards it. When the clone has local changes, or has diverged from its remote, the sync stops, says so, and changes nothing. It never forces. The overlay isn't signed the way public content is (Suite D60), because the trust comes from it being your own private repo.
