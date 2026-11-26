# Bud 2.0: OpenCode only, no Claude Code

Status: planned 2026-11-26 (Suite D139, BASE D154, taken on recommendation under Jason's standing rule). Nothing below is built yet; Linear (JGA, project "Bud Suite", milestone Bud 2.0) tracks each task.

## Goal

Bud 2.0 is built for Jason alone: OpenCode is the only local harness and Hermes is the server brain. Bud stops being backend-agnostic and stops naming Claude Code anywhere it still runs. Configuring Bud as a general LLM assistance engine for other people is the 3.0 milestone, so nothing here designs for it.

## What stays and what goes

The survey (2026-11-26, `git grep -i -E 'claude|anthropic|openclaw|codex|\bacp\b'` over the whole tree) found three kinds of hit.

**Goes (dead or Claude-only code):**

- Lua: `bud.ai.claude`, the IDE bridge (`bud.ai.bridge`, the WebSocket MCP server Claude Code's IDE integration used), the Claude path of `bud.ai.permissions`, and every non-OpenCode backend in `bud.ai.agent` (OpenClaw, Codex, custom, `claude`). Their tests and fixtures go with them (`claude_*_test.lua`, `bridge_test.lua`, `term_backend_test.lua`, `lib/claude_backend.lua`, `fixtures/openclaw/`).
- Go: `internal/claude`, `bud claude`, `claudedir.go`, `cli.UseClaudeConfig`, the Claude and Headroom steps of `bud install`, the Claude Code doctor checks, `paths.ClaudeConfig`, the `ai.backend`, `ai.claude.*`, `ai.openclaw.*` and `ai.custom.*` keys, and `ai.work.where = "cloud"` (it ran `claude --cloud`).
- The embedded `bud-ide` Claude Code plugin (`ide/plugins/bud-ide`, `internal/plugin`, `bud plugin`, `bud.Plugin()`); `content/skills/file-issue`, `content/skills/bud-dev` and `content/agents/chat.md` already carry it to OpenCode.
- User docs for the backend concept: `docs/claude-code.md`, `docs/claude-tui.md`, `docs/backends.md`, `docs/openclaw.md`, `docs/internals/claude-code.md`.

**Stays, renamed or reworded:**

- `bud.ai.acp` and `bud.ai.agent` stay, talking only to `opencode acp`. They are the programmatic session that `:BudGen`, issue filing (`<leader>ai`), working an issue (`:BudIssues` w), inline actions and commit drafts run on; opencode.nvim (D150) serves only the chat. This corrects the 2026-11-26 handoff survey, which listed `bud.ai.acp` as dead. The fake ACP agent in the tests stays.
- `bud chat` and the dashboard's chat entry run OpenCode with the synced `chat` agent instead of Claude Code's chat mode.
- `docs/internals/claude-session.md` becomes `docs/internals/session.md`, and `docs/internals/acp.md` describes only the OpenCode transport.
- The release skill reviews a release from a written checklist in an OpenCode session; Copilot stays the per-PR gate.

**Stays as is (not Claude nomenclature):**

- The Anthropic secret patterns in `content/env/registry.jsonc`, `scripts/gen-secret-rules` and `internal/registry/packs_test.go` (they detect leaked keys).
- The provider env names in `internal/adapter/hermes/hermes.go` and the Anthropic mention in `content/skills/add-provider`.
- History, verbatim (Suite D139 b): `handoff/decisions.md`, BASE.md's decision rows, `handoff/memory/`, `handoff/design/`, `handoff/notes/`, `docs/internals/jga-348-worklog.md`. Superseded decisions are marked, never rewritten.
- `.gitignore`'s `.claude/` line (harmless for anyone who still runs it by hand).

## Tasks

Each task is one PR off main, cold-startable by a Sonnet thread. Every PR runs the acceptance commands and pastes their output in its description, then follows the standing workflow: Copilot review, fix, reply and resolve, one summary comment naming the fix commits, merge. Each keeps `handoff/state.md` current in the same PR.

The order keeps dependent PRs from conflicting: the Lua side stops reading a config key before the Go side deletes it, and the docs passes run last so they describe the finished code.

| # | Task | Linear | After | Runs beside |
| --- | --- | --- | --- | --- |
| 1 | Release review without Claude Code | JGA-400 | none | 2 |
| 2 | Lua: the IDE talks only to OpenCode | JGA-401 | none | 1 |
| 3 | Lua: delete `bud.ai.claude` and the IDE bridge | JGA-402 | 2 | 1 |
| 4 | Go: drop `ai.backend` and the non-OpenCode backends | JGA-403 | 2 | 3 |
| 5 | Go: delete Claude Code support | JGA-404 | 3, 4 | |
| 6 | Delete the embedded `bud-ide` plugin | JGA-405 | 5 | 7 |
| 7 | Drop cloud work and Claude from working an issue | JGA-406 | 5 | 6 |
| 8 | User docs and site: OpenCode only | JGA-407 | 6, 7 | 9 |
| 9 | Living docs: AGENTS.md, delete CLAUDE.md, handoff, comments | JGA-408 | 1, 6, 7 | 8 |

The whole effort is done when this prints only the allowed hits listed under "Stays as is":

```sh
git grep -n -i -E 'claude|openclaw|codex' -- . ':!handoff/decisions.md' ':!handoff/memory' ':!handoff/design' ':!handoff/notes' ':!docs/internals/jga-348-worklog.md'
```

### 1. Release review without Claude Code

Rewrite `content/skills/release/SKILL.md` so an OpenCode session can cut a release: the code review and security review of everything since the last tag become a written checklist the session walks (correctness, tests for new behaviour, docs matching code, secrets and personal data, shell and file safety, signing and install paths), with findings reported before `VERSION` is bumped. Copilot stays the per-PR gate. Update the release lines in `AGENTS.md` ("Release and install") and `handoff/README.md` that say a release needs Claude Code.

Done when:

```sh
git grep -n -i claude -- content/skills/release   # prints nothing
git grep -n -i 'needs claude code\|claude code session' -- AGENTS.md handoff/README.md   # prints nothing
go test ./...
just leak-check
```

### 2. Lua: the IDE talks only to OpenCode

`bud.ai.agent` always starts `opencode acp` and ignores `ai.backend`; `bud.ai.session` always returns `bud.ai.agent`. Delete the OpenClaw, Codex, custom and `claude` branches in `agent.lua` (argv, TUI, install hints), `actions.mention`'s Claude Code `@path#La-b` syntax, and their tests and fixtures (`term_backend_test.lua`, `ide/tests/fixtures/openclaw/`, the backend cases in `agent_test.lua`, `acp_test.lua`, `chat_key_test.lua`, `actions_test.lua`). Keep `bud.ai.acp`, the fake ACP agent and the OpenCode cases. Update `docs/internals/acp.md` to describe only the OpenCode transport. Don't touch `bud.ai.claude` yet (task 3) or any Go file.

Done when:

```sh
git grep -n -i -E 'openclaw|codex' -- ide/ docs/internals/acp.md   # prints nothing
git grep -n 'ai.backend\|backend()' -- ide/nvim   # prints nothing
just lua-test
stylua --check ide && selene ide
just smoke
```

### 3. Lua: delete `bud.ai.claude` and the IDE bridge

Delete `ide/nvim/lua/bud/ai/claude.lua`, `ide/nvim/lua/bud/ai/bridge/`, and every require of them: `term.lua`, `chat.lua`, `permissions.lua` (its Claude path), `issue.lua` (the `plugin_dir` check: the file-issue skill now comes from synced content), `open.lua`, `core/autocmds.lua`, the dashboard's chat entry (it opens the opencode.nvim chat), settings, quit and statusline. Delete their tests (`claude_test.lua`, `claude_term_test.lua`, `claude_resume_default_test.lua`, `claude_plugin_test.lua`, `bridge_test.lua`, `lib/claude_backend.lua`) and the Claude cases in the others. Rename `docs/internals/claude-session.md` to `docs/internals/session.md`, rewrite it for what's left, and update its row in AGENTS.md's "Where the detail is" table. The IDE's references to the plugin's names become the synced content names while the plugin still exists: `issue.lua`'s prompt `bud-ide:file-issue` becomes `file-issue`, and `chat.lua`'s `bud-ide:chat` agent becomes `chat`, so task 6 can delete the plugin without breaking issue filing or chat. The Lua stops reading every `ai.claude.*` key; the Go side still defines them until task 5.

Done when (also `git grep -n 'bud-ide:' -- ide/nvim` prints nothing):

```sh
test ! -e ide/nvim/lua/bud/ai/claude.lua && test ! -e ide/nvim/lua/bud/ai/bridge
git grep -n -i claude -- ide/nvim ide/tests   # prints nothing
just lua-test
stylua --check ide && selene ide
just smoke
```

### 4. Go: drop `ai.backend` and the non-OpenCode backends

`internal/agent` knows only OpenCode: `Argv` returns `opencode acp`, and `config.Backends`, `ai.backend`, `ai.openclaw.*`, `ai.custom.*` and the OpenClaw gateway probe go. `bud ai setup` and `bud ai status` check OpenCode only; the doctor's backend check becomes an OpenCode check; `bud install` offers OpenCode, not a backend. Remove the keys' rows from `docs/config.md` (`TestDocsCoverEveryKey`) and the backend lines in `docs/commands.md`. Leave the `ai.claude.*` keys and `internal/claude` for task 5, so the two PRs touch different lines.

Done when:

```sh
git grep -n -i -E 'openclaw|codex|ai\.backend|ai\.custom|config\.Backends' -- internal cmd docs/config.md docs/commands.md   # prints nothing
go vet ./... && gofmt -l .   # gofmt prints nothing
go test ./...
```

### 5. Go: delete Claude Code support

Delete `internal/claude`, `internal/cli/claude.go`, `internal/cli/claudedir.go` and their tests; `cli.UseClaudeConfig` and its call in `cmd/bud/main.go`; the Claude Code and Headroom steps in `bud install`; the `claude code` and `claude command` doctor checks; `paths.ClaudeConfig`; the `ai.claude.*` keys; and the Claude path of `bud agent`. Port `bud chat` to OpenCode with the synced `chat` agent (`opencode --agent chat` in `paths.Chat`; the Go `chatAgent` constant drops its `bud-ide:` prefix). Delete `docs/internals/claude-code.md` and its AGENTS.md row, and remove the keys' rows from `docs/config.md` and `bud claude` from `docs/commands.md`.

Done when:

```sh
test ! -e internal/claude
git grep -n -i claude -- internal cmd docs/config.md docs/commands.md ':!internal/adapter/hermes' ':!internal/improve/digest.go' ':!internal/plugin' ':!internal/cli/plugin.go' ':!internal/paths/paths.go' ':!internal/nvim' ':!internal/installer/managed.go'   # prints nothing
git grep -n 'ClaudeConfig\|UseClaudeConfig' -- .   # prints nothing
go vet ./... && gofmt -l .
go test ./...
just smoke
```

### 6. Delete the embedded `bud-ide` plugin

First diff `ide/plugins/bud-ide` against `content/skills/file-issue`, `content/skills/bud-dev` and `content/agents/chat.md`, and carry anything the IDE still relies on into the content copies (the `bud-issues` JSON block `bud.ai.issue` parses, the notes on working inside bud). Then delete `ide/plugins/`, `internal/plugin`, `internal/cli/plugin.go`, `bud.Plugin()` in `embed.go`, `paths.Plugins`, the AGENTS.md rule about the plugin, and the plugin section of `docs/internals/cli.md`.

Done when:

```sh
test ! -e ide/plugins && test ! -e internal/plugin
git grep -n -i -E 'bud-ide|\.claude-plugin|bud\.Plugin' -- . ':!handoff'   # prints nothing
go test ./... && just lua-test
just smoke
```

### 7. Drop cloud work and Claude from working an issue

Delete `ai.work.where` and the cloud path of `bud.ai.work` (`claude --cloud`, `start_cloud`, the `claude -p` Linear helper and its `mcp__claude_ai_Linear__*` tool names, the `claude/` branch-prefix rule) and its tests. The prompts point at `AGENTS.md` instead of `CLAUDE.md`. Remove the key from `internal/config` and `docs/config.md`, and update `docs/issues.md`'s working section.

Done when:

```sh
git grep -n -i -E 'claude|ai\.work\.where|cloud' -- ide/nvim/lua/bud/ai/work.lua ide/nvim/lua/bud/ai/issue.lua ide/tests/lua/work_test.lua   # prints nothing
go test ./... && just lua-test
stylua --check ide && selene ide
```

### 8. User docs and site: OpenCode only

Delete `docs/claude-code.md`, `docs/claude-tui.md`, `docs/backends.md` and `docs/openclaw.md` and their `site/_data/nav.yml` lines. Rewrite the AI sections of `docs/setup.md`, `keys.md`, `layouts.md`, `issues.md`, `troubleshooting.md`, `tools.md`, `languages.md`, the remaining `docs/internals/*.md` mentions, `site/index.html`, `README.md`, and `ide/bundles/linux/bud.desktop`'s wording, so they describe OpenCode and opencode.nvim. Fix every link to a deleted page.

Done when:

```sh
git grep -n -i -E 'claude|openclaw|codex' -- docs site README.md ide/bundles ':!docs/internals/jga-348-worklog.md'   # prints nothing
just site-test
go test ./internal/config -run TestDocsCoverEveryKey
```

### 9. Living docs: AGENTS.md, delete CLAUDE.md, handoff, comments

Delete the root `CLAUDE.md` (it only imports AGENTS.md). Rewrite AGENTS.md's IDE chat paragraph and docs paragraph for OpenCode only; reword the living parts of `handoff/README.md`, `HANDOFF.md` and `handoff/state.md` (their history sentences stay); mark BASE.md's section 4.2 as superseded by D154 without deleting it; reword the code comments that still name Claude (`internal/improve/digest.go`'s "Claude threads", `server/improve/README.md`, `internal/installer/managed.go`, `internal/nvim/devlink.go`, `internal/nvim/startup.go`). Update `TestDigest`-style goldens if the digest text changes.

Done when:

```sh
test ! -e CLAUDE.md
git grep -n -i -E 'claude|openclaw|codex' -- . ':!handoff/decisions.md' ':!handoff/memory' ':!handoff/design' ':!handoff/notes' ':!docs/internals/jga-348-worklog.md' ':!BASE.md'   # prints only the "Stays as is" hits
go test ./...
just leak-check
```

## Spillover, Jason's call

- `jgarner9/bud-plugins` still holds the Claude-era `bud` plugin and its release skill.
- The private overlay's `handoff/` and profiles still use Claude-era wording.
- JGA-399 (go-imap) lives on an unpushed branch in a worktree on the home server; it needs Jason or a session there, not a cloud thread. It claims Suite D138, so this plan's decision is D139.
- `TestAgentPTYArgumentsAndStatus` (`internal/cli/agent_test.go`) flaked once on CI with a pty read race; file it if it recurs.
