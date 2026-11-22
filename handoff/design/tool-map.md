# Bud Suite: tool map by device profile

Drafted 2026-11-16 for JGA-355. Status: **decided** for the MCP servers and additions (D72-D90 in the architecture doc's log), Jason, 2026-11-15 and 2026-11-16. Jason approved the whole map, including the built-in toolset on/off picks in the Hermes and OpenCode tables, on 2026-11-16 ("done, i approve"). JGA-358 renders them into the configs. Jason confirmed on 2026-11-16 that `local` is the Qwen box.

Sources: the Hermes toolset reference (`toolsets.py` and `website/docs/reference/tools-reference.md` in NousResearch/hermes-agent), the OpenCode V2 permissions and web search docs (opencode.ai/docs/permissions, /websearch, /policies), and the architecture doc. The facts were read through Context7 on 2026-11-16.

## How to read this

- **Profiles.** The overlay has three profiles: `dev`, `server` and `local`. `local` is the Qwen box (confirmed by Jason, 2026-11-16).
- **Groups.** These are the JGA-355 groups. *Server only* means the tool connects on the home server and is used only there. *Gateway* means the home server holds the connection and the key, and devices reach it through the Bud MCP gateway on the tailnet. *Local direct* means each device connects to it itself.
- **Tiers.** Privacy runs P0 (private), P1 (internal) and P2 (public). Actions follow D10 and D16: run alone, ask in the editor, approve by phone, or never.
- **P0 tool rule.** This one is proposed. A P0 chat may use only tools that never leave the house: files, shell, Bud memory and Serena. The gateway and OpenCode's permissions deny every cloud tool in a P0 session.

## The Bud MCP gateway (approved)

This is a small standard-library Go service on the home server, and it listens on the tailnet only. It forwards MCP calls to Linear, Exa and later additions. It adds each server's key from `op run`, so devices hold no MCP keys. It logs every call to the audit log (JGA-348), and it refuses cloud tools for P0 sessions. Devices connect to it as a remote MCP server, at a URL that lives in the overlay.

---

## Profile `server` (the home server)

### Hermes, the 24/7 manager: built-in toolsets

| Toolset | On? | What it's for | Privacy, action tier |
| --- | --- | --- | --- |
| `terminal`, `file` | On, inside Docker (D48) | Chores, scripts, reading and writing in its workspace | P0-safe. D16 rules apply: read-only commands run alone, the rest ask. |
| `code_execution` | On, sandboxed | Quick calculations and data wrangling | P0-safe. Runs alone. |
| `web` (search and extract) | On, using the Exa backend with `keyless_fallback: false` | Research and current facts. Hermes has Exa built in, so it doesn't need the Exa MCP. | P2 only. Runs alone. The keyless fallback is turned off, so searches never quietly go to other providers. |
| `browser` | On, inside Docker | Pages that need a real browser, such as logins, forms and JavaScript sites | P2. Reading runs alone. Submitting a form needs approval by phone. `browser_vault` stays off (D10). |
| `memory`, `session_search` | On, through the Bud memory provider (D51) | Working memory and recalling past chats | P0-safe. Runs alone. |
| `skills` | On, with synced skills locked (D48) | Using Bud's skills. Any new skill Hermes writes waits for Jason's review. | Writes are staged for review. |
| `cronjob` | On | Digests, the nightly curator trigger, reminders | Creating a job asks first. Running one follows the tier of what it does. |
| `delegation`, `todo`, `clarify` | On | Subtasks, planning, asking Jason | Run alone |
| `vision` | On | Screenshots and photos Jason sends, using the registry's vision role | Follows the privacy tier of the image |
| `tts` | Off for now | Voice replies, if he ever wants them | none |
| `computer_use`, `image_gen`, `video`, `x_search`, `kanban`, `discord`, `feishu`, `debugging` | Off | Not needed. Off means less to attack and fewer tools in every prompt. | none |

Messaging (Signal, D30) and mail (D31) are Hermes gateway platforms and Bud plugins, not agent tools, so they aren't listed here.

### The planning OpenCode (D49): built-in tools

| Tool | On? | What it's for | Tier |
| --- | --- | --- | --- |
| `read`, `glob`, `grep` | On | Reading repos and the KB | Run alone |
| `edit` | Planning docs and KB drafts only | Writing specs and drafts | Runs alone inside its workspace, and asks outside it |
| `shell` | Read-only list (D16) | git log, tests, metrics | Follows D16 and D41 |
| `subagent`, `skill`, `question` | On | Planning workflows | Run alone |
| `webfetch` | On | Reading a known URL | P2. Runs alone. |
| `websearch` | On, with `exa` as the provider. It holds the key on the server, so it doesn't need the gateway. | Research while planning | P2. Runs alone. |

### MCP servers on `server`

| Server | Group | What it's for | Privacy, action | Credentials |
| --- | --- | --- | --- | --- |
| Bud memory (D51) | Server-hosted, and devices connect | Working memory for Hermes, the planning OpenCode and the laptops | P0-safe. Reads and writes run alone. | Tailnet only |
| Linear | Gateway | /plan-to-linear, triage, status updates | P1. Reads run alone. Writes ask. | `op://` API key |
| Exa | Gateway | Search for the laptops. On the server itself, the built-in web tools use Exa directly. | P2. Runs alone. | `op://` API key |
| GitHub | Server only, at GitHub's hosted server `api.githubcopilot.com/mcp/` (D94) | PRs from the D47 loop, CI status, the curator's bud-kb commits | P1. Reads and PRs run alone. Merges and pushes to main need approval by phone. | Fine-grained PAT, `op://`, sent as a bearer header |
| Context7 | Local direct | Library docs | P2. Runs alone. | None |
| 1Password | Not an MCP server | `op run` puts keys into each tool's environment (D7) | Values are never shown to the model | Service account, Bud vault |

---

## Profile `dev` (the laptop)

### OpenCode, the coding harness: built-in tools

| Tool | On? | What it's for | Tier |
| --- | --- | --- | --- |
| `read`, `glob`, `grep`, `edit` | On | Coding | Run alone inside the project. Paths outside it ask (`external_directory`). |
| `shell` | On | Builds, tests, git | D16, D41 and D43: tests, linters and read-only git run alone. `git commit` asks. Pushes to main need approval by phone. |
| `subagent`, `skill`, `question` | On | Coder and reviewer agents, Bud skills | Run alone |
| `webfetch` | On | Reading a known docs URL | P2. Runs alone. |
| `websearch` | **Off**. Search goes through the gateway's Exa instead. | Turning it off keeps the laptop free of keys and routes every search through the P0 gate. | none |
| `experimental_image_generate` | Off | Not needed | none |

### MCP servers on `dev`

| Server | Group | What it's for | Privacy, action | Credentials |
| --- | --- | --- | --- | --- |
| Bud memory | Server-hosted | Memory shared with Hermes, so the laptop and the manager know the same things | P0-safe. Runs alone. | Tailnet only |
| Linear (through the gateway) | Gateway | Filing and working issues from the IDE (file-issue, bud:build) | P1. Reads run alone. Writes ask. | None on the laptop |
| Exa (through the gateway) | Gateway | Web search and fetch | P2. Runs alone. | None on the laptop |
| Context7 | Local direct | Library docs | P2. Runs alone. | None |
| Serena | Local direct | Symbols, references and callers in the open project | P0-safe. Reads only. | None |
| GitHub | None, since `gh` and `git` are already signed in | | Through `shell` under D16 | The existing gh login |

---

## Profile `local` (the Qwen box)

There are no agent tools and no MCP servers. The box only serves models (llama-server) and is woken by the home server (D26). Nothing on it calls out.

---

## Retired or dropped

| Tool | Why |
| --- | --- |
| Basic Memory | Replaced by Bud memory (D13, D51). The existing notes move into bud-kb in phase 5. |
| Claude Docs, Artifacts, Have I Been Pwned | claude.ai connectors, and Bud is moving off Claude (D17) |

---

## Suggested additions

These are ranked by how much each one moves Bud toward a 24/7 manager, knowledge keeper and coding partner. Nothing here is decided.

### The power additions

| # | Addition | What it unlocks | Group, profiles | Privacy, action | Credentials | Cost to build |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | **Calendar and Reminders (iCloud CalDAV)** | This is the biggest gap for a manager. It covers "what's my week", morning briefs, reminders by Signal and scheduling around his day. | Server only (Hermes), so laptops ask the manager | **P0**, since a calendar is personal and should stay with Qwen. Reads run alone. Creating or moving events needs approval by phone at first. | `op://` app-specific password, like mail (D31) | Medium: a small Bud-owned CalDAV MCP in Go, or a vetted existing one |
| 2 | **Bud ops MCP** (Bud-owned) | Puts Bud's own state behind tools: `bud doctor`, sync status, router spend against the C2 cap, the approval queue and the review queue. Hermes can then answer "is everything healthy" and "what's waiting on me", and the D47 loop can see what it changed. | Server-hosted, and devices connect through the gateway | P1. Reads run alone. Nothing in it writes. | None, tailnet only | Small: wrappers over code that already exists |
| 3 | **KB search in the memory server** | One recall tool that searches the working memory and bud-kb, so "what did we decide about X" works from any chat. It extends D51's server rather than adding a new one. | Server-hosted | P0-safe. Runs alone. | None | Small to medium, and it lands with phase 5 |
| 4 | **SearXNG on the home server** | Free, keyless, private meta-search for Hermes (`web.search_backend: searxng`). It cuts Exa spend and keeps routine searches off a paid API. Exa stays for extraction and hard queries. | Server only | P2 queries, but no account is tied to them | None | Small: one container |

### Worth having

| Addition | What for | Group, profiles | Notes |
| --- | --- | --- | --- |
| Playwright MCP | OpenCode has no browser. This lets it test the docs site, the Tauri app's web view and any UI it builds. | Local direct, `dev` | P2. Runs alone on localhost. Approval needed for anything off localhost. |
| cocoindex-code (semantic code search) | "Where's the code that does X" in large repos. bud:tools already names it. | Local direct, `dev` | Only if Serena and grep aren't enough. It's local, so it's P0-safe. |
| Contacts (iCloud CardDAV) | "Text Mom" or "mail the plumber" without asking him for the address | Server only | P0. Reads only. Pairs with addition 1. |
| Home Assistant (Hermes catalog plugin) | Only if he runs Home Assistant | Server only | Every action needs approval by phone, since it touches the physical home |
| Headroom MCP (D53, optional) | Compressing large tool output if Go allowances run short | Server | Already noted. Measure first. |

### Considered and not recommended

| Tool | Why not |
| --- | --- |
| A 1Password MCP | It would expose secret reads, which D10 rules out |
| OpenCode's hosted web search from the Console | It's unverified whether Go Plus includes it, and it would bypass the gateway's P0 gate |
| ~~Finance tools (Dollarwise and the like)~~ | Superseded: Jason wants a small Dollarwise connector after 2.0 (D90): P0, Signal updates, obvious categories applied on its own and reported, never moves money. |

## Follow-ups this would file, once approved

1. Build the Bud MCP gateway (phase 2), including the P0 tool gate and the audit log hook (JGA-348).
2. Render the MCP servers and the built-in tool on/off settings into the OpenCode and Hermes configs from content `env/` and the overlay's `env/personal.jsonc`.
3. Fix the `op://` gap in env fragments (low priority, since nothing local needs a key).
4. Each approved addition gets its own issue.
