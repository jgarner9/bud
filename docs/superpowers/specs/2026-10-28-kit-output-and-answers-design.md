# Kit: terminal-style output, an answer ladder and a docs cache

2026-10-28. Ships in the Claude Code kit (`kit/`), so it reaches every machine and every session bud starts (the TUI, the chat pane, headless project sessions).

## Intent

1. **Replies that look like a terminal when Claude is doing work.** Fixing code and filing issues today end in prose. The user wants a short, consistent log of what changed, with explanation only in a labeled block, shown when asked or when Claude judges it's needed. Questions and planning keep normal prose.
2. **Answers come from the cheapest source that is certainly current.** A fixed order: Claude's own knowledge (only when it can't have gone stale), then Basic Memory, then Context7, then Exa. Whatever Context7 or Exa returns is written back to Basic Memory, so the next lookup stops at rung 2.
3. **The kit's instructions cover code, questions and planning,** including the task workflow that today lives only in one machine's local memory.

## Goals

- One output style, `bud`, on by default in bud's Claude Code config dir (D73).
- An answer ladder every session follows, with a one-line note of where each answer came from.
- A docs cache in Basic Memory that uses the user's existing freshness fields.
- `kit/CLAUDE.md` updated: answer ladder, task workflow, planning flow, all connected tools.

## Non-goals

- No change to how Claude works (which tools it uses, how it tests), only to how it reports. The style keeps Claude Code's coding instructions (`keep-coding-instructions: true`).
- No new MCP servers, no bud-side cache store (D26: bud stores no keys and has no API client; the cache lives in Basic Memory).
- Not touching `~/.claude/settings.json`. The style is set only in the config dir bud syncs into.

## Anti-goals (failure even if it works)

- Emoji, or symbols that break in a narrow Neovim pane.
- A `▸ why` block on every reply. It's only worth having if it's rare.
- Answers from memory about anything versioned or current: library APIs, CLI flags, prices, releases, who runs what.
- The cache growing into full doc dumps that cost more to read than calling Context7 again.
- Overwriting a style the user picked themselves.

## Part 1: the `bud` output style

`kit/output-styles/bud.md`:

```markdown
---
name: bud
description: Terminal-style task logs; prose for questions; a ▸ why block when it's earned
keep-coding-instructions: true
---
```

The body sets these rules:

**Task replies** (anything that changed files, ran commands, or touched Linear/git):

```
● fix: dashboard drops last project row (JGA-140)
  ✎ nvim/lua/bud/dashboard/view.lua   +4 −2   row clamp was off by one
  + tests/lua/dashboard_test.lua      +12     narrow-terminal case
  − nvim/lua/bud/old.lua                      unused since D45
  ✓ just lua-test   38 passed
  ✗ just smoke      no network (sandbox)
  → JGA-140 → In Review · commit 1a2b3c4
  ? which branch should this land on?
```

| Symbol | Meaning |
|---|---|
| `●` | header: what the task was, one line |
| `✎` `+` `−` | file edited / added / removed, with line counts and a few words of why |
| `✓` `✗` | a check that passed / failed, with the count or the reason |
| `→` | a state change: issue status, commit, push, release, sync |
| `?` | something the user has to answer, always last |

Paths are repo-relative so they stay clickable. No prose paragraphs before or after the block.

**`▸ why` block**, after the log, only when:
- the user asked for an explanation, or
- a check failed, a step was skipped, Claude made a judgment call the user would want to know about, or something surprising turned up.

It's a few indented lines, never a second report.

**Questions and explanations** get normal prose, led by the answer, and end with a source line (Part 2):
`◇ context7 /websites/code_claude · cached → cache/claude-code/output-styles`

**Planning** (brainstorming, writing plans) keeps whatever format those skills set.

### Shipping it (new kit kind, D75)

- `kit.json` gets `"output-styles": ["bud"]` and `"output_style": "bud"` (the one to turn on); files live in `kit/output-styles/<name>.md`.
- `kit.Sync` copies each into `<ClaudeDir>/output-styles/`, with the same rules as skills: managed files are recorded in the ledger, and a file the user changed is never overwritten (D56).
- `kit.Sync` sets `"outputStyle": "bud"` in `<ClaudeDir>/settings.json` only when the key is missing or already `"bud"`. A user's own choice wins, and the ledger records that bud set it. `Unsync` removes the key only if it's still `"bud"` and the ledger says bud set it. The file is read as a raw JSON tree, so unknown keys survive, and written with `atomicfile.WriteFile`.
- `Kinds` in `edit.go` gets `"output-style": "output-styles"`, so `bud kit ls/status`, `Scaffold`, `Remove`, `Adopt` and `kit.disabled` work for it.
- Docs: `docs/config.md` if a key is added (none planned), the kit section of the docs, `BASE.md` D75, the `CLAUDE.md` kit paragraph.
- Tests in `internal/kit` follow the existing temp-dir pattern: sync writes the file and the setting, a user-set style survives, unsync removes only bud's.

## Part 2: the answer ladder

Applies to factual, theoretical and "how do I" questions, and to facts Claude needs mid-task.

1. **Claude's own knowledge**, only for things that don't change: language semantics, algorithms, maths, protocols by RFC, history, general concepts. Anything versioned, priced, released, or about current state skips to rung 2. If Claude can't say why the fact couldn't have changed since training, it isn't rung 1.
2. **Basic Memory.** `search_notes` (short keywords, `page_size` 3-5) over the `cache/` folder, then the rest of the knowledge base when the question is about the user's own projects or setup. A fresh hit answers the question. A stale hit (by `last_verified` + `stale_after_days`) is a lead, not an answer: go to rung 3 or 4 and refresh the note.
3. **Context7** for anything about a library, framework, SDK, CLI or service.
4. **Exa** (`web_search_exa`, then `web_fetch_exa`) for everything else current, or when Context7 has nothing.

After rung 3 or 4, write the result back to the cache (Part 3). The source line names the rung that answered.

The full procedure lives in a new skill, `kit/skills/lookup/SKILL.md` (Part 4), so `kit/CLAUDE.md` only needs a few lines.

## Part 3: the docs cache in Basic Memory

Uses the conventions in the user's "Knowledge Base Conventions" note (`last_verified`, `stale_after_days`, search before writing, notes under about 1,500 words).

- **Where:** `cache/<library-or-topic>/<question-slug>`, for example `cache/claude-code/output-styles`, `cache/neovim/vim-pack-lockfile`. The `cache/` folder is new, and a line about it is added to "Knowledge Base Conventions" (Part 5).
- **Frontmatter:** `type: cache`, `source` (Context7 library id or URL), `version` (when known), `last_verified` (today), `stale_after_days`: 30 for versioned library docs, 7 for web results about current events, prices or releases, 90 for standards and specs.
- **Body:** the answer distilled (the facts and the snippet that answers it, under about 300 words), the source links, and `[[library]]` relations so topics connect. Never a raw docs dump.
- **Refresh:** when a stale note is re-fetched, update it in place: bump `last_verified`, mark changed facts with `[superseded YYYY-MM-DD]` per the conventions note.
- **Recent lookups:** "what did I look up recently" is `recent_activity` on `cache/`, so no separate index note is needed.
- **Never cached:** secrets, anything from the user's private code, Linear contents.

## Part 4: skills

| Skill | Change |
|---|---|
| `lookup` (new) | The ladder (Part 2) and the cache rules (Part 3), with the exact tool calls and note template. Description triggers on questions, "how does X work", "what's the latest", and on needing a fact mid-task. |
| `bud-dev` | Point to the `bud` style for task reports. No other change. |
| `file-issue`, `plan-to-linear` | Their closing summaries use the task-log format, without changing the fenced `bud-issues`/`bud-filed` blocks the editor parses. |

No skill for planning: the chain is already brainstorming → writing-plans → plan-to-linear, and `kit/CLAUDE.md` names it.

## Part 5: `kit/CLAUDE.md` and the user's notes

`kit/CLAUDE.md` keeps its two sections, tightened, and gains:

- **Answering questions:** the ladder in four lines, the source line, and a pointer to `lookup`.
- **Doing work:** one task at a time, one commit per issue, the issue goes to In Review when committed and Done after release, and ask only on real decisions (moved from this machine's local memory, so every machine and headless session gets it).
- **Planning:** brainstorming → writing-plans → plan-to-linear; Linear is status, the repo holds the spec and plan.
- **Tools:** add code-index, Linear, Claude Docs (only when asked for a doc) and Headroom retrieval. Serena stays first for symbols, with "say so if it fails to connect" kept.

The user's Basic Memory "Knowledge Base Conventions" note gets one folder line and one `[convention]` observation for `cache/`. That's an edit to the user's own note; it's in the plan as its own step.

## Order of work

1. `lookup` skill + `kit/CLAUDE.md` update + conventions note line. Markdown only, works after one `bud kit sync`.
2. `output-styles` kit kind in Go, with tests, D75, docs.
3. `kit/output-styles/bud.md` and the closing-format tweaks to `file-issue`/`plan-to-linear`.
4. `bud kit sync` locally, then check a task reply and a question reply in a real session.

Each step is one Linear issue and one commit.

## Calls made (the user can overrule)

- The style is set only in bud's config dir, never `~/.claude/settings.json`. A plain `claude` run by hand keeps the default.
- Cache shelf lives: 30 / 7 / 90 days.
- One skill (`lookup`) for both the ladder and the cache, not two.
- Slimming the repo's root `CLAUDE.md` (it's very large) is left out of this work. It's a separate piece, worth its own issue.
- `?` lines go last in a task log, so a question is never buried.

## Left to the builder

- The exact wording of the style body and the skill, within the rules above.
- Ledger field names for "bud set outputStyle".
- Whether `bud kit ls` shows the style under its own heading or as a row kind.
