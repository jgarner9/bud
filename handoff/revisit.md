# Revisit: what's waiting on Jason

This is the Claude project's revisit list as of 2026-11-22 18:45 UTC, with host names replaced by roles and `op://` paths by placeholders. The exact home-server commands, with the real vault, IPs and host names, are in the overlay's `handoff/home-server.md`. Strike an item when it's done, and add new ones at the end with the next number.

## Open, in the order to do them

Native macOS check for the OpenCode launcher: with a synced `opencode.env`, open `bud agent` in a large terminal, shrink it, then expand it. Check that the footer follows the lower edge each time.

For interactive OpenCode on the server profile, install a build with the overlay provider-key fix before syncing the server-only Go and GitHub references. Verify authentication in a fresh `bud agent .` session.

Home-server rollouts Jason hasn't done yet. Each one changes how the home server behaves, so they're his to run; give him paste blocks, never run them for him.

| Order | Item | What | Waits on |
| --- | --- | --- | --- |
| 1 | 24 | Hold P1 back: put `qwen-35b` first in the overlay's `roles.manager.pool` | nothing; do it before item 22's sync |
| 2 | 22 | Approvals and privacy rollout (JGA-377 to JGA-382): update bud, promote content, `bud sync`, restart router, gateway and Hermes, then the Signal checks | item 24 |
| 3 | 29 | Memory privacy rollout (JGA-389, D130, PR #116): sync, restart, re-check already-approved records, test, then lift the item 24 hold | PR #116 merged, item 22 |
| 4 | 23 | Overlay rules only he can write: mail to himself runs alone; an `ask` rule for shared calendars | item 22 |
| 5 | 21 | Self-improvement rollout (JGA-321): GitHub token, `bud-proposal` label in both repos, the `bud-improve` timer | nothing |
| 6 | 27 | KB curator and site rollout (JGA-311, JGA-312): Hugo, bud-kb repo and deploy key, kb-site unit, curator token and timer | nothing |
| - | 7 | Laptop: live test of the opencode.nvim chat keys | nothing |
| - | 30 | Spend views (JGA-391): update bud on the laptop and the home server, `bud sync`, restart the router and mcp-ops | the PR merged |

Decisions waiting on him (bring them up with a recommendation; don't decide them):

- **Item 29 step 3:** ~~already-approved memory records were saved at P1, even ones from chats that read his calendar, and the memory service has no action to re-tier or remove an approved record.~~ Decided 2026-11-24 (Suite D136), built 2026-11-26: `bud memory-review retier <id> <tier>`, asked on Signal the way memory-review approves; removing a record is deferred. See item 36.
- **Item 8:** ~~removing the old `bud.ai.claude` / `bud.ai.acp` chat code, and whether `ai.backend` defaults to `opencode`.~~ Done 2026-11-26 (Suite D139, JGA-402 to JGA-404): the old chat code is gone, `ai.backend` no longer exists, and OpenCode is the only backend.
- **Item 9:** the JGA-364 deletion list.
- **Item 10:** R5c, other chat keys (recommended: wait until he's used opencode.nvim).
- **Item 11:** R9, the custom TUI (JGA-375): a design session before any code.
- **JGA-399 (go-imap):** after it merges, the first live mail run on the home server is the one check left: iCloud's `MOVE` and `UIDPLUS`, whether it files a copy in Sent Messages, and how long the trash keeps mail. `bud mail-mcp --record` captures it, and its redacted transcript shows what the adapter sends and what iCloud answers. The module fetch and the Linear filing, which the overnight sessions had to leave for Jason, are done.
- **Item 12:** JGA-317 summarizer shape — decided 2026-11-24 (Suite D133): a direct router call with no tools. Still waits on the mail `--record` output.
- **Item 13:** the stale branch `claude/hermes-router` can be deleted.
- **JGA-393:** whether laptop OpenCode goes through the router (Suite D70) for one spend meter and the same privacy rules, at the cost of coding needing the home server. Jason chose on 2026-11-24 to decide it later.
- **Item 16:** router spend past the $10 cap (recommended: hold and ask on Signal, one yes covers one request or the rest of that turn, never a raised cap).
- **Item 28:** moving bud-kb's remote onto the home server so P0 facts can reach the KB (recommended: wait a few weeks).
- Older open items from the 2026-11-22 handoff: the JGA-368 hard-kill and backup-restore checks, the mail `--record` output for JGA-317, and a separate vault for the memory reviewer token and the bud-planning service account (both can read the whole Bud vault today).

Work an agent can pick up without him: the re-tier action (JGA-396, Suite D136), then the narrower Bud 2.0 backlog in [state.md](state.md). JGA-390 (Suite D135, the always-P0 subagent path) is built; its rollout check is item 35.

Closed since the list was last read: item 19 (the `improve:digest` prompt title) landed in PR #110; item 25's memory gap became D130 (PR #116).

---

## The full list

Started 2026-11-21 by the thread "Continue Bud roadmap tasks". Each item says what to do and where the detail is. Strike an item when it's done.

### Home server

Worked through on 2026-11-22 (UTC) by the thread "Continue Bud roadmap tasks" on the home server. What's left for you is in **one paste block** under "the home server: commands for Jason" at the end.

1. ~~**Mail setup.**~~ RUNNING (2026-11-22 07:59 UTC). `bud-mail-mcp` is active, `list_folders` answers through the gateway at P0, and after a Hermes restart Hermes registers all 8 mail tools with no mail error. Self-test email via Signal worked (2026-11-22). Only the `--record` output for JGA-317 is still to share, if you ran it.
2. ~~**Calendar on the "iCloud" item.**~~ DONE. `calendar-mcp.env` points at `op://vault/iCloud/...` (backup `.pre-icloud`), and `list_calendars` answered through the gateway. Contacts was still on "iCloud CalDAV" too and was stopping the Hermes gateway from starting, so `contacts-mcp.env` and `hermes-contacts.env` now point at "iCloud" as well (backups `.pre-icloud`). This settles side note 1 in `r7-side-notes.md` the recommended way.
3. ~~**Memory MCP.**~~ RUNNING (2026-11-22 07:35 UTC). Jason made the "Bud Memory" item (seven fields). `bud-embed`, `bud-memory` and `bud-memory-mcp` are enabled and active; `bud doctor --net` says "embeddings qwen3-embedding-0.6b (1024 dims) answered"; through the gateway a P1 `memory_stage` saved record 1 ("staged for Jason's review") and a P0 `memory_search` answered (empty, since nothing is approved yet). Overlay PR #8 sets `mcp.memory` to `"gateway": true` on server, dev and server-planning. Record 1 was approved by hand (part F), embedded and found by `memory_search` at P0 by vector. **Gap:** nothing approves staged records except a hand-run curl; bud-signal's reviewer handles tool approvals only.
4. ~~**Hermes's own memory: off.**~~ DONE (2026-11-22 07:57 UTC). bud #103 and overlay #8 merged; content 2026.11.22.1 promoted; the home server synced. Both Hermes profiles have no `memory` toolset and register `mcp__memory__memory_search` / `memory_stage` (private P0, cloud P1). The cloud `config.yaml` was skipped as hand-edited again, so it was moved aside (`.pre-memory`), re-rendered, and its `telemetry` opt-out block appended back; the diff was only the memory change. Gateway, router and Hermes restarted; Signal connected.
5. ~~**bud-planning user.**~~ DONE (2026-11-22). Jason ran parts E and G and signed `op` out on the home server. `bud-planning-opencode` is active on 100.64.0.1:8797 and answers 401 without the password. Jason's curl with the password from the laptop worked. `<leader>aP` is part of item 7.
6. ~~**Cutover after R6b.**~~ DONE. bud is built from main 7a55fe7 (previous binary `bud.9430861.bak`), synced, and the gateway, router and Hermes restarted. All units are active. Signal connected.

### Laptop

7. **Live test of opencode.nvim chat keys.** Press `<leader>ac` for the chat, then try a permission prompt (`<leader>ay` / `<leader>an`), `<leader>ad` after an edit, `<leader>ap`, and `<leader>aP` once the planning server runs.

### Decisions for you

8. ~~**JGA-350 follow-ups.** Removing the old `bud.ai.claude` / `bud.ai.acp` chat code (a delete you review), and whether the default `ai.backend` moves from `claude` to `opencode`.~~ **DONE 2026-11-26 (Suite D139, JGA-402 to JGA-404).** The old chat code and the `ai.backend` key are gone; OpenCode is the only backend, and `bud.ai.agent` and `bud.ai.acp` stay as the programmatic session.
9. **JGA-364 deletion list** needs your review before anything is removed.
10. **R5c** (other chat keys): recommended to wait until you've used opencode.nvim for a while.
11. **R9 custom TUI (JGA-375):** a design session before any code.
12. **JGA-317 summarizer shape:** a direct router call with no tools (recommended) or a tool-less Hermes profile. Waits on item 1's recording.
13. **Stale branch** `claude/hermes-router` can be deleted from PR #77.

### Added by this thread

(New items go below as they come up.)
14. **Suite D112 privacy rules (PR #95), decided on my recommendation.** A message starting with `#private`, or a pasted private key, forces P0 at the router. Say if you'd like another marker. Personal rules (names, accounts) are yours to add in the overlay's `registry.jsonc` under `privacy_rules`. The gateway half is PR #96 (below); still not done: the D67 classifier (needs Gemma running on the home server).
15. **Gateway privacy rules (PR #96).** After the next `bud update` on the home server, restart the gateway and check its journal says "N privacy rules". It now refuses to start without a synced registry (run `bud sync`, or pass `--registry`), which the home server already has. **2026-11-22: BLOCKED on content.** The gateway starts on the new build, but it loads **0 privacy rules**, because the home server syncs stable content 2026.11.21.1, which predates #95. Edge 2026.11.21.5 (main 7a55fe7) has them, along with the mail tiers and the mail skill: paste block, part A. Also, the "N privacy rules" line only prints with `bud --verbose`, so it never shows in the journal. Part A checks it with a throwaway gateway instead.
16. **Router spend past the cap (decision).** At the $10 top-up cap the router answers 402 and says approval "isn't built yet". The action tiers already say spending asks on the phone, so the mechanism would mirror the P0 fallback (D104): hold the request, ask on Signal, go ahead on a yes. What's yours to pick is how much one yes covers. Recommendation: one request (or the rest of that turn, as D105 does for P0), never a raised cap, so every dollar past $10 is one you saw.
17. **Ops `reviews` tool (decision).** It still says "not built". The memory service has no way to count staged records without a reviewer-strength token, and Hermes keeps its own staged skill/memory queue. Recommendation: a new count-only `pending_count` action in the memory service with its own token (tiers P0-P2, no bodies), and the tool reports memory's count plus "Hermes: see /skills pending" until Hermes exposes one. It touches the memory service's token model, so it waits for you.
18. **After the next `bud update` on the home server:** `bud doctor` now shows `signal-cli` (daemon answers on 8793) and `Hermes version` (must be the release bud-signal is pinned to, 0.23.2). The ops `spend` tool reads `router-spend.json`; the cap appears in it after the router's next charge. (PRs #98, #99.) **2026-11-22: DONE.** `bud doctor` shows signal-cli answering on 8793 and Hermes 0.23.2. The only leftovers are the ones already known: the nvim config (`bud update`) and the cloud `config.yaml` telemetry hand edit.

### Home server: commands for Jason

All done as of 2026-11-22 08:20 UTC. To approve another staged memory record by hand, until a reviewer client exists:

```sh
cd ~/.config/bud-server && set -a && . ./op-service-account.env && set +a
curl -s --unix-socket /run/user/1000/bud-memory/memory.sock -H "Authorization: Bearer $(op read 'op://vault/Bud Memory/reviewer')" \
  -H 'Content-Type: application/json' -d '{"id":N}' http://memory/approve
```

### Self-improvement loop (JGA-321, PR #109)

19. **DONE 2026-11-22 (folded into PR #110).** ~~Decide: the digest's prompt title.~~ **Was:** `bud improve`'s weekly Signal prompt needs one entry in `internal/approval/socket.go`'s `titles` map, `"improve:digest"` with `whole: ["digest"]` and `hide: ["proposals"]`, so the digest shows in full. Without it the prompt shows only its first 160 characters. I didn't add it because that file belongs to the approvals work. Recommendation: add it (4 lines, the same pattern `memory:approve` uses).
20. **Decisions taken on recommendation (Suite D125 to D129).** Proposals go to a Bud-owned queue, at most one Signal digest a week, and a yes opens them as draft PRs that you merge yourself. Changes ship edge first, and promoting to stable stays your `gh workflow run content.yml -f mode=promote -f version=<v>`. The loop checks the 7 days before and after a merge on router spend and doctor counts, and proposes a revert if they get worse. The model scan is deterministic. Detail is in `architecture/jga-321-self-improvement-design.md`.
21. **the home server rollout, after PR #109 merges and a `bud update`:**
    1. Create a fine-grained GitHub token with Contents and Pull requests read/write on jgarner9/bud and the overlay. Add it to the Bud vault and put its `op://` ref in `~/.config/bud-server/improve.env` (template: `server/improve/improve.env.example`, mode 0600).
    2. Add a `bud-proposal` label in both repos.
    3. `cp server/improve/bud-improve.{service,timer} ~/.config/systemd/user/ && systemctl --user daemon-reload && systemctl --user enable --now bud-improve.timer`
    4. `systemctl --user start bud-improve`, then `journalctl --user -u bud-improve`, `bud improve list` and `bud improve show`. The first digest goes out on the next Sunday after 10:00.

### Approvals and privacy routing (JGA-377 to JGA-382, PRs #106, #108, #110, #112, #113, #114)

22. **the home server rollout (PR #114 merged 2026-11-22 as 39bdd7a; do item 24 first).** These steps change how the home server behaves. Step 3 is the one that sends plain private chats to Go.
    1. Update bud on the home server. Content format is now 8, and an older bud refuses the new content.
    2. Promote the edge content cut from the merge: `gh workflow run content.yml -R jgarner9/bud -f mode=promote -f version=<v>`.
    3. Run `bud sync`. It writes:
       - `router-p0-servers.json`
       - the Hermes tiers with `when` and `trusted`
       - the gateway approvals with `when`
       - Hermes's config with `extra_headers: X-Bud-Privacy: P1`
    4. Restart the router, the gateway, and `hermes gateway restart`. The router refuses to start without `router-p0-servers.json`.
    5. **Check:**
       - The router's journal says `N privacy rules, 2 P0-only servers`.
       - `bud doctor` shows "gateway approvals" OK.
       - On Signal:
         - `ls` and `curl` run without asking, and `git push` still asks.
         - A Linear write asks once, not twice.
         - An event with no attendees and "list my cron jobs" run alone.
         - A plain question gets a fast answer, and the router's journal names a Go model.
         - A message starting with `#private` stays local.
         - "What's on my calendar today" followed by a follow-up shows "a call to mcp_calendar_... raised", then "an earlier request of this conversation".
       - If every chat still logs as P0, Hermes 0.23.2 lacks `extra_headers`. That fails closed, so tell me and I'll find another way.
23. **Overlay rules for you to add** (they need your address and calendars, so they can't live in the public repo):
    - **Mail to yourself runs alone (Suite D131, JGA-392):** the overlay PR adds `{ "tool": "mail_send_message", "effect": "allow", "args": { "to": { "equals": ["<your address>"] }, "cc": { "empty": true }, "bcc": { "absent": true } } }` under `bud.mcp.mail.approval.when` in `env/personal.jsonc`. Fill in the `email` field of the 1Password item "iCloud" before merging (exactly as written there, lower case, no name), then `bud sync` and restart `bud-mcp-gateway`. An approval block with only `when` adds to the public tools. Check: a note to yourself sends with no prompt, and one that copies anyone else still asks.
    - **Shared calendars:** if you have any, add an `ask` rule on `calendar_create_event` with `"calendar": { "glob": "<name>" }`, since a shared calendar notifies its members.
24. **To hold P1 back** after updating, put `qwen-35b` first in the overlay's `roles.manager.pool`. P1 chats then stay on Qwen, while everything else ships. **Chosen 2026-11-22 18:33 UTC ("Merge, hold P1 on the home server"):** #114 is merged as 39bdd7a; do this overlay change before the step 3 sync, and keep it until memory records carry a tier.
25. **Known gaps after #114 (decision; my recommendation follows each):**
    - ~~**Bud memory search results** can hold private records and now reach Go in P1 chats.~~ **Decided 2026-11-22 (Suite D130, JGA-389, PR #116):** Hermes reaches memory at P0, and the router keeps a chat that recalls a P0 record local. See item 29.
    - ~~**A delegated subagent (JGA-390)** gets its own conversation id, so P0 data the parent writes into its task prompt isn't held.~~ **Decided 2026-11-24 (Suite D135), built 2026-11-26:** subagents go to the router's always-P0 `/p0/v1` endpoint. See item 35.

### KB curator and Hugo site (JGA-311, JGA-312, PR #115)

26. **Decisions taken on recommendation (Suite D122 to D124).** The KB layout and site (D122); the curator asks local models only, never the router, promotes P1 and P2 only while bud-kb's remote is GitHub, and keeps its state outside the KB so a revert sticks (D123); sections are flagged at 6 months and archived at 12, and the review queue is `data/review.json`, cleared with `bud curator resolve`, with no Signal prompts (D124). One change found while testing: Hugo reserves the front-matter key `kind`, so pages say `"page_kind": "system"` instead. Say if you want any of this reversed.
27. **the home server rollout, after PR #115 merges and a `bud update`.** Detail in `server/hugo/README.md` and `server/curator/README.md`.
    1. Install Hugo 0.146 or newer from Hugo's GitHub releases `.deb` (the distro package may be older). The standard edition is enough.
    2. Make bud-kb if it doesn't exist yet (a private GitHub repo with a first commit, say `content/_index.md` holding `{"title": "Bud KB"}`), add a deploy key with write access, and clone it to `~/.local/share/bud/kb` with that key.
    3. Build once: `bud kb-site --once`. It prints the commit it built.
    4. Copy `server/hugo/kb-site.env.example` to `~/.config/bud-server/kb-site.env`, set `BUD_KB_LISTEN` to the home server's tailnet IP with port 8799, then install and start `bud-kb-site.service` with the commands at its top. Open `http://<tailnet IP>:8799/` from the laptop.
    5. Make a curator token (a long random string) and add it to the JSON in the Bud Memory item's `clients` field with `"actions": ["export"], "tiers": ["P1", "P2"]`, add the token itself as a `curator` field on that item, and restart `bud-memory`.
    6. Put `BUD_MEMORY_TOKEN_CURATOR=op://vault/Bud Memory/curator` in `~/.config/bud-server/curator.env`, mode 0600.
    7. Run it once by hand, with the Qwen box awake: `op run --env-file ~/.config/bud-server/curator.env -- bud curator run`. It prints a summary line; `git -C ~/.local/share/bud/kb log` shows its commits. Nothing gets promoted until two records agree on a fact, so an empty first run is normal; `bud curator promote <record id>` pushes one through.
    8. Install and start the timer: `cp server/curator/bud-curator.{service,timer} ~/.config/systemd/user/ && systemctl --user daemon-reload && systemctl --user enable --now bud-curator.timer`. It runs at 03:30; `journalctl --user -u bud-curator` shows each night.
28. **Later, your call:** moving bud-kb's remote onto the home server (a bare repo over SSH) would let P0 facts into the KB, by adding P0 to the token's tiers and the unit's `--tiers`. Recommendation: wait until you've seen what the curator writes from P1 and P2 for a few weeks.

### Memory privacy (JGA-389, Suite D130, PR #116)

29. **the home server steps once PR #116 is merged** (after item 22's steps):
    1. Update bud from main again (the nightly or a build from main, as in item 22; no release includes #116 yet), promote the edge content cut from the #116 merge (format 9) with `gh workflow run content.yml -R jgarner9/bud -f mode=promote -f version=<v>`, then `bud sync`. Check that `~/.hermes/config.yaml` shows `X-Bud-Privacy: "P0"` under `mcp_servers.memory.headers`.
    2. Restart the router, then Hermes, so both pick up the change. Restart memory-mcp too, so its answers carry the marker.
    3. Re-check the records you've already approved, since Hermes saved them at P1, even ones from chats that read your calendar. You can see them with a `memory_search` from Hermes once it's at P0, since each result shows its tier. `bud memory-review retier <id> P0` (Suite D136, item 36) re-tiers one, asked on Signal like memory-review.
    4. Test: in a Signal chat, ask Hermes something that recalls a P0 record. The router log should say "a private Bud memory record in the history raised a P1 request to P0", and the reply should come from Qwen.
    5. Then take out the item 24 hold (remove `qwen-35b` from the front of the overlay's `roles.manager.pool`), and `bud sync` again.

### Spend views (JGA-391)

30. **After the JGA-391 PR merges and the next nightly:**
    1. On the laptop: `bud update`, then `bud sync`. `~/.config/opencode/opencode.jsonc` now has a `cost` block on each Go model, and `opencode stats --cost --models --days 7` shows dollars per model.
    2. On the home server: update bud from main as in item 22, then restart `bud-router` and `bud-mcp-ops`. The ledger gains `tokens` and `hours` with the router's next charge; until then `ops_spend` shows zero tokens and windows.

### Orchestrator handoffs (JGA-394, Suite D132)

31. **Starting the next orchestrator after a handoff.** Set up with the steps in `server/resume/README.md` (tmux, opencode, a checkout with push access and `gh`, then the timer). Two calls for you:
    - **tmux or headless.** Built as tmux on the recommendation: asks wait for you to attach and are refused after 10 minutes. Headless `opencode run` would reject every ask, so the session couldn't commit or push.
    - **Whether `bud resume queue` runs alone.** The base rules ask for it like any unlisted command, so an unattended session's `/handoff` would time out and print the prompt instead of queueing it. **You said yes on 2026-11-24:** jgarner9/bud-overlay#11 adds `{ "action": "shell", "resource": "bud resume queue *", "effect": "allow" }` to the overlay's `env/personal.jsonc`, for you to merge after #122, then `bud sync`.

### Resumed orchestrator's Go key (JGA-411)

The timer's tmux session had no `OPENCODE_GO_API_KEY`, so a resumed session failed with "OpenCode Go requires an API key". `bud resume` now resolves `opencode.env` with `op read` and the service account in `op-service-account.env`. On archie after the update: reinstall the unit (`server/resume/bud-resume.service` has new flags) and `systemctl --user daemon-reload`. That service account must be able to read the Go key's item; if it can't, `bud resume status` shows op's error.

### JSONC parsing (BASE D153)

32. **The one stdlib exception.** You granted a D1 exception in chat overnight ("I'm giving a D1 exception here") for a tested JSONC library, and the swap was done on it: `StripJSONC` now wraps github.com/tidwall/jsonc. The library the note named didn't exist; tidwall's is the only established one that keeps byte offsets and line breaks, which `bud registry refresh --write`'s in-place editing relies on. If you'd rather keep the homegrown stripper — its error for an unterminated comment was friendlier — say so and the PR reverts.

### OpenCode-only follow-ups (Suite D139)

33. **The Linear policy rename is untested against a live permission request.** JGA-406 (PR #143) found that the Linear rules in `issue.lua` and `work.lua` matched `mcp__<server>__<tool>` names, which OpenCode never sends, and renamed them to OpenCode's `linear_<tool>` names. Nothing has yet run a real ACP permission request through them. Next time you file or work an issue from the editor (`<leader>ai`, `:BudIssues` w), check that Linear reads run without a prompt and writes ask as before; tell me if either is wrong.
34. ~~**Claude-era wording outside this repo.**~~ **DONE 2026-11-26** (Suite D141, your approval in the project chat). `jgarner9/bud-plugins` is archived: nothing in this repo loads it (the skills live in `content/skills/`), and the only references left are dated history in `BASE.md`, `handoff/decisions.md`, `handoff/design/` and `handoff/memory/`, kept verbatim under D139. The overlay's living handoff docs now say OpenCode and Hermes (overlay PR #13); its `handoff/memory/` and `revisit-raw.md` stay verbatim as history. If your Claude surfaces still sync the `bud-plugins` marketplace, remove it there.

### Subagents at P0 (JGA-390, Suite D135)

35. ~~**Rollout check on the home server, with item 22 or 29's sync.**~~ **DONE 2026-11-26.** `bud sync` writes a `delegation:` block into the private Hermes profile (`base_url` is the router's `/p0/v1`). Jason watched the router's port with `sudo tcpdump -i any -A -s 0 -l 'tcp port 8795' | grep --line-buffered -oE 'POST /[a-z0-9/_.-]*'` while asking Hermes on Signal to "use a subagent to list three tools you have". Hermes's own turns arrived as `POST /v1/chat/completions` and the subagent's three requests as `POST /p0/v1/chat/completions`, so Hermes 0.23.2 does read `delegation.base_url`. The router doesn't log request paths, so its journal can't show this; use the capture above if it needs checking again. Hermes also sent one `POST /p0/api/show` (an Ollama-style model probe); the router answers 404 to it, which Hermes tolerates. A P1 chat that delegates is now local for the subagent, so expect it slower.

### Re-tiering approved records (JGA-396, Suite D136)

36. **Three steps on the home server before `retier` works** (item 29 step 3 needs them), in this order. First, copy the memory service's files from a current checkout, because archie runs a copy in `~/.local/lib/bud/memory/` that `bud update` doesn't touch and an old copy rejects `"retier"` as an unknown action (`ValueError: invalid client policy`, then `start-limit-hit`, so memory is down): `cp ~/dev/bud/server/memory/{main,service,store,history,vectors,retrieval}.py ~/.local/lib/bud/memory/`. Second, add `"retier"` and `"export"` to the reviewer token's `actions` in the Bud Memory item's `clients` field (the policy shape is in `server/memory/README.md`), then `systemctl --user restart bud-memory` and check `ActiveState`, `SubState`, `NRestarts` and the journal, not `is-active`, which can say `active` mid-crash-loop; the service refuses a policy that gives one token both `stage` and `retier`. Third, update bud from main so `bud memory-review retier` exists, and create `~/.config/bud-server/memory-review.env` with the reviewer `op://` reference if it isn't there. Done 2026-11-26. Then, for each record that should be private: `op run --env-file ~/.config/bud-server/memory-review.env -- bud memory-review retier <id> P0`, and answer the Signal prompt. The prompt shows the whole record; his no or no answer changes nothing.

### Secrets for sessions (JGA-412 to JGA-421, Suite D142 to D148)

37. **Four steps, in this order, once the PR is merged.** Design: `design/op-run-access.md`; how it works: `docs/internals/secrets.md`. (1) *Measure*, on each device (JGA-412): start `bud agent` in a project and ask the session to run `env | cut -d= -f1 | sort`, `command -v op security` and `test -r ~/.config/bud-server/op-service-account.env && echo token-file-readable`, then record the names it prints (never values) in the design doc's table. (2) *Mac Keychain* (JGA-416): `security add-generic-password -a "$USER" -s bud-op-service-account -U -w` asks for the Bud Vault service account's token at a hidden prompt. Then close the 1Password app, run `bud doctor --net` (it should name the Keychain and one vault), and start `bud agent`: nothing should ask you to sign in. Also try adding `-T` with the path of bud to the item and see whether a different process's `security find-generic-password -s bud-op-service-account -w` prompts, and whether `bud update` re-prompts; record both as D149. (3) *Home server* (JGA-420): `bud update`, `bud sync`, `bud doctor --net`, then in a session `bud secrets run --secret GITHUB_PERSONAL_ACCESS_TOKEN=GH_TOKEN -- gh api user --jq .login` should print the login, `bud secrets run --secret EXA_API_KEY -- printenv` should be refused, `op read` should be refused, and `audit.jsonl` should hold a line for each with no values. (4) *Mac* (JGA-421): the same checks with the 1Password app locked.
