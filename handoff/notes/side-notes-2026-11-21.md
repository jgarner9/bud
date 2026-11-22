# Side notes for when you're back (2026-11-21)

## Done while you were out

- R6b approvals (PRs #71–#75) and the router fallback (PRs #76–#80) are all merged.
- R7 mail is decided (Suite D106), and its own thread is building `bud mail-mcp`.
- In progress: the home-server embeddings unit (JGA-294, code side only). You'll run the install on the home server.

## Decisions I made on my own recommendation (you can reverse any of them)

- **D105:** a P0 fallback approval covers the rest of that message's turn for 10 minutes, instead of a single request. This came from your test where one message asked twice. Say so if you'd rather it asked every time.

## To talk about

1. **the home server cutover.**
   - Run `bud update`, then `bud sync`, then restart the gateway and the router.
   - Any gateway server that exists only in the overlay (Exa, say) needs `approval` or `read_only: true` in the overlay's env fragment, or the gateway won't start.
2. **Planning OpenCode as its own system user.** This is my recommendation from the R6b design (open question 1). It's still unconfirmed, and it needs you on the home server (`useradd`).
3. **JGA-364 cleanup.** The deletion list needs your review before anything is removed.
4. **R5c.** I'd hold it until you've used opencode.nvim for a bit.
5. **R9 custom TUI (JGA-375).** This needs a design session when we get close.
6. **The `claude/hermes-router` branch** is still on GitHub. You can delete it from PR #77.
7. **Gap:** an OpenCode server with no client attached waits forever on a permission ask. This matters once the planning server unit exists.

## Embeddings (JGA-294), added 10:45Z

- **PR #86** adds the home server's embedding server (`server/embed/`) and a `bud doctor --net` check that the vector size matches the registry. A second PR adds the re-embed path to the memory service.
- **Your step on the home server:** run `scripts/build-embed-server.sh ~/.local/lib/bud/embed` (needs git, cmake, a C++ compiler and curl), fill in `~/.config/bud-server/embed.env`, set `--host` to the home server's tailnet IP, and point the overlay registry's `embed` provider at `http://<ip>:8082/v1`.
- **Memory:** the server holds about 2.1 GB resident. Most of that is the 4,096-token batch buffer. If the home server is tight on RAM, one slot (`--parallel 1`) saves about 0.4 GB but lets a re-embed batch hold up queries, and a smaller token limit saves more but refuses longer memory records.
- **No web UI and no key:** llama-server warns that it has no API key and allows any CORS origin. It serves only embeddings, and the tailnet is the access control (D100), so I left it. Say so if you'd like a key on it anyway.
- **Pin by source:** I build llama.cpp from a pinned tag and commit, not a release archive, because this session could verify a git commit but not GitHub's release digests. It also lets the build tune for the home server's CPU.

## Memory MCP (R4), for when you're back

- Hermes still has its built-in `memory` toolset on (with writes staged). JGA-313 says one store, so I'd turn it off once memory-mcp runs on the home server and the Hermes skills point at `memory_search` and `memory_stage`. Your call, since it changes how Hermes remembers things day to day.
- The overlay needs `mcp.memory` set to `"gateway": true` (instead of a url) so devices reach memory through the gateway.
- the home server setup for memory-mcp needs five new memory-service tokens in 1Password (P0, P1, P2, STAGE, EMBED) and the policy entries in `server/memory-mcp/README.md`. That's your step, since it touches secrets.

## Overnight run (thread "Continue Bud roadmap overnight"), from 11:45Z

- **JGA-350, PR #91:** bud's `<leader>a` keys for opencode.nvim on `ai.backend = "opencode"`: `ad` reviews the session diff, `ap` models, `aS` sessions, `ay`/`an` allow or deny a waiting permission request, `as` shares the selection, `ac` toggles. Recorded as BASE D151 on my recommendation. Edits on this backend are reviewed after they're written (OpenCode's permission requests carry no diff); that's documented in docs/backends.md.
- **Still for you on JGA-350:** removing the old `bud.ai.claude` / `bud.ai.acp` chat code (a delete you review), and whether the default `ai.backend` should move from `claude` to `opencode`. I left the default alone because it changes every machine's chat key on update.
- **JGA-290 looks wrongly Done:** nothing in `server/` runs the planning OpenCode yet. R5b's design is in architecture/r5b-planning-server-design.md.
- **Planning OpenCode as its own user (R5b), your steps on the home server once it's built:** `sudo useradd --system --create-home bud-planning`, a 1Password service-account token for it (scoped to the provider, Exa and GitHub items) in a 0600 file it owns, then `sudo -iu bud-planning` and run install.sh plus `bud install --device server-planning` once, and write the OpenCode server password to a 0600 file. The exact paths come with the slice's README.
- **Idea, not built:** a statusline marker when opencode.nvim has a permission request waiting while its window is hidden.
- **Not checked live:** PR #91 was tested with stubs only. Worth a try on the laptop with `bud config set ai.backend opencode`: `<leader>ac`, a permission prompt (`<leader>ay`/`<leader>an`), `<leader>ad` after an edit, `<leader>ap`.

## R5b planning server and IDE switch (thread "Build home-server planning chat"), from 11:52Z

- **Slice 1, PR #92:** `server/planning-opencode/` runs `opencode serve` as its own system user `bud-planning` (Suite D109), home `/var/lib/bud-planning`, with a systemd sandbox that hides `/home` and `/run/user`. New `server-planning` example profile and `bud.opencode_secrets` for `EXA_API_KEY` / `GITHUB_PERSONAL_ACCESS_TOKEN`. Port 8797, Basic auth.
- **Slice 2:** `<leader>aP` / `:BudPlan` attach `opencode --server` in its own pane after a password-checked `/api/info` call; `ai.planning.url` and `ai.planning.password_file`; statusline `plan:home` / `plan:off` (Suite D110, BASE D152).
- **Your steps on the home server** (full detail in `server/planning-opencode/README.md`):
  1. `sudo useradd --system --user-group --create-home --home-dir /var/lib/bud-planning --shell /bin/bash bud-planning`. Node and `op` must be system-wide.
  2. A 1Password service account for it, scoped to the keyed providers, Exa and the GitHub PAT; its token in `/var/lib/bud-planning/.config/bud-server/op-service-account.env` (0600, owned by bud-planning).
  3. A read-only deploy key for the overlay repo for bud-planning (bud sync pulls it over SSH).
  4. As `sudo -iu bud-planning`: install.sh, `npm config set prefix ~/.local`, `bud install --yes --device server-planning`, `bud sync`.
  5. In the overlay: a fragment with `bud.opencode_secrets` (the Exa and GitHub op:// refs), plus `bud.gateway` and memory, on the server-planning profile.
  6. `openssl rand -base64 32` → `OPENCODE_PASSWORD=` in `/var/lib/bud-planning/.config/bud-server/planning-opencode.env` (0600), and the bare value in the laptop's `~/.config/bud/planning-password` (0600).
  7. Edit `--hostname` in the unit to the home server's tailnet IP, copy it to `/etc/systemd/system/`, enable it. On the laptop: `bud config set ai.planning.url http://<ip>:8797`.
- **Decided on my recommendation (reversible):** own system user (D109); home planning as OpenCode's own TUI rather than re-pointing opencode.nvim (D110). If the TUI grates in use, a second opencode.nvim-backed panel upstream is the follow-up.
- **Known gap left as is:** server.jsonc's Linear/GitHub writes still `ask`; if you detach the IDE mid-ask, that session waits until you reattach (the D99 server gap).
- **JGA-290:** I referenced it in PR #92; it can stay Done once that merges.
- **Flaky test, fixed in PR #93 (6a5ae53) after it failed twice:** `internal/approval` `TestDispatchExecutorReceivesReviewDeadlineAndCancellation/parent_cancellation` failed once on CI (deadline exceeded before the cancel, 500 ms deadline on a slow runner); passed 30/30 locally and on the next push. Likely fix: a long deadline in the parent-cancellation case.

## D99 server gap (thread "Deny unattended planning server permission asks"), from 12:14Z

- **PR #94, Suite D111 (my call, reversible):** a content plugin, `bud-unattended-deny`, rejects any OpenCode permission ask still unanswered after 10 minutes, with a message telling the model nobody answered. It only ever replies `reject`, so approvals can't loosen. Chosen over rendering the planning server's Linear/GitHub writes as `deny`, which would stop attended planning chats from filing issues.
- **A default you'll feel:** it ships to every profile that runs OpenCode, the laptop too, since profiles without a `plugins` list take every plugin. On the laptop that means an opencode.nvim ask you leave for 10 minutes is refused (re-ask to retry). To keep it off a device, give that overlay profile `"plugins": []`; an overlay profile that does list plugins must name `bud-unattended-deny` on the planning server.
- **Not checked live:** tested with a fake plugin context; the API was read from OpenCode's pinned source. On the home server, once bud-planning is set up and synced: `opencode plugin list` (or the server log) should show `bud-unattended-deny`, and a Linear write ask left alone should come back refused after 10 minutes.
- **Idea, not built:** resubscribe if the plugin's event stream ever ends (today it logs "event stream ended" and asks wait again until a restart).
