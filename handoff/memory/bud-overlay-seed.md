---
name: bud-overlay-seed
description: State of seeding jgarner9/bud-overlay (2026-11-15) and the overlay gaps found in bud
metadata:
  type: project
  modified: 2026-11-15T18:05:25.643Z
---

Thread "Seed bud-overlay and wire sync merge", 2026-11-15.

- `bud sync` already fetched (ff-only, over the device's git creds) and layered the overlay before this thread; the only missing piece was doctor showing layers. PR jgarner9/bud#16 adds a "content layers" doctor check.
- jgarner9/bud-overlay: Jason OK'd pushing main (README + overlay.json, pushed 18:20Z). Seed is PR jgarner9/bud-overlay#1 (branch claude/seed-overlay): device names are dev, server, local (Jason's pick 2026-11-15); overlay URL git@github.com:jgarner9/bud-overlay.git confirmed; he'll provide the home-server deploy key when asked. env/registry.jsonc with <qwen-box-host>/<home-server-host> placeholders, env/personal.jsonc (empty mcp/permissions), content/AGENTS.md persona from Jason's Bud preferences.
- Seed also has env/registry.jsonc with <qwen-box-host>/<home-server-host> placeholders, env/personal.jsonc (empty), content/AGENTS.md persona.
- Hosts (MagicDNS names) are in the overlay's env/registry.jsonc, pushed 18:23Z: home server = the home server, Qwen box = the Qwen box. Never put them in jgarner9/bud. Personal skills and MCP servers: none for now (2026-11-15). Jason wants to build the system up from the ground, adding each piece only once it has been decided, so do not pre-fill the overlay with skills or servers.
- JGA-355 (Bud 2.0, filed 2026-11-15 at Jason's ask): decide each MCP server and plugin as server-only, server-as-proxy for local instances, or local-direct in OpenCode (1Password is a given; only its placement is open).
- Gap: op:// values inside env fragments are passed through literally (only registry provider keys become op run env vars), so MCP server keys can't be references yet. Flagged, not built.

Related: [[bud-suite-implementation]]
- 2026-11-15: bud-overlay PR #1 merged 18:32Z; bud PR #16 merged 18:57Z (after Copilot fixes: OpenCode-less profiles list only skills/plugins, "rendered:" label, "overlay on disk (last sync layered X)"). bud-overlay PR #2 (classify provider -> the home server:8083) merged 19:14Z. First real sync verified 2026-11-15 19:24Z on Jason's dev machine (the laptop, macOS) with nightly 1.1.1-nightly.202611151917: overlay cloned over SSH, dev profile from the overlay, doctor all ok. Thread done.
