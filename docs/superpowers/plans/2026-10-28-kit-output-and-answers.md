# Kit output style and answer ladder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** The kit ships a `bud` output style (terminal-style task logs, `▸ why` when earned), a `lookup` skill (answer ladder + Basic Memory docs cache) and an updated `kit/CLAUDE.md`.

**Architecture:** Markdown in `kit/` plus one new kit kind, `output-style`, in `internal/kit`: files sync to `<ClaudeDir>/output-styles/` like agents, and `Sync` sets `outputStyle` in `<ClaudeDir>/settings.json` unless the user picked another, recording that in the ledger so `Unsync` can undo only its own change.

**Tech Stack:** Go stdlib (D1), JSON (D2), Markdown, Basic Memory MCP.

**Linear:** Bud IDE (team JGA), JGA-141..144

**Spec:** `docs/superpowers/specs/2026-10-28-kit-output-and-answers-design.md`

## Global Constraints

- Go standard library only; files bud rewrites go through `internal/atomicfile.WriteFile`.
- Never touch `~/.claude/settings.json` directly: only `env.ClaudeDir`'s (bud's dir under D73).
- A user's own `outputStyle` always wins; a user-edited managed file is never overwritten (D56).
- Symbols, exactly: `●` `✎` `+` `−` `✓` `✗` `→` `?` `▸ why` `◇`. No emoji.
- Cache shelf lives: 30 days versioned library docs, 7 days current events/prices/releases, 90 days standards/specs.
- Cache notes under `cache/<library-or-topic>/<question-slug>`, `type: cache`, body under ~300 words.
- Fenced `bud-issues` / `bud-filed` / `bud-work` blocks the editor parses must not change shape.
- Commits: lowercase conventional, one per task, with the Linear id.

## Review Focus

- `settings.json` that isn't valid JSON: sync must report an error for `outputStyle` and leave the file byte-for-byte untouched (test in Task 2).
- `settings.json` with other keys (permissions, hooks): all survive a sync and an unsync (test in Task 2).
- User switches style to something else after bud set `bud`: unsync must not remove it (test in Task 2).
- `bud kit disable bud` (style disabled on this machine): neither the file nor the setting is written (test in Task 2).
- No `settings.json` and no `ClaudeDir` yet (fresh dir): sync creates both (test in Task 2).

---

### Task 1: `lookup` skill, `kit/CLAUDE.md`, conventions note (JGA-141)

**Files:**
- Create: `kit/skills/lookup/SKILL.md`
- Modify: `kit/kit.json` (add `"lookup"` to `skills`)
- Modify: `kit/CLAUDE.md`
- Modify: `kit/skills/bud-dev/SKILL.md` (one line: task reports use the `bud` output style's log format)
- Basic Memory: note "Knowledge Base Conventions" (project `main`)

**Interfaces:**
- Produces: skill name `lookup`; cache path scheme `cache/<topic>/<slug>` used by the style's `◇` line in Task 3.

- [x] **Step 1: Write `kit/skills/lookup/SKILL.md`.** Frontmatter `name: lookup`; description triggers on factual/theoretical/"how do I"/"what's the latest" questions and on needing a fact mid-task. Body: the four rungs exactly as spec Part 2 (rung-1 test: "if you can't say why it couldn't have changed since training, it isn't rung 1"); Basic Memory retrieval per the conventions note (`search_notes` short keywords, `page_size` 3-5, `cache/` first, vector retry once); stale hit = lead, refresh it; Context7 then Exa; write-back template:

  ```markdown
  ---
  title: <topic>: <question gist>
  type: cache
  source: <context7 id or URL>
  version: <if known>
  last_verified: <today>
  stale_after_days: <30|7|90>
  ---
  # <topic>: <question gist>
  <answer distilled, snippet that answers it, < ~300 words>
  ## Sources
  - <links>
  ## Relations
  - about [[<library or topic>]]
  ```
  Plus: update in place with `[superseded YYYY-MM-DD]`; never cache secrets, private code or Linear contents; end answers with `◇ <rung> <source> · cached → cache/<topic>/<slug>` (or `· from cache (verified <date>)`).

- [x] **Step 2: Rewrite `kit/CLAUDE.md`.** Keep "When you're running inside bud" and "Use the tools you have" (tightened; add code-index, Linear, Claude Docs only when a doc is asked for, Headroom retrieval). Add sections "Answering questions" (ladder in four lines, source line, use `lookup`), "Doing work" (one task at a time, one commit per issue with its id, In Review on commit, Done after release, ask only on real decisions), "Planning" (brainstorming → writing-plans → plan-to-linear; repo holds spec/plan, Linear holds status).

- [x] **Step 3: Add `"lookup"` to `kit/kit.json` `skills` and the line to `bud-dev`.**

- [x] **Step 4: Verify**

  Run: `GOCACHE=$TMPDIR/gocache go test ./internal/kit ./internal/cli && just run kit sync --dry-run`
  Expected: PASS; dry run lists `add skill lookup/SKILL.md` and `update instructions CLAUDE.md`.

- [x] **Step 5: Edit "Knowledge Base Conventions"** with `edit_note`: under Folders add ``- `cache/` — docs and web lookups Claude saved; one note per question, `type: cache`, short shelf life``; under Observations add `- [convention] cache/ holds Claude's lookups (Context7, Exa); stale ones are leads, refreshed in place (2026-10-28) #memory`. Add "cache/ 30/7/90" to the Freshness defaults line.

- [x] **Step 6: Commit**

  ```bash
  git add kit/
  git commit -m "feat: kit lookup skill and answer ladder, task and planning workflow in kit CLAUDE.md (JGA-141)"
  ```

### Task 2: `output-style` kit kind (JGA-142)

**Files:**
- Modify: `internal/kit/kit.go` (`Kit`, `Load`, `Ledger`, `without`, `files`, `Sync`, `Entries`)
- Modify: `internal/kit/edit.go` (`Kinds`, `entryPath`, `Scaffold`)
- Test: `internal/kit/kit_test.go`, `internal/kit/edit_test.go`
- Modify: `BASE.md` (D75), `CLAUDE.md` (kit paragraph), `docs/issues.md` or the kit section of `docs/config.md` wherever kit kinds are listed

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces:
  - `kit.json` keys `"output-styles": ["bud"]` and `"output_style": "bud"` (the one to turn on).
  - `Kit.OutputStyles []string`, `Kit.OutputStyle string`.
  - `Ledger.OutputStyle string \`json:"output_style,omitempty"\`` — the value bud wrote to settings.json, "" if it didn't.
  - `Kinds["output-style"] = "output-styles"`; `entryPath("output-style", n) = "output-styles/" + n + ".md"`.
  - `Entries()` rows `{"output-style", name, ""}`; `Action{"output-style", "settings.json", do, why}` for the setting.

- [x] **Step 1: Write failing tests** in `kit_test.go`, each with a temp `claudeDir` and `fstest.MapFS` holding `kit.json` `{"output-styles": ["bud"], "output_style": "bud"}` and `output-styles/bud.md`:
  - `TestSyncOutputStyle`: no settings.json → after `Sync`, `<claudeDir>/output-styles/bud.md` exists and `settings.json` decodes to `outputStyle == "bud"`; ledger `OutputStyle == "bud"`; second sync's action for `settings.json` is `ok`.
  - `TestSyncKeepsOtherSettings`: settings.json `{"permissions":{"allow":["Bash(ls)"]},"outputStyle":"bud"}` pre-written by the user (ledger empty) → after sync, `Ledger.OutputStyle == ""` (bud didn't write it); after `Unsync`, `permissions` is unchanged and `outputStyle` is still `"bud"`.
  - `TestSyncUserOutputStyleWins`: settings.json `{"outputStyle":"Explanatory"}` → sync action `skip` with why `you picked Explanatory`, file unchanged.
  - `TestUnsyncOutputStyle`: sync, then user changes to `"Explanatory"`, `Unsync` → still `"Explanatory"`; separately sync then `Unsync` with no change → key gone, other keys kept, style file removed.
  - `TestSyncBadSettings`: settings.json `{not json` → `Sync` returns an error mentioning `settings.json`; file bytes unchanged; style file still synced.
  - `TestSyncOutputStyleDisabled`: `Env.Disabled = ["bud"]` → no file, no settings.json.
  - In `edit_test.go`: `Scaffold("output-style", "terse")` writes `output-styles/terse.md` with `name: terse`, `keep-coding-instructions: true`, and lists it under `"output-styles"`.

- [x] **Step 2: Run** `GOCACHE=$TMPDIR/gocache go test ./internal/kit` — Expected: FAIL (unknown fields / missing files).

- [x] **Step 3: Implement.**
  - `Load`: decode `output-styles` and `output_style`.
  - `files()`: add `{"output-style", "output-styles", k.OutputStyles}` to the agents/commands group.
  - `without`: filter `OutputStyles`; clear `OutputStyle` if its name is disabled.
  - `Sync`, after the instructions block and before the ledger save: `syncOutputStyle(k.OutputStyle, env, ledger, dryRun) (Action, error)` in the same file. Read `<ClaudeDir>/settings.json` into `map[string]any` (missing = empty; invalid JSON = error, no write). Current `cur, _ := m["outputStyle"].(string)`. Decide: want `k.OutputStyle`; if `want != ""` and (`cur == ""` or `cur == ledger.OutputStyle` or `cur == want`) → set (`add`/`ok`), record `ledger.OutputStyle = want`; if `want != ""` and `cur` is something else → `skip` "you picked <cur>", ledger "" ; if `want == ""` and `ledger.OutputStyle != ""` and `cur == ledger.OutputStyle` → delete key (`remove`), ledger "". Write with `json.MarshalIndent(m, "", "  ")` + newline via `atomicfile.WriteFile`, `MkdirAll(ClaudeDir)` first. Recording only when bud wrote it means a user's own `"bud"` is never removed.
  - `Entries`: add output-style rows.
  - `edit.go`: `Kinds`, `entryPath`, `Scaffold` body `"---\nname: " + name + "\ndescription: TODO: what this style does\nkeep-coding-instructions: true\n---\n\nTODO: how replies should look.\n"`.

- [x] **Step 4: Run** `GOCACHE=$TMPDIR/gocache go test ./internal/kit ./internal/cli` — Expected: PASS.

- [x] **Step 5: Docs.** `BASE.md` D75 row ("Kit ships output styles; `bud kit sync` turns one on in bud's Claude config dir unless you picked another", JGA id, 2026-10-28). Root `CLAUDE.md` kit paragraph: one sentence on `output-styles` and `Ledger.OutputStyle`. Kit docs: add the kind where kinds are listed. Run `just check` — Expected: all green (smoke may need network; report if skipped).

- [x] **Step 6: Commit**

  ```bash
  git add internal/kit BASE.md CLAUDE.md docs
  git commit -m "feat: kit output styles, bud kit sync turns the kit's style on unless you picked one (JGA-142)"
  ```

### Task 3: the `bud` style and skill closings (JGA-143)

**Files:**
- Create: `kit/output-styles/bud.md`
- Modify: `kit/kit.json` (`"output-styles": ["bud"]`, `"output_style": "bud"`)
- Modify: `kit/skills/file-issue/SKILL.md`, `kit/skills/plan-to-linear/SKILL.md` (closing summary only)

**Interfaces:**
- Consumes: Task 2's `kit.json` keys; Task 1's `◇` source line and cache paths.

- [x] **Step 1: Write `kit/output-styles/bud.md`.** Frontmatter `name: bud`, `description: Terminal-style task logs; prose for questions; a ▸ why block when it's earned`, `keep-coding-instructions: true`. Body: spec Part 1 verbatim in substance: when a reply is a task reply; the example log; symbol table; repo-relative paths; `?` always last; `▸ why` triggers (asked, check failed, step skipped, judgment call, surprise) and "a few indented lines"; questions get prose led by the answer plus the `◇` line; planning skills keep their own format; no emoji; narrow pane.

- [x] **Step 2: Skill closings.** `file-issue`: after filing, the human-readable line(s) outside the `bud-filed` block use `→ JGA-N filed: <title>`. `plan-to-linear`: its final report is a log (`+ JGA-N <title>` per issue, `→` for state changes). Don't touch fenced block formats.

- [x] **Step 3: Verify** `GOCACHE=$TMPDIR/gocache go test ./... && just run kit sync --dry-run` — Expected: PASS; dry run shows `add output-style output-styles/bud.md` and `add output-style settings.json`.

- [x] **Step 4: Commit**

  ```bash
  git add kit/
  git commit -m "feat: bud output style in the kit: terminal-style task logs, why blocks when earned (JGA-143)"
  ```

### Task 4: sync and check in a real session (JGA-144)

- [x] **Step 1:** `just run kit sync` (real sync into bud's config dir; the user runs it if the sandbox blocks `~/.local/share/bud/claude`). Expected: `outputStyle` is `"bud"` in `~/.local/share/bud/claude/settings.json`, and `output-styles/bud.md` and `skills/lookup/SKILL.md` are there.
- [x] **Step 2:** In `bud agent`, ask for a tiny task, then ask a versioned question (e.g. a Neovim 0.12 API). Expected: the task reply is a log; the question reply ends with a `◇` line; a `cache/` note exists in Basic Memory afterwards.
- [x] **Step 3:** Check a headless chat-pane session too (`<leader>ac` off, chat pane on): the style applies there, or note in D75 that `-p` sessions ignore it and the CLAUDE.md lines carry the format instead.
- [x] **Step 4:** Move the issues to In Review, and update local memory: the workflow now lives in the kit.
