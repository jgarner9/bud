# Bud Suite architecture

This is the current direction for the public Suite repository. The external Suite decision log retains the history, and `BASE.md` covers the IDE and CLI. Suite D48–D61 take precedence where they revise earlier decisions. The November 19 Herdr decision supersedes Suite D6 and the Tauri-specific client and distribution choices in D50 and D56.

## Components

```text
Desktop Herdr workspace → Bud Neovim and local OpenCode sessions
iOS Herdr workspace     → SSH → home-server herdr-core → agent terminals
Home server             → Hermes, planning OpenCode and Bud services
```

Bud owns the Neovim configuration, server bootstrap, device profiles, signed updates, privacy policy, memory and action approvals. Herdr owns the agent workspace and terminal connections. Hermes runs home-server assistant sessions. OpenCode runs local coding and home-server planning sessions. Bud's profiles select components and update channels, and the model registry assigns models to roles. The public tree contains examples. Real device configuration and personal content live in a private overlay, with credentials resolved from 1Password references at the point of use.

## Resolved boundaries

- **Memory writes.** Suite D48 stages routine captures with provenance for review. Retrieval serves approved records within the client's privacy scope. Curated Markdown stays in a separate private Git repository. The local memory service owns SQLite, FTS5, sqlite-vec and migrations behind a Unix-socket API. The Go CLI remains standard-library-only.
- **Recovery.** Suite D60 prevents a normal channel update from replaying older signed content. `bud sync --rollback` is explicit local recovery to a compatible saved version. The binary updater has a separate recovery path that restores the previously installed binary if sync or quick health checks fail after an update. [Content verification](content.md) · [Sync recovery](sync.md) · [Binary updater](updater.md)
- **Client ownership.** Bud manages installed versions, configuration and local update policy. Phase 7 targets Herdr desktop and mobile. The workspace integration must establish how Herdr's updater and Bud's update and rollback policy work together.

## Build-time validation

| Area | Evidence to collect | Work item |
| --- | --- | --- |
| Runtime and provider contracts | Verify pinned Hermes and OpenCode sessions, tools, streaming, auth and provider limits with synthetic inputs. | JGA-291 |
| Action approvals | Prove that final dispatch binds the authenticated decision to the session, action, target and final arguments and rejects expired or reused decisions. Keep the existing Signal flow while validating other surfaces. | JGA-348, JGA-349 |
| Local models | Decide how Lemonade fits with llama.cpp on the target hardware. Measure Qwen and FIM candidates before choosing hardware settings or a completion model. | JGA-345, JGA-352 |
| Content activation | Verify which changes reload live and which need a controlled restart. | JGA-297 |
| Memory packaging and recall | Verify runtime and extension pins, restart and isolated recovery on the home server. Measure recall through local embeddings and runtime integration against the 300 ms p95 target. | JGA-294, JGA-310, JGA-313, JGA-368 |
| Mail and autocomplete | Verify recoverable mail cleanup and the provider terms for cloud completion before enabling those paths. | JGA-315, JGA-352 |
| Desktop workspace | Prototype the Herdr dashboard with Bud Neovim and local coding panes. Verify update ownership, rollback and session reconnect. | JGA-292 |
| Mobile workspace | Check SSH authentication, app backgrounding, terminal persistence, session recovery, Hermes and Signal context, notifications and approval expiry on the pinned versions. | JGA-354 |
