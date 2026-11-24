# Handoff: Bud Suite, from Claude Code to OpenCode

Written 2026-11-22 by the Claude Code project thread "Hand off Bud project to OpenCode", when Jason's weekly Claude usage ran low. Until then the work ran in a Claude Code project whose shared folder, memory and thread history aren't visible from the laptop. Everything a fresh session needs is in this folder, or in the private overlay for anything personal. Read it in this order:

| File | What it holds |
| --- | --- |
| this README | How Jason works, the PR workflow, the rules, and the resume prompt |
| [state.md](state.md) | Where everything stands: what's merged, what's in flight, what's next, in order |
| [revisit.md](revisit.md) | Everything waiting on Jason: home-server rollouts he hasn't done, and decisions |
| [decisions.md](decisions.md) | The Suite decision log, D1 to D132, with the reason for each |
| [design/](design/) | The design docs the decisions came from (listed below) |
| [memory/](memory/) | The Claude project's memory, one file per subject (`MEMORY.md` is the index) |

Personal and host details (the real host names, tailnet IPs, real `op://` paths and the exact paste blocks for the home server) are in the private overlay, `jgarner9/bud-overlay`, under `handoff/`. That folder isn't synced to devices; `bud sync` reads only the overlay's `profiles/`, `env/` and `content/`. Here the machines are named by role: **the home server** runs Hermes, the gateway, the router and the other Bud services; **the Qwen box** runs the local model and sleeps; **the laptop** is Jason's dev machine.

The design docs and memory files were copied as they were, with host names replaced by roles. They still cite paths like `/mnt/project-files/architecture/<name>`, which was the Claude project's shared folder: read those as `handoff/design/<name>`, `/mnt/project-files/notes/<name>` and `/mnt/project-files/handoff/<name>` as `handoff/notes/<name>` (the old project-side `handoff/HANDOFF.md` and `architecture/HANDOFF.md` are `handoff/notes/project-handoff-2026-11-21.md`), and `/mnt/project-files/revisit-*.md` as [revisit.md](revisit.md). Status lines inside the design docs are as they were when each was written; [state.md](state.md) is what's current.

## How Jason works

These are his standing preferences. Follow them in every reply, commit and PR.

- **Lead with the answer**, then only the detail that helps. Say when you're unsure and what you checked, and tell him plainly when you think he's wrong.
- **Plain Unicode symbols, never emoji.** Write professionally and in full sentences. When explaining a change or teaching, say what it is and why.
- **He drives the design; you support.** For a decision that's genuinely his (a design trade-off, a delete, a default he'll feel), give two or three options with the trade-off of each in a line and your recommendation, and let him choose. Keep changes to what he asked for: an idea that comes up mid-task gets noted (in [revisit.md](revisit.md) or a Linear issue), not built.
- **Decision-only items: take the recommendation.** His standing rule from 2026-11-21: "if it's just a decision needs to be made go with the recommendation." Take the recommended option, record it as a Suite decision noting it was taken on recommendation, and tell him so he can reverse it. This doesn't cover security-sensitive loosening (privacy tiers, approvals: he decides those, as he did D114 to D121), deletions, or anything on the home server that needs his hands.
- **Full insight into the architecture.** He's vibe coding Bud for now, but wants to understand every part. Keep the docs (`docs/architecture.md`, `docs/internals/`, `BASE.md`, [decisions.md](decisions.md)) complete, with the reason for each component, and explain what you built and why.
- **In use, Bud is his assistant**, not the driver: it gathers, drafts, remembers and lays out options; he decides.
- **Fewer Signal prompts.** He finds the approval prompts on his phone too many; don't add new ones without saying so. He likes plain-text prompts (no JSON) and dislikes Telegram.
- **Paste blocks for his shell.** His shell is zsh with no interactive comments, so a block he pastes has no `#` lines. Put explanations above the block, not inside it.
- **Home-server privileged commands are his.** Agent sessions on the home server can't run approval-loosening commands, `op`, merges or binary replacement; give him the exact commands to paste. A Remote Control or agent session there that asks permission every turn should be restarted in auto mode (his standing request).

## The skills

In Claude Code these were the `bud` plugin's skills (from `jgarner9/bud-plugins`). If your OpenCode session can't load them, these are the rules that matter most:

- **bud:code-style** (writing, reviewing or committing code): the repo's `AGENTS.md`, formatter and surrounding code win. Standard library first (D1 here); `flag.FlagSet` CLIs, flags before positionals; one source of truth per concern; deterministic output; JSON config that keeps unknown keys; build only what was asked. Functions return errors; only the entry point prints one `bud: message` line and picks the exit code (0, 1, 2 for usage). Errors say what was being done, lowercase, and what to do next. gofmt, `go vet`, shellcheck and stylua clean. Lowercase conventional commits with the issue id: `fix: skip dot entries in dev link (JGA-167)`, one commit per task once the checks pass.
- **bud:planning** (specs, plans, decisions, issues): read what exists before asking; ask only what changes the plan; give options with a recommendation. Decisions are numbered and dated with the reason, never renumbered or deleted; a changed one is marked superseded and the new one gets the next number. Open questions have their own section. Tasks are one commit each with their own check, ordered by dependency. Status lives in Linear (team JGA, project "Bud Suite"); the plan and Linear never disagree. Don't file issues for a plan he hasn't approved, and never put secrets in a doc or issue.
- **bud:tools** (looking anything up): use the tools you have before guessing, and say in one line when one you'd expect is missing. Code: repomap, then a language server or Serena, then `rg`/`ast-grep` (search only). Facts: what can't change from knowledge; his own projects from memory; libraries from Context7; anything else current from Exa or web search. End an answer that needed a lookup with one source line such as `◇ context7 /neovim/neovim`.
- **bud:verify**: say done only after running the check in this session and reading its output. A check you couldn't run is "not verified", with why.
- **bud:release** cuts a release with a code review and a security review of everything since the last tag. It needs Claude Code; leave releases for a Claude Code session or for Jason.

## The PR workflow

Jason authorized this on 2026-11-20 ("leave a comment and automatically merge... just crank out tasks") and it still stands:

1. One task, one branch, one PR. Run `just check` (or the tests that cover the change) before opening it, and show the result.
2. Start the PR body with what changed and why, the Linear issue, and how it was tested. Assign Jason.
3. Request a Copilot review and wait for it. A review with no comments still counts as done, and nothing notifies you about it: check the PR for it.
4. Fix every real finding, push all the fixes, reply on and resolve each thread, then leave **one top-level comment** that lists each finding, what changed (or why not) and the commit that carries it. Thread replies alone aren't enough, because he can't tell when a round is finished.
5. When CI is green, merge it yourself (squash). Ping him only for a genuine decision, with a recommendation.
6. Never merge a PR labelled `bud-proposal` (the self-improvement loop's PRs, D126): merging one is his approval of that change. Never force-push a shared branch; merge the old head instead.

Releases come from `VERSION` (see `AGENTS.md`, "Release and install"); a push to main that changes `content/` cuts an edge content release, and promoting it to stable is `gh workflow run content.yml -R jgarner9/bud -f mode=promote -f version=<v>`, which Jason runs or asks for.

## Rules that hold everywhere

- This repo is public and CI fails on personal details (`just leak-check`; the `BUD_LEAK_PATTERNS` secret holds his own strings). Host names, tailnet IPs, real `op://` paths, his address and personal skills go in the overlay.
- Secrets are 1Password `op://` references resolved at launch; no agent ever reads a value (D7). The home server's service account sees only the Bud vault.
- Action tiers (D10): do alone (reads, drafts, working memory, texts to Jason); approve on his phone (sends, pushes to main, spending, infrastructure, secrets); never (deleting vault items, revealing secrets, disabling approvals).
- Privacy tiers (D23, loosened by D114 to D121): P0 private stays on local models unless he approves one request on Signal (D104); P1 internal may use OpenCode Go; P2 public. Cost tiers: C0 local, C1 Go included, C2 top-up capped at about $10 a month.
- `AGENTS.md`'s hard constraints and "Where the detail is" table apply as usual.

## Resume prompt

Paste this into OpenCode at the root of a bud checkout:

```text
Resume the Bud Suite work from the handoff. Read handoff/README.md first and follow its "How Jason works" and PR workflow sections for everything you do. Then read handoff/state.md for where things stand and handoff/revisit.md for what's waiting on me; decisions are in handoff/decisions.md and the designs in handoff/design/. For host names and the exact home-server steps, read handoff/ in my overlay checkout (jgarner9/bud-overlay). Linear (team JGA, project "Bud Suite", milestone Bud 2.0) is the source of truth for status, so check the issues state.md names before starting. Fetch origin first and confirm main matches state.md. Then tell me in a few lines where things stand and recommend the next task; don't start building until I say which.
```

## Keeping this folder current

When you finish a task, update [state.md](state.md) and [revisit.md](revisit.md) in the same PR, and add any new Suite decision to [decisions.md](decisions.md) with the next number. The memory files are a snapshot of 2026-11-22; record new lasting facts in `state.md` or the relevant doc rather than editing them.
